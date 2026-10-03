// Package tui is postctl's terminal UI shell: the responsive three/two/one
// pane layout, focus and navigation, the collection tree, the environment
// switcher, the command palette, help, first-run and error states, and file
// watching. Request editing and response viewing plug in as [Pane]s.
//
// The package follows the Bubble Tea rules from docs/tui-stack.md: Update
// mutates state and returns commands; disk access happens only inside
// commands that return typed messages; View is a pure function of state.
package tui

import (
	"context"
	"errors"
	"os"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"github.com/shubham/postctl/internal/model"
	"github.com/shubham/postctl/internal/store"
	"github.com/shubham/postctl/internal/ui"
)

// Options configures [Run] and [New].
type Options struct {
	// Backend persists the workspace. Required.
	Backend Backend
	// Accessible selects the linear presentation: no alternate screen, no
	// animation, no color, ASCII glyphs, a single column.
	Accessible bool
	// ASCII swaps Unicode glyphs for ASCII.
	ASCII bool
	// NoColor disables color (NO_COLOR).
	NoColor bool
	// NewRequestPane and NewResponsePane supply the panes for the Request and
	// Response slots. Nil selects the built-in placeholders.
	NewRequestPane, NewResponsePane PaneFactory
	// NewPaneForKind supplies alternative Request-slot panes for requests of
	// other kinds (WebSocket). The pane from NewRequestPane stays the default
	// for every kind not listed. Nil keeps a single pane for all requests.
	NewPaneForKind map[model.Kind]PaneFactory
	// Sender runs the send pipeline for the Request pane's send key. Nil
	// disables sending (the pane says so). cmd/postctl supplies the engine
	// adapter, or a fake until the engine lands.
	Sender Sender
	// NewEnvEditor and NewCookieManager supply the environment editor
	// (Ctrl-E) and the cookie manager screens. Nil hides their actions.
	NewEnvEditor, NewCookieManager ScreenFactory
	// NewRunner and NewHistory supply the collection runner and history
	// screens. Nil hides their actions and keys.
	NewRunner, NewHistory ScreenFactory
	// Registry receives palette actions; a fresh one is used when nil.
	Registry *Registry
	// OnEdge is called when Ctrl-h/j/k/l is pressed and postctl has no pane
	// in that direction (plan U2). The terminal-handoff workstream sets it to
	// hand the move to tmux. Nil does nothing.
	OnEdge func(dir Direction) tea.Cmd
	// NoWatch disables file watching (tests).
	NoWatch bool
	// Light assumes a light terminal background until the terminal answers;
	// the default is dark.
	Light bool
}

// phase is the top-level state of the shell.
type phase int

const (
	phaseLoading phase = iota // opening the workspace
	phaseReady                // workspace on screen (possibly the first-run guide)
	phaseFatal                // the workspace could not be opened at all
)

const (
	toastTTL        = 3 * time.Second
	loadingDelay    = 250 * time.Millisecond
	selfWriteWindow = 900 * time.Millisecond
	minWidth        = 40
	minHeight       = 8
)

type toastState struct {
	id     int
	kind   ui.ToastKind
	text   string
	sticky bool
}

// app is the root model. It is used through a pointer: Update mutates in place
// and returns the same pointer.
type app struct {
	opts Options
	be   Backend
	keys keyMap

	theme                      ui.Theme
	dark                       bool
	noColor, ascii, accessible bool
	width, height              int

	phase       phase
	fatal       error
	showLoading bool
	spin        spinner.Model

	ws             *model.Workspace
	problems       []*store.FileError
	state          store.State
	activeEnv      model.ID
	notInitialized bool // the directory has no postctl.yaml yet
	loadSeq        int
	recent         []model.ID

	focus     Slot
	prevFocus Slot // where Esc returns to from the single-pane sidebar overlay
	rightShow Slot // two-pane layout: which of Request/Response is shown

	tree     *treeState
	request  Pane
	response Pane
	registry *Registry
	first    firstRunState
	lastSel  selectionSig

	ov overlay

	toast    *toastState
	toastSeq int

	pend          *pendingKeys // a prefix key waiting for its follow-up
	pendSeq       int
	whichKeyDelay time.Duration
	lastCtrlC     time.Time // the first press of a Ctrl-C quit
	jumps         jumpList
	jumping       bool // a jump-list move is opening a request
	enhanced      bool // the terminal reports disambiguated keys (Ctrl-i)

	foldSeq        int
	sends          map[model.ID]*flight
	saver          saveQueue
	quitting       bool
	selfWriteUntil time.Time
	watch          watcher
	watching       bool
	now            func() time.Time
}

