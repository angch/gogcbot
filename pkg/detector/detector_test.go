package detector

import (
	"testing"

	"github.com/angch/gogcbot/pkg/db"
)

type dummyTrigger struct {
	id        string
	enabled   bool
	triggered bool
	actions   []Action
}

func (d *dummyTrigger) ID() string      { return d.id }
func (d *dummyTrigger) Name() string    { return "Dummy Trigger " + d.id }
func (d *dummyTrigger) IsEnabled() bool { return d.enabled }
func (d *dummyTrigger) Evaluate(ctx *TriggerContext) (*TriggerResult, error) {
	if !d.enabled || !d.triggered {
		return &TriggerResult{Triggered: false}, nil
	}
	return &TriggerResult{
		Triggered: true,
		TriggerID: d.id,
		Reason:    "Dummy fired",
		Actions:   d.actions,
	}, nil
}

func TestDetector_Evaluate(t *testing.T) {
	t1 := &dummyTrigger{
		id:        "trig1",
		enabled:   true,
		triggered: true,
		actions: []Action{
			{Type: ActionDeleteMessage},
			{Type: ActionBanUser},
			{Type: ActionAdjustReputation, RepDelta: -20},
		},
	}
	t2 := &dummyTrigger{
		id:        "trig2",
		enabled:   false,
		triggered: true,
		actions:   []Action{{Type: ActionFlagMessage}},
	}
	t3 := &dummyTrigger{
		id:        "trig3",
		enabled:   true,
		triggered: false,
	}

	det := NewDetector(t1, t2, t3)
	if len(det.Triggers()) != 3 {
		t.Fatalf("expected 3 triggers registered, got %d", len(det.Triggers()))
	}

	ctx := &TriggerContext{
		User: &db.User{UserID: 123},
		Text: "test",
	}

	results, err := det.Evaluate(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 triggered result, got %d", len(results))
	}

	res := results[0]
	if res.TriggerID != "trig1" {
		t.Errorf("expected TriggerID trig1, got %s", res.TriggerID)
	}
	if len(res.Actions) != 3 {
		t.Errorf("expected 3 actions, got %d", len(res.Actions))
	}
}
