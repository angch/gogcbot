package cmd

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/angch/gogcbot/pkg/bot"
	"github.com/angch/gogcbot/pkg/config"
	"github.com/angch/gogcbot/pkg/db"
	svcpkg "github.com/angch/gogcbot/pkg/service"
	"github.com/kardianos/service"
	"github.com/spf13/cobra"
)

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Start the Telegram moderation bot service",
	RunE: func(cmd *cobra.Command, args []string) error {
		svc, err := svcpkg.GetService(cfgFile)
		if err != nil {
			return err
		}

		if !service.Interactive() {
			log.Println("[Main] Running as OS background system service...")
			return svc.Run()
		}

		log.Println("[Main] Loading configuration...")
		cfg, err := config.LoadConfig(cfgFile)
		if err != nil {
			return fmt.Errorf("failed to load configuration from '%s': %w\n-> Action: Ensure the file exists and contains valid YAML, or run 'gogcbot init-config' to generate a default config file.", cfgFile, err)
		}

		if cfg.TelegramToken == "" {
			return fmt.Errorf("telegram_token is missing in configuration file '%s' (or GOGCBOT_TELEGRAM_TOKEN env variable).\n-> Action: Open '%s' and add your Telegram bot token from @BotFather, or set export GOGCBOT_TELEGRAM_TOKEN=\"<your_token>\".", cfgFile, cfgFile)
		}

		log.Printf("[Main] Opening pure Go SQLite database at %s...", cfg.DBPath)
		dbPath := cfg.DBPath
		if !filepath.IsAbs(dbPath) {
			dbPath = filepath.Join(filepath.Dir(cfgFile), dbPath)
		}
		database, err := db.OpenDB(dbPath)
		if err != nil {
			return fmt.Errorf("failed to open SQLite database at '%s': %w\n-> Action: Check file permissions and ensure the parent directory is writable.", dbPath, err)
		}
		defer database.Close()

		log.Println("[Main] Performing database auto-migrations...")
		if err := database.AutoMigrate(); err != nil {
			return fmt.Errorf("failed auto-migrating database schema: %w", err)
		}

		log.Println("[Main] Initializing Telegram Bot engine...")
		b, err := bot.NewBot(cfg, database)
		if err != nil {
			return fmt.Errorf("failed to initialize Telegram bot engine: %w\n-> Action: Verify your Telegram Bot Token with @BotFather and ensure internet connectivity.", err)
		}
		b.SetCfgPath(cfgFile)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

		go func() {
			sig := <-sigChan
			log.Printf("[Main] Received signal %v, shutting down gracefully...", sig)
			cancel()
		}()

		log.Println("[Main] Starting GoGCBot daemon...")
		return b.Start(ctx)
	},
}

func init() {
	rootCmd.AddCommand(runCmd)
}
