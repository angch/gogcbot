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
	banCheckDryRun  bool
	banCheckDelayMs int
)

var banCheckCmd = &cobra.Command{
	Use:     "bancheck",
	Aliases: []string{"checkbans", "verifybans"},
	Short:   "Audit all banned users across monitored channels/groups and enforce missing kick bans",
	Long: `Audit all banned users in the SQLite database to verify they are permanently banned
across all monitored Telegram channels and groups. If a banned user is found unbanned, issues
a kick ban. Requests and kick bans are strictly throttled to at most 1 request per second.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.LoadConfig(cfgFile)
		if err != nil {
			return fmt.Errorf("failed to load config from '%s': %w", cfgFile, err)
		}

		if cfg.TelegramToken == "" {
			return fmt.Errorf("telegram_token is required to query Telegram API and verify bans")
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
			fmt.Println("\nReceived interrupt signal, stopping ban check gracefully...")
			cancel()
		}()

		delay := time.Duration(banCheckDelayMs) * time.Millisecond
		if delay < time.Second {
			delay = time.Second
		}

		opts := bot.BanCheckOptions{
			Delay:  delay,
			DryRun: banCheckDryRun,
		}

		out := cmd.OutOrStdout()
		fmt.Fprintln(out, "🔍 Starting ban check audit across monitored channels & groups...")
		fmt.Fprintf(out, "Database: %s | Delay: %v (rate <= 1 req/sec) | DryRun: %t\n\n",
			dbPath, opts.Delay, opts.DryRun)

		res, err := b.CheckBannedUsersAcrossGroups(ctx, opts, func(currUser, totalUsers, currGroup, totalGroups int, u *db.User, g *db.Group, status string, rebanned bool, err error) {
			if err != nil {
				fmt.Fprintf(out, "[User %d/%d | Group %d/%d] ❌ User %d (@%s) in '%s' (%d): %v\n",
					currUser, totalUsers, currGroup, totalGroups, u.UserID, u.Username, g.Title, g.ChatID, err)
			} else if rebanned {
				fmt.Fprintf(out, "[User %d/%d | Group %d/%d] 🔨 ENFORCED BAN for user %d (@%s) in '%s' (%d) (was: %s)\n",
					currUser, totalUsers, currGroup, totalGroups, u.UserID, u.Username, g.Title, g.ChatID, status)
			} else if status != "kicked" && opts.DryRun {
				fmt.Fprintf(out, "[User %d/%d | Group %d/%d] ⚠️ [DRY RUN] Missing ban for user %d (@%s) in '%s' (%d) (Status: %s)\n",
					currUser, totalUsers, currGroup, totalGroups, u.UserID, u.Username, g.Title, g.ChatID, status)
			} else {
				fmt.Fprintf(out, "[User %d/%d | Group %d/%d] ✅ User %d (@%s) verified banned in '%s' (%d) (Status: %s)\n",
					currUser, totalUsers, currGroup, totalGroups, u.UserID, u.Username, g.Title, g.ChatID, status)
			}
		})

		fmt.Fprintln(out, "\n📊 === Ban Check Summary ===")
		if res != nil {
			fmt.Fprintf(out, "Total Banned Users in DB: %d\n", res.TotalBannedUsers)
			fmt.Fprintf(out, "Monitored Groups/Channels : %d\n", res.TotalGroups)
			fmt.Fprintf(out, "Total Checks Evaluated   : %d\n", res.TotalChecks)
			fmt.Fprintf(out, "✅ Already Banned         : %d\n", res.AlreadyBanned)
			fmt.Fprintf(out, "🔨 Newly Re-banned        : %d\n", res.RebannedCount)
			fmt.Fprintf(out, "⚠️ Errors                 : %d\n", res.ErrorCount)
			fmt.Fprintf(out, "⏱️ Elapsed Time           : %s\n", res.Duration.Round(time.Second))
			fmt.Fprintf(out, "Dry Run Mode             : %t\n", opts.DryRun)
		}

		if err != nil && err != context.Canceled {
			return err
		}
		return nil
	},
}

func init() {
	banCheckCmd.Flags().BoolVar(&banCheckDryRun, "dry-run", false, "Simulate ban check and report missing bans without issuing kick bans")
	banCheckCmd.Flags().IntVar(&banCheckDelayMs, "delay-ms", 1000, "Delay in milliseconds between Telegram API calls (minimum 1000ms enforced)")
	rootCmd.AddCommand(banCheckCmd)
}
