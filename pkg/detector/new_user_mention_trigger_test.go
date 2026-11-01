package detector

import (
	"testing"

	"github.com/angch/gogcbot/pkg/db"
)

func TestNewUserMentionTrigger(t *testing.T) {
	defaultCfg := NewUserMentionTriggerConfig{
		Enabled:       true,
		MaxReputation: 5,
		MaxUserPosts:  5,
		RepPenalty:    50,
	}

	tests := []struct {
		name          string
		cfg           NewUserMentionTriggerConfig
		ctx           *TriggerContext
		wantTriggered bool
	}{
		{
			name: "New user mentions non-member bots",
			cfg:  defaultCfg,
			ctx: &TriggerContext{
				User: &db.User{
					UserID:     1079944493,
					Username:   "dfgh1566z",
					Reputation: 0,
				},
				IsNewUser:        true,
				UserMessageCount: 1,
				MentionedEntities: []MentionedEntity{
					{Username: "kc15_cghbot", IsBot: true, IsInGroup: false},
					{Username: "jjskh_o5bot", IsBot: true, IsInGroup: false},
					{Username: "w_7fnotwbot", IsBot: true, IsInGroup: false},
				},
			},
			wantTriggered: true,
		},
		{
			name: "New user mentions non-member regular user",
			cfg:  defaultCfg,
			ctx: &TriggerContext{
				User: &db.User{
					UserID:     1079944493,
					Username:   "dfgh1566z",
					Reputation: 0,
				},
				IsNewUser:        true,
				UserMessageCount: 1,
				MentionedEntities: []MentionedEntity{
					{Username: "external_user", IsBot: false, IsInGroup: false},
				},
			},
			wantTriggered: true,
		},
		{
			name: "New user mentions members who ARE in the group",
			cfg:  defaultCfg,
			ctx: &TriggerContext{
				User: &db.User{
					UserID:     1079944493,
					Username:   "dfgh1566z",
					Reputation: 0,
				},
				IsNewUser:        true,
				UserMessageCount: 1,
				MentionedEntities: []MentionedEntity{
					{Username: "group_admin", IsBot: false, IsInGroup: true},
					{Username: "helpful_bot", IsBot: true, IsInGroup: true},
				},
			},
			wantTriggered: false,
		},
		{
			name: "Established user with high reputation mentions non-member (exempt)",
			cfg:  defaultCfg,
			ctx: &TriggerContext{
				User: &db.User{
					UserID:     2000000001,
					Username:   "veteran_member",
					Reputation: 50,
				},
				IsNewUser:        false,
				UserMessageCount: 100,
				MentionedEntities: []MentionedEntity{
					{Username: "external_friend", IsBot: false, IsInGroup: false},
				},
			},
			wantTriggered: false,
		},
		{
			name: "Established user with many posts (exceeds max_user_posts) mentions non-member (exempt)",
			cfg:  defaultCfg,
			ctx: &TriggerContext{
				User: &db.User{
					UserID:     2000000002,
					Username:   "regular_poster",
					Reputation: 0,
				},
				IsNewUser:        false,
				UserMessageCount: 20,
				MentionedEntities: []MentionedEntity{
					{Username: "external_user", IsBot: false, IsInGroup: false},
				},
			},
			wantTriggered: false,
		},
		{
			name: "Admin mentions non-member (exempt)",
			cfg:  defaultCfg,
			ctx: &TriggerContext{
				User: &db.User{
					UserID:     123456,
					Username:   "admin",
					Reputation: 100,
					IsAdmin:    true,
				},
				IsNewUser:        true,
				UserMessageCount: 0,
				MentionedEntities: []MentionedEntity{
					{Username: "bad_bot", IsBot: true, IsInGroup: false},
				},
			},
			wantTriggered: false,
		},
		{
			name: "Whitelisted user (rep >= 100) mentions non-member (exempt)",
			cfg:  defaultCfg,
			ctx: &TriggerContext{
				User: &db.User{
					UserID:     999999,
					Username:   "whitelisted",
					Reputation: 100,
				},
				IsNewUser:        true,
				UserMessageCount: 0,
				MentionedEntities: []MentionedEntity{
					{Username: "bad_bot", IsBot: true, IsInGroup: false},
				},
			},
			wantTriggered: false,
		},
		{
			name: "No mentions at all",
			cfg:  defaultCfg,
			ctx: &TriggerContext{
				User: &db.User{
					UserID:     1079944493,
					Username:   "newbie",
					Reputation: 0,
				},
				IsNewUser:         true,
				UserMessageCount:  1,
				MentionedEntities: nil,
			},
			wantTriggered: false,
		},
		{
			name: "Trigger disabled",
			cfg: NewUserMentionTriggerConfig{
				Enabled: false,
			},
			ctx: &TriggerContext{
				User: &db.User{
					UserID:     1079944493,
					Username:   "newbie",
					Reputation: 0,
				},
				IsNewUser:        true,
				UserMessageCount: 1,
				MentionedEntities: []MentionedEntity{
					{Username: "kc15_cghbot", IsBot: true, IsInGroup: false},
				},
			},
			wantTriggered: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			trigger := NewNewUserMentionTrigger(tt.cfg)
			res, err := trigger.Evaluate(tt.ctx)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if res.Triggered != tt.wantTriggered {
				t.Errorf("got triggered=%v, want %v", res.Triggered, tt.wantTriggered)
			}
			if tt.wantTriggered {
				if len(res.Actions) != 3 {
					t.Fatalf("expected 3 actions, got %d", len(res.Actions))
				}
				if res.Actions[0].Type != ActionDeleteMessage {
					t.Errorf("expected ActionDeleteMessage, got %v", res.Actions[0].Type)
				}
				if res.Actions[1].Type != ActionBanUser {
					t.Errorf("expected ActionBanUser, got %v", res.Actions[1].Type)
				}
				if res.Actions[2].Type != ActionAdjustReputation || res.Actions[2].RepDelta != -tt.cfg.RepPenalty {
					t.Errorf("expected ActionAdjustReputation with delta %d, got %v", -tt.cfg.RepPenalty, res.Actions[2])
				}
			}
		})
	}
}
