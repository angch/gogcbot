// Package bot provides the main Telegram bot engine, update processing, and moderation command handlers.
package bot

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/angch/gogcbot/pkg/cleaner"
	"github.com/angch/gogcbot/pkg/config"
	"github.com/angch/gogcbot/pkg/db"
	"github.com/angch/gogcbot/pkg/detector"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// Bot wraps the Telegram Bot API client, database connection, retention cleaner, and configuration state.
type Bot struct {
	cfg      *config.Config
	cfgPath  string
	db       *db.DB
	api      *tgbotapi.BotAPI
	cleaner  *cleaner.RetentionCleaner
	detector *detector.Detector
	botUser  tgbotapi.User
	mu       sync.RWMutex
	stopChan chan struct{}
}

var (
	newBotAPIFunc   = tgbotapi.NewBotAPI
	loginRetryDelay = 5 * time.Second
	maxLoginRetries = 6
)

// NewBot initializes a new Bot instance using the provided configuration and database client.
func NewBot(cfg *config.Config, database *db.DB) (*Bot, error) {
	if cfg.TelegramToken == "" {
		return nil, fmt.Errorf("telegram_token is required\n-> Action: Set 'telegram_token' in configuration file or export GOGCBOT_TELEGRAM_TOKEN=\"<token>\"")
	}

	var api *tgbotapi.BotAPI
	var err error

	for attempt := 1; attempt <= maxLoginRetries; attempt++ {
		api, err = newBotAPIFunc(cfg.TelegramToken)
		if err == nil {
			break
		}

		log.Printf("[Bot] Telegram login denied/failed (attempt %d/%d): %v. Sleeping %v before retrying (may have started up too soon)...",
			attempt, maxLoginRetries, err, loginRetryDelay)

		if attempt < maxLoginRetries {
			time.Sleep(loginRetryDelay)
		}
	}

	if err != nil {
		return nil, fmt.Errorf("failed to authenticate Telegram Bot API after %d attempts: %w\n-> Action: Check your Telegram Bot Token with @BotFather and ensure active internet connectivity", maxLoginRetries, err)
	}

	// Enable debug mode for tgbotapi to log raw HTTP Telegram API calls & responses
	if cfg.LogLevel == "debug" || cfg.LogLevel == "" {
		api.Debug = true
	}

	log.Printf("[Bot] Authorized on account %s (ID: %d)", api.Self.UserName, api.Self.ID)

	// If super admin ID is not set in config, attempt fallback to bot user ID or log warning
	if cfg.SuperAdminID == 0 {
		log.Printf("[Bot] WARNING: super_admin_id is not specified in config. You can set it using /setsuperadmin or updating config.yaml")
	}

	rc := cleaner.NewRetentionCleaner(database, cfg.CleanupIntervalHr)

	det := detector.NewDetector()
	if cfg.Detector.Enabled {
		if cfg.Detector.NewUserCJK.Enabled || cfg.Detector.NewUserChinese.Enabled {
			cjkCfg := cfg.Detector.NewUserCJK
			if !cjkCfg.Enabled && cfg.Detector.NewUserChinese.Enabled {
				cjkCfg = cfg.Detector.NewUserChinese
			}
			det.RegisterTrigger(detector.NewNewUserCJKTrigger(cjkCfg))
		}
		if cfg.Detector.NewUserSpamBio.Enabled {
			spamBioCfg := cfg.Detector.NewUserSpamBio
			var kws []string
			kws = append(kws, cfg.AutoFlag.BlockedKeywords...)
			if database != nil {
				dbKws, _ := database.GetSpamSnippetStrings()
				kws = append(kws, dbKws...)
			}
			det.RegisterTrigger(detector.NewNewUserSpamBioTriggerWithKeywords(spamBioCfg, kws...))
		}
		if cfg.Detector.RedPacketName.Enabled || cfg.Detector.NewUserRedPacket.Enabled {
			rpCfg := cfg.Detector.RedPacketName
			if !rpCfg.Enabled && cfg.Detector.NewUserRedPacket.Enabled {
				rpCfg = cfg.Detector.NewUserRedPacket
			}
			det.RegisterTrigger(detector.NewRedPacketNameTrigger(rpCfg))
		}
		if cfg.Detector.ProfileNameKeywordBan.Enabled {
			det.RegisterTrigger(detector.NewProfileNameKeywordBanTrigger(cfg.Detector.ProfileNameKeywordBan))
		}
	}

	if database != nil && len(cfg.AutoFlag.BlockedKeywords) > 0 {
		_ = database.SyncSpamSnippets(cfg.AutoFlag.BlockedKeywords)
	}

	b := &Bot{
		cfg:      cfg,
		cfgPath:  "config.yaml",
		db:       database,
		api:      api,
		cleaner:  rc,
		detector: det,
		botUser:  api.Self,
		stopChan: make(chan struct{}),
	}

	return b, nil
}

