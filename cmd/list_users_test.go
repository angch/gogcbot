package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/angch/gogcbot/pkg/config"
	"github.com/angch/gogcbot/pkg/db"
)

func TestListUsersCmd(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "gogcbot_cmd_test_*")
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
	superAdminID := int64(1001)
	modAdminID := int64(2002)
	badUserID := int64(3003)

	_, _, _ = database.GetOrCreateUser(superAdminID, "adminuser", "Admin", "User", 100)
	_, _, _ = database.GetOrCreateUser(modAdminID, "moduser", "Mod", "User", 100)
	_ = database.SetUserAdmin(modAdminID, true)

	_, _, _ = database.GetOrCreateUser(badUserID, "banneduser", "Bad", "Guy", 0)
	fp, _ := database.CreateFlaggedPost(-100, 10, badUserID, "Crypto scam link")
	_ = database.ResolveFlaggedPost(fp.ID, "banned", modAdminID)
	_ = database.SetUserBanned(badUserID, true)

	database.Close()

	// Save test config
	cfg := config.DefaultConfig()
	cfg.DBPath = dbPath
	cfg.SuperAdminID = superAdminID
	if err := config.SaveConfig(cfgPath, &cfg); err != nil {
		t.Fatalf("failed to save test config: %v", err)
	}

	// Set global cfgFile
	cfgFile = cfgPath

	t.Run("Execute default list-users", func(t *testing.T) {
		buf := new(bytes.Buffer)
		rootCmd.SetOut(buf)
		rootCmd.SetErr(buf)
		rootCmd.SetArgs([]string{"list-users", "--config", cfgPath})

		listUsersGoodOnly = false
		listUsersBadOnly = false
		listUsersManualBansOnly = false
		listUsersLimit = 0
		listUsersOutputFile = ""

		err := rootCmd.Execute()
		if err != nil {
			t.Fatalf("list-users command failed: %v", err)
		}
	})

	t.Run("Execute list-users with output file", func(t *testing.T) {
		outMd := filepath.Join(tmpDir, "users_report.md")
		listUsersOutputFile = outMd
		listUsersGoodOnly = false
		listUsersBadOnly = false
		listUsersManualBansOnly = false
		listUsersLimit = 0

		rootCmd.SetArgs([]string{"list-users", "--config", cfgPath, "--output", outMd})
		err := rootCmd.Execute()
		if err != nil {
			t.Fatalf("list-users with output file failed: %v", err)
		}

		data, err := os.ReadFile(outMd)
		if err != nil {
			t.Fatalf("failed to read output markdown file: %v", err)
		}
		content := string(data)
		if !strings.Contains(content, "# 📋 GoGCBot User Directory") {
			t.Errorf("expected header in generated file")
		}
		if !strings.Contains(content, "@adminuser") {
			t.Errorf("expected @adminuser in generated file")
		}
		if !strings.Contains(content, "@banneduser") {
			t.Errorf("expected @banneduser in generated file")
		}
		if !strings.Contains(content, "Manually Banned by Moderators") {
			t.Errorf("expected manual bans section in generated file")
		}
	})
}

func TestListSpamBiosCmd(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "gogcbot_spambios_test_*")
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
	_, _, _ = database.GetOrCreateUser(5001, "spammer_new", "Spam", "User", 0)
	_ = database.SaveUserProfile(&db.UserProfile{
		UserID:     5001,
		Username:   "spammer_new",
		FirstName:  "Spam",
		LastName:   "User",
		Bio:        "锦鲤代发 @mmmmue 6折础油卡E卡、沃尔玛、永辉、携程。天猫、苹果礼品卡、Steam等 联系 @xgshenqing888",
		HasPhoto:   true,
		PhotoCount: 1,
	})

	database.Close()

	// Save test config
	cfg := config.DefaultConfig()
	cfg.DBPath = dbPath
	if err := config.SaveConfig(cfgPath, &cfg); err != nil {
		t.Fatalf("failed to save test config: %v", err)
	}

	cfgFile = cfgPath

	t.Run("Execute list-spambios stdout", func(t *testing.T) {
		buf := new(bytes.Buffer)
		rootCmd.SetOut(buf)
		rootCmd.SetErr(buf)
		rootCmd.SetArgs([]string{"list-spambios", "--config", cfgPath})

		listSpamBiosOutputFile = ""
		listSpamBiosKeyword = ""
		listSpamBiosMaxPosts = 5
		listSpamBiosLimit = 0

		err := rootCmd.Execute()
		if err != nil {
			t.Fatalf("list-spambios command failed: %v", err)
		}
	})

	t.Run("Execute list-spambios output file", func(t *testing.T) {
		outMd := filepath.Join(tmpDir, "spambios_report.md")
		listSpamBiosOutputFile = outMd
		listSpamBiosKeyword = "沃尔玛"
		listSpamBiosMaxPosts = 5
		listSpamBiosLimit = 0

		rootCmd.SetArgs([]string{"list-spambios", "--config", cfgPath, "--output", outMd, "--keyword", "沃尔玛"})
		err := rootCmd.Execute()
		if err != nil {
			t.Fatalf("list-spambios with output file failed: %v", err)
		}

		data, err := os.ReadFile(outMd)
		if err != nil {
			t.Fatalf("failed to read output markdown file: %v", err)
		}
		content := string(data)
		if !strings.Contains(content, "# 📋 Unbanned New Users with Profile Bios") {
			t.Errorf("expected header in generated file")
		}
		if !strings.Contains(content, "@spammer_new") {
			t.Errorf("expected @spammer_new in generated file")
		}
		if !strings.Contains(content, "5001") {
			t.Errorf("expected user ID 5001 in generated file")
		}
	})
}
