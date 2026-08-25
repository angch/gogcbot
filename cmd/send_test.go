package cmd

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/angch/gogcbot/pkg/bot"
	"github.com/angch/gogcbot/pkg/config"
	"github.com/angch/gogcbot/pkg/db"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func setupSendTestEnv(t *testing.T) (string, string, *db.DB, func()) {
	t.Helper()

	tmpDir, err := os.MkdirTemp("", "gogcbot_send_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}

	dbPath := filepath.Join(tmpDir, "test.db")
	cfgPath := filepath.Join(tmpDir, "config.yaml")

	database, err := db.OpenDB(dbPath)
	if err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("failed to open test db: %v", err)
	}

	// Insert test groups
	_ = database.SaveGroup(-1001234567890, "General Chat", "supergroup")
	_ = database.SaveGroup(-1009998887776, "Announcements Channel", "channel")

	cfg := config.DefaultConfig()
	cfg.DBPath = dbPath
	cfg.TelegramToken = "mock_telegram_token_12345"
	cfg.SuperAdminID = 1001
	if err := config.SaveConfig(cfgPath, &cfg); err != nil {
		database.Close()
		os.RemoveAll(tmpDir)
		t.Fatalf("failed to save test config: %v", err)
	}

	cfgFile = cfgPath

	cleanup := func() {
		database.Close()
		os.RemoveAll(tmpDir)
	}

	return tmpDir, cfgPath, database, cleanup
}

func TestSendCmd_PositionalArgs(t *testing.T) {
	_, cfgPath, _, cleanup := setupSendTestEnv(t)
	defer cleanup()

	restoreBot := bot.SetNewBotAPIFuncForTesting(func(token string) (*tgbotapi.BotAPI, error) {
		return &tgbotapi.BotAPI{Self: tgbotapi.User{ID: 999, UserName: "testbot"}}, nil
	})
	defer restoreBot()

	restoreSend := bot.SetMockSendFuncForTesting(func(c tgbotapi.Chattable) (tgbotapi.Message, error) {
		mc := c.(tgbotapi.MessageConfig)
		return tgbotapi.Message{
			MessageID: 1001,
			Chat:      &tgbotapi.Chat{ID: mc.ChatID},
			Text:      mc.Text,
		}, nil
	})
	defer restoreSend()

	t.Run("Send simple message with negative group ID", func(t *testing.T) {
		buf := new(bytes.Buffer)
		rootCmd.SetOut(buf)
		rootCmd.SetErr(buf)
		rootCmd.SetArgs([]string{"send", "-1001234567890", "Hello", "world", "from", "test", "--config", cfgPath})

		err := rootCmd.Execute()
		if err != nil {
			t.Fatalf("send command failed: %v", err)
		}

		output := buf.String()
		if !strings.Contains(output, "Message sent successfully to group -1001234567890 (General Chat)") {
			t.Errorf("expected success output, got: %s", output)
		}
		if !strings.Contains(output, "Group ID   : -1001234567890") {
			t.Errorf("expected group ID in output, got: %s", output)
		}
		if !strings.Contains(output, "Message ID : 1001") {
			t.Errorf("expected message ID 1001 in output, got: %s", output)
		}
	})

	t.Run("Send with alias send-message", func(t *testing.T) {
		buf := new(bytes.Buffer)
		rootCmd.SetOut(buf)
		rootCmd.SetErr(buf)
		rootCmd.SetArgs([]string{"send-message", "-1001234567890", "Broadcast notification", "--config", cfgPath})

		err := rootCmd.Execute()
		if err != nil {
			t.Fatalf("send-message alias failed: %v", err)
		}

		output := buf.String()
		if !strings.Contains(output, "Message sent successfully") {
			t.Errorf("expected success message, got: %s", output)
		}
	})

	t.Run("Send with alias msg", func(t *testing.T) {
		buf := new(bytes.Buffer)
		rootCmd.SetOut(buf)
		rootCmd.SetErr(buf)
		rootCmd.SetArgs([]string{"msg", "-1001234567890", "Quick note", "--config", cfgPath})

		err := rootCmd.Execute()
		if err != nil {
			t.Fatalf("msg alias failed: %v", err)
		}

		output := buf.String()
		if !strings.Contains(output, "Message sent successfully") {
			t.Errorf("expected success message, got: %s", output)
		}
	})

	t.Run("Send with alias say", func(t *testing.T) {
		buf := new(bytes.Buffer)
		rootCmd.SetOut(buf)
		rootCmd.SetErr(buf)
		rootCmd.SetArgs([]string{"say", "-1001234567890", "Saying something", "--config", cfgPath})

		err := rootCmd.Execute()
		if err != nil {
			t.Fatalf("say alias failed: %v", err)
		}

		output := buf.String()
		if !strings.Contains(output, "Message sent successfully") {
			t.Errorf("expected success message, got: %s", output)
		}
	})

	t.Run("Send with alias post", func(t *testing.T) {
		buf := new(bytes.Buffer)
		rootCmd.SetOut(buf)
		rootCmd.SetErr(buf)
		rootCmd.SetArgs([]string{"post", "-1001234567890", "Posting an update", "--config", cfgPath})

		err := rootCmd.Execute()
		if err != nil {
			t.Fatalf("post alias failed: %v", err)
		}

		output := buf.String()
		if !strings.Contains(output, "Message sent successfully") {
			t.Errorf("expected success message, got: %s", output)
		}
	})
}

