package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/angch/gogcbot/pkg/bot"
	"github.com/angch/gogcbot/pkg/config"
	"github.com/angch/gogcbot/pkg/db"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func TestBanCheckCmd(t *testing.T) {
	restore := bot.SetNewBotAPIFuncForTesting(func(token string) (*tgbotapi.BotAPI, error) {
		return &tgbotapi.BotAPI{Self: tgbotapi.User{ID: 999, UserName: "testbot"}}, nil
	})
	defer restore()

	tmpDir, err := os.MkdirTemp("", "gogcbot_bancheck_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	cfgPath := filepath.Join(tmpDir, "config.yaml")

	database, err := db.OpenDB(dbPath)
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}

	// Insert test group and banned user
	_ = database.SaveGroup(-100123456, "Main Channel", "supergroup")
	userID := int64(8890998877)
	_, _, _ = database.GetOrCreateUser(userID, "banned_spammer", "Banned", "Spammer", -50)
	_ = database.SetUserBanned(userID, true)

	database.Close()

	cfg := config.DefaultConfig()
	cfg.DBPath = dbPath
	cfg.TelegramToken = "mock_token"
	if err := config.SaveConfig(cfgPath, &cfg); err != nil {
		t.Fatalf("failed to save test config: %v", err)
	}

	cfgFile = cfgPath

	t.Run("Execute bancheck dry-run", func(t *testing.T) {
		buf := new(bytes.Buffer)
		rootCmd.SetOut(buf)
		rootCmd.SetErr(buf)
		rootCmd.SetArgs([]string{"bancheck", "--config", cfgPath, "--dry-run", "--delay-ms", "1000"})

		err := rootCmd.Execute()
		if err != nil {
			t.Fatalf("bancheck failed: %v", err)
		}

		output := buf.String()
		if !strings.Contains(output, "Ban Check Summary") {
			t.Errorf("expected Ban Check Summary in output, got: %s", output)
		}
		if !strings.Contains(output, "Total Banned Users in DB: 1") {
			t.Errorf("expected 1 banned user in output, got: %s", output)
		}
	})
}
