package tui

import (
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/shubham/postctl/internal/ui"
)

// firstRunState is the selection in the first-run guide.
type firstRunState struct{ cursor int }

// firstRunAction is one row of the guide. Only "create" acts in place; the
// import rows copy their command, because importing is a separate process.
type firstRunAction struct {
	label string
	key   string // shown at the right when there is no command
	cmd   string // shell command to copy
}

var firstRunActions = []firstRunAction{
	{label: "Create a request", key: "Enter"},
	{label: "Import from the Postman app", cmd: "postctl import postman-local"},
	{label: "Import a Postman export", cmd: "postctl import postman <file.json>"},
}

func (a *app) firstRunKey(msg tea.KeyPressMsg) tea.Cmd {
	k := a.keys
	switch {
	case key.Matches(msg, k.Up):
		a.first.cursor = max(0, a.first.cursor-1)
	case key.Matches(msg, k.Down):
		a.first.cursor = min(len(firstRunActions)-1, a.first.cursor+1)
	case key.Matches(msg, k.Open):
		act := firstRunActions[a.first.cursor]
		if act.cmd == "" {
			return a.openNew()
		}
		return tea.Batch(tea.SetClipboard(act.cmd),
			a.toastOK("Copied: "+act.cmd+". Run it in another terminal; this screen updates when the files appear."))
	case key.Matches(msg, k.New):
		return a.openNew()
	case key.Matches(msg, k.Help):
		a.ov = &helpOverlay{}
	case key.Matches(msg, k.Env):
		a.ov = newEnvOverlay(a)
	case key.Matches(msg, k.Reload):
		return a.reload()
	case key.Matches(msg, k.Quit):
		return a.quit(false)
	}
	return nil
}

func (a *app) firstRunHints() []ui.Hint {
	return []ui.Hint{
		hint("Enter", "Choose", 0), hint(arrowsUD(a.ascii), "Move", 2), hint("n", "New", 1),
		hint("Ctrl-P", "Palette", 3), hint("?", "Help", 0), hint("q", "Quit", 4),
	}
}

// firstRunView is the one-screen guide: what to do next, how data gets in and
// where it lives. Empty is not an error.
func (a *app) firstRunView(w, h int) string {
	t := a.theme
	var lines []string
	lines = append(lines, t.Bold().Render("Welcome to postctl."), "")
	if a.notInitialized {
		lines = append(lines, t.Render(ui.TextPrimary, ui.Wrap("There is no workspace in "+a.displayRoot()+" yet. Creating your first request sets one up there. You can also bring in your Postman data:", w)))
	} else {
		lines = append(lines, t.Render(ui.TextPrimary, ui.Wrap("No collections yet. Start with a request, or bring in your Postman data:", w)))
	}
	lines = append(lines, "")

	wide := w >= 76
	labelW := 0
	for _, act := range firstRunActions {
		labelW = max(labelW, ui.Width(act.label))
	}
	for i, act := range firstRunActions {
		sel := i == a.first.cursor
		marker, tok := "  ", ui.TextPrimary
		if sel {
			marker, tok = t.Glyphs.Select+" ", ui.AccentPrimary
		}
		right := act.key
		if act.cmd != "" {
			right = act.cmd
		}
		if wide {
			lines = append(lines, t.Render(tok, marker+t.Fit(act.label, labelW+3))+t.Render(ui.TextSecondary, right))
			continue
		}
		lines = append(lines, t.Render(tok, marker+act.label))
		lines = append(lines, "  "+t.Render(ui.TextSecondary, t.Truncate(right, w-2)))
	}

	lines = append(lines, "",
		t.Render(ui.TextSecondary, ui.Wrap("Everything is stored as plain files in "+a.displayRoot()+", so it works with git.", w)))
	return strings.Join(lines, "\n")
}