func sanitizeChattable(c tgbotapi.Chattable) tgbotapi.Chattable {
	switch v := c.(type) {
	case tgbotapi.MessageConfig:
		v.Text = strings.ToValidUTF8(v.Text, "")
		return v
	case *tgbotapi.MessageConfig:
		v.Text = strings.ToValidUTF8(v.Text, "")
		return v
	case tgbotapi.EditMessageTextConfig:
		v.Text = strings.ToValidUTF8(v.Text, "")
		return v
	case *tgbotapi.EditMessageTextConfig:
		v.Text = strings.ToValidUTF8(v.Text, "")
		return v
	case tgbotapi.EditMessageCaptionConfig:
		v.Caption = strings.ToValidUTF8(v.Caption, "")
		return v
	case *tgbotapi.EditMessageCaptionConfig:
		v.Caption = strings.ToValidUTF8(v.Caption, "")
		return v
	case tgbotapi.PhotoConfig:
		v.Caption = strings.ToValidUTF8(v.Caption, "")
		return v
	case *tgbotapi.PhotoConfig:
		v.Caption = strings.ToValidUTF8(v.Caption, "")
		return v
	case tgbotapi.DocumentConfig:
		v.Caption = strings.ToValidUTF8(v.Caption, "")
		return v
	case *tgbotapi.DocumentConfig:
		v.Caption = strings.ToValidUTF8(v.Caption, "")
		return v
	default:
		return c
	}
}

// Send wraps b.api.Send to echo all outgoing Telegram API message calls to standard logs for debugging.
func (b *Bot) Send(c tgbotapi.Chattable) (tgbotapi.Message, error) {
	c = sanitizeChattable(c)
	log.Printf("[Telegram API Call] Send -> Payload: %#v", c)
	if b.api == nil {
		return tgbotapi.Message{}, nil
	}
	msg, err := b.api.Send(c)
	if err != nil {
		log.Printf("[Telegram API Error] Send failed: %v", err)
	} else {
		log.Printf("[Telegram API Response] Send success -> MessageID: %d in ChatID: %d", msg.MessageID, msg.Chat.ID)
	}
	return msg, err
}

// Request wraps b.api.Request to echo all outgoing Telegram API request calls to standard logs for debugging.
func (b *Bot) Request(c tgbotapi.Chattable) (*tgbotapi.APIResponse, error) {
	c = sanitizeChattable(c)
	log.Printf("[Telegram API Call] Request -> Payload: %#v", c)
	if b.api == nil {
		return &tgbotapi.APIResponse{Ok: true}, nil
	}
	resp, err := b.api.Request(c)
	if err != nil {
		log.Printf("[Telegram API Error] Request failed: %v", err)
	} else if resp != nil {
		log.Printf("[Telegram API Response] Request success -> Ok: %t, Description: %s", resp.Ok, resp.Description)
	}
	return resp, err
}

