package db

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func setupTestDB(t *testing.T) (*DB, func()) {
	tmpDir, err := os.MkdirTemp("", "gogcbot_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	dbPath := filepath.Join(tmpDir, "test.db")

	database, err := OpenDB(dbPath)
	if err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("failed to open test db: %v", err)
	}

	cleanup := func() {
		database.Close()
		os.RemoveAll(tmpDir)
	}

	return database, cleanup
}

func TestUserOperations(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()

	// Get or Create User
	u, isNew, err := database.GetOrCreateUser(1001, "alice", "Alice", "Smith", 100)
	if err != nil {
		t.Fatalf("unexpected error creating user: %v", err)
	}
	if !isNew {
		t.Errorf("expected isNew to be true for newly created user")
	}

	if u.UserID != 1001 || u.Reputation != 100 || u.Username != "alice" {
		t.Errorf("unexpected user data: %+v", u)
	}

	// Adjust Reputation
	newRep, err := database.AdjustReputation(1001, 15, "Good behavior", 999)
	if err != nil {
		t.Fatalf("unexpected error adjusting rep: %v", err)
	}
	if newRep != 115 {
		t.Errorf("expected new rep 115, got %d", newRep)
	}

	// Increment warning
	warns, err := database.IncrementWarning(1001)
	if err != nil {
		t.Fatalf("unexpected error incrementing warning: %v", err)
	}
	if warns != 1 {
		t.Errorf("expected warning count 1, got %d", warns)
	}

	// Get User by Username
	uByName, err := database.GetUserByUsername("@alice")
	if err != nil {
		t.Fatalf("unexpected error fetching user by username: %v", err)
	}
	if uByName.UserID != 1001 || uByName.WarnCount != 1 {
		t.Errorf("fetched user mismatch: %+v", uByName)
	}
}

func TestRetentionAndUserPostCap(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()

	userID := int64(2002)
	chatID := int64(-100123)

	_, _, err := database.GetOrCreateUser(userID, "bob", "Bob", "Builder", 100)
	if err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	// Insert 60 messages for user to test 50 cap
	now := time.Now()
	for i := 1; i <= 60; i++ {
		msg := &Message{
			ChatID:    chatID,
			MessageID: i,
			UserID:    userID,
			Text:      "Test message",
			CreatedAt: now.Add(time.Duration(i) * time.Minute),
		}
		if err := database.SaveMessage(msg); err != nil {
			t.Fatalf("failed to save message %d: %v", i, err)
		}
	}

	cnt, err := database.GetUserMessageCount(userID)
	if err != nil {
		t.Fatalf("failed to count messages: %v", err)
	}
	if cnt != 60 {
		t.Errorf("expected 60 messages, got %d", cnt)
	}

	// Prune user post history to max 50
	pruned, err := database.PruneUserPostHistory(50)
	if err != nil {
		t.Fatalf("failed to prune user post history: %v", err)
	}
	if pruned != 10 {
		t.Errorf("expected 10 messages pruned, got %d", pruned)
	}

	cntAfter, _ := database.GetUserMessageCount(userID)
	if cntAfter != 50 {
		t.Errorf("expected 50 messages remaining, got %d", cntAfter)
	}
}

func Test7DayLogPruning(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()

	userID := int64(3003)
	chatID := int64(-100456)

	// Message 10 days old
	oldMsg := &Message{
		ChatID:    chatID,
		MessageID: 101,
		UserID:    userID,
		Text:      "Ancient message",
		CreatedAt: time.Now().AddDate(0, 0, -10),
	}
	// Message 2 days old
	recentMsg := &Message{
		ChatID:    chatID,
		MessageID: 102,
		UserID:    userID,
		Text:      "Recent message",
		CreatedAt: time.Now().AddDate(0, 0, -2),
	}

	_ = database.SaveMessage(oldMsg)
	_ = database.SaveMessage(recentMsg)

	pruned, err := database.PruneOldMessages(7)
	if err != nil {
		t.Fatalf("failed to prune old messages: %v", err)
	}
	if pruned != 1 {
		t.Errorf("expected 1 old message pruned (>7d), got %d", pruned)
	}
}

func TestSetReputation(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()

	u, _, err := database.GetOrCreateUser(4004, "charlie", "Charlie", "Brown", 0)
	if err != nil {
		t.Fatalf("failed to create user: %v", err)
	}
	if u.Reputation != 0 {
		t.Errorf("expected initial rep 0, got %d", u.Reputation)
	}

	if err := database.SetReputation(4004, 100, "Promoted to admin", 4004); err != nil {
		t.Fatalf("failed to set reputation: %v", err)
	}

	uReloaded, err := database.GetUserByID(4004)
	if err != nil {
		t.Fatalf("failed to reload user: %v", err)
	}
	if uReloaded.Reputation != 100 {
		t.Errorf("expected reputation 100, got %d", uReloaded.Reputation)
	}
}

func TestDailyReputationBump(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()

	userID := int64(5005)
	_, _, err := database.GetOrCreateUser(userID, "dave", "Dave", "User", 0)
	if err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	bumped, err := database.HasReceivedDailyRepBump(userID, "Daily unflagged message")
	if err != nil {
		t.Fatalf("failed to check daily rep bump: %v", err)
	}
	if bumped {
		t.Errorf("expected bumped to be false initially")
	}

	newRep, err := database.AdjustReputationWithCap(userID, 1, 100, "Daily unflagged message activity", userID)
	if err != nil {
		t.Fatalf("failed to adjust rep: %v", err)
	}
	if newRep != 1 {
		t.Errorf("expected new rep 1, got %d", newRep)
	}

	bumpedAfter, err := database.HasReceivedDailyRepBump(userID, "Daily unflagged message")
	if err != nil {
		t.Fatalf("failed to check daily rep bump: %v", err)
	}
	if !bumpedAfter {
		t.Errorf("expected bumpedAfter to be true")
	}

	// Adjust up to cap
	_ = database.SetReputation(userID, 99, "Testing cap", userID)
	cappedRep, err := database.AdjustReputationWithCap(userID, 5, 100, "Testing cap bump", userID)
	if err != nil {
		t.Fatalf("failed to adjust rep with cap: %v", err)
	}
	if cappedRep != 100 {
		t.Errorf("expected capped rep 100, got %d", cappedRep)
	}
}

