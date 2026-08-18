package detector

import (
	"strings"
	"unicode"

	lingua "github.com/pemistahl/lingua-go"
)

// NewUserCJKTriggerConfig configures thresholds for the CJK spam detection trigger.
type NewUserCJKTriggerConfig struct {
	Enabled         bool  `mapstructure:"enabled" yaml:"enabled"`
	MinHighUserID   int64 `mapstructure:"min_high_user_id" yaml:"min_high_user_id"`
	MaxReputation   int   `mapstructure:"max_reputation" yaml:"max_reputation"`
	MinCJKChars     int   `mapstructure:"min_cjk_chars" yaml:"min_cjk_chars"`
	MinChineseChars int   `mapstructure:"min_chinese_chars" yaml:"min_chinese_chars"` // Backwards compatibility alias
	RepPenalty      int   `mapstructure:"rep_penalty" yaml:"rep_penalty"`
}

// NewUserChineseTriggerConfig is a type alias for backwards compatibility.
type NewUserChineseTriggerConfig = NewUserCJKTriggerConfig

// NewUserCJKTrigger detects messages from brand new, high-ID users with low/no rep containing CJK characters.
type NewUserCJKTrigger struct {
	cfg      NewUserCJKTriggerConfig
	detector lingua.LanguageDetector
}

// NewUserChineseTrigger is a type alias for backwards compatibility.
type NewUserChineseTrigger = NewUserCJKTrigger

// NewNewUserCJKTrigger constructs a new NewUserCJKTrigger with a default Lingua detector.
func NewNewUserCJKTrigger(cfg NewUserCJKTriggerConfig) *NewUserCJKTrigger {
	ld := lingua.NewLanguageDetectorBuilder().FromAllLanguages().Build()
	return &NewUserCJKTrigger{
		cfg:      cfg,
		detector: ld,
	}
}

// NewNewUserChineseTrigger is a backwards compatible constructor.
func NewNewUserChineseTrigger(cfg NewUserChineseTriggerConfig) *NewUserCJKTrigger {
	return NewNewUserCJKTrigger(cfg)
}

// NewNewUserCJKTriggerWithDetector constructs a new trigger with a custom Lingua LanguageDetector.
func NewNewUserCJKTriggerWithDetector(cfg NewUserCJKTriggerConfig, ld lingua.LanguageDetector) *NewUserCJKTrigger {
	return &NewUserCJKTrigger{
		cfg:      cfg,
		detector: ld,
	}
}

// NewNewUserChineseTriggerWithDetector is a backwards compatible constructor.
func NewNewUserChineseTriggerWithDetector(cfg NewUserChineseTriggerConfig, ld lingua.LanguageDetector) *NewUserCJKTrigger {
	return NewNewUserCJKTriggerWithDetector(cfg, ld)
}

func (t *NewUserCJKTrigger) ID() string {
	return "new_user_cjk"
}

func (t *NewUserCJKTrigger) Name() string {
	return "New High-ID User CJK Message Detection"
}

func (t *NewUserCJKTrigger) IsEnabled() bool {
	return t.cfg.Enabled
}

func (t *NewUserCJKTrigger) Evaluate(ctx *TriggerContext) (*TriggerResult, error) {
	if !t.IsEnabled() || ctx == nil {
		return &TriggerResult{Triggered: false}, nil
	}

	// Condition 0: Shieldy verification message ("I am not a bot") or verified user
	if ctx.HasVerifiedNotBot || IsShieldyVerificationText(ctx.Text) {
		return &TriggerResult{Triggered: false}, nil
	}

	// Condition 1: Must be a new user we have not seen before
	if !ctx.IsNewUser {
		return &TriggerResult{Triggered: false}, nil
	}

	// Condition 2: High ID
	if ctx.User == nil || ctx.User.UserID < t.cfg.MinHighUserID {
		return &TriggerResult{Triggered: false}, nil
	}

	// Condition 3: Low or no reputation
	if ctx.User.Reputation > t.cfg.MaxReputation {
		return &TriggerResult{Triggered: false}, nil
	}

	// Condition 4: Message text contains CJK characters
	text := strings.TrimSpace(ctx.Text)
	if text == "" {
		return &TriggerResult{Triggered: false}, nil
	}

	if !t.ContainsCJK(text) {
		return &TriggerResult{Triggered: false}, nil
	}

	repPenalty := t.cfg.RepPenalty
	if repPenalty <= 0 {
		repPenalty = 20
	}

	reason := "Detection trigger (new_user_cjk): High-ID new user with low/no rep sent message containing CJK characters"

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

// IsCJK returns true if the rune is part of CJK scripts (Han, Hiragana, Katakana, Hangul, Bopomofo) or CJK Unicode ranges.
func IsCJK(r rune) bool {
	if unicode.Is(unicode.Han, r) ||
		unicode.Is(unicode.Hiragana, r) ||
		unicode.Is(unicode.Katakana, r) ||
		unicode.Is(unicode.Hangul, r) ||
		unicode.Is(unicode.Bopomofo, r) {
		return true
	}

	// Additional CJK Unicode ranges
	switch {
	case r >= 0x3000 && r <= 0x303F: // CJK Symbols and Punctuation
		return true
	case r >= 0x31C0 && r <= 0x31EF: // CJK Strokes
		return true
	case r >= 0x3200 && r <= 0x32FF: // Enclosed CJK Letters and Months
		return true
	case r >= 0x3300 && r <= 0x33FF: // CJK Compatibility
		return true
	case r >= 0xFE30 && r <= 0xFE4F: // CJK Compatibility Forms
		return true
	case r >= 0x2E80 && r <= 0x2EFF: // CJK Radicals Supplement
		return true
	case r >= 0x2F00 && r <= 0x2FDF: // Kangxi Radicals
		return true
	case r >= 0x2FF0 && r <= 0x2FFF: // Ideographic Description Characters
		return true
	}
	return false
}

// ContainsCJK returns true if text contains at least MinCJKChars (or MinChineseChars) CJK script characters.
func (t *NewUserCJKTrigger) ContainsCJK(text string) bool {
	minChars := t.cfg.MinCJKChars
	if minChars <= 0 {
		minChars = t.cfg.MinChineseChars
	}
	if minChars <= 0 {
		minChars = 1
	}

	count := 0
	for _, r := range text {
		if IsCJK(r) {
			count++
			if count >= minChars {
				return true
			}
		}
	}
	return false
}

// ContainsChinese is a backwards-compatible alias for ContainsCJK.
func (t *NewUserCJKTrigger) ContainsChinese(text string) bool {
	return t.ContainsCJK(text)
}

// IsOnlyChinese is a backwards-compatible alias for ContainsCJK.
func (t *NewUserCJKTrigger) IsOnlyChinese(text string) bool {
	return t.ContainsCJK(text)
}

// IsOnlyCJK returns true if text contains CJK characters.
func (t *NewUserCJKTrigger) IsOnlyCJK(text string) bool {
	return t.ContainsCJK(text)
}

// IsShieldyVerificationText returns true if text matches Shieldy captcha verification phrase ("I am not a bot").
func IsShieldyVerificationText(text string) bool {
	cleaned := strings.ToLower(strings.TrimSpace(text))
	cleaned = strings.Trim(cleaned, ".,!?'\"`")
	cleaned = strings.Join(strings.Fields(cleaned), " ")
	return cleaned == "i am not a bot" || cleaned == "i'm not a bot" || cleaned == "im not a bot"
}
