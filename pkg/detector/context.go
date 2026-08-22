package detector

import (
	"github.com/angch/gogcbot/pkg/db"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// TriggerContext encapsulates incoming message context and user information passed to detection triggers.
type TriggerContext struct {
	Message              *db.Message
	RawMessage           *tgbotapi.Message
	Text                 string
	User                 *db.User
	UserBio              string // Profile bio if available
	LanguageCode         string // User client language tag (e.g., zh-hans, en)
	IsPremium            bool   // Telegram Premium status
	HasPrivateForwards   bool   // Restricted forward privacy setting
	PersonalChatTitle    string // Linked personal channel title
	PersonalChatUsername string // Linked personal channel @username
	BusinessIntro        string // Telegram business intro message/title
	HasPhoto             bool   // Whether user has profile avatar
	PhotoCount           int    // Number of profile photos
	IsNewUser            bool   // True if user was newly created in DB for this message
	UserMessageCount     int    // Total posts by user in DB
	ChatID               int64
	GroupTitle           string
	HasVerifiedNotBot    bool // True if user verified with "I am not a bot"
}

// DisplayName returns the trimmed combined display name of the context user, or empty string.
func (ctx *TriggerContext) DisplayName() string {
	if ctx == nil || ctx.User == nil {
		return ""
	}
	return ctx.User.DisplayName()
}

// TriggerResult contains the outcome of evaluating a Trigger.
type TriggerResult struct {
	Triggered bool     `json:"triggered" yaml:"triggered"`
	TriggerID string   `json:"trigger_id" yaml:"trigger_id"`
	Reason    string   `json:"reason" yaml:"reason"`
	Actions   []Action `json:"actions" yaml:"actions"`
}
