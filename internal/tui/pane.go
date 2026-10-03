package tui

import (
	"context"
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
// the shell forwards every key (except Ctrl-P, Ctrl-B, Alt-1/2/3, Ctrl-S,
// Ctrl-Enter and Ctrl-C, which acts as Esc) to the pane, so typing Space, "g",
// "q", ":" or "1" never triggers a global shortcut or starts a leader sequence.
type Editor interface {
	// Editing reports whether a text field has focus.
	Editing() bool
}

// ModeReporter is implemented by panes whose text fields have an editing mode.
// The footer starts with the label while the pane has focus. Until the vim
// editor lands (plan U1, V2a), existing widgets report "INSERT" while a text
// field is being edited and "" otherwise.
type ModeReporter interface {
	// VimMode returns "NORMAL", "INSERT", "VISUAL", "VISUAL LINE", or "" when
	// no text field has focus.
	VimMode() string
}

// SectionLister is implemented by panes that have named sections (tabs). The
// shell uses it to decide which section jumps (g prefix) and section steps
// ([ and ]) are valid, so the which-key popup never lists a key that does
// nothing.
type SectionLister interface {
	// Sections returns the section names the pane currently offers, in tab
	// order, using the Section constants.
	Sections() []string
}

// MouseHandler is implemented by panes that accept mouse events. Coordinates
// are relative to the pane's inner body (below its header).
type MouseHandler interface {
	HandleMouse(msg tea.MouseMsg, x, y int) tea.Cmd
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

// Section names carried by [FocusSectionMsg]. The request pane understands
// the first eight, the WebSocket pane [SectionMessages] and the shared ones,
// and the response pane the Resp ones.
const (
	// SectionURL is the URL bar.
	SectionURL = "url"
	// SectionParams is the query parameters tab.
	SectionParams = "params"
	// SectionAuth is the authorization tab.
	SectionAuth = "auth"
	// SectionHeaders is the request headers tab.
	SectionHeaders = "headers"
	// SectionBody is the request body tab.
	SectionBody = "body"
	// SectionPre is the pre-request script tab.
	SectionPre = "pre"
	// SectionTests is the tests script tab.
	SectionTests = "tests"
	// SectionSettings is the request settings tab.
	SectionSettings = "settings"
	// SectionMessages is the WebSocket messages tab.
	SectionMessages = "messages"
	// SectionRespBody is the response Body tab.
	SectionRespBody = "resp-body"
	// SectionRespHistory is the response History tab (ignored until it
	// exists).
	SectionRespHistory = "resp-history"
)

// FocusSectionMsg asks the pane that owns Section to show it and put the
// cursor there. The shell sends it after focusing the owning slot, in answer
// to a g-prefix jump (gu, gp, ga, gh, gb, gs, gt, gS, gm, gr, gH). A pane
// ignores a section it does not have.
type FocusSectionMsg struct {
	// Section is one of the Section constants.
	Section string
}

// SectionStepMsg asks the focused pane to move to the previous (Delta -1) or
// next (Delta +1) section tab. It is sent for [ and ] while no text field has
// focus.
type SectionStepMsg struct {
	// Delta is -1 or +1.
	Delta int
}

// CancelOAuthMsg asks the request pane to cancel a running OAuth 2.0 sign-in.
// The shell sends it for Esc when another pane has focus, so the flow can be
// stopped from anywhere like a running send.
type CancelOAuthMsg struct{}

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

// ---- saving (additive, WS6a) ------------------------------------------------

// SaveKind says what a [SaveMsg] persists.
type SaveKind int

// Save kinds.
const (
	// SaveRequest writes one request of a collection (autosave).
	SaveRequest SaveKind = iota
	// SaveCollection writes a whole collection (collection variables).
	SaveCollection
	// SaveEnvironment writes one environment.
	SaveEnvironment
	// SaveGlobals writes the workspace globals.
	SaveGlobals
	// DeleteEnvironment removes one environment from disk.
	DeleteEnvironment
)

// SaveMsg asks the shell to persist data a pane or screen has already applied
// to the in-memory workspace. The shell snapshots it on the Update goroutine,
// writes it through the [Backend] one write at a time, keeps the file watcher
// from mistaking the write for an outside change, and answers every pane and
// screen with a [SaveDoneMsg].
type SaveMsg struct {
	// Kind picks what to write.
	Kind SaveKind
	// Collection owns the request for [SaveRequest] and is the collection for
	// [SaveCollection].
	Collection model.ID
	// ID is the request or environment ID; empty for [SaveGlobals] and
	// [SaveCollection].
	ID model.ID
}

// SaveDoneMsg reports a finished [SaveMsg]. Err is nil on success.
type SaveDoneMsg struct {
	// Kind and ID echo the request.
	Kind SaveKind
	// ID is the request, environment or collection ID that was written.
	ID model.ID
	// Err is the failure, or nil.
	Err error
}

// Optional Backend capabilities. The shell type-asserts for them, so a Backend
// that lacks one simply cannot do that operation.
type (
	// EnvironmentDeleter removes an environment file and its secrets.
	EnvironmentDeleter interface {
		DeleteEnvironment(ctx context.Context, id model.ID) error
	}
	// GlobalsSaver writes globals.yaml.
	GlobalsSaver interface {
		SaveGlobals(ctx context.Context, g *model.Globals) error
	}
	// Hydrator fills in secret variable values (and OAuth2 token secrets)
	// from the secret store, in place, on demand.
	Hydrator interface {
		HydrateVariables(ctx context.Context, vars []model.Variable) error
		HydrateAuth(ctx context.Context, a *model.Auth) error
	}
	// PathResolver maps a saved request to its YAML file, for $EDITOR.
	PathResolver interface {
		PathOf(c *model.Collection, id model.ID) (string, error)
	}
)

// ---- screens (additive, WS6a) -----------------------------------------------

// Screen is a full-body screen another package supplies: the environment
// editor and the cookie manager. While one is open it receives every key and
// every non-key message the shell does not consume itself.
type Screen interface {
	// Update handles a message. Returning a nil Screen closes it.
	Update(msg tea.Msg) (Screen, tea.Cmd)
	// View renders the screen at the size given to SetSize.
	View() string
	// SetSize sets the drawing area in cells.
	SetSize(w, h int)
	// KeyHints are the footer hints while the screen is open.
	KeyHints() []ui.Hint
}

// ScreenEnv is what the shell gives a [ScreenFactory].
type ScreenEnv struct {
	// PaneEnv carries the theme, registry and backend.
	PaneEnv
	// Workspace is the live in-memory workspace. Screens may edit it on the
	// Update goroutine and must persist edits with [SaveMsg].
	Workspace *model.Workspace
	// ActiveEnv is the active environment ID, or empty.
	ActiveEnv model.ID
	// Focus is the environment to open on, or empty for the active one.
	Focus model.ID
	// ASCII reports the ASCII glyph set.
	ASCII bool
	// RunScope seeds the collection runner when opening from the tree.
	RunScope *RunScope
}

// ScreenFactory builds a screen.
type ScreenFactory func(env ScreenEnv) Screen

// OpenScreenMsg asks the shell to show a screen (return it from a command or
// a palette action).
type OpenScreenMsg struct {
	// Screen is the screen to open.
	Screen Screen
}

// OpenSwitcherMsg asks the shell to open the request quick switcher overlay.
type OpenSwitcherMsg struct{}
