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
		// Slide the menu left when the cursor is near the right edge, so its
		// widest row (label and detail) stays readable.
		need := 0
		for i := 0; i < popupRows; i++ {
			item := m.completions[(m.completeSel+i)%len(m.completions)]
			w := 2 + ui.Width(item.Label)
			if item.Detail != "" {
				w += 2 + ui.Width(item.Detail)
			}
			need = max(need, w)
		}
		cursorX = min(max(0, cursorX), max(0, m.width-max(8, need)))
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
		if mask != "" {
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

// ScrollLines moves the top visible line by n lines (positive scrolls down),
// like Ctrl-e and Ctrl-y: the window moves, the cursor stays inside it (it is
// pushed to the nearest row that is visible, honouring the scroll margin), and
// the text, the mode and any pending command are untouched.
func (m *Model) ScrollLines(n int) {
	if n == 0 || len(m.lines) == 0 {
		return
	}
	visible := max(1, m.height)
	if m.searching {
		visible = max(1, visible-1)
	}
	maxTop := max(0, len(m.lines)-visible)
	top := min(max(0, m.top+n), maxTop)
	if top == m.top {
		return
	}
	m.top = top
	margin := min(2, max(0, (visible-1)/2))
	lo, hi := top+margin, top+visible-1-margin
	if top == 0 {
		lo = 0
	}
	if top == maxTop {
		hi = len(m.lines) - 1
	}
	switch {
	case m.row < lo:
		m.moveVertical(lo - m.row)
	case m.row > hi:
		m.moveVertical(hi - m.row)
	}
	m.ensureVisible()
}

// Top is the zero-based index of the first visible line.
func (m *Model) Top() int { return m.top }

// ScrollColumns moves a single-line editor's horizontal window by grapheme
// columns. The cursor is kept in the visible window and the buffer and mode
// are unchanged.
func (m *Model) ScrollColumns(n int) {
	if n == 0 || len(m.lines) == 0 {
		return
	}
	cs := clusters(m.lines[m.row])
	room := max(1, m.width-m.gutterWidth())
	maxLeft := max(0, len(cs)-room)
	m.left = min(max(0, m.left+n), maxLeft)
	if m.col < m.left {
		m.col = m.left
	}
	if m.col >= m.left+room {
		m.col = min(len(cs), m.left+room-1)
	}
}

// PosAt maps a cell in the editor's own frame to a zero-based grapheme
// position. Cells in the line-number gutter or outside the frame do not map.
// Either cell of a wide grapheme maps to that grapheme. A position beyond the
// displayed end of a line clamps to its last grapheme in normal mode and to
// EOL in insert mode. An empty buffer, including its placeholder, maps to 0,0.
func (m *Model) PosAt(x, y int) (row, col int, ok bool) {
	if x < 0 || x >= m.width || y < 0 || y >= m.height {
		return 0, 0, false
	}
	bodyHeight := m.height
	if m.searching {
		bodyHeight--
	}
	if y >= bodyHeight {
		return 0, 0, false
	}
	gutter := m.gutterWidth()
	if x < gutter {
		return 0, 0, false
	}
	row = m.top + y
	if row >= len(m.lines) {
		return 0, 0, false
	}
	cs := clusters(m.lines[row])
	if len(cs) == 0 {
		return row, 0, true
	}
	cell := x - gutter
	used := 0
	for i := min(m.left, len(cs)); i < len(cs); i++ {
		width := clusterWidth(cs[i], m.opts.TabText)
		if m.opts.Mask != "" {
			width = max(1, uniseg.StringWidth(m.opts.Mask))
		}
		if cell < used+width {
			return row, i, true
		}
		used += width
	}
	if m.mode == Insert {
		return row, len(cs), true
	}
	return row, len(cs) - 1, true
}
