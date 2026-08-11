package detector

// Trigger defines the interface that all modular detection triggers must implement.
type Trigger interface {
	ID() string
	Name() string
	IsEnabled() bool
	Evaluate(ctx *TriggerContext) (*TriggerResult, error)
}