// New builds the root model. Most callers use [Run].
func New(opts Options) tea.Model { return newApp(opts) }

func newApp(opts Options) *app {
	a := &app{
		opts:       opts,
		be:         opts.Backend,
		accessible: opts.Accessible,
		ascii:      opts.ASCII || opts.Accessible,
		noColor:    opts.NoColor || opts.Accessible,
		dark:       !opts.Light,
		width:      100,
		height:     30,
		focus:      SlotSidebar,
		prevFocus:  SlotRequest,
		rightShow:  SlotRequest,
		registry:   opts.Registry,
		now:        time.Now,

		whichKeyDelay: whichKeyDelay,
	}
	if a.registry == nil {
		a.registry = &Registry{}
	}
	a.keys = newKeyMap(a.ascii)
	a.theme = ui.NewTheme(a.dark, a.noColor, a.ascii)
	a.spin = a.theme.NewSpinner()
	a.tree = newTree(a.theme)

	env := PaneEnv{Theme: a.theme, Actions: a.registry, Backend: a.be}
	newReq, newResp := opts.NewRequestPane, opts.NewResponsePane
	if newReq == nil {
		newReq = newRequestPlaceholder
	}
	if newResp == nil {
		newResp = newResponsePlaceholder
	}
	a.request, a.response = newReq(env), newResp(env)
	if len(opts.NewPaneForKind) > 0 {
		a.request = newKindRouter(env, a.request, opts.NewPaneForKind)
	}
	a.registry.AddFunc(a.builtinActions)
	a.registry.AddFunc(a.paneActions)
	a.registry.AddFunc(a.screenActions)
	a.layoutPanes()
	return a
}

// Init starts loading the workspace and asks the terminal for its background.
func (a *app) Init() tea.Cmd {
	cmds := []tea.Cmd{a.loadCmd(nil), tea.RequestBackgroundColor}
	if !a.accessible {
		cmds = append(cmds, tea.Tick(loadingDelay, func(time.Time) tea.Msg { return showLoadingMsg{} }))
	}
	return tea.Batch(cmds...)
}

// ---- messages ---------------------------------------------------------------

type (
	showLoadingMsg struct{}
	toastExpireMsg int
	loadedMsg      struct {
		seq      int
		ws       *model.Workspace
		problems []*store.FileError
		state    store.State
		err      error
		then     func(a *app) tea.Cmd
	}
)

// loadCmd reads the workspace and local state off the Update goroutine. then,
// when set, runs after the result is applied (used by first-run creation).
func (a *app) loadCmd(then func(a *app) tea.Cmd) tea.Cmd {
	a.loadSeq++
	seq, be := a.loadSeq, a.be
	return func() tea.Msg {
		ws, problems, err := be.Load(context.Background())
		msg := loadedMsg{seq: seq, ws: ws, problems: problems, err: err, then: then}
		if err == nil {
			msg.state, _ = be.LoadState() // a broken state file must not block opening
		}
		return msg
	}
}

// ---- update -----------------------------------------------------------------

