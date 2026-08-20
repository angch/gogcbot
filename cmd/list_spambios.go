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
	listSpamBiosOutputFile string
	listSpamBiosKeyword    string
	listSpamBiosMaxPosts   int
	listSpamBiosLimit      int
)

var listSpamBiosCmd = &cobra.Command{
	Use:     "list-spambios",
	Aliases: []string{"spambios", "listspambios", "spam-bios", "spammers"},
	Short:   "List unbanned new users with profile bios (matches all by default, or filtered by keyword)",
	Long: `Scan cached user profiles for unbanned new users with non-empty bios.
By default (empty keyword), matches all unbanned users with bios.
Use --keyword / -k to filter by specific keywords or phrases (e.g. 锦鲤代发, 油卡, E卡, 沃尔玛, 联系 @...).`,
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

		opts := db.SpamBioOptions{
			Keyword:      listSpamBiosKeyword,
			MaxPosts:     listSpamBiosMaxPosts,
			Limit:        listSpamBiosLimit,
			DatabaseName: cfg.DBPath,
		}

		items, err := database.GetUnbannedSpamBioUsers(opts)
		if err != nil {
			return fmt.Errorf("failed to query spam bio users: %w", err)
		}

		markdownReport := db.GenerateSpamBioMarkdown(items, opts)

		if listSpamBiosOutputFile != "" {
			if err := os.WriteFile(listSpamBiosOutputFile, []byte(markdownReport), 0644); err != nil {
				return fmt.Errorf("failed to write Markdown report to '%s': %w", listSpamBiosOutputFile, err)
			}
			fmt.Printf("✅ Spam bio user directory Markdown report written to '%s' (%d users found)\n", listSpamBiosOutputFile, len(items))
			return nil
		}

		fmt.Print(markdownReport)
		return nil
	},
}

func init() {
	listSpamBiosCmd.Flags().StringVarP(&listSpamBiosOutputFile, "output", "o", "", "Write Markdown output to a file instead of stdout")
	listSpamBiosCmd.Flags().StringVarP(&listSpamBiosKeyword, "keyword", "k", "", "Filter by specific keyword or phrase in bio (empty matches all)")
	listSpamBiosCmd.Flags().IntVarP(&listSpamBiosMaxPosts, "max-posts", "m", 5, "Filter users with at most N logged posts (0 for any post count)")
	listSpamBiosCmd.Flags().IntVarP(&listSpamBiosLimit, "limit", "l", 0, "Limit number of users displayed (0 for unlimited)")
	rootCmd.AddCommand(listSpamBiosCmd)
}
