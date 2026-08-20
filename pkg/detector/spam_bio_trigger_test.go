package detector

import (
	"strings"
	"testing"

	"github.com/angch/gogcbot/pkg/db"
)

func TestNewUserSpamBioTrigger_Evaluate(t *testing.T) {
	cfg := NewUserSpamBioTriggerConfig{
		Enabled:        true,
		MaxReputation:  5,
		MaxUserPosts:   5,
		RepPenalty:     20,
		CustomKeywords: []string{"custom_scam_keyword"},
	}

	trig := NewNewUserSpamBioTrigger(cfg)

	if trig.ID() != "new_user_spam_bio" {
		t.Errorf("expected ID 'new_user_spam_bio', got %q", trig.ID())
	}
	if !trig.IsEnabled() {
		t.Errorf("expected trigger to be enabled")
	}

	tests := []struct {
		name                 string
		user                 *db.User
		userMsgCount         int
		bio                  string
		personalChatTitle    string
		personalChatUsername string
		businessIntro        string
		wantTriggered        bool
		wantKeyword          string
	}{
		{
			name: "New user with default spam keyword in bio (e.g. 沃尔玛)",
			user: &db.User{
				UserID:     888999,
				Reputation: 0,
			},
			userMsgCount:  1,
			bio:           "锦鲤代发 @mmmmue 6折础油卡E卡、沃尔玛、永辉、携程。联系 @xgshenqing888",
			wantTriggered: true,
			wantKeyword:   "沃尔玛",
		},
		{
			name: "New user with custom keyword in bio",
			user: &db.User{
				UserID:     999111,
				Reputation: 0,
			},
			userMsgCount:  0,
			bio:           "Check this out: custom_scam_keyword here",
			wantTriggered: true,
			wantKeyword:   "custom_scam_keyword",
		},
		{
			name: "New user with spam keyword in personal channel title",
			user: &db.User{
				UserID:     333111,
				Reputation: 0,
			},
			userMsgCount:         0,
			bio:                  "",
			personalChatTitle:    "6折油卡代发专区",
			personalChatUsername: "youkaspam",
			wantTriggered:        true,
			wantKeyword:          "油卡",
		},
		{
			name: "New user with spam keyword in business intro",
			user: &db.User{
				UserID:     333222,
				Reputation: 0,
			},
			userMsgCount:  0,
			bio:           "",
			businessIntro: "招兼职日结，每天200-500，加微信咨询",
			wantTriggered: true,
			wantKeyword:   "兼职",
		},
		{
			name: "Clean bio user",
			user: &db.User{
				UserID:     777111,
				Reputation: 0,
			},
			userMsgCount:  1,
			bio:           "Just a regular Telegram user bio",
			wantTriggered: false,
		},
		{
			name: "Empty bio user",
			user: &db.User{
				UserID:     777222,
				Reputation: 0,
			},
			userMsgCount:  0,
			bio:           "",
			wantTriggered: false,
		},
		{
			name: "Established user with high message count (>5)",
			user: &db.User{
				UserID:     666111,
				Reputation: 0,
			},
			userMsgCount:  10,
			bio:           "代发 沃尔玛",
			wantTriggered: false,
		},
		{
			name: "High reputation user (>5)",
			user: &db.User{
				UserID:     555111,
				Reputation: 50,
			},
			userMsgCount:  1,
			bio:           "代发 沃尔玛",
			wantTriggered: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := &TriggerContext{
				User:                 tc.user,
				UserBio:              tc.bio,
				PersonalChatTitle:    tc.personalChatTitle,
				PersonalChatUsername: tc.personalChatUsername,
				BusinessIntro:        tc.businessIntro,
				UserMessageCount:     tc.userMsgCount,
			}

			res, err := trig.Evaluate(ctx)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if res.Triggered != tc.wantTriggered {
				t.Errorf("expected triggered=%v, got %v (res: %+v)", tc.wantTriggered, res.Triggered, res)
			}
			if tc.wantTriggered {
				if tc.wantKeyword != "" && !strings.Contains(res.Reason, tc.wantKeyword) {
					t.Errorf("expected reason to contain %q, got %q", tc.wantKeyword, res.Reason)
				}
				if len(res.Actions) != 3 {
					t.Errorf("expected 3 actions, got %d", len(res.Actions))
				}
			}
		})
	}
}

func TestNewUserSpamBioTrigger_Disabled(t *testing.T) {
	cfg := NewUserSpamBioTriggerConfig{
		Enabled: false,
	}
	trig := NewNewUserSpamBioTrigger(cfg)
	if trig.IsEnabled() {
		t.Errorf("expected trigger to be disabled")
	}
	ctx := &TriggerContext{
		User:    &db.User{UserID: 123, Reputation: 0},
		UserBio: "沃尔玛 礼品卡 代发",
	}
	res, err := trig.Evaluate(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Triggered {
		t.Errorf("expected triggered=false when disabled")
	}
}