// Update implements tea.Model.
func (a *app) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.width, a.height = msg.Width, msg.Height
		a.layoutPanes()
		if o, ok := a.ov.(interface{ setSize(int, int) }); ok {
			g := a.geo()
			o.setSize(g.width, g.bodyH)
		}
		return a, nil
	case tea.BackgroundColorMsg:
		return a, a.setDark(msg.IsDark())
	case tea.KeyPressMsg:
		return a, tea.Batch(a.onKey(msg), a.syncFold())
	case whichKeyMsg:
		a.onWhichKey(msg)
		return a, nil
	case tea.KeyboardEnhancementsMsg:
		a.enhanced = msg.SupportsKeyDisambiguation()
		return a, a.forward(msg)
	case tea.PasteMsg:
		return a, a.onPaste(msg)
	case showLoadingMsg:
		if a.phase == phaseLoading {
			a.showLoading = true
			return a, a.spin.Tick
		}
		return a, nil
	case spinner.TickMsg:
		if a.phase != phaseLoading || a.accessible {
			return a, nil
		}
		var cmd tea.Cmd
		a.spin, cmd = a.spin.Update(msg)
		return a, cmd
	case toastExpireMsg:
		if a.toast != nil && a.toast.id == int(msg) {
			a.toast = nil
		}
		return a, nil
	case loadedMsg:
		return a, a.onLoaded(msg)
	case savedMsg:
		return a, a.onSaved(msg)
	case stateSavedMsg:
		if msg.err != nil {
			return a, a.toastWarn("Could not remember the environment choice: " + errText(msg.err))
		}
		return a, nil
	case fsChangedMsg:
		return a, a.onFSChanged(msg)
	case watchReadyMsg:
		return a, a.onWatchReady(msg)
	case FocusPaneMsg:
		return a, a.setFocus(msg.Slot)
	case ToastMsg:
		return a, a.toastKind(msg.Kind, msg.Text)
	case DirtyMsg:
		a.tree.dirty[msg.ID] = msg.Dirty
		a.tree.cache = nil
		return a, nil
	case ReloadMsg:
		return a, a.loadCmd(nil)
	case quitNowMsg:
		return a, a.quit(true)
	case SendRequestMsg:
		return a, a.startSend(msg.Request)
	case RetrySendMsg:
		return a, a.retrySend(msg.RequestID)
	case CancelSendMsg:
		a.cancelSend(msg.RequestID)
		return a, nil
	case sendEventsMsg:
		return a, a.onSendEvents(msg)
	case SaveMsg:
		return a, a.onSaveMsg(msg)
	case OpenScreenMsg:
		return a, a.openScreen(msg.Screen)
	case OpenSwitcherMsg:
		return a, a.openSwitcher()
	case OpenRequestHistoryMsg:
		return a, a.openRequestHistory(msg)
	case SelectRequestMsg:
		return a, a.selectRequest(msg.RequestID)
	case CreateRequestFromHistoryMsg:
		return a, a.createRequestFromHistory(msg)
	case SendSavedRequestMsg:
		return a, a.sendSavedRequest(msg.RequestID)
	case foldPersistMsg:
		return a, a.onFoldPersist(msg)
	}
	// Anything else (including pane-private messages) goes to both panes.
	return a, a.forward(msg)
}

// forward gives msg to both panes and batches their commands.
func (a *app) forward(msg tea.Msg) tea.Cmd {
	var c1, c2 tea.Cmd
	a.request, c1 = a.request.Update(msg)
	a.response, c2 = a.response.Update(msg)
	var c3 tea.Cmd
	if _, ok := a.ov.(*screenOverlay); ok {
		c3 = a.updateOverlay(msg)
	}
	return tea.Batch(c1, c2, c3)
}

// broadcast is forward for shell-originated messages.
func (a *app) broadcast(msg tea.Msg) tea.Cmd { return a.forward(msg) }

func (a *app) setDark(dark bool) tea.Cmd {
	a.dark = dark
	a.theme = ui.NewTheme(dark, a.noColor, a.ascii)
	a.spin = a.theme.NewSpinner()
	a.tree.setTheme(a.theme)
	return a.broadcast(ThemeChangedMsg{Theme: a.theme})
}

// onLoaded applies a load result.
func (a *app) onLoaded(msg loadedMsg) tea.Cmd {
	if msg.seq != a.loadSeq {
		return nil // superseded by a newer load
	}
	if msg.err != nil {
		return a.onLoadFailed(msg)
	}
	first := a.phase == phaseLoading
	a.phase = phaseReady
	a.notInitialized = false
	a.fatal = nil
	a.ws, a.problems = msg.ws, msg.problems
	if first {
		a.state = msg.state
		a.activeEnv = msg.state.ActiveEnvironment
	}
	if a.activeEnv != "" && a.ws.Environment(a.activeEnv) == nil {
		a.activeEnv = "" // the environment was deleted on disk
	}
	if first {
		a.tree.seedFolds(a.state.Folded, a.ws)
	}
	a.tree.setWorkspace(a.ws, a.problems, a.be.Root())
	a.layoutPanes()

	cmds := []tea.Cmd{a.broadcast(WorkspaceReloadedMsg{Workspace: a.ws})}
	if first {
		cmds = append(cmds, a.broadcast(EnvironmentChangedMsg{Env: a.activeEnvironment()}))
	}
	cmds = append(cmds, a.syncSelection(false))
	if !a.watching && !a.opts.NoWatch {
		a.watching = true
		cmds = append(cmds, startWatchCmd(a.be.Root()))
	}
	if msg.then != nil {
		cmds = append(cmds, msg.then(a))
	}
	return tea.Batch(cmds...)
}

