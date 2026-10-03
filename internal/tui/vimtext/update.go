package vimtext

import (
	"strings"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
)

// Update applies a Bubble Tea message and returns any parent-facing event.
func (m *Model) Update(msg tea.Msg) (tea.Cmd, Event) {
	if !m.focused {
		return nil, Event{}
	}
	if p, ok := msg.(tea.PasteMsg); ok {
		return m.updatePaste(p)
	}
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil, Event{}
	}
	if key.Key().Mod&tea.ModAlt != 0 {
		m.clearPending()
		return nil, Event{Kind: Unhandled, Key: key}
	}
	if m.searching {
		return m.updateSearch(key)
	}
	switch m.mode {
	case Insert:
		return m.updateInsert(key)
	case Visual, VisualLine:
		return m.updateVisual(key)
	default:
		return m.updateNormal(key)
	}
}

func (m *Model) updatePaste(p tea.PasteMsg) (tea.Cmd, Event) {
	if m.mode != Insert || m.opts.ReadOnly {
		return nil, Event{}
	}
	m.beginChange(p)
	if m.insertText(p.Content) {
		m.insertSession += p.Content
		m.recordChange(p)
		m.afterEdit()
		return nil, Event{Kind: Changed}
	}
	return nil, Event{}
}

func (m *Model) updateInsert(key tea.KeyPressMsg) (tea.Cmd, Event) {
	k := key.String()
	if len(m.completions) > 0 {
		switch k {
		case "ctrl+n", "down":
			m.completeSel = (m.completeSel + 1) % len(m.completions)
			return nil, Event{}
		case "ctrl+p", "up":
			m.completeSel = (m.completeSel - 1 + len(m.completions)) % len(m.completions)
			return nil, Event{}
		case "enter", "tab":
			m.beginChange(key)
			m.acceptCompletion()
			m.recordChange(key)
			m.afterEdit()
			return nil, Event{Kind: Changed}
		case "esc", "ctrl+c":
			m.completions = nil
			return nil, Event{}
		}
	}
	switch k {
	case "esc", "ctrl+c":
		m.finishCountedInsert()
		m.finishChange()
		m.insertBreak = false
		m.mode = Normal
		if m.col > 0 {
			m.col--
		}
		m.col = min(m.col, max(0, m.lineLen(m.row)-1))
		m.completions = nil
		m.ensureVisible()
		return nil, Event{}
	case "ctrl+s", "ctrl+enter", "ctrl+p":
		return nil, Event{Kind: Unhandled, Key: key}
	case "enter":
		if m.opts.SingleLine {
			return nil, Event{Kind: Submit}
		}
		return m.insertKey(key, "\n"+leadingIndent(m.lines[m.row]))
	case "tab":
		if m.opts.SingleLine {
			return nil, Event{Kind: Unhandled, Key: key}
		}
		return m.insertKey(key, m.opts.TabText)
	case "left":
		if m.col > 0 {
			m.finishChange()
			m.moveHorizontal(-1)
			m.insertStart = position{m.row, m.col}
			m.insertBreak = true
		}
	case "right":
		if m.col < m.lineLen(m.row) {
			m.finishChange()
			m.moveHorizontal(1)
			m.insertStart = position{m.row, m.col}
			m.insertBreak = true
		}
	case "up":
		if m.row > 0 {
			m.finishChange()
			m.moveVertical(-1)
			m.insertStart = position{m.row, m.col}
			m.insertBreak = true
		}
	case "down":
		if m.row+1 < len(m.lines) {
			m.finishChange()
			m.moveVertical(1)
			m.insertStart = position{m.row, m.col}
			m.insertBreak = true
		}
	case "backspace":
		m.beginChange(key)
		if m.deleteBackward() {
			m.recordChange(key)
			m.afterEdit()
			return nil, Event{Kind: Changed}
		}
	case "delete":
		m.beginChange(key)
		if m.deleteForward() {
			m.recordChange(key)
			m.afterEdit()
			return nil, Event{Kind: Changed}
		}
	case "ctrl+w":
		m.beginChange(key)
		if m.row == m.insertStart.row && m.col > m.insertStart.col {
			start := position{m.row, m.col}
			m.wordBackward(false)
			if m.col < m.insertStart.col {
				m.col = m.insertStart.col
			}
			if less(position{m.row, m.col}, start) {
				m.deleteRange(position{m.row, m.col}, start, false)
				m.recordChange(key)
				m.afterEdit()
				return nil, Event{Kind: Changed}
			}
		}
		if m.col == 0 && m.row > 0 && !m.opts.SingleLine {
			m.deleteBackward()
			m.recordChange(key)
			m.afterEdit()
			return nil, Event{Kind: Changed}
		}
		start := position{m.row, m.col}
		m.wordBackward(false)
		if less(position{m.row, m.col}, start) {
			m.deleteRange(position{m.row, m.col}, start, false)
			m.recordChange(key)
			m.afterEdit()
			return nil, Event{Kind: Changed}
		}
	case "ctrl+u":
		if m.row == m.insertStart.row && m.col > m.insertStart.col {
			m.beginChange(key)
			m.deleteRange(m.insertStart, position{m.row, m.col}, false)
			m.recordChange(key)
			m.afterEdit()
			return nil, Event{Kind: Changed}
		}
	default:
		if key.Key().Text != "" && key.Key().Mod&(tea.ModCtrl|tea.ModAlt) == 0 {
			return m.insertKey(key, key.Key().Text)
		}
		return nil, Event{Kind: Unhandled, Key: key}
	}
	m.ensureVisible()
	return nil, Event{}
}

