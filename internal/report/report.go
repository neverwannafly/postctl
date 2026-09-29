// Package report writes machine-readable collection-run reports.
package report

import "github.com/shubham/postctl/internal/engine"

// Reporter consumes collection-run events.
type Reporter interface {
	Event(engine.RunEvent) error
	Close() error
}

// Options controls safe text transformation in reports.
type Options struct {
	// Mask removes secret values from errors, warnings and assertion messages.
	// Nil leaves text unchanged.
	Mask func(string) string
}

func (o Options) mask(s string) string {
	if o.Mask != nil {
		return o.Mask(s)
	}
	return s
}

// Write sends events to a reporter and closes it.
func Write(r Reporter, events []engine.RunEvent) error {
	for _, ev := range events {
		if err := r.Event(ev); err != nil {
			return err
		}
	}
	return r.Close()
}
