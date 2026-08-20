package cmd

import (
	"fmt"
	"path/filepath"

	"github.com/angch/gogcbot/pkg/config"
	"github.com/angch/gogcbot/pkg/db"
	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Inspect database stats and configuration status",
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
			return fmt.Errorf("failed to open database at '%s': %w\n-> Action: Verify that '%s' exists and you have read/write permissions.", dbPath, err, dbPath)
		}
		defer database.Close()

		stats, err := database.GetStats()
		if err != nil {
			return fmt.Errorf("failed to query database stats: %w\n-> Action: Ensure the database schema is initialized by running 'gogcbot run'.", err)
		}

		fmt.Println("📊 === GoGCBot Status Summary ===")
		fmt.Printf("Config File Path    : %s\n", cfgFile)
		fmt.Printf("Database Path       : %s\n", cfg.DBPath)
		fmt.Printf("Super Admin ID      : %d\n", cfg.SuperAdminID)
		fmt.Printf("Moderation Group ID : %d\n", cfg.ModerationGroupID)
		fmt.Printf("Auto-Flag Enabled   : %t\n", cfg.AutoFlag.Enabled)
		fmt.Println("---------------------------------")
		fmtPrintfStats(stats)
		return nil
	},
}

func fmtPrintfStats(stats *db.Stats) {
	fmt.Printf("Total Monitored Groups : %d\n", stats.TotalGroups)
	fmt.Printf("Total Tracked Users    : %d\n", stats.TotalUsers)
	fmt.Printf("Total Logged Messages  : %d\n", stats.TotalMessages)
	fmt.Printf("Pending Mod Flags      : %d\n", stats.PendingFlags)
	fmt.Printf("Resolved Mod Flags     : %d\n", stats.ResolvedFlags)
	fmt.Printf("Cached User Profiles   : %d\n", stats.TotalUserProfiles)
}

func init() {
	rootCmd.AddCommand(statusCmd)
}
