package bot

import (
	"context"
	"testing"
	"time"

	"github.com/angch/gogcbot/pkg/db"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func TestParseCommand(t *testing.T) {
	tests := []struct {
		name     string
		msg      *tgbotapi.Message
		wantCmd  string
		wantArgs string
	}{
		{
			name: "Standard slash command with args",
			msg: &tgbotapi.Message{
				Text: "/warn @user123 test reason",
				Entities: []tgbotapi.MessageEntity{
					{Type: "bot_command", Offset: 0, Length: 5},
				},
			},
			wantCmd:  "warn",
			wantArgs: "@user123 test reason",
		},
		{
			name: "Exclamation mark command",
			msg: &tgbotapi.Message{
				Text: "!rep +10",
			},
			wantCmd:  "rep",
			wantArgs: "+10",
		},
		{
			name: "Uppercase command",
			msg: &tgbotapi.Message{
				Text: "/STATUS",
				Entities: []tgbotapi.MessageEntity{
					{Type: "bot_command", Offset: 0, Length: 7},
				},
			},
			wantCmd:  "status",
			wantArgs: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotCmd, gotArgs := parseCommand(tt.msg)
			if gotCmd != tt.wantCmd {
				t.Errorf("cmd = %q, want %q", gotCmd, tt.wantCmd)
			}
			if gotArgs != tt.wantArgs {
				t.Errorf("args = %q, want %q", gotArgs, tt.wantArgs)
			}
		})
	}
}

func TestParseRepArgs(t *testing.T) {
	tests := []struct {
		name         string
		args         string
		isReply      bool
		wantTarget   string
		wantAbsolute bool
		wantAbsVal   int
		wantHasDelta bool
		wantDeltaVal int
	}{
		{
			name:         "Relative negative delta with User ID",
			args:         "8841787542 -100",
			isReply:      false,
			wantTarget:   "8841787542",
			wantAbsolute: false,
			wantHasDelta: true,
			wantDeltaVal: -100,
		},
		{
			name:         "Absolute rep setting with User ID",
			args:         "8841787542 =0",
			isReply:      false,
			wantTarget:   "8841787542",
			wantAbsolute: true,
			wantAbsVal:   0,
			wantHasDelta: false,
		},
		{
			name:         "Absolute rep setting with username",
			args:         "@alice =50",
			isReply:      false,
			wantTarget:   "@alice",
			wantAbsolute: true,
			wantAbsVal:   50,
			wantHasDelta: false,
		},
		{
			name:         "Absolute rep setting for @pickfire =100",
			args:         "@pickfire =100",
			isReply:      false,
			wantTarget:   "@pickfire",
			wantAbsolute: true,
			wantAbsVal:   100,
			wantHasDelta: false,
		},
		{
			name:         "Absolute rep setting with spaces @pickfire = 100",
			args:         "@pickfire = 100",
			isReply:      false,
			wantTarget:   "@pickfire",
			wantAbsolute: true,
			wantAbsVal:   100,
			wantHasDelta: false,
		},
		{
			name:         "Reply with absolute rep setting",
			args:         "=0",
			isReply:      true,
			wantTarget:   "",
			wantAbsolute: true,
			wantAbsVal:   0,
			wantHasDelta: false,
		},
		{
			name:         "Reply with relative positive delta",
			args:         "+10",
			isReply:      true,
			wantTarget:   "",
			wantAbsolute: false,
			wantHasDelta: true,
			wantDeltaVal: 10,
		},
		{
			name:         "Query rep for user without value",
			args:         "8841787542",
			isReply:      false,
			wantTarget:   "8841787542",
			wantAbsolute: false,
			wantHasDelta: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotTarget, gotAbs, gotAbsVal, gotHasDelta, gotDeltaVal := parseRepArgs(tt.args, tt.isReply)
			if gotTarget != tt.wantTarget {
				t.Errorf("target = %q, want %q", gotTarget, tt.wantTarget)
			}
			if gotAbs != tt.wantAbsolute {
				t.Errorf("isAbsolute = %v, want %v", gotAbs, tt.wantAbsolute)
			}
			if gotAbsVal != tt.wantAbsVal {
				t.Errorf("absVal = %d, want %d", gotAbsVal, tt.wantAbsVal)
			}
			if gotHasDelta != tt.wantHasDelta {
				t.Errorf("hasDelta = %v, want %v", gotHasDelta, tt.wantHasDelta)
			}
			if gotDeltaVal != tt.wantDeltaVal {
				t.Errorf("deltaVal = %d, want %d", gotDeltaVal, tt.wantDeltaVal)
			}
		})
	}
}

