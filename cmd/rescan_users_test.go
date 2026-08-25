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

	t.Run("Execute rescan-users with --live without dryrun (should be gated)", func(t *testing.T) {
		buf := new(bytes.Buffer)
		rootCmd.SetOut(buf)
		rootCmd.SetErr(buf)
		rootCmd.SetArgs([]string{"rescan-users", "--config", cfgPath, "--live", "--delay-ms", "1"})

		err := rootCmd.Execute()
		if err == nil {
			t.Fatalf("expected rescan-users --live to fail without prior dryrun gate")
		}
		if !strings.Contains(err.Error(), "live rescan gated") {
			t.Errorf("expected gating error message, got: %v", err)
		}
		output := buf.String()
		if !strings.Contains(output, "EXECUTION GATED") {
			t.Errorf("expected EXECUTION GATED in output, got: %s", output)
		}
	})

	t.Run("Execute rescan-users default (dry-run)", func(t *testing.T) {
		buf := new(bytes.Buffer)
		rootCmd.SetOut(buf)
		rootCmd.SetErr(buf)
		rootCmd.SetArgs([]string{"rescan-users", "--config", cfgPath, "--live=false", "--delay-ms", "1"})

		err := rootCmd.Execute()
		if err != nil {
			t.Fatalf("rescan-users failed: %v", err)
		}

		output := buf.String()
		if !strings.Contains(output, "User Rescan Summary") {
			t.Errorf("expected User Rescan Summary in output, got: %s", output)
		}
		if !strings.Contains(output, "Dry Run Mode         : true") {
			t.Errorf("expected Dry Run Mode to be true by default, got: %s", output)
		}
		if !strings.Contains(output, "Live execution gate is UNLOCKED") {
			t.Errorf("expected unlock message in dry-run output, got: %s", output)
		}
	})

	t.Run("Execute rescan-users with --live after dryrun (should succeed and consume gate)", func(t *testing.T) {
		buf := new(bytes.Buffer)
		rootCmd.SetOut(buf)
		rootCmd.SetErr(buf)
		rootCmd.SetArgs([]string{"rescan-users", "--config", cfgPath, "--live", "--delay-ms", "1"})

		err := rootCmd.Execute()
		if err != nil {
			t.Fatalf("rescan-users failed: %v", err)
		}

		output := buf.String()
		if !strings.Contains(output, "Dry Run Mode         : false") {
			t.Errorf("expected Dry Run Mode to be false with --live, got: %s", output)
		}
		if !strings.Contains(output, "Dry-run gate consumed") {
			t.Errorf("expected gate consumed message, got: %s", output)
		}
	})

	t.Run("Execute rescan-users with --live second time (should be gated again)", func(t *testing.T) {
		buf := new(bytes.Buffer)
		rootCmd.SetOut(buf)
		rootCmd.SetErr(buf)
		rootCmd.SetArgs([]string{"rescan-users", "--config", cfgPath, "--live", "--delay-ms", "1"})

		err := rootCmd.Execute()
		if err == nil {
			t.Fatalf("expected second --live run to be gated because gate was consumed")
		}
	})
}

func TestValidateRulesCmd(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "gogcbot_val_cmd_test_*")
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
	_ = database.SaveKnownGoodUser(&db.KnownGoodUser{
		UserID:     1001,
		Username:   "good_user",
		FirstName:  "Good",
		Reputation: 100,
		Bio:        "Gopher developer",
	})
	_ = database.SaveKnownBadUser(&db.KnownBadUser{
		UserID:    2002,
		Username:  "cbzbQFLOuHNkJZ",
		FirstName: "每日首发🧧",
		Pattern:   "每日首发🧧",
		BanReason: "Spam name",
	})
	database.Close()

	cfg := config.DefaultConfig()
	cfg.DBPath = dbPath
	if err := config.SaveConfig(cfgPath, &cfg); err != nil {
		t.Fatalf("failed to save test config: %v", err)
	}

	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{"validate-rules", "--config", cfgPath})

	err = rootCmd.Execute()
	if err != nil {
		t.Fatalf("validate-rules failed: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "Known Data Rule Validation Report") {
		t.Errorf("expected validation report in output, got: %s", output)
	}
	if !strings.Contains(output, "passed with 0 false positive regressions") {
		t.Errorf("expected passed message in output, got: %s", output)
	}
}

func TestKnownDataCmd(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "gogcbot_kd_cmd_test_*")
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
	_, _, _ = database.GetOrCreateUser(5001, "topdev", "Top", "Dev", 60)
	_, _, _ = database.GetOrCreateUser(6001, "bannedspammer", "Spam", "Bot", 0)
	_ = database.SetUserBanned(6001, true)
	database.Close()

	cfg := config.DefaultConfig()
	cfg.DBPath = dbPath
	if err := config.SaveConfig(cfgPath, &cfg); err != nil {
		t.Fatalf("failed to save test config: %v", err)
	}

	// 1. Test known-data --sync
	bufSync := new(bytes.Buffer)
	rootCmd.SetOut(bufSync)
	rootCmd.SetErr(bufSync)
	rootCmd.SetArgs([]string{"known-data", "--config", cfgPath, "--sync"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("known-data --sync failed: %v", err)
	}
	if !strings.Contains(bufSync.String(), "Synchronized Known Datasets") {
		t.Errorf("expected sync confirmation, got: %s", bufSync.String())
	}

	// 2. Test known-data --list
	bufList := new(bytes.Buffer)
	rootCmd.SetOut(bufList)
	rootCmd.SetErr(bufList)
	rootCmd.SetArgs([]string{"known-data", "--config", cfgPath, "--sync=false", "--list"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("known-data --list failed: %v", err)
	}
	output := bufList.String()
	if !strings.Contains(output, "Known Good Users (rep >= 50) : 1") {
		t.Errorf("expected 1 known good user, got: %s", output)
	}
	if !strings.Contains(output, "Known Bad Users (banned)     : 1") {
		t.Errorf("expected 1 known bad user, got: %s", output)
	}
	if !strings.Contains(output, "@topdev") {
		t.Errorf("expected @topdev in list, got: %s", output)
	}
}
