package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/angch/gogcbot/pkg/config"
	"github.com/angch/gogcbot/pkg/db"
	"github.com/spf13/cobra"
)

var (
	listUsersOutputFile     string
	listUsersGoodOnly       bool
	listUsersBadOnly        bool
	listUsersManualBansOnly bool
	listUsersLimit          int
)

var listUsersCmd = &cobra.Command{
	Use:     "list-users",
	Aliases: []string{"users", "listusers"},
	Short:   "List known good and bad users with metadata in Markdown format",
	Long: `List known good users (username, ID, reputation, message count, role, etc.)
followed by known bad users (banned, highlighting manual moderator bans vs automated trigger bans)
in Markdown format.`,
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
			return fmt.Errorf("failed to open database at '%s': %w\n-> Action: Verify that '%s' exists and you have read permissions.", dbPath, err, dbPath)
		}
		defer database.Close()

		goodUsers, badUsers, err := database.GetUserDirectoryReport(cfg.SuperAdminID)
		if err != nil {
			return fmt.Errorf("failed to generate user directory report: %w\n-> Action: Ensure the database schema is intact and accessible.", err)
		}

		opts := db.UserReportOptions{
			SuperAdminID:   cfg.SuperAdminID,
			GoodOnly:       listUsersGoodOnly,
			BadOnly:        listUsersBadOnly,
			ManualBansOnly: listUsersManualBansOnly,
			Limit:          listUsersLimit,
			DatabaseName:   cfg.DBPath,
		}

		markdownReport := db.GenerateUserDirectoryMarkdown(goodUsers, badUsers, opts)

		if listUsersOutputFile != "" {
			if err := os.WriteFile(listUsersOutputFile, []byte(markdownReport), 0644); err != nil {
				return fmt.Errorf("failed to write Markdown report to '%s': %w\n-> Action: Check file path and write permissions.", listUsersOutputFile, err)
			}
			fmt.Printf("✅ User directory Markdown report written to '%s'\n", listUsersOutputFile)
			return nil
		}

		fmt.Print(markdownReport)
		return nil
	},
}

func init() {
	listUsersCmd.Flags().StringVarP(&listUsersOutputFile, "output", "o", "", "Write Markdown output to a file instead of stdout")
	listUsersCmd.Flags().BoolVar(&listUsersGoodOnly, "good-only", false, "List only known good users")
	listUsersCmd.Flags().BoolVar(&listUsersBadOnly, "bad-only", false, "List only known bad users")
	listUsersCmd.Flags().BoolVar(&listUsersManualBansOnly, "manual-bans-only", false, "List only users manually banned by moderators")
	listUsersCmd.Flags().IntVarP(&listUsersLimit, "limit", "l", 0, "Limit number of users displayed per category (0 for unlimited)")
	rootCmd.AddCommand(listUsersCmd)
}
