package cmd

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode"

	"github.com/angch/gogcbot/pkg/config"
	"github.com/angch/gogcbot/pkg/db"
	"github.com/gotd/td/bin"
	"github.com/gotd/td/session"
	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/auth"
	"github.com/gotd/td/telegram/query/channels/participants"
	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"
	"github.com/spf13/cobra"
)

var (
	seedAPIID     int
	seedAPIHash   string
	seedPhone     string
	seedSession   string
	seedAllGroups bool
	seedDryRun    bool
	seedDelayMs   int
)

type terminalAuth struct {
	phone string
}

func (a terminalAuth) Phone(ctx context.Context) (string, error) {
	if a.phone != "" {
		return a.phone, nil
	}
	reader := bufio.NewReader(os.Stdin)
	fmt.Print("Enter your phone number (e.g. +1234567890): ")
	phone, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(phone), nil
}

func (a terminalAuth) Password(ctx context.Context) (string, error) {
	reader := bufio.NewReader(os.Stdin)
	fmt.Print("Enter your 2FA password (if configured): ")
	pwd, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(pwd), nil
}

func (a terminalAuth) AcceptTermsOfService(ctx context.Context, tos tg.HelpTermsOfService) error {
	return nil
}

func (a terminalAuth) SignUp(ctx context.Context) (auth.UserInfo, error) {
	return auth.UserInfo{}, errors.New("sign-up is not supported in this script")
}

func (a terminalAuth) Code(ctx context.Context, sentCode *tg.AuthSentCode) (string, error) {
	reader := bufio.NewReader(os.Stdin)
	fmt.Print("Enter Telegram login code: ")
	code, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(code), nil
}

// floodWaitMiddleware automatically intercepts Telegram FLOOD_WAIT errors and sleeps before retrying.
type floodWaitMiddleware struct {
	maxRetries int
	out        io.Writer
}

func (m floodWaitMiddleware) Handle(next tg.Invoker) telegram.InvokeFunc {
	return func(ctx context.Context, input bin.Encoder, output bin.Decoder) error {
		retries := 0
		for {
			err := next.Invoke(ctx, input, output)
			if err == nil {
				return nil
			}

			if d, ok := tgerr.AsFloodWait(err); ok {
				retries++
				if m.maxRetries > 0 && retries > m.maxRetries {
					return err
				}
				waitDuration := d + 1*time.Second
				if m.out != nil {
					fmt.Fprintf(m.out, "⏳ Telegram FLOOD_WAIT hit: waiting %v before retrying (attempt %d)...\n", waitDuration, retries)
				}
				select {
				case <-time.After(waitDuration):
					continue
				case <-ctx.Done():
					return ctx.Err()
				}
			}

			return err
		}
	}
}

// SeedMembersSummary holds the statistics collected during the seed operation.
type SeedMembersSummary struct {
	TotalGroups        int
	ScannedGroups      int
	FailedGroups       int
	TotalParticipants  int
	NewUsersCreated    int
	ExistingUsersSeen  int
	TotalJoinsRecorded int
}

