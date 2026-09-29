package tuiengine

import (
	"github.com/shubham/postctl/internal/engine"
	"github.com/shubham/postctl/internal/tui"
)

// OutcomeToResult maps an engine outcome to the TUI send result shape.
func OutcomeToResult(out *engine.Outcome) tui.SendResult {
	if out == nil {
		return tui.SendResult{}
	}
	return result(out)
}
