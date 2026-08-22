package bot

import (
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