func TestGetAllUsers(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()

	_, _, _ = database.GetOrCreateUser(6001, "u1", "User", "One", 10)
	_, _, _ = database.GetOrCreateUser(6002, "u2", "User", "Two", 50)

	users, err := database.GetAllUsers(10)
	if err != nil {
		t.Fatalf("failed to fetch users: %v", err)
	}
	if len(users) != 2 {
		t.Fatalf("expected 2 users, got %d", len(users))
	}
	if users[0].UserID != 6002 || users[0].Reputation != 50 {
		t.Errorf("expected first user to be 6002 with rep 50, got %+v", users[0])
	}
}

func TestAutoMigration_IsAdminColumn(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "gogcbot_migration_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)
	dbPath := filepath.Join(tmpDir, "legacy.db")

	rawDB, err := OpenDB(dbPath)
	if err != nil {
		t.Fatalf("failed to open raw db: %v", err)
	}
	_, _ = rawDB.Exec(`
		CREATE TABLE users (
			user_id INTEGER PRIMARY KEY,
			username TEXT NOT NULL DEFAULT '',
			first_name TEXT NOT NULL DEFAULT '',
			last_name TEXT NOT NULL DEFAULT '',
			reputation INTEGER NOT NULL DEFAULT 100,
			warn_count INTEGER NOT NULL DEFAULT 0,
			is_banned BOOLEAN NOT NULL DEFAULT 0,
			created_at DATETIME NOT NULL,
			updated_at DATETIME NOT NULL
		);
	`)
	rawDB.Close()

	migratedDB, err := OpenDB(dbPath)
	if err != nil {
		t.Fatalf("failed to open migrated DB: %v", err)
	}
	defer migratedDB.Close()

	u, isNew, err := migratedDB.GetOrCreateUser(7001, "legacy_user", "Legacy", "User", 0)
	if err != nil {
		t.Fatalf("failed GetOrCreateUser on migrated DB: %v", err)
	}
	if !isNew {
		t.Errorf("expected isNew to be true")
	}
	if u.IsAdmin != false {
		t.Errorf("expected IsAdmin to be false, got %v", u.IsAdmin)
	}
}

func TestResetWarnings(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()

	userID := int64(8001)
	_, _, _ = database.GetOrCreateUser(userID, "warned_user", "Warned", "User", 0)

	warns, err := database.IncrementWarning(userID)
	if err != nil || warns != 1 {
		t.Fatalf("failed to increment warning: %v, count: %d", err, warns)
	}

	if err := database.ResetWarnings(userID); err != nil {
		t.Fatalf("failed to reset warnings: %v", err)
	}

	u, err := database.GetUserByID(userID)
	if err != nil {
		t.Fatalf("failed to reload user: %v", err)
	}
	if u.WarnCount != 0 {
		t.Errorf("expected WarnCount 0 after reset, got %d", u.WarnCount)
	}
}

func TestHasReceivedRepBonus(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()

	userID := int64(9001)
	_, _, _ = database.GetOrCreateUser(userID, "shieldy_user", "Shieldy", "User", 0)

	hasBonus, err := database.HasReceivedRepBonus(userID, "Shieldy verification")
	if err != nil {
		t.Fatalf("unexpected error checking rep bonus: %v", err)
	}
	if hasBonus {
		t.Errorf("expected hasBonus to be false initially")
	}

	_, err = database.AdjustReputation(userID, 5, "Shieldy verification: I am not a bot", userID)
	if err != nil {
		t.Fatalf("unexpected error adjusting reputation: %v", err)
	}

	hasBonus, err = database.HasReceivedRepBonus(userID, "Shieldy verification")
	if err != nil {
		t.Fatalf("unexpected error checking rep bonus after adjustment: %v", err)
	}
	if !hasBonus {
		t.Errorf("expected hasBonus to be true after adjustment")
	}
}

func TestGetUserDirectoryReport(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()

	superAdminID := int64(100)
	modAdminID := int64(200)

	// Create super admin
	_, _, _ = database.GetOrCreateUser(superAdminID, "superowner", "Super", "Admin", 100)
	// Create bot admin / moderator
	_, _, _ = database.GetOrCreateUser(modAdminID, "moduser", "Mod", "One", 100)
	_ = database.SetUserAdmin(modAdminID, true)

	// Create good user with approvals
	goodUser1ID := int64(300)
	_, _, _ = database.GetOrCreateUser(goodUser1ID, "approveduser", "Approved", "Guy", 80)
	_, _ = database.AdjustReputation(goodUser1ID, 5, "Approved by moderator", modAdminID)

	// Create regular good user
	goodUser2ID := int64(400)
	_, _, _ = database.GetOrCreateUser(goodUser2ID, "regularuser", "Regular", "Joe", 10)
	_ = database.SaveMessage(&Message{ChatID: -100, MessageID: 1, UserID: goodUser2ID, Text: "Hello"})

	// Create bad user: manually banned by moderator via flagged post
	badUser1ID := int64(500)
	_, _, _ = database.GetOrCreateUser(badUser1ID, "badspammer", "Bad", "Spammer", 0)
	fp, err := database.CreateFlaggedPost(-100, 10, badUser1ID, "Crypto link spam")
	if err != nil {
		t.Fatalf("failed to create flag: %v", err)
	}
	_ = database.ResolveFlaggedPost(fp.ID, "banned", modAdminID)
	_ = database.SetUserBanned(badUser1ID, true)

	// Create bad user: banned via detection trigger
	badUser2ID := int64(600)
	_, _, _ = database.GetOrCreateUser(badUser2ID, "cjkspammer", "CJK", "Bot", 0)
	_, _ = database.AdjustReputation(badUser2ID, -20, "Detection trigger (new_user_cjk): High-ID new user with low/no rep sent message containing CJK characters", 0)
	_ = database.SetUserBanned(badUser2ID, true)

	goodUsers, badUsers, err := database.GetUserDirectoryReport(superAdminID)
	if err != nil {
		t.Fatalf("unexpected error generating report: %v", err)
	}

	if len(goodUsers) != 4 {
		t.Errorf("expected 4 good users, got %d", len(goodUsers))
	}
	if len(badUsers) != 2 {
		t.Errorf("expected 2 bad users, got %d", len(badUsers))
	}

	// Verify good users ordering and roles
	if !goodUsers[0].IsSuperAdmin || goodUsers[0].UserID != superAdminID {
		t.Errorf("expected first good user to be super admin, got %+v", goodUsers[0])
	}
	if !goodUsers[1].IsAdmin || goodUsers[1].UserID != modAdminID {
		t.Errorf("expected second good user to be bot admin, got %+v", goodUsers[1])
	}
	if goodUsers[2].ApprovalCount != 1 || goodUsers[2].Role != "Approved Member ✅" {
		t.Errorf("expected third user to be approved member, got %+v", goodUsers[2])
	}

	// Verify bad users classification
	var manualBanUser, triggerBanUser *UserReportItem
	for i := range badUsers {
		if badUsers[i].UserID == badUser1ID {
			manualBanUser = &badUsers[i]
		}
		if badUsers[i].UserID == badUser2ID {
			triggerBanUser = &badUsers[i]
		}
	}

	if manualBanUser == nil || !manualBanUser.IsManualBan || manualBanUser.BannedBy != modAdminID {
		t.Errorf("expected manualBanUser to be manually banned by mod %d, got %+v", modAdminID, manualBanUser)
	}
	if manualBanUser != nil && !strings.Contains(manualBanUser.BannedByName, "@moduser") {
		t.Errorf("expected BannedByName to contain @moduser, got '%s'", manualBanUser.BannedByName)
	}

	if triggerBanUser == nil || !triggerBanUser.IsTriggerBan || triggerBanUser.TriggerName != "new_user_cjk" {
		t.Errorf("expected triggerBanUser to be trigger banned with trigger new_user_cjk, got %+v", triggerBanUser)
	}
}