var seedMembersCmd = &cobra.Command{
	Use:     "seedmembers",
	Aliases: []string{"seed-members", "seed", "seedusers"},
	Short:   "Iterate through groups in sqlite db and seed their members into users & user_joins tables via MTProto",
	Long: `Connects to Telegram via MTProto user API, iterates through the groups configured in the SQLite database,
fetches all group/channel participants, and seeds the users into the 'users' table alongside their group membership
in the 'user_joins' table.

Credentials can be supplied via flags (--api-id, --api-hash, --phone) or environment variables (TG_API_ID, TG_API_HASH, TG_PHONE).`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.LoadConfig(cfgFile)
		if err != nil {
			return fmt.Errorf("failed to load config from '%s': %w", cfgFile, err)
		}

		dbPath := cfg.DBPath
		if !filepath.IsAbs(dbPath) {
			dbPath = filepath.Join(filepath.Dir(cfgFile), dbPath)
		}

		database, err := db.OpenDB(dbPath)
		if err != nil {
			return fmt.Errorf("failed to open database at '%s': %w", dbPath, err)
		}
		defer database.Close()

		// Resolve API ID, API Hash, Phone, and Session
		apiID := seedAPIID
		apiHash := seedAPIHash
		phone := seedPhone
		sessionFile := seedSession

		if apiID == 0 {
			if envID := os.Getenv("TG_API_ID"); envID != "" {
				if parsed, err := strconv.Atoi(envID); err == nil {
					apiID = parsed
				}
			}
		}
		if apiHash == "" {
			if envHash := os.Getenv("TG_API_HASH"); envHash != "" {
				apiHash = envHash
			}
		}
		if phone == "" {
			if envPhone := os.Getenv("TG_PHONE"); envPhone != "" {
				phone = envPhone
			}
		}
		if envSession := os.Getenv("TG_SESSION"); envSession != "" && sessionFile == "session_dump.json" {
			sessionFile = envSession
		}

		reader := bufio.NewReader(os.Stdin)
		if apiID == 0 {
			fmt.Print("Enter Telegram API ID (from https://my.telegram.org): ")
			line, _ := reader.ReadString('\n')
			apiID, _ = strconv.Atoi(strings.TrimSpace(line))
		}
		if apiHash == "" {
			fmt.Print("Enter Telegram API Hash: ")
			line, _ := reader.ReadString('\n')
			apiHash = strings.TrimSpace(line)
		}

		if apiID == 0 || apiHash == "" {
			return errors.New("both api-id and api-hash must be provided via flags, environment variables, or interactive prompt")
		}

		// Retrieve target groups from DB
		var groups []db.Group
		if seedAllGroups {
			groups, err = database.GetAllGroups()
		} else {
			groups, err = database.GetMonitoredGroups()
		}
		if err != nil {
			return fmt.Errorf("failed to query groups from database: %w", err)
		}

		out := cmd.OutOrStdout()
		if len(groups) == 0 {
			fmt.Fprintln(out, "ℹ️ No groups found in the database to seed members from.")
			fmt.Fprintln(out, "   Add groups first using `/addgroup` in Telegram or by configuring monitored groups.")
			return nil
		}

		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer cancel()

		delay := time.Duration(seedDelayMs) * time.Millisecond
		if delay < 0 {
			delay = 0
		}

		fmt.Fprintln(out, "🚀 Starting member seeding from SQLite groups via MTProto...")
		fmt.Fprintf(out, "Database: %s | Total Groups: %d | Dry Run: %t | Delay: %v\n\n",
			dbPath, len(groups), seedDryRun, delay)

		summary, err := runSeedMembers(ctx, apiID, apiHash, phone, sessionFile, groups, cfg, database, seedDryRun, delay, out)
		if err != nil && !errors.Is(err, context.Canceled) {
			return err
		}

		printSummary(out, summary, seedDryRun)
		return nil
	},
}

func fetchAllDialogs(ctx context.Context, api *tg.Client, out io.Writer) (map[int64]*tg.Channel, map[int64]*tg.Chat, map[string]*tg.Channel, map[string]*tg.Chat) {
	channelMap := make(map[int64]*tg.Channel)
	chatMap := make(map[int64]*tg.Chat)
	titleMap := make(map[string]*tg.Channel)
	chatTitleMap := make(map[string]*tg.Chat)

	// Fetch from both folder 0 (Main) and folder 1 (Archived)
	for _, folderID := range []int{0, 1} {
		offsetDate := 0
		offsetID := 0
		var offsetPeer tg.InputPeerClass = &tg.InputPeerEmpty{}

		for {
			res, err := api.MessagesGetDialogs(ctx, &tg.MessagesGetDialogsRequest{
				FolderID:   folderID,
				OffsetDate: offsetDate,
				OffsetID:   offsetID,
				OffsetPeer: offsetPeer,
				Limit:      100,
			})
			if err != nil {
				break
			}

			var chats []tg.ChatClass
			var dialogsList []tg.DialogClass
			var messagesList []tg.MessageClass

			switch d := res.(type) {
			case *tg.MessagesDialogs:
				chats = d.Chats
				dialogsList = d.Dialogs
				messagesList = d.Messages
			case *tg.MessagesDialogsSlice:
				chats = d.Chats
				dialogsList = d.Dialogs
				messagesList = d.Messages
			default:
				break
			}

			for _, c := range chats {
				switch ch := c.(type) {
				case *tg.Channel:
					channelMap[ch.ID] = ch
					if ch.Title != "" {
						titleMap[strings.ToLower(strings.TrimSpace(ch.Title))] = ch
					}
				case *tg.Chat:
					chatMap[ch.ID] = ch
					if ch.Title != "" {
						chatTitleMap[strings.ToLower(strings.TrimSpace(ch.Title))] = ch
					}
				}
			}

			if len(dialogsList) == 0 {
				break
			}

			if _, isFull := res.(*tg.MessagesDialogs); isFull {
				break
			}

			lastDialog := dialogsList[len(dialogsList)-1]
			if p, ok := lastDialog.(interface{ GetPeer() tg.PeerClass }); ok {
				peerClass := p.GetPeer()
				switch pc := peerClass.(type) {
				case *tg.PeerChannel:
					if ch, ok := channelMap[pc.ChannelID]; ok {
						offsetPeer = ch.AsInputPeer()
					} else {
						offsetPeer = &tg.InputPeerChannel{ChannelID: pc.ChannelID}
					}
				case *tg.PeerChat:
					offsetPeer = &tg.InputPeerChat{ChatID: pc.ChatID}
				case *tg.PeerUser:
					offsetPeer = &tg.InputPeerUser{UserID: pc.UserID}
				default:
					offsetPeer = &tg.InputPeerEmpty{}
				}
			}

			if len(messagesList) > 0 {
				lastMsg := messagesList[len(messagesList)-1]
				if msg, ok := lastMsg.AsNotEmpty(); ok {
					offsetDate = msg.GetDate()
					offsetID = msg.GetID()
				}
			} else {
				break
			}
		}
	}

	return channelMap, chatMap, titleMap, chatTitleMap
}

