package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/angch/gogcbot/pkg/config"
	"github.com/angch/gogcbot/pkg/db"
	"github.com/spf13/cobra"
)

var (
	userOutputFile string
	userJSON       bool
)

var userCmd = &cobra.Command{
	Use:     "user <@username|user_id>",
	Aliases: []string{"userinfo", "inspect-user", "get-user", "dump-user", "u"},
	Short:   "Dump all database information about a telegram user, including profiles",
	Long: `Dump all stored database records for a specified Telegram user by @username or numeric User ID.
Includes core user record, cached Telegram profile & bio, spam bio matching status,
logged messages, reputation change logs, and moderation flags/bans.`,
	Args: cobra.ExactArgs(1),
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

		dump, err := database.GetUserFullDump(args[0], cfg.SuperAdminID, cfg.AutoFlag.BlockedKeywords...)
		if err != nil {
			return fmt.Errorf("failed to find user: %w", err)
		}

		var outputContent string
		if userJSON {
			data, err := json.MarshalIndent(dump, "", "  ")
			if err != nil {
				return fmt.Errorf("failed to marshal user data to JSON: %w", err)
			}
			outputContent = string(data) + "\n"
		} else {
			outputContent = db.FormatUserDump(dump)
		}

		if userOutputFile != "" {
			if err := os.WriteFile(userOutputFile, []byte(outputContent), 0644); err != nil {
				return fmt.Errorf("failed to write output to '%s': %w", userOutputFile, err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "✅ User dossier successfully saved to: %s\n", userOutputFile)
			return nil
		}

		fmt.Fprint(cmd.OutOrStdout(), outputContent)
		return nil
	},
}

func init() {
	userCmd.Flags().StringVarP(&userOutputFile, "output", "o", "", "output file path to write the user dossier (default is stdout)")
	userCmd.Flags().BoolVar(&userJSON, "json", false, "output raw user record in JSON format")
	rootCmd.AddCommand(userCmd)
}