// GetChatMember wraps b.api.GetChatMember to echo chat member query calls to standard logs for debugging.
func (b *Bot) GetChatMember(config tgbotapi.GetChatMemberConfig) (tgbotapi.ChatMember, error) {
	log.Printf("[Telegram API Call] GetChatMember -> ChatID: %d | UserID: %d", config.ChatID, config.UserID)
	if b.api == nil {
		return tgbotapi.ChatMember{Status: "member"}, nil
	}
	cm, err := b.api.GetChatMember(config)
	if err != nil {
		log.Printf("[Telegram API Error] GetChatMember failed: %v", err)
	} else {
		log.Printf("[Telegram API Response] GetChatMember success -> UserID: %d, Status: %s", cm.User.ID, cm.Status)
	}
	return cm, err
}

// GetChat wraps b.api.GetChat to echo chat / user query calls to standard logs for debugging.
func (b *Bot) GetChat(config tgbotapi.ChatInfoConfig) (tgbotapi.Chat, error) {
	log.Printf("[Telegram API Call] GetChat -> ChatID: %d", config.ChatID)
	if b.api == nil {
		if config.ChatID < 0 || config.ChatID == 404 || config.ChatID == 999999 {
			return tgbotapi.Chat{}, fmt.Errorf("Bad Request: chat not found")
		}
		mockBio := "Mock bio description"
		mockUsername := "mockuser"
		mockFirstName := "Mock"
		mockLastName := "User"
		if b.db != nil {
			if existing, err := b.db.GetUserProfile(config.ChatID); err == nil && existing != nil {
				if existing.Bio != "" {
					mockBio = existing.Bio
				}
				if existing.Username != "" {
					mockUsername = existing.Username
				}
				if existing.FirstName != "" {
					mockFirstName = existing.FirstName
				}
				if existing.LastName != "" {
					mockLastName = existing.LastName
				}
			}
		}
		return tgbotapi.Chat{
			ID:        config.ChatID,
			Type:      "private",
			FirstName: mockFirstName,
			LastName:  mockLastName,
			UserName:  mockUsername,
			Bio:       mockBio,
		}, nil
	}
	chat, err := b.api.GetChat(config)
	if err != nil {
		log.Printf("[Telegram API Error] GetChat failed for %d: %v", config.ChatID, err)
	} else {
		log.Printf("[Telegram API Response] GetChat success -> ID: %d, UserName: @%s, Bio: %q", chat.ID, chat.UserName, chat.Bio)
	}
	return chat, err
}

// TelegramChatFullInfo represents the Telegram Bot API 7.0+ ChatFullInfo response object.
type TelegramChatFullInfo struct {
	ID                 int64  `json:"id"`
	Type               string `json:"type"`
	Title              string `json:"title,omitempty"`
	Username           string `json:"username,omitempty"`
	FirstName          string `json:"first_name,omitempty"`
	LastName           string `json:"last_name,omitempty"`
	Bio                string `json:"bio,omitempty"`
	Description        string `json:"description,omitempty"`
	HasPrivateForwards bool   `json:"has_private_forwards,omitempty"`
	PersonalChat       *struct {
		ID       int64  `json:"id"`
		Title    string `json:"title"`
		Username string `json:"username"`
		Type     string `json:"type"`
	} `json:"personal_chat,omitempty"`
	BusinessIntro *struct {
		Title   string `json:"title"`
		Message string `json:"message"`
	} `json:"business_intro,omitempty"`
	Photo *struct {
		SmallFileID       string `json:"small_file_id"`
		SmallFileUniqueID string `json:"small_file_unique_id"`
		BigFileID         string `json:"big_file_id"`
		BigFileUniqueID   string `json:"big_file_unique_id"`
	} `json:"photo,omitempty"`
}