func runSeedMembers(
	ctx context.Context,
	apiID int,
	apiHash string,
	phone string,
	sessionFile string,
	groups []db.Group,
	cfg *config.Config,
	database *db.DB,
	dryRun bool,
	delay time.Duration,
	out io.Writer,
) (*SeedMembersSummary, error) {
	summary := &SeedMembersSummary{
		TotalGroups: len(groups),
	}

	client := telegram.NewClient(apiID, apiHash, telegram.Options{
		SessionStorage: &session.FileStorage{
			Path: sessionFile,
		},
		Middlewares: []telegram.Middleware{
			floodWaitMiddleware{
				maxRetries: 10,
				out:        out,
			},
		},
	})

	err := client.Run(ctx, func(ctx context.Context) error {
		flow := auth.NewFlow(
			terminalAuth{phone: phone},
			auth.SendCodeOptions{},
		)

		if err := client.Auth().IfNecessary(ctx, flow); err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}

		api := client.API()

		fmt.Fprintln(out, "🔄 Indexing Telegram dialogs (channels and chats)...")
		channelMap, chatMap, titleMap, chatTitleMap := fetchAllDialogs(ctx, api, out)
		fmt.Fprintf(out, "ℹ️ Indexed %d channel(s) and %d basic chat(s) from account dialogs.\n\n", len(channelMap), len(chatMap))

		defaultRep := 0
		if cfg != nil {
			defaultRep = cfg.Reputation.DefaultInitial
		}

		for i, g := range groups {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}

			fmt.Fprintf(out, "[%d/%d] 🔍 Processing group: \"%s\" (ID: %d, Type: %s)...\n",
				i+1, len(groups), g.Title, g.ChatID, g.Type)

			// Find MTProto Channel or Chat
			channel, basicChat := findPeer(g, channelMap, chatMap, titleMap, chatTitleMap)

			// Fallback: search via ContactsSearch if not found in local dialog index
			if channel == nil && basicChat == nil && g.Title != "" {
				cleanTitle := normalizeText(g.Title)
				if len(cleanTitle) >= 3 {
					found, err := api.ContactsSearch(ctx, &tg.ContactsSearchRequest{
						Q:     cleanTitle,
						Limit: 5,
					})
					if err == nil && len(found.Chats) > 0 {
						for _, c := range found.Chats {
							switch ch := c.(type) {
							case *tg.Channel:
								if matchChannelOrChat(g, ch.ID, ch.Title) {
									channel = ch
									channelMap[ch.ID] = ch
									break
								}
							case *tg.Chat:
								if matchChannelOrChat(g, ch.ID, ch.Title) {
									basicChat = ch
									chatMap[ch.ID] = ch
									break
								}
							}
						}
					}
				}
			}

			if channel == nil && basicChat == nil {
				fmt.Fprintf(out, "  ⚠️ Could not resolve group \"%s\" (ID: %d) in MTProto dialogs. Skipping...\n\n", g.Title, g.ChatID)
				summary.FailedGroups++
				continue
			}

			groupParticipants := 0
			groupNewUsers := 0
			groupExistingUsers := 0
			groupJoins := 0

			if channel != nil {
				inputChannel := channel.AsInput()
				q := participants.NewQueryBuilder(api).GetParticipants(inputChannel).Recent()
				if err := q.ForEach(ctx, func(ctx context.Context, p participants.Elem) error {
					user, ok := p.User()
					if !ok {
						return nil
					}

					groupParticipants++
					summary.TotalParticipants++

					isAdmin := false
					if _, _, ok := p.Admin(); ok {
						isAdmin = true
					} else if _, _, ok := p.Creator(); ok {
						isAdmin = true
					}

					initialRep := defaultRep
					if isAdmin {
						initialRep = 100
					}

					chatType := g.Type
					if chatType == "" {
						if channel.Megagroup {
							chatType = "supergroup"
						} else if channel.Broadcast {
							chatType = "channel"
						} else {
							chatType = "supergroup"
						}
					}

					if !dryRun {
						dbUser, created, err := database.GetOrCreateUser(
							user.ID,
							user.Username,
							user.FirstName,
							user.LastName,
							initialRep,
						)
						if err != nil {
							return fmt.Errorf("failed to save user %d: %w", user.ID, err)
						}

						if created {
							groupNewUsers++
							summary.NewUsersCreated++
						} else {
							groupExistingUsers++
							summary.ExistingUsersSeen++
						}

						if isAdmin && !dbUser.IsAdmin {
							_ = database.SetUserAdmin(user.ID, true)
						}

						if user.LangCode != "" || user.Premium {
							_ = database.UpdateUserMetadata(user.ID, user.LangCode, user.Premium)
						}

						if err := database.EnsureUserJoin(user.ID, g.ChatID, g.Title, chatType); err != nil {
							return fmt.Errorf("failed to record user join for %d in %d: %w", user.ID, g.ChatID, err)
						}
						groupJoins++
						summary.TotalJoinsRecorded++
					} else {
						groupJoins++
						summary.TotalJoinsRecorded++
					}

					return nil
				}); err != nil {
					fmt.Fprintf(out, "  ❌ Error fetching participants for \"%s\": %v\n\n", g.Title, err)
					summary.FailedGroups++
					continue
				}
			} else if basicChat != nil {
				fullChat, err := api.MessagesGetFullChat(ctx, basicChat.ID)
				if err != nil {
					fmt.Fprintf(out, "  ❌ Error fetching full chat for \"%s\": %v\n\n", g.Title, err)
					summary.FailedGroups++
					continue
				}

				chatType := g.Type
				if chatType == "" {
					chatType = "group"
				}

				for _, u := range fullChat.Users {
					user, ok := u.(*tg.User)
					if !ok {
						continue
					}

					groupParticipants++
					summary.TotalParticipants++

					if !dryRun {
						_, created, err := database.GetOrCreateUser(
							user.ID,
							user.Username,
							user.FirstName,
							user.LastName,
							defaultRep,
						)
						if err != nil {
							continue
						}

						if created {
							groupNewUsers++
							summary.NewUsersCreated++
						} else {
							groupExistingUsers++
							summary.ExistingUsersSeen++
						}

						if user.LangCode != "" || user.Premium {
							_ = database.UpdateUserMetadata(user.ID, user.LangCode, user.Premium)
						}

						if err := database.EnsureUserJoin(user.ID, g.ChatID, g.Title, chatType); err == nil {
							groupJoins++
							summary.TotalJoinsRecorded++
						}
					} else {
						groupJoins++
						summary.TotalJoinsRecorded++
					}
				}
			}

			summary.ScannedGroups++
			if dryRun {
				fmt.Fprintf(out, "  -> [DRY RUN] Found %d participants from Telegram\n\n", groupParticipants)
			} else {
				fmt.Fprintf(out, "  -> ✅ Seeded %d participants (%d new users created, %d existing updated, %d join records saved)\n\n",
					groupParticipants, groupNewUsers, groupExistingUsers, groupJoins)
			}

			if delay > 0 && i < len(groups)-1 {
				time.Sleep(delay)
			}
		}

		return nil
	})

	return summary, err
}

