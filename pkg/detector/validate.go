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

		// 4. Check Secondary Group context (simulating user joining secondary group without primary group)
		secJoinCtx := baseCtx()
		secJoinCtx.IsNewUser = true
		secJoinCtx.UserMessageCount = 0
		secJoinCtx.IsSecondaryGroup = true
		secJoinCtx.IsMemberOfPrimaryGroup = false
		secJoinResults, err := d.Evaluate(secJoinCtx)
		if err != nil {
			return nil, fmt.Errorf("error evaluating secondary group join context for user %d: %w", userCopy.UserID, err)
		}
		for _, res := range secJoinResults {
			if res != nil && res.Triggered {
				violations = append(violations, RuleMatchViolation{
					TriggerID:   res.TriggerID,
					TriggerName: d.getTriggerName(res.TriggerID),
					User:        &userCopy,
					Profile:     profile,
					ContextType: "secondary_group_join",
					Reason:      res.Reason,
				})
			}
		}
	}

	return violations, nil
}

// ValidateAgainstDB is an alias for ValidateAgainstHighRepUsers.
func (d *Detector) ValidateAgainstDB(database *db.DB, minRep int) ([]RuleMatchViolation, error) {
	return d.ValidateAgainstHighRepUsers(database, minRep)
}

// KnownDataValidationReport encapsulates the results of regression testing against known good and bad datasets.
type KnownDataValidationReport struct {
	KnownGoodTotal  int                  `json:"known_good_total"`
	KnownBadTotal   int                  `json:"known_bad_total"`
	FalsePositives  []RuleMatchViolation `json:"false_positives,omitempty"`
	KnownBadMatches []RuleMatchViolation `json:"known_bad_matches,omitempty"`
	Passed          bool                 `json:"passed"`
	Summary         string               `json:"summary"`
}

