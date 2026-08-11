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
			return fmt.Errorf("failed to install service: %w\n-> Action: Installing OS system services requires root/sudo privileges. Try running:\n   sudo gogcbot service install --config %s", err, cfgFile)
		}
		fmt.Println("✅ GoGCBot service successfully installed!")
		fmt.Println("You can now start it using: sudo gogcbot service start (or via Windows Services Control Panel / systemctl)")
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
			return fmt.Errorf("failed to uninstall service: %w\n-> Action: Uninstalling OS system services requires root/sudo privileges. Try running:\n   sudo gogcbot service uninstall", err)
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
		st, err := svc.Status()
		if err == service.ErrNotInstalled || st == service.StatusUnknown {
			return fmt.Errorf("cannot start service: GoGCBot OS service is not installed.\n-> Action: Install the service first by running:\n   sudo gogcbot service install --config %s\n   then start it with:\n   sudo gogcbot service start", cfgFile)
		}
		if st == service.StatusRunning {
			fmt.Println("▶️ GoGCBot service is already running.")
			return nil
		}
		if err := service.Control(svc, "start"); err != nil {
			return fmt.Errorf("failed to start service: %w\n-> Action: Starting system services requires root/sudo privileges. Try running:\n   sudo gogcbot service start\n   Inspect service logs with:\n   journalctl -u GoGCBot.service -n 50 --no-pager", err)
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
		st, err := svc.Status()
		if err == service.ErrNotInstalled || st == service.StatusUnknown {
			return fmt.Errorf("cannot stop service: GoGCBot OS service is not installed.\n-> Action: Install the service first by running:\n   sudo gogcbot service install --config %s", cfgFile)
		}
		if st == service.StatusStopped {
			fmt.Println("⏹️ GoGCBot service is already stopped.")
			return nil
		}
		if err := service.Control(svc, "stop"); err != nil {
			return fmt.Errorf("failed to stop service: %w\n-> Action: Stopping system services requires root/sudo privileges. Try running:\n   sudo gogcbot service stop", err)
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
			fmt.Println("-> Action: Run 'sudo gogcbot service install' to install the service.")
			return nil
		} else if err != nil {
			return fmt.Errorf("failed to get service status: %w\n-> Action: Ensure systemd / OS service manager is active on your system.", err)
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
