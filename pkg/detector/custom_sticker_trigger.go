package detector

import (
	"fmt"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// CustomStickerTriggerConfig configures thresholds for auto-kickbanning custom stickers
// sent by low reputation users.
type CustomStickerTriggerConfig struct {
	Enabled                    bool `mapstructure:"enabled" yaml:"enabled"`
	MaxReputation              int  `mapstructure:"max_reputation" yaml:"max_reputation"`
	MaxUserPosts               int  `mapstructure:"max_user_posts" yaml:"max_user_posts,omitempty"`
	RepPenalty                 int  `mapstructure:"rep_penalty" yaml:"rep_penalty"`
	AllowOfficialAnimatedEmoji bool `mapstructure:"allow_official_animated_emoji" yaml:"allow_official_animated_emoji,omitempty"`
}

// CustomStickerTrigger detects and kickbans low reputation users who post custom stickers.
type CustomStickerTrigger struct {
	cfg CustomStickerTriggerConfig
}

// NewCustomStickerTrigger constructs a new CustomStickerTrigger.
func NewCustomStickerTrigger(cfg CustomStickerTriggerConfig) *CustomStickerTrigger {
	return &CustomStickerTrigger{cfg: cfg}
}

func (t *CustomStickerTrigger) ID() string {
	return "custom_sticker"
}

func (t *CustomStickerTrigger) Name() string {
	return "Low Reputation User Custom Sticker Detection"
}

func (t *CustomStickerTrigger) IsEnabled() bool {
	return t.cfg.Enabled
}

func (t *CustomStickerTrigger) Evaluate(ctx *TriggerContext) (*TriggerResult, error) {
	if !t.IsEnabled() || ctx == nil || ctx.User == nil {
		return &TriggerResult{Triggered: false}, nil
	}

	// Whitelist: Admins and high reputation users (>= 100) are exempt
	if ctx.User.IsAdmin || ctx.User.Reputation >= 100 {
		return &TriggerResult{Triggered: false}, nil
	}

	// Reputation check: only low reputation users are evaluated
	maxRep := t.cfg.MaxReputation
	if maxRep <= 0 {
		maxRep = 5
	}
	if ctx.User.Reputation > maxRep {
		return &TriggerResult{Triggered: false}, nil
	}

	// Optional post count check
	if t.cfg.MaxUserPosts > 0 && ctx.UserMessageCount > t.cfg.MaxUserPosts {
		return &TriggerResult{Triggered: false}, nil
	}

	isSticker, stickerDesc := t.checkCustomSticker(ctx)
	if !isSticker {
		return &TriggerResult{Triggered: false}, nil
	}

	repPenalty := t.cfg.RepPenalty
	if repPenalty <= 0 {
		repPenalty = 50
	}

	reasonDesc := "custom sticker"
	if stickerDesc != "" {
		reasonDesc = fmt.Sprintf("custom sticker (%s)", stickerDesc)
	}
	reason := fmt.Sprintf("Detection trigger (custom_sticker): Low reputation user (rep=%d <= %d) posted %s",
		ctx.User.Reputation, maxRep, reasonDesc)

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

func (t *CustomStickerTrigger) checkCustomSticker(ctx *TriggerContext) (bool, string) {
	if ctx == nil {
		return false, ""
	}

	// 1. Raw Telegram Message sticker check
	if ctx.RawMessage != nil {
		if ctx.RawMessage.Sticker != nil {
			st := ctx.RawMessage.Sticker
			if t.cfg.AllowOfficialAnimatedEmoji && isOfficialAnimatedEmoji(st) {
				return false, ""
			}
			desc := st.Emoji
			if desc == "" && st.SetName != "" {
				desc = st.SetName
			}
			if desc == "" {
				desc = "custom sticker"
			}
			return true, desc
		}

		// Also detect custom emoji entity in text messages if present
		if hasCustomEmoji(ctx.RawMessage) {
			return true, "custom_emoji"
		}
	}

	// 2. Fallback to message text format (e.g. from DB replay or tests: "[Sticker 👍]" or "[Sticker]")
	textToCheck := ctx.Text
	if textToCheck == "" && ctx.Message != nil {
		textToCheck = ctx.Message.Text
	}

	if strings.HasPrefix(textToCheck, "[Sticker") {
		// Extract emoji if present: e.g. "[Sticker 👍]" -> "👍"
		inner := strings.TrimPrefix(textToCheck, "[Sticker")
		inner = strings.TrimSuffix(inner, "]")
		inner = strings.TrimSpace(inner)
		if inner != "" {
			return true, inner
		}
		return true, "sticker"
	}

	return false, ""
}

// isOfficialAnimatedEmoji checks if a sticker belongs to Telegram's official AnimatedEmojies set.
func isOfficialAnimatedEmoji(s *tgbotapi.Sticker) bool {
	if s == nil {
		return false
	}
	return strings.EqualFold(s.SetName, "AnimatedEmojies")
}

// hasCustomEmoji checks if a message contains Telegram custom emoji entities.
func hasCustomEmoji(msg *tgbotapi.Message) bool {
	if msg == nil {
		return false
	}
	for _, e := range msg.Entities {
		if e.Type == "custom_emoji" {
			return true
		}
	}
	for _, e := range msg.CaptionEntities {
		if e.Type == "custom_emoji" {
			return true
		}
	}
	return false
}