func TestCmdGetDB_PrivateChat_SuperAdmin(t *testing.T) {
	b, cleanup := setupTestBot(t)
	defer cleanup()

	superAdminID := int64(111222)
	b.cfg.SuperAdminID = superAdminID

	user, _, err := b.db.GetOrCreateUser(superAdminID, "superadmin", "Super", "Admin", 100)
	if err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	msg := &tgbotapi.Message{
		MessageID: 1,
		From: &tgbotapi.User{
			ID:        superAdminID,
			UserName:  "superadmin",
			FirstName: "Super",
			LastName:  "Admin",
		},
		Chat: &tgbotapi.Chat{
			ID:   superAdminID,
			Type: "private",
		},
		Text: "/getdb",
		Entities: []tgbotapi.MessageEntity{
			{Type: "bot_command", Offset: 0, Length: 6},
		},
	}

	// Should execute cleanly without error
	b.handleCommand(msg, user)
}

func TestCmdGetDB_PrivateChat_BotAdmin(t *testing.T) {
	b, cleanup := setupTestBot(t)
	defer cleanup()

	botAdminID := int64(333444)
	user, _, err := b.db.GetOrCreateUser(botAdminID, "botadmin", "Bot", "Admin", 100)
	if err != nil {
		t.Fatalf("failed to create user: %v", err)
	}
	_ = b.db.SetUserAdmin(botAdminID, true)
	user.IsAdmin = true

	msg := &tgbotapi.Message{
		MessageID: 2,
		From: &tgbotapi.User{
			ID:        botAdminID,
			UserName:  "botadmin",
			FirstName: "Bot",
			LastName:  "Admin",
		},
		Chat: &tgbotapi.Chat{
			ID:   botAdminID,
			Type: "private",
		},
		Text: "/getdb",
		Entities: []tgbotapi.MessageEntity{
			{Type: "bot_command", Offset: 0, Length: 6},
		},
	}

	b.handleCommand(msg, user)
}

func TestCmdGetDB_PrivateChat_ModGroupMember(t *testing.T) {
	b, cleanup := setupTestBot(t)
	defer cleanup()

	modGroupID := int64(-100998877)
	b.cfg.ModerationGroupID = modGroupID

	modMemberID := int64(555666)
	user, _, err := b.db.GetOrCreateUser(modMemberID, "modmember", "Mod", "Member", 50)
	if err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	msg := &tgbotapi.Message{
		MessageID: 3,
		From: &tgbotapi.User{
			ID:        modMemberID,
			UserName:  "modmember",
			FirstName: "Mod",
			LastName:  "Member",
		},
		Chat: &tgbotapi.Chat{
			ID:   modMemberID,
			Type: "private",
		},
		Text: "/backup",
		Entities: []tgbotapi.MessageEntity{
			{Type: "bot_command", Offset: 0, Length: 7},
		},
	}

	b.handleCommand(msg, user)
}

func TestCmdGetDB_GroupChat_Rejected(t *testing.T) {
	b, cleanup := setupTestBot(t)
	defer cleanup()

	superAdminID := int64(111222)
	b.cfg.SuperAdminID = superAdminID

	user, _, err := b.db.GetOrCreateUser(superAdminID, "superadmin", "Super", "Admin", 100)
	if err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	// Executing in a supergroup chat
	msg := &tgbotapi.Message{
		MessageID: 4,
		From: &tgbotapi.User{
			ID:        superAdminID,
			UserName:  "superadmin",
			FirstName: "Super",
			LastName:  "Admin",
		},
		Chat: &tgbotapi.Chat{
			ID:    -100123456,
			Title: "Test Group",
			Type:  "supergroup",
		},
		Text: "/getdb",
		Entities: []tgbotapi.MessageEntity{
			{Type: "bot_command", Offset: 0, Length: 6},
		},
	}

	// Should reject sending db to group
	b.handleCommand(msg, user)
}

