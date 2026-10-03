package tui

import (
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/shubham/postctl/internal/ui"
)

// helpOverlay is the "?" screen, generated from the key map and grouped by
// context. It scrolls when the terminal is short.
type helpOverlay struct {
	scroll int
	lines  int // total rendered lines, for clamping
	page   int
}

func (o *helpOverlay) update(a *app, msg tea.Msg) (overlay, tea.Cmd) {
	o.measure(a)
	k, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return o, nil
	}
	switch k.String() {
	case "esc", "?", "q", "enter":
		return nil, nil
	case "up", "k":
		o.scroll--
	case "down", "j":
		o.scroll++
	case "pgup", "ctrl+u":
		o.scroll -= max(1, o.page-1)
	case "pgdown", "space", "ctrl+d":
		o.scroll += max(1, o.page-1)
	case "home", "g":
		o.scroll = 0
	case "end", "G":
		o.scroll = o.lines
	}
	o.scroll = max(0, min(o.scroll, max(0, o.lines-o.page)))
	return o, nil
}

// helpGroups builds the groups shown by the help overlay: the key map, the
// leader keys that work right now, and the focused pane's own hints.
func (a *app) helpGroups() []helpGroup {
	var leader []helpRow
	for _, e := range a.leaderEntries() {
		leader = append(leader, helpRow{Key: "Space " + e.Key, Desc: e.Label})
	}
	return a.keys.helpGroups(a.focusTitle(), a.focusHints(), leader, a.responseHidden(), a.enhanced)
}

func (o *helpOverlay) measure(a *app) {
	g := a.geo()
	groups := a.helpGroups()
	o.lines = 0
	if g.width >= 110 {
		left, right := 0, 0
		for i, group := range groups {
			n := len(group.Rows) + 2
			if i%2 == 0 {
				left += n
			} else {
				right += n
			}
		}
		o.lines = max(left, right)
	} else {
		for _, group := range groups {
			o.lines += len(group.Rows) + 2
		}
	}
	o.page = max(1, g.bodyH-3)
	o.scroll = max(0, min(o.scroll, max(0, o.lines-o.page)))
}

func (o *helpOverlay) view(a *app, w, h int) string {
	t := a.theme
	groups := a.helpGroups()

	keyW := 0
	for _, g := range groups {
		for _, r := range g.Rows {
			keyW = max(keyW, ui.Width(r.Key))
		}
	}
	keyW = min(keyW, 14)

	render := func(g helpGroup, colW int) []string {
		out := []string{t.Section(g.Title, "", false)}
		for _, r := range g.Rows {
			out = append(out, "  "+t.Bold().Render(t.Fit(r.Key, keyW))+"  "+t.Render(ui.TextSecondary, t.Truncate(r.Desc, max(1, colW-keyW-4))))
		}
		return out
	}

	var body []string
	if w >= 110 {
		// Two columns: groups alternate left and right.
		colW := (w - 4) / 2
		var left, right []string
		for i, g := range groups {
			if i%2 == 0 {
				left = append(append(left, render(g, colW)...), "")
			} else {
				right = append(append(right, render(g, colW)...), "")
			}
		}
		n := max(len(left), len(right))
		for i := 0; i < n; i++ {
			l, r := "", ""
			if i < len(left) {
				l = left[i]
			}
			if i < len(right) {
				r = right[i]
			}
			body = append(body, t.Fit(l, colW)+"    "+r)
		}
	} else {
		for _, g := range groups {
			body = append(body, render(g, w)...)
			body = append(body, "")
		}
	}

	head := t.Bold().Render("Help")
	esc := t.Render(ui.TextSecondary, "Esc Close")
	title := head + strings.Repeat(" ", max(1, w-ui.Width(head)-ui.Width(esc))) + esc
	page := max(1, h-3)
	lines := len(body)
	scroll := max(0, min(o.scroll, max(0, lines-page)))
	end := min(len(body), scroll+page)
	visible := strings.Join(body[scroll:end], "\n")
	if lines > page {
		more := "  " + strconv.Itoa(scroll+1) + "-" + strconv.Itoa(end) + " of " + strconv.Itoa(lines) + " lines, scroll for more"
		visible += "\n" + t.Render(ui.TextQuiet, more)
	}
	return title + "\n" + t.Rule(w) + "\n" + visible
}

func (o *helpOverlay) hints(a *app) []ui.Hint {
	arrows := "↑↓"
	if a.ascii {
		arrows = "Up/Down"
	}
	return []ui.Hint{hint("Esc", "Close help", 0), hint(arrows, "Scroll", 1)}
}
