package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/angch/gogcbot/pkg/bot"
	"github.com/angch/gogcbot/pkg/config"
	"github.com/angch/gogcbot/pkg/db"
	"github.com/spf13/cobra"
)

var (
	backfillForce   bool
	backfillDelayMs int
)

var backfillProfilesCmd = &cobra.Command{
	Use:   "backfill-profiles",
	Short: "Backfill user profiles (bio and profile pictures) from Telegram API",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.LoadConfig(cfgFile)
		if err != nil {
			return fmt.Errorf("failed to load config from '%s': %w", cfgFile, err)
		}

		if cfg.TelegramToken == "" {
			return fmt.Errorf("telegram_token is required to fetch user profiles from Telegram API")
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

		b, err := bot.NewBot(cfg, database)
		if err != nil {
			return fmt.Errorf("failed to initialize Telegram bot engine: %w", err)
		}

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
		go func() {
			<-sigChan
			fmt.Println("\nReceived interrupt signal, stopping backfill gracefully...")
			cancel()
		}()

		delay := time.Duration(backfillDelayMs) * time.Millisecond
		if delay <= 0 {
			delay = 100 * time.Millisecond
		}

		fmt.Println("🚀 Starting Telegram user profile backfill...")
		fmt.Printf("Database: %s | Delay: %v | Force All: %t\n", dbPath, delay, backfillForce)

		success, failed, err := b.BackfillUserProfiles(ctx, delay, backfillForce, func(curr, total int, u *db.User, p *db.UserProfile, err error) {
			if err != nil {
				if p != nil && p.NotFound {
					fmt.Printf("[%d/%d] ⚠️ User %d (@%s): profile not found on Telegram (marked as not found)\n", curr, total, u.UserID, u.Username)
				} else {
					fmt.Printf("[%d/%d] ❌ User %d (@%s): %v\n", curr, total, u.UserID, u.Username, err)
				}
			} else {
				bioSnippet := p.Bio
				if len(bioSnippet) > 30 {
					bioSnippet = bioSnippet[:27] + "..."
				}
				if bioSnippet == "" {
					bioSnippet = "(no bio)"
				}
				fmt.Printf("[%d/%d] ✅ User %d (@%s) | Photos: %d | Bio: %s\n", curr, total, u.UserID, u.Username, p.PhotoCount, bioSnippet)
			}
		})

		fmt.Println("\n📊 === Profile Backfill Summary ===")
		fmt.Printf("Successfully Fetched : %d\n", success)
		fmt.Printf("Failed / Not Found   : %d\n", failed)
		if err != nil && err != context.Canceled {
			return err
		}
		return nil
	},
}

func init() {
	backfillProfilesCmd.Flags().BoolVar(&backfillForce, "force", false, "Force re-fetching profiles for all users even if already saved")
	backfillProfilesCmd.Flags().IntVar(&backfillDelayMs, "delay-ms", 100, "Delay in milliseconds between Telegram API requests to prevent rate limiting")
	rootCmd.AddCommand(backfillProfilesCmd)
}