func TestCmdGetDB_PrivateChat_UnauthorizedUser(t *testing.T) {
	b, cleanup := setupTestBot(t)
	defer cleanup()

	b.cfg.SuperAdminID = 999999
	b.cfg.ModerationGroupID = 0

	regularUserID := int64(777888)
	user, _, err := b.db.GetOrCreateUser(regularUserID, "regular", "Regular", "User", 0)
	if err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	msg := &tgbotapi.Message{
		MessageID: 5,
		From: &tgbotapi.User{
			ID:        regularUserID,
			UserName:  "regular",
			FirstName: "Regular",
			LastName:  "User",
		},
		Chat: &tgbotapi.Chat{
			ID:   regularUserID,
			Type: "private",
		},
		Text: "/getdb",
		Entities: []tgbotapi.MessageEntity{
			{Type: "bot_command", Offset: 0, Length: 6},
		},
	}

	// Should be ignored as unauthorized
	b.handleCommand(msg, user)
}

func TestIsUserInModGroup(t *testing.T) {
	b, cleanup := setupTestBot(t)
	defer cleanup()

	// Mod group is 0
	b.cfg.ModerationGroupID = 0
	if b.IsUserInModGroup(12345) {
		t.Errorf("expected IsUserInModGroup to return false when ModerationGroupID is 0")
	}

	// Mod group is set, b.api is nil (test fallback returns member)
	b.cfg.ModerationGroupID = -100998877
	if !b.IsUserInModGroup(12345) {
		t.Errorf("expected IsUserInModGroup to return true when ModerationGroupID is set and member status is returned")
	}
}

func TestFetchUserProfile(t *testing.T) {
	b, cleanup := setupTestBot(t)
	defer cleanup()

	userID := int64(123456)
	_, _, err := b.db.GetOrCreateUser(userID, "testuser", "Test", "User", 100)
	if err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	profile, err := b.FetchUserProfile(userID)
	if err != nil {
		t.Fatalf("FetchUserProfile failed: %v", err)
	}

	if profile.UserID != userID {
		t.Errorf("expected profile UserID %d, got %d", userID, profile.UserID)
	}
	if profile.Bio == "" {
		t.Errorf("expected non-empty bio from test fallback")
	}

	// Check persisted in database
	dbProfile, err := b.db.GetUserProfile(userID)
	if err != nil {
		t.Fatalf("failed to get user profile from DB: %v", err)
	}
	if dbProfile.UserID != userID || dbProfile.Bio != profile.Bio {
		t.Errorf("DB profile mismatch: %+v", dbProfile)
	}
}

func TestFetchUserProfile_NotFound(t *testing.T) {
	b, cleanup := setupTestBot(t)
	defer cleanup()

	userID := int64(404)
	_, _, err := b.db.GetOrCreateUser(userID, "ghostuser", "Ghost", "User", 100)
	if err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	profile, err := b.FetchUserProfile(userID)
	if err == nil {
		t.Fatalf("expected FetchUserProfile to return error for not-found user, got nil")
	}
	if profile == nil {
		t.Fatalf("expected non-nil profile object returned even on error")
	}
	if !profile.NotFound {
		t.Errorf("expected profile.NotFound to be true")
	}

	// Verify it is saved in DB with NotFound = true
	dbProfile, err := b.db.GetUserProfile(userID)
	if err != nil {
		t.Fatalf("failed to get user profile from DB: %v", err)
	}
	if !dbProfile.NotFound {
		t.Errorf("expected DB profile.NotFound to be true")
	}
	if dbProfile.Username != "ghostuser" {
		t.Errorf("expected fallback username 'ghostuser', got %q", dbProfile.Username)
	}
}

