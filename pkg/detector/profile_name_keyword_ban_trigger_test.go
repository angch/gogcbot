package detector

import (
	"strings"
	"testing"

	"github.com/angch/gogcbot/pkg/db"
)

// The calibrated spam-keyword families (normalized form) from the classified
// banned profile-name data.
var spamProfileKeywords = []string{"0壹天", "每日", "吴压", "吾思", "兼织"}

// Farm profile names that MUST be caught (after homoglyph normalization).
func TestProfileNameKeywordBanScore_Calibration(t *testing.T) {
	farm := []string{
		"六o0壹天", // -> 六00壹天 hits 0壹天
		"玖Oo壹天", // -> 玖00壹天 hits 0壹天
		"每日2000吴压",
		"吾思兼织",
	}
	for _, n := range farm {
		if s := ProfileNameKeywordBanScore(n, spamProfileKeywords); s < 3 {
			t.Errorf("banned profile name %q scored %d < 3 (missed keyword ban)", n, s)
		}
	}
}

// Clean human names (from the clean corpus) must NOT trigger.
func TestProfileNameKeywordScore_NoFalsePositives(t *testing.T) {
	clean := []string{
		"Ang ChinHan", "Albertine", "Catherine Aguilar", "Han Hui Teoh",
		"Michael Leow", "Sahil Khan", "Tara Wells", "Zhengqun Koo",
		"David Brown", "Ilario",
	}
	for _, n := range clean {
		if s := ProfileNameKeywordBanScore(n, spamProfileKeywords); s >= 3 {
			t.Errorf("clean name %q scored %d >= 3 (false positive)", n, s)
		}
	}
}

// Homoglyph folding must unify the o/O/0 variants of the 壹天 family into the
// canonical 0壹天 substring (the leading CJK char 六/玖 legitimately differs).
func TestNormalizeProfileName_Homoglyphs(t *testing.T) {
	variants := []string{"六o0壹天", "六Oo壹天", "六0o壹天", "玖0o壹天"}
	for _, v := range variants {
		got := NormalizeProfileName(v)
		if want := "0壹天"; !strings.Contains(got, want) {
			t.Errorf("normalize(%q) = %q, want it to contain %q", v, got, want)
		}
	}
	if got := NormalizeProfileName("A1ice"); got != "a1ice" {
		t.Errorf("Normalize(A1ice) = %q, want a1ice", got)
	}
	if got := NormalizeProfileName("L0ra"); got != "10ra" {
		t.Errorf("Normalize(L0ra) = %q, want 10ra", got)
	}
}

func TestProfileNameKeywordBanTrigger_Evaluate(t *testing.T) {
	cfg := ProfileNameKeywordBanConfig{
		Enabled:         true,
		MinHighUserID:   1000000000,
		MaxReputation:   5,
		MaxUserPosts:    5,
		MinScore:        3,
		FlagOnly:        true,
		BlockedKeywords: spamProfileKeywords,
	}
	trig := NewProfileNameKeywordBanTrigger(cfg)

	tests := []struct {
		name          string
		ctx           *TriggerContext
		wantTriggered bool
	}{
		{
			name: "new high-ID user with spam profile name",
			ctx: &TriggerContext{
				IsNewUser: true,
				User:      &db.User{UserID: 5000000000, Reputation: 0, FirstName: "六o0壹天"},
			},
			wantTriggered: true,
		},
		{
			name: "new high-ID user with clean profile name",
			ctx: &TriggerContext{
				IsNewUser: true,
				User:      &db.User{UserID: 5000000000, Reputation: 0, FirstName: "Michael", LastName: "Lee"},
			},
			wantTriggered: false,
		},
		{
			name: "established user with many posts skipped",
			ctx: &TriggerContext{
				IsNewUser:        false,
				UserMessageCount: 15,
				User:             &db.User{UserID: 5000000000, Reputation: 0, FirstName: "六o0壹天"},
			},
			wantTriggered: false,
		},
		{
			name: "low ID user skipped",
			ctx: &TriggerContext{
				IsNewUser: true,
				User:      &db.User{UserID: 123456, Reputation: 0, FirstName: "六o0壹天"},
			},
			wantTriggered: false,
		},
		{
			name: "high rep user skipped",
			ctx: &TriggerContext{
				IsNewUser: true,
				User:      &db.User{UserID: 5000000000, Reputation: 50, FirstName: "六o0壹天"},
			},
			wantTriggered: false,
		},
		{
			name: "empty name not triggered",
			ctx: &TriggerContext{
				IsNewUser: true,
				User:      &db.User{UserID: 5000000000, Reputation: 0},
			},
			wantTriggered: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := trig.Evaluate(tt.ctx)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if res.Triggered != tt.wantTriggered {
				t.Fatalf("Triggered = %v, want %v (reason: %s)", res.Triggered, tt.wantTriggered, res.Reason)
			}
		})
	}
}

func TestProfileNameKeywordBanTrigger_BanMode(t *testing.T) {
	cfg := ProfileNameKeywordBanConfig{
		Enabled:         true,
		MinHighUserID:   1000000000,
		MaxUserPosts:    5,
		MinScore:        3,
		FlagOnly:        false,
		BlockedKeywords: spamProfileKeywords,
	}
	trig := NewProfileNameKeywordBanTrigger(cfg)
	res, err := trig.Evaluate(&TriggerContext{
		IsNewUser: true,
		User:      &db.User{UserID: 5000000000, Reputation: 0, FirstName: "每日", LastName: "2000"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.Triggered {
		t.Fatal("expected trigger in ban mode")
	}
	if len(res.Actions) != 4 {
		t.Fatalf("ban mode should emit 4 actions, got %d", len(res.Actions))
	}
	var hasDelete, hasBan, hasRep, hasFlag bool
	for _, a := range res.Actions {
		switch a.Type {
		case ActionDeleteMessage:
			hasDelete = true
		case ActionBanUser:
			hasBan = true
		case ActionAdjustReputation:
			hasRep = true
		case ActionFlagMessage:
			hasFlag = true
		}
	}
	if !hasDelete || !hasBan || !hasRep || !hasFlag {
		t.Errorf("ban mode actions incomplete: %#v", res.Actions)
	}
}
