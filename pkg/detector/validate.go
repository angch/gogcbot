package detector

import (
	"fmt"
	"strings"

	"github.com/angch/gogcbot/pkg/db"
)

// RuleMatchViolation records details of an unexpected rule trigger against a user who should be exempt (e.g. rep > 40).
type RuleMatchViolation struct {
	TriggerID   string          `json:"trigger_id"`
	TriggerName string          `json:"trigger_name"`
	User        *db.User        `json:"user"`
	Profile     *db.UserProfile `json:"profile,omitempty"`
	Message     *db.Message     `json:"message,omitempty"`
	ContextType string          `json:"context_type"` // e.g. "join", "rescan", "message"
	Reason      string          `json:"reason"`
}

func (v RuleMatchViolation) String() string {
	var userStr string
	if v.User != nil {
		userStr = fmt.Sprintf("User %d (@%s, %q, rep=%d, admin=%v)", v.User.UserID, v.User.Username, v.User.DisplayName(), v.User.Reputation, v.User.IsAdmin)
	} else {
		userStr = "User <nil>"
	}
	msgStr := ""
	if v.Message != nil {
		msgStr = fmt.Sprintf(" [Message ID %d: %q]", v.Message.MessageID, v.Message.Text)
	}
	return fmt.Sprintf("Rule %q (%s) matched against %s in %s context: %s%s", v.TriggerID, v.TriggerName, userStr, v.ContextType, v.Reason, msgStr)
}

// ValidateAgainstHighRepUsers checks all registered triggers in the detector against users in the database with reputation > minRep.
// If minRep is <= 0, it defaults to 40.
// It verifies:
// 1. Join context: when user joins with IsNewUser=true, UserMessageCount=0
// 2. Rescan context: when user is rescanned with IsNewUser=false, UserMessageCount=actual
// 3. Message context: for each logged message sent by the user
//
// Returns a list of any violations where Triggered == true.
func (d *Detector) ValidateAgainstHighRepUsers(database *db.DB, minRep int) ([]RuleMatchViolation, error) {
	if minRep <= 0 {
		minRep = 40
	}
	if database == nil {
		return nil, fmt.Errorf("database cannot be nil")
	}

	users, err := database.GetUsersWithReputationAbove(minRep)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch users with reputation > %d: %w", minRep, err)
	}

	var violations []RuleMatchViolation

	for _, u := range users {
		userCopy := u

		// Fetch profile if exists
		profile, _ := database.GetUserProfile(userCopy.UserID)
		msgCount, _ := database.GetUserMessageCount(userCopy.UserID)
		msgs, _ := database.GetAllUserMessages(userCopy.UserID)

		userBio := ""
		personalChatTitle := ""
		personalChatUsername := ""
		businessIntro := ""
		hasPhoto := false
		photoCount := 0
		hasPrivateForwards := false

		if profile != nil {
			userBio = profile.Bio
			personalChatTitle = profile.PersonalChatTitle
			personalChatUsername = profile.PersonalChatUsername
			businessIntro = profile.BusinessIntro
			hasPhoto = profile.HasPhoto
			photoCount = profile.PhotoCount
			hasPrivateForwards = profile.HasPrivateForwards
		}

		baseCtx := func() *TriggerContext {
			return &TriggerContext{
				User:                 &userCopy,
				UserBio:              userBio,
				LanguageCode:         userCopy.LanguageCode,
				IsPremium:            userCopy.IsPremium,
				HasPrivateForwards:   hasPrivateForwards,
				PersonalChatTitle:    personalChatTitle,
				PersonalChatUsername: personalChatUsername,
				BusinessIntro:        businessIntro,
				HasPhoto:             hasPhoto,
				PhotoCount:           photoCount,
			}
		}

		// 1. Check Join context (simulating brand new join)
		joinCtx := baseCtx()
		joinCtx.IsNewUser = true
		joinCtx.UserMessageCount = 0
		joinResults, err := d.Evaluate(joinCtx)
		if err != nil {
			return nil, fmt.Errorf("error evaluating join context for user %d: %w", userCopy.UserID, err)
		}
		for _, res := range joinResults {
			if res != nil && res.Triggered {
				violations = append(violations, RuleMatchViolation{
					TriggerID:   res.TriggerID,
					TriggerName: d.getTriggerName(res.TriggerID),
					User:        &userCopy,
					Profile:     profile,
					ContextType: "join",
					Reason:      res.Reason,
				})
			}
		}

		// 2. Check Rescan context (simulating user directory / rescan)
		rescanCtx := baseCtx()
		rescanCtx.IsNewUser = false
		rescanCtx.UserMessageCount = msgCount
		rescanResults, err := d.Evaluate(rescanCtx)
		if err != nil {
			return nil, fmt.Errorf("error evaluating rescan context for user %d: %w", userCopy.UserID, err)
		}
		for _, res := range rescanResults {
			if res != nil && res.Triggered {
				violations = append(violations, RuleMatchViolation{
					TriggerID:   res.TriggerID,
					TriggerName: d.getTriggerName(res.TriggerID),
					User:        &userCopy,
					Profile:     profile,
					ContextType: "rescan",
					Reason:      res.Reason,
				})
			}
		}

		// 3. Check Message context for each message from this user
		for _, m := range msgs {
			msgCopy := m
			msgCtx := baseCtx()
			msgCtx.Message = &msgCopy
			msgCtx.Text = msgCopy.Text
			msgCtx.ChatID = msgCopy.ChatID
			msgCtx.IsNewUser = false
			msgCtx.UserMessageCount = msgCount

			msgResults, err := d.Evaluate(msgCtx)
			if err != nil {
				return nil, fmt.Errorf("error evaluating message %d for user %d: %w", msgCopy.MessageID, userCopy.UserID, err)
			}
			for _, res := range msgResults {
				if res != nil && res.Triggered {
					violations = append(violations, RuleMatchViolation{
						TriggerID:   res.TriggerID,
						TriggerName: d.getTriggerName(res.TriggerID),
						User:        &userCopy,
						Profile:     profile,
						Message:     &msgCopy,
						ContextType: "message",
						Reason:      res.Reason,
					})
				}
			}
		}
	}

	return violations, nil
}

// ValidateAgainstDB is an alias for ValidateAgainstHighRepUsers.
func (d *Detector) ValidateAgainstDB(database *db.DB, minRep int) ([]RuleMatchViolation, error) {
	return d.ValidateAgainstHighRepUsers(database, minRep)
}

func (d *Detector) getTriggerName(id string) string {
	d.mu.RLock()
	defer d.mu.RUnlock()
	for _, t := range d.triggers {
		if t != nil && t.ID() == id {
			return t.Name()
		}
	}
	return id
}

// ValidateTriggersAgainstHighRepUsers is a convenience function that constructs a detector with the given triggers
// and validates them against all users in database with reputation > minRep.
func ValidateTriggersAgainstHighRepUsers(database *db.DB, minRep int, triggers ...Trigger) ([]RuleMatchViolation, error) {
	det := NewDetector(triggers...)
	return det.ValidateAgainstHighRepUsers(database, minRep)
}

// FormatRuleMatchViolations formats a slice of violations into a descriptive error summary string.
func FormatRuleMatchViolations(violations []RuleMatchViolation) string {
	if len(violations) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Found %d rule violation(s) matching against high-reputation (>40) users in DB:\n", len(violations)))
	for i, v := range violations {
		sb.WriteString(fmt.Sprintf("  [%d] %s\n", i+1, v.String()))
	}
	return sb.String()
}
