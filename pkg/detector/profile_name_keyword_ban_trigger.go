package detector

import (
	"strings"
)

// ProfileNameKeywordBanConfig configures a keyword blocklist applied to the
// user's Telegram profile name.
//
// It shares the new/high-ID/low-rep cohort gate with UsernameAnomalyTrigger, so
// established members are never evaluated. Names are homoglyph-normalized before
// being matched against the configured spam-keyword families (e.g. "六o0壹天"
// and "玖Oo壹天" both hit the normalized keyword "0壹天").
type ProfileNameKeywordBanConfig struct {
	Enabled       bool  `mapstructure:"enabled" yaml:"enabled"`
	MinHighUserID int64 `mapstructure:"min_high_user_id" yaml:"min_high_user_id"`
	MaxReputation int   `mapstructure:"max_reputation" yaml:"max_reputation"`
	MaxUserPosts  int   `mapstructure:"max_user_posts" yaml:"max_user_posts"`
	// MinScore is the matched-keyword threshold above which a profile name
	// triggers. Each distinct matched keyword adds +3, so the default of 3 fires
	// on any single keyword match.
	MinScore int `mapstructure:"min_score" yaml:"min_score"`
	// FlagOnly emits only a flag/report action instead of auto-banning.
	FlagOnly bool `mapstructure:"flag_only" yaml:"flag_only"`
	// RepPenalty is applied when not in flag-only mode.
	RepPenalty int `mapstructure:"rep_penalty" yaml:"rep_penalty"`
	// BlockedKeywords are spam words matched against the homoglyph-normalized
	// profile name. Values must already be in normalized form.
	BlockedKeywords []string `mapstructure:"blocked_keywords" yaml:"blocked_keywords"`
}

// ProfileNameKeywordBanTrigger detects brand new, high-ID, low-rep users whose
// profile name contains a known spam keyword (e.g. "六o0壹天").
type ProfileNameKeywordBanTrigger struct {
	cfg ProfileNameKeywordBanConfig
}

// NewProfileNameKeywordBanTrigger constructs a new ProfileNameKeywordBanTrigger.
func NewProfileNameKeywordBanTrigger(cfg ProfileNameKeywordBanConfig) *ProfileNameKeywordBanTrigger {
	return &ProfileNameKeywordBanTrigger{cfg: cfg}
}

func (t *ProfileNameKeywordBanTrigger) ID() string {
	return "profile_name_keyword_ban"
}

func (t *ProfileNameKeywordBanTrigger) Name() string {
	return "New High-ID User Spam Profile Name Keyword Ban"
}

func (t *ProfileNameKeywordBanTrigger) IsEnabled() bool {
	return t.cfg.Enabled
}

func (t *ProfileNameKeywordBanTrigger) Evaluate(ctx *TriggerContext) (*TriggerResult, error) {
	if !t.IsEnabled() || !MatchesCohort(ctx, t.cfg.MinHighUserID, t.cfg.MaxReputation, t.cfg.MaxUserPosts) {
		return &TriggerResult{Triggered: false}, nil
	}

	name := ctx.DisplayName()
	if name == "" || len(t.cfg.BlockedKeywords) == 0 {
		return &TriggerResult{Triggered: false}, nil
	}

	score := ProfileNameKeywordBanScore(name, t.cfg.BlockedKeywords)
	minScore := t.cfg.MinScore
	if minScore <= 0 {
		minScore = 3
	}
	if score < minScore {
		return &TriggerResult{Triggered: false}, nil
	}

	repPenalty := t.cfg.RepPenalty
	if repPenalty <= 0 {
		repPenalty = 20
	}

	reason := "Detection trigger (profile_name_keyword_ban): High-ID new user with low/no rep has a spam-keyword profile name"

	actions := []Action{
		{
			Type:   ActionFlagMessage,
			Reason: reason,
		},
	}
	if !t.cfg.FlagOnly {
		actions = append(actions,
			Action{Type: ActionDeleteMessage, Reason: reason},
			Action{Type: ActionBanUser, Reason: reason},
			Action{Type: ActionAdjustReputation, RepDelta: -repPenalty, Reason: reason},
		)
	}

	return &TriggerResult{
		Triggered: true,
		TriggerID: t.ID(),
		Reason:    reason,
		Actions:   actions,
	}, nil
}

// ProfileNameKeywordBanScore scores a profile name by counting distinct matched
// keywords from the configured blocklist. Each distinct matched keyword adds +3.
// Names are homoglyph-normalized before matching, so "六o0壹天" and "玖Oo壹天"
// both match the keyword "0壹天". Returns 0 when no keyword matches.
func ProfileNameKeywordBanScore(name string, keywords []string) int {
	if len(keywords) == 0 {
		return 0
	}
	norm := NormalizeProfileName(name)
	score := 0
	seen := map[string]bool{}
	for _, kw := range keywords {
		kn := NormalizeProfileName(kw)
		if kn == "" || seen[kn] {
			continue
		}
		seen[kn] = true
		if strings.Contains(norm, kn) {
			score += 3
		}
	}
	return score
}
