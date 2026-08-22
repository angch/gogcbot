package bot

import (
	"context"
	"encoding/json"
	"fmt"
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

func TestHandleChatMemberUpdate_UserJoin_SpamBio(t *testing.T) {
	b, cleanup := setupTestBot(t)
	defer cleanup()

	userID := int64(888111)
	chatID := int64(-100123)

	b.cfg.ModerationGroupID = -100998877

	// Pre-seed mock user profile with spam bio so FetchUserProfile returns it
	_ = b.db.SaveUserProfile(&db.UserProfile{
		UserID:     userID,
		Username:   "spammer_join",
		FirstName:  "Spam",
		LastName:   "Joiner",
		Bio:        "锦鲤代发 @mmmmue 6折础油卡E卡、沃尔玛、永辉、携程。联系 @xgshenqing888",
		HasPhoto:   true,
		PhotoCount: 1,
		FetchedAt:  time.Now(),
	})

	cmu := &tgbotapi.ChatMemberUpdated{
		Chat: tgbotapi.Chat{
			ID:    chatID,
			Title: "Test Monitored Group",
			Type:  "supergroup",
		},
		OldChatMember: tgbotapi.ChatMember{
			Status: "left",
			User:   &tgbotapi.User{ID: userID, UserName: "spammer_join", FirstName: "Spam", LastName: "Joiner"},
		},
		NewChatMember: tgbotapi.ChatMember{
			Status: "member",
			User:   &tgbotapi.User{ID: userID, UserName: "spammer_join", FirstName: "Spam", LastName: "Joiner"},
		},
	}

	b.handleChatMemberUpdate(cmu)

	// User should be banned in DB and reputation adjusted
	u, err := b.db.GetUserByID(userID)
	if err != nil {
		t.Fatalf("failed to query user %d: %v", userID, err)
	}
	if !u.IsBanned {
		t.Errorf("expected user %d to be banned after joining with spam bio", userID)
	}
	if u.Reputation >= 0 {
		t.Errorf("expected user %d reputation to be penalized (< 0), got %d", userID, u.Reputation)
	}
}

func TestHandleMessage_NewChatMembers_SpamBio(t *testing.T) {
	b, cleanup := setupTestBot(t)
	defer cleanup()

	userID := int64(888222)
	chatID := int64(-100123)

	b.cfg.ModerationGroupID = -100998877

	_ = b.db.SaveUserProfile(&db.UserProfile{
		UserID:     userID,
		Username:   "spammer_join_msg",
		FirstName:  "Spam",
		LastName:   "Joiner2",
		Bio:        "招兼职 日赚300-500 沃尔玛 永辉 礼品卡联系",
		HasPhoto:   false,
		PhotoCount: 0,
		FetchedAt:  time.Now(),
	})

	joinMsg := &tgbotapi.Message{
		MessageID: 555,
		Chat: &tgbotapi.Chat{
			ID:    chatID,
			Title: "Test Monitored Group",
			Type:  "supergroup",
		},
		From: &tgbotapi.User{
			ID:        userID,
			UserName:  "spammer_join_msg",
			FirstName: "Spam",
			LastName:  "Joiner2",
		},
		NewChatMembers: []tgbotapi.User{
			{
				ID:        userID,
				UserName:  "spammer_join_msg",
				FirstName: "Spam",
				LastName:  "Joiner2",
			},
		},
	}

	b.handleMessage(joinMsg)

	u, err := b.db.GetUserByID(userID)
	if err != nil {
		t.Fatalf("failed to query user %d: %v", userID, err)
	}
	if !u.IsBanned {
		t.Errorf("expected user %d to be banned after joining via NewChatMembers with spam bio", userID)
	}
}

func TestHandleChatMemberUpdate_UserJoin_CleanBio(t *testing.T) {
	b, cleanup := setupTestBot(t)
	defer cleanup()

	userID := int64(777333)
	chatID := int64(-100123)

	b.cfg.ModerationGroupID = -100998877

	_ = b.db.SaveUserProfile(&db.UserProfile{
		UserID:     userID,
		Username:   "clean_joiner",
		FirstName:  "Clean",
		LastName:   "User",
		Bio:        "Hello everyone! I'm a software developer from Singapore.",
		HasPhoto:   true,
		PhotoCount: 1,
		FetchedAt:  time.Now(),
	})

	cmu := &tgbotapi.ChatMemberUpdated{
		Chat: tgbotapi.Chat{
			ID:    chatID,
			Title: "Test Monitored Group",
			Type:  "supergroup",
		},
		OldChatMember: tgbotapi.ChatMember{
			Status: "left",
			User:   &tgbotapi.User{ID: userID, UserName: "clean_joiner", FirstName: "Clean", LastName: "User"},
		},
		NewChatMember: tgbotapi.ChatMember{
			Status: "member",
			User:   &tgbotapi.User{ID: userID, UserName: "clean_joiner", FirstName: "Clean", LastName: "User"},
		},
	}

	b.handleChatMemberUpdate(cmu)

	u, err := b.db.GetUserByID(userID)
	if err != nil {
		t.Fatalf("failed to query user %d: %v", userID, err)
	}
	if u.IsBanned {
		t.Errorf("clean user %d should NOT be banned", userID)
	}
}

func TestHandleMessage_SpamBioMessage_TriggersBan(t *testing.T) {
	b, cleanup := setupTestBot(t)
	defer cleanup()

	userID := int64(888333)
	chatID := int64(-100123)

	b.cfg.ModerationGroupID = -100998877

	// Register detector triggers
	b.detector = detector.NewDetector(
		detector.NewNewUserSpamBioTrigger(b.cfg.Detector.NewUserSpamBio),
	)

	// Pre-seed profile with spam bio
	_ = b.db.SaveUserProfile(&db.UserProfile{
		UserID:     userID,
		Username:   "spammer_msg",
		FirstName:  "Spam",
		LastName:   "Poster",
		Bio:        "油卡 礼品卡 沃尔玛 6折联系",
		HasPhoto:   true,
		PhotoCount: 1,
		FetchedAt:  time.Now(),
	})

	msg := &tgbotapi.Message{
		MessageID: 100,
		Chat: &tgbotapi.Chat{
			ID:    chatID,
			Title: "Test Monitored Group",
			Type:  "supergroup",
		},
		From: &tgbotapi.User{
			ID:        userID,
			UserName:  "spammer_msg",
			FirstName: "Spam",
			LastName:  "Poster",
		},
		Text: "Hello world this is my first message",
	}

	b.handleMessage(msg)

	u, err := b.db.GetUserByID(userID)
	if err != nil {
		t.Fatalf("failed to query user %d: %v", userID, err)
	}
	if !u.IsBanned {
		t.Errorf("expected user %d to be banned after sending message with spam bio", userID)
	}
}

func TestHandleUserJoined_PersonalChannelSpam_TriggersBan(t *testing.T) {
	b, cleanup := setupTestBot(t)
	defer cleanup()

	userID := int64(888444)
	chatID := int64(-100123)

	b.cfg.ModerationGroupID = -100998877

	// Pre-seed profile where Bio is innocent, but PersonalChatTitle contains spam keyword
	_ = b.db.SaveUserProfile(&db.UserProfile{
		UserID:            userID,
		Username:          "channel_spammer",
		FirstName:         "Channel",
		LastName:          "Spam",
		Bio:               "Welcome to my channel!",
		PersonalChatTitle: "6折油卡代发专区",
		HasPhoto:          true,
		PhotoCount:        1,
		FetchedAt:         time.Now(),
	})

	cmu := &tgbotapi.ChatMemberUpdated{
		Chat: tgbotapi.Chat{
			ID:    chatID,
			Title: "Test Monitored Group",
			Type:  "supergroup",
		},
		OldChatMember: tgbotapi.ChatMember{
			Status: "left",
			User:   &tgbotapi.User{ID: userID, UserName: "channel_spammer", FirstName: "Channel", LastName: "Spam", LanguageCode: "zh-hans"},
		},
		NewChatMember: tgbotapi.ChatMember{
			Status: "member",
			User:   &tgbotapi.User{ID: userID, UserName: "channel_spammer", FirstName: "Channel", LastName: "Spam", LanguageCode: "zh-hans"},
		},
	}

	b.handleChatMemberUpdate(cmu)

	u, err := b.db.GetUserByID(userID)
	if err != nil {
		t.Fatalf("failed to query user %d: %v", userID, err)
	}
	if !u.IsBanned {
		t.Errorf("expected user %d to be banned on join due to spam personal channel title", userID)
	}
	if u.LanguageCode != "zh-hans" {
		t.Errorf("expected user language code 'zh-hans', got %q", u.LanguageCode)
	}
}

func TestHandleMessage_UserLanguageMetadataRecorded(t *testing.T) {
	b, cleanup := setupTestBot(t)
	defer cleanup()

	userID := int64(888555)
	chatID := int64(-100123)

	msg := &tgbotapi.Message{
		MessageID: 105,
		Chat: &tgbotapi.Chat{
			ID:    chatID,
			Title: "Test Monitored Group",
			Type:  "supergroup",
		},
		From: &tgbotapi.User{
			ID:           userID,
			UserName:     "lang_user",
			FirstName:    "Lang",
			LastName:     "User",
			LanguageCode: "zh-cn",
		},
		Text: "Hello general conversation",
	}

	b.handleMessage(msg)

	u, err := b.db.GetUserByID(userID)
	if err != nil {
		t.Fatalf("failed to query user %d: %v", userID, err)
	}
	if u.LanguageCode != "zh-cn" {
		t.Errorf("expected user language code 'zh-cn', got %q", u.LanguageCode)
	}
}

func TestNewBot_LoginRetry_Success(t *testing.T) {
	origFunc := newBotAPIFunc
	origDelay := loginRetryDelay
	origRetries := maxLoginRetries
	defer func() {
		newBotAPIFunc = origFunc
		loginRetryDelay = origDelay
		maxLoginRetries = origRetries
	}()

	loginRetryDelay = 1 * time.Millisecond
	maxLoginRetries = 4

	attempts := 0
	newBotAPIFunc = func(token string) (*tgbotapi.BotAPI, error) {
		attempts++
		if attempts < 3 {
			return nil, fmt.Errorf("HTTP 403 Forbidden: Telegram login denied (started up too soon)")
		}
		return &tgbotapi.BotAPI{
			Self: tgbotapi.User{ID: 123456, UserName: "testretrybot"},
		}, nil
	}

	cfg := config.DefaultConfig()
	cfg.TelegramToken = "mock_token"

	b, err := NewBot(&cfg, nil)
	if err != nil {
		t.Fatalf("expected NewBot to succeed after retry, got err: %v", err)
	}
	if attempts != 3 {
		t.Errorf("expected 3 attempts, got %d", attempts)
	}
	if b.BotUser().UserName != "testretrybot" {
		t.Errorf("expected bot username 'testretrybot', got %s", b.BotUser().UserName)
	}
}

func TestNewBot_LoginRetry_Exhausted(t *testing.T) {
	origFunc := newBotAPIFunc
	origDelay := loginRetryDelay
	origRetries := maxLoginRetries
	defer func() {
		newBotAPIFunc = origFunc
		loginRetryDelay = origDelay
		maxLoginRetries = origRetries
	}()

	loginRetryDelay = 1 * time.Millisecond
	maxLoginRetries = 3

	attempts := 0
	newBotAPIFunc = func(token string) (*tgbotapi.BotAPI, error) {
		attempts++
		return nil, fmt.Errorf("HTTP 401 Unauthorized: token denied")
	}

	cfg := config.DefaultConfig()
	cfg.TelegramToken = "mock_token"

	_, err := NewBot(&cfg, nil)
	if err == nil {
		t.Fatalf("expected NewBot to fail when all login attempts denied")
	}
	if attempts != 3 {
		t.Errorf("expected 3 attempts, got %d", attempts)
	}
	if !strings.Contains(err.Error(), "after 3 attempts") {
		t.Errorf("expected error message to mention 3 attempts, got: %v", err)
	}
}

func TestTelegramChatFullInfo_Unmarshal(t *testing.T) {
	rawJSON := `{
		"id": 99887766,
		"type": "private",
		"username": "spammer_showcase",
		"first_name": "Spam",
		"last_name": "ChannelOwner",
		"bio": "Check out my channel below",
		"has_private_forwards": true,
		"personal_chat": {
			"id": -1001999888,
			"title": "6折油卡代发专区",
			"username": "youkaspam_official",
			"type": "channel"
		},
		"business_intro": {
			"title": "Crypto Card Services",
			"message": "24/7 automated delivery"
		}
	}`

	var fullInfo TelegramChatFullInfo
	if err := json.Unmarshal([]byte(rawJSON), &fullInfo); err != nil {
		t.Fatalf("failed to unmarshal ChatFullInfo: %v", err)
	}

	if fullInfo.ID != 99887766 {
		t.Errorf("expected ID 99887766, got %d", fullInfo.ID)
	}
	if fullInfo.Bio != "Check out my channel below" {
		t.Errorf("expected bio 'Check out my channel below', got %q", fullInfo.Bio)
	}
	if !fullInfo.HasPrivateForwards {
		t.Errorf("expected has_private_forwards to be true")
	}
	if fullInfo.PersonalChat == nil {
		t.Fatalf("expected personal_chat to not be nil")
	}
	if fullInfo.PersonalChat.Title != "6折油卡代发专区" {
		t.Errorf("expected personal channel title '6折油卡代发专区', got %q", fullInfo.PersonalChat.Title)
	}
	if fullInfo.PersonalChat.Username != "youkaspam_official" {
		t.Errorf("expected personal channel username 'youkaspam_official', got %q", fullInfo.PersonalChat.Username)
	}
	if fullInfo.BusinessIntro == nil {
		t.Fatalf("expected business_intro to not be nil")
	}
	if fullInfo.BusinessIntro.Title != "Crypto Card Services" {
		t.Errorf("expected business intro title 'Crypto Card Services', got %q", fullInfo.BusinessIntro.Title)
	}
}

func TestHandleChatMemberUpdate_UserJoin_RedPacketCJKName_TriggersBan(t *testing.T) {
	b, cleanup := setupTestBot(t)
	defer cleanup()

	userID := int64(6890123456)
	chatID := int64(-100123)

	b.cfg.ModerationGroupID = -100998877

	cmu := &tgbotapi.ChatMemberUpdated{
		Chat: tgbotapi.Chat{
			ID:    chatID,
			Title: "Test Monitored Group",
			Type:  "supergroup",
		},
		OldChatMember: tgbotapi.ChatMember{
			Status: "left",
			User:   &tgbotapi.User{ID: userID, UserName: "cbzbQFLOuHNkJZ", FirstName: "全网最高扶持🧧", LastName: ""},
		},
		NewChatMember: tgbotapi.ChatMember{
			Status: "member",
			User:   &tgbotapi.User{ID: userID, UserName: "cbzbQFLOuHNkJZ", FirstName: "全网最高扶持🧧", LastName: ""},
		},
	}

	b.handleChatMemberUpdate(cmu)

	u, err := b.db.GetUserByID(userID)
	if err != nil {
		t.Fatalf("failed to query user %d: %v", userID, err)
	}
	if !u.IsBanned {
		t.Errorf("expected user %d to be banned after joining with red packet CJK name and mixed caps username", userID)
	}
	if u.Reputation >= 0 {
		t.Errorf("expected user %d reputation to be penalized (< 0), got %d", userID, u.Reputation)
	}
}

func TestHandleMessage_NewChatMembers_RedPacketCJKName_TriggersBan(t *testing.T) {
	b, cleanup := setupTestBot(t)
	defer cleanup()

	userID := int64(7890123456)
	chatID := int64(-100123)

	b.cfg.ModerationGroupID = -100998877

	joinMsg := &tgbotapi.Message{
		MessageID: 777,
		Chat: &tgbotapi.Chat{
			ID:    chatID,
			Title: "Test Monitored Group",
			Type:  "supergroup",
		},
		From: &tgbotapi.User{
			ID:        userID,
			UserName:  "aBcDeF",
			FirstName: "兼职",
			LastName:  "日结🧧",
		},
		NewChatMembers: []tgbotapi.User{
			{
				ID:        userID,
				UserName:  "aBcDeF",
				FirstName: "兼职",
				LastName:  "日结🧧",
			},
		},
	}

	b.handleMessage(joinMsg)

	u, err := b.db.GetUserByID(userID)
	if err != nil {
		t.Fatalf("failed to query user %d: %v", userID, err)
	}
	if !u.IsBanned {
		t.Errorf("expected user %d to be banned after joining via NewChatMembers with red packet CJK name", userID)
	}
}

func TestHandleMessage_RedPacketCJKName_TriggersBan(t *testing.T) {
	b, cleanup := setupTestBot(t)
	defer cleanup()

	userID := int64(8890123456)
	chatID := int64(-100123)

	b.cfg.ModerationGroupID = -100998877
	b.detector = detector.NewDetector(
		detector.NewRedPacketNameTrigger(b.cfg.Detector.RedPacketName),
	)

	msg := &tgbotapi.Message{
		MessageID: 888,
		Chat: &tgbotapi.Chat{
			ID:    chatID,
			Title: "Test Monitored Group",
			Type:  "supergroup",
		},
		From: &tgbotapi.User{
			ID:        userID,
			UserName:  "cbzbQFLOuHNkJZ",
			FirstName: "兼职日结🧧",
		},
		Text: "Hello general conversation",
	}

	b.handleMessage(msg)

	u, err := b.db.GetUserByID(userID)
	if err != nil {
		t.Fatalf("failed to query user %d: %v", userID, err)
	}
	if !u.IsBanned {
		t.Errorf("expected user %d to be banned after message evaluation with red packet CJK name", userID)
	}
}

func TestHandleChatMemberUpdate_ChannelJoin_SpamBio_TriggersBan(t *testing.T) {
	b, cleanup := setupTestBot(t)
	defer cleanup()

	userID := int64(888222)
	channelID := int64(-100987654321)

	b.cfg.ModerationGroupID = -100998877

	// Pre-seed mock user profile with spam bio so FetchUserProfile returns it
	_ = b.db.SaveUserProfile(&db.UserProfile{
		UserID:     userID,
		Username:   "channel_spammer_join",
		FirstName:  "ChannelSpam",
		LastName:   "Joiner",
		Bio:        "锦鲤代发 @mmmmue 6折础油卡E卡、沃尔玛、永辉、携程。联系 @xgshenqing888",
		HasPhoto:   true,
		PhotoCount: 1,
		FetchedAt:  time.Now(),
	})

	cmu := &tgbotapi.ChatMemberUpdated{
		Chat: tgbotapi.Chat{
			ID:    channelID,
			Title: "Test Monitored Broadcast Channel",
			Type:  "channel",
		},
		OldChatMember: tgbotapi.ChatMember{
			Status: "left",
			User:   &tgbotapi.User{ID: userID, UserName: "channel_spammer_join", FirstName: "ChannelSpam", LastName: "Joiner"},
		},
		NewChatMember: tgbotapi.ChatMember{
			Status: "member",
			User:   &tgbotapi.User{ID: userID, UserName: "channel_spammer_join", FirstName: "ChannelSpam", LastName: "Joiner"},
		},
	}

	b.handleUpdate(tgbotapi.Update{ChatMember: cmu})

	// Channel should be saved in DB
	grp, err := b.db.GetGroup(channelID)
	if err != nil || grp == nil {
		t.Errorf("expected channel %d to be saved in groups DB, got err: %v", channelID, err)
	} else if grp.Type != "channel" {
		t.Errorf("expected group type 'channel', got %q", grp.Type)
	}

	// User should be banned in DB and reputation adjusted
	u, err := b.db.GetUserByID(userID)
	if err != nil {
		t.Fatalf("failed to query user %d: %v", userID, err)
	}
	if !u.IsBanned {
		t.Errorf("expected user %d to be banned after joining channel with spam bio", userID)
	}
	if u.Reputation >= 0 {
		t.Errorf("expected user %d reputation to be penalized (< 0), got %d", userID, u.Reputation)
	}
}

func TestHandleChatMemberUpdate_ChannelJoin_RedPacketCJKName_TriggersBan(t *testing.T) {
	b, cleanup := setupTestBot(t)
	defer cleanup()

	userID := int64(8890998877)
	channelID := int64(-100987654322)

	b.cfg.ModerationGroupID = -100998877

	cmu := &tgbotapi.ChatMemberUpdated{
		Chat: tgbotapi.Chat{
			ID:    channelID,
			Title: "Test Monitored Announcement Channel",
			Type:  "channel",
		},
		OldChatMember: tgbotapi.ChatMember{
			Status: "left",
			User:   &tgbotapi.User{ID: userID, UserName: "cbzbQFLOuHNkJZ", FirstName: "全网首发🧧"},
		},
		NewChatMember: tgbotapi.ChatMember{
			Status: "member",
			User:   &tgbotapi.User{ID: userID, UserName: "cbzbQFLOuHNkJZ", FirstName: "全网首发🧧"},
		},
	}

	b.handleUpdate(tgbotapi.Update{ChatMember: cmu})

	u, err := b.db.GetUserByID(userID)
	if err != nil {
		t.Fatalf("failed to query user %d: %v", userID, err)
	}
	if !u.IsBanned {
		t.Errorf("expected user %d to be banned after joining channel with red packet CJK name", userID)
	}
}

func TestHandleChatMemberUpdate_ChannelJoin_CleanUser_Allowed(t *testing.T) {
	b, cleanup := setupTestBot(t)
	defer cleanup()

	userID := int64(334455)
	channelID := int64(-100987654323)

	b.cfg.ModerationGroupID = -100998877

	_ = b.db.SaveUserProfile(&db.UserProfile{
		UserID:     userID,
		Username:   "normal_subscriber",
		FirstName:  "Normal",
		LastName:   "User",
		Bio:        "Just a regular tech enthusiast and developer.",
		HasPhoto:   true,
		PhotoCount: 1,
		FetchedAt:  time.Now(),
	})

	cmu := &tgbotapi.ChatMemberUpdated{
		Chat: tgbotapi.Chat{
			ID:    channelID,
			Title: "Test Monitored Public Channel",
			Type:  "channel",
		},
		OldChatMember: tgbotapi.ChatMember{
			Status: "left",
			User:   &tgbotapi.User{ID: userID, UserName: "normal_subscriber", FirstName: "Normal", LastName: "User"},
		},
		NewChatMember: tgbotapi.ChatMember{
			Status: "member",
			User:   &tgbotapi.User{ID: userID, UserName: "normal_subscriber", FirstName: "Normal", LastName: "User"},
		},
	}

	b.handleUpdate(tgbotapi.Update{ChatMember: cmu})

	u, err := b.db.GetUserByID(userID)
	if err != nil {
		t.Fatalf("failed to query user %d: %v", userID, err)
	}
	if u.IsBanned {
		t.Errorf("expected normal user %d NOT to be banned after joining channel", userID)
	}
}

func TestHandleUpdate_MyChatMemberAndChatJoinRequest(t *testing.T) {
	b, cleanup := setupTestBot(t)
	defer cleanup()

	channelID := int64(-10011223344)

	// Test MyChatMember update
	mcm := &tgbotapi.ChatMemberUpdated{
		Chat: tgbotapi.Chat{
			ID:    channelID,
			Title: "New Bot Channel",
			Type:  "channel",
		},
		OldChatMember: tgbotapi.ChatMember{
			Status: "left",
		},
		NewChatMember: tgbotapi.ChatMember{
			Status: "administrator",
		},
	}
	b.handleUpdate(tgbotapi.Update{MyChatMember: mcm})

	grp, err := b.db.GetGroup(channelID)
	if err != nil || grp == nil {
		t.Errorf("expected channel %d to be saved on MyChatMember update, got %v", channelID, err)
	}

	// Test ChatJoinRequest update
	joinReqChatID := int64(-10055667788)
	cjr := &tgbotapi.ChatJoinRequest{
		Chat: tgbotapi.Chat{
			ID:    joinReqChatID,
			Title: "Private Request Channel",
			Type:  "channel",
		},
		From: tgbotapi.User{
			ID:        999111,
			UserName:  "applicant",
			FirstName: "App",
			LastName:  "Licant",
		},
	}
	b.handleUpdate(tgbotapi.Update{ChatJoinRequest: cjr})

	grp2, err := b.db.GetGroup(joinReqChatID)
	if err != nil || grp2 == nil {
		t.Errorf("expected channel %d to be saved on ChatJoinRequest update, got %v", joinReqChatID, err)
	}
}

func TestRescanLowRepUsers_SpamBio_TriggersBan(t *testing.T) {
	b, cleanup := setupTestBot(t)
	defer cleanup()

	userID := int64(777111)
	b.cfg.ModerationGroupID = -100998877

	// Create low rep user with old profile scan (30 hours ago)
	_, _, err := b.db.GetOrCreateUser(userID, "rescan_spammer", "Rescan", "Spammer", 5)
	if err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	_ = b.db.SaveUserProfile(&db.UserProfile{
		UserID:     userID,
		Username:   "rescan_spammer",
		FirstName:  "Rescan",
		LastName:   "Spammer",
		Bio:        "兼职代发 6折加油卡 沃尔玛卡，联系客服",
		HasPhoto:   true,
		PhotoCount: 1,
		FetchedAt:  time.Now().Add(-30 * time.Hour),
	})

	opts := RescanOptions{
		MaxReputation: 20,
		Hours:         24,
		Delay:         1 * time.Millisecond,
	}

	res, err := b.RescanLowRepUsers(context.Background(), opts, nil)
	if err != nil {
		t.Fatalf("rescan failed: %v", err)
	}

	if res.TotalCandidates != 1 || res.BannedCount != 1 {
		t.Errorf("expected 1 candidate and 1 ban, got %+v", res)
	}

	u, err := b.db.GetUserByID(userID)
	if err != nil || !u.IsBanned {
		t.Errorf("expected user %d to be banned after rescan matched spam bio, got u: %+v", userID, u)
	}
}

func TestRescanLowRepUsers_RedPacketCJKName_TriggersBan(t *testing.T) {
	b, cleanup := setupTestBot(t)
	defer cleanup()

	userID := int64(8890554433)
	b.cfg.ModerationGroupID = -100998877

	// User originally had clean name, but recently changed to red packet CJK name
	_, _, err := b.db.GetOrCreateUser(userID, "cbzbQFLOuHNkJZ", "NormalFirst", "NormalLast", 5)
	if err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	_ = b.db.SaveUserProfile(&db.UserProfile{
		UserID:     userID,
		Username:   "cbzbQFLOuHNkJZ",
		FirstName:  "每日首发🧧",
		LastName:   "",
		Bio:        "General bio",
		HasPhoto:   true,
		PhotoCount: 1,
		FetchedAt:  time.Now().Add(-48 * time.Hour),
	})

	opts := RescanOptions{
		MaxReputation: 20,
		Hours:         24,
		Delay:         1 * time.Millisecond,
	}

	res, err := b.RescanLowRepUsers(context.Background(), opts, nil)
	if err != nil {
		t.Fatalf("rescan failed: %v", err)
	}

	if res.TotalCandidates != 1 || res.BannedCount != 1 {
		t.Errorf("expected 1 candidate and 1 ban, got %+v", res)
	}

	u, err := b.db.GetUserByID(userID)
	if err != nil || !u.IsBanned {
		t.Errorf("expected user %d to be banned after rescan matched red packet CJK name", userID)
	}
}

func TestRescanLowRepUsers_CleanUser_NotBanned(t *testing.T) {
	b, cleanup := setupTestBot(t)
	defer cleanup()

	userID := int64(665544)
	b.cfg.ModerationGroupID = -100998877

	_, _, err := b.db.GetOrCreateUser(userID, "clean_dev", "Clean", "Developer", 10)
	if err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	_ = b.db.SaveUserProfile(&db.UserProfile{
		UserID:     userID,
		Username:   "clean_dev",
		FirstName:  "Clean",
		LastName:   "Developer",
		Bio:        "Just writing Go and Python code.",
		HasPhoto:   true,
		PhotoCount: 1,
		FetchedAt:  time.Now().Add(-50 * time.Hour),
	})

	opts := RescanOptions{
		MaxReputation: 20,
		Hours:         24,
		Delay:         1 * time.Millisecond,
	}

	res, err := b.RescanLowRepUsers(context.Background(), opts, nil)
	if err != nil {
		t.Fatalf("rescan failed: %v", err)
	}

	if res.TotalCandidates != 1 || res.CleanCount != 1 || res.BannedCount != 0 {
		t.Errorf("expected 1 clean user and 0 bans, got %+v", res)
	}

	u, err := b.db.GetUserByID(userID)
	if err != nil || u.IsBanned {
		t.Errorf("expected user %d NOT to be banned, got u: %+v", userID, u)
	}
}

func TestRescanLowRepUsers_TimeFilter_And_Force(t *testing.T) {
	b, cleanup := setupTestBot(t)
	defer cleanup()

	userID := int64(112233)
	b.cfg.ModerationGroupID = -100998877

	_, _, err := b.db.GetOrCreateUser(userID, "recent_scanned", "Recent", "User", 5)
	if err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	// Scanned only 2 hours ago
	_ = b.db.SaveUserProfile(&db.UserProfile{
		UserID:    userID,
		Username:  "recent_scanned",
		FirstName: "Recent",
		LastName:  "User",
		Bio:       "Clean bio",
		FetchedAt: time.Now().Add(-2 * time.Hour),
	})

	// 1. Without force (Hours: 24) -> 0 candidates
	opts := RescanOptions{
		MaxReputation: 20,
		Hours:         24,
		Delay:         1 * time.Millisecond,
	}
	res, err := b.RescanLowRepUsers(context.Background(), opts, nil)
	if err != nil {
		t.Fatalf("rescan failed: %v", err)
	}
	if res.TotalCandidates != 0 {
		t.Errorf("expected 0 candidates because scan is only 2h old, got %d", res.TotalCandidates)
	}

	// 2. With force -> 1 candidate
	optsForce := RescanOptions{
		MaxReputation: 20,
		Force:         true,
		Delay:         1 * time.Millisecond,
	}
	resForce, err := b.RescanLowRepUsers(context.Background(), optsForce, nil)
	if err != nil {
		t.Fatalf("rescan force failed: %v", err)
	}
	if resForce.TotalCandidates != 1 || resForce.CleanCount != 1 {
		t.Errorf("expected 1 candidate with force, got %+v", resForce)
	}
}

func TestRescanLowRepUsers_DryRun(t *testing.T) {
	b, cleanup := setupTestBot(t)
	defer cleanup()

	userID := int64(998877)
	b.cfg.ModerationGroupID = -100998877

	_, _, err := b.db.GetOrCreateUser(userID, "dryrun_spammer", "DryRun", "Spam", 5)
	if err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	_ = b.db.SaveUserProfile(&db.UserProfile{
		UserID:    userID,
		Username:  "dryrun_spammer",
		FirstName: "DryRun",
		LastName:  "Spam",
		Bio:       "兼职代发 6折加油卡 沃尔玛卡",
		FetchedAt: time.Now().Add(-30 * time.Hour),
	})

	opts := RescanOptions{
		MaxReputation: 20,
		Hours:         24,
		DryRun:        true,
		Delay:         1 * time.Millisecond,
	}

	res, err := b.RescanLowRepUsers(context.Background(), opts, nil)
	if err != nil {
		t.Fatalf("rescan dry run failed: %v", err)
	}

	if res.BannedCount != 1 {
		t.Errorf("expected BannedCount metric 1 in dry run, got %d", res.BannedCount)
	}

	// DB should NOT have banned the user because of dry-run
	u, err := b.db.GetUserByID(userID)
	if err != nil || u.IsBanned {
		t.Errorf("expected user %d NOT to be banned in DB during dry run, got u: %+v", userID, u)
	}
}

func TestCmdRescanUsers_TelegramCommand(t *testing.T) {
	b, cleanup := setupTestBot(t)
	defer cleanup()

	adminID := int64(12345)
	b.cfg.SuperAdminID = adminID
	b.cfg.ModerationGroupID = -100998877

	adminUser, _, _ := b.db.GetOrCreateUser(adminID, "admin", "Admin", "User", 100)

	msg := &tgbotapi.Message{
		MessageID: 101,
		Chat: &tgbotapi.Chat{
			ID:    b.cfg.ModerationGroupID,
			Title: "Mod Group",
			Type:  "supergroup",
		},
		From: &tgbotapi.User{
			ID:       adminID,
			UserName: "admin",
		},
		Text: "/rescanusers force dryrun",
	}

	// Should execute without panic or error
	b.handleCommand(msg, adminUser)
}

func TestSendPrivateMessageMirror_ModGroupIDZero(t *testing.T) {
	b, cleanup := setupTestBot(t)
	defer cleanup()

	b.cfg.ModerationGroupID = 0
	user, _, _ := b.db.GetOrCreateUser(123456, "pmuser", "PM", "User", 0)

	msg := &tgbotapi.Message{
		MessageID: 55,
		Chat: &tgbotapi.Chat{
			ID:   123456,
			Type: "private",
		},
		From: &tgbotapi.User{
			ID:        123456,
			UserName:  "pmuser",
			FirstName: "PM",
			LastName:  "User",
		},
		Text: "Hello bot in private",
		Date: int(time.Now().Unix()),
	}
	dbMsg := &db.Message{
		ChatID:    123456,
		MessageID: 55,
		UserID:    user.UserID,
		Text:      "Hello bot in private",
		CreatedAt: time.Now(),
	}

	// Should return nil without error when ModerationGroupID is 0
	if err := b.SendPrivateMessageMirror(msg, dbMsg, user); err != nil {
		t.Errorf("expected no error when ModerationGroupID is 0, got %v", err)
	}

	// Should return error if user is nil
	if err := b.SendPrivateMessageMirror(msg, dbMsg, nil); err == nil {
		t.Errorf("expected error when user is nil, got nil")
	}
}

func TestSendPrivateMessageMirror_Success(t *testing.T) {
	b, cleanup := setupTestBot(t)
	defer cleanup()

	b.cfg.ModerationGroupID = -100998877
	b.SetMockChatMemberFunc(func(config tgbotapi.GetChatMemberConfig) (tgbotapi.ChatMember, error) {
		return tgbotapi.ChatMember{Status: "left"}, nil
	})
	user, _, _ := b.db.GetOrCreateUser(234567, "tester", "Test", "User", 10)

	// Sub-test 1: Regular text message
	msg1 := &tgbotapi.Message{
		MessageID: 101,
		Chat: &tgbotapi.Chat{
			ID:   234567,
			Type: "private",
		},
		From: &tgbotapi.User{
			ID:        234567,
			UserName:  "tester",
			FirstName: "Test",
			LastName:  "User",
		},
		Text: "Can you help unban my account?",
		Date: int(time.Now().Unix()),
	}
	dbMsg1 := &db.Message{
		ChatID:    234567,
		MessageID: 101,
		UserID:    user.UserID,
		Text:      "Can you help unban my account?",
		CreatedAt: time.Now(),
	}
	if err := b.SendPrivateMessageMirror(msg1, dbMsg1, user); err != nil {
		t.Errorf("expected successful mirror for regular text, got %v", err)
	}

	// Sub-test 2: Edited message
	msg2 := &tgbotapi.Message{
		MessageID: 102,
		Chat: &tgbotapi.Chat{
			ID:   234567,
			Type: "private",
		},
		From: &tgbotapi.User{
			ID:        234567,
			UserName:  "tester",
			FirstName: "Test",
			LastName:  "User",
		},
		Text:     "Edited message content",
		Date:     int(time.Now().Unix()),
		EditDate: int(time.Now().Unix()),
	}
	dbMsg2 := &db.Message{
		ChatID:    234567,
		MessageID: 102,
		UserID:    user.UserID,
		Text:      "Edited message content",
		CreatedAt: time.Now(),
	}
	if err := b.SendPrivateMessageMirror(msg2, dbMsg2, user); err != nil {
		t.Errorf("expected successful mirror for edited message, got %v", err)
	}

	// Sub-test 3: Forwarded from user
	msg3 := &tgbotapi.Message{
		MessageID: 103,
		Chat: &tgbotapi.Chat{
			ID:   234567,
			Type: "private",
		},
		From: &tgbotapi.User{
			ID:        234567,
			UserName:  "tester",
			FirstName: "Test",
			LastName:  "User",
		},
		ForwardFrom: &tgbotapi.User{
			ID:        345678,
			UserName:  "origuser",
			FirstName: "Original",
			LastName:  "Author",
		},
		Text: "Forwarded scam message",
		Date: int(time.Now().Unix()),
	}
	if err := b.SendPrivateMessageMirror(msg3, nil, user); err != nil {
		t.Errorf("expected successful mirror for forwarded message from user, got %v", err)
	}

	// Sub-test 4: Forwarded from channel
	msg4 := &tgbotapi.Message{
		MessageID: 104,
		Chat: &tgbotapi.Chat{
			ID:   234567,
			Type: "private",
		},
		From: &tgbotapi.User{
			ID:        234567,
			UserName:  "tester",
			FirstName: "Test",
			LastName:  "User",
		},
		ForwardFromChat: &tgbotapi.Chat{
			ID:    -100223344,
			Title: "Crypto Signals Channel",
			Type:  "channel",
		},
		Text: "Join our signal channel now!",
		Date: int(time.Now().Unix()),
	}
	if err := b.SendPrivateMessageMirror(msg4, nil, user); err != nil {
		t.Errorf("expected successful mirror for forwarded message from channel, got %v", err)
	}

	// Sub-test 5: Forwarded with hidden sender name
	msg5 := &tgbotapi.Message{
		MessageID: 105,
		Chat: &tgbotapi.Chat{
			ID:   234567,
			Type: "private",
		},
		From: &tgbotapi.User{
			ID:        234567,
			UserName:  "tester",
			FirstName: "Test",
			LastName:  "User",
		},
		ForwardSenderName: "Anonymous Trader",
		Text:              "Secret tips inside",
		Date:              int(time.Now().Unix()),
	}
	if err := b.SendPrivateMessageMirror(msg5, nil, user); err != nil {
		t.Errorf("expected successful mirror for hidden sender forward, got %v", err)
	}

	// Sub-test 6: Media message (photo with no text)
	msg6 := &tgbotapi.Message{
		MessageID: 106,
		Chat: &tgbotapi.Chat{
			ID:   234567,
			Type: "private",
		},
		From: &tgbotapi.User{
			ID:        234567,
			UserName:  "tester",
			FirstName: "Test",
			LastName:  "User",
		},
		Photo: []tgbotapi.PhotoSize{
			{FileID: "photo123", Width: 100, Height: 100},
		},
		Date: int(time.Now().Unix()),
	}
	if err := b.SendPrivateMessageMirror(msg6, nil, user); err != nil {
		t.Errorf("expected successful mirror for photo message, got %v", err)
	}
}

func TestHandleMessage_PrivateMessage_LoggedAndMirrored(t *testing.T) {
	b, cleanup := setupTestBot(t)
	defer cleanup()

	b.cfg.ModerationGroupID = -100998877
	b.SetMockChatMemberFunc(func(config tgbotapi.GetChatMemberConfig) (tgbotapi.ChatMember, error) {
		return tgbotapi.ChatMember{Status: "left"}, nil
	})
	userID := int64(789012)

	msg := &tgbotapi.Message{
		MessageID: 201,
		Chat: &tgbotapi.Chat{
			ID:   userID,
			Type: "private",
		},
		From: &tgbotapi.User{
			ID:        userID,
			UserName:  "pm_sender",
			FirstName: "Private",
			LastName:  "Sender",
		},
		Text: "Hello bot, this is a private direct message.",
		Date: int(time.Now().Unix()),
	}

	// Execute message handler
	b.handleMessage(msg)

	// Verify user is created in database
	u, err := b.db.GetUserByID(userID)
	if err != nil || u == nil {
		t.Fatalf("expected user to be created in DB, got err: %v, u: %+v", err, u)
	}
	if u.Username != "pm_sender" {
		t.Errorf("expected username 'pm_sender', got %s", u.Username)
	}

	// Verify message is saved to database
	count, err := b.db.GetUserMessageCount(userID)
	if err != nil || count != 1 {
		t.Errorf("expected 1 logged message in DB, got count=%d, err=%v", count, err)
	}

	recent, err := b.db.GetRecentUserMessages(userID, 5)
	if err != nil || len(recent) != 1 {
		t.Fatalf("expected 1 recent message in DB, got %d, err=%v", len(recent), err)
	}
	if recent[0].Text != "Hello bot, this is a private direct message." {
		t.Errorf("unexpected message text in DB: %s", recent[0].Text)
	}
}

func TestHandleMessage_PrivateMessage_Command(t *testing.T) {
	b, cleanup := setupTestBot(t)
	defer cleanup()

	b.cfg.ModerationGroupID = -100998877
	b.SetMockChatMemberFunc(func(config tgbotapi.GetChatMemberConfig) (tgbotapi.ChatMember, error) {
		return tgbotapi.ChatMember{Status: "left"}, nil
	})
	userID := int64(654321)

	msg := &tgbotapi.Message{
		MessageID: 202,
		Chat: &tgbotapi.Chat{
			ID:   userID,
			Type: "private",
		},
		From: &tgbotapi.User{
			ID:        userID,
			UserName:  "cmd_sender",
			FirstName: "Cmd",
			LastName:  "Sender",
		},
		Text: "/help",
		Date: int(time.Now().Unix()),
		Entities: []tgbotapi.MessageEntity{
			{Type: "bot_command", Offset: 0, Length: 5},
		},
	}

	// Execute message handler with command in private chat
	b.handleMessage(msg)

	// Verify message was still logged into DB
	count, err := b.db.GetUserMessageCount(userID)
	if err != nil || count != 1 {
		t.Errorf("expected 1 logged command message in DB, got count=%d, err=%v", count, err)
	}
}

func TestHandleMessage_PrivateMessage_Edited(t *testing.T) {
	b, cleanup := setupTestBot(t)
	defer cleanup()

	b.cfg.ModerationGroupID = -100998877
	b.SetMockChatMemberFunc(func(config tgbotapi.GetChatMemberConfig) (tgbotapi.ChatMember, error) {
		return tgbotapi.ChatMember{Status: "left"}, nil
	})
	userID := int64(876543)

	msg := &tgbotapi.Message{
		MessageID: 203,
		Chat: &tgbotapi.Chat{
			ID:   userID,
			Type: "private",
		},
		From: &tgbotapi.User{
			ID:        userID,
			UserName:  "edit_sender",
			FirstName: "Edit",
			LastName:  "Sender",
		},
		Text:     "Edited PM text",
		Date:     int(time.Now().Unix()),
		EditDate: int(time.Now().Unix()),
	}

	b.handleUpdate(tgbotapi.Update{
		UpdateID:      99,
		EditedMessage: msg,
	})

	// Verify message was logged into DB
	count, err := b.db.GetUserMessageCount(userID)
	if err != nil || count != 1 {
		t.Errorf("expected 1 logged edited message in DB, got count=%d, err=%v", count, err)
	}
}

func TestHandleMessage_PrivateMessage_Media(t *testing.T) {
	b, cleanup := setupTestBot(t)
	defer cleanup()

	b.cfg.ModerationGroupID = -100998877
	b.SetMockChatMemberFunc(func(config tgbotapi.GetChatMemberConfig) (tgbotapi.ChatMember, error) {
		return tgbotapi.ChatMember{Status: "left"}, nil
	})
	userID := int64(987654)

	msg := &tgbotapi.Message{
		MessageID: 204,
		Chat: &tgbotapi.Chat{
			ID:   userID,
			Type: "private",
		},
		From: &tgbotapi.User{
			ID:        userID,
			UserName:  "media_sender",
			FirstName: "Media",
			LastName:  "Sender",
		},
		Voice: &tgbotapi.Voice{
			FileID:   "voice_file_123",
			Duration: 5,
		},
		Date: int(time.Now().Unix()),
	}

	b.handleMessage(msg)

	recent, err := b.db.GetRecentUserMessages(userID, 1)
	if err != nil || len(recent) != 1 {
		t.Fatalf("expected 1 recent message in DB, got %d, err=%v", len(recent), err)
	}
	if !recent[0].HasMedia {
		t.Errorf("expected HasMedia to be true for voice message")
	}
	if recent[0].Text != "[Voice Message]" {
		t.Errorf("expected text '[Voice Message]', got %q", recent[0].Text)
	}
}

func TestSendPrivateMessageMirror_KnownBotAdminSkipped(t *testing.T) {
	b, cleanup := setupTestBot(t)
	defer cleanup()

	b.cfg.ModerationGroupID = -100998877
	b.cfg.SuperAdminID = 11111

	// Case 1: Super Admin
	superAdmin, _, _ := b.db.GetOrCreateUser(11111, "superadmin", "Super", "Admin", 100)
	msg1 := &tgbotapi.Message{
		MessageID: 301,
		Chat:      &tgbotapi.Chat{ID: 11111, Type: "private"},
		From:      &tgbotapi.User{ID: 11111, UserName: "superadmin"},
		Text:      "Super admin private message",
		Date:      int(time.Now().Unix()),
	}
	if err := b.SendPrivateMessageMirror(msg1, nil, superAdmin); err != nil {
		t.Errorf("expected no error for super admin, got %v", err)
	}

	// Case 2: Promoted Bot Admin (IsAdmin == true)
	botAdmin, _, _ := b.db.GetOrCreateUser(22222, "botadmin", "Bot", "Admin", 100)
	_ = b.db.SetUserAdmin(22222, true)
	botAdmin.IsAdmin = true
	msg2 := &tgbotapi.Message{
		MessageID: 302,
		Chat:      &tgbotapi.Chat{ID: 22222, Type: "private"},
		From:      &tgbotapi.User{ID: 22222, UserName: "botadmin"},
		Text:      "Bot admin private message",
		Date:      int(time.Now().Unix()),
	}
	if err := b.SendPrivateMessageMirror(msg2, nil, botAdmin); err != nil {
		t.Errorf("expected no error for bot admin, got %v", err)
	}

	// Case 3: Mod Group Member
	modMember, _, _ := b.db.GetOrCreateUser(33333, "modmember", "Mod", "Member", 50)
	b.SetMockChatMemberFunc(func(config tgbotapi.GetChatMemberConfig) (tgbotapi.ChatMember, error) {
		if config.UserID == 33333 {
			return tgbotapi.ChatMember{Status: "member"}, nil
		}
		return tgbotapi.ChatMember{Status: "left"}, nil
	})
	msg3 := &tgbotapi.Message{
		MessageID: 303,
		Chat:      &tgbotapi.Chat{ID: 33333, Type: "private"},
		From:      &tgbotapi.User{ID: 33333, UserName: "modmember"},
		Text:      "Mod member private message",
		Date:      int(time.Now().Unix()),
	}
	if err := b.SendPrivateMessageMirror(msg3, nil, modMember); err != nil {
		t.Errorf("expected no error for mod member, got %v", err)
	}

	// Case 4: Regular non-admin user (should be mirrored)
	regularUser, _, _ := b.db.GetOrCreateUser(44444, "regular", "Regular", "User", 10)
	msg4 := &tgbotapi.Message{
		MessageID: 304,
		Chat:      &tgbotapi.Chat{ID: 44444, Type: "private"},
		From:      &tgbotapi.User{ID: 44444, UserName: "regular"},
		Text:      "Regular user private message",
		Date:      int(time.Now().Unix()),
	}
	if err := b.SendPrivateMessageMirror(msg4, nil, regularUser); err != nil {
		t.Errorf("expected no error for regular user, got %v", err)
	}
}

func TestHandleMessage_PrivateMessage_BotAdmin_SkippedFromMirroring(t *testing.T) {
	b, cleanup := setupTestBot(t)
	defer cleanup()

	b.cfg.ModerationGroupID = -100998877
	b.cfg.SuperAdminID = 55555

	adminUser, _, _ := b.db.GetOrCreateUser(55555, "superadmin", "Super", "Admin", 100)

	msg := &tgbotapi.Message{
		MessageID: 401,
		Chat: &tgbotapi.Chat{
			ID:   55555,
			Type: "private",
		},
		From: &tgbotapi.User{
			ID:        55555,
			UserName:  "superadmin",
			FirstName: "Super",
			LastName:  "Admin",
		},
		Text: "Admin private note",
		Date: int(time.Now().Unix()),
	}

	b.handleMessage(msg)

	// Verify the message was still logged into DB
	count, err := b.db.GetUserMessageCount(adminUser.UserID)
	if err != nil || count != 1 {
		t.Errorf("expected admin message to be logged in DB, got count=%d, err=%v", count, err)
	}
}

func TestCheckBannedUsersAcrossGroups_AlreadyBanned(t *testing.T) {
	b, cleanup := setupTestBot(t)
	defer cleanup()

	_ = b.db.SaveGroup(-100111222, "Test Group", "supergroup")
	userID := int64(99001)
	_, _, _ = b.db.GetOrCreateUser(userID, "banned_guy", "Banned", "Guy", -50)
	_ = b.db.SetUserBanned(userID, true)

	b.SetMockChatMemberFunc(func(config tgbotapi.GetChatMemberConfig) (tgbotapi.ChatMember, error) {
		return tgbotapi.ChatMember{
			Status:    "kicked",
			UntilDate: 0,
			User:      &tgbotapi.User{ID: userID, UserName: "banned_guy"},
		}, nil
	})

	opts := BanCheckOptions{
		Delay: 10 * time.Millisecond,
	}

	res, err := b.CheckBannedUsersAcrossGroups(context.Background(), opts, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if res.TotalBannedUsers != 1 || res.TotalGroups != 1 || res.TotalChecks != 1 {
		t.Errorf("unexpected counts: %+v", res)
	}
	if res.AlreadyBanned != 1 {
		t.Errorf("expected 1 already banned, got %d", res.AlreadyBanned)
	}
	if res.RebannedCount != 0 {
		t.Errorf("expected 0 rebanned, got %d", res.RebannedCount)
	}
}

func TestCheckBannedUsersAcrossGroups_MissingBan_Rebanned(t *testing.T) {
	b, cleanup := setupTestBot(t)
	defer cleanup()

	_ = b.db.SaveGroup(-100111222, "Test Group", "supergroup")
	userID := int64(99002)
	_, _, _ = b.db.GetOrCreateUser(userID, "unbanned_guy", "Unbanned", "Guy", -50)
	_ = b.db.SetUserBanned(userID, true)

	b.SetMockChatMemberFunc(func(config tgbotapi.GetChatMemberConfig) (tgbotapi.ChatMember, error) {
		return tgbotapi.ChatMember{
			Status: "left",
			User:   &tgbotapi.User{ID: userID, UserName: "unbanned_guy"},
		}, nil
	})

	opts := BanCheckOptions{
		Delay: 10 * time.Millisecond,
	}

	res, err := b.CheckBannedUsersAcrossGroups(context.Background(), opts, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if res.AlreadyBanned != 0 {
		t.Errorf("expected 0 already banned, got %d", res.AlreadyBanned)
	}
	if res.RebannedCount != 1 {
		t.Errorf("expected 1 rebanned, got %d", res.RebannedCount)
	}
}

func TestCheckBannedUsersAcrossGroups_DryRun(t *testing.T) {
	b, cleanup := setupTestBot(t)
	defer cleanup()

	_ = b.db.SaveGroup(-100111222, "Test Group", "supergroup")
	userID := int64(99003)
	_, _, _ = b.db.GetOrCreateUser(userID, "dry_guy", "Dry", "Guy", -50)
	_ = b.db.SetUserBanned(userID, true)

	b.SetMockChatMemberFunc(func(config tgbotapi.GetChatMemberConfig) (tgbotapi.ChatMember, error) {
		return tgbotapi.ChatMember{
			Status: "member",
			User:   &tgbotapi.User{ID: userID, UserName: "dry_guy"},
		}, nil
	})

	opts := BanCheckOptions{
		Delay:  10 * time.Millisecond,
		DryRun: true,
	}

	res, err := b.CheckBannedUsersAcrossGroups(context.Background(), opts, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if res.RebannedCount != 1 {
		t.Errorf("expected 1 reported missing ban in dry run, got %d", res.RebannedCount)
	}
	if res.AlreadyBanned != 0 {
		t.Errorf("expected 0 already banned, got %d", res.AlreadyBanned)
	}
}

func TestCheckBannedUsersAcrossGroups_SafetySkipAdmin(t *testing.T) {
	b, cleanup := setupTestBot(t)
	defer cleanup()

	_ = b.db.SaveGroup(-100111222, "Test Group", "supergroup")
	superAdminID := int64(99004)
	b.cfg.SuperAdminID = superAdminID
	_, _, _ = b.db.GetOrCreateUser(superAdminID, "admin_guy", "Admin", "Guy", 100)
	_ = b.db.SetUserBanned(superAdminID, true)

	opts := BanCheckOptions{
		Delay: 10 * time.Millisecond,
	}

	res, err := b.CheckBannedUsersAcrossGroups(context.Background(), opts, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if res.SkippedCount != 1 {
		t.Errorf("expected 1 skipped admin user, got %d", res.SkippedCount)
	}
	if res.TotalChecks != 0 {
		t.Errorf("expected 0 checks performed on admin, got %d", res.TotalChecks)
	}
}

func TestCheckBannedUsersAcrossGroups_ErrorHandling(t *testing.T) {
	b, cleanup := setupTestBot(t)
	defer cleanup()

	_ = b.db.SaveGroup(-100111222, "Test Group", "supergroup")
	userID := int64(99005)
	_, _, _ = b.db.GetOrCreateUser(userID, "err_guy", "Err", "Guy", -50)
	_ = b.db.SetUserBanned(userID, true)

	b.SetMockChatMemberFunc(func(config tgbotapi.GetChatMemberConfig) (tgbotapi.ChatMember, error) {
		return tgbotapi.ChatMember{}, fmt.Errorf("Bad Request: chat not found")
	})

	opts := BanCheckOptions{
		Delay: 10 * time.Millisecond,
	}

	res, err := b.CheckBannedUsersAcrossGroups(context.Background(), opts, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if res.ErrorCount != 1 {
		t.Errorf("expected 1 error count, got %d", res.ErrorCount)
	}
}

func TestTryStartBanCheck_Concurrency(t *testing.T) {
	b, cleanup := setupTestBot(t)
	defer cleanup()

	if !b.TryStartBanCheck() {
		t.Fatalf("expected TryStartBanCheck to succeed first time")
	}

	// Second attempt should fail
	if b.TryStartBanCheck() {
		t.Fatalf("expected TryStartBanCheck to fail while already running")
	}

	b.FinishBanCheck()

	// Should succeed after finish
	if !b.TryStartBanCheck() {
		t.Fatalf("expected TryStartBanCheck to succeed after FinishBanCheck")
	}
	b.FinishBanCheck()
}

func TestCmdBanCheck_TelegramCommand(t *testing.T) {
	b, cleanup := setupTestBot(t)
	defer cleanup()

	adminID := int64(12345)
	b.cfg.SuperAdminID = adminID
	_ = b.db.SaveGroup(-100111222, "Test Group", "supergroup")

	userID := int64(99006)
	_, _, _ = b.db.GetOrCreateUser(userID, "banned_check_user", "Banned", "User", -50)
	_ = b.db.SetUserBanned(userID, true)

	b.SetMockChatMemberFunc(func(config tgbotapi.GetChatMemberConfig) (tgbotapi.ChatMember, error) {
		return tgbotapi.ChatMember{
			Status: "kicked",
			User:   &tgbotapi.User{ID: userID, UserName: "banned_check_user"},
		}, nil
	})

	msg := &tgbotapi.Message{
		MessageID: 501,
		Chat:      &tgbotapi.Chat{ID: adminID, Type: "private"},
		From:      &tgbotapi.User{ID: adminID, UserName: "admin"},
		Text:      "/bancheck dryrun delay 5",
		Date:      int(time.Now().Unix()),
	}

	// Execute command
	b.cmdBanCheck(msg, "dryrun delay 5", true)

	// Wait briefly for background goroutine to execute
	time.Sleep(100 * time.Millisecond)

	// Verify ban check completed and reset running flag
	if !b.TryStartBanCheck() {
		t.Errorf("expected ban check to have finished and released lock")
	}
	b.FinishBanCheck()
}

func TestCmdBanCheck_PermissionDenied(t *testing.T) {
	b, cleanup := setupTestBot(t)
	defer cleanup()

	msg := &tgbotapi.Message{
		MessageID: 502,
		Chat:      &tgbotapi.Chat{ID: 99999, Type: "private"},
		From:      &tgbotapi.User{ID: 99999, UserName: "normal_user"},
		Text:      "/bancheck",
		Date:      int(time.Now().Unix()),
	}

	b.cmdBanCheck(msg, "", false)

	// Should not have acquired lock
	if !b.TryStartBanCheck() {
		t.Errorf("expected lock not to be held")
	}
	b.FinishBanCheck()
}



