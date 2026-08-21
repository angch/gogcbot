package detector

import (
	"fmt"
	"strings"
	"unicode"
)

// RedPacketNameTriggerConfig configures thresholds for the red packet emoji CJK name detection trigger.
type RedPacketNameTriggerConfig struct {
	Enabled           bool    `mapstructure:"enabled" yaml:"enabled"`
	MinHighUserID     int64   `mapstructure:"min_high_user_id" yaml:"min_high_user_id"`
	MaxReputation     int     `mapstructure:"max_reputation" yaml:"max_reputation"`
	MaxUserPosts      int     `mapstructure:"max_user_posts" yaml:"max_user_posts"`
	MinUsernameLength int     `mapstructure:"min_username_length" yaml:"min_username_length"`
	MinCJKRatio       float64 `mapstructure:"min_cjk_ratio" yaml:"min_cjk_ratio"`
	MinCJKChars       int     `mapstructure:"min_cjk_chars" yaml:"min_cjk_chars"`
	RepPenalty        int     `mapstructure:"rep_penalty" yaml:"rep_penalty"`
}

// NewUserRedPacketTriggerConfig is a type alias for backwards compatibility.
type NewUserRedPacketTriggerConfig = RedPacketNameTriggerConfig

// RedPacketNameTrigger detects joining or new users who have "🧧" at the end of their name,
// mostly CJK characters in their name, high User ID, and a mixed-caps username of at least 5 length.
type RedPacketNameTrigger struct {
	cfg RedPacketNameTriggerConfig
}

// NewUserRedPacketTrigger is a type alias for backwards compatibility.
type NewUserRedPacketTrigger = RedPacketNameTrigger

// NewRedPacketNameTrigger constructs a new RedPacketNameTrigger.
func NewRedPacketNameTrigger(cfg RedPacketNameTriggerConfig) *RedPacketNameTrigger {
	return &RedPacketNameTrigger{
		cfg: cfg,
	}
}

// NewNewUserRedPacketTrigger constructs a new RedPacketNameTrigger using the alias constructor.
func NewNewUserRedPacketTrigger(cfg RedPacketNameTriggerConfig) *RedPacketNameTrigger {
	return NewRedPacketNameTrigger(cfg)
}

func (t *RedPacketNameTrigger) ID() string {
	return "red_packet_cjk_name"
}

func (t *RedPacketNameTrigger) Name() string {
	return "Red Packet CJK Name & Mixed-Caps Username Detection"
}

func (t *RedPacketNameTrigger) IsEnabled() bool {
	return t.cfg.Enabled
}

