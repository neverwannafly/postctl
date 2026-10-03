package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/shubham/postctl/internal/model"
)

// builtinActions are the shell's own palette actions.
func (a *app) builtinActions() []Action {
	in := func(s Slot) func() tea.Cmd { return func() tea.Cmd { return a.setFocus(s) } }
	return []Action{
		{ID: "request.find", Title: "Find request", Group: "Navigation", Keys: "",
			Run: func() tea.Cmd { return a.openSwitcher() }},
		{ID: "env.switch", Title: "Switch environment", Group: "Environment", Keys: "E",
			Run: func() tea.Cmd { a.ov = newEnvOverlay(a); return nil }},
		{ID: "tree.new", Title: "New request, folder or collection", Group: "Collections", Keys: "n",
			Run: func() tea.Cmd { return a.setFocusThen(SlotSidebar, a.openNew) }},
		{ID: "tree.newWebSocket", Title: "New WebSocket request", Group: "Collections",
			Enabled: func() bool { return a.hasKindPane(model.KindWebSocket) },
			Run:     func() tea.Cmd { return a.setFocusThen(SlotSidebar, a.openNewWebSocket) }},
		{ID: "tree.filter", Title: "Filter requests", Group: "Collections", Keys: "/",
			Run: func() tea.Cmd { return tea.Batch(a.setFocus(SlotSidebar), a.tree.startFilter()) }},
		{ID: "focus.collections", Title: "Go to collections", Group: "Navigation", Keys: "1", Run: in(SlotSidebar)},
		{ID: "focus.request", Title: "Go to request", Group: "Navigation", Keys: "2", Run: in(SlotRequest)},
		{ID: "focus.response", Title: "Go to response", Group: "Navigation", Keys: "3",
			Enabled: func() bool { return !a.responseHidden() }, Run: in(SlotResponse)},
		{ID: "workspace.reload", Title: "Reload workspace from disk", Group: "Workspace", Keys: "Ctrl-R",
			Run: func() tea.Cmd { return a.reload() }},
		{ID: "help.show", Title: "Show help", Group: "Help", Keys: "?",
			Run: func() tea.Cmd { a.ov = &helpOverlay{}; return nil }},
		{ID: "app.quit", Title: "Quit", Group: "Workspace", Keys: "q",
			Run: func() tea.Cmd { return a.quit(false) }},
	}
}

// setFocusThen focuses a slot and then runs f.
func (a *app) setFocusThen(s Slot, f func() tea.Cmd) tea.Cmd {
	return tea.Batch(a.setFocus(s), f())
}

// reload re-reads the workspace unless writes are still in flight.
func (a *app) reload() tea.Cmd {
	if a.saver.busy() {
		return a.toastInfo("Still saving. Reload once the save finishes.")
	}
	return tea.Batch(a.loadCmd(nil), a.toastInfo("Reloading from disk"))
}

// paneActions asks the panes for their palette actions.
func (a *app) paneActions() []Action {
	var out []Action
	panes := []Pane{a.request}
	if !a.responseHidden() {
		panes = append(panes, a.response)
	}
	for _, p := range panes {
		if src, ok := p.(ActionSource); ok {
			out = append(out, src.Actions()...)
		}
	}
	return out
}

// paletteEntry payloads.
type (
	actionPayload  struct{ act Action }
	envPayload     struct{ id model.ID }
	requestPayload struct{ id model.ID }
)

// openPalette builds the palette from the current state: actions and
// environments first (they are what an empty query shows), then every
// request with its path.
func (a *app) openPalette() tea.Cmd {
	var items []pickItem
	for _, act := range a.registry.All() {
		detail := act.Keys
		if detail == "" {
			detail = act.Group
		}
		items = append(items, pickItem{label: act.Title, detail: detail, payload: actionPayload{act}})
	}
	if a.ws != nil {
		if len(a.ws.Environments) > 0 {
			items = append(items, pickItem{label: "Use no environment", detail: "environment", payload: envPayload{}})
		}
		for _, e := range a.ws.Environments {
			label := "Use environment: " + e.Name
			if e.Production {
				label += " (production)"
			}
			items = append(items, pickItem{label: label, detail: "environment", payload: envPayload{e.ID}})
		}
		arrow := " " + a.theme.Glyphs.Arrow + " "
		for _, c := range a.ws.Collections {
			_ = c.Walk(func(it model.Item, _ model.Container, _ int) error {
				if r, ok := it.(*model.Request); ok {
					n := a.tree.nodes[r.ID]
					items = append(items, pickItem{label: strings.Join(n.path, arrow), method: methodBadge(r), payload: requestPayload{r.ID}})
				}
				return nil
			})
		}
	}
	p, cmd := newPicker(a, "Command palette", "search requests, environments and actions", "Nothing to search yet. Press Esc, then n to create a request.", items,
		func(a *app, it pickItem) tea.Cmd {
			switch v := it.payload.(type) {
			case actionPayload:
				return v.act.Run()
			case envPayload:
				return a.setEnvironment(v.id, true)
			case requestPayload:
				if !a.tree.reveal(v.id) {
					return a.toastWarn("That request no longer exists.")
				}
				a.layoutPanes()
				return a.syncSelection(true)
			}
			return nil
		})
	a.ov = p
	return cmd
}