func TestSendCmd_Flags(t *testing.T) {
	_, cfgPath, _, cleanup := setupSendTestEnv(t)
	defer cleanup()

	var lastChattable tgbotapi.Chattable
	restoreBot := bot.SetNewBotAPIFuncForTesting(func(token string) (*tgbotapi.BotAPI, error) {
		return &tgbotapi.BotAPI{Self: tgbotapi.User{ID: 999, UserName: "testbot"}}, nil
	})
	defer restoreBot()

	restoreSend := bot.SetMockSendFuncForTesting(func(c tgbotapi.Chattable) (tgbotapi.Message, error) {
		lastChattable = c
		mc := c.(tgbotapi.MessageConfig)
		return tgbotapi.Message{
			MessageID: 1002,
			Chat:      &tgbotapi.Chat{ID: mc.ChatID},
			Text:      mc.Text,
		}, nil
	})
	defer restoreSend()

	t.Run("Send with -g and -m flags", func(t *testing.T) {
		buf := new(bytes.Buffer)
		rootCmd.SetOut(buf)
		rootCmd.SetErr(buf)
		rootCmd.SetArgs([]string{"send", "-g", "-1001234567890", "-m", "Flag based message", "--config", cfgPath})

		err := rootCmd.Execute()
		if err != nil {
			t.Fatalf("send with -g and -m flags failed: %v", err)
		}

		output := buf.String()
		if !strings.Contains(output, "Message sent successfully") {
			t.Errorf("expected success message, got: %s", output)
		}
	})

	t.Run("Send with --group and --message flags", func(t *testing.T) {
		buf := new(bytes.Buffer)
		rootCmd.SetOut(buf)
		rootCmd.SetErr(buf)
		rootCmd.SetArgs([]string{"send", "--group=-1001234567890", "--message=Equal format message", "--config", cfgPath})

		err := rootCmd.Execute()
		if err != nil {
			t.Fatalf("send with --group= and --message= failed: %v", err)
		}

		output := buf.String()
		if !strings.Contains(output, "Message sent successfully") {
			t.Errorf("expected success message, got: %s", output)
		}
	})

	t.Run("Send with --silent and --html flags", func(t *testing.T) {
		buf := new(bytes.Buffer)
		rootCmd.SetOut(buf)
		rootCmd.SetErr(buf)
		rootCmd.SetArgs([]string{"send", "-1001234567890", "<b>HTML alert</b>", "--silent", "--html", "--config", cfgPath})

		err := rootCmd.Execute()
		if err != nil {
			t.Fatalf("send with --silent and --html failed: %v", err)
		}

		output := buf.String()
		if !strings.Contains(output, "Parse Mode : html") {
			t.Errorf("expected parse mode html, got: %s", output)
		}

		mc, ok := lastChattable.(tgbotapi.MessageConfig)
		if !ok {
			t.Fatalf("expected MessageConfig, got %T", lastChattable)
		}
		if !mc.DisableNotification {
			t.Errorf("expected DisableNotification to be true")
		}
		if mc.ParseMode != "HTML" {
			t.Errorf("expected ParseMode to be HTML, got %q", mc.ParseMode)
		}
	})

	t.Run("Send with --plain flag", func(t *testing.T) {
		buf := new(bytes.Buffer)
		rootCmd.SetOut(buf)
		rootCmd.SetErr(buf)
		rootCmd.SetArgs([]string{"send", "-1001234567890", "plain_text_test", "--plain", "--config", cfgPath})

		err := rootCmd.Execute()
		if err != nil {
			t.Fatalf("send with --plain failed: %v", err)
		}

		output := buf.String()
		if !strings.Contains(output, "Parse Mode : plain") {
			t.Errorf("expected parse mode plain, got: %s", output)
		}

		mc, ok := lastChattable.(tgbotapi.MessageConfig)
		if !ok {
			t.Fatalf("expected MessageConfig, got %T", lastChattable)
		}
		if mc.ParseMode != "" {
			t.Errorf("expected empty ParseMode for plain text, got %q", mc.ParseMode)
		}
	})

	t.Run("Send with --reply-to flag", func(t *testing.T) {
		buf := new(bytes.Buffer)
		rootCmd.SetOut(buf)
		rootCmd.SetErr(buf)
		rootCmd.SetArgs([]string{"send", "-1001234567890", "-r", "42", "Replying to message 42", "--config", cfgPath})

		err := rootCmd.Execute()
		if err != nil {
			t.Fatalf("send with --reply-to failed: %v", err)
		}

		output := buf.String()
		if !strings.Contains(output, "Message sent successfully") {
			t.Errorf("expected success message, got: %s", output)
		}

		mc, ok := lastChattable.(tgbotapi.MessageConfig)
		if !ok {
			t.Fatalf("expected MessageConfig, got %T", lastChattable)
		}
		if mc.ReplyToMessageID != 42 {
			t.Errorf("expected ReplyToMessageID 42, got %d", mc.ReplyToMessageID)
		}
	})
}

