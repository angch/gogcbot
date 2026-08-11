package cmd

import (
	"fmt"
	"path/filepath"

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
			return fmt.Errorf("failed to load configuration from '%s': %w\n-> Action: Ensure '%s' exists, or run 'gogcbot init-config' to create a default configuration.", cfgFile, err, cfgFile)
		}

		dbPath := cfg.DBPath
		if !filepath.IsAbs(dbPath) {
			dbPath = filepath.Join(filepath.Dir(cfgFile), dbPath)
		}

		database, err := db.OpenDB(dbPath)
		if err != nil {
			return fmt.Errorf("failed to open database at '%s': %w\n-> Action: Verify that '%s' exists and you have write permissions.", dbPath, err, dbPath)
		}
		defer database.Close()

		rc := cleaner.NewRetentionCleaner(database, cfg.CleanupIntervalHr)
		oldP, userP, err := rc.RunOnce()
		if err != nil {
			return fmt.Errorf("retention cleanup execution failed: %w\n-> Action: Check if database is locked or corrupted.", err)
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