func (a *app) onLoadFailed(msg loadedMsg) tea.Cmd {
	if errors.Is(msg.err, store.ErrNotWorkspace) {
		// No workspace here yet: the first-run guide, not an error.
		a.phase = phaseReady
		a.notInitialized = true
		a.ws, a.problems = nil, nil
		a.tree.setWorkspace(nil, nil, a.be.Root())
		if msg.then != nil {
			return msg.then(a)
		}
		return nil
	}
	if a.phase == phaseReady && a.notInitialized {
		// Setting up a new workspace failed; stay on the guide.
		return a.toastErr(errText(msg.err) + " Nothing was created.")
	}
	if a.ws != nil {
		// A reload failed: keep the last good copy on screen.
		return a.toastErr("Could not reload the workspace: " + errText(msg.err) + " The copy on screen is unchanged.")
	}
	a.phase = phaseFatal
	a.fatal = msg.err
	return nil
}

// errText renders an error for the user without the wrapping noise.
func errText(err error) string {
	var fe *store.FileError
	if errors.As(err, &fe) {
		return firstLine(fe.Error())
	}
	return firstLine(err.Error())
}

// onFSChanged reacts to a debounced burst of file changes.
func (a *app) onFSChanged(msg fsChangedMsg) tea.Cmd {
	next := waitWatchCmd(msg.w)
	if a.phase == phaseLoading || a.saver.busy() || a.now().Before(a.selfWriteUntil) {
		// Our own writes (or a load already in progress) caused this.
		return next
	}
	return tea.Batch(a.loadCmd(nil), next)
}

func (a *app) onWatchReady(msg watchReadyMsg) tea.Cmd {
	if msg.err != nil {
		a.watching = false
		return a.toastWarn("Not watching files for changes: " + errText(msg.err) + " Press Ctrl-R to reload.")
	}
	a.watch = msg.w
	return waitWatchCmd(msg.w)
}

// ---- quitting ---------------------------------------------------------------

type quitNowMsg struct{}

// quit leaves the program. Pending saves finish first so no edit is lost;
// force skips that wait.
func (a *app) quit(force bool) tea.Cmd {
	a.cancelAllSends()
	if !force {
		if flush, n := a.flushDirty(); n > 0 && !a.quitting {
			// Edits whose autosave timer has not fired yet are written now.
			a.quitting = true
			return tea.Batch(flush, a.toastInfo("Saving before quit…"))
		}
	}
	if !force && a.saver.busy() {
		a.quitting = true
		return a.toastInfo("Saving before quit…")
	}
	if a.watch != nil {
		_ = a.watch.Close()
		a.watch = nil
	}
	return tea.Quit
}

// ---- toasts -----------------------------------------------------------------

func (a *app) toastKind(kind ui.ToastKind, text string) tea.Cmd {
	a.toastSeq++
	id := a.toastSeq
	sticky := kind == ui.ToastError
	a.toast = &toastState{id: id, kind: kind, text: text, sticky: sticky}
	if sticky {
		return nil
	}
	return tea.Tick(toastTTL, func(time.Time) tea.Msg { return toastExpireMsg(id) })
}

func (a *app) toastInfo(text string) tea.Cmd { return a.toastKind(ui.ToastInfo, text) }
func (a *app) toastOK(text string) tea.Cmd   { return a.toastKind(ui.ToastSuccess, text) }
func (a *app) toastWarn(text string) tea.Cmd { return a.toastKind(ui.ToastWarning, text) }
func (a *app) toastErr(text string) tea.Cmd  { return a.toastKind(ui.ToastError, text) }

// ---- input ------------------------------------------------------------------

// onPaste sends pasted text to the open overlay or the focused pane.
func (a *app) onPaste(msg tea.PasteMsg) tea.Cmd {
	if a.ov != nil {
		return a.updateOverlay(msg)
	}
	if a.focus == SlotSidebar {
		if a.tree.filtering {
			var cmd tea.Cmd
			a.tree.filter, cmd = a.tree.filter.Update(msg)
			return cmd
		}
		return nil
	}
	return a.updateFocused(msg)
}