// GetChatFullInfo queries Telegram's getChat API for a user's private chat and returns the full ChatFullInfo including personal_chat.
func (b *Bot) GetChatFullInfo(userID int64) (*TelegramChatFullInfo, string, error) {
	log.Printf("[Telegram API Call] GetChatFullInfo (getChat) -> UserID: %d", userID)
	if b.api == nil {
		// Mock handling for unit testing
		chat, err := b.GetChat(tgbotapi.ChatInfoConfig{ChatConfig: tgbotapi.ChatConfig{ChatID: userID}})
		if err != nil {
			return nil, "", err
		}
		info := &TelegramChatFullInfo{
			ID:                 chat.ID,
			Type:               chat.Type,
			Username:           chat.UserName,
			FirstName:          chat.FirstName,
			LastName:           chat.LastName,
			Bio:                chat.Bio,
			Description:        chat.Description,
			HasPrivateForwards: chat.HasPrivateForwards,
		}
		return info, "", nil
	}

	resp, err := b.api.Request(tgbotapi.ChatInfoConfig{
		ChatConfig: tgbotapi.ChatConfig{
			ChatID: userID,
		},
	})
	if err != nil {
		log.Printf("[Telegram API Error] getChat failed for %d: %v", userID, err)
		return nil, "", err
	}
	if !resp.Ok {
		log.Printf("[Telegram API Error] getChat not ok for %d: %s (code %d)", userID, resp.Description, resp.ErrorCode)
		return nil, "", fmt.Errorf("telegram API error %d: %s", resp.ErrorCode, resp.Description)
	}

	rawJSON := string(resp.Result)
	var fullInfo TelegramChatFullInfo
	if err := json.Unmarshal(resp.Result, &fullInfo); err != nil {
		log.Printf("[Telegram API Error] Failed to unmarshal ChatFullInfo for %d: %v", userID, err)
		return nil, rawJSON, err
	}

	log.Printf("[Telegram API Response] getChat success -> ID: %d, UserName: @%s, Bio: %q, PersonalChat: %+v",
		fullInfo.ID, fullInfo.Username, fullInfo.Bio, fullInfo.PersonalChat)

	return &fullInfo, rawJSON, nil
}

// GetUserProfilePhotos wraps b.api.GetUserProfilePhotos to echo query calls to standard logs for debugging.
func (b *Bot) GetUserProfilePhotos(config tgbotapi.UserProfilePhotosConfig) (tgbotapi.UserProfilePhotos, error) {
	log.Printf("[Telegram API Call] GetUserProfilePhotos -> UserID: %d, Offset: %d, Limit: %d", config.UserID, config.Offset, config.Limit)
	if b.api == nil {
		if config.UserID < 0 || config.UserID == 404 || config.UserID == 999999 {
			return tgbotapi.UserProfilePhotos{}, fmt.Errorf("Bad Request: user not found")
		}
		return tgbotapi.UserProfilePhotos{
			TotalCount: 1,
			Photos: [][]tgbotapi.PhotoSize{
				{
					{FileID: "mock_small_file_id", FileUniqueID: "mock_small_unique_id", Width: 160, Height: 160, FileSize: 1024},
					{FileID: "mock_big_file_id", FileUniqueID: "mock_big_unique_id", Width: 640, Height: 640, FileSize: 4096},
				},
			},
		}, nil
	}
	photos, err := b.api.GetUserProfilePhotos(config)
	if err != nil {
		log.Printf("[Telegram API Error] GetUserProfilePhotos failed for %d: %v", config.UserID, err)
	} else {
		log.Printf("[Telegram API Response] GetUserProfilePhotos success -> UserID: %d, TotalCount: %d", config.UserID, photos.TotalCount)
	}
	return photos, err
}