func (m *Model) insertKey(key tea.KeyPressMsg, text string) (tea.Cmd, Event) {
	m.beginChange(key)
	if m.insertText(text) {
		m.insertSession += text
		m.recordChange(key)
		m.afterEdit()
		return nil, Event{Kind: Changed}
	}
	return nil, Event{}
}

func (m *Model) updateNormal(key tea.KeyPressMsg) (tea.Cmd, Event) {
	k := key.String()
	if k == "esc" {
		if m.pending != "" {
			m.clearPending()
			return nil, Event{}
		}
		return nil, Event{Kind: Leave}
	}
	if m.gPending {
		m.gPending = false
		switch k {
		case "g":
			want := m.preferred
			if m.count > 0 {
				m.row = min(m.count-1, len(m.lines)-1)
			} else {
				m.row = 0
			}
			m.col = columnAtDisplay(m.lines[m.row], max(0, want), false, m.opts.TabText)
			m.preferred = displayColumn(m.lines[m.row], m.col, m.opts.TabText)
			m.clearPending()
			m.ensureVisible()
			return nil, Event{}
		case "e", "E":
			for range max(1, m.count) {
				m.wordEndBackward(k == "E")
			}
			m.clearPending()
			return nil, Event{}
		default:
			m.clearPending()
			return nil, Event{Kind: Seq, Seq: "g" + keyText(key)}
		}
	}
	if k == "ctrl+s" || k == "ctrl+enter" || k == "ctrl+p" || k == "ctrl+c" ||
		k == "ctrl+h" || k == "ctrl+j" || k == "ctrl+k" || k == "ctrl+l" ||
		k == "ctrl+o" || k == "ctrl+i" || k == "tab" || k == "shift+tab" ||
		k == " " || k == ":" || k == "q" {
		m.clearPending()
		return nil, Event{Kind: Unhandled, Key: key}
	}
	if m.replace {
		count := max(1, m.operatorCnt)
		m.replace = false
		m.pending = ""
		if m.opts.ReadOnly {
			return nil, Event{}
		}
		if k == "enter" {
			cs := clusters(m.lines[m.row])
			if m.col+count > len(cs) {
				return nil, Event{}
			}
			before := m.snap()
			left := joinClusters(cs[:m.col])
			right := joinClusters(cs[m.col+count:])
			if strings.TrimSpace(left) == "" {
				right = strings.TrimLeft(right, " \t")
			}
			m.lines[m.row] = left
			m.lines = append(m.lines[:m.row+1], append([]string{right}, m.lines[m.row+1:]...)...)
			m.row++
			m.col = 0
			m.pushUndo(before)
			m.redo = nil
			m.lastChange = append(countedKeys(count, 'r'), key)
			m.rehighlight()
			return nil, Event{Kind: Changed}
		}
		if key.Key().Text == "" {
			return nil, Event{}
		}
		cs := clusters(m.lines[m.row])
		if m.col < len(cs) && m.col+count <= len(cs) {
			before := m.snap()
			end := m.col + count
			for i := m.col; i < end; i++ {
				cs[i] = key.Key().Text
			}
			m.lines[m.row] = joinClusters(cs)
			m.col = end - 1
			m.pushUndo(before)
			m.redo = nil
			m.lastChange = append(countedKeys(count, 'r'), key)
			m.rehighlightRow(m.row)
			return nil, Event{Kind: Changed}
		}
		return nil, Event{}
	}
	if m.findKind != 0 {
		kind, count, op, at := m.findKind, max(1, m.operatorCnt)*max(1, m.count), m.operator, m.operatorAt
		before := m.snap()
		m.clearPending()
		if key.Key().Text != "" {
			found := m.findChar(kind, key.Key().Text, count)
			if op != 0 {
				if !found {
					return nil, Event{}
				}
				end := position{m.row, m.col}
				inclusive := kind == 'f' || kind == 't'
				text := m.rangeText(at, end, inclusive)
				m.reg, m.regLine = text, false
				if op == 'y' {
					m.row, m.col = at.row, at.col
					return m.clipboard(text), Event{}
				}
				m.deleteRange(at, end, inclusive)
				if op == 'c' {
					m.mode = Insert
					m.changeStart = &before
					m.changeKeys = []tea.Msg{runeKey(op), runeKey(kind), key}
				} else {
					m.pushUndo(before)
					m.redo = nil
					m.lastChange = []tea.Msg{runeKey(op), runeKey(kind), key}
					m.clampNormalCursor()
				}
				m.rehighlight()
				return m.clipboard(text), Event{Kind: Changed}
			}
		}
		return nil, Event{}
	}
	if m.textObject != 0 {
		return m.finishTextObject(key)
	}
	if isDigitKey(k) && (k != "0" || m.count > 0) {
		m.count = m.count*10 + int(k[0]-'0')
		m.pending += k
		return nil, Event{}
	}
	if m.operator != 0 {
		return m.finishOperator(key)
	}

	count := max(1, m.count)
	if m.applyMotion(k, count) {
		m.clearPending()
		return nil, Event{}
	}
	switch k {
	case "enter":
		if m.opts.SingleLine {
			m.clearPending()
			return nil, Event{Kind: Submit}
		}
	case "g":
		m.gPending, m.pending = true, m.pending+"g"
		return nil, Event{}
	case "f", "t", "F", "T":
		m.findKind, m.pending = k[0], m.pending+k
		return nil, Event{}
	case ";", ",":
		if !m.repeatFind(k == ",", count) {
			m.clearPending()
			return nil, Event{}
		}
	case "d", "c", "y":
		if m.opts.ReadOnly && k != "y" {
			m.clearPending()
			return nil, Event{}
		}
		m.operator, m.operatorCnt, m.operatorAt = k[0], count, position{m.row, m.col}
		m.count = 0
		m.pending += k
		return nil, Event{}
	case "D", "C", "Y":
		if m.opts.ReadOnly && k != "Y" {
			break
		}
		return m.lineTail(key, k[0], count)
	case "x", "X", "s", "S":
		if m.opts.ReadOnly {
			break
		}
		m.count = 0
		return m.simpleDelete(key, k, count)
	case "r":
		if !m.opts.ReadOnly {
			m.replace, m.pending, m.operatorCnt = true, "r", count
			m.count = 0
		}
		return nil, Event{}
	case "p", "P":
		if !m.opts.ReadOnly {
			m.count = 0
			return m.pasteRegister(key, k == "P", count)
		}
	case "J":
		if !m.opts.ReadOnly && m.row+1 < len(m.lines) {
			m.count = 0
			before := m.snap()
			joinCount := max(2, count)
			m.joinLines(m.row, min(len(m.lines)-1, m.row+joinCount-1))
			before.redoPos = before.pos
			before.hasRedoPos = true
			m.pushUndo(before)
			m.redo = nil
			m.lastChange = countedKeys(joinCount, 'J')
			return nil, Event{Kind: Changed}
		}
	case "~":
		if !m.opts.ReadOnly {
			return m.toggleCase(key, count)
		}
	case ">":
		if !m.opts.ReadOnly {
			m.operator, m.operatorCnt, m.pending = '>', count, ">"
			m.count = 0
			return nil, Event{}
		}
	case "<":
		if !m.opts.ReadOnly {
			m.operator, m.operatorCnt, m.pending = '<', count, "<"
			m.count = 0
			return nil, Event{}
		}
	case "u":
		if !m.opts.ReadOnly && m.undoChange() {
			return nil, Event{Kind: Changed}
		}
	case "ctrl+r":
		if !m.opts.ReadOnly && m.redoChange() {
			return nil, Event{Kind: Changed}
		}
	case ".":
		if !m.opts.ReadOnly {
			return m.repeatLast()
		}
	case "/", "?":
		m.searching, m.searchDir, m.searchText = true, map[bool]int{true: -1, false: 1}[k == "?"], ""
	case "n", "N":
		dir := m.lastDir
		if k == "N" {
			dir = -dir
		}
		for range count {
			m.search(m.lastSearch, dir)
		}
	case "*":
		if word := m.wordUnderCursor(); word != "" {
			m.lastSearch, m.lastDir = word, 1
			m.searchWholeWord(word, 1)
		}
	case "i", "a", "I", "A", "o", "O":
		if !m.opts.ReadOnly {
			m.count = 0
			return m.enterInsert(key, k, count)
		}
	case "v":
		m.mode, m.anchor = Visual, position{m.row, m.col}
		m.visualLineStart, m.visualObjectFailed = false, false
	case "V":
		m.mode, m.anchor = VisualLine, position{m.row, 0}
		m.visualLineStart, m.visualObjectFailed = true, false
	default:
		m.clearPending()
		return nil, Event{Kind: Unhandled, Key: key}
	}
	m.clearPending()
	m.ensureVisible()
	return nil, Event{}
}

