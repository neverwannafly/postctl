package tui

import (
	"sort"

	tea "charm.land/bubbletea/v2"
	"github.com/shubham/postctl/internal/model"
	"github.com/shubham/postctl/internal/ui"
)

// kindRouter fills the Request slot when Options.NewPaneForKind is set. It
// holds the default pane and one pane per special request kind, and shows the
// one that matches the selected request. Only the active pane sees key presses
// and pastes; everything else (private messages, workspace and theme changes)
// reaches all of them, so a pane that is not on screen still receives its own
// results, such as WebSocket events.
type kindRouter struct {
	panes   []Pane // panes[0] is the default
	byKind  map[model.Kind]int
	active  int
	w, h    int
	focused bool
}

func newKindRouter(env PaneEnv, def Pane, factories map[model.Kind]PaneFactory) *kindRouter {
	r := &kindRouter{panes: []Pane{def}, byKind: map[model.Kind]int{}}
	kinds := make([]string, 0, len(factories))
	for k, f := range factories {
		if f != nil {
			kinds = append(kinds, string(k))
		}
	}
	sort.Strings(kinds)
	for _, k := range kinds {
		r.byKind[model.Kind(k)] = len(r.panes)
		r.panes = append(r.panes, factories[model.Kind(k)](env))
	}
	return r
}

// indexFor picks the pane for a selection.
func (r *kindRouter) indexFor(ref RequestRef) int {
	if ref.Request != nil {
		if i, ok := r.byKind[ref.Request.Kind]; ok {
			return i
		}
	}
	return 0
}

// update gives msg to child i and keeps the pane value it returns.
func (r *kindRouter) update(i int, msg tea.Msg) tea.Cmd {
	next, cmd := r.panes[i].Update(msg)
	r.panes[i] = next
	return cmd
}

// Update implements Pane.
func (r *kindRouter) Update(msg tea.Msg) (Pane, tea.Cmd) {
	switch m := msg.(type) {
	case tea.KeyMsg, tea.PasteMsg, tea.MouseMsg:
		return r, r.update(r.active, msg)
	case SelectionMsg:
		return r, r.onSelection(m)
	case FocusSectionMsg, SectionStepMsg:
		// Only the pane on screen has the section.
		return r, r.update(r.active, msg)
	}
	cmds := make([]tea.Cmd, len(r.panes))
	for i := range r.panes {
		cmds[i] = r.update(i, msg)
	}
	return r, tea.Batch(cmds...)
}

// onSelection switches to the pane for the selected request. The pane that
// steps down gets an empty selection so it saves and lets go of its request.
func (r *kindRouter) onSelection(m SelectionMsg) tea.Cmd {
	var cmds []tea.Cmd
	if next := r.indexFor(m.Ref); next != r.active {
		old := r.active
		cmds = append(cmds, r.update(old, SelectionMsg{}))
		if r.focused {
			r.panes[old].Blur()
			if b, ok := r.panes[old].(interface{ BlurCmd() tea.Cmd }); ok {
				cmds = append(cmds, b.BlurCmd())
			}
		}
		r.active = next
		r.panes[next].SetSize(r.w, r.h)
		if r.focused {
			cmds = append(cmds, r.panes[next].Focus())
		}
	}
	cmds = append(cmds, r.update(r.active, m))
	return tea.Batch(cmds...)
}

// View implements Pane.
func (r *kindRouter) View() string { return r.panes[r.active].View() }

// SetSize implements Pane. Every child is sized so a switch never shows a
// stale layout.
func (r *kindRouter) SetSize(w, h int) {
	r.w, r.h = w, h
	for _, p := range r.panes {
		p.SetSize(w, h)
	}
}

// Focus implements Pane.
func (r *kindRouter) Focus() tea.Cmd {
	r.focused = true
	return r.panes[r.active].Focus()
}

// Blur implements Pane.
func (r *kindRouter) Blur() {
	r.focused = false
	r.panes[r.active].Blur()
}

// KeyHints implements Pane.
func (r *kindRouter) KeyHints() []ui.Hint { return r.panes[r.active].KeyHints() }

// Title implements Titled: the active pane's title, or "Request".
func (r *kindRouter) Title() string {
	if t, ok := r.panes[r.active].(Titled); ok {
		return t.Title()
	}
	return SlotRequest.String()
}

// Editing implements Editor.
func (r *kindRouter) Editing() bool {
	e, ok := r.panes[r.active].(Editor)
	return ok && e.Editing()
}

// VimMode implements ModeReporter for the active pane.
func (r *kindRouter) VimMode() string {
	if m, ok := r.panes[r.active].(ModeReporter); ok {
		return m.VimMode()
	}
	return ""
}

// Sections implements SectionLister for the active pane.
func (r *kindRouter) Sections() []string {
	if l, ok := r.panes[r.active].(SectionLister); ok {
		return l.Sections()
	}
	return nil
}

// BlurCmd runs the active pane's save-on-blur command.
func (r *kindRouter) BlurCmd() tea.Cmd {
	if b, ok := r.panes[r.active].(interface{ BlurCmd() tea.Cmd }); ok {
		return b.BlurCmd()
	}
	return nil
}

// Actions implements ActionSource: the union of the children's actions, each
// of which is enabled only while it applies.
func (r *kindRouter) Actions() []Action {
	var out []Action
	for _, p := range r.panes {
		if src, ok := p.(ActionSource); ok {
			out = append(out, src.Actions()...)
		}
	}
	return out
}
