package detector

import (
	"testing"

	"github.com/angch/gogcbot/pkg/db"
)

func TestNonsenseBioTrigger_Evaluate(t *testing.T) {
	cfg := NonsenseBioTriggerConfig{
		Enabled:       true,
		MinHighUserID: 1000000000,
		MaxReputation: 5,
		MaxUserPosts:  5,
		MinWords:      5,
		FlagOnly:      true,
		RepPenalty:    20,
	}
	trig := NewNonsenseBioTrigger(cfg)

	if trig.ID() != "nonsense_bio" {
		t.Errorf("expected ID 'nonsense_bio', got %q", trig.ID())
	}
	if !trig.IsEnabled() {
		t.Errorf("expected trigger to be enabled")
	}

	tests := []struct {
		name              string
		user              *db.User
		userMsgCount      int
		isNewUser         bool
		bio               string
		personalChatTitle string
		businessIntro     string
		wantTriggered     bool
	}{
		{
			name: "filler bio: Scientist near entire offer stay.",
			user: &db.User{UserID: 8670811039, Reputation: 0},
			bio:  "Scientist near entire offer stay.",
			wantTriggered: true,
		},
		{
			name: "filler bio: Serious perhaps its maintain material.",
			user: &db.User{UserID: 8703314204, Reputation: 1},
			bio:  "Serious perhaps its maintain material.",
			wantTriggered: true,
		},
		{
			name: "filler bio: Go community and mother hot everybody certain.",
			user: &db.User{UserID: 8891843593, Reputation: 0},
			bio:  "Go community and mother hot everybody certain.",
			wantTriggered: true,
		},
		{
			name: "filler bio: Provide strategy marriage rich hour night movie.",
			user: &db.User{UserID: 8893808775, Reputation: 1},
			bio:  "Provide strategy marriage rich hour night movie.",
			wantTriggered: true,
		},
		{
			name: "filler bio: Beat listen short more trouble heart.",
			user: &db.User{UserID: 8962578902, Reputation: 0},
			bio:  "Beat listen short more trouble heart.",
			wantTriggered: true,
		},
		{
			name: "gap case: Hour thought real million thing leg present standard.",
			user: &db.User{UserID: 8964159831, Reputation: 0},
			bio:  "Hour thought real million thing leg present standard.",
			wantTriggered: true,
		},
		{
			name: "filler in personal chat title",
			user: &db.User{UserID: 2000000000, Reputation: 0},
			personalChatTitle: "Every morning helps decide living quality over.",
			wantTriggered:     true,
		},
		{
			name: "filler in business intro",
			user: &db.User{UserID: 2000000001, Reputation: 0},
			businessIntro:     "Team around finish distance easily matter.",
			wantTriggered:     true,
		},
		{
			name: "legit short fragment bio (Queen)",
			user: &db.User{UserID: 2000000002, Reputation: 0},
			bio:  "Queen",
			wantTriggered: false,
		},
		{
			name: "legit fragment bio (Vinyl records and vintage finds)",
			user: &db.User{UserID: 2000000003, Reputation: 0},
			bio:  "Vinyl records and vintage finds",
			wantTriggered: false,
		},
		{
			name: "legit short sentence bio (Progress, not perfection.)",
			user: &db.User{UserID: 2000000004, Reputation: 0},
			bio:  "Progress, not perfection.",
			wantTriggered: false,
		},
		{
			name: "bio with digits (ad copy, not filler)",
			user: &db.User{UserID: 2000000005, Reputation: 0},
			bio:  "Big life every day 2000 work.",
			wantTriggered: false,
		},
		{
			name: "bio with contact handle (ad copy, not filler)",
			user: &db.User{UserID: 2000000006, Reputation: 0},
			bio:  "Join the community today @handler.",
			wantTriggered: false,
		},
		{
			name: "bio with non-ASCII (CJK, handled by sibling)",
			user: &db.User{UserID: 2000000007, Reputation: 0},
			bio:  "每日2000吴压",
			wantTriggered: false,
		},
		{
			name: "no trailing period",
			user: &db.User{UserID: 2000000008, Reputation: 0},
			bio:  "Every morning helps decide living quality over",
			wantTriggered: false,
		},
		{
			name: "empty bio",
			user: &db.User{UserID: 2000000009, Reputation: 0},
			bio:  "",
			wantTriggered: false,
		},
		{
			name: "established user (message count > max_posts)",
			user: &db.User{UserID: 2000000010, Reputation: 0},
			userMsgCount:  6,
			isNewUser:     false,
			bio:           "Scientist near entire offer stay.",
			wantTriggered: false,
		},
		{
			name: "high reputation user (> max_rep)",
			user: &db.User{UserID: 2000000011, Reputation: 50},
			bio:  "Scientist near entire offer stay.",
			wantTriggered: false,
		},
		{
			name: "low ID user (below min_high_user_id)",
			user: &db.User{UserID: 867081, Reputation: 0},
			// same filler bio but below threshold => cohort gate rejects
			bio:           "Scientist near entire offer stay.",
			wantTriggered: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := &TriggerContext{
				User:              tc.user,
				UserMessageCount:  tc.userMsgCount,
				UserBio:           tc.bio,
				PersonalChatTitle: tc.personalChatTitle,
				BusinessIntro:     tc.businessIntro,
				IsNewUser:         tc.isNewUser,
			}
			res, err := trig.Evaluate(ctx)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if res.Triggered != tc.wantTriggered {
				t.Errorf("triggered=%v, want %v", res.Triggered, tc.wantTriggered)
			}
		})
	}
}

func TestNonsenseBioTrigger_Disabled(t *testing.T) {
	cfg := NonsenseBioTriggerConfig{Enabled: false}
	trig := NewNonsenseBioTrigger(cfg)
	if trig.IsEnabled() {
		t.Errorf("expected trigger to be disabled")
	}
	ctx := &TriggerContext{
		User: &db.User{UserID: 8670811039, Reputation: 0},
		UserBio: "Scientist near entire offer stay.",
	}
	res, err := trig.Evaluate(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Triggered {
		t.Errorf("expected triggered=false when disabled")
	}
}

func TestNonsenseBioTrigger_FlagOnly(t *testing.T) {
	cfg := NonsenseBioTriggerConfig{
		Enabled:       true,
		MinHighUserID: 1000000000,
		MaxReputation: 5,
		MaxUserPosts:  5,
		MinWords:      5,
		FlagOnly:      true,
		RepPenalty:    20,
	}
	trig := NewNonsenseBioTrigger(cfg)
	ctx := &TriggerContext{
		User:         &db.User{UserID: 8670811039, Reputation: 0},
		UserBio:      "Scientist near entire offer stay.",
		IsNewUser:    true,
	}
	res, err := trig.Evaluate(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.Triggered {
		t.Fatalf("expected triggered=true")
	}
	hasBan := false
	for _, a := range res.Actions {
		if a.Type == ActionBanUser {
			hasBan = true
		}
	}
	if hasBan {
		t.Errorf("flag_only trigger must not emit ActionBanUser")
	}
	wantFlag := false
	for _, a := range res.Actions {
		if a.Type == ActionFlagMessage {
			wantFlag = true
		}
	}
	if !wantFlag {
		t.Errorf("flag_only trigger must emit ActionFlagMessage")
	}
}
