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
	u, err := database.GetOrCreateUser(1001, "alice", "Alice", "Smith", 100)
	if err != nil {
		t.Fatalf("unexpected error creating user: %v", err)
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

	_, err := database.GetOrCreateUser(userID, "bob", "Bob", "Builder", 100)
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

	u, err := database.GetOrCreateUser(4004, "charlie", "Charlie", "Brown", 0)
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
