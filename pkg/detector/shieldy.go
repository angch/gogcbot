package detector

import "strings"

// IsShieldyChallengeText returns true if text matches the Shieldy captcha challenge message.
// Matches phrases like:
// "please, press the button below within the time amount specified, otherwise you will be kicked."
func IsShieldyChallengeText(text string) bool {
	if text == "" {
		return false
	}
	lower := strings.ToLower(text)
	// Match key distinctive phrases of Shieldy's button captcha challenge
	if strings.Contains(lower, "press the button below within the time amount specified") {
		return true
	}
	if strings.Contains(lower, "press the button below") && strings.Contains(lower, "otherwise you will be kicked") {
		return true
	}
	if strings.Contains(lower, "please, press the button below") {
		return true
	}
	if strings.Contains(lower, "within the time amount specified") && strings.Contains(lower, "otherwise you will be kicked") {
		return true
	}
	return false
}

// IsProfileTriggerID returns true if the trigger ID represents a profile-based detection rule (not message content).
func IsProfileTriggerID(triggerID string) bool {
	switch triggerID {
	case "new_user_spam_bio", "profile_keyword_ban", "red_packet_name", "username_anomaly", "nonsense_bio":
		return true
	default:
		return false
	}
}

// IsProfileBanReason returns true if the ban reason originates from a profile-based trigger or rescan.
func IsProfileBanReason(reason string) bool {
	lower := strings.ToLower(reason)
	return strings.Contains(lower, "new_user_spam_bio") ||
		strings.Contains(lower, "profile_keyword_ban") ||
		strings.Contains(lower, "red_packet_name") ||
		strings.Contains(lower, "username_anomaly") ||
		strings.Contains(lower, "nonsense_bio") ||
		strings.Contains(lower, "profile") ||
		strings.Contains(lower, "bio") ||
		strings.Contains(lower, "username anomaly") ||
		strings.Contains(lower, "join") ||
		strings.Contains(lower, "rescan")
}
