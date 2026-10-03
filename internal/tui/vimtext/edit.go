package vimtext

import (
	"strings"
	"unicode"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/rivo/uniseg"
)

func (m *Model) beginChange(msg tea.Msg) {
	if m.opts.ReadOnly || m.changeStart != nil {
		return
	}
	s := m.snap()
	m.changeStart = &s
	m.changeKeys = nil
	if msg != nil {
		m.changeKeys = append(m.changeKeys, msg)
	}
}

func (m *Model) recordChange(msg tea.Msg) {
	if m.changeStart != nil && !m.replaying {
		m.changeKeys = append(m.changeKeys, msg)
	}
}

func (m *Model) finishChange() {
	if m.changeStart == nil {
		return
	}
	if strings.Join(m.changeStart.lines, "\n") != m.Value() {
		m.pushUndo(*m.changeStart)
		m.redo = nil
		if !m.replaying {
			m.lastChange = append([]tea.Msg(nil), m.changeKeys...)
		}
	}
	m.changeStart, m.changeKeys = nil, nil
	m.rehighlight()
	m.ensureVisible()
}

func (m *Model) undoChange() bool {
	if len(m.undo) == 0 {
		return false
	}
	m.pushRedo(m.snap())
	s := m.undo[len(m.undo)-1]
	m.undo = m.undo[:len(m.undo)-1]
	m.restore(s)
	return true
}

func (m *Model) redoChange() bool {
	if len(m.redo) == 0 {
		return false
	}
	m.pushUndo(m.snap())
	s := m.redo[len(m.redo)-1]
	m.redo = m.redo[:len(m.redo)-1]
	m.restore(s)
	return true
}

const historyLimit = 1000

func appendBounded[T any](items []T, item T) []T {
	if len(items) >= historyLimit {
		copy(items, items[len(items)-historyLimit+1:])
		items = items[:historyLimit-1]
	}
	return append(items, item)
}

func (m *Model) pushUndo(s snapshot) { m.undo = appendBounded(m.undo, s) }
func (m *Model) pushRedo(s snapshot) { m.redo = appendBounded(m.redo, s) }

func (m *Model) insertText(s string) bool {
	if m.opts.SingleLine {
		s = collapseNewlines(s)
	}
	if s == "" {
		return false
	}
	cs := clusters(m.lines[m.row])
	before, after := joinClusters(cs[:m.col]), joinClusters(cs[m.col:])
	parts := strings.Split(s, "\n")
	if len(parts) == 1 {
		m.lines[m.row] = before + s + after
		m.col += len(clusters(s))
		return true
	}
	repl := make([]string, len(parts))
	repl[0] = before + parts[0]
	copy(repl[1:], parts[1:])
	repl[len(repl)-1] += after
	m.lines = append(m.lines[:m.row], append(repl, m.lines[m.row+1:]...)...)
	m.row += len(repl) - 1
	m.col = len(clusters(parts[len(parts)-1]))
	return true
}

func (m *Model) deleteBackward() bool {
	if m.col > 0 {
		cs := clusters(m.lines[m.row])
		m.lines[m.row] = joinClusters(append(cs[:m.col-1], cs[m.col:]...))
		m.col--
		return true
	}
	if m.row == 0 || m.opts.SingleLine {
		return false
	}
	prev := m.lines[m.row-1]
	m.col = len(clusters(prev))
	m.lines[m.row-1] = prev + m.lines[m.row]
	m.lines = append(m.lines[:m.row], m.lines[m.row+1:]...)
	m.row--
	return true
}

func (m *Model) deleteForward() bool {
	cs := clusters(m.lines[m.row])
	if m.col < len(cs) {
		m.lines[m.row] = joinClusters(append(cs[:m.col], cs[m.col+1:]...))
		return true
	}
	if m.row+1 >= len(m.lines) || m.opts.SingleLine {
		return false
	}
	m.lines[m.row] += m.lines[m.row+1]
	m.lines = append(m.lines[:m.row+1], m.lines[m.row+2:]...)
	return true
}

func less(a, b position) bool {
	return a.row < b.row || a.row == b.row && a.col < b.col
}

func ordered(a, b position) (position, position) {
	if less(b, a) {
		return b, a
	}
	return a, b
}

func (m *Model) rangeText(a, b position, inclusive bool) string {
	a, b = ordered(a, b)
	if inclusive {
		b.col++
	}
	b.col = min(b.col, m.lineLen(b.row))
	if a.row == b.row {
		cs := clusters(m.lines[a.row])
		return joinClusters(cs[a.col:b.col])
	}
	out := []string{joinClusters(clusters(m.lines[a.row])[a.col:])}
	out = append(out, m.lines[a.row+1:b.row]...)
	out = append(out, joinClusters(clusters(m.lines[b.row])[:b.col]))
	return strings.Join(out, "\n")
}

