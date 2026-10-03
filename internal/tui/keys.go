package tui

import (
	"charm.land/bubbles/v2/key"
	"github.com/shubham/postctl/internal/ui"
)

// keyMap is the single source of truth for the shell's own shortcuts: input
// handling matches against it, the footer hints and the help screen are
// generated from it. The map follows the vim key map in
// docs/design-language.md (plan §0 U1). The leader (Space) and g-prefix tables
// live in leader.go.
type keyMap struct {
	// Global.
	Help, Palette, Ex, Leader, Quit, ForceQuit, Next, Prev, Sidebar, Filter key.Binding
	Pane1, Pane2, Pane3                                                     key.Binding
	Back                                                                    key.Binding
	Goto, SectionPrev, SectionNext                                          key.Binding
	JumpBack, JumpForward                                                   key.Binding
	PaneLeft, PaneDown, PaneUp, PaneRight                                   key.Binding
	Undo, Redo                                                              key.Binding
	Send                                                                    key.Binding

	// Tree.
	Up, Down, PageUp, PageDown, Home, End, Fold, Unfold, Open key.Binding
	ReorderUp, ReorderDown                                    key.Binding
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
		Ex:        b(":", "Command line (q, q!, w, wq)", ":"),
		Leader:    b("Space", "Actions menu (which-key)", "space"),
		Quit:      b("q", "Quit", "q"),
		ForceQuit: b("Ctrl-C", "Quit (twice), or Esc while typing", "ctrl+c"),
		Next:      b("Tab", "Next pane", "tab"),
		Prev:      b("Shift-Tab", "Previous pane", "shift+tab"),
		Sidebar:   b("Ctrl-B", "Show or hide collections", "ctrl+b"),
		Filter:    b("/", "Filter requests", "/"),
		Pane1:     b("1", "Collections", "1", "alt+1"),
		Pane2:     b("2", "Request", "2", "alt+2"),
		Pane3:     b("3", "Response", "3", "alt+3"),
		Back:      b("Esc", "Back or cancel", "esc"),

		Goto:        b("g", "Go to a section (gu gp ga gh gb gs gt gS gm gr)", "g"),
		SectionPrev: b("[", "Previous section tab", "["),
		SectionNext: b("]", "Next section tab", "]"),
		JumpBack:    b("Ctrl-o", "Back to the previous request", "ctrl+o"),
		JumpForward: b("Ctrl-i", "Forward to the next request", "ctrl+i"),
		PaneLeft:    b("Ctrl-h", "Move to the pane on the left", "ctrl+h"),
		PaneDown:    b("Ctrl-j", "Move to the pane below", "ctrl+j"),
		PaneUp:      b("Ctrl-k", "Move to the pane above", "ctrl+k"),
		PaneRight:   b("Ctrl-l", "Move to the pane on the right", "ctrl+l"),
		Undo:        b("u", "Undo request edit", "u"),
		Redo:        b("Ctrl-r", "Redo request edit", "ctrl+r"),
		Send:        b("Ctrl-Enter", "Send request (also Ctrl-S)", "ctrl+enter", "ctrl+s"),

		Up:          b("k "+up, "Move up", "up", "k"),
		Down:        b("j "+down, "Move down", "down", "j"),
		PageUp:      b("Ctrl-u", "Half page up", "pgup", "ctrl+u"),
		PageDown:    b("Ctrl-d", "Half page down", "pgdown", "ctrl+d"),
		Home:        b("gg", "First row", "home"),
		End:         b("G", "Last row", "end", "G"),
		Fold:        b("h "+left, "Fold folder or go to parent", "left", "h"),
		Unfold:      b("l "+right, "Unfold folder", "right", "l"),
		Open:        b("Enter", "Open request or toggle folder", "enter"),
		ReorderUp:   b("Alt-"+up, "Move up in its folder", "alt+up"),
		ReorderDown: b("Alt-"+down, "Move down in its folder", "alt+down"),
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
// leader lists the leader rows that are currently available. Rows for keys a
// legacy terminal cannot send (Ctrl-Enter, Ctrl-i) appear only when enhanced
// keyboard reporting is known to be on.
func (k keyMap) helpGroups(paneTitle string, focusHints []ui.Hint, leader []helpRow, responseHidden, enhanced bool) []helpGroup {
	groups := []helpGroup{
		{"Everywhere", rows(k.Palette, k.Ex, k.Leader, k.Filter, k.Next, k.Prev, k.Pane1, k.PaneLeft, k.JumpBack, k.Sidebar, k.Goto, k.SectionPrev, k.Help, k.Quit, k.ForceQuit)},
		{"Collections", rows(k.Up, k.Down, k.Unfold, k.Fold, k.Open, k.PageUp, k.Home, k.End, k.ReorderUp)},
		{"Request and response", rows(k.Send, k.Undo, k.Redo)},
		{"Lists and dialogs", []helpRow{
			{"Enter", "Choose"}, {"Esc", "Close or cancel"}, {k.Up.Help().Key + " " + k.Down.Help().Key, "Move"},
		}},
	}
	if len(leader) > 0 {
		groups = append(groups[:2], append([]helpGroup{{"After Space", leader}}, groups[2:]...)...)
	}
	idx := func(title string) int {
		for i, g := range groups {
			if g.Title == title {
				return i
			}
		}
		return 0
	}
	groups[idx("Collections")].Rows = replaceRow(groups[idx("Collections")].Rows, k.ReorderUp.Help().Key, helpRow{k.ReorderUp.Help().Key + " " + k.ReorderDown.Help().Key, "Move up or down in its folder"})
	// The overview rows for the pane numbers and moves read better as one line.
	ev := idx("Everywhere")
	if responseHidden {
		groups[ev].Rows = replaceRow(groups[ev].Rows, k.Pane1.Help().Key, helpRow{"1 2", "Jump to Collections or WebSocket"})
		groups[idx("Lists and dialogs")].Title = "WebSocket"
	} else {
		groups[ev].Rows = replaceRow(groups[ev].Rows, k.Pane1.Help().Key, helpRow{"1 2 3", "Jump to Collections, Request, Response"})
	}
	groups[ev].Rows = replaceRow(groups[ev].Rows, k.PaneLeft.Help().Key, helpRow{"Ctrl-h j k l", "Move to the neighbouring pane"})
	if enhanced {
		groups[ev].Rows = replaceRow(groups[ev].Rows, k.JumpBack.Help().Key, helpRow{"Ctrl-o Ctrl-i", "Back and forward through opened requests"})
	} else {
		groups[ev].Rows = replaceRow(groups[ev].Rows, k.JumpBack.Help().Key, helpRow{"Ctrl-o", "Back through opened requests (Space ] forward)"})
	}
	groups[ev].Rows = replaceRow(groups[ev].Rows, k.SectionPrev.Help().Key, helpRow{"[ ]", "Previous and next section tab"})
	co := idx("Collections")
	groups[co].Rows = replaceRow(groups[co].Rows, k.PageUp.Help().Key, helpRow{"Ctrl-u Ctrl-d", "Half page up or down"})
	groups[co].Rows = replaceRow(groups[co].Rows, k.Home.Help().Key, helpRow{"gg G", "First and last row"})
	groups[co].Rows = dropRow(groups[co].Rows, k.End.Help().Key)
	rr := idx("Request and response")
	if !enhanced {
		groups[rr].Rows = replaceRow(groups[rr].Rows, k.Send.Help().Key, helpRow{"Ctrl-S", "Send request"})
	}
	groups[rr].Rows = replaceRow(groups[rr].Rows, k.Undo.Help().Key, helpRow{"u Ctrl-r", "Undo and redo request edits"})
	groups[rr].Rows = dropRow(groups[rr].Rows, k.Redo.Help().Key)
	if len(focusHints) > 0 {
		var extra []helpRow
		seen := map[string]bool{}
		for _, r := range groups[ev].Rows {
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

func dropRow(rs []helpRow, key string) []helpRow {
	out := rs[:0:0]
	for _, r := range rs {
		if r.Key != key {
			out = append(out, r)
		}
	}
	return out
}
