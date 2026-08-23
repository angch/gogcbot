package detector

import (
	"testing"

	"github.com/angch/gogcbot/pkg/db"
)

func TestSecondaryGroupTrigger_NotSecondaryGroup(t *testing.T) {
	trig := NewSecondaryGroupTrigger(SecondaryGroupTriggerConfig{
		Enabled:    true,
		RepPenalty: 20,
	})

	ctx := &TriggerContext{
		User: &db.User{
			UserID:     123456,
			Username:   "normaluser",
			Reputation: 0,
		},
		ChatID:                 -1001072966891,
		GroupTitle:             "Primary Golang Group",
		IsSecondaryGroup:       false,
		IsMemberOfPrimaryGroup: false,
	}

	res, err := trig.Evaluate(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Triggered {
		t.Errorf("expected trigger not to fire for primary group, got triggered: true")
	}
}

func TestSecondaryGroupTrigger_UserIsMemberOfPrimaryGroup(t *testing.T) {
	trig := NewSecondaryGroupTrigger(SecondaryGroupTriggerConfig{
		Enabled:    true,
		RepPenalty: 20,
	})

	ctx := &TriggerContext{
		User: &db.User{
			UserID:     123456,
			Username:   "gooduser",
			Reputation: 0,
		},
		ChatID:                 -1001783678483,
		GroupTitle:             "Overflow Secondary Group",
		IsSecondaryGroup:       true,
		IsMemberOfPrimaryGroup: true,
		PrimaryGroupNames:      "'Golang Malaysia' (-1001072966891)",
	}

	res, err := trig.Evaluate(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Triggered {
		t.Errorf("expected trigger not to fire when user is a member of primary group")
	}
}

func TestSecondaryGroupTrigger_UserNotMemberOfPrimaryGroup_Triggers(t *testing.T) {
	trig := NewSecondaryGroupTrigger(SecondaryGroupTriggerConfig{
		Enabled:    true,
		RepPenalty: 20,
	})

	ctx := &TriggerContext{
		User: &db.User{
			UserID:     998877,
			Username:   "intruder",
			FirstName:  "Intruder",
			LastName:   "User",
			Reputation: 0,
		},
		ChatID:                 -1001783678483,
		GroupTitle:             "Overflow Secondary Group",
		IsSecondaryGroup:       true,
		IsMemberOfPrimaryGroup: false,
		PrimaryGroupNames:      "'Golang Malaysia' (-1001072966891)",
	}

	res, err := trig.Evaluate(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.Triggered {
		t.Fatalf("expected trigger to fire for user not in primary group")
	}
	if res.TriggerID != "secondary_group_not_in_primary" {
		t.Errorf("expected trigger ID 'secondary_group_not_in_primary', got %s", res.TriggerID)
	}

	hasBan := false
	hasDelete := false
	hasRep := false
	for _, act := range res.Actions {
		if act.Type == ActionBanUser {
			hasBan = true
		}
		if act.Type == ActionDeleteMessage {
			hasDelete = true
		}
		if act.Type == ActionAdjustReputation && act.RepDelta == -20 {
			hasRep = true
		}
	}
	if !hasBan || !hasDelete || !hasRep {
		t.Errorf("expected Actions to contain BanUser, DeleteMessage, and AdjustReputation(-20), got: %+v", res.Actions)
	}
}

func TestSecondaryGroupTrigger_AdminExemption(t *testing.T) {
	trig := NewSecondaryGroupTrigger(SecondaryGroupTriggerConfig{
		Enabled:    true,
		RepPenalty: 20,
	})

	ctx := &TriggerContext{
		User: &db.User{
			UserID:     111,
			Username:   "admin",
			IsAdmin:    true,
			Reputation: 100,
		},
		ChatID:                 -1001783678483,
		GroupTitle:             "Secondary Group",
		IsSecondaryGroup:       true,
		IsMemberOfPrimaryGroup: false,
	}

	res, err := trig.Evaluate(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Triggered {
		t.Errorf("expected admin to be exempt from secondary group trigger")
	}
}

func TestSecondaryGroupTrigger_Disabled(t *testing.T) {
	trig := NewSecondaryGroupTrigger(SecondaryGroupTriggerConfig{
		Enabled:    false,
		RepPenalty: 20,
	})

	ctx := &TriggerContext{
		User: &db.User{
			UserID:     222,
			Username:   "user",
			Reputation: 0,
		},
		ChatID:                 -1001783678483,
		IsSecondaryGroup:       true,
		IsMemberOfPrimaryGroup: false,
	}

	res, err := trig.Evaluate(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Triggered {
		t.Errorf("expected disabled trigger not to fire")
	}
}