// FetchUserProfile retrieves the latest profile (bio, personal_chat channel, profile picture) for a user from Telegram API and saves it to user_profiles table.
// If the profile cannot be found on Telegram, it is saved with NotFound = true so subsequent backfills skip it.
func (b *Bot) FetchUserProfile(userID int64) (*db.UserProfile, error) {
	if userID == 0 {
		return nil, fmt.Errorf("invalid user ID 0")
	}

	profile := &db.UserProfile{
		UserID:    userID,
		FetchedAt: time.Now(),
	}

	// 1. Fetch Chat Info (using getChat on the user's private chat ID to retrieve ChatFullInfo)
	fullInfo, rawJSON, errChat := b.GetChatFullInfo(userID)
	if errChat == nil && fullInfo != nil {
		profile.Username = fullInfo.Username
		profile.FirstName = fullInfo.FirstName
		profile.LastName = fullInfo.LastName
		profile.Bio = fullInfo.Bio
		profile.HasPrivateForwards = fullInfo.HasPrivateForwards
		profile.RawJSON = rawJSON

		if fullInfo.Description != "" && profile.Bio == "" {
			profile.Bio = fullInfo.Description
		}
		if fullInfo.PersonalChat != nil {
			profile.PersonalChatTitle = fullInfo.PersonalChat.Title
			profile.PersonalChatUsername = fullInfo.PersonalChat.Username
		}
		if fullInfo.BusinessIntro != nil {
			intro := fullInfo.BusinessIntro.Title
			if fullInfo.BusinessIntro.Message != "" {
				if intro != "" {
					intro += " - " + fullInfo.BusinessIntro.Message
				} else {
					intro = fullInfo.BusinessIntro.Message
				}
			}
			profile.BusinessIntro = intro
		}
		if fullInfo.Photo != nil {
			profile.HasPhoto = true
			profile.PhotoSmallFileID = fullInfo.Photo.SmallFileID
			profile.PhotoSmallFileUniqueID = fullInfo.Photo.SmallFileUniqueID
			profile.PhotoFileID = fullInfo.Photo.BigFileID
			profile.PhotoFileUniqueID = fullInfo.Photo.BigFileUniqueID
		}
	} else {
		log.Printf("[Bot] Warning: GetChatFullInfo failed for user %d: %v", userID, errChat)
	}

	// 2. Fetch User Profile Photos (for full photo count & highest resolution file_id)
	photos, errPhotos := b.GetUserProfilePhotos(tgbotapi.UserProfilePhotosConfig{
		UserID: userID,
		Offset: 0,
		Limit:  1,
	})
	if errPhotos == nil {
		profile.PhotoCount = photos.TotalCount
		if photos.TotalCount > 0 && len(photos.Photos) > 0 && len(photos.Photos[0]) > 0 {
			profile.HasPhoto = true
			sizes := photos.Photos[0]
			if profile.PhotoSmallFileID == "" {
				profile.PhotoSmallFileID = sizes[0].FileID
				profile.PhotoSmallFileUniqueID = sizes[0].FileUniqueID
			}
			largest := sizes[len(sizes)-1]
			if profile.PhotoFileID == "" || profile.PhotoFileID == profile.PhotoSmallFileID {
				profile.PhotoFileID = largest.FileID
				profile.PhotoFileUniqueID = largest.FileUniqueID
			}
		}
	} else {
		log.Printf("[Bot] Warning: GetUserProfilePhotos failed for user %d: %v", userID, errPhotos)
	}

	// Fallback names/username/language from DB user record if Telegram GetChat didn't return them
	if dbUser, err := b.db.GetUserByID(userID); err == nil && dbUser != nil {
		if profile.Username == "" {
			profile.Username = dbUser.Username
		}
		if profile.FirstName == "" {
			profile.FirstName = dbUser.FirstName
		}
		if profile.LastName == "" {
			profile.LastName = dbUser.LastName
		}
		if profile.LanguageCode == "" {
			profile.LanguageCode = dbUser.LanguageCode
		}
		profile.IsPremium = dbUser.IsPremium
	}

	// Fallback to existing cached profile in DB for fields not present in Telegram response
	if existing, err := b.db.GetUserProfile(userID); err == nil && existing != nil {
		if profile.Bio == "" {
			profile.Bio = existing.Bio
		}
		if profile.PersonalChatTitle == "" {
			profile.PersonalChatTitle = existing.PersonalChatTitle
		}
		if profile.PersonalChatUsername == "" {
			profile.PersonalChatUsername = existing.PersonalChatUsername
		}
		if profile.BusinessIntro == "" {
			profile.BusinessIntro = existing.BusinessIntro
		}
		if profile.LanguageCode == "" {
			profile.LanguageCode = existing.LanguageCode
		}
		if !profile.IsPremium {
			profile.IsPremium = existing.IsPremium
		}
		if !profile.HasPrivateForwards {
			profile.HasPrivateForwards = existing.HasPrivateForwards
		}
	}

	if errChat != nil && errPhotos != nil {
		// If we already have a cached profile in DB with bio/photo, return it rather than overwriting with empty
		if existing, err := b.db.GetUserProfile(userID); err == nil && existing != nil && (existing.Bio != "" || existing.PersonalChatTitle != "") {
			return existing, fmt.Errorf("user profile not found on Telegram: returning cached profile (chat err: %v, photos err: %v)", errChat, errPhotos)
		}
		profile.NotFound = true
		if saveErr := b.db.SaveUserProfile(profile); saveErr != nil {
			log.Printf("[Bot] Warning: failed to save not_found user profile for %d: %v", userID, saveErr)
		}
		return profile, fmt.Errorf("user profile not found on Telegram (chat err: %v, photos err: %v)", errChat, errPhotos)
	}

	profile.NotFound = false
	if err := b.db.SaveUserProfile(profile); err != nil {
		return profile, fmt.Errorf("failed to save user profile to database: %w", err)
	}

	return profile, nil
}

