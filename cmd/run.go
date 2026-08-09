package cmd

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
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
			return fmt.Errorf("failed to load configuration: %w", err)
		}

		if cfg.TelegramToken == "" {
			return fmt.Errorf("telegram_token missing in config or GOGCBOT_TELEGRAM_TOKEN env var")
		}

		log.Printf("[Main] Opening pure Go SQLite database at %s...", cfg.DBPath)
		database, err := db.OpenDB(cfg.DBPath)
		if err != nil {
			return fmt.Errorf("failed to open database: %w", err)
		}
		defer database.Close()

		log.Println("[Main] Initializing Telegram Bot engine...")
		b, err := bot.NewBot(cfg, database)
		if err != nil {
			return fmt.Errorf("failed to initialize bot: %w", err)
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
