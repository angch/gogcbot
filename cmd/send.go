package cmd

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/angch/gogcbot/pkg/bot"
	"github.com/angch/gogcbot/pkg/config"
	"github.com/angch/gogcbot/pkg/db"
	"github.com/spf13/cobra"
)

var (
	sendGroupID   string
	sendMessage   string
	sendParseMode string
	sendMarkdown  bool
	sendHTML      bool
	sendPlain     bool
	sendSilent    bool
	sendReplyTo   int
	sendDryRun    bool
)

var sendCmd = &cobra.Command{
	Use:     "send <group_id> [message...]",
	Aliases: []string{"send-message", "sendmsg", "msg", "say", "post"},
	Short:   "Send a message to a Telegram group or channel as the bot",
	Long: `Send a message to a specified Telegram group, supergroup, or channel as the bot.

Accepts numeric group chat IDs (e.g. -1001234567890) or matching group titles from the database.
Message content can be supplied as positional arguments, via the -m/--message flag, or piped via stdin.

Examples:
  # Send text message to a group
  gogcbot send -1001234567890 "Important announcement for all members."

  # Send multiple arguments as message
  gogcbot send -1001234567890 Weekly status update: all systems operational.

  # Send Markdown formatted message
  gogcbot send -1001234567890 -m "*Notice:* Please review the updated rules."

  # Send HTML formatted message
  gogcbot send -1001234567890 --html "<b>Notice:</b> Please review the updated rules."

  # Send silent message (no notification sound)
  gogcbot send -1001234567890 --silent "Scheduled maintenance starting in 10 minutes."

  # Pipe message content from stdin
  echo "Automated alert from CI/CD pipeline" | gogcbot send -1001234567890

  # Reply to a specific message ID in the group
  gogcbot send -1001234567890 --reply-to 42 "Acknowledged."`,
	DisableFlagParsing: true,
	RunE: func(cmd *cobra.Command, rawArgs []string) error {
		opts, err := parseSendArgs(rawArgs, cfgFile)
		if err != nil {
			return err
		}

		if opts.help {
			return cmd.Help()
		}

		// Resolve message from stdin if needed
		if opts.message == "" || opts.message == "-" {
			stat, _ := os.Stdin.Stat()
			isPipe := (stat != nil && (stat.Mode()&os.ModeCharDevice) == 0)
			if isPipe || opts.message == "-" {
				data, err := io.ReadAll(cmd.InOrStdin())
				if err == nil {
					opts.message = strings.TrimSpace(string(data))
				}
			}
		}

		if opts.groupIDRaw == "" {
			return fmt.Errorf("group ID is required\n-> Usage: gogcbot send <group_id> [message...]\n-> Example: gogcbot send -1001234567890 \"Hello group members!\"")
		}

		if opts.message == "" {
			return fmt.Errorf("message is required\n-> Usage: gogcbot send <group_id> [message...]\n-> Action: Provide the message as an argument, via -m/--message, or pipe through stdin.")
		}

		cfg, err := config.LoadConfig(opts.configPath)
		if err != nil {
			return fmt.Errorf("failed to load configuration from '%s': %w\n-> Action: Ensure '%s' exists, or run 'gogcbot init-config' to create a default configuration.", opts.configPath, err, opts.configPath)
		}

		if cfg.TelegramToken == "" {
			return fmt.Errorf("telegram_token is missing in configuration file '%s' (or GOGCBOT_TELEGRAM_TOKEN env variable).\n-> Action: Add your Telegram bot token to '%s' or export GOGCBOT_TELEGRAM_TOKEN=\"<your_token>\".", opts.configPath, opts.configPath)
		}

		var database *db.DB
		if cfg.DBPath != "" {
			dbPath := cfg.DBPath
			if !filepath.IsAbs(dbPath) {
				dbPath = filepath.Join(filepath.Dir(opts.configPath), dbPath)
			}
			if dbInst, err := db.OpenDB(dbPath); err == nil {
				database = dbInst
				defer database.Close()
			}
		}

		chatID, groupTitle, err := resolveTargetGroupID(opts.groupIDRaw, database)
		if err != nil {
			return err
		}

		titleSuffix := ""
		if groupTitle != "" {
			titleSuffix = fmt.Sprintf(" (%s)", groupTitle)
		}

		parseMode := opts.parseMode
		if parseMode == "" {
			parseMode = "markdown"
		}

		out := cmd.OutOrStdout()

		if opts.dryRun {
			fmt.Fprintln(out, "🚀 [DRY RUN] Would send message as bot:")
			fmt.Fprintf(out, "Target Group ID    : %d%s\n", chatID, titleSuffix)
			fmt.Fprintf(out, "Parse Mode         : %s\n", parseMode)
			fmt.Fprintf(out, "Silent (No Sound)  : %t\n", opts.silent)
			if opts.replyTo > 0 {
				fmt.Fprintf(out, "Reply-To Message ID: %d\n", opts.replyTo)
			} else {
				fmt.Fprintln(out, "Reply-To Message ID: none")
			}
			fmt.Fprintln(out, "Message Content    :")
			fmt.Fprintln(out, "--------------------------------------------------")
			fmt.Fprintln(out, opts.message)
			fmt.Fprintln(out, "--------------------------------------------------")
			return nil
		}

		b, err := bot.NewBot(cfg, database)
		if err != nil {
			return fmt.Errorf("failed to initialize Telegram bot engine: %w\n-> Action: Verify your Telegram Bot Token with @BotFather and ensure internet connectivity.", err)
		}

		botOpts := bot.SendMessageOptions{
			ParseMode:           parseMode,
			DisableNotification: opts.silent,
			ReplyToMessageID:    opts.replyTo,
		}

		sentMsgs, err := b.SendMessage(chatID, opts.message, botOpts)
		if err != nil {
			return fmt.Errorf("failed to send message to group %d: %w\n-> Action: Ensure the bot is added to group %d with message sending permissions.", chatID, err, chatID)
		}

		if len(sentMsgs) == 1 {
			fmt.Fprintf(out, "✅ Message sent successfully to group %d%s!\n", chatID, titleSuffix)
			fmt.Fprintf(out, "Group ID   : %d\n", chatID)
			fmt.Fprintf(out, "Message ID : %d\n", sentMsgs[0].MessageID)
			fmt.Fprintf(out, "Parse Mode : %s\n", parseMode)
			fmt.Fprintf(out, "Timestamp  : %s\n", time.Now().Format("2006-01-02 15:04:05 MST"))
		} else {
			var msgIDs []string
			for _, m := range sentMsgs {
				msgIDs = append(msgIDs, strconv.Itoa(m.MessageID))
			}
			fmt.Fprintf(out, "✅ Message sent successfully to group %d%s (in %d chunks)!\n", chatID, titleSuffix, len(sentMsgs))
			fmt.Fprintf(out, "Group ID    : %d\n", chatID)
			fmt.Fprintf(out, "Message IDs : %s\n", strings.Join(msgIDs, ", "))
			fmt.Fprintf(out, "Parse Mode  : %s\n", parseMode)
			fmt.Fprintf(out, "Timestamp   : %s\n", time.Now().Format("2006-01-02 15:04:05 MST"))
		}

		return nil
	},
}

