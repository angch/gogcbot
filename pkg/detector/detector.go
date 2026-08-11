package detector

import (
	"fmt"
	"sync"
)

// Detector manages a collection of registered triggers and runs them against message context.
type Detector struct {
	triggers []Trigger
	mu       sync.RWMutex
}

// NewDetector constructs a new Detector initialized with the provided triggers.
func NewDetector(triggers ...Trigger) *Detector {
	d := &Detector{
		triggers: make([]Trigger, 0),
	}
	for _, t := range triggers {
		if t != nil {
			d.triggers = append(d.triggers, t)
		}
	}
	return d
}

// RegisterTrigger registers a new Trigger into the detector pipeline.
func (d *Detector) RegisterTrigger(t Trigger) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if t != nil {
		d.triggers = append(d.triggers, t)
	}
}

// Triggers returns a copy of all currently registered triggers.
func (d *Detector) Triggers() []Trigger {
	d.mu.RLock()
	defer d.mu.RUnlock()
	tCopy := make([]Trigger, len(d.triggers))
	copy(tCopy, d.triggers)
	return tCopy
}

// Evaluate runs all enabled triggers against the context and returns all triggered results.
func (d *Detector) Evaluate(ctx *TriggerContext) ([]*TriggerResult, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	var results []*TriggerResult
	for _, t := range d.triggers {
		if !t.IsEnabled() {
			continue
		}
		res, err := t.Evaluate(ctx)
		if err != nil {
			return nil, fmt.Errorf("trigger %s failed evaluation: %w", t.ID(), err)
		}
		if res != nil && res.Triggered {
			if res.TriggerID == "" {
				res.TriggerID = t.ID()
			}
			results = append(results, res)
		}
	}
	return results, nil
}
