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
		NewSecondaryGroupTrigger(SecondaryGroupTriggerConfig{
			Enabled:       true,
			MaxReputation: 5,
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
			// High ID, rep 50, has CJK in name and mixed-caps username (red_packet_cjk_name trigger target)
			userID:   5000000001,
			username: "cbzbQFLOuHNkJZ",
			first:    "每日首发🧧",
			last:     "",
			rep:      50,
			bio:      "Regular member bio",
			messages: []string{"Just chatting here", "大家好！"},
		},
		{
			// Rep 50, has blocked keyword in name and spammy bio (profile_name_keyword_ban & spam_bio target)
			userID:   6000000002,
			username: "member50_spamkw",
			first:    "六o0壹天",
			last:     "",
			rep:      50,
			bio:      "联系 沃尔玛 永辉 crypto giveaway airdrop",
			messages: []string{"Nice group!", "Check out this crypto airdrop t.me/airdrop"},
		},
		{
			// Rep 50, has auto-generated handle signature (username_anomaly target)
			userID:   7000000003,
			username: "qtdulcljcptk6950",
			first:    "David",
			last:     "Brown",
			rep:      50,
			bio:      "Tech enthusiast",
			messages: []string{"Welcome to the community!"},
		},
		{
			// Rep 50, has nonsense filler bio (nonsense_bio target)
			userID:   7000000004,
			username: "filleruser50",
			first:    "James",
			last:     "Smith",
			rep:      50,
			bio:      "Scientist near entire offer stay.",
			messages: []string{"Hello everyone here."},
		},
		{
			// Rep 50, high ID, sent purely CJK messages (new_user_cjk target)
			userID:   7000000005,
			username: "cjkuser50",
			first:    "李明",
			last:     "",
			rep:      50,
			bio:      "Software developer",
			messages: []string{"你好大家，欢迎来到群组！", "今天天气真好。"},
		},
		{
			// Rep 41, has blocked keyword in name
			userID:   6000000006,
			username: "member41",
			first:    "吴压吾思",
			last:     "",
			rep:      41,
			bio:      "Member bio",
			messages: []string{"Hello!"},
		},
		{
			// Rep 5 (low rep user) - should be skipped by ValidateAgainstHighRepUsers(minRep=40)
			userID:   8000000007,
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

// TestTriggers_ValidateAgainstKnownGoodAndBad_RealDB verifies rules against known good and bad tables in real DB.
func TestTriggers_ValidateAgainstKnownGoodAndBad_RealDB(t *testing.T) {
	dbPath := findRealDatabase()
	if dbPath == "" {
		t.Skip("gogcbot.db not found, skipping real database check")
	}

	database, err := db.OpenDB(dbPath)
	if err != nil {
		t.Fatalf("failed to open database at %s: %v", dbPath, err)
	}
	defer database.Close()

	det := buildStandardTestDetector()
	report, err := det.ValidateAgainstKnownGoodAndBad(database)
	if err != nil {
		t.Fatalf("validation check failed: %v", err)
	}

	t.Logf("Validation report summary:\n%s", report.Summary)

	if !report.Passed {
		t.Fatalf("Rule validation regression on known good data in real DB! %d false positive violation(s):\n%s",
			len(report.FalsePositives), report.Summary)
	}

	if report.KnownGoodTotal == 0 {
		t.Errorf("expected >0 known good profiles in real DB")
	}
}

// TestTriggers_ValidateAgainstKnownGoodAndBad_Synthetic tests Known Good and Known Bad validation on synthesized database.
func TestTriggers_ValidateAgainstKnownGoodAndBad_Synthetic(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "gogcbot_knowndata_test_*")
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

	// 1. Add Known Good users (rep >= 50)
	kg1 := &db.KnownGoodUser{
		UserID:         100001,
		Username:       "devlead",
		FirstName:      "David",
		LastName:       "Lee",
		Reputation:     100,
		Bio:            "Go developer and open source enthusiast",
		SampleMessages: []string{"Welcome everyone!", "Let's review the PR."},
		Source:         "auto_reputation",
	}
	kg2 := &db.KnownGoodUser{
		UserID:         100002,
		Username:       "alice_w",
		FirstName:      "Alice",
		LastName:       "Wong",
		Reputation:     55,
		Bio:            "Golang Malaysia member",
		SampleMessages: []string{"Happy coding everyone!", "See you at the meetup."},
		Source:         "auto_reputation",
	}
	_ = database.SaveKnownGoodUser(kg1)
	_ = database.SaveKnownGoodUser(kg2)

	// 2. Add Known Bad users (banned spammers)
	kb1 := &db.KnownBadUser{
		UserID:         5000000001,
		Username:       "cbzbQFLOuHNkJZ",
		FirstName:      "每日首发🧧",
		Pattern:        "每日首发🧧",
		BanReason:      "Red packet name spam",
		SampleMessages: []string{"Spam link t.me/airdrop"},
		Source:         "banned_user",
	}
	kb2 := &db.KnownBadUser{
		UserID:    5000000002,
		Username:  "spammer_kw",
		FirstName: "六o0壹天",
		Pattern:   "六o0壹天",
		BanReason: "Profile keyword ban",
		Bio:       "crypto giveaway airdrop",
		Source:    "banned_user",
	}
	_ = database.SaveKnownBadUser(kb1)
	_ = database.SaveKnownBadUser(kb2)

	det := buildStandardTestDetector()
	report, err := det.ValidateAgainstKnownGoodAndBad(database)
	if err != nil {
		t.Fatalf("unexpected validation error: %v", err)
	}

	if !report.Passed {
		t.Fatalf("expected report to pass with 0 false positives, got %d:\n%s",
			len(report.FalsePositives), report.Summary)
	}
	if report.KnownGoodTotal != 2 {
		t.Errorf("expected 2 known good profiles, got %d", report.KnownGoodTotal)
	}
	if report.KnownBadTotal != 2 {
		t.Errorf("expected 2 known bad profiles, got %d", report.KnownBadTotal)
	}
	if len(report.KnownBadMatches) == 0 {
		t.Errorf("expected detection matches on known bad data, got 0")
	}
}

// TestTriggers_ValidateAgainstKnownGoodAndBad_RegressionDetection verifies that a faulty rule is caught.
func TestTriggers_ValidateAgainstKnownGoodAndBad_RegressionDetection(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "gogcbot_knowndata_regr_test_*")
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

	// Add known good user
	kg := &db.KnownGoodUser{
		UserID:     100001,
		Username:   "gooddev",
		FirstName:  "David",
		Reputation: 100,
		Bio:        "Passionate Go programmer",
	}
	_ = database.SaveKnownGoodUser(kg)

	// Faulty rule that bans anyone with "David" in profile
	buggyTrigger := &dummyTrigger{
		id:        "buggy_david_rule",
		enabled:   true,
		triggered: true,
		actions:   []Action{{Type: ActionBanUser, Reason: "Fired on David"}},
	}

	det := NewDetector(buggyTrigger)
	report, err := det.ValidateAgainstKnownGoodAndBad(database)
	if err != nil {
		t.Fatalf("unexpected validation error: %v", err)
	}

	if report.Passed {
		t.Fatalf("expected validation to FAIL due to buggy trigger on known good user, but passed")
	}
	if len(report.FalsePositives) == 0 {
		t.Fatalf("expected false positives, got 0")
	}
	if report.FalsePositives[0].TriggerID != "buggy_david_rule" {
		t.Errorf("expected TriggerID 'buggy_david_rule', got %s", report.FalsePositives[0].TriggerID)
	}
}
