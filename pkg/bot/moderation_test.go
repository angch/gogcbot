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
	if err := b.SendTriggerBanAlert(-1001, user, 42, "CJK spam trigger"); err != nil {
		t.Errorf("expected no error when mod group is 0, got %v", err)
	}

	// Case 2: ModerationGroupID set
	b.cfg.ModerationGroupID = -100998877
	if err := b.SendTriggerBanAlert(-1001, user, 42, "CJK spam trigger"); err != nil {
		t.Errorf("expected no error when sending trigger ban alert, got %v", err)
	}
}

func TestExecuteActions_BanUser(t *testing.T) {
	b, cleanup := setupTestBot(t)
	defer cleanup()

	b.cfg.ModerationGroupID = -100998877
	user, _, _ := b.db.GetOrCreateUser(112233, "badactor", "Bad", "Actor", 0)

	b.ExecuteActions(-1001, user, 101, []detector.Action{
		{Type: detector.ActionBanUser, Reason: "Detection trigger: High-ID CJK Spam"},
	})

	u, err := b.db.GetUserByID(user.UserID)
	if err != nil {
		t.Fatalf("failed to fetch user: %v", err)
	}
	if !u.IsBanned {
		t.Errorf("expected user IsBanned to be true after ExecuteActions ActionBanUser")
	}
}

func TestSendFirstEmptyMessageInfo(t *testing.T) {
	b, cleanup := setupTestBot(t)
	defer cleanup()

	user, _, _ := b.db.GetOrCreateUser(334455, "emptyuser", "Empty", "User", 0)
	msg := &db.Message{
		ChatID:    -1001,
		MessageID: 10,
		UserID:    user.UserID,
		Text:      "",
		CreatedAt: time.Now(),
	}

	// Case 1: ModerationGroupID == 0 (Warning logged, no error)
	b.cfg.ModerationGroupID = 0
	if err := b.SendFirstEmptyMessageInfo(-1001, msg, user, "Test Group"); err != nil {
		t.Errorf("expected no error when mod group is 0, got %v", err)
	}

	// Case 2: ModerationGroupID set
	b.cfg.ModerationGroupID = -100998877
	if err := b.SendFirstEmptyMessageInfo(-1001, msg, user, "Test Group"); err != nil {
		t.Errorf("expected no error when sending first empty message info, got %v", err)
	}
}

func TestHandleMessage_FirstEmptyMessageNotFlagged(t *testing.T) {
	b, cleanup := setupTestBot(t)
	defer cleanup()

	b.cfg.ModerationGroupID = -100998877
	b.cfg.Reputation.DefaultInitial = 0
	b.cfg.Reputation.FlagThreshold = 40

	chatID := int64(-100123)
	userID := int64(778899)

	// Message 1: First message from this user, and it is empty
	emptyMsg := &tgbotapi.Message{
		MessageID: 100,
		From: &tgbotapi.User{
			ID:        userID,
			UserName:  "firstempty",
			FirstName: "First",
			LastName:  "Empty",
		},
		Chat: &tgbotapi.Chat{
			ID:    chatID,
			Title: "Monitored Channel",
			Type:  "supergroup",
		},
		Text: "",
		Date: int(time.Now().Unix()),
	}

	b.handleMessage(emptyMsg)

	// Ensure no flagged posts were created in DB
	pendingFlags, err := b.db.GetPendingFlagsCount()
	if err != nil {
		t.Fatalf("failed to get pending flags count: %v", err)
	}
	if pendingFlags != 0 {
		t.Errorf("expected 0 pending flags for first empty message, got %d", pendingFlags)
	}

	// Message 2: Second message from this user, also empty - should be flagged by low reputation rule
	emptyMsg2 := &tgbotapi.Message{
		MessageID: 101,
		From: &tgbotapi.User{
			ID:        userID,
			UserName:  "firstempty",
			FirstName: "First",
			LastName:  "Empty",
		},
		Chat: &tgbotapi.Chat{
			ID:    chatID,
			Title: "Monitored Channel",
			Type:  "supergroup",
		},
		Text: "",
		Date: int(time.Now().Unix()),
	}

	b.handleMessage(emptyMsg2)

	pendingFlagsAfter, err := b.db.GetPendingFlagsCount()
	if err != nil {
		t.Fatalf("failed to get pending flags count: %v", err)
	}
	if pendingFlagsAfter != 1 {
		t.Errorf("expected 1 pending flag for second empty message from low-rep user, got %d", pendingFlagsAfter)
	}
}

func TestHandleMessage_FirstNonEmptyMessageFlagged(t *testing.T) {
	b, cleanup := setupTestBot(t)
	defer cleanup()

	b.cfg.ModerationGroupID = -100998877
	b.cfg.Reputation.DefaultInitial = 0
	b.cfg.Reputation.FlagThreshold = 40

	chatID := int64(-100123)
	userID := int64(889900)

	// Message: First message from this user, but has content and user has low rep (0 <= 40)
	nonEmptyMsg := &tgbotapi.Message{
		MessageID: 200,
		From: &tgbotapi.User{
			ID:        userID,
			UserName:  "nonempty",
			FirstName: "Non",
			LastName:  "Empty",
		},
		Chat: &tgbotapi.Chat{
			ID:    chatID,
			Title: "Monitored Channel",
			Type:  "supergroup",
		},
		Text: "Hello world",
		Date: int(time.Now().Unix()),
	}

	b.handleMessage(nonEmptyMsg)

	pendingFlags, err := b.db.GetPendingFlagsCount()
	if err != nil {
		t.Fatalf("failed to get pending flags count: %v", err)
	}
	if pendingFlags != 1 {
		t.Errorf("expected 1 pending flag for first non-empty message with low rep, got %d", pendingFlags)
	}
}
