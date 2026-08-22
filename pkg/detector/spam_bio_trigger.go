package detector

import (
	"fmt"
	"strings"

	"github.com/angch/gogcbot/pkg/db"
)

// NewUserSpamBioTriggerConfig configures thresholds for the spam profile bio detection trigger.
type NewUserSpamBioTriggerConfig struct {
	Enabled        bool     `mapstructure:"enabled" yaml:"enabled"`
	MaxReputation  int      `mapstructure:"max_reputation" yaml:"max_reputation"`
	MaxUserPosts   int      `mapstructure:"max_user_posts" yaml:"max_user_posts"`
	RepPenalty     int      `mapstructure:"rep_penalty" yaml:"rep_penalty"`
	CustomKeywords []string `mapstructure:"custom_keywords" yaml:"custom_keywords"`
}

// NewUserSpamBioTrigger detects messages from new users whose profile bio contains known spam or syndicate marketing keywords.
type NewUserSpamBioTrigger struct {
	cfg            NewUserSpamBioTriggerConfig
	customKeywords []string
}

// NewNewUserSpamBioTrigger constructs a new NewUserSpamBioTrigger.
func NewNewUserSpamBioTrigger(cfg NewUserSpamBioTriggerConfig) *NewUserSpamBioTrigger {
	return &NewUserSpamBioTrigger{
		cfg:            cfg,
		customKeywords: cfg.CustomKeywords,
	}
}

// NewNewUserSpamBioTriggerWithKeywords constructs a new NewUserSpamBioTrigger with extra custom keywords.
func NewNewUserSpamBioTriggerWithKeywords(cfg NewUserSpamBioTriggerConfig, customKeywords ...string) *NewUserSpamBioTrigger {
	allKws := append([]string{}, cfg.CustomKeywords...)
	allKws = append(allKws, customKeywords...)
	return &NewUserSpamBioTrigger{
		cfg:            cfg,
		customKeywords: allKws,
	}
}

func (t *NewUserSpamBioTrigger) ID() string {
	return "new_user_spam_bio"
}

func (t *NewUserSpamBioTrigger) Name() string {
	return "New User Spam Profile Bio Detection"
}

func (t *NewUserSpamBioTrigger) IsEnabled() bool {
	return t.cfg.Enabled
}

func (t *NewUserSpamBioTrigger) Evaluate(ctx *TriggerContext) (*TriggerResult, error) {
	if !t.IsEnabled() || ctx == nil {
		return &TriggerResult{Triggered: false}, nil
	}

	if ctx.User == nil {
		return &TriggerResult{Triggered: false}, nil
	}

	// Condition 1: Whitelist / Max reputation check
	maxRep := t.cfg.MaxReputation
	if maxRep <= 0 {
		maxRep = 5
	}
	if ctx.User.Reputation > maxRep {
		return &TriggerResult{Triggered: false}, nil
	}

	// Condition 2: Max user posts (new user check)
	maxPosts := t.cfg.MaxUserPosts
	if maxPosts <= 0 {
		maxPosts = 5
	}
	if ctx.UserMessageCount > maxPosts {
		return &TriggerResult{Triggered: false}, nil
	}

	// Condition 3: Check user bio, personal channel title/username, and business intro
	var profileTexts []string
	if strings.TrimSpace(ctx.UserBio) != "" {
		profileTexts = append(profileTexts, ctx.UserBio)
	}
	if strings.TrimSpace(ctx.PersonalChatTitle) != "" {
		profileTexts = append(profileTexts, ctx.PersonalChatTitle)
	}
	if strings.TrimSpace(ctx.PersonalChatUsername) != "" {
		profileTexts = append(profileTexts, ctx.PersonalChatUsername)
	}
	if strings.TrimSpace(ctx.BusinessIntro) != "" {
		profileTexts = append(profileTexts, ctx.BusinessIntro)
	}

	var matched []string
	if len(profileTexts) > 0 {
		combinedProfileText := strings.Join(profileTexts, " | ")
		_, matchedKws := db.MatchSpamBioAll(combinedProfileText, t.customKeywords...)
		matched = append(matched, matchedKws...)
	}

	if ctx.PersonalChatUsername != "" && db.IsSpammyUsername(ctx.PersonalChatUsername) {
		cleanHandle := strings.TrimPrefix(strings.TrimSpace(ctx.PersonalChatUsername), "@")
		matched = append(matched, fmt.Sprintf("spammy_channel_username:@%s", cleanHandle))
	}

	if len(matched) == 0 {
		return &TriggerResult{Triggered: false}, nil
	}

	repPenalty := t.cfg.RepPenalty
	if repPenalty <= 0 {
		repPenalty = 20
	}

	matchedStr := strings.Join(matched, ", ")
	if matchedStr == "" {
		matchedStr = "spam keyword match"
	}
	reason := fmt.Sprintf("Detection trigger (new_user_spam_bio): New user profile signals matched spam keywords [%s]", matchedStr)

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
