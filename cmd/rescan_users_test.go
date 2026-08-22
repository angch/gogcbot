package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/angch/gogcbot/pkg/bot"
	"github.com/angch/gogcbot/pkg/config"
	"github.com/angch/gogcbot/pkg/db"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func TestRescanUsersCmd(t *testing.T) {
	restore := bot.SetNewBotAPIFuncForTesting(func(token string) (*tgbotapi.BotAPI, error) {
		return &tgbotapi.BotAPI{Self: tgbotapi.User{ID: 999, UserName: "testbot"}}, nil
	})
	defer restore()

	tmpDir, err := os.MkdirTemp("", "gogcbot_rescan_test_*")
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

	// Insert test data
	userID := int64(8890112233)
	_, _, _ = database.GetOrCreateUser(userID, "cbzbQFLOuHNkJZ", "每日首发🧧", "", 5)
	_ = database.SaveUserProfile(&db.UserProfile{
		UserID:    userID,
		Username:  "cbzbQFLOuHNkJZ",
		FirstName: "每日首发🧧",
		Bio:       "General bio",
		FetchedAt: time.Now().Add(-30 * time.Hour),
	})

	database.Close()

	cfg := config.DefaultConfig()
	cfg.DBPath = dbPath
	cfg.TelegramToken = "mock_token"
	if err := config.SaveConfig(cfgPath, &cfg); err != nil {
		t.Fatalf("failed to save test config: %v", err)
	}

	cfgFile = cfgPath

	t.Run("Execute rescan-users dry-run", func(t *testing.T) {
		buf := new(bytes.Buffer)
		rootCmd.SetOut(buf)
		rootCmd.SetErr(buf)
		rootCmd.SetArgs([]string{"rescan-users", "--config", cfgPath, "--dry-run", "--delay-ms", "1"})

		err := rootCmd.Execute()
		if err != nil {
			t.Fatalf("rescan-users failed: %v", err)
		}

		output := buf.String()
		if !strings.Contains(output, "User Rescan Summary") {
			t.Errorf("expected User Rescan Summary in output, got: %s", output)
		}
	})
}
