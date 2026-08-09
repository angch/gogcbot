package cmd

import (
	"fmt"

	"github.com/angch/gogcbot/pkg/cleaner"
	"github.com/angch/gogcbot/pkg/config"
	"github.com/angch/gogcbot/pkg/db"
	"github.com/spf13/cobra"
)

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Execute a manual database retention cleanup (7-day logs & 50 posts per user)",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.LoadConfig(cfgFile)
		if err != nil {
			return fmt.Errorf("failed to load configuration: %w", err)
		}

		database, err := db.OpenDB(cfg.DBPath)
		if err != nil {
			return fmt.Errorf("failed to open database: %w", err)
		}
		defer database.Close()

		rc := cleaner.NewRetentionCleaner(database, cfg.CleanupIntervalHr)
		oldP, userP, err := rc.RunOnce()
		if err != nil {
			return fmt.Errorf("cleanup error: %w", err)
		}

		fmt.Println("🧹 === Database Retention Cleanup Complete ===")
		fmt.Printf("Purged Old Messages (>7 days)     : %d\n", oldP)
		fmt.Printf("Purged Excess User Posts (>50/user): %d\n", userP)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(cleanupCmd)
}
