package cmd

import (
	"fmt"

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
			return fmt.Errorf("failed to load configuration: %w", err)
		}

		database, err := db.OpenDB(cfg.DBPath)
		if err != nil {
			return fmt.Errorf("failed to open database at %s: %w", cfg.DBPath, err)
		}
		defer database.Close()

		stats, err := database.GetStats()
		if err != nil {
			return fmt.Errorf("failed to query database stats: %w", err)
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
}

func init() {
	rootCmd.AddCommand(statusCmd)
}
