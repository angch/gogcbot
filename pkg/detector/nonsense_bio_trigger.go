package detector

import (
	"strings"
)

// NonsenseBioTriggerConfig configures thresholds for the nonsense/filler English
// profile bio detection trigger.
//
// It shares the new/high-ID/low-rep cohort gate with the sibling triggers, so
// established members are never evaluated. Filler bios are sentence-shaped,
// semantically empty English (e.g. "Scientist near entire offer stay.") that
// spam-farms use as profile padding. Unlike new_user_spam_bio, which matches a
// phrase blocklist, this trigger matches the SHAPE of the text, so it catches
// unseen rephrasings rather than only already-seen phrases.
type NonsenseBioTriggerConfig struct {
	Enabled       bool  `mapstructure:"enabled" yaml:"enabled"`
	MinHighUserID int64 `mapstructure:"min_high_user_id" yaml:"min_high_user_id"`
	MaxReputation int   `mapstructure:"max_reputation" yaml:"max_reputation"`
	MaxUserPosts  int   `mapstructure:"max_user_posts" yaml:"max_user_posts"`
	// MinWords is the minimum number of ASCII words a sentence-shaped bio must
	// contain to be considered filler. Real bios are short fragments; filler is
	// padded to a full clause. Default 5.
	MinWords int `mapstructure:"min_words" yaml:"min_words"`
	// FlagOnly emits only a flag/report action instead of auto-banning.
	// Recommended true: a shape heuristic is weaker than a known phrase.
	FlagOnly   bool `mapstructure:"flag_only" yaml:"flag_only"`
	RepPenalty int  `mapstructure:"rep_penalty" yaml:"rep_penalty"`
}

// NonsenseBioTrigger detects new users whose profile bio or linked-channel
// prose is a sentence-shaped, semantically empty English filler string.
type NonsenseBioTrigger struct {
	cfg NonsenseBioTriggerConfig
}

// NewNonsenseBioTrigger constructs a new NonsenseBioTrigger.
func NewNonsenseBioTrigger(cfg NonsenseBioTriggerConfig) *NonsenseBioTrigger {
	return &NonsenseBioTrigger{cfg: cfg}
}

func (t *NonsenseBioTrigger) ID() string {
	return "nonsense_bio"
}

func (t *NonsenseBioTrigger) Name() string {
	return "New High-ID User Nonsense Bio Detection"
}

func (t *NonsenseBioTrigger) IsEnabled() bool {
	return t.cfg.Enabled
}

func (t *NonsenseBioTrigger) Evaluate(ctx *TriggerContext) (*TriggerResult, error) {
	if !t.IsEnabled() || !MatchesCohort(ctx, t.cfg.MinHighUserID, t.cfg.MaxReputation, t.cfg.MaxUserPosts) {
		return &TriggerResult{Triggered: false}, nil
	}

	minWords := t.cfg.MinWords
	if minWords <= 0 {
		minWords = 5
	}

	// Scan the free-text profile prose fields (excluding the channel @username,
	// which is a handle, not prose, and is covered by new_user_spam_bio).
	var profileTexts []string
	if strings.TrimSpace(ctx.UserBio) != "" {
		profileTexts = append(profileTexts, ctx.UserBio)
	}
	if strings.TrimSpace(ctx.PersonalChatTitle) != "" {
		profileTexts = append(profileTexts, ctx.PersonalChatTitle)
	}
	if strings.TrimSpace(ctx.BusinessIntro) != "" {
		profileTexts = append(profileTexts, ctx.BusinessIntro)
	}

	matched := false
	for _, text := range profileTexts {
		if IsNonsenseBio(text, minWords) {
			matched = true
			break
		}
	}
	if !matched {
		return &TriggerResult{Triggered: false}, nil
	}

	repPenalty := t.cfg.RepPenalty
	if repPenalty <= 0 {
		repPenalty = 20
	}

	reason := "Detection trigger (nonsense_bio): High-ID new user with low/no rep has a sentence-shaped filler profile bio"

	actions := []Action{
		{Type: ActionFlagMessage, Reason: reason},
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

// IsNonsenseBio returns true if text reads as a sentence-shaped, semantically
// empty English filler string. All of the following must hold:
//
//   - the text is a complete sentence ending in a period
//   - it contains at least minWords plain ASCII alphabetic words
//   - it contains no digits, no '@', and no non-ASCII characters (contact info,
//     CJK/emoji names, and ad copy are handled by the sibling triggers)
func IsNonsenseBio(text string, minWords int) bool {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return false
	}
	if !strings.HasSuffix(trimmed, ".") {
		return false
	}
	if minWords <= 0 {
		minWords = 5
	}

	words := 0
	for _, field := range strings.Fields(trimmed) {
		w := field
		// Strip trailing sentence punctuation so the final word classifies.
		for strings.HasSuffix(w, ".") || strings.HasSuffix(w, ",") || strings.HasSuffix(w, "?") || strings.HasSuffix(w, "!") {
			w = strings.TrimSuffix(w, ".")
			w = strings.TrimSuffix(w, ",")
			w = strings.TrimSuffix(w, "?")
			w = strings.TrimSuffix(w, "!")
		}
		if w == "" {
			continue
		}
		// A handle, a number, or any non-ASCII/non-letter character disqualifies
		// the filler class (those are ad copy or contact info, not padding).
		if strings.ContainsAny(w, "@0123456789") {
			return false
		}
		for _, r := range w {
			if r < 0x41 || (r > 0x5A && r < 0x61) || r > 0x7A {
				return false
			}
		}
		words++
	}

	return words >= minWords
}
