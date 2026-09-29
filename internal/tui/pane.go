package tui

import (
	"sync"

	tea "charm.land/bubbletea/v2"
	"github.com/shubham/postctl/internal/model"
	"github.com/shubham/postctl/internal/store"
	"github.com/shubham/postctl/internal/ui"
)

// Slot names one of the three regions of the main screen.
type Slot int

// Slots, in focus-cycling order.
const (
	// SlotSidebar is the collection tree.
	SlotSidebar Slot = iota
	// SlotRequest is the request editor (a placeholder until WS6 lands).
	SlotRequest
	// SlotResponse is the response viewer (a placeholder until WS6 lands).
	SlotResponse
)

// String returns the pane's user-facing title.
func (s Slot) String() string {
	switch s {
	case SlotSidebar:
		return "Collections"
	case SlotRequest:
		return "Request"
	default:
		return "Response"
	}
}

// Pane is what the shell asks of the Request and Response slots. WS6 supplies
// real implementations through [Options.NewRequestPane] and
// [Options.NewResponsePane]; the shell ships placeholders.
//
// Contract:
//   - Update receives every key press while the pane is focused, and every
//     non-key message (see the message types in this package) whether focused
//     or not. It must not block; effects are returned as commands.
//   - View returns exactly what fits in the size given to SetSize; the shell
//     pads or truncates defensively, but a pane that wraps and scrolls its own
//     content never shows a hard cut.
//   - Focus and Blur switch the pane's input state. Focus may return a command
//     such as a cursor blink.
//   - KeyHints are shown in the footer only while the pane is focused. Use
//     [ui.Hint.Priority] so the least important hints drop first on narrow
//     terminals.
//
// A pane may additionally implement [Titled], [Editor] and [ActionSource].
type Pane interface {
	// Update handles a message and returns the (possibly new) pane.
	Update(msg tea.Msg) (Pane, tea.Cmd)
	// View renders the pane at its current size.
	View() string
	// SetSize sets the content area in cells, excluding the title row the
	// shell draws.
	SetSize(w, h int)
	// Focus gives the pane input focus.
	Focus() tea.Cmd
	// Blur takes input focus away.
	Blur()
	// KeyHints lists the hints for the footer while focused.
	KeyHints() []ui.Hint
}

// Titled lets a pane override the title the shell draws above it.
type Titled interface {
	// Title is the pane title, for example "Request".
	Title() string
}

// Editor is implemented by panes with text fields. While Editing reports true
// the shell forwards every key (except Ctrl-P, Ctrl-B and Alt-1/2/3) to the
// pane, so typing "E", "q" or "1" never triggers a global shortcut.
type Editor interface {
	// Editing reports whether a text field has focus.
	Editing() bool
}

// ActionSource lets a pane contribute command-palette actions. The shell asks
// on every palette open, so the list may change with pane state. Actions can
// also be added with [Registry].
type ActionSource interface {
	// Actions returns the pane's current actions.
	Actions() []Action
}

// Action is a command-palette entry.
type Action struct {
	// ID is a stable identifier, for example "request.send".
	ID string
	// Title is the palette label in sentence case, for example "Send request".
	Title string
	// Group groups related actions in the palette and help, for example
	// "Request".
	Group string
	// Keys is the display form of the shortcut, if any, for example
	// "Ctrl-Enter".
	Keys string
	// Enabled reports whether the action applies right now; nil means always.
	Enabled func() bool
	// Run performs the action on the Update goroutine. It must not block;
	// effects are returned as a command.
	Run func() tea.Cmd
}

// Registry collects palette actions. Panes and other workstreams register
// providers once; the palette reads them when it opens.
type Registry struct {
	mu        sync.Mutex
	providers []func() []Action
}

// Add registers fixed actions.
func (r *Registry) Add(actions ...Action) {
	r.AddFunc(func() []Action { return actions })
}

// AddFunc registers a provider that is asked for its actions each time the
// palette opens.
func (r *Registry) AddFunc(provider func() []Action) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.providers = append(r.providers, provider)
}

// All returns every currently enabled action in registration order.
func (r *Registry) All() []Action {
	r.mu.Lock()
	providers := append([]func() []Action(nil), r.providers...)
	r.mu.Unlock()
	var out []Action
	for _, p := range providers {
		for _, a := range p() {
			if a.Run == nil || (a.Enabled != nil && !a.Enabled()) {
				continue
			}
			out = append(out, a)
		}
	}
	return out
}

// PaneEnv is what the shell gives a pane factory.
type PaneEnv struct {
	// Theme is the current theme; later changes arrive as [ThemeChangedMsg].
	Theme ui.Theme
	// Actions is where the pane can register palette actions.
	Actions *Registry
	// Backend persists workspace data (SaveRequest, SaveEnvironment, ...).
	// Panes must call it from commands, never from Update or View.
	Backend Backend
}

// PaneFactory builds a pane for a slot.
type PaneFactory func(env PaneEnv) Pane

// RequestRef identifies a request in the workspace.
type RequestRef struct {
	// Collection owns the request.
	Collection *model.Collection
	// Request is the selected request, or nil when the selection is a folder,
	// a collection or nothing.
	Request *model.Request
	// Path is the display path from the collection to the request, for
	// example ["Payments", "Charges", "Create"].
	Path []string
	// Problem is set when the selected row is a file that failed to load.
	Problem *store.FileError
}

// Messages the shell sends to panes. Panes receive them in Update whether or
// not they are focused.

// SelectionMsg tells panes which tree row is selected. Open is true when the
// user pressed Enter on a request or picked it in the palette, meaning "start
// working on this": the shell also moves focus to the request pane.
type SelectionMsg struct {
	// Ref is the selection. Ref.Request is nil when nothing editable is
	// selected.
	Ref RequestRef
	// Open is true for an explicit open, false for cursor movement.
	Open bool
}

// EnvironmentChangedMsg reports the active environment; Env is nil for none.
// It is also sent once after the workspace first loads.
type EnvironmentChangedMsg struct {
	// Env is the active environment, or nil.
	Env *model.Environment
}

// WorkspaceReloadedMsg reports a new in-memory workspace after load or a
// reload from disk (for example after `git pull`). Request pointers change
// across reloads; hold IDs, not pointers, across this message.
type WorkspaceReloadedMsg struct {
	// Workspace is the freshly loaded workspace.
	Workspace *model.Workspace
}

// ThemeChangedMsg reports a new theme (terminal background detected).
type ThemeChangedMsg struct {
	// Theme is the new theme.
	Theme ui.Theme
}

// Messages panes send to the shell (return them from commands).

// FocusPaneMsg asks the shell to focus a slot, for example the response pane
// after a send completes.
type FocusPaneMsg struct {
	// Slot is the slot to focus.
	Slot Slot
}

// ToastMsg asks the shell to show a transient status line.
type ToastMsg struct {
	// Kind picks the symbol and color.
	Kind ui.ToastKind
	// Text is plain, redacted text.
	Text string
}

// DirtyMsg marks a request as having unsaved edits; the tree shows a marker
// after its name.
type DirtyMsg struct {
	// ID is the request ID.
	ID model.ID
	// Dirty is the new state.
	Dirty bool
}

// ReloadMsg asks the shell to reload the workspace from disk, preserving the
// selection.
type ReloadMsg struct{}