type parsedSendArgs struct {
	configPath string
	groupIDRaw string
	message    string
	parseMode  string
	silent     bool
	replyTo    int
	dryRun     bool
	help       bool
	positional []string
}

var negativeNumRegex = regexp.MustCompile(`^-\d+$`)

func parseSendArgs(rawArgs []string, defaultCfg string) (*parsedSendArgs, error) {
	opts := &parsedSendArgs{
		configPath: defaultCfg,
		parseMode:  "markdown",
	}

	i := 0
	for i < len(rawArgs) {
		arg := rawArgs[i]

		if arg == "--" {
			opts.positional = append(opts.positional, rawArgs[i+1:]...)
			break
		}

		if arg == "-h" || arg == "--help" {
			opts.help = true
			i++
			continue
		}

		if arg == "-c" || arg == "--config" {
			if i+1 >= len(rawArgs) {
				return nil, fmt.Errorf("flag needs an argument: %s", arg)
			}
			opts.configPath = rawArgs[i+1]
			i += 2
			continue
		} else if strings.HasPrefix(arg, "--config=") {
			opts.configPath = strings.TrimPrefix(arg, "--config=")
			i++
			continue
		}

		if arg == "-g" || arg == "--group" {
			if i+1 >= len(rawArgs) {
				return nil, fmt.Errorf("flag needs an argument: %s", arg)
			}
			opts.groupIDRaw = rawArgs[i+1]
			i += 2
			continue
		} else if strings.HasPrefix(arg, "--group=") {
			opts.groupIDRaw = strings.TrimPrefix(arg, "--group=")
			i++
			continue
		}

		if arg == "-m" || arg == "--message" {
			if i+1 >= len(rawArgs) {
				return nil, fmt.Errorf("flag needs an argument: %s", arg)
			}
			opts.message = rawArgs[i+1]
			i += 2
			continue
		} else if strings.HasPrefix(arg, "--message=") {
			opts.message = strings.TrimPrefix(arg, "--message=")
			i++
			continue
		}

		if arg == "-p" || arg == "--parse-mode" {
			if i+1 >= len(rawArgs) {
				return nil, fmt.Errorf("flag needs an argument: %s", arg)
			}
			opts.parseMode = rawArgs[i+1]
			i += 2
			continue
		} else if strings.HasPrefix(arg, "--parse-mode=") {
			opts.parseMode = strings.TrimPrefix(arg, "--parse-mode=")
			i++
			continue
		}

		if arg == "--markdown" {
			opts.parseMode = "markdown"
			i++
			continue
		}

		if arg == "--html" {
			opts.parseMode = "html"
			i++
			continue
		}

		if arg == "--plain" || arg == "--text" {
			opts.parseMode = "plain"
			i++
			continue
		}

		if arg == "-s" || arg == "--silent" {
			opts.silent = true
			i++
			continue
		}

		if arg == "-r" || arg == "--reply-to" {
			if i+1 >= len(rawArgs) {
				return nil, fmt.Errorf("flag needs an argument: %s", arg)
			}
			val, err := strconv.Atoi(rawArgs[i+1])
			if err != nil {
				return nil, fmt.Errorf("invalid message ID for --reply-to: '%s'", rawArgs[i+1])
			}
			opts.replyTo = val
			i += 2
			continue
		} else if strings.HasPrefix(arg, "--reply-to=") {
			valStr := strings.TrimPrefix(arg, "--reply-to=")
			val, err := strconv.Atoi(valStr)
			if err != nil {
				return nil, fmt.Errorf("invalid message ID for --reply-to: '%s'", valStr)
			}
			opts.replyTo = val
			i++
			continue
		}

		if arg == "--dry-run" {
			opts.dryRun = true
			i++
			continue
		}

		if strings.HasPrefix(arg, "--") {
			return nil, fmt.Errorf("unknown flag: %s\nRun 'gogcbot send --help' for usage.", arg)
		}

		// Check if it's a single dash flag that isn't a negative number or stdin indicator '-'
		if strings.HasPrefix(arg, "-") && !negativeNumRegex.MatchString(arg) && arg != "-" {
			return nil, fmt.Errorf("unknown shorthand flag: '%s'\nRun 'gogcbot send --help' for usage.", arg)
		}

		opts.positional = append(opts.positional, arg)
		i++
	}

	if opts.groupIDRaw == "" && len(opts.positional) > 0 {
		opts.groupIDRaw = opts.positional[0]
		opts.positional = opts.positional[1:]
	}

	if opts.message == "" && len(opts.positional) > 0 {
		opts.message = strings.Join(opts.positional, " ")
	}

	return opts, nil
}