// ValidateAgainstKnownGoodAndBad checks all registered triggers against known good and known bad datasets,
// explicitly bypassing reputation and banned requirements to ensure no false positives on good data
// and verifying detections on bad data.
func (d *Detector) ValidateAgainstKnownGoodAndBad(database *db.DB) (*KnownDataValidationReport, error) {
	if database == nil {
		return nil, fmt.Errorf("database cannot be nil")
	}

	goodUsers, err := database.GetKnownGoodUsers()
	if err != nil {
		return nil, fmt.Errorf("failed to fetch known good users: %w", err)
	}

	badUsers, err := database.GetKnownBadUsers()
	if err != nil {
		return nil, fmt.Errorf("failed to fetch known bad users: %w", err)
	}

	report := &KnownDataValidationReport{
		KnownGoodTotal: len(goodUsers),
		KnownBadTotal:  len(badUsers),
	}

	// 1. Evaluate Known Good users (Bypassing reputation & banned requirements)
	for _, kg := range goodUsers {
		kgCopy := kg

		baseCtx := func(simUserID int64) *TriggerContext {
			u := &db.User{
				UserID:       simUserID,
				Username:     kgCopy.Username,
				FirstName:    kgCopy.FirstName,
				LastName:     kgCopy.LastName,
				LanguageCode: kgCopy.LanguageCode,
				IsPremium:    kgCopy.IsPremium,
				Reputation:   0,     // BYPASS rep requirement
				IsAdmin:      false, // BYPASS admin exemption
				IsBanned:     false, // BYPASS banned check
			}
			return &TriggerContext{
				User:                 u,
				UserBio:              kgCopy.Bio,
				LanguageCode:         kgCopy.LanguageCode,
				IsPremium:            kgCopy.IsPremium,
				HasPrivateForwards:   kgCopy.HasPrivateForwards,
				PersonalChatTitle:    kgCopy.PersonalChatTitle,
				PersonalChatUsername: kgCopy.PersonalChatUsername,
				BusinessIntro:        kgCopy.BusinessIntro,
				HasPhoto:             kgCopy.HasPhoto,
				PhotoCount:           kgCopy.PhotoCount,
			}
		}

		testUserIDs := []int64{kgCopy.UserID}
		if kgCopy.UserID < 1000000000 {
			testUserIDs = append(testUserIDs, 5000000000+kgCopy.UserID)
		}

		for _, testUID := range testUserIDs {
			// (A) Join context
			joinCtx := baseCtx(testUID)
			joinCtx.IsNewUser = true
			joinCtx.UserMessageCount = 0
			joinRes, err := d.Evaluate(joinCtx)
			if err == nil {
				for _, res := range joinRes {
					if res != nil && res.Triggered {
						report.FalsePositives = append(report.FalsePositives, RuleMatchViolation{
							TriggerID:   res.TriggerID,
							TriggerName: d.getTriggerName(res.TriggerID),
							User:        joinCtx.User,
							Profile: &db.UserProfile{
								UserID:               kgCopy.UserID,
								Username:             kgCopy.Username,
								FirstName:            kgCopy.FirstName,
								LastName:             kgCopy.LastName,
								Bio:                  kgCopy.Bio,
								PersonalChatTitle:    kgCopy.PersonalChatTitle,
								PersonalChatUsername: kgCopy.PersonalChatUsername,
								BusinessIntro:        kgCopy.BusinessIntro,
								HasPhoto:             kgCopy.HasPhoto,
								PhotoCount:           kgCopy.PhotoCount,
							},
							ContextType: "known_good_join",
							Reason:      res.Reason,
						})
					}
				}
			}

			// (B) Rescan context
			rescanCtx := baseCtx(testUID)
			rescanCtx.IsNewUser = false
			rescanCtx.UserMessageCount = len(kgCopy.SampleMessages)
			rescanRes, err := d.Evaluate(rescanCtx)
			if err == nil {
				for _, res := range rescanRes {
					if res != nil && res.Triggered {
						report.FalsePositives = append(report.FalsePositives, RuleMatchViolation{
							TriggerID:   res.TriggerID,
							TriggerName: d.getTriggerName(res.TriggerID),
							User:        rescanCtx.User,
							Profile: &db.UserProfile{
								UserID:               kgCopy.UserID,
								Username:             kgCopy.Username,
								FirstName:            kgCopy.FirstName,
								LastName:             kgCopy.LastName,
								Bio:                  kgCopy.Bio,
								PersonalChatTitle:    kgCopy.PersonalChatTitle,
								PersonalChatUsername: kgCopy.PersonalChatUsername,
								BusinessIntro:        kgCopy.BusinessIntro,
								HasPhoto:             kgCopy.HasPhoto,
								PhotoCount:           kgCopy.PhotoCount,
							},
							ContextType: "known_good_rescan",
							Reason:      res.Reason,
						})
					}
				}
			}

			// (C) Sample Messages context
			for idx, msgText := range kgCopy.SampleMessages {
				msgCtx := baseCtx(testUID)
				msgCtx.IsNewUser = false
				msgCtx.UserMessageCount = len(kgCopy.SampleMessages)
				msgCtx.Text = msgText
				msgCtx.Message = &db.Message{
					ChatID:    123456,
					MessageID: idx + 1,
					UserID:    testUID,
					Text:      msgText,
				}
				msgRes, err := d.Evaluate(msgCtx)
				if err == nil {
					for _, res := range msgRes {
						if res != nil && res.Triggered {
							report.FalsePositives = append(report.FalsePositives, RuleMatchViolation{
								TriggerID:   res.TriggerID,
								TriggerName: d.getTriggerName(res.TriggerID),
								User:        msgCtx.User,
								Message:     msgCtx.Message,
								ContextType: "known_good_message",
								Reason:      res.Reason,
							})
						}
					}
				}
			}

			// (D) Secondary group context (testing that primary members are not flagged in secondary groups)
			secCtx := baseCtx(testUID)
			secCtx.IsNewUser = true
			secCtx.UserMessageCount = 0
			secCtx.IsSecondaryGroup = true
			secCtx.IsMemberOfPrimaryGroup = true
			secRes, err := d.Evaluate(secCtx)
			if err == nil {
				for _, res := range secRes {
					if res != nil && res.Triggered {
						report.FalsePositives = append(report.FalsePositives, RuleMatchViolation{
							TriggerID:   res.TriggerID,
							TriggerName: d.getTriggerName(res.TriggerID),
							User:        secCtx.User,
							ContextType: "known_good_secondary_group",
							Reason:      res.Reason,
						})
					}
				}
			}
		}
	}

	// 2. Evaluate Known Bad users (Bypassing reputation & banned requirements)
	for _, kb := range badUsers {
		kbCopy := kb
		badCtx := &TriggerContext{
			User: &db.User{
				UserID:       kbCopy.UserID,
				Username:     kbCopy.Username,
				FirstName:    kbCopy.FirstName,
				LastName:     kbCopy.LastName,
				LanguageCode: kbCopy.LanguageCode,
				IsPremium:    kbCopy.IsPremium,
				Reputation:   0,     // BYPASS
				IsAdmin:      false, // BYPASS
				IsBanned:     false, // BYPASS
			},
			UserBio:              kbCopy.Bio,
			LanguageCode:         kbCopy.LanguageCode,
			IsPremium:            kbCopy.IsPremium,
			HasPrivateForwards:   kbCopy.HasPrivateForwards,
			PersonalChatTitle:    kbCopy.PersonalChatTitle,
			PersonalChatUsername: kbCopy.PersonalChatUsername,
			BusinessIntro:        kbCopy.BusinessIntro,
			HasPhoto:             kbCopy.HasPhoto,
			PhotoCount:           kbCopy.PhotoCount,
			IsNewUser:            true,
			UserMessageCount:     0,
		}

		resList, err := d.Evaluate(badCtx)
		if err == nil {
			for _, res := range resList {
				if res != nil && res.Triggered {
					report.KnownBadMatches = append(report.KnownBadMatches, RuleMatchViolation{
						TriggerID:   res.TriggerID,
						TriggerName: d.getTriggerName(res.TriggerID),
						User:        badCtx.User,
						ContextType: "known_bad_profile",
						Reason:      res.Reason,
					})
				}
			}
		}

		for idx, msgText := range kbCopy.SampleMessages {
			msgCtx := &TriggerContext{
				User:             badCtx.User,
				UserBio:          kbCopy.Bio,
				Text:             msgText,
				IsNewUser:        true,
				UserMessageCount: 0,
				Message: &db.Message{
					ChatID:    123456,
					MessageID: idx + 1,
					UserID:    kbCopy.UserID,
					Text:      msgText,
				},
			}
			mResList, err := d.Evaluate(msgCtx)
			if err == nil {
				for _, res := range mResList {
					if res != nil && res.Triggered {
						report.KnownBadMatches = append(report.KnownBadMatches, RuleMatchViolation{
							TriggerID:   res.TriggerID,
							TriggerName: d.getTriggerName(res.TriggerID),
							User:        msgCtx.User,
							Message:     msgCtx.Message,
							ContextType: "known_bad_message",
							Reason:      res.Reason,
						})
					}
				}
			}
		}
	}

	report.Passed = len(report.FalsePositives) == 0
	report.Summary = FormatKnownDataReport(report)
	return report, nil
}

