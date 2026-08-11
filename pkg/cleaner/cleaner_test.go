package cleaner

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/angch/gogcbot/pkg/db"
)

func setupTestDB(t *testing.T) (*db.DB, func()) {
	tmpDir, err := os.MkdirTemp("", "gogcbot_cleaner_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	dbPath := filepath.Join(tmpDir, "test.db")

	database, err := db.OpenDB(dbPath)
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

func TestRetentionCleaner_RunOnce(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()

	rc := NewRetentionCleaner(database, 1)

	// Insert 10-day old message
	oldMsg := &db.Message{
		ChatID:    -1001,
		MessageID: 1,
		UserID:    5001,
		Text:      "Old message",
		CreatedAt: time.Now().AddDate(0, 0, -10),
	}
	if err := database.SaveMessage(oldMsg); err != nil {
		t.Fatalf("failed to save old message: %v", err)
	}

	// Run cleaner
	oldPruned, userPruned, err := rc.RunOnce()
	if err != nil {
		t.Fatalf("RunOnce failed: %v", err)
	}

	if oldPruned != 1 {
		t.Errorf("expected 1 old message pruned, got %d", oldPruned)
	}
	if userPruned != 0 {
		t.Errorf("expected 0 user posts pruned, got %d", userPruned)
	}
}
