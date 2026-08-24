package detector

import (
	"fmt"
)

// SecondaryGroupTriggerConfig configures thresholds for the secondary group join detection trigger.
type SecondaryGroupTriggerConfig struct {
	Enabled       bool `mapstructure:"enabled" yaml:"enabled"`
	MaxReputation int  `mapstructure:"max_reputation" yaml:"max_reputation"`
	RepPenalty    int  `mapstructure:"rep_penalty" yaml:"rep_penalty"`
}

// SecondaryGroupTrigger detects users joining a secondary group without being a member of the primary group.
type SecondaryGroupTrigger struct {
	cfg SecondaryGroupTriggerConfig
}

// NewSecondaryGroupTrigger constructs a new SecondaryGroupTrigger.
func NewSecondaryGroupTrigger(cfg SecondaryGroupTriggerConfig) *SecondaryGroupTrigger {
	return &SecondaryGroupTrigger{
		cfg: cfg,
	}
}

func (t *SecondaryGroupTrigger) ID() string {
	return "secondary_group_not_in_primary"
}

func (t *SecondaryGroupTrigger) Name() string {
	return "Secondary Group Join Without Primary Group Membership Detection"
}

func (t *SecondaryGroupTrigger) IsEnabled() bool {
	return t.cfg.Enabled
}

func (t *SecondaryGroupTrigger) Evaluate(ctx *TriggerContext) (*TriggerResult, error) {
	if !t.IsEnabled() || ctx == nil || ctx.User == nil {
		return &TriggerResult{Triggered: false}, nil
	}

	// Exempt admins and whitelisted users (reputation >= 100)
	if ctx.User.IsAdmin || ctx.User.Reputation >= 100 {
		return &TriggerResult{Triggered: false}, nil
	}

	// Exempt users whose reputation exceeds MaxReputation threshold (e.g. rep >= 50 or rep > maxRep)
	maxRep := t.cfg.MaxReputation
	if maxRep <= 0 {
		maxRep = 5
	}
	if ctx.User.Reputation > maxRep {
		return &TriggerResult{Triggered: false}, nil
	}

	// Only triggers when the chat is a secondary group and user is not verified as a member of primary group
	if !ctx.IsSecondaryGroup || ctx.IsMemberOfPrimaryGroup {
		return &TriggerResult{Triggered: false}, nil
	}

	repPenalty := t.cfg.RepPenalty
	if repPenalty <= 0 {
		repPenalty = 20
	}

	groupName := ctx.GroupTitle
	if groupName == "" {
		groupName = fmt.Sprintf("Chat %d", ctx.ChatID)
	}

	primaryDesc := ctx.PrimaryGroupNames
	if primaryDesc == "" {
		primaryDesc = "primary group"
	}

	reason := fmt.Sprintf("Detection trigger (secondary_group_not_in_primary): User joined secondary group '%s' (%d) without being a member of %s", groupName, ctx.ChatID, primaryDesc)

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
