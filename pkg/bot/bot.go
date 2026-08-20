// Package bot provides the main Telegram bot engine, update processing, and moderation command handlers.
package bot

import (
	"context"
	"fmt"
	"log"
	"sync"

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

// NewBot initializes a new Bot instance using the provided configuration and database client.
func NewBot(cfg *config.Config, database *db.DB) (*Bot, error) {
	if cfg.TelegramToken == "" {
		return nil, fmt.Errorf("telegram_token is required\n-> Action: Set 'telegram_token' in configuration file or export GOGCBOT_TELEGRAM_TOKEN=\"<token>\"")
	}

	api, err := tgbotapi.NewBotAPI(cfg.TelegramToken)
	if err != nil {
		return nil, fmt.Errorf("failed to authenticate Telegram Bot API: %w\n-> Action: Check your Telegram Bot Token with @BotFather and ensure active internet connectivity", err)
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
		if cfg.Detector.UsernameAnomaly.Enabled {
			det.RegisterTrigger(detector.NewUsernameAnomalyTrigger(cfg.Detector.UsernameAnomaly))
		}
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

// Send wraps b.api.Send to echo all outgoing Telegram API message calls to standard logs for debugging.
func (b *Bot) Send(c tgbotapi.Chattable) (tgbotapi.Message, error) {
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