func (m *Model) finishOperator(key tea.KeyPressMsg) (tea.Cmd, Event) {
	k, op, count := key.String(), m.operator, max(1, m.operatorCnt)*max(1, m.count)
	if m.operatorG {
		if k == "e" || k == "E" {
			before := m.snap()
			start := m.operatorAt
			for range count {
				m.wordEndBackward(k == "E")
			}
			end := position{m.row, m.col}
			m.clearPending()
			text := m.rangeText(start, end, true)
			m.reg, m.regLine = text, false
			keys := append(countedKeys(count, op), runeKey('g'), key)
			if op == 'y' {
				first, _ := ordered(start, end)
				m.SetCursor(first.row, first.col)
				return m.clipboard(text), Event{}
			}
			first, _ := ordered(start, end)
			before.pos = first
			m.deleteRange(start, end, true)
			if op == 'c' {
				m.mode = Insert
				m.changeStart = &before
				m.changeKeys = keys
			} else {
				m.pushUndo(before)
				m.redo = nil
				m.lastChange = keys
				m.clampNormalCursor()
			}
			m.rehighlight()
			return m.clipboard(text), Event{Kind: Changed}
		}
		if k != "g" {
			m.clearPending()
			return nil, Event{Kind: Seq, Seq: "g" + keyText(key)}
		}
		before := m.snap()
		from := m.operatorAt.row
		m.clearPending()
		keys := append(countedKeys(count, op), runeKey('g'), key)
		return m.applyLineOperator(op, from, min(count-1, len(m.lines)-1), before, keys)
	}
	if k == "g" {
		m.operatorG = true
		m.pending += "g"
		return nil, Event{}
	}
	if k == "f" || k == "t" || k == "F" || k == "T" {
		m.findKind = k[0]
		m.pending += k
		return nil, Event{}
	}
	if k == ";" || k == "," {
		before := m.snap()
		start := m.operatorAt
		if !m.repeatFind(k == ",", count) {
			m.clearPending()
			return nil, Event{}
		}
		end := position{m.row, m.col}
		forward := less(start, end)
		m.clearPending()
		inclusive := forward
		text := m.rangeText(start, end, inclusive)
		m.reg, m.regLine = text, false
		if op == 'y' {
			m.SetCursor(min(start.row, end.row), min(start.col, end.col))
			return m.clipboard(text), Event{}
		}
		m.deleteRange(start, end, inclusive)
		if op == 'c' {
			m.mode = Insert
			m.changeStart = &before
			m.changeKeys = append(countedKeys(count, op), key)
		} else {
			m.pushUndo(before)
			m.redo = nil
			m.lastChange = append(countedKeys(count, op), key)
			m.clampNormalCursor()
		}
		m.rehighlight()
		return m.clipboard(text), Event{Kind: Changed}
	}
	if k == "/" || k == "?" {
		m.searching, m.searchText = true, ""
		if k == "?" {
			m.searchDir = -1
		} else {
			m.searchDir = 1
		}
		m.pending += k
		return nil, Event{}
	}
	if k == "n" || k == "N" {
		before := m.snap()
		start := m.operatorAt
		dir := m.lastDir
		if k == "N" {
			dir = -dir
		}
		found := false
		for range count {
			found = m.search(m.lastSearch, dir)
			if !found {
				break
			}
		}
		if !found {
			m.clearPending()
			return nil, Event{}
		}
		end := position{m.row, m.col}
		m.clearPending()
		text := m.rangeText(start, end, false)
		m.reg, m.regLine = text, false
		if op == 'y' {
			m.SetCursor(start.row, start.col)
			return m.clipboard(text), Event{}
		}
		m.deleteRange(start, end, false)
		if op == 'c' {
			m.mode = Insert
			m.insertStart = position{m.row, m.col}
			m.changeStart = &before
			m.changeKeys = append(countedKeys(count, op), key)
		} else {
			m.pushUndo(before)
			m.redo = nil
			m.lastChange = append(countedKeys(count, op), key)
			m.clampNormalCursor()
		}
		m.rehighlight()
		return m.clipboard(text), Event{Kind: Changed}
	}
	if k == "i" || k == "a" {
		m.textObject = k[0]
		m.pending += k
		return nil, Event{}
	}
	if k == string(op) {
		before := m.snap()
		m.clearPending()
		if m.row+count > len(m.lines) {
			if op == '>' || op == '<' {
				if m.row == len(m.lines)-1 && count > 1 {
					return nil, Event{}
				}
				return m.applyLineOperator(op, m.row, len(m.lines)-1, before, append(countedKeys(count, op), key))
			}
			return nil, Event{}
		}
		end := m.row + count - 1
		return m.applyLineOperator(op, m.row, end, before, append(countedKeys(count, op), key))
	}
	start := position{m.row, m.col}
	before := m.snap()
	lastWordMoved := true
	lastWordExhausted := false
	motionOK := true
	if k == "w" || k == "W" {
		for range count {
			previous := position{m.row, m.col}
			m.wordForward(k == "W", false)
			lastWordMoved = position{m.row, m.col} != previous
			end := position{m.row, m.col}
			lastWordExhausted = previous.row == end.row && wordRunBetween(m.lines[end.row], previous.col, end.col, k == "W")
		}
	} else {
		motionOK = m.applyMotion(k, count)
	}
	if !motionOK {
		m.clearPending()
		return nil, Event{Kind: Unhandled, Key: key}
	}
	end := position{m.row, m.col}
	if op == '>' || op == '<' {
		m.clearPending()
		return m.applyLineOperator(op, start.row, end.row, before, append(countedKeys(count, op), key))
	}
	if k == "j" || k == "down" || k == "k" || k == "up" || k == "G" {
		if (k == "j" || k == "down" || k == "k" || k == "up") && start.row == end.row {
			m.clearPending()
			return nil, Event{}
		}
		m.clearPending()
		return m.applyLineOperator(op, start.row, end.row, before, append(countedKeys(count, op), key))
	}
	inclusive := k == "e" || k == "E" || k == "$" || k == "%"
	if k == "w" || k == "W" {
		if end.row > start.row && end.col == 0 {
			end = position{end.row - 1, m.lineLen(end.row - 1)}
		} else if count == 1 && end.row > start.row {
			end = position{start.row, m.lineLen(start.row)}
		} else if !lastWordMoved || lastWordExhausted {
			end.col = m.lineLen(end.row)
		}
		if op == 'c' && start.col < m.lineLen(start.row) && !isSpaceCluster(clusters(m.lines[start.row])[start.col]) {
			m.row, m.col = start.row, start.col
			cs := clusters(m.lines[m.row])
			for m.col+1 < len(cs) && sameWordClass(cs[m.col], cs[m.col+1], k == "W") {
				m.col++
			}
			for range count - 1 {
				m.wordEndForward(k == "W")
			}
			end = position{m.row, m.col}
			inclusive = true
		}
	}
	if k == "l" || k == "right" {
		if m.lineLen(start.row) == 0 {
			m.clearPending()
			return nil, Event{}
		}
		end = position{start.row, min(m.lineLen(start.row)-1, start.col+count-1)}
		inclusive = true
	}
	m.clearPending()
	if op == 'y' {
		text := m.rangeText(start, end, inclusive)
		m.reg, m.regLine = text, false
		yankStart, _ := ordered(start, end)
		m.SetCursor(yankStart.row, yankStart.col)
		return m.clipboard(text), Event{}
	}
	undoStart, _ := ordered(start, end)
	before.pos = undoStart
	text := m.deleteRange(start, end, inclusive)
	m.reg, m.regLine = text, false
	if op == 'c' {
		m.mode = Insert
		m.changeStart = &before
		m.changeKeys = append(countedKeys(count, op), key)
	} else {
		m.pushUndo(before)
		m.redo = nil
		m.lastChange = append(countedKeys(count, op), key)
		m.clampNormalCursor()
		if (k == "b" || k == "B") && end.row < start.row && start.col == 0 {
			m.col = firstWordColumn(m.lines[m.row])
		}
	}
	m.rehighlight()
	return m.clipboard(text), Event{Kind: Changed}
}