// BackfillUserProfiles iterates over tracked users and fetches their latest profile from Telegram API with rate limiting.
func (b *Bot) BackfillUserProfiles(ctx context.Context, delay time.Duration, force bool, progressCb func(current, total int, user *db.User, profile *db.UserProfile, err error)) (int, int, error) {
	var users []db.User
	var err error

	if force {
		users, err = b.db.GetAllUsers(0)
	} else {
		users, err = b.db.GetUsersWithoutProfile(0)
	}
	if err != nil {
		return 0, 0, fmt.Errorf("failed to query users for profile backfill: %w", err)
	}

	if delay <= 0 {
		delay = 100 * time.Millisecond
	}

	total := len(users)
	successCount := 0
	errorCount := 0

	for i, u := range users {
		select {
		case <-ctx.Done():
			return successCount, errorCount, ctx.Err()
		default:
		}

		profile, err := b.FetchUserProfile(u.UserID)
		if err != nil {
			errorCount++
			if profile != nil && profile.NotFound {
				log.Printf("[Profile Backfill] (%d/%d) User %d (@%s) not found on Telegram (marked as not found)", i+1, total, u.UserID, u.Username)
			} else {
				log.Printf("[Profile Backfill] (%d/%d) Error fetching user %d (@%s): %v", i+1, total, u.UserID, u.Username, err)
			}
		} else {
			successCount++
			log.Printf("[Profile Backfill] (%d/%d) Fetched user %d (@%s) -> Bio: %q, Photos: %d", i+1, total, u.UserID, u.Username, profile.Bio, profile.PhotoCount)
		}

		if progressCb != nil {
			userCopy := u
			progressCb(i+1, total, &userCopy, profile, err)
		}

		if i < total-1 {
			select {
			case <-ctx.Done():
				return successCount, errorCount, ctx.Err()
			case <-time.After(delay):
			}
		}
	}

	return successCount, errorCount, nil
}

func (b *Bot) SetCfgPath(path string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if path != "" {
		b.cfgPath = path
	}
}

func (b *Bot) SaveConfig() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	path := b.cfgPath
	if path == "" {
		path = "config.yaml"
	}
	return config.SaveConfig(path, b.cfg)
}

func (b *Bot) Start(ctx context.Context) error {
	// Start background retention cleaner
	go b.cleaner.Start(ctx)

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	updates := b.api.GetUpdatesChan(u)

	log.Println("[Bot] Started listening for Telegram updates...")

	for {
		select {
		case <-ctx.Done():
			log.Println("[Bot] Stopping update handler (context done)...")
			b.api.StopReceivingUpdates()
			return nil
		case update, ok := <-updates:
			if !ok {
				log.Println("[Bot] Updates channel closed.")
				return nil
			}
			b.handleUpdate(update)
		}
	}
}

func (b *Bot) API() *tgbotapi.BotAPI {
	return b.api
}

func (b *Bot) Config() *config.Config {
	return b.cfg
}

func (b *Bot) DB() *db.DB {
	return b.db
}

func (b *Bot) BotUser() tgbotapi.User {
	return b.botUser
}

func (b *Bot) Detector() *detector.Detector {
	return b.detector
}
