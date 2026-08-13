package bot

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/angch/gogcbot/pkg/config"
	"github.com/angch/gogcbot/pkg/db"
	"github.com/angch/gogcbot/pkg/detector"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func setupTestBot(t *testing.T) (*Bot, func()) {
	tmpDir, err := os.MkdirTemp("", "gogcbot_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	dbPath := filepath.Join(tmpDir, "test.db")
	database, err := db.OpenDB(dbPath)
	if err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("failed to open test db: %v", err)
	}

	cfg := config.DefaultConfig()
	cfg.TelegramToken = "123456:dummy_token"

	b := &Bot{
		cfg:     &cfg,
		db:      database,
		botUser: tgbotapi.User{ID: 999, UserName: "testbot"},
	}

	cleanup := func() {
		database.Close()
		os.RemoveAll(tmpDir)
	}

	return b, cleanup
}

func TestScheduleBanRecheck_SkippedWhenUnbannedInDB(t *testing.T) {
	b, cleanup := setupTestBot(t)
	defer cleanup()

	userID := int64(12345)
	chatID := int64(-1001)

	// User not marked as banned in DB
	_, _, _ = b.db.GetOrCreateUser(userID, "user1", "User", "One", 0)

	// Schedule recheck with tiny delay
	b.ScheduleBanRecheck(chatID, userID, 10*time.Millisecond)

	time.Sleep(50 * time.Millisecond)

	u, err := b.db.GetUserByID(userID)
	if err != nil {
		t.Fatalf("failed to fetch user: %v", err)
	}
	if u.IsBanned {
		t.Errorf("expected user IsBanned to be false")
	}
}

func TestHandleChatMemberUpdate_DetectsBanChanged(t *testing.T) {
	b, cleanup := setupTestBot(t)
	defer cleanup()

	userID := int64(54321)
	chatID := int64(-1001)

	_, _, _ = b.db.GetOrCreateUser(userID, "user2", "User", "Two", 0)
	_ = b.db.SetUserBanned(userID, true)

	cmu := &tgbotapi.ChatMemberUpdated{
		Chat: tgbotapi.Chat{ID: chatID},
		OldChatMember: tgbotapi.ChatMember{
			Status: "kicked",
		},
		NewChatMember: tgbotapi.ChatMember{
			User:      &tgbotapi.User{ID: userID},
			Status:    "restricted",
			UntilDate: time.Now().Add(5 * time.Minute).Unix(),
		},
	}

	// Should not panic, logs detection and schedules re-ban recheck
	b.handleChatMemberUpdate(cmu)
}

func TestSendTriggerBanAlert(t *testing.T) {
	b, cleanup := setupTestBot(t)
	defer cleanup()

	user, _, _ := b.db.GetOrCreateUser(998877, "spammer", "Spam", "User", 0)

	// Case 1: ModerationGroupID == 0 (Warning logged, no error)
	b.cfg.ModerationGroupID = 0
	if err := b.SendTriggerBanAlert(-1001, user, 42, "Chinese spam trigger"); err != nil {
		t.Errorf("expected no error when mod group is 0, got %v", err)
	}

	// Case 2: ModerationGroupID set
	b.cfg.ModerationGroupID = -100998877
	if err := b.SendTriggerBanAlert(-1001, user, 42, "Chinese spam trigger"); err != nil {
		t.Errorf("expected no error when sending trigger ban alert, got %v", err)
	}
}

func TestExecuteActions_BanUser(t *testing.T) {
	b, cleanup := setupTestBot(t)
	defer cleanup()

	b.cfg.ModerationGroupID = -100998877
	user, _, _ := b.db.GetOrCreateUser(112233, "badactor", "Bad", "Actor", 0)

	b.ExecuteActions(-1001, user, 101, []detector.Action{
		{Type: detector.ActionBanUser, Reason: "Detection trigger: High-ID Chinese Spam"},
	})

	u, err := b.db.GetUserByID(user.UserID)
	if err != nil {
		t.Fatalf("failed to fetch user: %v", err)
	}
	if !u.IsBanned {
		t.Errorf("expected user IsBanned to be true after ExecuteActions ActionBanUser")
	}
}