func (a *app) updateFocused(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	switch a.focus {
	case SlotRequest:
		a.request, cmd = a.request.Update(msg)
	case SlotResponse:
		a.response, cmd = a.response.Update(msg)
	}
	return cmd
}

// editing reports whether a text field owns the keyboard, in which case plain
// keys must reach it instead of triggering shortcuts.
func (a *app) editing() bool {
	switch a.focus {
	case SlotSidebar:
		return a.tree.filtering
	case SlotRequest:
		e, ok := a.request.(Editor)
		return ok && e.Editing()
	default:
		e, ok := a.response.(Editor)
		return ok && e.Editing()
	}
}

// insertActive reports whether a text field has the keyboard in a way that
// makes Ctrl-C act as Esc and keys like Backspace belong to the field: a pane
// or tree-filter field being edited, or an overlay built around a text input
// (palette, request switcher, prompts).
func (a *app) insertActive() bool {
	switch a.ov.(type) {
	case *pickerOverlay, *promptOverlay, *switcherOverlay:
		return true
	case nil:
		return a.editing() || (a.tree != nil && a.tree.filtering)
	}
	return false
}

// ctrlC is the Ctrl-C ladder: it acts as Esc while typing; elsewhere the first
// press asks for a second one within a second.
func (a *app) ctrlC() tea.Cmd {
	switch {
	case a.phase != phaseReady, a.quitting:
		return a.quit(a.quitting) // nothing to lose, or a second press while saving
	case a.pend != nil:
		a.pend = nil
		return nil
	case a.insertActive():
		return a.onKey(keyNamed(tea.KeyEscape))
	}
	now := a.now()
	if !a.lastCtrlC.IsZero() && now.Sub(a.lastCtrlC) <= time.Second {
		a.lastCtrlC = time.Time{}
		return a.quit(false)
	}
	a.lastCtrlC = now
	return a.toastInfo("Press Ctrl-C again to quit.")
}

func (a *app) onKey(msg tea.KeyPressMsg) tea.Cmd {
	s := msg.String()
	if s == "ctrl+c" {
		return a.ctrlC()
	}
	a.lastCtrlC = time.Time{} // any other key starts the ladder over
	if a.toast != nil && a.toast.sticky {
		a.toast = nil // any key acknowledges an error
	}

	switch a.phase {
	case phaseLoading:
		if key.Matches(msg, a.keys.Quit) {
			return a.quit(true)
		}
		return nil
	case phaseFatal:
		switch {
		case key.Matches(msg, a.keys.Quit):
			return a.quit(true)
		case s == "r", s == "enter":
			a.phase = phaseLoading
			return a.loadCmd(nil)
		}
		return nil
	}

	if a.ov != nil {
		a.pend = nil
		if cmd, ok := a.overlayAlways(msg); ok {
			return cmd
		}
		return a.updateOverlay(msg)
	}

	// A prefix key (Space, g, [, ]) is waiting for its follow-up.
	if p := a.pend; p != nil {
		a.pend = nil
		if cmd, ok := a.followUp(p, msg); ok {
			return cmd
		}
	}

	// Shortcuts that work even while typing.
	if cmd, ok := a.alwaysKey(msg); ok {
		return cmd
	}
	if a.editing() {
		if a.focus == SlotSidebar {
			return a.sidebarKey(msg)
		}
		return a.updateFocused(msg)
	}

	// Esc is the bottom of the ladder: stop a running send or sign-in no matter
	// which pane has focus (the request pane cancels its own).
	if s == "esc" && a.focus != SlotRequest {
		return tea.Batch(a.cancelOAuth(), a.routeKey(msg))
	}
	return a.routeKey(msg)
}

// cancelOAuth tells the request pane to stop a running OAuth sign-in.
func (a *app) cancelOAuth() tea.Cmd {
	var cmd tea.Cmd
	a.request, cmd = a.request.Update(CancelOAuthMsg{})
	return cmd
}

// routeKey sends a key that no overlay, prefix or text field claimed to the
// shell's own keys and then to the sidebar or the focused pane.
func (a *app) routeKey(msg tea.KeyPressMsg) tea.Cmd {
	if cmd, ok := a.commonKey(msg); ok {
		return cmd
	}
	if a.isFirstRun() {
		return a.firstRunKey(msg)
	}
	if cmd, ok := a.globalKey(msg); ok {
		return cmd
	}
	if a.focus == SlotSidebar {
		return a.sidebarKey(msg)
	}
	return a.paneKey(msg)
}

