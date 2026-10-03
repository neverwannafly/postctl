package vimtext

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/rivo/uniseg"
	"github.com/shubham/postctl/internal/ui"
)

// View renders the editor without mutating it.
func (m *Model) View() string {
	if !m.focused && m.Value() == "" && m.opts.Placeholder != "" {
		return m.th.Block(m.th.Render(ui.TextQuiet, m.opts.Placeholder), m.width, m.height)
	}
	gutter := m.gutterWidth()
	bodyWidth := max(1, m.width-gutter)
	rows := make([]string, 0, m.height)
	bodyHeight := m.height
	if m.searching {
		bodyHeight--
	}
	for screenRow := 0; screenRow < bodyHeight; screenRow++ {
		row := m.top + screenRow
		if row >= len(m.lines) {
			rows = append(rows, strings.Repeat(" ", m.width))
			continue
		}
		prefix := ""
		if m.opts.LineNumbers {
			prefix = m.th.Render(ui.TextQuiet, fmt.Sprintf("%*d ", gutter-1, row+1))
		}
		line := m.renderLine(row, bodyWidth)
		rows = append(rows, prefix+m.th.Fit(line, bodyWidth))
	}
	if m.searching {
		prefix := "/"
		if m.searchDir < 0 {
			prefix = "?"
		}
		rows = append(rows, m.th.Fit(m.th.Render(ui.AccentPrimary, prefix)+m.searchText, m.width))
	}
	if len(m.completions) > 0 && !m.searching {
		start := min(len(rows), max(0, m.row-m.top+1))
		for i := 0; i < min(4, len(m.completions)) && start+i < len(rows); i++ {
			idx := (m.completeSel + i) % len(m.completions)
			item := m.completions[idx]
			marker := "  "
			style := m.th.Style(ui.TextSecondary)
			if idx == m.completeSel {
				marker = m.th.Glyphs.Select + " "
				style = m.th.Style(ui.AccentPrimary).Reverse(true)
			}
			text := marker + item.Label
			if item.Detail != "" {
				text += "  " + item.Detail
			}
			rows[start+i] = m.th.Fit(style.Render(text), m.width)
		}
	}
	return strings.Join(rows, "\n")
}

func (m *Model) renderLine(row, width int) string {
	line := m.lines[row]
	cs := clusters(line)
	from := min(m.left, len(cs))
	var b strings.Builder
	used := 0
	for i := from; i < len(cs); i++ {
		cell := cs[i]
		if cell == "\t" {
			cell = strings.Repeat(" ", max(1, len(m.opts.TabText)))
		}
		cw := max(1, uniseg.StringWidth(cell))
		if used+cw > width {
			break
		}
		style := m.styleAt(row, i, byteAt(line, i))
		if m.selected(row, i) {
			style = style.Reverse(true)
			if bg := m.th.SelectedBG(); bg != nil {
				style = style.Background(bg)
			}
		}
		if m.focused && row == m.row && i == m.col {
			style = style.Reverse(true)
		}
		b.WriteString(style.Render(cell))
		used += cw
	}
	if m.focused && row == m.row && m.col >= len(cs) && used < width {
		b.WriteString(lipgloss.NewStyle().Reverse(true).Render(" "))
	}
	return b.String()
}

func (m *Model) styleAt(row, _ int, byteOff int) lipgloss.Style {
	if row < len(m.highlights) {
		for _, span := range m.highlights[row] {
			if byteOff >= span.Start && byteOff < span.End {
				return span.Style
			}
		}
	}
	return m.th.Style(ui.TextPrimary)
}

func (m *Model) selected(row, col int) bool {
	if m.mode != Visual && m.mode != VisualLine {
		return false
	}
	a, b := ordered(m.anchor, position{m.row, m.col})
	if m.mode == VisualLine {
		return row >= a.row && row <= b.row
	}
	p := position{row, col}
	return !less(p, a) && !less(b, p)
}

func (m *Model) gutterWidth() int {
	if !m.opts.LineNumbers {
		return 0
	}
	return len(fmt.Sprintf("%d", len(m.lines))) + 1
}

func (m *Model) ensureVisible() {
	visibleHeight := max(1, m.height)
	if m.searching {
		visibleHeight = max(1, visibleHeight-1)
	}
	if m.row < m.top+2 {
		m.top = max(0, m.row-2)
	}
	if m.row >= m.top+visibleHeight-2 {
		m.top = min(max(0, len(m.lines)-visibleHeight), m.row-visibleHeight+3)
	}
	room := max(1, m.width-m.gutterWidth())
	cs := clusters(m.lines[m.row])
	col := min(m.col, len(cs))
	if col < m.left {
		m.left = col
	}
	width := 0
	for i := m.left; i < col; i++ {
		width += max(1, uniseg.StringWidth(cs[i]))
	}
	for width >= room && m.left < col {
		width -= max(1, uniseg.StringWidth(cs[m.left]))
		m.left++
	}
	if m.left < 0 {
		m.left = 0
	}
}

func (m *Model) rehighlight() {
	if m.opts.Highlighter == nil {
		m.highlights = nil
		return
	}
	m.highlights = m.opts.Highlighter.Highlight(append([]string(nil), m.lines...))
}

func (m *Model) rehighlightRow(row int) {
	if m.opts.Highlighter == nil {
		return
	}
	if len(m.highlights) != len(m.lines) {
		m.rehighlight()
		return
	}
	if row < 0 || row >= len(m.lines) {
		return
	}
	spans := m.opts.Highlighter.Highlight([]string{m.lines[row]})
	if len(spans) == 1 {
		m.highlights[row] = spans[0]
	}
}
