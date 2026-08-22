package detector

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/angch/gogcbot/pkg/db"
)

// findRealDatabase searches parent directories for gogcbot.db.
func findRealDatabase() string {
	candidates := []string{
		"gogcbot.db",
		"../../gogcbot.db",
		"../gogcbot.db",
		"./gogcbot.db",
	}
	for _, c := range candidates {
		abs, err := filepath.Abs(c)
		if err == nil {
			if _, err := os.Stat(abs); err == nil {
				return abs
			}
		}
	}
	return ""
}

func buildStandardTestDetector() *Detector {
	return NewDetector(
		NewNewUserCJKTrigger(NewUserCJKTriggerConfig{
			Enabled:       true,
			MinHighUserID: 1000000000,
			MaxReputation: 5,
			MaxUserPosts:  5,
			RepPenalty:    20,
		}),
		NewNewUserSpamBioTrigger(NewUserSpamBioTriggerConfig{
			Enabled:       true,
			MaxReputation: 5,
			MaxUserPosts:  5,
			RepPenalty:    20,
			CustomKeywords: []string{
				"crypto giveaway", "t.me/", "whatsapp.com", "fast money",
				"airdrop", "free rolls", "油卡", "沃尔玛", "点我",
			},
		}),
		NewRedPacketNameTrigger(RedPacketNameTriggerConfig{
			Enabled:           true,
			MinHighUserID:     1000000000,
			MaxReputation:     5,
			MaxUserPosts:      5,
			MinUsernameLength: 5,
			MinCJKRatio:       0.5,
			MinCJKChars:       1,
			RepPenalty:        20,
		}),
		NewUsernameAnomalyTrigger(UsernameAnomalyTriggerConfig{
			Enabled:       true,
			MinHighUserID: 1000000000,
			MaxReputation: 5,
			MaxUserPosts:  5,
			MinScore:      3,
			FlagOnly:      true,
			RepPenalty:    20,
		}),
		NewProfileNameKeywordBanTrigger(ProfileNameKeywordBanConfig{
			Enabled:         true,
			MinHighUserID:   1000000000,
			MaxReputation:   5,
			MaxUserPosts:    5,
			MinScore:        3,
			FlagOnly:        true,
			RepPenalty:      20,
			BlockedKeywords: []string{"0壹天", "每日", "吴压", "吾思", "兼织"},
		}),
		NewNonsenseBioTrigger(NonsenseBioTriggerConfig{
			Enabled:       true,
			MinHighUserID: 1000000000,
			MaxReputation: 5,
			MaxUserPosts:  5,
			MinWords:      5,
			FlagOnly:      true,
			RepPenalty:    20,
		}),
	)
}

// TestTriggers_NoMatchAgainstHighRepUsers_RealDB verifies that no triggers match against any user in gogcbot.db with reputation > 40.
func TestTriggers_NoMatchAgainstHighRepUsers_RealDB(t *testing.T) {
	dbPath := findRealDatabase()
	if dbPath == "" {
		t.Skip("gogcbot.db not found, skipping real database check")
	}

	database, err := db.OpenDB(dbPath)
	if err != nil {
		t.Fatalf("failed to open database at %s: %v", dbPath, err)
	}
	defer database.Close()

	users, err := database.GetUsersWithReputationAbove(40)
	if err != nil {
		t.Fatalf("failed to query users with reputation > 40: %v", err)
	}
	if len(users) == 0 {
		t.Skip("no users with reputation > 40 found in gogcbot.db")
	}

	det := buildStandardTestDetector()
	violations, err := det.ValidateAgainstHighRepUsers(database, 40)
	if err != nil {
		t.Fatalf("validation check failed with error: %v", err)
	}

	if len(violations) > 0 {
		t.Fatalf("Rule validation violation against gogcbot.db users (rep > 40)!\n%s", FormatRuleMatchViolations(violations))
	}

	t.Logf("✅ Successfully verified %d users with reputation > 40 across all triggers in gogcbot.db with 0 violations.", len(users))
}