// alwaysKey handles the shortcuts that work in every pane, even while a text
// field has the keyboard: Ctrl-P, Ctrl-B, Alt-1/2/3, Ctrl-S and Ctrl-Enter.
func (a *app) alwaysKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	s := msg.String()
	switch {
	case key.Matches(msg, a.keys.Palette):
		return a.openPalette(), true
	case key.Matches(msg, a.keys.Sidebar):
		return a.toggleSidebar(), true
	case s == "alt+1":
		return a.setFocus(SlotSidebar), true
	case s == "alt+2":
		return a.setFocus(SlotRequest), true
	case s == "alt+3":
		return a.setFocus(SlotResponse), true
	case s == "ctrl+s" || s == "ctrl+enter":
		return a.sendKey(msg), true
	}
	return nil, false
}

// keyOwner is implemented by overlays that use one of the always-keys for
// their own purpose (the request switcher moves its cursor with Ctrl-P).
type keyOwner interface {
	ownsKey(s string) bool
}

// overlayAlways routes Ctrl-P, Ctrl-S, Ctrl-Enter and Alt-1/2/3 while an
// overlay is open. Non-modal overlays (palette, switcher, help, environment
// switcher) close and the key then acts as it would on the main screen.
// Confirmation dialogs and text prompts must be answered first and ignore
// these keys; hosted screens keep their own key handling (only Ctrl-P closes
// them). Esc and Ctrl-C are not routed here.
func (a *app) overlayAlways(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	s := msg.String()
	switch s {
	case "ctrl+p", "ctrl+s", "ctrl+enter", "alt+1", "alt+2", "alt+3":
	default:
		return nil, false
	}
	switch o := a.ov.(type) {
	case *confirmOverlay, *promptOverlay:
		return nil, true
	case *screenOverlay:
		if s == "ctrl+p" {
			a.ov = nil
			return a.afterScreen(), true // the same path as Esc: panes learn the environment again
		}
		return nil, false
	case *pickerOverlay:
		if s == "ctrl+p" {
			a.ov = nil // Ctrl-P again closes the palette
			return nil, true
		}
	case keyOwner:
		if o.ownsKey(s) {
			return nil, false
		}
	}
	a.ov = nil
	return a.alwaysKey(msg)
}

// sendKey is Ctrl-S or Ctrl-Enter: the focused pane sends; from the tree (or
// when the response pane has nothing to retry) the shell runs the send action.
func (a *app) sendKey(msg tea.KeyPressMsg) tea.Cmd {
	switch a.focus {
	case SlotRequest:
		return a.updateFocused(msg)
	case SlotResponse:
		if cmd := a.updateFocused(msg); cmd != nil {
			return cmd
		}
	}
	return a.runAction("request.send", "ws.send", "ws.connect")
}

// updateOverlay routes input to the open overlay. An overlay may open another
// one from its handler (the palette opening "New"), which must not be
// overwritten by the first one closing.
func (a *app) updateOverlay(msg tea.Msg) tea.Cmd {
	prev := a.ov
	next, cmd := prev.update(a, msg)
	if a.ov == prev {
		a.ov = next
	}
	return cmd
}

// commonKey handles the keys that mean the same on the first-run guide and on
// the main screen while nothing is being typed: Space, ?, : and q.
func (a *app) commonKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	switch s := msg.String(); {
	case s == "space" || s == " ":
		return a.startPending(prefixLeader), true
	case key.Matches(msg, a.keys.Help):
		a.ov = &helpOverlay{}
		return nil, true
	case key.Matches(msg, a.keys.Ex):
		return a.openPaletteEx(), true
	case key.Matches(msg, a.keys.Quit):
		return a.quit(false), true
	}
	return nil, false
}