func TestBackfillUserProfiles(t *testing.T) {
	b, cleanup := setupTestBot(t)
	defer cleanup()

	_, _, _ = b.db.GetOrCreateUser(1001, "u1", "User", "One", 100)
	_, _, _ = b.db.GetOrCreateUser(1002, "u2", "User", "Two", 100)

	ctx := context.Background()

	// 1. Initial backfill (missing only)
	success, failed, err := b.BackfillUserProfiles(ctx, 10*time.Millisecond, false, nil)
	if err != nil {
		t.Fatalf("BackfillUserProfiles failed: %v", err)
	}
	if success != 2 || failed != 0 {
		t.Errorf("expected 2 success, 0 failed, got %d success, %d failed", success, failed)
	}

	// 2. Second backfill (missing only) -> should find 0 users
	success2, failed2, err := b.BackfillUserProfiles(ctx, 10*time.Millisecond, false, nil)
	if err != nil {
		t.Fatalf("BackfillUserProfiles run 2 failed: %v", err)
	}
	if success2 != 0 || failed2 != 0 {
		t.Errorf("expected 0 success, 0 failed when none missing, got %d success, %d failed", success2, failed2)
	}

	// 3. Force backfill -> should re-fetch 2 users
	success3, failed3, err := b.BackfillUserProfiles(ctx, 10*time.Millisecond, true, nil)
	if err != nil {
		t.Fatalf("BackfillUserProfiles force run failed: %v", err)
	}
	if success3 != 2 || failed3 != 0 {
		t.Errorf("expected 2 success with force, got %d success, %d failed", success3, failed3)
	}
}

func TestBackfillUserProfiles_WithNotFoundUsers(t *testing.T) {
	b, cleanup := setupTestBot(t)
	defer cleanup()

	// User 1001 will succeed, User 404 will fail (not found)
	_, _, _ = b.db.GetOrCreateUser(1001, "u1", "User", "One", 100)
	_, _, _ = b.db.GetOrCreateUser(404, "ghost", "Ghost", "User", 100)

	ctx := context.Background()

	// 1. Initial backfill (missing only)
	success, failed, err := b.BackfillUserProfiles(ctx, 10*time.Millisecond, false, nil)
	if err != nil {
		t.Fatalf("BackfillUserProfiles failed: %v", err)
	}
	if success != 1 || failed != 1 {
		t.Errorf("expected 1 success, 1 failed, got %d success, %d failed", success, failed)
	}

	// Verify ghost user was marked as not found in DB
	ghostProfile, err := b.db.GetUserProfile(404)
	if err != nil {
		t.Fatalf("failed to query ghost user profile: %v", err)
	}
	if !ghostProfile.NotFound {
		t.Errorf("expected ghost user profile to have NotFound = true")
	}

	// 2. Second backfill (missing only) -> should find 0 users because 404 is recorded as not found in user_profiles
	success2, failed2, err := b.BackfillUserProfiles(ctx, 10*time.Millisecond, false, nil)
	if err != nil {
		t.Fatalf("BackfillUserProfiles run 2 failed: %v", err)
	}
	if success2 != 0 || failed2 != 0 {
		t.Errorf("expected 0 success, 0 failed on run 2 (not found user should NOT be retried), got %d success, %d failed", success2, failed2)
	}

	// 3. Force backfill -> should re-query both users (1 success, 1 failed)
	success3, failed3, err := b.BackfillUserProfiles(ctx, 10*time.Millisecond, true, nil)
	if err != nil {
		t.Fatalf("BackfillUserProfiles force run failed: %v", err)
	}
	if success3 != 1 || failed3 != 1 {
		t.Errorf("expected 1 success, 1 failed with force, got %d success, %d failed", success3, failed3)
	}
}

