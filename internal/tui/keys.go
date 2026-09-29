package tui

import (
	"charm.land/bubbles/v2/key"
	"github.com/shubham/postctl/internal/ui"
)

// keyMap is the single source of truth for shortcuts: input handling matches
// against it, the footer hints and the help screen are generated from it. No
// vim bindings (plan §0, Q11).
type keyMap struct {
	// Global.
	Help, Palette, Env, EnvEdit, Quit, ForceQuit, Next, Prev, Sidebar, Filter, Reload key.Binding
	Pane1, Pane2, Pane3                                                               key.Binding
	Back                                                                              key.Binding

	// Tree.
	Up, Down, PageUp, PageDown, Home, End, Fold, Unfold, Open, Toggle key.Binding
	New, Rename, Duplicate, Move, Delete, MoveUp, MoveDown            key.Binding

	// Reserved for the request and response panes (WS6); listed in help so
	// the shell's keys never collide with them.
	Send, Edit key.Binding
}

func newKeyMap(ascii bool) keyMap {
	up, down, left, right := "↑", "↓", "←", "→"
	if ascii {
		up, down, left, right = "Up", "Down", "Left", "Right"
	}
	b := func(help, desc string, keys ...string) key.Binding {
		return key.NewBinding(key.WithKeys(keys...), key.WithHelp(help, desc))
	}
	return keyMap{
		Help:      b("?", "Help", "?"),
		Palette:   b("Ctrl-P", "Command palette", "ctrl+p"),
		Env:       b("E", "Environments", "E"),
		EnvEdit:   b("Ctrl-E", "Edit environments", "ctrl+e"),
		Quit:      b("q", "Quit", "q"),
		ForceQuit: b("Ctrl-C", "Quit", "ctrl+c"),
		Next:      b("Tab", "Next pane", "tab"),
		Prev:      b("Shift-Tab", "Previous pane", "shift+tab"),
		Sidebar:   b("Ctrl-B", "Show or hide collections", "ctrl+b"),
		Filter:    b("/", "Filter requests", "/"),
		Reload:    b("Ctrl-R", "Reload from disk", "ctrl+r"),
		Pane1:     b("1", "Collections", "1", "alt+1"),
		Pane2:     b("2", "Request", "2", "alt+2"),
		Pane3:     b("3", "Response", "3", "alt+3"),
		Back:      b("Esc", "Back or cancel", "esc"),

		Up:        b(up, "Move up", "up"),
		Down:      b(down, "Move down", "down"),
		PageUp:    b("PgUp", "Page up", "pgup"),
		PageDown:  b("PgDn", "Page down", "pgdown"),
		Home:      b("Home", "First row", "home"),
		End:       b("End", "Last row", "end"),
		Fold:      b(left, "Fold folder or go to parent", "left"),
		Unfold:    b(right, "Unfold folder", "right"),
		Open:      b("Enter", "Open request or toggle folder", "enter"),
		Toggle:    b("Space", "Fold or unfold", "space"),
		New:       b("n", "New request, folder or collection", "n"),
		Rename:    b("r", "Rename", "r"),
		Duplicate: b("D", "Duplicate", "D"),
		Move:      b("m", "Move to another folder", "m"),
		Delete:    b("d", "Delete", "d"),
		MoveUp:    b("Alt-"+up, "Move up in its folder", "alt+up"),
		MoveDown:  b("Alt-"+down, "Move down in its folder", "alt+down"),

		Send: b("Ctrl-Enter", "Send request (also Ctrl-S)", "ctrl+enter", "ctrl+s"),
		Edit: b("e", "Edit in $EDITOR", "e"),
	}
}

// helpGroup is a titled list of key descriptions for the help screen.
type helpGroup struct {
	Title string
	Rows  []helpRow
}

type helpRow struct{ Key, Desc string }

func rows(bs ...key.Binding) []helpRow {
	out := make([]helpRow, 0, len(bs))
	for _, b := range bs {
		h := b.Help()
		out = append(out, helpRow{Key: h.Key, Desc: h.Desc})
	}
	return out
}

// helpGroups builds the help screen from the key map, grouped by context.
// focusHints are the focused pane's own hints, appended as their own group.
func (k keyMap) helpGroups(paneTitle string, focusHints []ui.Hint) []helpGroup {
	groups := []helpGroup{
		{"Everywhere", rows(k.Palette, k.Env, k.EnvEdit, k.Filter, k.Next, k.Prev, k.Pane1, k.Sidebar, k.Reload, k.Help, k.Quit)},
		{"Collections", rows(k.Up, k.Down, k.Unfold, k.Fold, k.Open, k.PageUp, k.Home, k.New, k.Rename, k.Duplicate, k.Move, k.Delete, k.MoveUp)},
		{"Request and response", rows(k.Send, k.Edit)},
		{"Lists and dialogs", []helpRow{
			{"Enter", "Choose"}, {"Esc", "Close or cancel"}, {k.Up.Help().Key + " " + k.Down.Help().Key, "Move"},
		}},
	}
	groups[1].Rows = replaceRow(groups[1].Rows, k.MoveUp.Help().Key, helpRow{k.MoveUp.Help().Key + " " + k.MoveDown.Help().Key, "Move up or down in its folder"})
	// The overview rows for the pane numbers read better as one line.
	groups[0].Rows = replaceRow(groups[0].Rows, k.Pane1.Help().Key, helpRow{"1 2 3", "Jump to Collections, Request, Response"})
	groups[1].Rows = replaceRow(groups[1].Rows, k.PageUp.Help().Key, helpRow{"PgUp PgDn", "Page up or down"})
	if len(focusHints) > 0 {
		var extra []helpRow
		seen := map[string]bool{}
		for _, r := range groups[0].Rows {
			seen[r.Key] = true
		}
		for _, h := range focusHints {
			if !seen[h.Key] {
				extra = append(extra, helpRow{h.Key, h.Label})
			}
		}
		if len(extra) > 0 {
			groups = append(groups, helpGroup{Title: paneTitle + " (focused)", Rows: extra})
		}
	}
	return groups
}

func replaceRow(rs []helpRow, key string, with helpRow) []helpRow {
	for i, r := range rs {
		if r.Key == key {
			rs[i] = with
		}
	}
	return rs
}