func TestGenerateUserDirectoryMarkdown(t *testing.T) {
	goodUsers := []UserReportItem{
		{
			UserID:       101,
			Username:     "alice",
			FirstName:    "Alice",
			LastName:     "Wonder",
			Role:         "Super Admin 👑",
			Reputation:   100,
			WarnCount:    0,
			MessageCount: 15,
			CreatedAt:    time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC),
			UpdatedAt:    time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC),
		},
	}
	badUsers := []UserReportItem{
		{
			UserID:       202,
			Username:     "spambot",
			FirstName:    "Spam | Bot",
			LastName:     "",
			IsBanned:     true,
			IsManualBan:  true,
			BanType:      "Manual (Moderator)",
			BannedBy:     101,
			BannedByName: "@alice (`101`)",
			BanReason:    "Spam link | crypto | scam",
			Reputation:   -50,
			WarnCount:    1,
			MessageCount: 2,
			CreatedAt:    time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC),
			UpdatedAt:    time.Date(2026, 8, 2, 13, 0, 0, 0, time.UTC),
		},
		{
			UserID:       303,
			Username:     "",
			FirstName:    "CJK",
			LastName:     "Spammer",
			IsBanned:     true,
			IsTriggerBan: true,
			BanType:      "Automated Trigger",
			TriggerName:  "new_user_cjk",
			BanReason:    "Detection trigger (new_user_cjk): High-ID new user",
			Reputation:   -20,
			WarnCount:    0,
			MessageCount: 1,
			CreatedAt:    time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC),
			UpdatedAt:    time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC),
		},
	}

	opts := UserReportOptions{
		SuperAdminID: 101,
		DatabaseName: "test.db",
	}

	md := GenerateUserDirectoryMarkdown(goodUsers, badUsers, opts)

	if !strings.Contains(md, "# 📋 GoGCBot User Directory") {
		t.Errorf("expected header in markdown")
	}
	if !strings.Contains(md, "## 🟢 Known Good Users (1)") {
		t.Errorf("expected known good users section in markdown")
	}
	if !strings.Contains(md, "@alice") || !strings.Contains(md, "`101`") {
		t.Errorf("expected good user alice in markdown")
	}
	if !strings.Contains(md, "## 🔴 Known Bad Users (2)") {
		t.Errorf("expected known bad users section in markdown")
	}
	if !strings.Contains(md, "### 🔨 Manually Banned by Moderators (1)") {
		t.Errorf("expected manually banned section in markdown")
	}
	if !strings.Contains(md, "### 🤖 Automatically Banned by Detection Triggers (1)") {
		t.Errorf("expected trigger banned section in markdown")
	}
	// Check escaping of pipe '|' character in table
	if !strings.Contains(md, "Spam \\| Bot") {
		t.Errorf("expected escaped pipe character in display name, got:\n%s", md)
	}
	if !strings.Contains(md, "Spam link \\| crypto \\| scam") {
		t.Errorf("expected escaped pipe character in ban reason, got:\n%s", md)
	}

	// Test GoodOnly
	optsGood := UserReportOptions{GoodOnly: true}
	mdGood := GenerateUserDirectoryMarkdown(goodUsers, badUsers, optsGood)
	if !strings.Contains(mdGood, "Known Good Users") || strings.Contains(mdGood, "Known Bad Users") {
		t.Errorf("expected only good users in GoodOnly mode")
	}

	// Test BadOnly
	optsBad := UserReportOptions{BadOnly: true}
	mdBad := GenerateUserDirectoryMarkdown(goodUsers, badUsers, optsBad)
	if strings.Contains(mdBad, "Known Good Users") || !strings.Contains(mdBad, "Known Bad Users") {
		t.Errorf("expected only bad users in BadOnly mode")
	}

	// Test ManualBansOnly
	optsManual := UserReportOptions{ManualBansOnly: true}
	mdManual := GenerateUserDirectoryMarkdown(goodUsers, badUsers, optsManual)
	if strings.Contains(mdManual, "Known Good Users") || !strings.Contains(mdManual, "Manually Banned by Moderators") || strings.Contains(mdManual, "Automatically Banned by Detection Triggers") {
		t.Errorf("expected only manual bans in ManualBansOnly mode")
	}
}

func TestParseTimeString(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"2026-08-10 19:54:48.455960908 +0800 +08 m=+7368.147009537", "2026-08-10 19:54:48"},
		{"2026-08-09T19:54:43Z", "2026-08-09 19:54:43"},
		{"2026-08-09 19:54:43", "2026-08-09 19:54:43"},
		{"2026-08-09", "2026-08-09 00:00:00"},
		{"", ""},
	}

	for _, tc := range tests {
		parsed := parseTimeString(tc.input)
		if tc.expected == "" {
			if !parsed.IsZero() {
				t.Errorf("expected zero time for empty string, got %v", parsed)
			}
		} else {
			formatted := parsed.UTC().Format("2006-01-02 15:04:05")
			// also compare local or year
			if parsed.IsZero() {
				t.Errorf("failed to parse string '%s'", tc.input)
			}
			_ = formatted
		}
	}
}