func TestSendCmd_DryRun(t *testing.T) {
	_, cfgPath, _, cleanup := setupSendTestEnv(t)
	defer cleanup()

	t.Run("Execute dry run", func(t *testing.T) {
		buf := new(bytes.Buffer)
		rootCmd.SetOut(buf)
		rootCmd.SetErr(buf)
		rootCmd.SetArgs([]string{"send", "-1001234567890", "This is a dry run test", "--dry-run", "--config", cfgPath})

		err := rootCmd.Execute()
		if err != nil {
			t.Fatalf("dry run failed: %v", err)
		}

		output := buf.String()
		if !strings.Contains(output, "[DRY RUN] Would send message as bot:") {
			t.Errorf("expected dry run header, got: %s", output)
		}
		if !strings.Contains(output, "Target Group ID    : -1001234567890 (General Chat)") {
			t.Errorf("expected group ID and title in dry run output, got: %s", output)
		}
		if !strings.Contains(output, "This is a dry run test") {
			t.Errorf("expected message content in dry run output, got: %s", output)
		}
	})
}

func TestSendCmd_GroupTitleLookup(t *testing.T) {
	_, cfgPath, _, cleanup := setupSendTestEnv(t)
	defer cleanup()

	restoreBot := bot.SetNewBotAPIFuncForTesting(func(token string) (*tgbotapi.BotAPI, error) {
		return &tgbotapi.BotAPI{Self: tgbotapi.User{ID: 999, UserName: "testbot"}}, nil
	})
	defer restoreBot()

	restoreSend := bot.SetMockSendFuncForTesting(func(c tgbotapi.Chattable) (tgbotapi.Message, error) {
		mc := c.(tgbotapi.MessageConfig)
		return tgbotapi.Message{
			MessageID: 1003,
			Chat:      &tgbotapi.Chat{ID: mc.ChatID},
			Text:      mc.Text,
		}, nil
	})
	defer restoreSend()

	t.Run("Send using exact group title", func(t *testing.T) {
		buf := new(bytes.Buffer)
		rootCmd.SetOut(buf)
		rootCmd.SetErr(buf)
		rootCmd.SetArgs([]string{"send", "General Chat", "Message for general chat", "--config", cfgPath})

		err := rootCmd.Execute()
		if err != nil {
			t.Fatalf("send by group title failed: %v", err)
		}

		output := buf.String()
		if !strings.Contains(output, "Message sent successfully to group -1001234567890 (General Chat)") {
			t.Errorf("expected resolved group ID and title, got: %s", output)
		}
	})

	t.Run("Send using partial group title", func(t *testing.T) {
		buf := new(bytes.Buffer)
		rootCmd.SetOut(buf)
		rootCmd.SetErr(buf)
		rootCmd.SetArgs([]string{"send", "Announcements", "Broadcast update", "--config", cfgPath})

		err := rootCmd.Execute()
		if err != nil {
			t.Fatalf("send by partial group title failed: %v", err)
		}

		output := buf.String()
		if !strings.Contains(output, "Message sent successfully to group -1009998887776 (Announcements Channel)") {
			t.Errorf("expected resolved channel ID and title, got: %s", output)
		}
	})
}