func (t *RedPacketNameTrigger) Evaluate(ctx *TriggerContext) (*TriggerResult, error) {
	if !t.IsEnabled() || ctx == nil || ctx.User == nil {
		return &TriggerResult{Triggered: false}, nil
	}

	// Exempt super admin / admins / whitelisted users with maximum reputation
	if ctx.User.IsAdmin || ctx.User.Reputation >= 100 {
		return &TriggerResult{Triggered: false}, nil
	}

	// Condition 1: Low/default reputation check
	maxRep := t.cfg.MaxReputation
	if maxRep <= 0 {
		maxRep = 5
	}
	if ctx.User.Reputation > maxRep {
		return &TriggerResult{Triggered: false}, nil
	}

	// Condition 2: Must be a new user (new in DB or within max user post threshold)
	maxPosts := t.cfg.MaxUserPosts
	if maxPosts <= 0 {
		maxPosts = 5
	}
	isNewUser := ctx.IsNewUser || ctx.UserMessageCount <= maxPosts
	if !isNewUser {
		return &TriggerResult{Triggered: false}, nil
	}

	// Condition 3: High ID
	minHighID := t.cfg.MinHighUserID
	if minHighID <= 0 {
		minHighID = 1000000000
	}
	if ctx.User.UserID < minHighID {
		return &TriggerResult{Triggered: false}, nil
	}

	// Condition 4: Ends with "🧧" at the end of their name
	fullName := strings.TrimSpace(ctx.User.FirstName + " " + ctx.User.LastName)
	firstName := strings.TrimSpace(ctx.User.FirstName)
	lastName := strings.TrimSpace(ctx.User.LastName)

	if !HasRedPacketSuffix(fullName, firstName, lastName) {
		return &TriggerResult{Triggered: false}, nil
	}

	// Condition 5: The name itself is mostly CJK unicode
	minCJKRatio := t.cfg.MinCJKRatio
	if minCJKRatio <= 0 {
		minCJKRatio = 0.5
	}
	minCJKChars := t.cfg.MinCJKChars
	if minCJKChars <= 0 {
		minCJKChars = 1
	}

	nameToCheck := fullName
	if nameToCheck == "" {
		nameToCheck = firstName
	}
	if !IsNameMostlyCJK(nameToCheck, minCJKRatio, minCJKChars) {
		return &TriggerResult{Triggered: false}, nil
	}

	// Condition 6: Username with mixed caps letters of at least 5 length
	minUsernameLen := t.cfg.MinUsernameLength
	if minUsernameLen <= 0 {
		minUsernameLen = 5
	}
	if !IsMixedCapsUsername(ctx.User.Username, minUsernameLen) {
		return &TriggerResult{Triggered: false}, nil
	}

	repPenalty := t.cfg.RepPenalty
	if repPenalty <= 0 {
		repPenalty = 20
	}

	cleanUsername := strings.TrimPrefix(strings.TrimSpace(ctx.User.Username), "@")
	reason := fmt.Sprintf("Detection trigger (red_packet_cjk_name): High-ID new user with mostly CJK name ending in '🧧' and mixed-caps username (@%s)", cleanUsername)

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

// RedPacketEmoji is the Unicode string for the red envelope / red packet emoji ("🧧", U+1F9E7).
const RedPacketEmoji = "🧧"

// HasRedPacketSuffix returns true if the full name, first name, or last name ends with "🧧".
func HasRedPacketSuffix(fullName, firstName, lastName string) bool {
	checkEnding := func(s string) bool {
		s = strings.TrimSpace(s)
		return strings.HasSuffix(s, RedPacketEmoji)
	}

	return checkEnding(fullName) || checkEnding(firstName) || checkEnding(lastName)
}

// IsNameMostlyCJK checks if the name (after stripping the trailing red packet emoji and spaces)
// contains at least minCJKChars CJK characters and meets the minimum CJK character ratio.
func IsNameMostlyCJK(name string, minRatio float64, minCJKChars int) bool {
	trimmed := strings.TrimSpace(name)
	// Strip trailing red envelope emoji and trailing spaces
	for strings.HasSuffix(trimmed, RedPacketEmoji) || (len(trimmed) > 0 && unicode.IsSpace(rune(trimmed[len(trimmed)-1]))) {
		trimmed = strings.TrimSuffix(trimmed, RedPacketEmoji)
		trimmed = strings.TrimSpace(trimmed)
	}

	if trimmed == "" {
		return false
	}

	cjkCount := 0
	totalRunes := 0

	for _, r := range trimmed {
		if unicode.IsSpace(r) {
			continue
		}
		totalRunes++
		if IsCJK(r) {
			cjkCount++
		}
	}

	if totalRunes == 0 || cjkCount < minCJKChars {
		return false
	}

	ratio := float64(cjkCount) / float64(totalRunes)
	return ratio >= minRatio
}

// IsMixedCapsUsername checks if a Telegram username has at least minLen length and contains
// a high degree of mixed uppercase and lowercase letters (e.g., @cbzbQFLOuHNkJZ).
func IsMixedCapsUsername(username string, minLen int) bool {
	clean := strings.TrimPrefix(strings.TrimSpace(username), "@")
	if minLen <= 0 {
		minLen = 5
	}
	if len(clean) < minLen {
		return false
	}

	upperCount := 0
	lowerCount := 0
	transitions := 0
	var prevCase int // 0 = unset, 1 = upper, 2 = lower

	for _, r := range clean {
		if r >= 'A' && r <= 'Z' {
			if prevCase == 2 {
				transitions++
			}
			prevCase = 1
			upperCount++
		} else if r >= 'a' && r <= 'z' {
			if prevCase == 1 {
				transitions++
			}
			prevCase = 2
			lowerCount++
		}
	}

	totalLetters := upperCount + lowerCount
	if totalLetters < minLen {
		return false
	}

	// Must have at least 2 uppercase and 2 lowercase letters
	if upperCount < 2 || lowerCount < 2 {
		return false
	}

	// Mixed caps condition:
	// 1) Has 2 or more case transitions (e.g. cbzbQ...uHNkJZ, aBcDe, AbCdE), OR
	// 2) Has 3+ upper and 3+ lower (e.g. cbzbQFLO), OR
	// 3) Significant balanced ratio of upper and lower letters (each >= 25% of total letters)
	if transitions >= 2 || (upperCount >= 3 && lowerCount >= 3) ||
		(float64(upperCount)/float64(totalLetters) >= 0.25 && float64(lowerCount)/float64(totalLetters) >= 0.25) {
		return true
	}

	return false
}