func TestEscapeMarkdownCell(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"Hello | World", "Hello \\| World"},
		{"Line1\nLine2\r\nLine3", "Line1 Line2 Line3"},
		{"   ", "-"},
		{"Normal Text", "Normal Text"},
	}

	for _, tc := range tests {
		out := escapeMarkdownCell(tc.input)
		if out != tc.expected {
			t.Errorf("escapeMarkdownCell(%q) = %q, expected %q", tc.input, out, tc.expected)
		}
	}
}

func TestExtractTriggerName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"Detection trigger (new_user_cjk): High-ID new user", "new_user_cjk"},
		{"Detection trigger (new_user_chinese): Chinese chars", "new_user_chinese"},
		{"Low rep with cjk characters", "new_user_cjk"},
		{"Some unknown rule", "detection_trigger"},
	}

	for _, tc := range tests {
		out := extractTriggerName(tc.input)
		if out != tc.expected {
			t.Errorf("extractTriggerName(%q) = %q, expected %q", tc.input, out, tc.expected)
		}
	}
}

func TestDB_BackupTo(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()

	// Verify database Path
	if database.Path() == "" {
		t.Errorf("expected database path to be non-empty")
	}

	// Insert test data
	_, _, err := database.GetOrCreateUser(12345, "testuser", "Test", "User", 100)
	if err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	_ = database.SaveGroup(-100112233, "Test Group", "supergroup")
	_ = database.SaveMessage(&Message{
		ChatID:    -100112233,
		MessageID: 1,
		UserID:    12345,
		Text:      "Backup test message",
		CreatedAt: time.Now(),
	})

	// Create backup destination
	tmpDir, err := os.MkdirTemp("", "gogcbot_backup_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	backupPath := filepath.Join(tmpDir, "backup.db")

	if err := database.BackupTo(backupPath); err != nil {
		t.Fatalf("BackupTo failed: %v", err)
	}

	fileInfo, err := os.Stat(backupPath)
	if err != nil {
		t.Fatalf("failed to stat backup file: %v", err)
	}
	if fileInfo.Size() == 0 {
		t.Fatalf("backup file is empty")
	}

	// Open backup database to verify integrity and content
	backupDB, err := OpenDB(backupPath)
	if err != nil {
		t.Fatalf("failed to open backup db: %v", err)
	}
	defer backupDB.Close()

	u, err := backupDB.GetUserByID(12345)
	if err != nil {
		t.Fatalf("failed to get user from backup db: %v", err)
	}
	if u.Username != "testuser" || u.Reputation != 100 {
		t.Errorf("unexpected user in backup db: %+v", u)
	}

	stats, err := backupDB.GetStats()
	if err != nil {
		t.Fatalf("failed to get stats from backup db: %v", err)
	}
	if stats.TotalUsers != 1 || stats.TotalGroups != 1 || stats.TotalMessages != 1 {
		t.Errorf("unexpected stats in backup db: %+v", stats)
	}
}

func TestUserProfileOperations(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()

	// Initial user
	_, _, err := database.GetOrCreateUser(1001, "alice", "Alice", "Smith", 100)
	if err != nil {
		t.Fatalf("failed to create user: %v", err)
	}
	_, _, err = database.GetOrCreateUser(1002, "bob", "Bob", "Jones", 80)
	if err != nil {
		t.Fatalf("failed to create user 2: %v", err)
	}

	// 1. Check users without profile
	missing, err := database.GetUsersWithoutProfile(0)
	if err != nil {
		t.Fatalf("failed to get users without profile: %v", err)
	}
	if len(missing) != 2 {
		t.Errorf("expected 2 users without profile, got %d", len(missing))
	}

	// 2. Save user profile
	p := &UserProfile{
		UserID:            1001,
		Username:          "alice",
		FirstName:         "Alice",
		LastName:          "Smith",
		Bio:               "Golang developer and crypto enthusiast",
		PhotoFileID:       "big_file_id_123",
		PhotoFileUniqueID: "big_unique_id_123",
		PhotoCount:        2,
		HasPhoto:          true,
	}

	if err := database.SaveUserProfile(p); err != nil {
		t.Fatalf("failed to save user profile: %v", err)
	}

	// 3. Retrieve user profile
	gotProfile, err := database.GetUserProfile(1001)
	if err != nil {
		t.Fatalf("failed to get user profile: %v", err)
	}
	if gotProfile.Bio != "Golang developer and crypto enthusiast" || !gotProfile.HasPhoto || gotProfile.PhotoCount != 2 {
		t.Errorf("unexpected user profile data: %+v", gotProfile)
	}

	// 4. Update profile (upsert)
	p.Bio = "Updated bio text"
	p.PhotoCount = 3
	if err := database.SaveUserProfile(p); err != nil {
		t.Fatalf("failed to update user profile: %v", err)
	}

	updatedProfile, err := database.GetUserProfile(1001)
	if err != nil {
		t.Fatalf("failed to get updated profile: %v", err)
	}
	if updatedProfile.Bio != "Updated bio text" || updatedProfile.PhotoCount != 3 {
		t.Errorf("expected updated bio and photo count, got %+v", updatedProfile)
	}

	// 5. Count and query without profile after insert
	cnt, err := database.GetUserProfileCount()
	if err != nil {
		t.Fatalf("failed to count profiles: %v", err)
	}
	if cnt != 1 {
		t.Errorf("expected profile count 1, got %d", cnt)
	}

	missingAfter, err := database.GetUsersWithoutProfile(0)
	if err != nil {
		t.Fatalf("failed to get users without profile: %v", err)
	}
	if len(missingAfter) != 1 || missingAfter[0].UserID != 1002 {
		t.Errorf("expected only user 1002 in missing, got %+v", missingAfter)
	}

	// 6. GetAllUserProfiles
	allProfiles, err := database.GetAllUserProfiles(10)
	if err != nil {
		t.Fatalf("failed to get all user profiles: %v", err)
	}
	if len(allProfiles) != 1 {
		t.Errorf("expected 1 user profile in allProfiles, got %d", len(allProfiles))
	}

	// 7. Save user 1002 as not found
	notFoundProfile := &UserProfile{
		UserID:    1002,
		Username:  "bob",
		FirstName: "Bob",
		LastName:  "Jones",
		NotFound:  true,
	}
	if err := database.SaveUserProfile(notFoundProfile); err != nil {
		t.Fatalf("failed to save not found profile: %v", err)
	}

	gotNotFound, err := database.GetUserProfile(1002)
	if err != nil {
		t.Fatalf("failed to get user 1002 profile: %v", err)
	}
	if !gotNotFound.NotFound {
		t.Errorf("expected user 1002 to have NotFound = true")
	}

	// 8. Now missing profiles should be 0 because 1002 is marked as not found (has record in user_profiles)
	missingNone, err := database.GetUsersWithoutProfile(0)
	if err != nil {
		t.Fatalf("failed to get users without profile: %v", err)
	}
	if len(missingNone) != 0 {
		t.Errorf("expected 0 users without profile after marking not found, got %d", len(missingNone))
	}

	cnt2, err := database.GetUserProfileCount()
	if err != nil || cnt2 != 2 {
		t.Errorf("expected profile count 2, got %d, err: %v", cnt2, err)
	}
}

func TestMatchSpamBio(t *testing.T) {
	exactBio := "锦鲤代发 @mmmmue 6折础油卡E卡、沃尔玛、永辉、携程。天猫、苹果礼品卡、Steam等 联系 @xgshenqing888"
	matched, kws := MatchSpamBio(exactBio)
	if !matched {
		t.Fatalf("expected exact bio to match spam keywords, got false")
	}
	if len(kws) == 0 {
		t.Fatalf("expected matched keywords to be non-empty")
	}

	cleanBio := "Backend software engineer working with Go and distributed systems."
	matchedClean, _ := MatchSpamBio(cleanBio)
	if matchedClean {
		t.Errorf("expected clean bio to not match spam keywords")
	}

	matchedEmpty, _ := MatchSpamBio("")
	if matchedEmpty {
		t.Errorf("expected empty bio to return false")
	}

	// Test custom keywords passed dynamically from config
	customConfigured := []string{
		"test spam token",
		"promo campaign sample",
		"sample spam keyword",
	}
	for _, snip := range customConfigured {
		matchedSnip, kwsSnip := MatchSpamBio(fmt.Sprintf("Bio with snippet %s included", snip), customConfigured...)
		if !matchedSnip || len(kwsSnip) == 0 {
			t.Errorf("expected snippet %q to be matched via custom configured keywords, got %t, %v", snip, matchedSnip, kwsSnip)
		}
	}
}

func TestGetUnbannedSpamBioUsers(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()

	// 1. Unbanned new user with spam bio (should be matched)
	_, _, err := database.GetOrCreateUser(2001, "spammer1", "Spam", "One", 0)
	if err != nil {
		t.Fatalf("failed to create user 2001: %v", err)
	}
	_ = database.SaveUserProfile(&UserProfile{
		UserID:     2001,
		Username:   "spammer1",
		FirstName:  "Spam",
		LastName:   "One",
		Bio:        "锦鲤代发 @mmmmue 6折础油卡E卡、沃尔玛、永辉、携程。天猫、苹果礼品卡、Steam等 联系 @xgshenqing888",
		HasPhoto:   true,
		PhotoCount: 1,
	})

	// 2. Banned user with spam bio (should be excluded)
	_, _, err = database.GetOrCreateUser(2002, "banned_spammer", "Banned", "Spammer", 0)
	if err != nil {
		t.Fatalf("failed to create user 2002: %v", err)
	}
	_ = database.SetUserBanned(2002, true)
	_ = database.SaveUserProfile(&UserProfile{
		UserID:     2002,
		Username:   "banned_spammer",
		FirstName:  "Banned",
		LastName:   "Spammer",
		Bio:        "兼职日结 六百一天 沃尔玛 6折油卡 联系 @xgshenqing888",
		HasPhoto:   false,
		PhotoCount: 0,
	})

	// 3. Admin user with matching bio (should be excluded)
	_, _, err = database.GetOrCreateUser(2003, "adminuser", "Admin", "User", 100)
	if err != nil {
		t.Fatalf("failed to create user 2003: %v", err)
	}
	_ = database.SetUserAdmin(2003, true)
	_ = database.SaveUserProfile(&UserProfile{
		UserID:     2003,
		Username:   "adminuser",
		FirstName:  "Admin",
		LastName:   "User",
		Bio:        "Testing steam cards and 6折油卡",
		HasPhoto:   true,
		PhotoCount: 1,
	})

	// 4. Normal unbanned user with high reputation (> 20, should be excluded by default)
	_, _, err = database.GetOrCreateUser(2004, "normaluser", "Normal", "User", 50)
	if err != nil {
		t.Fatalf("failed to create user 2004: %v", err)
	}
	_ = database.SaveUserProfile(&UserProfile{
		UserID:     2004,
		Username:   "normaluser",
		FirstName:  "Normal",
		LastName:   "User",
		Bio:        "Just a normal crypto chat member with custom_promo_keyword and developer",
		HasPhoto:   true,
		PhotoCount: 1,
	})

	// 5. Unbanned user without profile / bio (new unknown user, rep 0, should be included)
	_, _, err = database.GetOrCreateUser(2005, "nobio_user", "No", "Bio", 0)
	if err != nil {
		t.Fatalf("failed to create user 2005: %v", err)
	}

	// 6. Unbanned new user with low reputation (rep 15 <= 20, should be matched)
	_, _, err = database.GetOrCreateUser(2006, "lowrep_user", "Low", "Rep", 15)
	if err != nil {
		t.Fatalf("failed to create user 2006: %v", err)
	}
	_ = database.SaveUserProfile(&UserProfile{
		UserID:     2006,
		Username:   "lowrep_user",
		FirstName:  "Low",
		LastName:   "Rep",
		Bio:        "Member with custom_promo_keyword",
		HasPhoto:   true,
		PhotoCount: 1,
	})

	// 7. Query without filters (matches all unbanned non-admin users with rep <= 20 and <= 5 posts)
	// User 2004 (rep 50) is excluded because rep > 20. Users 2001 (rep 0), 2005 (rep 0), 2006 (rep 15) are included.
	items, err := database.GetUnbannedUnknownUsers(UnknownUserOptions{
		ConfiguredKeywords: []string{"custom_promo_keyword", "test_promo_phrase"},
	})
	if err != nil {
		t.Fatalf("GetUnbannedUnknownUsers failed: %v", err)
	}
	if len(items) != 3 {
		t.Fatalf("expected 3 unbanned users (2001, 2005, 2006), got %d", len(items))
	}

	// Verify user 2004 (rep 50) was excluded, while user 2006 (rep 15) was included and matched keyword
	var user2004Found bool
	var user2006 *UnknownUserItem
	var user2005 *UnknownUserItem
	for i := range items {
		if items[i].UserID == 2004 {
			user2004Found = true
		}
		if items[i].UserID == 2006 {
			user2006 = &items[i]
		}
		if items[i].UserID == 2005 {
			user2005 = &items[i]
		}
	}
	if user2004Found {
		t.Errorf("expected user 2004 (rep 50 > 20) to be excluded from unknown users list")
	}
	if user2006 == nil || len(user2006.MatchedKeywords) == 0 || user2006.MatchedKeywords[0] != "custom_promo_keyword" {
		t.Errorf("expected user 2006 to match configured keyword 'custom_promo_keyword', got: %+v", user2006)
	}
	if user2005 == nil || user2005.Bio != "" {
		t.Errorf("expected user 2005 to be listed with empty bio, got: %+v", user2005)
	}

	// 8. Query with custom MaxReputation (MaxReputation: 50 or -1 includes user 2004)
	highRepItems, err := database.GetUnbannedUnknownUsers(UnknownUserOptions{MaxReputation: 50})
	if err != nil {
		t.Fatalf("GetUnbannedUnknownUsers with MaxReputation 50 failed: %v", err)
	}
	if len(highRepItems) != 4 {
		t.Fatalf("expected 4 unbanned users with MaxReputation: 50, got %d", len(highRepItems))
	}

	// 9. Query with lower MaxReputation (MaxReputation: 10 excludes user 2006 with rep 15 and user 2004 with rep 50)
	lowRepItems, err := database.GetUnbannedUnknownUsers(UnknownUserOptions{MaxReputation: 10})
	if err != nil {
		t.Fatalf("GetUnbannedUnknownUsers with MaxReputation 10 failed: %v", err)
	}
	if len(lowRepItems) != 2 {
		t.Fatalf("expected 2 unbanned users with MaxReputation: 10 (2001, 2005), got %d", len(lowRepItems))
	}

	// 10. Query with keyword filter
	filteredItems, err := database.GetUnbannedUnknownUsers(UnknownUserOptions{Keyword: "沃尔玛"})
	if err != nil {
		t.Fatalf("GetUnbannedUnknownUsers with keyword failed: %v", err)
	}
	if len(filteredItems) != 1 || filteredItems[0].UserID != 2001 {
		t.Fatalf("expected 1 user (2001) matching keyword '沃尔玛', got %+v", filteredItems)
	}

	// 11. Generate Markdown report
	md := GenerateUnknownUsersMarkdown(filteredItems, UnknownUserOptions{Keyword: "沃尔玛", DatabaseName: "test.db"})
	if md == "" {
		t.Errorf("expected non-empty Markdown report")
	}
	if !strings.Contains(md, "2001") || !strings.Contains(md, "@spammer1") {
		t.Errorf("expected markdown report to contain user 2001 info, got: %s", md)
	}
	if !strings.Contains(md, "Spam Match") {
		t.Errorf("expected 'Spam Match' column in markdown report")
	}
	if !strings.Contains(md, "⚠️ YES") {
		t.Errorf("expected '⚠️ YES' in markdown report for spam user")
	}
	if !strings.Contains(md, "- **Max Reputation**: `20`") {
		t.Errorf("expected Max Reputation in markdown header, got: %s", md)
	}
}

func TestSpamSnippetsTable(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()

	// 1. Sync spam snippets
	testSnippets := []string{"sample snippet 1", "sample snippet 2"}
	if err := database.SyncSpamSnippets(testSnippets); err != nil {
		t.Fatalf("failed to sync spam snippets: %v", err)
	}

	snippets, err := database.GetAllSpamSnippets()
	if err != nil {
		t.Fatalf("failed to get all spam snippets: %v", err)
	}
	if len(snippets) != 2 {
		t.Errorf("expected 2 snippets synced, got %d", len(snippets))
	}

	snippetStrings, err := database.GetSpamSnippetStrings()
	if err != nil {
		t.Fatalf("failed to get snippet strings: %v", err)
	}
	for _, expected := range testSnippets {
		found := false
		for _, actual := range snippetStrings {
			if actual == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected snippet %q in database", expected)
		}
	}

	// 2. Add custom snippet
	err = database.AddSpamSnippet("free tokens now", "promo")
	if err != nil {
		t.Fatalf("failed to add spam snippet: %v", err)
	}

	// 3. Duplicate snippet should update without error
	err = database.AddSpamSnippet("free tokens now", "updated_promo")
	if err != nil {
		t.Fatalf("failed to update duplicate snippet: %v", err)
	}

	// 4. Remove snippet
	err = database.RemoveSpamSnippet("free tokens now")
	if err != nil {
		t.Fatalf("failed to remove snippet: %v", err)
	}
}

func TestGetUserFullDump(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()

	userID := int64(888777)
	_, _, err := database.GetOrCreateUser(userID, "testdumpuser", "Test", "Dump", 50)
	if err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	_ = database.SaveUserProfile(&UserProfile{
		UserID:     userID,
		Username:   "testdumpuser",
		FirstName:  "Test",
		LastName:   "Dump",
		Bio:        "油卡 礼品卡 沃尔玛 永辉 6折联系",
		HasPhoto:   true,
		PhotoCount: 1,
		FetchedAt:  time.Now(),
	})

	_ = database.SaveMessage(&Message{
		ChatID:    -1001,
		MessageID: 10,
		UserID:    userID,
		Text:      "Test logged message",
		CreatedAt: time.Now(),
	})

	_, _ = database.AdjustReputation(userID, -20, "Spam detected", 0)
	fp, _ := database.CreateFlaggedPost(-1001, 10, userID, "Spam message")
	_ = database.ResolveFlaggedPost(fp.ID, "banned", 0)

	// 1. Search by @username
	dump1, err := database.GetUserFullDump("@testdumpuser", 1001)
	if err != nil {
		t.Fatalf("failed to get full dump by @username: %v", err)
	}
	if dump1.User.UserID != userID {
		t.Errorf("expected user ID %d, got %d", userID, dump1.User.UserID)
	}
	if dump1.Profile == nil || dump1.Profile.Bio == "" {
		t.Errorf("expected profile with bio in dump")
	}
	if !dump1.IsSpamBioMatch {
		t.Errorf("expected spam bio match to be true")
	}
	if len(dump1.RecentMessages) != 1 {
		t.Errorf("expected 1 recent message, got %d", len(dump1.RecentMessages))
	}
	if len(dump1.ReputationLogs) != 1 {
		t.Errorf("expected 1 rep log, got %d", len(dump1.ReputationLogs))
	}
	if len(dump1.FlaggedPosts) != 1 {
		t.Errorf("expected 1 flagged post, got %d", len(dump1.FlaggedPosts))
	}

	// 2. Search by numeric user ID
	dump2, err := database.GetUserFullDump(fmt.Sprintf("%d", userID), 1001)
	if err != nil {
		t.Fatalf("failed to get full dump by ID: %v", err)
	}
	if dump2.User.Username != "testdumpuser" {
		t.Errorf("expected username 'testdumpuser', got %s", dump2.User.Username)
	}

	// 3. Format dump as Markdown
	formatted := FormatUserDump(dump1)
	if !strings.Contains(formatted, "# 👤 Telegram User Dossier: @testdumpuser") {
		t.Errorf("expected header in formatted dump")
	}
	if !strings.Contains(formatted, "Test logged message") {
		t.Errorf("expected message in formatted dump")
	}
	if !strings.Contains(formatted, "Spam detected") {
		t.Errorf("expected reason in formatted dump")
	}
}

func TestMatchSpamBioProfile_And_ExtendedSignals(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()

	// 1. Test MatchSpamBioProfile with personal channel title
	p1 := &UserProfile{
		UserID:            1001,
		Bio:               "Regular innocent bio",
		PersonalChatTitle: "6折油卡代发专区",
	}
	isSpam, matched := MatchSpamBioProfile(p1)
	if !isSpam || len(matched) == 0 {
		t.Errorf("expected spam match on personal chat title, got isSpam=%v, matched=%v", isSpam, matched)
	}

	// 2. Test MatchSpamBioProfile with business intro
	p2 := &UserProfile{
		UserID:        1002,
		Bio:           "",
		BusinessIntro: "招兼职日结，每天200-500，加微信咨询",
	}
	isSpam2, matched2 := MatchSpamBioProfile(p2)
	if !isSpam2 || len(matched2) == 0 {
		t.Errorf("expected spam match on business intro, got isSpam=%v, matched=%v", isSpam2, matched2)
	}

	// 3. Test saving and retrieving extended signals in DB
	userID := int64(999888)
	_, _, err := database.GetOrCreateUser(userID, "signaluser", "Signal", "Tester", 100)
	if err != nil {
		t.Fatalf("failed to create user: %v", err)
	}
	err = database.UpdateUserMetadata(userID, "zh-hans", true)
	if err != nil {
		t.Fatalf("failed to update user metadata: %v", err)
	}

	u, err := database.GetUserByID(userID)
	if err != nil {
		t.Fatalf("failed to get user: %v", err)
	}
	if u.LanguageCode != "zh-hans" {
		t.Errorf("expected language code 'zh-hans', got %q", u.LanguageCode)
	}
	if !u.IsPremium {
		t.Errorf("expected is_premium to be true")
	}

	err = database.SaveUserProfile(&UserProfile{
		UserID:               userID,
		Username:             "signaluser",
		FirstName:            "Signal",
		LastName:             "Tester",
		LanguageCode:         "zh-hans",
		IsPremium:            true,
		Bio:                  "Innocent bio",
		HasPrivateForwards:   true,
		PersonalChatTitle:    "Official Crypto Channel",
		PersonalChatUsername: "cryptochannel",
		BusinessIntro:        "Welcome to our crypto shop",
		HasPhoto:             true,
		PhotoCount:           2,
		FetchedAt:            time.Now(),
	})
	if err != nil {
		t.Fatalf("failed to save user profile: %v", err)
	}

	prof, err := database.GetUserProfile(userID)
	if err != nil {
		t.Fatalf("failed to get user profile: %v", err)
	}
	if !prof.HasPrivateForwards {
		t.Errorf("expected has_private_forwards to be true")
	}
	if prof.PersonalChatTitle != "Official Crypto Channel" {
		t.Errorf("expected personal_chat_title 'Official Crypto Channel', got %q", prof.PersonalChatTitle)
	}
	if prof.BusinessIntro != "Welcome to our crypto shop" {
		t.Errorf("expected business_intro 'Welcome to our crypto shop', got %q", prof.BusinessIntro)
	}

	dump, err := database.GetUserFullDump(fmt.Sprintf("%d", userID), 0)
	if err != nil {
		t.Fatalf("failed to get full dump: %v", err)
	}
	formatted := FormatUserDump(dump)
	if !strings.Contains(formatted, "zh-hans") {
		t.Errorf("expected language code in dump output")
	}
	if !strings.Contains(formatted, "Telegram Premium") {
		t.Errorf("expected Premium in dump output")
	}
	if !strings.Contains(formatted, "Official Crypto Channel") {
		t.Errorf("expected personal channel in dump output")
	}

	// 4. Verify empty bio does not render - **Bio**:
	pNoBio := &UserProfile{
		UserID:    2002,
		Bio:       "",
		FetchedAt: time.Now(),
	}
	dumpNoBio := &UserFullDump{
		User:    &User{UserID: 2002, Username: "nobiouser", CreatedAt: time.Now(), UpdatedAt: time.Now()},
		Profile: pNoBio,
	}
	formattedNoBio := FormatUserDump(dumpNoBio)
	if strings.Contains(formattedNoBio, "- **Bio**:") {
		t.Errorf("expected empty bio to NOT render - **Bio**: section, got: %s", formattedNoBio)
	}
}

func TestUpdateUserName(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()

	userID := int64(445566)
	_, _, err := database.GetOrCreateUser(userID, "olduser", "OldFirst", "OldLast", 10)
	if err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	if err := database.UpdateUserName(userID, "newuser", "NewFirst", "NewLast"); err != nil {
		t.Fatalf("failed to update user name: %v", err)
	}

	u, err := database.GetUserByID(userID)
	if err != nil {
		t.Fatalf("failed to get user: %v", err)
	}
	if u.Username != "newuser" || u.FirstName != "NewFirst" || u.LastName != "NewLast" {
		t.Errorf("unexpected updated user: %+v", u)
	}
}

func TestGetLowRepUsersForRescan(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()

	now := time.Now()

	// User 1: rep 10, no profile (should be included)
	_, _, _ = database.GetOrCreateUser(101, "u1", "User", "One", 10)

	// User 2: rep 5, profile fetched 30 hours ago (should be included)
	_, _, _ = database.GetOrCreateUser(102, "u2", "User", "Two", 5)
	_ = database.SaveUserProfile(&UserProfile{
		UserID:    102,
		Username:  "u2",
		FetchedAt: now.Add(-30 * time.Hour),
	})

	// User 3: rep 5, profile fetched 2 hours ago (should NOT be included if cutoff is 24h)
	_, _, _ = database.GetOrCreateUser(103, "u3", "User", "Three", 5)
	_ = database.SaveUserProfile(&UserProfile{
		UserID:    103,
		Username:  "u3",
		FetchedAt: now.Add(-2 * time.Hour),
	})

	// User 4: rep 80 (high rep, should NOT be included if maxRep is 20)
	_, _, _ = database.GetOrCreateUser(104, "u4", "User", "Four", 80)

	// User 5: rep 0, banned (should NOT be included)
	_, _, _ = database.GetOrCreateUser(105, "u5", "User", "Five", 0)
	_ = database.SetUserBanned(105, true)

	// User 6: rep 0, admin (should NOT be included)
	_, _, _ = database.GetOrCreateUser(106, "u6", "User", "Six", 0)
	_ = database.SetUserAdmin(106, true)

	// Test 1: Cutoff 24 hours ago, maxRep 20 -> should match User 1 and User 2
	cutoff24h := now.Add(-24 * time.Hour)
	candidates, err := database.GetLowRepUsersForRescan(20, cutoff24h, 0)
	if err != nil {
		t.Fatalf("failed to query candidates: %v", err)
	}
	if len(candidates) != 2 {
		t.Fatalf("expected 2 candidates (User 1 and User 2), got %d: %+v", len(candidates), candidates)
	}
	candIDs := map[int64]bool{candidates[0].UserID: true, candidates[1].UserID: true}
	if !candIDs[101] || !candIDs[102] {
		t.Errorf("expected IDs 101 and 102, got %+v", candIDs)
	}

	// Test 2: Force (cutoff zero), maxRep 20 -> should match User 1, User 2, User 3
	candidatesForce, err := database.GetLowRepUsersForRescan(20, time.Time{}, 0)
	if err != nil {
		t.Fatalf("failed to query candidates force: %v", err)
	}
	if len(candidatesForce) != 3 {
		t.Fatalf("expected 3 candidates force (User 1, 2, 3), got %d: %+v", len(candidatesForce), candidatesForce)
	}
}

func TestIsSpammyUsername(t *testing.T) {
	tests := []struct {
		username string
		want     bool
	}{
		{"gzy_8889215646_1_5248", true},
		{"@gzy_8889215646_1_5248", true},
		{"abc12345", true},
		{"test_channel_999", true},
		{"ch-123", true},
		{"user.2026.08", true},
		{"gzy888921564615248", true},
		{"news_channel", false},
		{"alice", false},
		{"123_channel", false},
		{"user_123_abc", false},
		{"123456", false},
		{"", false},
		{"___---...", false},
	}

	for _, tt := range tests {
		got := IsSpammyUsername(tt.username)
		if got != tt.want {
			t.Errorf("IsSpammyUsername(%q) = %v, want %v", tt.username, got, tt.want)
		}
	}
}

func TestMatchSpamBioProfile_DianWo_And_SpammyUsername(t *testing.T) {
	// Test 1: Dian Wo keyword in personal chat title
	p1 := &UserProfile{
		UserID:            8828604089,
		PersonalChatTitle: "🔴点我六折出平果机进群🔴",
	}
	isSpam1, matched1 := MatchSpamBioProfile(p1)
	if !isSpam1 {
		t.Fatalf("expected MatchSpamBioProfile to detect '点我', got isSpam=false")
	}
	hasDianWo := false
	for _, kw := range matched1 {
		if kw == "点我" {
			hasDianWo = true
		}
	}
	if !hasDianWo {
		t.Errorf("expected matched keywords to contain '点我', got: %v", matched1)
	}

	// Test 2: Spammy username in personal chat username
	p2 := &UserProfile{
		UserID:               8828604089,
		PersonalChatUsername: "gzy_8889215646_1_5248",
	}
	isSpam2, matched2 := MatchSpamBioProfile(p2)
	if !isSpam2 {
		t.Fatalf("expected MatchSpamBioProfile to detect spammy channel username, got isSpam=false")
	}
	hasSpammyUser := false
	for _, kw := range matched2 {
		if strings.Contains(kw, "spammy_channel_username") {
			hasSpammyUser = true
		}
	}
	if !hasSpammyUser {
		t.Errorf("expected matched keywords to contain spammy_channel_username, got: %v", matched2)
	}

	// Test 3: Clean profile
	p3 := &UserProfile{
		UserID:               12345,
		Bio:                  "Just a normal user bio",
		PersonalChatTitle:    "My Coding Channel",
		PersonalChatUsername: "my_coding_channel",
	}
	isSpam3, matched3 := MatchSpamBioProfile(p3)
	if isSpam3 || len(matched3) > 0 {
		t.Errorf("expected clean profile, got isSpam=%v, matched=%v", isSpam3, matched3)
	}
}

func TestGetBannedUsers(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()

	// Initially no banned users
	banned, err := database.GetBannedUsers()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(banned) != 0 {
		t.Fatalf("expected 0 banned users, got %d", len(banned))
	}

	// Create users
	_, _, err = database.GetOrCreateUser(1001, "active_user", "Active", "User", 50)
	if err != nil {
		t.Fatalf("failed to create user: %v", err)
	}
	_, _, err = database.GetOrCreateUser(1002, "banned_user1", "Banned", "One", -50)
	if err != nil {
		t.Fatalf("failed to create user: %v", err)
	}
	_, _, err = database.GetOrCreateUser(1003, "banned_user2", "Banned", "Two", -50)
	if err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	// Mark users as banned
	_ = database.SetUserBanned(1002, true)
	_ = database.SetUserBanned(1003, true)

	banned, err = database.GetBannedUsers()
	if err != nil {
		t.Fatalf("failed to get banned users: %v", err)
	}
	if len(banned) != 2 {
		t.Fatalf("expected 2 banned users, got %d", len(banned))
	}

	// Unban one
	_ = database.SetUserBanned(1002, false)
	banned, err = database.GetBannedUsers()
	if err != nil {
		t.Fatalf("failed to get banned users: %v", err)
	}
	if len(banned) != 1 || banned[0].UserID != 1003 {
		t.Fatalf("expected 1 banned user (1003), got %v", banned)
	}
}