func TestSendCmd_StdinInput(t *testing.T) {
	_, cfgPath, _, cleanup := setupSendTestEnv(t)
	defer cleanup()

	restoreBot := bot.SetNewBotAPIFuncForTesting(func(token string) (*tgbotapi.BotAPI, error) {
		return &tgbotapi.BotAPI{Self: tgbotapi.User{ID: 999, UserName: "testbot"}}, nil
	})
	defer restoreBot()

	var receivedText string
	restoreSend := bot.SetMockSendFuncForTesting(func(c tgbotapi.Chattable) (tgbotapi.Message, error) {
		mc := c.(tgbotapi.MessageConfig)
		receivedText = mc.Text
		return tgbotapi.Message{
			MessageID: 1004,
			Chat:      &tgbotapi.Chat{ID: mc.ChatID},
			Text:      mc.Text,
		}, nil
	})
	defer restoreSend()

	t.Run("Send message from stdin via explicit dash", func(t *testing.T) {
		inBuf := bytes.NewBufferString("Message read from stdin stream")
		outBuf := new(bytes.Buffer)

		rootCmd.SetIn(inBuf)
		rootCmd.SetOut(outBuf)
		rootCmd.SetErr(outBuf)
		rootCmd.SetArgs([]string{"send", "-1001234567890", "-", "--config", cfgPath})

		err := rootCmd.Execute()
		if err != nil {
			t.Fatalf("send via stdin dash failed: %v", err)
		}

		output := outBuf.String()
		if !strings.Contains(output, "Message sent successfully") {
			t.Errorf("expected success output, got: %s", output)
		}
		if receivedText != "Message read from stdin stream" {
			t.Errorf("expected stdin message text, got: %q", receivedText)
		}
	})
}

func TestSendCmd_ErrorCases(t *testing.T) {
	_, cfgPath, _, cleanup := setupSendTestEnv(t)
	defer cleanup()

	t.Run("Missing group ID", func(t *testing.T) {
		buf := new(bytes.Buffer)
		rootCmd.SetOut(buf)
		rootCmd.SetErr(buf)
		rootCmd.SetArgs([]string{"send", "--config", cfgPath})

		err := rootCmd.Execute()
		if err == nil {
			t.Fatalf("expected error for missing group ID, got nil")
		}
		if !strings.Contains(err.Error(), "group ID is required") {
			t.Errorf("unexpected error message: %v", err)
		}
	})

	t.Run("Missing message", func(t *testing.T) {
		buf := new(bytes.Buffer)
		rootCmd.SetIn(new(bytes.Buffer))
		rootCmd.SetOut(buf)
		rootCmd.SetErr(buf)
		rootCmd.SetArgs([]string{"send", "-1001234567890", "--config", cfgPath})

		err := rootCmd.Execute()
		if err == nil {
			t.Fatalf("expected error for missing message, got nil")
		}
		if !strings.Contains(err.Error(), "message is required") {
			t.Errorf("unexpected error message: %v", err)
		}
	})

	t.Run("Unknown flag", func(t *testing.T) {
		buf := new(bytes.Buffer)
		rootCmd.SetOut(buf)
		rootCmd.SetErr(buf)
		rootCmd.SetArgs([]string{"send", "--non-existent-flag", "-1001234567890", "hello", "--config", cfgPath})

		err := rootCmd.Execute()
		if err == nil {
			t.Fatalf("expected error for unknown flag, got nil")
		}
		if !strings.Contains(err.Error(), "unknown flag") {
			t.Errorf("unexpected error message: %v", err)
		}
	})

	t.Run("Invalid reply-to value", func(t *testing.T) {
		buf := new(bytes.Buffer)
		rootCmd.SetOut(buf)
		rootCmd.SetErr(buf)
		rootCmd.SetArgs([]string{"send", "-1001234567890", "hello", "--reply-to", "not_a_number", "--config", cfgPath})

		err := rootCmd.Execute()
		if err == nil {
			t.Fatalf("expected error for invalid reply-to, got nil")
		}
		if !strings.Contains(err.Error(), "invalid message ID for --reply-to") {
			t.Errorf("unexpected error message: %v", err)
		}
	})

	t.Run("Non-existent group name in database", func(t *testing.T) {
		buf := new(bytes.Buffer)
		rootCmd.SetOut(buf)
		rootCmd.SetErr(buf)
		rootCmd.SetArgs([]string{"send", "Non Existent Supergroup Name", "hello", "--config", cfgPath})

		err := rootCmd.Execute()
		if err == nil {
			t.Fatalf("expected error for non-existent group name, got nil")
		}
		if !strings.Contains(err.Error(), "group 'Non Existent Supergroup Name' not found in database") {
			t.Errorf("unexpected error message: %v", err)
		}
	})
}

