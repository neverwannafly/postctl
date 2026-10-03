package tui

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/shubham/postctl/internal/ui"
)

// whichKeyView renders the popup for the pending prefix, or "" when none is
// showing. It is a pure function of the model.
func (a *app) whichKeyView(g geometry) string {
	if a.pend == nil || !a.pend.shown {
		return ""
	}
	t := a.theme
	entries := a.pendingEntries()
	var title string
	switch a.pend.prefix {
	case prefixLeader:
		title = "Space"
	case prefixGoto:
		title = "g  go to"
	default:
		title = a.pend.prefix + "  sections"
	}
	width := min(g.width-2, 100)
	if width < 20 {
		return ""
	}
	inner := width - 4
	cols := 2
	if width >= 96 {
		cols = 3
	}
	gap := 3
	colW := max(10, (inner-gap*(cols-1))/cols)
	keyW := 0
	for _, e := range entries {
		keyW = max(keyW, ui.Width(e.Key))
	}
	keyW = min(keyW, 7)

	// Column-major layout: entries fill each column top to bottom. The popup
	// must leave room for the border, the title and the footer note.
	maxRows := max(1, g.bodyH-5)
	rows := (len(entries) + cols - 1) / cols
	more := 0
	if rows > maxRows {
		rows = maxRows
		more = len(entries) - rows*cols
	}
	cell := func(e whichEntry) string {
		label := t.Truncate(e.Label, max(1, colW-keyW-2))
		return t.Bold().Render(t.Fit(e.Key, keyW)) + "  " + t.Render(ui.TextSecondary, label)
	}
	lines := []string{t.Bold().Render(title) + strings.Repeat(" ", max(1, inner-ui.Width(title)-ui.Width("Esc Cancel"))) + t.Render(ui.TextSecondary, "Esc Cancel")}
	for r := 0; r < rows; r++ {
		var parts []string
		for c := 0; c < cols; c++ {
			i := c*rows + r
			if i >= len(entries) {
				break
			}
			cellText := cell(entries[i])
			if c < cols-1 {
				cellText = ansi.Truncate(cellText, colW, "")
				cellText += strings.Repeat(" ", max(0, colW-ui.Width(cellText)))
			}
			parts = append(parts, cellText)
		}
		lines = append(lines, strings.Join(parts, strings.Repeat(" ", gap)))
	}
	if more > 0 {
		lines = append(lines, t.Render(ui.TextQuiet, "…and "+plural(more, "more key", "more keys")+". Use Ctrl-P for the rest."))
	}
	return t.Modal(strings.Join(lines, "\n"), width, false)
}

// overlayBottom draws box centered horizontally at the bottom of base.
func overlayBottom(base, box string, g geometry) string {
	baseLines := strings.Split(base, "\n")
	boxLines := strings.Split(strings.Trim(box, "\n"), "\n")
	bw := 0
	for _, l := range boxLines {
		bw = max(bw, ui.Width(l))
	}
	x := max(0, (g.width-bw)/2)
	y := max(0, len(baseLines)-len(boxLines))
	for i, bl := range boxLines {
		row := y + i
		if row >= len(baseLines) {
			break
		}
		line := baseLines[row]
		left := ansi.Truncate(line, x, "")
		if pad := x - ui.Width(left); pad > 0 {
			left += strings.Repeat(" ", pad)
		}
		right := ansi.TruncateLeft(line, x+ui.Width(bl), "")
		baseLines[row] = left + bl + right
	}
	return strings.Join(baseLines, "\n")
}