// globalKey handles shortcuts that apply on every pane while nothing is being
// typed.
func (a *app) globalKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	s := msg.String()
	switch {
	case key.Matches(msg, a.keys.Next):
		return a.cycleFocus(1), true
	case key.Matches(msg, a.keys.Prev):
		return a.cycleFocus(-1), true
	case key.Matches(msg, a.keys.Filter) && a.focus == SlotSidebar:
		// "/" belongs to the focused pane elsewhere: search in the response,
		// the log in a WebSocket request, nothing in the request editor.
		return a.tree.startFilter(), true
	case s == "1":
		return a.setFocus(SlotSidebar), true
	case s == "2":
		return a.setFocus(SlotRequest), true
	case s == "3":
		return a.setFocus(SlotResponse), true
	case s == "g":
		return a.startPending(prefixGoto), true
	case s == "[" || s == "]":
		if a.focus == SlotSidebar {
			return nil, false
		}
		delta := -1
		if s == "]" {
			delta = 1
		}
		return tea.Batch(a.stepSection(delta), a.startPending(s)), true
	case key.Matches(msg, a.keys.JumpBack):
		return a.jump(-1), true
	case a.enhanced && key.Matches(msg, a.keys.JumpForward):
		return a.jump(1), true
	case key.Matches(msg, a.keys.PaneLeft), s == "backspace" && !a.insertActive():
		return a.movePane(DirLeft), true
	case key.Matches(msg, a.keys.PaneDown):
		return a.movePane(DirDown), true
	case key.Matches(msg, a.keys.PaneUp):
		return a.movePane(DirUp), true
	case key.Matches(msg, a.keys.PaneRight):
		return a.movePane(DirRight), true
	case a.focus == SlotSidebar && key.Matches(msg, a.keys.Undo):
		return a.runAction("request.undo"), true
	case a.focus == SlotSidebar && key.Matches(msg, a.keys.Redo):
		return a.runAction("request.redo"), true
	}
	return nil, false
}

// paneKey sends a key to the focused Request or Response pane, translating the
// vim motions the existing widgets do not know yet into the keys they do:
// j k h l to the arrows, G to End, Ctrl-d and Ctrl-u to the page keys. (gg
// arrives as a Home key from the g prefix.)
func (a *app) paneKey(msg tea.KeyPressMsg) tea.Cmd {
	if a.focus == SlotRequest {
		if t, ok := vimMotion(msg.String()); ok {
			return a.updateFocused(keyNamed(t))
		}
	}
	return a.updateFocused(msg)
}

// vimMotion maps a vim motion key to the standard key with the same effect.
func vimMotion(s string) (rune, bool) {
	switch s {
	case "j":
		return tea.KeyDown, true
	case "k":
		return tea.KeyUp, true
	case "h":
		return tea.KeyLeft, true
	case "l":
		return tea.KeyRight, true
	case "G":
		return tea.KeyEnd, true
	case "ctrl+d":
		return tea.KeyPgDown, true
	case "ctrl+u":
		return tea.KeyPgUp, true
	}
	return 0, false
}

// ---- focus ------------------------------------------------------------------

// focusOrder lists the slots Tab cycles through in the current layout.
func (a *app) focusOrder() []Slot {
	if a.responseHidden() {
		return []Slot{SlotSidebar, SlotRequest}
	}
	return []Slot{SlotSidebar, SlotRequest, SlotResponse}
}

func (a *app) cycleFocus(dir int) tea.Cmd {
	order := a.focusOrder()
	idx := 0
	for i, s := range order {
		if s == a.focus {
			idx = i
		}
	}
	return a.setFocus(order[(idx+dir+len(order))%len(order)])
}

// toggleSidebar shows or hides the collections. In the wide layouts the
// sidebar is always visible, so it simply takes focus or gives it back.
func (a *app) toggleSidebar() tea.Cmd {
	if a.focus == SlotSidebar {
		back := a.prevFocus
		if back == SlotSidebar {
			back = SlotRequest
		}
		return a.setFocus(back)
	}
	return a.setFocus(SlotSidebar)
}

// setFocus moves keyboard focus to s.
func (a *app) setFocus(s Slot) tea.Cmd {
	if s == SlotResponse && a.responseHidden() {
		return nil
	}
	if s == a.focus {
		return nil
	}
	var blur tea.Cmd
	switch a.focus {
	case SlotRequest:
		a.request.Blur()
		// A pane that saves on blur returns the command that does it.
		if b, ok := a.request.(interface{ BlurCmd() tea.Cmd }); ok {
			blur = b.BlurCmd()
		}
	case SlotResponse:
		a.response.Blur()
	}
	if a.focus != SlotSidebar {
		a.prevFocus = a.focus
	}
	a.focus = s
	var cmd tea.Cmd
	switch s {
	case SlotRequest:
		a.rightShow = SlotRequest
		cmd = a.request.Focus()
	case SlotResponse:
		a.rightShow = SlotResponse
		cmd = a.response.Focus()
	}
	return tea.Batch(blur, cmd)
}