func resolveTargetGroupID(input string, database *db.DB) (int64, string, error) {
	input = strings.TrimSpace(input)

	// Try direct integer parse (e.g. -1001234567890 or 123456789)
	if id, err := strconv.ParseInt(input, 10, 64); err == nil {
		if database != nil {
			if g, err := database.GetGroup(id); err == nil && g != nil && g.ChatID != 0 {
				return g.ChatID, g.Title, nil
			}
			// Try negative if positive ID given
			if id > 0 {
				if g, err := database.GetGroup(-id); err == nil && g != nil && g.ChatID != 0 {
					return g.ChatID, g.Title, nil
				}
				if superID, err := strconv.ParseInt("-100"+input, 10, 64); err == nil {
					if g, err := database.GetGroup(superID); err == nil && g != nil && g.ChatID != 0 {
						return g.ChatID, g.Title, nil
					}
				}
			}
		}
		return id, "", nil
	}

	// Non-numeric string: look up in database groups table if available
	if database != nil {
		groups, err := database.GetAllGroups()
		if err == nil && len(groups) > 0 {
			var matches []db.Group
			lowerInput := strings.ToLower(input)
			for _, g := range groups {
				if strings.ToLower(g.Title) == lowerInput {
					matches = append(matches, g)
				}
			}
			if len(matches) == 0 {
				for _, g := range groups {
					if strings.Contains(strings.ToLower(g.Title), lowerInput) {
						matches = append(matches, g)
					}
				}
			}

			if len(matches) == 1 {
				return matches[0].ChatID, matches[0].Title, nil
			} else if len(matches) > 1 {
				var titles []string
				for _, m := range matches {
					titles = append(titles, fmt.Sprintf("- %s (ID: %d)", m.Title, m.ChatID))
				}
				return 0, "", fmt.Errorf("multiple groups matched '%s':\n%s\n-> Action: Please specify the numeric group ID directly.", input, strings.Join(titles, "\n"))
			}
		}
		return 0, "", fmt.Errorf("group '%s' not found in database\n-> Action: Please provide the numeric Telegram group chat ID (e.g. -1001234567890).", input)
	}

	return 0, "", fmt.Errorf("invalid group ID '%s': must be a numeric Telegram chat ID (e.g. -1001234567890)", input)
}

func init() {
	sendCmd.Flags().StringVarP(&sendGroupID, "group", "g", "", "Target Telegram group or channel ID")
	sendCmd.Flags().StringVarP(&sendMessage, "message", "m", "", "Message content to send")
	sendCmd.Flags().StringVarP(&sendParseMode, "parse-mode", "p", "markdown", "Message parse mode (markdown, html, plain)")
	sendCmd.Flags().BoolVar(&sendMarkdown, "markdown", false, "Send message formatted as Markdown (default)")
	sendCmd.Flags().BoolVar(&sendHTML, "html", false, "Send message formatted as HTML")
	sendCmd.Flags().BoolVar(&sendPlain, "plain", false, "Send message as plain text (no formatting)")
	sendCmd.Flags().BoolVarP(&sendSilent, "silent", "s", false, "Send message silently without notification sound")
	sendCmd.Flags().IntVarP(&sendReplyTo, "reply-to", "r", 0, "Message ID to reply to in the group")
	sendCmd.Flags().BoolVar(&sendDryRun, "dry-run", false, "Simulate sending without making Telegram API call")

	rootCmd.AddCommand(sendCmd)
}
