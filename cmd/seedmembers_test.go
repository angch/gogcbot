package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/angch/gogcbot/pkg/config"
	"github.com/angch/gogcbot/pkg/db"
	"github.com/gotd/td/tg"
)

func TestFindPeer(t *testing.T) {
	channelMap := map[int64]*tg.Channel{
		1783678483: {
			ID:    1783678483,
			Title: "🗑🤪Golang Malaysia, Overflow, offtopic, etc. 🐳 feat 二郎",
		},
		1072966891: {
			ID:    1072966891,
			Title: "Golang Malaysia, feat Rustlang, Erlang 二郎, Haskelllang and .*-?(lang|script).",
		},
		987654321: {
			ID:    987654321,
			Title: "Another Channel",
		},
	}

	chatMap := map[int64]*tg.Chat{
		5576645586: {
			ID:    5576645586,
			Title: "Golang mod queue",
		},
		555666: {
			ID:    555666,
			Title: "Legacy Chat",
		},
	}

	titleMap := map[string]*tg.Channel{
		"🗑🤪golang malaysia, overflow, offtopic, etc. 🐳 feat 二郎":                         channelMap[1783678483],
		"golang malaysia, feat rustlang, erlang 二郎, haskelllang and .*-?(lang|script).": channelMap[1072966891],
		"another channel": channelMap[987654321],
	}

	chatTitleMap := map[string]*tg.Chat{
		"golang mod queue": chatMap[5576645586],
		"legacy chat":      chatMap[555666],
	}

	tests := []struct {
		name          string
		group         db.Group
		expectChannel bool
		expectChat    bool
		expectedID    int64
	}{
		{
			name: "Match supergroup 1 by -100 ID",
			group: db.Group{
				ChatID: -1001783678483,
				Title:  "🗑🤪Golang Malaysia, Overflow, offtopic, etc. 🐳 feat 二郎",
			},
			expectChannel: true,
			expectedID:    1783678483,
		},
		{
			name: "Match supergroup 2 by -100 ID",
			group: db.Group{
				ChatID: -1001072966891,
				Title:  "Golang Malaysia, feat Rustlang, Erlang 二郎, Haskelllang and .*-?(lang|script).",
			},
			expectChannel: true,
			expectedID:    1072966891,
		},
		{
			name: "Match group 3 (mod queue) by negative ID",
			group: db.Group{
				ChatID: -5576645586,
				Title:  "Golang mod queue",
			},
			expectChat: true,
			expectedID: 5576645586,
		},
		{
			name: "Match channel by fuzzy/clean title when ID differs",
			group: db.Group{
				ChatID: -1000000000,
				Title:  "Golang Malaysia, Overflow",
			},
			expectChannel: true,
			expectedID:    1783678483,
		},
		{
			name: "Match basic group by negative ID",
			group: db.Group{
				ChatID: -555666,
				Title:  "Legacy Chat",
			},
			expectChat: true,
			expectedID: 555666,
		},
		{
			name: "Unmatched group",
			group: db.Group{
				ChatID: -1009999999999,
				Title:  "Unknown Group",
			},
			expectChannel: false,
			expectChat:    false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ch, chat := findPeer(tc.group, channelMap, chatMap, titleMap, chatTitleMap)
			if tc.expectChannel {
				if ch == nil {
					t.Fatalf("expected channel match, got nil")
				}
				if ch.ID != tc.expectedID {
					t.Errorf("expected channel ID %d, got %d", tc.expectedID, ch.ID)
				}
			} else if tc.expectChat {
				if chat == nil {
					t.Fatalf("expected chat match, got nil")
				}
				if chat.ID != tc.expectedID {
					t.Errorf("expected chat ID %d, got %d", tc.expectedID, chat.ID)
				}
			} else {
				if ch != nil || chat != nil {
					t.Errorf("expected no match, got ch=%v, chat=%v", ch, chat)
				}
			}
		})
	}
}

func TestPrintSummary(t *testing.T) {
	summary := &SeedMembersSummary{
		TotalGroups:        5,
		ScannedGroups:      4,
		FailedGroups:       1,
		TotalParticipants:  120,
		NewUsersCreated:    25,
		ExistingUsersSeen:  95,
		TotalJoinsRecorded: 120,
	}

	t.Run("Print live summary", func(t *testing.T) {
		buf := new(bytes.Buffer)
		printSummary(buf, summary, false)
		out := buf.String()

		if !strings.Contains(out, "Member Seeding Summary") {
			t.Errorf("expected header, got: %s", out)
		}
		if !strings.Contains(out, "Total Groups in Database : 5") {
			t.Errorf("expected total groups, got: %s", out)
		}
		if !strings.Contains(out, "New Users Created        : 25") {
			t.Errorf("expected new users, got: %s", out)
		}
		if !strings.Contains(out, "Group Joins Recorded     : 120") {
			t.Errorf("expected joins recorded, got: %s", out)
		}
	})

	t.Run("Print dry run summary", func(t *testing.T) {
		buf := new(bytes.Buffer)
		printSummary(buf, summary, true)
		out := buf.String()

		if !strings.Contains(out, "[DRY RUN]") {
			t.Errorf("expected DRY RUN tag, got: %s", out)
		}
		if strings.Contains(out, "New Users Created") {
			t.Errorf("expected no user creation stats in dry run, got: %s", out)
		}
	})

	t.Run("Print nil summary does not panic", func(t *testing.T) {
		buf := new(bytes.Buffer)
		printSummary(buf, nil, false)
		if buf.Len() != 0 {
			t.Errorf("expected empty buffer for nil summary, got: %s", buf.String())
		}
	})
}

func TestSeedMembersNoGroups(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "gogcbot_seedmembers_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	cfgPath := filepath.Join(tmpDir, "config.yaml")

	database, err := db.OpenDB(dbPath)
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}
	database.Close()

	cfg := config.DefaultConfig()
	cfg.DBPath = dbPath
	if err := config.SaveConfig(cfgPath, &cfg); err != nil {
		t.Fatalf("failed to save test config: %v", err)
	}

	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{
		"seedmembers",
		"--config", cfgPath,
		"--api-id", "123456",
		"--api-hash", "fakehash",
	})

	err = rootCmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "No groups found in the database") {
		t.Errorf("expected 'No groups found in the database' message, got: %s", out)
	}
}