func (m *Model) deleteRange(a, b position, inclusive bool) string {
	a, b = ordered(a, b)
	text := m.rangeText(a, b, inclusive)
	if inclusive {
		b.col++
	}
	b.col = min(b.col, m.lineLen(b.row))
	if a.row == b.row {
		cs := clusters(m.lines[a.row])
		m.lines[a.row] = joinClusters(append(cs[:a.col], cs[b.col:]...))
	} else {
		left := joinClusters(clusters(m.lines[a.row])[:a.col])
		right := joinClusters(clusters(m.lines[b.row])[b.col:])
		m.lines[a.row] = left + right
		m.lines = append(m.lines[:a.row+1], m.lines[b.row+1:]...)
	}
	if len(m.lines) == 0 {
		m.lines = []string{""}
	}
	m.row, m.col = a.row, a.col
	m.SetCursor(m.row, m.col)
	return text
}

func (m *Model) clampNormalCursor() {
	if m.mode != Normal {
		return
	}
	m.col = min(m.col, max(0, m.lineLen(m.row)-1))
	m.preferred = displayColumn(m.lines[m.row], m.col, m.opts.TabText)
}

func (m *Model) lineRange(a, b int, remove bool) string {
	a, b = min(a, b), max(a, b)
	text := strings.Join(m.lines[a:b+1], "\n") + "\n"
	if remove {
		m.lines = append(m.lines[:a], m.lines[b+1:]...)
		if len(m.lines) == 0 {
			m.lines = []string{""}
		}
		m.SetCursor(min(a, len(m.lines)-1), 0)
	}
	return text
}

func (m *Model) clipboard(text string) tea.Cmd {
	return m.opts.Clipboard(m.opts.Redact(text))
}

func isSmallWord(s string) bool {
	r, _ := utf8.DecodeRuneInString(s)
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_'
}

func isSpaceCluster(s string) bool {
	r, _ := utf8.DecodeRuneInString(s)
	return unicode.IsSpace(r)
}

func sameWordClass(a, b string, big bool) bool {
	if big {
		return !isSpaceCluster(a) && !isSpaceCluster(b)
	}
	return isSmallWord(a) == isSmallWord(b) && isSpaceCluster(a) == isSpaceCluster(b)
}

func (m *Model) moveHorizontal(delta int) {
	m.col = min(max(0, m.col+delta), max(0, m.lineLen(m.row)-boolInt(m.mode == Insert)))
	m.preferred = displayColumn(m.lines[m.row], m.col, m.opts.TabText)
}

func boolInt(v bool) int {
	if v {
		return 0
	}
	return 1
}

func (m *Model) moveVertical(delta int) {
	if m.opts.SingleLine {
		return
	}
	m.row = min(max(0, m.row+delta), len(m.lines)-1)
	switch m.preferred {
	case -1:
		m.col = max(0, m.lineLen(m.row)-boolInt(m.mode == Insert))
	case -2:
		m.col = firstWordColumn(m.lines[m.row])
	default:
		m.col = columnAtDisplay(m.lines[m.row], m.preferred, m.mode == Insert, m.opts.TabText)
	}
}

func (m *Model) wordForward(big, end bool) {
	if end {
		m.wordEndForward(big)
		return
	}
	row, col := m.row, m.col
	cs := clusters(m.lines[row])
	if len(cs) == 0 {
		if row+1 < len(m.lines) {
			row++
			col = firstWordColumn(m.lines[row])
		}
		m.setMotionPosition(row, col)
		return
	}
	col = min(col, len(cs)-1)
	if !isSpaceCluster(cs[col]) {
		class := cs[col]
		for col < len(cs) && !isSpaceCluster(cs[col]) && sameWordClass(class, cs[col], big) {
			col++
		}
	}
	for col < len(cs) && isSpaceCluster(cs[col]) {
		col++
	}
	if col < len(cs) {
		m.setMotionPosition(row, col)
		return
	}
	if row+1 >= len(m.lines) {
		m.setMotionPosition(row, max(0, len(cs)-1))
		return
	}
	row++
	if m.lines[row] == "" {
		m.setMotionPosition(row, 0)
		return
	}
	m.setMotionPosition(row, firstWordColumn(m.lines[row]))
}

func (m *Model) wordBackward(big bool) {
	row, col := m.row, m.col
	if col == 0 {
		if row == 0 {
			return
		}
		row--
		if m.lines[row] == "" {
			m.setMotionPosition(row, 0)
			return
		}
		col = len(clusters(m.lines[row]))
	}
	cs := clusters(m.lines[row])
	for col > 0 && isSpaceCluster(cs[col-1]) {
		col--
	}
	if col > 0 {
		col--
		for col > 0 && sameWordClass(cs[col], cs[col-1], big) {
			col--
		}
	}
	m.setMotionPosition(row, col)
}

