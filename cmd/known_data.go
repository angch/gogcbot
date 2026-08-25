package cmd

import (
	"fmt"
	"path/filepath"

	"github.com/angch/gogcbot/pkg/config"
	"github.com/angch/gogcbot/pkg/db"
	"github.com/spf13/cobra"
)

var (
	knownDataSync bool
	knownDataList bool
)

var knownDataCmd = &cobra.Command{
	Use:     "known-data",
	Aliases: []string{"knowndata", "test-data"},
	Short:   "Manage known good and known bad datasets for rule regression testing",
	Long: `Inspect and synchronize the known good (rep >= 50) and known bad (banned users)
datasets stored in SQLite for rule validation and regression testing.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.LoadConfig(cfgFile)
		if err != nil {
			return fmt.Errorf("failed to load config from '%s': %w", cfgFile, err)
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

		out := cmd.OutOrStdout()
		syncFlag, _ := cmd.Flags().GetBool("sync")
		listFlag, _ := cmd.Flags().GetBool("list")

		if syncFlag {
			goodCnt, err := database.PopulateKnownGoodFromUsers(50)
			if err != nil {
				return fmt.Errorf("failed to populate known good data: %w", err)
			}
			badCnt, err := database.PopulateKnownBadFromUsers()
			if err != nil {
				return fmt.Errorf("failed to populate known bad data: %w", err)
			}
			fmt.Fprintf(out, "✅ Synchronized Known Datasets:\n  • Known Good (rep >= 50): %d entries\n  • Known Bad (banned users): %d entries\n", goodCnt, badCnt)
			return nil
		}

		goodUsers, err := database.GetKnownGoodUsers()
		if err != nil {
			return fmt.Errorf("failed to query known good users: %w", err)
		}
		badUsers, err := database.GetKnownBadUsers()
		if err != nil {
			return fmt.Errorf("failed to query known bad users: %w", err)
		}

		fmt.Fprintln(out, "📊 === Known Datasets Summary ===")
		fmt.Fprintf(out, "Known Good Users (rep >= 50) : %d\n", len(goodUsers))
		fmt.Fprintf(out, "Known Bad Users (banned)     : %d\n\n", len(badUsers))

		if listFlag {
			fmt.Fprintln(out, "🟢 Known Good Profiles:")
			for i, u := range goodUsers {
				username := "@" + u.Username
				if u.Username == "" {
					username = "(no handle)"
				}
				fmt.Fprintf(out, "  %d. %d | %s | %s | Rep: %d\n", i+1, u.UserID, username, u.DisplayName(), u.Reputation)
			}

			fmt.Fprintln(out, "\n🔴 Known Bad Profiles (Sample):")
			maxBad := 20
			for i, u := range badUsers {
				if i >= maxBad {
					fmt.Fprintf(out, "  ... and %d more known bad entries\n", len(badUsers)-maxBad)
					break
				}
				username := "@" + u.Username
				if u.Username == "" {
					username = "(no handle)"
				}
				fmt.Fprintf(out, "  %d. %d | %s | %s | Reason: %s\n", i+1, u.UserID, username, u.DisplayName(), u.BanReason)
			}
		} else {
			fmt.Fprintln(out, "💡 Use --list to inspect entries or --sync to update from users table.")
		}

		return nil
	},
}

func init() {
	knownDataCmd.Flags().BoolVar(&knownDataSync, "sync", false, "Synchronize known good and known bad datasets from users and profiles tables")
	knownDataCmd.Flags().BoolVar(&knownDataList, "list", false, "List all known good and sample known bad profiles")
	rootCmd.AddCommand(knownDataCmd)
}
