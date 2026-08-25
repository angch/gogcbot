package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/angch/gogcbot/pkg/bot"
	"github.com/angch/gogcbot/pkg/config"
	"github.com/angch/gogcbot/pkg/db"
	"github.com/spf13/cobra"
)

var (
	rescanMaxRep  int
	rescanHours   int
	rescanForce   bool
	rescanDryRun  bool
	rescanLive    bool
	rescanDelayMs int
)

var rescanUsersCmd = &cobra.Command{
	Use:     "rescan-users",
	Aliases: []string{"rescan", "rescan-profiles"},
	Short:   "Rescan low reputation users' names & profiles (>24h old) and trigger join ban rules (dry-run by default)",
	Long: `Rescan low-reputation users (>24 hours since last scan) by fetching their fresh names,
usernames, and profile bios from Telegram API. Re-evaluates join rules (e.g. red_packet_cjk_name,
new_user_spam_bio) and shows possible actions (dry-run by default). Use --live to execute bans.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.LoadConfig(cfgFile)
		if err != nil {
			return fmt.Errorf("failed to load config from '%s': %w", cfgFile, err)
		}

		if cfg.TelegramToken == "" {
			return fmt.Errorf("telegram_token is required to query Telegram API for user profiles")
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

		b, err := bot.NewBot(cfg, database)
		if err != nil {
			return fmt.Errorf("failed to initialize Telegram bot engine: %w", err)
		}

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
		go func() {
			<-sigChan
			fmt.Println("\nReceived interrupt signal, stopping rescan gracefully...")
			cancel()
		}()

		delay := time.Duration(rescanDelayMs) * time.Millisecond
		if delay <= 0 {
			delay = 100 * time.Millisecond
		}

		liveFlag, _ := cmd.Flags().GetBool("live")
		dryRunFlag, _ := cmd.Flags().GetBool("dry-run")
		isDryRun := true
		if liveFlag {
			isDryRun = false
		} else if cmd.Flags().Changed("dry-run") {
			isDryRun = dryRunFlag
		}

		out := cmd.OutOrStdout()

		// Explicit Gating: Live execution requires a verified prior dry-run
		if !isDryRun {
			gate, errGate := database.GetValidDryRunGate("rescan_users")
			if errGate != nil || gate == nil {
				fmt.Fprintln(out, "❌ EXECUTION GATED: Live mass-banning rescan requires a completed dry-run preview first!")
				fmt.Fprintln(out, "👉 Explicit Gating Rule: You must run without --live (or with --dry-run) to preview potential bans and validate against known good data before live actions can proceed.")
				return fmt.Errorf("live rescan gated: must run dry-run first")
			}
			fmt.Fprintf(out, "🔓 Dry-run gate verified (Preview completed at %s: %s).\n🚀 Proceeding with LIVE mass-banning...\n\n",
				gate.ExecutedAt.Format("2006-01-02 15:04:05"), gate.Details)
		} else {
			// In dry-run mode, validate rules against known good and bad datasets
			fmt.Fprintln(out, "🔍 Validating detection rules against Known Good and Bad datasets...")
			report, errVal := b.ValidateRulesAgainstKnownData()
			if errVal == nil && report != nil {
				if !report.Passed {
					fmt.Fprintln(out, "❌ RULE REGRESSION DETECTED against Known Good data!")
					fmt.Fprintln(out, report.Summary)
					return fmt.Errorf("rule validation failed: %d false positive violation(s) on known good data", len(report.FalsePositives))
				}
				fmt.Fprintf(out, "✅ Pre-check passed: 0 false positive regressions on %d known good profiles (%d known bad detections).\n\n",
					report.KnownGoodTotal, len(report.KnownBadMatches))
			}
		}

		opts := bot.RescanOptions{
			MaxReputation: rescanMaxRep,
			Hours:         rescanHours,
			Force:         rescanForce,
			Delay:         delay,
			DryRun:        isDryRun,
		}

		fmt.Fprintln(out, "🚀 Starting low-reputation user rescan...")
		fmt.Fprintf(out, "Database: %s | Max Rep: %d | Age Threshold: >%d hrs | Force: %t | DryRun: %t\n\n",
			dbPath, opts.MaxReputation, opts.Hours, opts.Force, opts.DryRun)

		res, err := b.RescanLowRepUsers(ctx, opts, func(curr, total int, u *db.User, p *db.UserProfile, triggeredRule, reason string, err error) {
			if err != nil {
				fmt.Fprintf(out, "[%d/%d] ❌ User %d (@%s): %v\n", curr, total, u.UserID, u.Username, err)
			} else if triggeredRule != "" {
				dryTag := ""
				if opts.DryRun {
					dryTag = " [DRY RUN - PROPOSED]"
				}
				fmt.Fprintf(out, "[%d/%d] 🚫 User %d (@%s) TRIGGERED RULE '%s'%s: %s\n", curr, total, u.UserID, u.Username, triggeredRule, dryTag, reason)
			} else {
				bioSnippet := ""
				if p != nil {
					bioSnippet = p.Bio
					if len(bioSnippet) > 30 {
						bioSnippet = bioSnippet[:27] + "..."
					}
				}
				if bioSnippet == "" {
					bioSnippet = "(clean / no bio)"
				}
				fmt.Fprintf(out, "[%d/%d] ✨ User %d (@%s - %s %s) | Rep: %d | Bio: %s\n", curr, total, u.UserID, u.Username, u.FirstName, u.LastName, u.Reputation, bioSnippet)
			}
		})

		fmt.Fprintln(out, "\n📊 === User Rescan Summary ===")
		if res != nil {
			fmt.Fprintf(out, "Total Candidates     : %d\n", res.TotalCandidates)
			fmt.Fprintf(out, "Successfully Scanned : %d\n", res.ScannedCount)
			fmt.Fprintf(out, "🚫 Matches (Triggered: %d\n", len(res.Matches))
			fmt.Fprintf(out, "✨ Clean             : %d\n", res.CleanCount)
			fmt.Fprintf(out, "⚠️ Errors / Not Found: %d\n", res.ErrorCount)
			fmt.Fprintf(out, "Dry Run Mode         : %t\n", opts.DryRun)

			if len(res.Matches) > 0 {
				if opts.DryRun {
					fmt.Fprintf(out, "\n🎯 Possible Actions on Matches (%d):\n", len(res.Matches))
				} else {
					fmt.Fprintf(out, "\n🔨 Executed Actions on Matches (%d):\n", len(res.Matches))
				}
				for i, m := range res.Matches {
					userTag := fmt.Sprintf("%d", m.User.UserID)
					if m.User.Username != "" {
						userTag = fmt.Sprintf("@%s (%d)", m.User.Username, m.User.UserID)
					}
					fmt.Fprintf(out, "  %d. %s -> Rule: %s | Action: %s\n     Reason: %s\n",
						i+1, userTag, m.TriggeredRule, m.ActionSummary, m.Reason)
				}
			}

			if opts.DryRun {
				// Record dry-run gate in DB
				_ = database.RecordDryRunGate("rescan_users", res.TotalCandidates, len(res.Matches), "rescan_rules", true, fmt.Sprintf("%d candidates, %d matches", res.TotalCandidates, len(res.Matches)), 1*time.Hour)
				fmt.Fprintln(out, "\n✅ Dry-run completed successfully!")
				fmt.Fprintln(out, "🔓 Live execution gate is UNLOCKED for 1 hour.")
				fmt.Fprintln(out, "👉 To execute these actions live, run:")
				fmt.Fprintln(out, "   ./gogcbot rescan-users --config config.yaml --live")
			} else {
				// Consume dry-run gate
				_ = database.ConsumeDryRunGate("rescan_users")
				fmt.Fprintln(out, "\n✅ Live rescan mass-banning completed. Dry-run gate consumed.")
			}
		}

		if err != nil && err != context.Canceled {
			return err
		}
		return nil
	},
}

func init() {
	rescanUsersCmd.Flags().IntVar(&rescanMaxRep, "max-rep", 20, "Maximum reputation score threshold for candidate users")
	rescanUsersCmd.Flags().IntVar(&rescanHours, "hours", 24, "Minimum hours since previous scan to be eligible for rescan")
	rescanUsersCmd.Flags().BoolVar(&rescanForce, "force", false, "Force rescan of all low-rep users regardless of 24h scan age")
	rescanUsersCmd.Flags().BoolVar(&rescanDryRun, "dry-run", true, "Simulate rescan and evaluate rules without issuing bans (default true)")
	rescanUsersCmd.Flags().BoolVar(&rescanLive, "live", false, "Execute live bans and actions for matching users (disables dry-run)")
	rescanUsersCmd.Flags().IntVar(&rescanDelayMs, "delay-ms", 100, "Delay in milliseconds between Telegram API requests")
	rootCmd.AddCommand(rescanUsersCmd)
}
