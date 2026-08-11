package detector

import "fmt"

// ActionType defines the type of action to be executed when a detection trigger fires.
type ActionType string

const (
	ActionDeleteMessage    ActionType = "delete_message"
	ActionBanUser          ActionType = "ban_user"
	ActionAdjustReputation ActionType = "adjust_reputation"
	ActionFlagMessage      ActionType = "flag_message"
)

// Action represents an action to execute in response to a triggered detection rule.
type Action struct {
	Type     ActionType `json:"type" yaml:"type"`
	RepDelta int        `json:"rep_delta,omitempty" yaml:"rep_delta,omitempty"` // Reputation change delta (e.g., -20)
	Reason   string     `json:"reason,omitempty" yaml:"reason,omitempty"`       // Rationale for taking this action
}

func (a Action) String() string {
	switch a.Type {
	case ActionAdjustReputation:
		return fmt.Sprintf("Action(%s, delta=%d, reason=%q)", a.Type, a.RepDelta, a.Reason)
	default:
		return fmt.Sprintf("Action(%s, reason=%q)", a.Type, a.Reason)
	}
}