// ValidateTriggersAgainstKnownGoodAndBad is a convenience function that constructs a detector with the given triggers
// and validates them against known good and known bad datasets in database.
func ValidateTriggersAgainstKnownGoodAndBad(database *db.DB, triggers ...Trigger) (*KnownDataValidationReport, error) {
	det := NewDetector(triggers...)
	return det.ValidateAgainstKnownGoodAndBad(database)
}

// FormatKnownDataReport formats the validation report into a human-readable summary.
func FormatKnownDataReport(report *KnownDataValidationReport) string {
	if report == nil {
		return "No validation report available."
	}
	var sb strings.Builder
	sb.WriteString("📊 Known Data Rule Validation Report:\n")
	sb.WriteString(fmt.Sprintf("  • Known Good Profiles Evaluated: %d\n", report.KnownGoodTotal))
	sb.WriteString(fmt.Sprintf("  • Known Bad Profiles Evaluated:  %d\n", report.KnownBadTotal))
	sb.WriteString(fmt.Sprintf("  • False Positives (Violations):  %d\n", len(report.FalsePositives)))
	sb.WriteString(fmt.Sprintf("  • Known Bad Detections:          %d\n", len(report.KnownBadMatches)))

	if len(report.FalsePositives) > 0 {
		sb.WriteString("\n❌ FALSE POSITIVE REGRESSIONS (Rules falsely triggered on known good data):\n")
		for i, fp := range report.FalsePositives {
			sb.WriteString(fmt.Sprintf("  [%d] %s\n", i+1, fp.String()))
		}
	} else {
		sb.WriteString("\n✅ All Known Good profiles passed with 0 false positive regressions!\n")
	}

	return sb.String()
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
