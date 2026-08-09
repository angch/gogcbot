package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var cfgFile string

var rootCmd = &cobra.Command{
	Use:   "gogcbot",
	Short: "GoGCBot - Telegram Group Management & Reputation Moderation Bot",
	Long: `GoGCBot is a high-performance Telegram moderation bot written in pure Go.
It monitors Telegram groups, maintains a 7-day log of messages, keeps up to 50 posts per user,
and uses user reputation scores alongside auto-flagging rules to forward suspicious posts to a Private Moderation Group.`,
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().StringVarP(&cfgFile, "config", "c", "config.yaml", "config file path (default is config.yaml)")
}