func TestCmdFetchProfile(t *testing.T) {
	b, cleanup := setupTestBot(t)
	defer cleanup()

	superAdminID := int64(111222)
	b.cfg.SuperAdminID = superAdminID

	user, _, _ := b.db.GetOrCreateUser(superAdminID, "superadmin", "Super", "Admin", 100)
	targetUser, _, _ := b.db.GetOrCreateUser(333444, "target", "Target", "User", 50)

	msg := &tgbotapi.Message{
		MessageID: 10,
		From: &tgbotapi.User{
			ID:        superAdminID,
			UserName:  "superadmin",
			FirstName: "Super",
			LastName:  "Admin",
		},
		Chat: &tgbotapi.Chat{
			ID:   superAdminID,
			Type: "private",
		},
		Text: "/fetchprofile 333444",
		Entities: []tgbotapi.MessageEntity{
			{Type: "bot_command", Offset: 0, Length: 13},
		},
	}

	b.handleCommand(msg, user)

	// Verify profile is saved
	p, err := b.db.GetUserProfile(targetUser.UserID)
	if err != nil {
		t.Fatalf("expected profile to be saved in DB: %v", err)
	}
	if p.UserID != targetUser.UserID {
		t.Errorf("expected profile user ID %d, got %d", targetUser.UserID, p.UserID)
	}
}

func TestCmdBackfillProfiles(t *testing.T) {
	b, cleanup := setupTestBot(t)
	defer cleanup()

	superAdminID := int64(111222)
	b.cfg.SuperAdminID = superAdminID

	user, _, _ := b.db.GetOrCreateUser(superAdminID, "superadmin", "Super", "Admin", 100)
	_, _, _ = b.db.GetOrCreateUser(555666, "u1", "User", "One", 50)

	msg := &tgbotapi.Message{
		MessageID: 20,
		From: &tgbotapi.User{
			ID:        superAdminID,
			UserName:  "superadmin",
			FirstName: "Super",
			LastName:  "Admin",
		},
		Chat: &tgbotapi.Chat{
			ID:   superAdminID,
			Type: "private",
		},
		Text: "/backfillprofiles",
		Entities: []tgbotapi.MessageEntity{
			{Type: "bot_command", Offset: 0, Length: 17},
		},
	}

	b.handleCommand(msg, user)
}

func TestCmdUserInfo_WithProfile(t *testing.T) {
	b, cleanup := setupTestBot(t)
	defer cleanup()

	superAdminID := int64(111222)
	b.cfg.SuperAdminID = superAdminID

	adminUser, _, _ := b.db.GetOrCreateUser(superAdminID, "superadmin", "Super", "Admin", 100)
	targetUser, _, _ := b.db.GetOrCreateUser(777888, "alice", "Alice", "Smith", 90)
	_ = targetUser

	// Save a profile for Alice
	_ = b.db.SaveUserProfile(&db.UserProfile{
		UserID:     777888,
		Username:   "alice",
		FirstName:  "Alice",
		LastName:   "Smith",
		Bio:        "Crypto enthusiast & designer",
		PhotoCount: 1,
		HasPhoto:   true,
		FetchedAt:  time.Now(),
	})

	msg := &tgbotapi.Message{
		MessageID: 30,
		From: &tgbotapi.User{
			ID:        superAdminID,
			UserName:  "superadmin",
			FirstName: "Super",
			LastName:  "Admin",
		},
		Chat: &tgbotapi.Chat{
			ID:   superAdminID,
			Type: "private",
		},
		Text: "/userinfo @alice",
		Entities: []tgbotapi.MessageEntity{
			{Type: "bot_command", Offset: 0, Length: 9},
		},
	}

	b.handleCommand(msg, adminUser)
}

func TestCmdFetchProfile_NotFound(t *testing.T) {
	b, cleanup := setupTestBot(t)
	defer cleanup()

	superAdminID := int64(111222)
	b.cfg.SuperAdminID = superAdminID

	adminUser, _, _ := b.db.GetOrCreateUser(superAdminID, "superadmin", "Super", "Admin", 100)
	targetUser, _, _ := b.db.GetOrCreateUser(999999, "ghost", "Ghost", "User", 50)
	_ = targetUser

	msg := &tgbotapi.Message{
		MessageID: 40,
		From: &tgbotapi.User{
			ID:        superAdminID,
			UserName:  "superadmin",
			FirstName: "Super",
			LastName:  "Admin",
		},
		Chat: &tgbotapi.Chat{
			ID:   superAdminID,
			Type: "private",
		},
		Text: "/fetchprofile 999999",
		Entities: []tgbotapi.MessageEntity{
			{Type: "bot_command", Offset: 0, Length: 13},
		},
	}

	b.handleCommand(msg, adminUser)

	// Verify profile in DB is marked NotFound
	p, err := b.db.GetUserProfile(999999)
	if err != nil {
		t.Fatalf("expected profile to be saved in DB: %v", err)
	}
	if !p.NotFound {
		t.Errorf("expected profile.NotFound to be true")
	}
}

