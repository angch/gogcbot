package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/angch/gogcbot/pkg/config"
	"github.com/angch/gogcbot/pkg/db"
	"github.com/spf13/cobra"
)

var (
	listUnknownUsersOutputFile    string
	listUnknownUsersKeyword       string
	listUnknownUsersMaxPosts      int
	listUnknownUsersMaxReputation int
	listUnknownUsersLimit         int
	listUnknownUsersBan           bool
)

var listUnknownUsersCmd = &cobra.Command{
	Use:     "list-unknownusers",
	Aliases: []string{"unknownusers", "listunknownusers", "list-spambios", "listspambios", "spambios", "spam-bios", "spammers"},
	Short:   "List unbanned new/unknown users with few messages (with or without bios)",
	Long: `Scan user accounts for unbanned new users with few messages (with or without bios).
By default, matches all unbanned users with reputation <= 20 and at most 5 logged posts.
Use --keyword / -k to filter by specific keywords or phrases in profile or names.
Use --max-posts / -m to adjust the message count threshold (default 5).
Use --max-rep / -r to adjust the reputation threshold (default 20, -1 for any reputation).
Use --ban / -b to automatically ban all users matching the spam filter in the database.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.LoadConfig(cfgFile)
		if err != nil {
			return fmt.Errorf("failed to load configuration from '%s': %w", cfgFile, err)
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

		if len(cfg.AutoFlag.BlockedKeywords) > 0 {
			_ = database.SyncSpamSnippets(cfg.AutoFlag.BlockedKeywords)
		}

		opts := db.UnknownUserOptions{
			Keyword:            listUnknownUsersKeyword,
			ConfiguredKeywords: cfg.AutoFlag.BlockedKeywords,
			MaxPosts:           listUnknownUsersMaxPosts,
			MaxReputation:      listUnknownUsersMaxReputation,
			Limit:              listUnknownUsersLimit,
			DatabaseName:       cfg.DBPath,
		}

		items, err := database.GetUnbannedUnknownUsers(opts)
		if err != nil {
			return fmt.Errorf("failed to query unknown users: %w", err)
		}

		if listUnknownUsersBan {
			var matching []db.UnknownUserItem
			for _, u := range items {
				if u.IsSpamMatch || len(u.MatchedKeywords) > 0 {
					matching = append(matching, u)
				}
			}

			if len(matching) == 0 {
				fmt.Println("✅ No unbanned users matching the spam filter to ban.")
				return nil
			}

			fmt.Printf("🔨 Banning %d unbanned user(s) matching spam filter...\n", len(matching))
			var successCount, failCount int
			for _, u := range matching {
				err := database.SetUserBanned(u.UserID, true)
				if err != nil {
					failCount++
					fmt.Printf("❌ User %d (@%s) - Failed: %v\n", u.UserID, u.Username, err)
				} else {
					successCount++
					fmt.Printf("✅ User %d (@%s) - Banned [Matched: %s]\n", u.UserID, u.Username, strings.Join(u.MatchedKeywords, ", "))
				}
			}
			fmt.Printf("\n🔨 Summary: %d successfully banned, %d failed out of %d total matching users.\n", successCount, failCount, len(matching))
			return nil
		}

		markdownReport := db.GenerateUnknownUsersMarkdown(items, opts)

		if listUnknownUsersOutputFile != "" {
			if err := os.WriteFile(listUnknownUsersOutputFile, []byte(markdownReport), 0644); err != nil {
				return fmt.Errorf("failed to write Markdown report to '%s': %w", listUnknownUsersOutputFile, err)
			}
			fmt.Printf("✅ Unknown/new user directory Markdown report written to '%s' (%d users found)\n", listUnknownUsersOutputFile, len(items))
			return nil
		}

		fmt.Print(markdownReport)
		return nil
	},
}

func init() {
	listUnknownUsersCmd.Flags().StringVarP(&listUnknownUsersOutputFile, "output", "o", "", "Write Markdown output to a file instead of stdout")
	listUnknownUsersCmd.Flags().StringVarP(&listUnknownUsersKeyword, "keyword", "k", "", "Filter by specific keyword or phrase in profile or names (empty matches all)")
	listUnknownUsersCmd.Flags().IntVarP(&listUnknownUsersMaxPosts, "max-posts", "m", 5, "Filter users with at most N logged posts (0 for any post count)")
	listUnknownUsersCmd.Flags().IntVarP(&listUnknownUsersMaxReputation, "max-rep", "r", 20, "Filter users with at most N reputation score (-1 for any reputation)")
	listUnknownUsersCmd.Flags().IntVarP(&listUnknownUsersLimit, "limit", "l", 0, "Limit number of users displayed (0 for unlimited)")
	listUnknownUsersCmd.Flags().BoolVarP(&listUnknownUsersBan, "ban", "b", false, "Ban all users matching the spam filter in database")
	rootCmd.AddCommand(listUnknownUsersCmd)
}
