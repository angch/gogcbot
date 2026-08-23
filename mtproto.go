//go:build ignore

package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/gotd/td/bin"
	"github.com/gotd/td/session"
	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/auth"
	"github.com/gotd/td/telegram/query/channels/participants"
	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"
)

// Obtain api_id and api_hash from https://my.telegram.org
const (
	defaultAPIID         = 12345678
	defaultAPIHash       = "your_api_hash"
	defaultGroupUsername = "your_group_username_or_id"
)

type Member struct {
	ID        int64  `json:"id"`
	Username  string `json:"username"`
	FirstName string `json:"first_name"`
	IsBot     bool   `json:"is_bot"`
}

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
	return auth.UserInfo{}, errors.New("sign-up is not supported")
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

type floodWaitMiddleware struct {
	maxRetries int
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
				fmt.Printf("⏳ Telegram FLOOD_WAIT hit: waiting %v before retrying (attempt %d)...\n", waitDuration, retries)
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

func resolveGroup(ctx context.Context, api *tg.Client, groupInput string) (tg.InputChannelClass, *tg.Chat, error) {
	cleanTarget := strings.TrimSpace(groupInput)
	cleanTarget = strings.TrimPrefix(cleanTarget, "https://")
	cleanTarget = strings.TrimPrefix(cleanTarget, "http://")
	cleanTarget = strings.TrimPrefix(cleanTarget, "t.me/")
	cleanTarget = strings.TrimPrefix(cleanTarget, "@")

	if cleanTarget == "" {
		return nil, nil, errors.New("group username or ID cannot be empty")
	}

	// Try numeric ID first if target is a number
	if id, err := strconv.ParseInt(cleanTarget, 10, 64); err == nil {
		var targetChannelID int64
		if strings.HasPrefix(cleanTarget, "-100") {
			targetChannelID, _ = strconv.ParseInt(strings.TrimPrefix(cleanTarget, "-100"), 10, 64)
		} else if id < 0 {
			targetChannelID = -id
		} else {
			targetChannelID = id
		}

		dialogs, err := api.MessagesGetDialogs(ctx, &tg.MessagesGetDialogsRequest{
			Limit:      100,
			OffsetPeer: &tg.InputPeerEmpty{},
		})
		if err == nil {
			var chats []tg.ChatClass
			switch d := dialogs.(type) {
			case *tg.MessagesDialogs:
				chats = d.Chats
			case *tg.MessagesDialogsSlice:
				chats = d.Chats
			}

			for _, c := range chats {
				switch ch := c.(type) {
				case *tg.Channel:
					if ch.ID == targetChannelID {
						return ch.AsInput(), nil, nil
					}
				case *tg.Chat:
					if ch.ID == targetChannelID {
						return nil, ch, nil
					}
				}
			}
		}
	}

	// Resolve by username
	resolved, err := api.ContactsResolveUsername(ctx, &tg.ContactsResolveUsernameRequest{
		Username: cleanTarget,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to resolve username %q: %w", cleanTarget, err)
	}

	if len(resolved.Chats) == 0 {
		return nil, nil, fmt.Errorf("no chat or channel found for username %q", cleanTarget)
	}

	switch ch := resolved.Chats[0].(type) {
	case *tg.Channel:
		return ch.AsInput(), nil, nil
	case *tg.Chat:
		return nil, ch, nil
	default:
		return nil, nil, fmt.Errorf("resolved peer is of unexpected type: %T", ch)
	}
}

func main() {
	apiIDFlag := flag.Int("api-id", defaultAPIID, "Telegram API ID (or set TG_API_ID)")
	apiHashFlag := flag.String("api-hash", defaultAPIHash, "Telegram API Hash (or set TG_API_HASH)")
	groupFlag := flag.String("group", defaultGroupUsername, "Target group username or chat ID (or set TG_GROUP)")
	phoneFlag := flag.String("phone", "", "Phone number for authentication (or set TG_PHONE)")
	sessionFlag := flag.String("session", "session_dump.json", "Session file path (or set TG_SESSION)")
	outputFlag := flag.String("output", "existing_members.json", "Output JSON file path")
	flag.Parse()

	apiID := *apiIDFlag
	apiHash := *apiHashFlag
	groupUsername := *groupFlag
	phone := *phoneFlag
	sessionFile := *sessionFlag
	outputFile := *outputFlag

	// Check environment variables if default/empty
	if apiID == 0 || apiID == defaultAPIID {
		if envID := os.Getenv("TG_API_ID"); envID != "" {
			if parsed, err := strconv.Atoi(envID); err == nil {
				apiID = parsed
			}
		}
	}
	if apiHash == "" || apiHash == defaultAPIHash {
		if envHash := os.Getenv("TG_API_HASH"); envHash != "" {
			apiHash = envHash
		}
	}
	if groupUsername == "" || groupUsername == defaultGroupUsername {
		if envGroup := os.Getenv("TG_GROUP"); envGroup != "" {
			groupUsername = envGroup
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
	if apiID == 0 || apiID == defaultAPIID {
		fmt.Print("Enter Telegram API ID: ")
		line, _ := reader.ReadString('\n')
		apiID, _ = strconv.Atoi(strings.TrimSpace(line))
	}
	if apiHash == "" || apiHash == defaultAPIHash {
		fmt.Print("Enter Telegram API Hash: ")
		line, _ := reader.ReadString('\n')
		apiHash = strings.TrimSpace(line)
	}
	if groupUsername == "" || groupUsername == defaultGroupUsername {
		fmt.Print("Enter group username or ID: ")
		line, _ := reader.ReadString('\n')
		groupUsername = strings.TrimSpace(line)
	}

	if apiID == 0 || apiHash == "" || groupUsername == "" {
		fmt.Fprintln(os.Stderr, "Error: api_id, api_hash, and group_username must be provided.")
		os.Exit(1)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	client := telegram.NewClient(apiID, apiHash, telegram.Options{
		SessionStorage: &session.FileStorage{
			Path: sessionFile,
		},
		Middlewares: []telegram.Middleware{
			floodWaitMiddleware{maxRetries: 10},
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

		inputChannel, basicChat, err := resolveGroup(ctx, api, groupUsername)
		if err != nil {
			return fmt.Errorf("error resolving group %q: %w", groupUsername, err)
		}

		var members []Member
		seen := make(map[int64]bool)

		if inputChannel != nil {
			// Supergroup or channel
			q := participants.NewQueryBuilder(api).GetParticipants(inputChannel).Recent()
			err = q.ForEach(ctx, func(ctx context.Context, p participants.Elem) error {
				if user, ok := p.User(); ok {
					if !seen[user.ID] {
						seen[user.ID] = true
						members = append(members, Member{
							ID:        user.ID,
							Username:  user.Username,
							FirstName: user.FirstName,
							IsBot:     user.Bot,
						})
					}
				}
				return nil
			})
			if err != nil {
				return fmt.Errorf("failed fetching participants: %w", err)
			}
		} else if basicChat != nil {
			// Basic chat
			fullChat, err := api.MessagesGetFullChat(ctx, basicChat.ID)
			if err != nil {
				return fmt.Errorf("failed fetching full chat: %w", err)
			}
			for _, u := range fullChat.Users {
				if user, ok := u.(*tg.User); ok {
					if !seen[user.ID] {
						seen[user.ID] = true
						members = append(members, Member{
							ID:        user.ID,
							Username:  user.Username,
							FirstName: user.FirstName,
							IsBot:     user.Bot,
						})
					}
				}
			}
		}

		data, err := json.MarshalIndent(members, "", "  ")
		if err != nil {
			return fmt.Errorf("failed encoding JSON: %w", err)
		}

		if err := os.WriteFile(outputFile, data, 0644); err != nil {
			return fmt.Errorf("failed writing to %s: %w", outputFile, err)
		}

		fmt.Printf("Exported %d members.\n", len(members))
		return nil
	})

	if err != nil && !errors.Is(err, context.Canceled) {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
