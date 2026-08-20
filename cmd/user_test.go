package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/angch/gogcbot/pkg/config"
	"github.com/angch/gogcbot/pkg/db"
)

func TestUserCmd(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "gogcbot_user_test_*")
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

	superAdminID := int64(1001)
	spammerID := int64(555666)

	_, _, _ = database.GetOrCreateUser(superAdminID, "bossadmin", "Super", "Admin", 100)
	_, _, _ = database.GetOrCreateUser(spammerID, "spambot", "Spam", "Account", 0)

	_ = database.SaveUserProfile(&db.UserProfile{
		UserID:     spammerID,
		Username:   "spambot",
		FirstName:  "Spam",
		LastName:   "Account",
		Bio:        "联系 @xgshenqing888 6折油卡 沃尔玛 永辉",
		RawJSON:    `{"id":555666,"username":"spambot","personal_chat":{"title":"6折卡"}}`,
		HasPhoto:   true,
		PhotoCount: 1,
		FetchedAt:  time.Now(),
	})

	_ = database.SaveMessage(&db.Message{
		ChatID:    -100999,
		MessageID: 42,
		UserID:    spammerID,
		Text:      "First spam message",
		CreatedAt: time.Now(),
	})

	_, _ = database.AdjustReputation(spammerID, -20, "Detection trigger (new_user_spam_bio)", 0)
	fp, _ := database.CreateFlaggedPost(-100999, 42, spammerID, "Spam bio detected")
	_ = database.ResolveFlaggedPost(fp.ID, "banned", 0)
	_ = database.SetUserBanned(spammerID, true)

	database.Close()

	cfg := config.DefaultConfig()
	cfg.DBPath = dbPath
	cfg.SuperAdminID = superAdminID
	if err := config.SaveConfig(cfgPath, &cfg); err != nil {
		t.Fatalf("failed to save test config: %v", err)
	}

	cfgFile = cfgPath

	t.Run("Query user by @tag", func(t *testing.T) {
		buf := new(bytes.Buffer)
		rootCmd.SetOut(buf)
		rootCmd.SetErr(buf)
		rootCmd.SetArgs([]string{"user", "@spambot", "--config", cfgPath})

		userOutputFile = ""
		userJSON = false

		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("user command by tag failed: %v", err)
		}

		out := buf.String()
		if !strings.Contains(out, "Telegram User Dossier: @spambot") {
			t.Errorf("expected header with username, got: %s", out)
		}
		if !strings.Contains(out, "555666") {
			t.Errorf("expected user ID in output, got: %s", out)
		}
		if !strings.Contains(out, "Spam Match") {
			t.Errorf("expected spam bio match in output, got: %s", out)
		}
		if !strings.Contains(out, "First spam message") {
			t.Errorf("expected logged message in output, got: %s", out)
		}
		if !strings.Contains(out, "Reputation Audit Trail") {
			t.Errorf("expected reputation logs in output, got: %s", out)
		}
		if !strings.Contains(out, "Raw Telegram Profile JSON") {
			t.Errorf("expected Raw Telegram Profile JSON section in output, got: %s", out)
		}
	})

	t.Run("Query user by numeric ID", func(t *testing.T) {
		buf := new(bytes.Buffer)
		rootCmd.SetOut(buf)
		rootCmd.SetErr(buf)
		rootCmd.SetArgs([]string{"user", "555666", "--config", cfgPath})

		userOutputFile = ""
		userJSON = false

		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("user command by ID failed: %v", err)
		}

		out := buf.String()
		if !strings.Contains(out, "555666") {
			t.Errorf("expected user ID 555666 in output, got: %s", out)
		}
	})

	t.Run("Query user with --json flag", func(t *testing.T) {
		buf := new(bytes.Buffer)
		rootCmd.SetOut(buf)
		rootCmd.SetErr(buf)
		rootCmd.SetArgs([]string{"user", "--json", "555666", "--config", cfgPath})

		userOutputFile = ""
		userJSON = false

		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("user command with --json failed: %v", err)
		}

		out := buf.String()
		if !strings.Contains(out, `"user_id": 555666`) {
			t.Errorf("expected JSON user_id field, got: %s", out)
		}
		if !strings.Contains(out, `"is_spam_bio_match": true`) {
			t.Errorf("expected JSON is_spam_bio_match field, got: %s", out)
		}
		if !strings.Contains(out, `"raw_json"`) {
			t.Errorf("expected JSON raw_json field, got: %s", out)
		}
	})

	t.Run("Query user with -o output file", func(t *testing.T) {
		outFile := filepath.Join(tmpDir, "dossier.md")
		buf := new(bytes.Buffer)
		rootCmd.SetOut(buf)
		rootCmd.SetErr(buf)
		rootCmd.SetArgs([]string{"user", "-o", outFile, "@spambot", "--config", cfgPath})

		userOutputFile = ""
		userJSON = false

		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("user command with -o failed: %v", err)
		}

		data, err := os.ReadFile(outFile)
		if err != nil {
			t.Fatalf("failed to read output file: %v", err)
		}
		if !strings.Contains(string(data), "@spambot") {
			t.Errorf("expected dossier file to contain @spambot")
		}
	})

	t.Run("Query nonexistent user returns error", func(t *testing.T) {
		buf := new(bytes.Buffer)
		rootCmd.SetOut(buf)
		rootCmd.SetErr(buf)
		rootCmd.SetArgs([]string{"user", "@nonexistent999", "--config", cfgPath})

		userOutputFile = ""
		userJSON = false

		if err := rootCmd.Execute(); err == nil {
			t.Errorf("expected error for nonexistent user, got nil")
		}
	})
}
