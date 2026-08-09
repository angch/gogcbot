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
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// Bot wraps the Telegram Bot API client, database connection, retention cleaner, and configuration state.
type Bot struct {
	cfg       *config.Config
	cfgPath   string
	db        *db.DB
	api       *tgbotapi.BotAPI
	cleaner   *cleaner.RetentionCleaner
	botUser   tgbotapi.User
	mu        sync.RWMutex
	stopChan  chan struct{}
}

// NewBot initializes a new Bot instance using the provided configuration and database client.
func NewBot(cfg *config.Config, database *db.DB) (*Bot, error) {
	if cfg.TelegramToken == "" {
		return nil, fmt.Errorf("telegram token is required in config or GOGCBOT_TELEGRAM_TOKEN env var")
	}

	api, err := tgbotapi.NewBotAPI(cfg.TelegramToken)
	if err != nil {
		return nil, fmt.Errorf("failed to create telegram bot API: %w", err)
	}

	log.Printf("[Bot] Authorized on account %s (ID: %d)", api.Self.UserName, api.Self.ID)

	// If super admin ID is not set in config, attempt fallback to bot user ID or log warning
	if cfg.SuperAdminID == 0 {
		log.Printf("[Bot] WARNING: super_admin_id is not specified in config. You can set it using /setsuperadmin or updating config.yaml")
	}

	rc := cleaner.NewRetentionCleaner(database, cfg.CleanupIntervalHr)

	b := &Bot{
		cfg:      cfg,
		cfgPath:  "config.yaml",
		db:       database,
		api:      api,
		cleaner:  rc,
		botUser:  api.Self,
		stopChan: make(chan struct{}),
	}

	return b, nil
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