func wordRunBetween(line string, from, to int, big bool) bool {
	cs := clusters(line)
	if from < 0 || to < from || to >= len(cs) || isSpaceCluster(cs[from]) {
		return false
	}
	for i := from + 1; i <= to; i++ {
		if !sameWordClass(cs[from], cs[i], big) {
			return false
		}
	}
	return true
}

func (m *Model) finishTextObject(key tea.KeyPressMsg) (tea.Cmd, Event) {
	prefix := m.textObject
	inner, op := prefix == 'i', m.operator
	count := max(1, m.operatorCnt) * max(1, m.count)
	object := byte(0)
	if key.Key().Text != "" {
		object = key.Key().Text[0]
	} else if len(key.String()) == 1 {
		object = key.String()[0]
	}
	a, b, ok, empty, linewise := m.textObjectRange(inner, object, count)
	m.clearPending()
	if !ok {
		return nil, Event{}
	}
	if op == '>' || op == '<' {
		before := m.snap()
		m.indentLines(min(a.row, b.row), max(a.row, b.row), op == '>')
		m.pushUndo(before)
		m.redo = nil
		m.lastChange = append(countedKeys(count, op), runeKey(prefix), key)
		m.SetCursor(a.row, before.pos.col)
		m.rehighlight()
		return nil, Event{Kind: Changed}
	}
	if linewise {
		text := m.lineOperatorText(a.row, b.row)
		m.reg, m.regLine = text, true
		if op == 'y' {
			m.SetCursor(a.row, 0)
			return m.clipboard(text), Event{}
		}
		before := m.snap()
		before.pos = a
		if op == 'c' {
			indent := leadingIndent(m.lines[a.row])
			m.lines[a.row] = indent
			m.lines = append(m.lines[:a.row+1], m.lines[b.row+1:]...)
			m.SetCursor(a.row, len(clusters(indent)))
			m.mode = Insert
			m.insertStart = position{m.row, m.col}
			m.changeStart = &before
			m.changeKeys = append(countedKeys(count, op), runeKey(prefix), key)
		} else {
			m.lineRange(a.row, b.row, true)
			m.pushUndo(before)
			m.redo = nil
			m.lastChange = append(countedKeys(count, op), runeKey(prefix), key)
		}
		m.rehighlight()
		return m.clipboard(text), Event{Kind: Changed}
	}
	text := ""
	if !empty {
		text = m.rangeText(a, b, true)
	}
	m.reg, m.regLine = text, false
	if op == 'y' {
		m.SetCursor(a.row, a.col)
		return m.clipboard(text), Event{}
	}
	before := m.snap()
	before.pos = a
	if inner && !empty && a.row == b.row && m.lines[a.row] == "" && a.row > 0 && a.row+1 < len(m.lines) {
		m.lines = append(m.lines[:a.row], m.lines[a.row+1:]...)
		m.SetCursor(min(a.row, len(m.lines)-1), 0)
		text = "\n"
	} else if !empty {
		m.deleteRange(a, b, true)
	} else {
		m.SetCursor(a.row, a.col)
	}
	if op == 'c' {
		m.mode = Insert
		m.changeStart = &before
		m.changeKeys = append(countedKeys(count, op), runeKey(prefix), key)
	} else {
		if !empty {
			m.pushUndo(before)
		}
		m.redo = nil
		m.lastChange = append(countedKeys(count, op), runeKey(prefix), key)
		m.clampNormalCursor()
	}
	m.rehighlight()
	ev := Event{}
	if !empty {
		ev.Kind = Changed
	}
	return m.clipboard(text), ev
}

