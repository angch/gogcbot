package bot

import (
	"os"
	"path/filepath"
	"strings"
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
	msg := &db.Message{
		ChatID:    -1001,
		MessageID: 42,
		UserID:    user.UserID,
		Text:      "Spam message content here",
		CreatedAt: time.Now(),
	}

	// Case 1: ModerationGroupID == 0 (Warning logged, no error)
	b.cfg.ModerationGroupID = 0
	if err := b.SendTriggerBanAlert(-1001, user, msg, "CJK spam trigger"); err != nil {
		t.Errorf("expected no error when mod group is 0, got %v", err)
	}

	// Case 2: ModerationGroupID set
	b.cfg.ModerationGroupID = -100998877
	if err := b.SendTriggerBanAlert(-1001, user, msg, "CJK spam trigger"); err != nil {
		t.Errorf("expected no error when sending trigger ban alert, got %v", err)
	}

	// Check flagged post audit record in DB
	fp, err := b.db.GetFlaggedPost(1)
	if err != nil {
		t.Fatalf("expected flagged post to be created in DB, got error: %v", err)
	}
	if fp.UserID != user.UserID || fp.Status != "banned" {
		t.Errorf("unexpected flagged post in DB: %+v", fp)
	}
}

func TestExecuteActions_BanUser(t *testing.T) {
	b, cleanup := setupTestBot(t)
	defer cleanup()

	b.cfg.ModerationGroupID = -100998877
	user, _, _ := b.db.GetOrCreateUser(112233, "badactor", "Bad", "Actor", 0)
	msg := &db.Message{
		ChatID:    -1001,
		MessageID: 101,
		UserID:    user.UserID,
		Text:      "Bad actor trigger message",
		CreatedAt: time.Now(),
	}

	b.ExecuteActions(-1001, user, msg, []detector.Action{
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

func TestHandleMessage_CJKSpamAfterFirstEmptyMessage_TriggersBan(t *testing.T) {
	b, cleanup := setupTestBot(t)
	defer cleanup()

	b.cfg.ModerationGroupID = -100998877
	b.cfg.Reputation.DefaultInitial = 0
	b.cfg.Reputation.FlagThreshold = 40
	b.cfg.Detector.Enabled = true
	b.cfg.Detector.NewUserCJK.Enabled = true
	b.cfg.Detector.NewUserCJK.MinHighUserID = 1000000000
	b.cfg.Detector.NewUserCJK.MaxReputation = 5
	b.cfg.Detector.NewUserCJK.MaxUserPosts = 5
	b.cfg.Detector.NewUserCJK.RepPenalty = 20

	det := detector.NewDetector(detector.NewNewUserCJKTrigger(b.cfg.Detector.NewUserCJK))
	b.detector = det

	chatID := int64(-1001072966891)
	userID := int64(6170094611)

	// Save group so it's monitored
	_ = b.db.SaveGroup(chatID, "Test Monitored Group", "supergroup")

	// Message 1: Empty message from new user
	emptyMsg := &tgbotapi.Message{
		MessageID: 64839,
		From: &tgbotapi.User{
			ID:        userID,
			UserName:  "",
			FirstName: "Kamsa",
			LastName:  "Jangid",
		},
		Chat: &tgbotapi.Chat{
			ID:    chatID,
			Title: "Test Monitored Group",
			Type:  "supergroup",
		},
		Text: "",
		Date: int(time.Now().Unix()),
	}
	b.handleMessage(emptyMsg)

	userAfterFirst, err := b.db.GetUserByID(userID)
	if err != nil {
		t.Fatalf("failed to get user: %v", err)
	}
	if userAfterFirst.IsBanned {
		t.Errorf("expected user not to be banned after first empty message")
	}
	if userAfterFirst.Reputation != 0 {
		t.Errorf("expected user rep to be 0 (no bump on empty message), got %d", userAfterFirst.Reputation)
	}

	// Message 2: Affiliate scam message with CJK characters
	spamMsg := &tgbotapi.Message{
		MessageID: 64841,
		From: &tgbotapi.User{
			ID:        userID,
			UserName:  "",
			FirstName: "Kamsa",
			LastName:  "Jangid",
		},
		Chat: &tgbotapi.Chat{
			ID:    chatID,
			Title: "Test Monitored Group",
			Type:  "supergroup",
		},
		Text: "油管联盟-fb联盟-外汇盘-币盘-商城盘-NFT盘-刷单盘-提供模特视频-可以挂自己地址和客服-联系; @ Ai16811",
		Date: int(time.Now().Unix()),
	}
	b.handleMessage(spamMsg)

	userAfterSpam, err := b.db.GetUserByID(userID)
	if err != nil {
		t.Fatalf("failed to get user after spam: %v", err)
	}
	if !userAfterSpam.IsBanned {
		t.Errorf("expected user to be banned after sending CJK spam message, but was not banned")
	}
	if userAfterSpam.Reputation != -20 {
		t.Errorf("expected user reputation to be -20, got %d", userAfterSpam.Reputation)
	}

	// Verify the spam message is recorded in DB with its actual text
	msgs, err := b.db.GetRecentUserMessages(userID, 5)
	if err != nil {
		t.Fatalf("failed to get user messages: %v", err)
	}
	if len(msgs) == 0 {
		t.Fatalf("expected at least 1 message in DB for user")
	}
	foundSpam := false
	for _, m := range msgs {
		if m.MessageID == 64841 && strings.Contains(m.Text, "油管联盟") {
			foundSpam = true
		}
	}
	if !foundSpam {
		t.Errorf("expected spam message with content '油管联盟...' to be saved in DB messages table")
	}

	// Verify trigger ban audit entry in flagged_posts
	goodUsers, badUsers, err := b.db.GetUserDirectoryReport(b.cfg.SuperAdminID)
	if err != nil {
		t.Fatalf("failed to get user directory report: %v", err)
	}
	_ = goodUsers
	var foundBad *db.UserReportItem
	for i := range badUsers {
		if badUsers[i].UserID == userID {
			foundBad = &badUsers[i]
			break
		}
	}
	if foundBad == nil {
		t.Fatalf("expected user %d in bad users report", userID)
	}
	if !foundBad.IsTriggerBan {
		t.Errorf("expected IsTriggerBan to be true, got %+v", foundBad)
	}
}

func TestExtractMessageText_MediaTypes(t *testing.T) {
	// Plain text
	m1 := &tgbotapi.Message{Text: "hello"}
	if extractMessageText(m1) != "hello" {
		t.Errorf("expected 'hello', got '%s'", extractMessageText(m1))
	}

	// Caption fallback
	m2 := &tgbotapi.Message{Caption: "photo caption"}
	if extractMessageText(m2) != "photo caption" {
		t.Errorf("expected 'photo caption', got '%s'", extractMessageText(m2))
	}

	// Photo without caption
	m3 := &tgbotapi.Message{Photo: []tgbotapi.PhotoSize{{FileID: "123"}}}
	if extractMessageText(m3) != "[Photo]" {
		t.Errorf("expected '[Photo]', got '%s'", extractMessageText(m3))
	}

	// Sticker with emoji
	m4 := &tgbotapi.Message{Sticker: &tgbotapi.Sticker{Emoji: "😀"}}
	if extractMessageText(m4) != "[Sticker 😀]" {
		t.Errorf("expected '[Sticker 😀]', got '%s'", extractMessageText(m4))
	}

	// Document with filename
	m5 := &tgbotapi.Message{Document: &tgbotapi.Document{FileName: "spam.pdf"}}
	if extractMessageText(m5) != "[Document: spam.pdf]" {
		t.Errorf("expected '[Document: spam.pdf]', got '%s'", extractMessageText(m5))
	}
}

func TestIsServiceMessage(t *testing.T) {
	joinMsg := &tgbotapi.Message{
		NewChatMembers: []tgbotapi.User{{ID: 12345}},
	}
	if !isServiceMessage(joinMsg) {
		t.Errorf("expected join message to be recognized as service message")
	}

	normalMsg := &tgbotapi.Message{
		Text: "regular message",
	}
	if isServiceMessage(normalMsg) {
		t.Errorf("expected normal text message not to be service message")
	}
}

func TestUTF8SanitizationAndTruncation(t *testing.T) {
	// 1. Truncating Chinese text should not split multi-byte characters
	cjkText := "锦鲤代发 @mmmmue 6折础油卡E卡、沃尔玛、永辉、携程。天猫、苹果礼品卡、Steam等"
	truncated := truncateText(cjkText, 10)
	if !strings.HasSuffix(truncated, "...") {
		t.Errorf("expected ellipsis suffix, got: %s", truncated)
	}
	if []rune(truncated)[10] != '.' {
		t.Errorf("expected truncation at exactly 10 runes, got %d runes", len([]rune(truncated)))
	}

	// 2. Escape Markdown on invalid UTF-8 byte sequences
	invalidByteString := "hello \xff\xfe world"
	escaped := escapeMarkdown(invalidByteString)
	if strings.Contains(escaped, "\xff") || strings.Contains(escaped, "\xfe") {
		t.Errorf("expected invalid UTF-8 bytes to be removed or replaced")
	}

	// 3. Chattable sanitization
	msgConfig := &tgbotapi.MessageConfig{
		Text: "Spam message with invalid \xed\xa0\x80 surrogate",
	}
	sanitizeChattable(msgConfig)
	if strings.Contains(msgConfig.Text, "\xed\xa0\x80") {
		t.Errorf("expected invalid surrogate sequence to be sanitized from message config")
	}
}
