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
		popupRows := min(4, len(m.completions), len(rows))
		cursorRow := min(len(rows)-1, max(0, m.row-m.top))
		start := cursorRow + 1
		if start+popupRows > len(rows) {
			start = max(0, cursorRow-popupRows)
		}
		cursorX := gutter + displayColumn(m.lines[m.row], m.col, m.opts.TabText) -
			displayColumn(m.lines[m.row], m.left, m.opts.TabText)
		cursorX = min(max(0, cursorX), max(0, m.width-8))
		for i := 0; i < popupRows; i++ {
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
			rows[start+i] = strings.Repeat(" ", cursorX) + m.th.Fit(style.Render(text), m.width-cursorX)
		}
	}
	return strings.Join(rows, "\n")
}

func (m *Model) renderLine(row, width int) string {
	line := m.lines[row]
	cs := clusters(line)
	mask := m.opts.Mask
	from := min(m.left, len(cs))
	var b strings.Builder
	used := 0
	byteOff := byteAt(line, from)
	for i := from; i < len(cs); i++ {
		source := cs[i]
		cell := source
		if mask != "" && source != "\t" {
			cell = mask
		} else if source == "\t" {
			cell = strings.Repeat(" ", max(1, len(m.opts.TabText)))
		}
		cw := max(1, uniseg.StringWidth(cell))
		if used+cw > width {
			break
		}
		style := m.styleAt(row, i, byteOff)
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
		byteOff += len(source)
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
	scrolloff := min(2, max(0, (visibleHeight-1)/2))
	if m.row < m.top+scrolloff {
		m.top = max(0, m.row-scrolloff)
	}
	if m.row >= m.top+visibleHeight-scrolloff {
		m.top = min(max(0, len(m.lines)-visibleHeight), m.row-visibleHeight+scrolloff+1)
	}
	room := max(1, m.width-m.gutterWidth())
	cs := clusters(m.lines[m.row])
	col := min(m.col, len(cs))
	if col < m.left {
		m.left = col
	}
	width := 0
	for i := m.left; i < col; i++ {
		width += clusterWidth(cs[i], m.opts.TabText)
	}
	cursorWidth := 1
	if col < len(cs) {
		cursorWidth = clusterWidth(cs[col], m.opts.TabText)
	}
	for width+cursorWidth > room && m.left < col {
		width -= clusterWidth(cs[m.left], m.opts.TabText)
		m.left++
	}
	if m.left < 0 {
		m.left = 0
	}
	// When the whole line fits, keep the start visible (SetValue/cursorEnd often
	// run before the widget is given its final width).
	total := 0
	for _, c := range cs {
		total += clusterWidth(c, m.opts.TabText)
	}
	pastEnd := col >= len(cs)
	if pastEnd {
		if total+cursorWidth <= room {
			m.left = 0
		} else {
			need := total + cursorWidth - room
			left := 0
			widthBefore := 0
			for left < len(cs) && widthBefore < need {
				widthBefore += clusterWidth(cs[left], m.opts.TabText)
				left++
			}
			m.left = left
		}
	} else if total <= room {
		m.left = 0
	}
}

func clusterWidth(cluster, tabText string) int {
	if cluster == "\t" {
		return max(1, len(tabText))
	}
	return max(1, uniseg.StringWidth(cluster))
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