func TestCmdUserInfo_WithNotFoundProfile(t *testing.T) {
	b, cleanup := setupTestBot(t)
	defer cleanup()

	superAdminID := int64(111222)
	b.cfg.SuperAdminID = superAdminID

	adminUser, _, _ := b.db.GetOrCreateUser(superAdminID, "superadmin", "Super", "Admin", 100)
	targetUser, _, _ := b.db.GetOrCreateUser(999, "notfounduser", "NotFound", "User", 90)
	_ = targetUser

	_ = b.db.SaveUserProfile(&db.UserProfile{
		UserID:    999,
		Username:  "notfounduser",
		NotFound:  true,
		FetchedAt: time.Now(),
	})

	msg := &tgbotapi.Message{
		MessageID: 50,
		From: &tgbotapi.User{
			ID:        superAdminID,
			UserName:  "superadmin",
			FirstName: "Super",
			LastName:  "Admin",
		},
		Chat: &tgbotapi.Chat{
			ID:   superAdminID,
			Type: "private",
		},
		Text: "/userinfo @notfounduser",
		Entities: []tgbotapi.MessageEntity{
			{Type: "bot_command", Offset: 0, Length: 9},
		},
	}

	b.handleCommand(msg, adminUser)
}

func TestCmdListSpamBios(t *testing.T) {
	b, cleanup := setupTestBot(t)
	defer cleanup()

	superAdminID := int64(111222)
	b.cfg.SuperAdminID = superAdminID

	adminUser, _, _ := b.db.GetOrCreateUser(superAdminID, "superadmin", "Super", "Admin", 100)

	// Create an unbanned user with exact spam bio
	targetUser, _, _ := b.db.GetOrCreateUser(888999, "spambot", "Spam", "Bot", 0)
	_ = b.db.SaveUserProfile(&db.UserProfile{
		UserID:     targetUser.UserID,
		Username:   "spambot",
		FirstName:  "Spam",
		LastName:   "Bot",
		Bio:        "锦鲤代发 @mmmmue 6折础油卡E卡、沃尔玛、永辉、携程。天猫、苹果礼品卡、Steam等 联系 @xgshenqing888",
		HasPhoto:   true,
		PhotoCount: 1,
		FetchedAt:  time.Now(),
	})

	msg := &tgbotapi.Message{
		MessageID: 60,
		From: &tgbotapi.User{
			ID:        superAdminID,
			UserName:  "superadmin",
			FirstName: "Super",
			LastName:  "Admin",
		},
		Chat: &tgbotapi.Chat{
			ID:   superAdminID,
			Type: "private",
		},
		Text: "/listspambios",
		Entities: []tgbotapi.MessageEntity{
			{Type: "bot_command", Offset: 0, Length: 13},
		},
	}

	b.handleCommand(msg, adminUser)

	// Test with filter keyword
	msgFilter := &tgbotapi.Message{
		MessageID: 61,
		From: &tgbotapi.User{
			ID:        superAdminID,
			UserName:  "superadmin",
			FirstName: "Super",
			LastName:  "Admin",
		},
		Chat: &tgbotapi.Chat{
			ID:   superAdminID,
			Type: "private",
		},
		Text: "/spambios 沃尔玛",
		Entities: []tgbotapi.MessageEntity{
			{Type: "bot_command", Offset: 0, Length: 9},
		},
	}

	b.handleCommand(msgFilter, adminUser)
}
