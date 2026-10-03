package tui

import (
	tea "charm.land/bubbletea/v2"
	"github.com/shubham/postctl/internal/model"
)

// OpenRequestHistoryMsg asks the shell to open the history screen limited to
// one request's sends, on the detail view of EntryID when it is set. The
// response pane's History tab sends it for Enter on an entry.
type OpenRequestHistoryMsg struct {
	// RequestID is the request whose sends the screen lists.
	RequestID model.ID
	// EntryID is the history entry to open on, or empty for the list.
	EntryID string
}

// openRequestHistory shows the history screen for msg. The screen is told it
// opened so it loads at once.
func (a *app) openRequestHistory(msg OpenRequestHistoryMsg) tea.Cmd {
	if a.opts.NewHistory == nil || a.ws == nil {
		return a.toastInfo("History is not available in this build.")
	}
	env := a.screenEnv("")
	env.HistoryRequest = msg.RequestID
	env.HistoryEntry = msg.EntryID
	open := a.openScreen(a.opts.NewHistory(env))
	return tea.Batch(open, func() tea.Msg { return ScreenOpenedMsg{} })
}
