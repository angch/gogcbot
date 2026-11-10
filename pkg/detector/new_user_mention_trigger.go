package detector

import (
	"fmt"
	"strings"
)

// NewUserMentionTriggerConfig configures thresholds for detecting new unknown users
// who mention or invite non-members (including bots) into monitored groups.
type NewUserMentionTriggerConfig struct {
	Enabled       bool  `mapstructure:"enabled" yaml:"enabled"`
	MinHighUserID int64 `mapstructure:"min_high_user_id" yaml:"min_high_user_id"` // Optional min user ID (0 to apply to any new user)
	MaxReputation int   `mapstructure:"max_reputation" yaml:"max_reputation"`     // Default 5
	MaxUserPosts  int   `mapstructure:"max_user_posts" yaml:"max_user_posts"`     // Default 5
	RepPenalty    int   `mapstructure:"rep_penalty" yaml:"rep_penalty"`           // Default 50
}

// NewUserMentionTrigger detects messages from brand-new, low-reputation users
// that mention other users or bots who are not already in the group.
type NewUserMentionTrigger struct {
	cfg NewUserMentionTriggerConfig
}

// NewNewUserMentionTrigger constructs a new NewUserMentionTrigger.
func NewNewUserMentionTrigger(cfg NewUserMentionTriggerConfig) *NewUserMentionTrigger {
	return &NewUserMentionTrigger{cfg: cfg}
}

func (t *NewUserMentionTrigger) ID() string {
	return "new_user_mention"
}

func (t *NewUserMentionTrigger) Name() string {
	return "New Unknown User Non-Member Mention & Bot Invite Detection"
}

func (t *NewUserMentionTrigger) IsEnabled() bool {
	return t.cfg.Enabled
}

func (t *NewUserMentionTrigger) Evaluate(ctx *TriggerContext) (*TriggerResult, error) {
	if !t.IsEnabled() || ctx == nil || ctx.User == nil {
		return &TriggerResult{Triggered: false}, nil
	}

	// Exempt admins and whitelisted users (reputation >= 100)
	if ctx.User.IsAdmin || ctx.User.Reputation >= 100 {
		return &TriggerResult{Triggered: false}, nil
	}

	maxRep := t.cfg.MaxReputation
	if maxRep <= 0 {
		maxRep = 5
	}
	if ctx.User.Reputation > maxRep {
		return &TriggerResult{Triggered: false}, nil
	}

	maxPosts := t.cfg.MaxUserPosts
	if maxPosts <= 0 {
		maxPosts = 5
	}
	isNewUser := ctx.IsNewUser || (ctx.UserMessageCount <= maxPosts)
	if !isNewUser {
		return &TriggerResult{Triggered: false}, nil
	}

	if t.cfg.MinHighUserID > 0 && ctx.User.UserID < t.cfg.MinHighUserID && !ctx.User.IsBot {
		return &TriggerResult{Triggered: false}, nil
	}

	// Check if any mentioned entity is not in the group
	var nonMemberNames []string
	for _, m := range ctx.MentionedEntities {
		if !m.IsInGroup {
			name := m.Username
			if name == "" && m.UserID != 0 {
				name = fmt.Sprintf("ID:%d", m.UserID)
			}
			if name != "" {
				if !strings.HasPrefix(name, "@") && !strings.HasPrefix(name, "ID:") {
					name = "@" + name
				}
				nonMemberNames = append(nonMemberNames, name)
			}
		}
	}

	if len(nonMemberNames) == 0 {
		return &TriggerResult{Triggered: false}, nil
	}

	repPenalty := t.cfg.RepPenalty
	if repPenalty <= 0 {
		repPenalty = 50
	}

	reason := fmt.Sprintf("Detection trigger (new_user_mention): New unknown user mentioned non-member user(s)/bot(s) (%s)", strings.Join(nonMemberNames, ", "))

	actions := []Action{
		{
			Type:   ActionDeleteMessage,
			Reason: reason,
		},
		{
			Type:   ActionBanUser,
			Reason: reason,
		},
		{
			Type:     ActionAdjustReputation,
			RepDelta: -repPenalty,
			Reason:   reason,
		},
	}

	return &TriggerResult{
		Triggered: true,
		TriggerID: t.ID(),
		Reason:    reason,
		Actions:   actions,
	}, nil
}
