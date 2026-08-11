package detector

import (
	"strings"
	"unicode"

	lingua "github.com/pemistahl/lingua-go"
)

// NewUserChineseTriggerConfig configures thresholds for the Chinese spam detection trigger.
type NewUserChineseTriggerConfig struct {
	Enabled         bool  `mapstructure:"enabled" yaml:"enabled"`
	MinHighUserID   int64 `mapstructure:"min_high_user_id" yaml:"min_high_user_id"`
	MaxReputation   int   `mapstructure:"max_reputation" yaml:"max_reputation"`
	MinChineseChars int   `mapstructure:"min_chinese_chars" yaml:"min_chinese_chars"`
	RepPenalty      int   `mapstructure:"rep_penalty" yaml:"rep_penalty"`
}

// NewUserChineseTrigger detects messages from brand new, high-ID users with low/no rep containing Chinese characters.
type NewUserChineseTrigger struct {
	cfg      NewUserChineseTriggerConfig
	detector lingua.LanguageDetector
}

// NewNewUserChineseTrigger constructs a new NewUserChineseTrigger with a default Lingua detector.
func NewNewUserChineseTrigger(cfg NewUserChineseTriggerConfig) *NewUserChineseTrigger {
	ld := lingua.NewLanguageDetectorBuilder().FromAllLanguages().Build()
	return &NewUserChineseTrigger{
		cfg:      cfg,
		detector: ld,
	}
}

// NewNewUserChineseTriggerWithDetector constructs a new trigger with a custom Lingua LanguageDetector.
func NewNewUserChineseTriggerWithDetector(cfg NewUserChineseTriggerConfig, ld lingua.LanguageDetector) *NewUserChineseTrigger {
	return &NewUserChineseTrigger{
		cfg:      cfg,
		detector: ld,
	}
}

func (t *NewUserChineseTrigger) ID() string {
	return "new_user_chinese"
}

func (t *NewUserChineseTrigger) Name() string {
	return "New High-ID User Chinese Message Detection"
}

func (t *NewUserChineseTrigger) IsEnabled() bool {
	return t.cfg.Enabled
}

func (t *NewUserChineseTrigger) Evaluate(ctx *TriggerContext) (*TriggerResult, error) {
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

	// Condition 4: Message text contains Chinese characters
	text := strings.TrimSpace(ctx.Text)
	if text == "" {
		return &TriggerResult{Triggered: false}, nil
	}

	if !t.ContainsChinese(text) {
		return &TriggerResult{Triggered: false}, nil
	}

	repPenalty := t.cfg.RepPenalty
	if repPenalty <= 0 {
		repPenalty = 20
	}

	reason := "Detection trigger (new_user_chinese): High-ID new user with low/no rep sent message containing Chinese characters"

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

// ContainsChinese returns true if text contains at least MinChineseChars Han script characters.
func (t *NewUserChineseTrigger) ContainsChinese(text string) bool {
	minChars := t.cfg.MinChineseChars
	if minChars <= 0 {
		minChars = 1
	}

	count := 0
	for _, r := range text {
		if unicode.Is(unicode.Han, r) {
			count++
			if count >= minChars {
				return true
			}
		}
	}
	return false
}

// IsOnlyChinese returns true if text contains Chinese characters.
func (t *NewUserChineseTrigger) IsOnlyChinese(text string) bool {
	return t.ContainsChinese(text)
}

// IsShieldyVerificationText returns true if text matches Shieldy captcha verification phrase ("I am not a bot").
func IsShieldyVerificationText(text string) bool {
	cleaned := strings.ToLower(strings.TrimSpace(text))
	cleaned = strings.Trim(cleaned, ".,!?'\"`")
	cleaned = strings.Join(strings.Fields(cleaned), " ")
	return cleaned == "i am not a bot" || cleaned == "i'm not a bot" || cleaned == "im not a bot"
}
