package response

import (
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/shubham/postctl/internal/tui"
)

// Deps are the effects the response pane needs from its host. The zero value
// is valid: without Log the pane has no History tab and no diff action.
type Deps struct {
	// Log opens the workspace send log. It is called from commands, never
	// from Update or View.
	Log func() (tui.HistoryReader, error)
	// Now supplies the clock for relative times in the History tab; nil
	// means time.Now.
	Now func() time.Time
	// Clipboard copies text; nil uses the system clipboard.
	Clipboard func(text string) tea.Cmd
}

// Factory returns a [tui.PaneFactory] that builds response panes with d.
func Factory(d Deps) tui.PaneFactory {
	return func(env tui.PaneEnv) tui.Pane { return NewWithDeps(env, d) }
}

func (d Deps) now() time.Time {
	if d.Now != nil {
		return d.Now()
	}
	return time.Now()
}