func getCandidateIDs(chatID int64) []int64 {
	ids := []int64{chatID}
	idStr := fmt.Sprintf("%d", chatID)
	if strings.HasPrefix(idStr, "-100") {
		if raw, err := strconv.ParseInt(strings.TrimPrefix(idStr, "-100"), 10, 64); err == nil {
			ids = append(ids, raw)
		}
	}
	if chatID < 0 {
		ids = append(ids, -chatID)
	}
	if chatID > 0 {
		ids = append(ids, -chatID)
		if parsed, err := strconv.ParseInt("-100"+idStr, 10, 64); err == nil {
			ids = append(ids, parsed)
		}
	}
	return ids
}

func normalizeText(s string) string {
	var sb strings.Builder
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.Is(unicode.Han, r) {
			sb.WriteRune(r)
		}
	}
	return sb.String()
}

func matchChannelOrChat(g db.Group, peerID int64, peerTitle string) bool {
	candidateIDs := getCandidateIDs(g.ChatID)
	for _, cid := range candidateIDs {
		if cid == peerID {
			return true
		}
	}

	normGTitle := normalizeText(g.Title)
	normPTitle := normalizeText(peerTitle)
	if normGTitle != "" && normPTitle != "" {
		if normGTitle == normPTitle || strings.Contains(normGTitle, normPTitle) || strings.Contains(normPTitle, normGTitle) {
			return true
		}
	}

	return false
}