func (m *Model) textObjectRange(inner bool, object byte, count int) (position, position, bool, bool, bool) {
	return m.objectRange(inner, object, count)
}

func (m *Model) updateVisual(key tea.KeyPressMsg) (tea.Cmd, Event) {
	k := key.String()
	if m.textObject != 0 {
		prefix := m.textObject
		object := byte(0)
		if key.Key().Text != "" {
			object = key.Key().Text[0]
		}
		a, b, ok, _, linewise := m.textObjectRange(prefix == 'i', object, max(1, m.count))
		m.clearPending()
		if ok {
			m.visualObjectFailed = false
			m.anchor, m.row, m.col = a, b.row, b.col
			if linewise {
				m.mode = VisualLine
				m.anchor.col, m.col = 0, 0
			} else {
				m.mode = Visual
			}
			m.ensureVisible()
		} else {
			m.visualObjectFailed = true
		}
		return nil, Event{}
	}
	if m.findKind != 0 {
		kind, count := m.findKind, max(1, m.count)
		m.clearPending()
		if key.Key().Text != "" {
			m.findChar(kind, key.Key().Text, count)
		}
		return nil, Event{}
	}
	if m.gPending {
		m.gPending = false
		switch k {
		case "g":
			want := m.preferred
			if m.count > 0 {
				m.row = min(m.count-1, len(m.lines)-1)
			} else {
				m.row = 0
			}
			m.col = columnAtDisplay(m.lines[m.row], max(0, want), false, m.opts.TabText)
			m.preferred = displayColumn(m.lines[m.row], m.col, m.opts.TabText)
			m.clearPending()
			m.ensureVisible()
			return nil, Event{}
		case "e", "E":
			for range max(1, m.count) {
				m.wordEndBackward(k == "E")
			}
			m.clearPending()
			return nil, Event{}
		default:
			m.clearPending()
			return nil, Event{Kind: Seq, Seq: "g" + keyText(key)}
		}
	}
	if k == "esc" || k == "ctrl+c" {
		m.mode = Normal
		m.visualEOL = false
		m.clearPending()
		return nil, Event{}
	}
	if k == "ctrl+s" || k == "ctrl+enter" || k == "ctrl+p" ||
		k == "ctrl+h" || k == "ctrl+j" || k == "ctrl+k" || k == "ctrl+l" {
		m.clearPending()
		return nil, Event{Kind: Unhandled, Key: key}
	}
	if k == "o" {
		cur := position{m.row, m.col}
		m.row, m.col = m.anchor.row, m.anchor.col
		m.anchor = cur
		return nil, Event{}
	}
	if k == "v" {
		if m.mode == Visual {
			m.mode = Normal
		} else {
			m.mode = Visual
			m.visualLineStart = false
		}
		m.clearPending()
		return nil, Event{}
	}
	if k == "V" {
		if m.mode == VisualLine {
			m.mode = Normal
		} else {
			m.mode = VisualLine
			m.visualLineStart = true
			m.anchor.col = 0
		}
		m.clearPending()
		return nil, Event{}
	}
	if isDigitKey(k) && (k != "0" || m.count > 0) {
		m.count = m.count*10 + int(k[0]-'0')
		return nil, Event{}
	}
	switch k {
	case "i", "a":
		m.textObject = k[0]
		m.pending = k
		return nil, Event{}
	case "g":
		m.gPending, m.pending = true, "g"
		return nil, Event{}
	case "f", "t", "F", "T":
		m.findKind, m.pending = k[0], k
		return nil, Event{}
	case ";", ",":
		m.repeatFind(k == ",", max(1, m.count))
		m.count = 0
		return nil, Event{}
	case "/", "?":
		m.searching = true
		m.searchText = ""
		if k == "?" {
			m.searchDir = -1
		} else {
			m.searchDir = 1
		}
		return nil, Event{}
	case "n", "N":
		dir := m.lastDir
		if k == "N" {
			dir = -dir
		}
		for range max(1, m.count) {
			m.search(m.lastSearch, dir)
		}
		m.count = 0
		return nil, Event{}
	case "*":
		if word := m.wordUnderCursor(); word != "" {
			m.lastSearch, m.lastDir = word, 1
			m.searchWholeWord(word, 1)
		}
		return nil, Event{}
	}
	if m.applyMotion(k, max(1, m.count)) {
		if m.mode == Visual && (k == "w" || k == "W") &&
			m.row == len(m.lines)-1 && m.col == max(0, m.lineLen(m.row)-1) {
			m.col = m.lineLen(m.row)
		}
		m.visualEOL = k == "$" && m.row+1 < len(m.lines)
		m.count = 0
		return nil, Event{}
	}
	if len(k) != 1 || !strings.ContainsRune("dxcy><~J", rune(k[0])) {
		m.clearPending()
		return nil, Event{Kind: Unhandled, Key: key}
	}
	if m.opts.ReadOnly && k != "y" {
		return nil, Event{}
	}
	linewise := m.mode == VisualLine
	a, b := m.anchor, position{m.row, m.col}
	if k == ">" || k == "<" {
		before := m.snap()
		oldValue := m.Value()
		start, _ := ordered(a, b)
		before.pos = start
		m.indentLines(min(a.row, b.row), max(a.row, b.row), k == ">")
		m.pushUndo(before)
		m.redo = nil
		m.lastChange = visualRepeatKeys(linewise, a, b, k)
		m.mode = Normal
		if linewise && !m.visualObjectFailed && m.Value() != oldValue {
			m.SetCursor(min(a.row, b.row), 0)
		} else {
			m.SetCursor(b.row, b.col)
		}
		m.rehighlight()
		return nil, Event{Kind: Changed}
	}
	if k == "~" {
		before := m.snap()
		start, _ := ordered(a, b)
		before.pos = start
		m.toggleRange(a, b, linewise)
		m.pushUndo(before)
		m.redo = nil
		m.mode = Normal
		m.SetCursor(start.row, start.col)
		m.rehighlight()
		return nil, Event{Kind: Changed}
	}
	if k == "J" {
		before := m.snap()
		start, _ := ordered(a, b)
		before.pos = start
		from, to := min(a.row, b.row), max(a.row, b.row)
		if from == to && to+1 < len(m.lines) {
			to++
		}
		m.joinLines(from, to)
		if from == to {
			if linewise || m.visualLineStart {
				m.SetCursor(from, 0)
			} else {
				m.SetCursor(b.row, b.col)
			}
		}
		m.pushUndo(before)
		m.redo = nil
		m.mode = Normal
		m.rehighlight()
		return nil, Event{Kind: Changed}
	}
	var text string
	if linewise {
		before := m.snap()
		start, _ := ordered(a, b)
		if k != "c" {
			before.pos = position{start.row, 0}
		}
		m.mode = Normal
		keys := visualRepeatKeys(true, a, b, k)
		cmd, event := m.applyLineOperator(k[0], a.row, b.row, before, keys)
		if k == "y" || k == "d" || k == "x" {
			m.SetCursor(m.row, 0)
		}
		return cmd, event
	} else {
		inclusive := true
		if m.visualEOL && b.row+1 < len(m.lines) {
			b = position{b.row + 1, 0}
			inclusive = false
		}
		text = m.rangeText(a, b, inclusive)
		if k != "y" {
			before := m.snap()
			start, _ := ordered(a, b)
			before.pos = start
			m.deleteRange(a, b, inclusive)
			if k == "c" {
				m.mode = Insert
				m.changeStart = &before
				m.changeKeys = visualRepeatKeys(false, a, b, k)
			} else {
				m.pushUndo(before)
				m.redo = nil
				m.lastChange = visualRepeatKeys(false, a, b, k)
				m.mode = Normal
				m.clampNormalCursor()
			}
		}
	}
	if k == "y" || k == "d" || k == "x" || k == "c" {
		m.reg, m.regLine = text, linewise
	}
	if k == "y" {
		start, _ := ordered(a, b)
		m.SetCursor(start.row, start.col)
		m.mode = Normal
	} else if k != "c" {
		m.mode = Normal
	}
	m.visualEOL = false
	m.rehighlight()
	if k == "y" || k == "d" || k == "x" || k == "c" {
		ev := Event{}
		if k != "y" {
			ev.Kind = Changed
		}
		return m.clipboard(text), ev
	}
	return nil, Event{Kind: Changed}
}

