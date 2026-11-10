package detector

import (
	"strings"
	"unicode"
)

// NewUserRussianTriggerConfig configures thresholds for the Russian/Cyrillic spam detection trigger.
type NewUserRussianTriggerConfig struct {
	Enabled          bool    `mapstructure:"enabled" yaml:"enabled"`
	MinHighUserID    int64   `mapstructure:"min_high_user_id" yaml:"min_high_user_id"`
	MaxReputation    int     `mapstructure:"max_reputation" yaml:"max_reputation"`
	MaxUserPosts     int     `mapstructure:"max_user_posts" yaml:"max_user_posts"`
	MinCyrillicChars int     `mapstructure:"min_cyrillic_chars" yaml:"min_cyrillic_chars"`
	MinRussianChars  int     `mapstructure:"min_russian_chars" yaml:"min_russian_chars"` // Backwards compatibility alias
	MinCyrillicRatio float64 `mapstructure:"min_cyrillic_ratio" yaml:"min_cyrillic_ratio"`
	RepPenalty       int     `mapstructure:"rep_penalty" yaml:"rep_penalty"`
}

// NewUserCyrillicTriggerConfig is a type alias for backwards compatibility.
type NewUserCyrillicTriggerConfig = NewUserRussianTriggerConfig

// NewUserRussianTrigger detects messages from brand new, high-ID users with low/no rep containing Russian/Cyrillic characters.
type NewUserRussianTrigger struct {
	cfg NewUserRussianTriggerConfig
}

// NewUserCyrillicTrigger is a type alias for backwards compatibility.
type NewUserCyrillicTrigger = NewUserRussianTrigger

// NewNewUserRussianTrigger constructs a new NewUserRussianTrigger.
func NewNewUserRussianTrigger(cfg NewUserRussianTriggerConfig) *NewUserRussianTrigger {
	return &NewUserRussianTrigger{
		cfg: cfg,
	}
}

// NewNewUserCyrillicTrigger is a backwards compatible constructor.
func NewNewUserCyrillicTrigger(cfg NewUserCyrillicTriggerConfig) *NewUserRussianTrigger {
	return NewNewUserRussianTrigger(cfg)
}

func (t *NewUserRussianTrigger) ID() string {
	return "new_user_russian"
}

func (t *NewUserRussianTrigger) Name() string {
	return "New High-ID User Russian/Cyrillic Message Detection"
}

func (t *NewUserRussianTrigger) IsEnabled() bool {
	return t.cfg.Enabled
}

func (t *NewUserRussianTrigger) Evaluate(ctx *TriggerContext) (*TriggerResult, error) {
	if !t.IsEnabled() || !MatchesCohort(ctx, t.cfg.MinHighUserID, t.cfg.MaxReputation, t.cfg.MaxUserPosts) {
		return &TriggerResult{Triggered: false}, nil
	}

	// Condition 0: Shieldy verification text message ("I am not a bot")
	if IsShieldyVerificationText(ctx.Text) {
		return &TriggerResult{Triggered: false}, nil
	}

	// Condition 1: Message text contains Russian/Cyrillic characters
	text := strings.TrimSpace(ctx.Text)
	if text == "" {
		return &TriggerResult{Triggered: false}, nil
	}

	if !t.ContainsCyrillic(text) {
		return &TriggerResult{Triggered: false}, nil
	}

	repPenalty := t.cfg.RepPenalty
	if repPenalty <= 0 {
		repPenalty = 20
	}

	reason := "Detection trigger (new_user_russian): High-ID new user with low/no rep sent message containing Russian/Cyrillic characters"

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

// IsCyrillic returns true if the rune is part of the Cyrillic script.
// It returns false for whitespace, punctuation, and symbols.
func IsCyrillic(r rune) bool {
	if unicode.IsSpace(r) || unicode.IsPunct(r) || unicode.IsSymbol(r) {
		return false
	}
	return unicode.Is(unicode.Cyrillic, r)
}

// ContainsCyrillic returns true if text contains enough Cyrillic script characters to be considered Russian/Cyrillic spam.
// It applies progressive thresholding that is forgiving for longer messages containing incidental Cyrillic characters:
//   - Short messages (<= 50 non-space runes): requires at least MinCyrillicChars (default 2) and Cyrillic ratio >= 0.10 (or >= 5 Cyrillic chars).
//   - Medium messages (51-150 non-space runes): requires at least 5 Cyrillic chars and Cyrillic ratio >= MinCyrillicRatio (default 0.15).
//   - Long messages (> 150 non-space runes): requires at least 10 Cyrillic chars and Cyrillic ratio >= MinCyrillicRatio (default 0.15),
//     or at least 25 Cyrillic chars (substantial Cyrillic block).
func (t *NewUserRussianTrigger) ContainsCyrillic(text string) bool {
	minChars := t.cfg.MinCyrillicChars
	if minChars <= 0 {
		minChars = t.cfg.MinRussianChars
	}
	if minChars <= 0 {
		minChars = 2
	}

	minRatio := t.cfg.MinCyrillicRatio
	if minRatio <= 0 {
		minRatio = 0.15
	}

	cyrillicCount := 0
	totalRunes := 0
	for _, r := range text {
		if unicode.IsSpace(r) {
			continue
		}
		totalRunes++
		if IsCyrillic(r) {
			cyrillicCount++
		}
	}

	if totalRunes == 0 || cyrillicCount < minChars {
		return false
	}

	ratio := float64(cyrillicCount) / float64(totalRunes)

	// Short messages (<= 50 non-space runes):
	// Spammers frequently post short Russian phrases (e.g. "Буду покупать юсдт, пишите", "Куплю USDT срочно").
	// Requires at least minChars Cyrillic characters and a minimal ratio of 0.10 (or >= 5 Cyrillic chars).
	if totalRunes <= 50 {
		return cyrillicCount >= 5 || ratio >= 0.10
	}

	// Medium messages (51 to 150 non-space runes):
	// Longer text requires at least 5 Cyrillic characters and meets minRatio (default 15%).
	if totalRunes <= 150 {
		return cyrillicCount >= 5 && ratio >= minRatio
	}

	// Long messages (> 150 non-space runes):
	// Long posts (such as technical articles or discussions) are forgiven if they contain
	// incidental Cyrillic mentions, quotes, or code. Require at least 10 Cyrillic characters and minRatio,
	// or an absolute Cyrillic character count >= 25 (a substantial Cyrillic block).
	return (cyrillicCount >= 10 && ratio >= minRatio) || cyrillicCount >= 25
}

// ContainsRussian is an alias for ContainsCyrillic.
func (t *NewUserRussianTrigger) ContainsRussian(text string) bool {
	return t.ContainsCyrillic(text)
}

// IsOnlyRussian is an alias for ContainsCyrillic.
func (t *NewUserRussianTrigger) IsOnlyRussian(text string) bool {
	return t.ContainsCyrillic(text)
}

// IsOnlyCyrillic is an alias for ContainsCyrillic.
func (t *NewUserRussianTrigger) IsOnlyCyrillic(text string) bool {
	return t.ContainsCyrillic(text)
}
