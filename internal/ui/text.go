package ui

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// Width is the display width of s in terminal cells; escape sequences are
// ignored.
func Width(s string) int { return ansi.StringWidth(s) }

// Truncate shortens s to at most w cells, ending with the glyph ellipsis when
// something was cut. It keeps escape sequences intact.
func (t Theme) Truncate(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if Width(s) <= w {
		return s
	}
	tail := t.Glyphs.Ellipsis
	if Width(tail) >= w {
		return ansi.Truncate(s, w, "")
	}
	return ansi.Truncate(s, w, tail)
}

// Fit truncates s and right-pads it with spaces to exactly w cells.
func (t Theme) Fit(s string, w int) string {
	if w <= 0 {
		return ""
	}
	s = t.Truncate(s, w)
	if pad := w - Width(s); pad > 0 {
		s += strings.Repeat(" ", pad)
	}
	return s
}

// Wrap wraps plain text to width, breaking long tokens such as URLs at
// natural boundaries.
func Wrap(s string, width int) string {
	if width < 1 {
		width = 1
	}
	return ansi.Wrap(s, width, "/&?=-")
}

// Rule is a subtle horizontal separator width cells wide.
func (t Theme) Rule(width int) string {
	if width < 1 {
		return ""
	}
	return t.subtle().Render(strings.Repeat(t.Glyphs.Rule, width))
}

// subtle is the style of separators.
func (t Theme) subtle() lipgloss.Style {
	s := lipgloss.NewStyle()
	if c := t.BorderColor(); c != nil {
		s = s.Foreground(c)
	}
	return s
}

// Block cuts or pads text to exactly h lines of exactly w cells, so panes can
// be laid out side by side without shifting.
func (t Theme) Block(text string, w, h int) string {
	if w < 1 || h < 1 {
		return ""
	}
	lines := strings.Split(text, "\n")
	if len(lines) > h {
		lines = lines[:h]
	}
	out := make([]string, h)
	for i := range out {
		if i < len(lines) {
			out[i] = t.Fit(lines[i], w)
		} else {
			out[i] = strings.Repeat(" ", w)
		}
	}
	return strings.Join(out, "\n")
}

// Columns joins equally tall blocks side by side. Each block must already be
// a fixed width (use [Theme.Block]); gap is drawn between neighbours on every
// line (see [Theme.Gap]).
func Columns(blocks []string, gap string) string {
	if len(blocks) == 0 {
		return ""
	}
	split := make([][]string, len(blocks))
	rows := 0
	for i, b := range blocks {
		split[i] = strings.Split(b, "\n")
		rows = max(rows, len(split[i]))
	}
	var sb strings.Builder
	for r := 0; r < rows; r++ {
		if r > 0 {
			sb.WriteByte('\n')
		}
		for i := range split {
			if i > 0 {
				sb.WriteString(gap)
			}
			if r < len(split[i]) {
				sb.WriteString(split[i][r])
			}
		}
	}
	return sb.String()
}

// Gap is the vertical separator between side-by-side panes: a subtle rule
// with one cell of space on each side.
func (t Theme) Gap() string { return " " + t.subtle().Render(t.Glyphs.VRule) + " " }

// Indent prefixes every line of s with n spaces.
func Indent(s string, n int) string {
	if n <= 0 {
		return s
	}
	pad := strings.Repeat(" ", n)
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		if l != "" {
			lines[i] = pad + l
		}
	}
	return strings.Join(lines, "\n")
}

// Center places block (possibly multi-line) horizontally in width cells.
func Center(block string, width int) string {
	w := 0
	for _, l := range strings.Split(block, "\n") {
		w = max(w, Width(l))
	}
	return Indent(block, max(0, (width-w)/2))
}
