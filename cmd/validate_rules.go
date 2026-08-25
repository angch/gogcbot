package cmd

import (
	"fmt"
	"path/filepath"

	"github.com/angch/gogcbot/pkg/bot"
	"github.com/angch/gogcbot/pkg/config"
	"github.com/angch/gogcbot/pkg/db"
	"github.com/spf13/cobra"
)

var validateRulesCmd = &cobra.Command{
	Use:     "validate-rules",
	Aliases: []string{"check-rules", "test-rules"},
	Short:   "Validate all detector rules against known good and known bad datasets",
	Long: `Validate all registered detector rules against known good data (rep >= 50)
and known bad data (banned spammers) in the SQLite database, bypassing reputation
and banned requirements to ensure no false-positive regressions.`,
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

		det := bot.BuildDetector(cfg, database)
		report, err := det.ValidateAgainstKnownGoodAndBad(database)
		if err != nil {
			return fmt.Errorf("validation check failed: %w", err)
		}

		out := cmd.OutOrStdout()
		fmt.Fprintln(out, report.Summary)

		if !report.Passed {
			return fmt.Errorf("rule validation failed with %d false positive violation(s)", len(report.FalsePositives))
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(validateRulesCmd)
}