// TestTriggers_NoMatchAgainstHighRepUsers_SyntheticDB tests rules against synthesized high-reputation users.
func TestTriggers_NoMatchAgainstHighRepUsers_SyntheticDB(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "gogcbot_rule_val_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	database, err := db.OpenDB(dbPath)
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}
	defer database.Close()

	// Insert various users with rep > 40 (some with patterns that would trigger if rep were low)
	testUsers := []struct {
		userID   int64
		username string
		first    string
		last     string
		rep      int
		admin    bool
		bio      string
		messages []string
	}{
		{
			userID:   10104269,
			username: "angch",
			first:    "Ang",
			last:     "ChinHan",
			rep:      100,
			admin:    true,
			bio:      "Developer & bot maintainer",
			messages: []string{"Hello world", "测试中文消息", "Check out https://github.com/angch/gogcbot"},
		},
		{
			// High ID, rep 50, has CJK in name and mixed-caps username
			userID:   5000000001,
			username: "cbzbQFLOuHNkJZ",
			first:    "每日首发🧧",
			last:     "",
			rep:      50,
			bio:      "Regular member bio",
			messages: []string{"Just chatting here", "大家好！"},
		},
		{
			// Rep 41, has blocked keyword in name and spammy bio
			userID:   6000000002,
			username: "member41",
			first:    "六o0壹天",
			last:     "",
			rep:      41,
			bio:      "联系 沃尔玛 永辉",
			messages: []string{"Nice group!"},
		},
		{
			// Rep 100, has auto-generated handle signature
			userID:   7000000003,
			username: "qtdulcljcptk6950",
			first:    "David",
			last:     "Brown",
			rep:      100,
			bio:      "Crypto and tech enthusiast",
			messages: []string{"Welcome to the community!"},
		},
		{
			// Rep 5 (low rep user) - should be skipped by ValidateAgainstHighRepUsers(minRep=40)
			userID:   8000000004,
			username: "lowrepuser",
			first:    "SpamBot",
			last:     "",
			rep:      5,
			bio:      "Spam bio 沃尔玛",
			messages: []string{"Spam link t.me/spam"},
		},
	}

	for _, tu := range testUsers {
		u, _, err := database.GetOrCreateUser(tu.userID, tu.username, tu.first, tu.last, tu.rep)
		if err != nil {
			t.Fatalf("failed to create user %d: %v", tu.userID, err)
		}
		if tu.admin {
			_ = database.SetUserAdmin(tu.userID, true)
		}
		_ = database.SetReputation(tu.userID, tu.rep, "Test setup", 0)

		if tu.bio != "" {
			_ = database.SaveUserProfile(&db.UserProfile{
				UserID:    tu.userID,
				Username:  tu.username,
				FirstName: tu.first,
				LastName:  tu.last,
				Bio:       tu.bio,
				FetchedAt: time.Now(),
			})
		}

		for msgID, text := range tu.messages {
			_ = database.SaveMessage(&db.Message{
				ChatID:    12345,
				MessageID: msgID + 1,
				UserID:    u.UserID,
				Text:      text,
				CreatedAt: time.Now(),
			})
		}
	}

	det := buildStandardTestDetector()
	violations, err := det.ValidateAgainstHighRepUsers(database, 40)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(violations) > 0 {
		t.Fatalf("Expected 0 violations for synthetic users with rep > 40, got %d:\n%s",
			len(violations), FormatRuleMatchViolations(violations))
	}
}

// TestTriggers_ViolationDetection_BuggyRule tests that ValidateAgainstHighRepUsers correctly catches a buggy trigger.
func TestTriggers_ViolationDetection_BuggyRule(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "gogcbot_buggy_rule_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	database, err := db.OpenDB(dbPath)
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}
	defer database.Close()

	_, _, _ = database.GetOrCreateUser(10104269, "angch", "Ang", "ChinHan", 100)

	// Create a faulty trigger that ignores reputation and triggers on anyone with "Ang" in display name
	buggyTrigger := &dummyTrigger{
		id:        "buggy_name_rule",
		enabled:   true,
		triggered: true,
		actions:   []Action{{Type: ActionBanUser, Reason: "Fired unconditionally"}},
	}

	det := NewDetector(buggyTrigger)
	violations, err := det.ValidateAgainstHighRepUsers(database, 40)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(violations) == 0 {
		t.Fatal("Expected buggy trigger to be caught by ValidateAgainstHighRepUsers, but got 0 violations")
	}

	v := violations[0]
	if v.TriggerID != "buggy_name_rule" {
		t.Errorf("Expected TriggerID 'buggy_name_rule', got %q", v.TriggerID)
	}
	if v.User == nil || v.User.UserID != 10104269 {
		t.Errorf("Expected violation on user 10104269, got %+v", v.User)
	}

	formatted := FormatRuleMatchViolations(violations)
	if !strings.Contains(formatted, "buggy_name_rule") || !strings.Contains(formatted, "10104269") {
		t.Errorf("Formatted string missing details: %s", formatted)
	}
}
