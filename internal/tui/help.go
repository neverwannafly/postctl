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

func (o *helpOverlay) update(_ *app, msg tea.Msg) (overlay, tea.Cmd) {
	k, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return o, nil
	}
	switch k.String() {
	case "esc", "?", "q", "enter":
		return nil, nil
	case "up":
		o.scroll--
	case "down":
		o.scroll++
	case "pgup":
		o.scroll -= max(1, o.page-1)
	case "pgdown", "space":
		o.scroll += max(1, o.page-1)
	case "home":
		o.scroll = 0
	case "end":
		o.scroll = o.lines
	}
	o.scroll = max(0, min(o.scroll, max(0, o.lines-o.page)))
	return o, nil
}

func (o *helpOverlay) view(a *app, w, h int) string {
	t := a.theme
	groups := a.keys.helpGroups(a.focusTitle(), a.focusHints(), a.responseHidden())

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
	o.page = max(1, h-3)
	o.lines = len(body)
	o.scroll = max(0, min(o.scroll, max(0, o.lines-o.page)))
	end := min(len(body), o.scroll+o.page)
	visible := strings.Join(body[o.scroll:end], "\n")
	if o.lines > o.page {
		more := "  " + strconv.Itoa(o.scroll+1) + "-" + strconv.Itoa(end) + " of " + strconv.Itoa(o.lines) + " lines, scroll for more"
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