func TestSendCmd_HelpFlag(t *testing.T) {
	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{"send", "--help"})

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("send --help failed: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "Send a message to a specified Telegram group") {
		t.Errorf("expected help description in output, got: %s", output)
	}
	if !strings.Contains(output, "--parse-mode") {
		t.Errorf("expected --parse-mode in help output, got: %s", output)
	}
}

func TestBot_SendMessage(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "gogcbot_bot_send_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	database, err := db.OpenDB(dbPath)
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}
	defer database.Close()

	cfg := config.DefaultConfig()
	cfg.TelegramToken = "mock_token"

	restore := bot.SetNewBotAPIFuncForTesting(func(token string) (*tgbotapi.BotAPI, error) {
		return &tgbotapi.BotAPI{Self: tgbotapi.User{ID: 888, UserName: "senderbot"}}, nil
	})
	defer restore()

	b, err := bot.NewBot(&cfg, database)
	if err != nil {
		t.Fatalf("failed to init bot: %v", err)
	}

	t.Run("Send simple text message", func(t *testing.T) {
		var sentChattable tgbotapi.Chattable
		b.SetMockSendFunc(func(c tgbotapi.Chattable) (tgbotapi.Message, error) {
			sentChattable = c
			return tgbotapi.Message{
				MessageID: 101,
				Chat:      &tgbotapi.Chat{ID: -10012345},
				Text:      "Hello world",
			}, nil
		})

		msgs, err := b.SendMessage(-10012345, "Hello world", bot.SendMessageOptions{
			ParseMode: "markdown",
		})
		if err != nil {
			t.Fatalf("SendMessage failed: %v", err)
		}
		if len(msgs) != 1 || msgs[0].MessageID != 101 {
			t.Errorf("expected 1 sent message with ID 101, got: %v", msgs)
		}

		msgConfig, ok := sentChattable.(tgbotapi.MessageConfig)
		if !ok {
			t.Fatalf("expected MessageConfig, got: %T", sentChattable)
		}
		if msgConfig.Text != "Hello world" || msgConfig.ChatID != -10012345 {
			t.Errorf("unexpected message config: %+v", msgConfig)
		}
	})

	t.Run("Send markdown with fallback on error", func(t *testing.T) {
		attempts := 0
		var modes []string
		b.SetMockSendFunc(func(c tgbotapi.Chattable) (tgbotapi.Message, error) {
			attempts++
			mc := c.(tgbotapi.MessageConfig)
			modes = append(modes, mc.ParseMode)
			if attempts == 1 {
				return tgbotapi.Message{}, fmt.Errorf("Bad Request: can't parse entities: character '_' is reserved")
			}
			return tgbotapi.Message{
				MessageID: 102,
				Chat:      &tgbotapi.Chat{ID: -10012345},
				Text:      mc.Text,
			}, nil
		})

		msgs, err := b.SendMessage(-10012345, "unmatched _underscore text", bot.SendMessageOptions{
			ParseMode: "markdown",
		})
		if err != nil {
			t.Fatalf("SendMessage should succeed via fallback: %v", err)
		}
		if len(msgs) != 1 || msgs[0].MessageID != 102 {
			t.Errorf("expected 1 sent message with ID 102, got: %v", msgs)
		}
		if attempts != 2 {
			t.Errorf("expected 2 attempts (markdown then plain text), got: %d", attempts)
		}
		if len(modes) == 2 && modes[1] != "" {
			t.Errorf("expected second attempt parse mode to be empty (plain text), got: %q", modes[1])
		}
	})

	t.Run("Send long message chunked", func(t *testing.T) {
		sentCount := 0
		b.SetMockSendFunc(func(c tgbotapi.Chattable) (tgbotapi.Message, error) {
			sentCount++
			return tgbotapi.Message{
				MessageID: 200 + sentCount,
				Chat:      &tgbotapi.Chat{ID: -10012345},
			}, nil
		})

		longText := strings.Repeat("A very long line of text for chunking testing.\n", 150) // ~7050 characters
		msgs, err := b.SendMessage(-10012345, longText, bot.SendMessageOptions{})
		if err != nil {
			t.Fatalf("chunked SendMessage failed: %v", err)
		}
		if len(msgs) < 2 {
			t.Errorf("expected text to be split into at least 2 chunks, got: %d", len(msgs))
		}
	})

	t.Run("Send empty text returns error", func(t *testing.T) {
		_, err := b.SendMessage(-10012345, "   ", bot.SendMessageOptions{})
		if err == nil {
			t.Fatalf("expected error for empty message, got nil")
		}
	})
}