// layoutKind is the layout in effect: accessible mode is always linear.
func (a *app) layoutKind() ui.Layout {
	if a.accessible {
		return ui.LayoutSingle
	}
	return ui.LayoutFor(a.width)
}

// responseHidden reports whether a WebSocket request owns the whole content
// area beside the tree. Single-pane mode keeps its existing slot switching.
func (a *app) responseHidden() bool {
	ref := a.currentRef()
	return a.layoutKind() != ui.LayoutSingle &&
		ref.Request != nil && ref.Request.Kind == model.KindWebSocket
}

// ---- selection --------------------------------------------------------------

type selectionSig struct {
	id  model.ID
	req *model.Request
}

// currentRef describes the selected tree row for the panes.
func (a *app) currentRef() RequestRef {
	r, ok := a.tree.current()
	if !ok {
		return RequestRef{}
	}
	switch r.kind {
	case rowRequest:
		if r.node != nil {
			return RequestRef{Collection: r.node.coll, Request: r.node.item.(*model.Request), Path: r.node.path}
		}
	case rowFolder, rowCollection:
		if r.node != nil {
			return RequestRef{Collection: r.node.coll, Path: r.node.path}
		}
	case rowProblem:
		return RequestRef{Problem: r.problem}
	}
	return RequestRef{}
}

// syncSelection tells the panes when the selection changed (or always, for an
// explicit open) and moves focus on open.
func (a *app) syncSelection(open bool) tea.Cmd {
	ref := a.currentRef()
	sig := selectionSig{id: a.tree.sel, req: ref.Request}
	if !open && sig == a.lastSel {
		return nil
	}
	a.lastSel = sig
	if open {
		a.recordJump(ref)
	}
	if open && ref.Request != nil {
		a.recordRecent(ref.Request.ID)
	}
	a.layoutPanes()
	cmds := []tea.Cmd{a.broadcast(SelectionMsg{Ref: ref, Open: open})}
	if open || (a.responseHidden() && a.focus == SlotResponse) {
		cmds = append(cmds, a.setFocus(SlotRequest))
	}
	return tea.Batch(cmds...)
}

func (a *app) selectRequest(id model.ID) tea.Cmd {
	if a.tree.reveal(id) {
		return tea.Batch(a.syncSelection(true), a.setFocus(SlotRequest))
	}
	return a.toastWarn("That request is not in the workspace.")
}

func (a *app) sendSavedRequest(id model.ID) tea.Cmd {
	if a.ws == nil {
		return nil
	}
	coll, req := findRequest(a.ws, id)
	if req == nil {
		return a.toastWarn("That request is not in the workspace.")
	}
	return a.startSend(SendRequest{
		Collection:  coll,
		Request:     req,
		Environment: a.activeEnvironment(),
	})
}

func findRequest(ws *model.Workspace, id model.ID) (*model.Collection, *model.Request) {
	for _, c := range ws.Collections {
		if r := findRequestInItems(c.Items, id); r != nil {
			return c, r
		}
	}
	return nil, nil
}

func findRequestInItems(items []model.Item, id model.ID) *model.Request {
	for _, it := range items {
		switch v := it.(type) {
		case *model.Request:
			if v.ID == id {
				return v
			}
		case *model.Folder:
			if r := findRequestInItems(v.Items, id); r != nil {
				return r
			}
		}
	}
	return nil
}

// isFirstRun reports whether the guide replaces the main screen: there is
// nothing to browse and nothing broken to show.
func (a *app) isFirstRun() bool {
	if a.phase != phaseReady {
		return false
	}
	return a.ws == nil || (len(a.ws.Collections) == 0 && len(a.problems) == 0)
}

// workspaceName is the name shown in the header.
func (a *app) workspaceName() string {
	if a.ws != nil && a.ws.Name != "" {
		return a.ws.Name
	}
	root := a.be.Root()
	if i := strings.LastIndexAny(root, `/\`); i >= 0 && i < len(root)-1 {
		return root[i+1:]
	}
	return root
}

// displayRoot is the workspace path with the home directory abbreviated.
func (a *app) displayRoot() string {
	root := a.be.Root()
	if home, err := os.UserHomeDir(); err == nil && home != "" && strings.HasPrefix(root, home) {
		return "~" + strings.TrimPrefix(root, home)
	}
	return root
}