func (m *Model) wordEndForward(big bool) {
	row, col := m.row, m.col
	cs := clusters(m.lines[row])
	if len(cs) == 0 {
		if row+1 < len(m.lines) {
			row++
			cs = clusters(m.lines[row])
			col = 0
		}
	} else if col+1 >= len(cs) || !sameWordClass(cs[col], cs[col+1], big) {
		col = min(col+1, len(cs))
	}
	for {
		for col < len(cs) && isSpaceCluster(cs[col]) {
			col++
		}
		if col < len(cs) {
			for col+1 < len(cs) && sameWordClass(cs[col], cs[col+1], big) {
				col++
			}
			m.setMotionPosition(row, col)
			return
		}
		if row+1 >= len(m.lines) {
			m.setMotionPosition(row, max(0, len(cs)-1))
			return
		}
		row++
		cs = clusters(m.lines[row])
		col = 0
		if len(cs) == 0 {
			m.setMotionPosition(row, 0)
			return
		}
	}
}

func (m *Model) wordEndBackward(big bool) {
	row, col := m.row, m.col
	if col > 0 {
		cs := clusters(m.lines[row])
		col--
		for col > 0 && !isSpaceCluster(cs[col-1]) && sameWordClass(cs[col], cs[col-1], big) {
			col--
		}
	}
	for {
		if col == 0 {
			if row == 0 {
				m.setMotionPosition(0, 0)
				return
			}
			row--
			if m.lines[row] == "" {
				m.setMotionPosition(row, 0)
				return
			}
			col = len(clusters(m.lines[row]))
		}
		cs := clusters(m.lines[row])
		for col > 0 && isSpaceCluster(cs[col-1]) {
			col--
		}
		if col > 0 {
			col--
			for col+1 < len(cs) && sameWordClass(cs[col], cs[col+1], big) {
				col++
			}
			m.setMotionPosition(row, col)
			return
		}
	}
}

func firstWordColumn(line string) int {
	cs := clusters(line)
	for i, c := range cs {
		if !isSpaceCluster(c) {
			return i
		}
	}
	return 0
}

func (m *Model) setMotionPosition(row, col int) {
	m.row, m.col = row, col
	m.preferred = displayColumn(m.lines[row], col, m.opts.TabText)
}

func displayColumn(line string, col int, tabText string) int {
	cs := clusters(line)
	col = min(max(0, col), len(cs))
	width := 0
	for _, c := range cs[:col] {
		if c == "\t" {
			width += max(1, len(tabText))
		} else {
			width += max(1, uniseg.StringWidth(c))
		}
	}
	return width
}

func columnAtDisplay(line string, want int, insert bool, tabText string) int {
	cs := clusters(line)
	width := 0
	for i, c := range cs {
		cw := max(1, uniseg.StringWidth(c))
		if c == "\t" {
			cw = max(1, len(tabText))
		}
		if width+cw > want {
			return i
		}
		width += cw
	}
	if insert {
		return len(cs)
	}
	return max(0, len(cs)-1)
}

func (m *Model) firstNonBlank() {
	cs := clusters(m.lines[m.row])
	m.col = 0
	for m.col < len(cs) && isSpaceCluster(cs[m.col]) {
		m.col++
	}
	m.preferred = -2
}

func (m *Model) findChar(kind byte, target string, count int) {
	cs := clusters(m.lines[m.row])
	if len(target) == 0 {
		return
	}
	forward := kind == 'f' || kind == 't'
	idx := m.col
	for range max(1, count) {
		found := -1
		if forward {
			for i := idx + 1; i < len(cs); i++ {
				if cs[i] == target {
					found = i
					break
				}
			}
		} else {
			for i := idx - 1; i >= 0; i-- {
				if cs[i] == target {
					found = i
					break
				}
			}
		}
		if found < 0 {
			return
		}
		idx = found
	}
	switch kind {
	case 't':
		idx--
	case 'T':
		idx++
	}
	m.col = min(max(0, idx), max(0, len(cs)-1))
	m.preferred = m.col
	m.lastFind, m.lastFindKey, m.lastFindPos = kind, target, idx
}

func (m *Model) repeatFind(reverse bool, count int) {
	if m.lastFind == 0 {
		return
	}
	kind := m.lastFind
	if reverse {
		kind = reverseFind(kind)
	}
	switch kind {
	case 't':
		m.col = min(m.lineLen(m.row)-1, m.col+1)
	case 'T':
		m.col = max(0, m.col-1)
	}
	m.findChar(kind, m.lastFindKey, count)
}
