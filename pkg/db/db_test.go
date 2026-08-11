package db

import (
	"os"
	"path/filepath"
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