func (m *Model) updateSearch(key tea.KeyPressMsg) (tea.Cmd, Event) {
	switch key.String() {
	case "esc", "ctrl+c":
		m.searching, m.searchText = false, ""
	case "enter":
		m.searching = false
		pattern := m.searchText
		if pattern == "" {
			pattern = m.lastSearch
		}
		if pattern != "" {
			m.lastSearch, m.lastDir = pattern, m.searchDir
			op, start := m.operator, m.operatorAt
			before := m.snap()
			if m.search(m.lastSearch, m.lastDir) && op != 0 {
				end := position{m.row, m.col}
				if m.searchDir > 0 && less(end, start) {
					start = end
					end = position{end.row, m.lineLen(end.row)}
				}
				before.pos, _ = ordered(start, end)
				m.clearPending()
				text := m.rangeText(start, end, false)
				m.reg, m.regLine = text, false
				if op == 'y' {
					m.SetCursor(min(start.row, end.row), min(start.col, end.col))
					return m.clipboard(text), Event{}
				}
				m.deleteRange(start, end, false)
				if op == 'c' {
					m.mode = Insert
					m.changeStart = &before
				} else {
					m.pushUndo(before)
					m.redo = nil
					m.clampNormalCursor()
				}
				m.rehighlight()
				return m.clipboard(text), Event{Kind: Changed}
			}
		}
	case "backspace":
		if m.searchText != "" {
			_, n := utf8.DecodeLastRuneInString(m.searchText)
			m.searchText = m.searchText[:len(m.searchText)-n]
		}
	default:
		if key.Key().Text != "" && key.Key().Mod&(tea.ModCtrl|tea.ModAlt) == 0 {
			m.searchText += key.Key().Text
		}
	}
	return nil, Event{}
}

func isDigitKey(k string) bool { return len(k) == 1 && k[0] >= '0' && k[0] <= '9' }
func keyText(k tea.KeyPressMsg) string {
	if k.Key().Text != "" {
		return k.Key().Text
	}
	return k.String()
}

func runeKey(r byte) tea.KeyPressMsg {
	return tea.KeyPressMsg(tea.Key{Code: rune(r), Text: string(r)})
}

func visualRepeatKeys(linewise bool, a, b position, action string) []tea.Msg {
	start, end := ordered(a, b)
	keys := []tea.Msg{runeKey('v')}
	if linewise {
		keys[0] = runeKey('V')
	}
	for range end.row - start.row {
		keys = append(keys, runeKey('j'))
	}
	if !linewise {
		for range end.col - start.col {
			keys = append(keys, runeKey('l'))
		}
	}
	if action != "" {
		keys = append(keys, runeKey(action[0]))
	}
	return keys
}

func reverseFind(k byte) byte {
	switch k {
	case 'f':
		return 'F'
	case 'F':
		return 'f'
	case 't':
		return 'T'
	default:
		return 't'
	}
}