func findPeer(
	g db.Group,
	channelMap map[int64]*tg.Channel,
	chatMap map[int64]*tg.Chat,
	titleMap map[string]*tg.Channel,
	chatTitleMap map[string]*tg.Chat,
) (*tg.Channel, *tg.Chat) {
	candidateIDs := getCandidateIDs(g.ChatID)

	// 1. Try matching by candidate IDs
	for _, cid := range candidateIDs {
		if ch, ok := channelMap[cid]; ok {
			return ch, nil
		}
		if chat, ok := chatMap[cid]; ok {
			return nil, chat
		}
	}

	// 2. Try exact title match (case-insensitive)
	cleanTitle := strings.ToLower(strings.TrimSpace(g.Title))
	if cleanTitle != "" {
		if ch, ok := titleMap[cleanTitle]; ok {
			return ch, nil
		}
		if chat, ok := chatTitleMap[cleanTitle]; ok {
			return nil, chat
		}
	}

	// 3. Try fuzzy/normalized title match
	normGTitle := normalizeText(g.Title)
	if normGTitle != "" {
		for _, ch := range channelMap {
			normChTitle := normalizeText(ch.Title)
			if normChTitle != "" && (normChTitle == normGTitle || strings.Contains(normChTitle, normGTitle) || strings.Contains(normGTitle, normChTitle)) {
				return ch, nil
			}
		}
		for _, chat := range chatMap {
			normChatTitle := normalizeText(chat.Title)
			if normChatTitle != "" && (normChatTitle == normGTitle || strings.Contains(normChatTitle, normGTitle) || strings.Contains(normGTitle, normChatTitle)) {
				return nil, chat
			}
		}
	}

	return nil, nil
}

func printSummary(out io.Writer, summary *SeedMembersSummary, dryRun bool) {
	if summary == nil {
		return
	}
	dryTag := ""
	if dryRun {
		dryTag = " [DRY RUN]"
	}

	fmt.Fprintln(out, "📊 === Member Seeding Summary ===")
	fmt.Fprintf(out, "Total Groups in Database : %d\n", summary.TotalGroups)
	fmt.Fprintf(out, "Groups Scanned%s       : %d\n", dryTag, summary.ScannedGroups)
	fmt.Fprintf(out, "Groups Skipped / Failed  : %d\n", summary.FailedGroups)
	fmt.Fprintf(out, "Total Participants Found : %d\n", summary.TotalParticipants)
	if !dryRun {
		fmt.Fprintf(out, "New Users Created        : %d\n", summary.NewUsersCreated)
		fmt.Fprintf(out, "Existing Users Updated   : %d\n", summary.ExistingUsersSeen)
		fmt.Fprintf(out, "Group Joins Recorded     : %d\n", summary.TotalJoinsRecorded)
	}
}

func init() {
	seedMembersCmd.Flags().IntVar(&seedAPIID, "api-id", 0, "Telegram API ID (or set TG_API_ID)")
	seedMembersCmd.Flags().StringVar(&seedAPIHash, "api-hash", "", "Telegram API Hash (or set TG_API_HASH)")
	seedMembersCmd.Flags().StringVar(&seedPhone, "phone", "", "Phone number for Telegram authentication (or set TG_PHONE)")
	seedMembersCmd.Flags().StringVar(&seedSession, "session", "session_dump.json", "Session file path for Telegram MTProto client (or set TG_SESSION)")
	seedMembersCmd.Flags().BoolVarP(&seedAllGroups, "all", "a", false, "Seed members for all groups in database (default is monitored groups only)")
	seedMembersCmd.Flags().BoolVar(&seedDryRun, "dry-run", false, "Dry run mode (simulate fetching without modifying database)")
	seedMembersCmd.Flags().IntVar(&seedDelayMs, "delay-ms", 200, "Delay in milliseconds between groups to prevent rate limits")
	rootCmd.AddCommand(seedMembersCmd)
}
