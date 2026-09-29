package tui

import (
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/shubham/postctl/internal/model"
	"github.com/shubham/postctl/internal/ui"
)

// envOverlay is the environment switcher (E): "No environment" plus every
// environment, the current one marked, production ones badged, and the
// highlighted environment's variables below with secrets masked.
type envOverlay struct {
	cursor int // 0 is "No environment"
}

func newEnvOverlay(a *app) *envOverlay {
	o := &envOverlay{}
	for i, e := range a.envs() {
		if e.ID == a.activeEnv {
			o.cursor = i + 1
		}
	}
	return o
}

func (o *envOverlay) update(a *app, msg tea.Msg) (overlay, tea.Cmd) {
	k, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return o, nil
	}
	n := len(a.envs()) + 1
	switch k.String() {
	case "esc", "E":
		return nil, nil
	case "up":
		o.cursor = max(0, o.cursor-1)
	case "down":
		o.cursor = min(n-1, o.cursor+1)
	case "home":
		o.cursor = 0
	case "end":
		o.cursor = n - 1
	case "enter":
		var id model.ID
		if o.cursor > 0 {
			id = a.envs()[o.cursor-1].ID
		}
		return nil, a.setEnvironment(id, true)
	}
	return o, nil
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return strconv.Itoa(n) + " " + many
}

func (o *envOverlay) view(a *app, w, h int) string {
	t := a.theme
	envs := a.envs()
	head := t.Bold().Render("Environments")
	esc := t.Render(ui.TextSecondary, "Esc Close")
	lines := []string{head + strings.Repeat(" ", max(1, w-ui.Width(head)-ui.Width(esc))) + esc, t.Rule(w)}

	nameW := len("No environment")
	for _, e := range envs {
		nameW = max(nameW, ui.Width(e.Name)+2)
	}
	nameW = min(nameW, max(10, w/3))

	row := func(i int, glyph, name string, tok ui.Token, meta, badge string, active bool) string {
		sel := i == o.cursor
		on := func(tk ui.Token) lipgloss.Style { return t.StyleOn(tk, sel) }
		var s seg
		if sel {
			s.add(on(ui.AccentPrimary), t.Glyphs.Select+" ")
		} else {
			s.add(on(ui.TextPrimary), "  ")
		}
		s.add(on(tok), glyph+" ")
		nameStyle := on(ui.TextPrimary)
		if sel {
			nameStyle = nameStyle.Bold(true)
		}
		s.add(nameStyle, t.Fit(name, nameW))
		s.add(on(ui.TextSecondary), "  "+meta)
		if badge != "" {
			s.add(on(ui.EnvProduction), "  "+badge)
		}
		right := ""
		if active {
			right = "active"
		}
		if right != "" {
			s.add(on(ui.StatusSuccess), strings.Repeat(" ", max(2, w-s.w-len(right)))+right)
		}
		if pad := w - s.w; pad > 0 {
			s.add(on(ui.TextPrimary), strings.Repeat(" ", pad))
		}
		return s.sb.String()
	}

	lines = append(lines, row(0, t.Glyphs.Expired, "No environment", ui.TextSecondary, "", "", a.activeEnv == ""))
	for i, e := range envs {
		glyph, tok, badge := t.Glyphs.OK, ui.TextSecondary, ""
		if e.Production {
			glyph, tok, badge = t.Glyphs.Warn, ui.EnvProduction, "production"
		}
		lines = append(lines, row(i+1, glyph, e.Name, tok, plural(len(e.Variables), "variable", "variables"), badge, e.ID == a.activeEnv))
	}

	// Variables of the highlighted environment, masked.
	lines = append(lines, "", t.Render(ui.TextSecondary, "Variables"))
	var vars []model.Variable
	name := "No environment"
	if o.cursor > 0 {
		e := envs[o.cursor-1]
		vars, name = e.Variables, e.Name
	}
	room := h - len(lines) - 1
	switch {
	case len(envs) == 0:
		lines = append(lines, t.Render(ui.TextSecondary, "No environments in this workspace yet. Import them with: postctl import postman-local"))
	case o.cursor == 0:
		lines = append(lines, t.Render(ui.TextSecondary, "Requests run without environment variables. {{name}} references stay unresolved."))
	case len(vars) == 0:
		lines = append(lines, t.Render(ui.TextSecondary, name+" has no variables."))
	default:
		shown := vars
		more := 0
		if room > 0 && len(shown) > room-1 {
			more = len(shown) - (room - 1)
			shown = shown[:max(0, room-1)]
		}
		kv := make([]ui.KVRow, len(shown))
		for i, v := range shown {
			kv[i] = ui.KVRow{Key: v.Key, Value: v.Value, Description: v.Description, Disabled: v.Disabled, Secret: v.IsSecret()}
		}
		lines = append(lines, t.KVTable(kv, ui.KVTableOptions{Width: w, Focus: -1, Header: true}))
		if more > 0 {
			lines = append(lines, t.Render(ui.TextQuiet, "  + "+plural(more, "more variable", "more variables")))
		}
	}
	return strings.Join(lines, "\n")
}

func (o *envOverlay) hints(a *app) []ui.Hint {
	arrows := "↑↓"
	if a.ascii {
		arrows = "Up/Down"
	}
	return []ui.Hint{hint("Enter", "Use", 0), hint(arrows, "Move", 1), hint("Esc", "Close", 0)}
}

// envs returns the workspace's environments.
func (a *app) envs() []*model.Environment {
	if a.ws == nil {
		return nil
	}
	return a.ws.Environments
}

// activeEnvironment returns the active environment, or nil.
func (a *app) activeEnvironment() *model.Environment {
	if a.ws == nil || a.activeEnv == "" {
		return nil
	}
	return a.ws.Environment(a.activeEnv)
}

// envInfo is the header's view of the active environment.
func (a *app) envInfo() ui.EnvInfo {
	if e := a.activeEnvironment(); e != nil {
		return ui.EnvInfo{Name: e.Name, Production: e.Production}
	}
	return ui.EnvInfo{}
}

// setEnvironment activates an environment (empty id for none), tells the
// panes and persists the choice in local state without blocking.
func (a *app) setEnvironment(id model.ID, announce bool) tea.Cmd {
	a.activeEnv = id
	a.state.ActiveEnvironment = id
	env := a.activeEnvironment()
	a.broadcast(EnvironmentChangedMsg{Env: env})

	cmds := []tea.Cmd{a.persistState()}
	if announce {
		if env == nil {
			cmds = append(cmds, a.toastInfo("No environment selected"))
		} else if env.Production {
			cmds = append(cmds, a.toastWarn("Environment: "+env.Name+" (production)"))
		} else {
			cmds = append(cmds, a.toastInfo("Environment: "+env.Name))
		}
	}
	return tea.Batch(cmds...)
}

// persistState writes local UI state from a copy, on a command.
func (a *app) persistState() tea.Cmd {
	be, st := a.be, a.state
	return func() tea.Msg {
		return stateSavedMsg{err: be.SaveState(st)}
	}
}

// stateSavedMsg reports the result of persisting local state.
type stateSavedMsg struct{ err error }
