package cmd

import (
	"fmt"

	"github.com/angch/gogcbot/pkg/config"
	"github.com/spf13/cobra"
)

var outputFile string

var initConfigCmd = &cobra.Command{
	Use:   "init-config",
	Short: "Generate a default configuration YAML file",
	RunE: func(cmd *cobra.Command, args []string) error {
		target := outputFile
		if target == "" {
			target = cfgFile
		}

		if err := config.SaveDefaultConfig(target); err != nil {
			return fmt.Errorf("failed to create config file: %w", err)
		}

		fmt.Printf("✅ Default configuration file created successfully at: %s\n", target)
		fmt.Println("Please edit the file and fill in your 'telegram_token' and 'super_admin_id' before running.")
		return nil
	},
}

func init() {
	initConfigCmd.Flags().StringVarP(&outputFile, "output", "o", "config.yaml", "destination path for configuration file")
	rootCmd.AddCommand(initConfigCmd)
}
