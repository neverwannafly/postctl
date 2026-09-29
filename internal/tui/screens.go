package tui

import (
	tea "charm.land/bubbletea/v2"
	"github.com/shubham/postctl/internal/model"
	"github.com/shubham/postctl/internal/ui"
)

// screenOverlay hosts a [Screen] supplied by another package (environment
// editor, cookie manager) as a full-body overlay.
type screenOverlay struct {
	s Screen
}

func (o *screenOverlay) update(a *app, msg tea.Msg) (overlay, tea.Cmd) {
	next, cmd := o.s.Update(msg)
	if next == nil {
		return nil, tea.Batch(cmd, a.afterScreen())
	}
	o.s = next
	return o, cmd
}

func (o *screenOverlay) view(_ *app, w, h int) string {
	o.s.SetSize(w, h)
	return o.s.View()
}

func (o *screenOverlay) hints(*app) []ui.Hint { return o.s.KeyHints() }

// openScreen shows s over the main screen.
func (a *app) openScreen(s Screen) tea.Cmd {
	if s == nil {
		return nil
	}
	g := a.geo()
	s.SetSize(g.width, g.bodyH)
	a.ov = &screenOverlay{s: s}
	return nil
}

// afterScreen runs when a screen closes: environments may have been renamed,
// edited or deleted, so the panes learn the active environment again.
func (a *app) afterScreen() tea.Cmd {
	if a.ws == nil {
		return nil
	}
	cmds := []tea.Cmd{}
	if a.activeEnv != "" && a.ws.Environment(a.activeEnv) == nil {
		a.activeEnv = ""
		a.state.ActiveEnvironment = ""
		cmds = append(cmds, a.persistState(), a.toastInfo("The active environment was deleted. No environment selected."))
	}
	cmds = append(cmds, a.broadcast(EnvironmentChangedMsg{Env: a.activeEnvironment()}))
	return tea.Batch(cmds...)
}

func (a *app) screenEnv(focus model.ID) ScreenEnv {
	return ScreenEnv{
		PaneEnv:   PaneEnv{Theme: a.theme, Actions: a.registry, Backend: a.be},
		Workspace: a.ws,
		ActiveEnv: a.activeEnv,
		Focus:     focus,
		ASCII:     a.ascii,
	}
}

// openEnvEditor opens the environment editor on env focus (or the active
// environment when empty).
func (a *app) openEnvEditor(focus model.ID) tea.Cmd {
	if a.opts.NewEnvEditor == nil || a.ws == nil {
		return a.toastInfo("The environment editor is not available in this build.")
	}
	return a.openScreen(a.opts.NewEnvEditor(a.screenEnv(focus)))
}

func (a *app) openCookies() tea.Cmd {
	if a.opts.NewCookieManager == nil {
		return a.toastInfo("The cookie manager is not available in this build.")
	}
	return a.openScreen(a.opts.NewCookieManager(a.screenEnv("")))
}

// screenActions are the palette entries for the hosted screens.
func (a *app) screenActions() []Action {
	var out []Action
	if a.opts.NewEnvEditor != nil {
		out = append(out, Action{ID: "env.edit", Title: "Edit environments", Group: "Environment", Keys: "Ctrl-E",
			Enabled: func() bool { return a.ws != nil },
			Run:     func() tea.Cmd { return a.openEnvEditor("") }})
	}
	if a.opts.NewCookieManager != nil {
		out = append(out, Action{ID: "cookies.open", Title: "Cookies", Group: "Workspace",
			Run: func() tea.Cmd { return a.openCookies() }})
	}
	return out
}
