package cmd

import (
	"fmt"

	svcpkg "github.com/angch/gogcbot/pkg/service"
	"github.com/kardianos/service"
	"github.com/spf13/cobra"
)

var serviceCmd = &cobra.Command{
	Use:   "service",
	Short: "Manage GoGCBot as an OS system service (Windows Service / Systemd)",
	Long:  `Install, uninstall, start, stop, or check status of GoGCBot as a background OS system service.`,
}

var installCmd = &cobra.Command{
	Use:   "install",
	Short: "Install GoGCBot as an OS system service",
	RunE: func(cmd *cobra.Command, args []string) error {
		svc, err := svcpkg.GetService(cfgFile)
		if err != nil {
			return err
		}
		if err := svc.Install(); err != nil {
			return fmt.Errorf("failed to install service: %w", err)
		}
		fmt.Println("✅ GoGCBot service successfully installed!")
		fmt.Println("You can now start it using: gogcbot service start (or via Windows Services Control Panel / systemctl)")
		return nil
	},
}

var uninstallCmd = &cobra.Command{
	Use:   "uninstall",
	Short: "Uninstall GoGCBot OS system service",
	RunE: func(cmd *cobra.Command, args []string) error {
		svc, err := svcpkg.GetService(cfgFile)
		if err != nil {
			return err
		}
		if err := svc.Uninstall(); err != nil {
			return fmt.Errorf("failed to uninstall service: %w", err)
		}
		fmt.Println("🗑️ GoGCBot service successfully uninstalled.")
		return nil
	},
}

var startCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the GoGCBot system service",
	RunE: func(cmd *cobra.Command, args []string) error {
		svc, err := svcpkg.GetService(cfgFile)
		if err != nil {
			return err
		}
		if err := service.Control(svc, "start"); err != nil {
			return fmt.Errorf("failed to start service: %w", err)
		}
		fmt.Println("▶️ GoGCBot service started successfully!")
		return nil
	},
}

var stopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the GoGCBot system service",
	RunE: func(cmd *cobra.Command, args []string) error {
		svc, err := svcpkg.GetService(cfgFile)
		if err != nil {
			return err
		}
		if err := service.Control(svc, "stop"); err != nil {
			return fmt.Errorf("failed to stop service: %w", err)
		}
		fmt.Println("⏹️ GoGCBot service stopped successfully.")
		return nil
	},
}

var serviceStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Check the status of the GoGCBot system service",
	RunE: func(cmd *cobra.Command, args []string) error {
		svc, err := svcpkg.GetService(cfgFile)
		if err != nil {
			return err
		}
		st, err := svc.Status()
		if err == service.ErrNotInstalled {
			fmt.Println("⚙️ GoGCBot System Service Status: Not Installed ⏹️")
			return nil
		} else if err != nil {
			return fmt.Errorf("failed to get service status: %w", err)
		}

		statusStr := "Unknown"
		switch st {
		case service.StatusRunning:
			statusStr = "Running ▶️"
		case service.StatusStopped:
			statusStr = "Stopped ⏹️"
		case service.StatusUnknown:
			statusStr = "Not Installed / Unknown ❓"
		}

		fmt.Printf("⚙️ GoGCBot System Service Status: %s\n", statusStr)
		return nil
	},
}

func init() {
	serviceCmd.AddCommand(installCmd)
	serviceCmd.AddCommand(uninstallCmd)
	serviceCmd.AddCommand(startCmd)
	serviceCmd.AddCommand(stopCmd)
	serviceCmd.AddCommand(serviceStatusCmd)

	rootCmd.AddCommand(serviceCmd)
}
