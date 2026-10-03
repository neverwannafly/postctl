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
		case "esc":
			m.completions = nil
			return nil, Event{}
		}
	}
	switch k {
	case "esc", "ctrl+c":
		m.finishChange()
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
		return m.insertKey(key, "\n")
	case "tab":
		if m.opts.SingleLine {
			return nil, Event{Kind: Unhandled, Key: key}
		}
		return m.insertKey(key, m.opts.TabText)
	case "left":
		m.moveHorizontal(-1)
	case "right":
		m.moveHorizontal(1)
	case "up":
		m.moveVertical(-1)
	case "down":
		m.moveVertical(1)
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
		start := position{m.row, m.col}
		m.wordBackward(false)
		if less(position{m.row, m.col}, start) {
			m.deleteRange(position{m.row, m.col}, start, false)
			m.recordChange(key)
			m.afterEdit()
			return nil, Event{Kind: Changed}
		}
	case "ctrl+u":
		if m.col > 0 {
			m.beginChange(key)
			m.deleteRange(position{m.row, 0}, position{m.row, m.col}, false)
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
	if k == "ctrl+s" || k == "ctrl+enter" || k == "ctrl+p" || k == "ctrl+c" ||
		k == "ctrl+h" || k == "ctrl+j" || k == "ctrl+k" || k == "ctrl+l" ||
		k == "ctrl+o" || k == "ctrl+i" || k == "tab" || k == "shift+tab" ||
		k == " " || k == ":" || k == "q" {
		m.clearPending()
		return nil, Event{Kind: Unhandled, Key: key}
	}
	if m.replace {
		m.replace = false
		m.pending = ""
		if m.opts.ReadOnly || key.Key().Text == "" {
			return nil, Event{}
		}
		m.beginChange(key)
		cs := clusters(m.lines[m.row])
		if m.col < len(cs) {
			cs[m.col] = key.Key().Text
			m.lines[m.row] = joinClusters(cs)
			m.finishChange()
			return nil, Event{Kind: Changed}
		}
		return nil, Event{}
	}
	if m.findKind != 0 {
		kind, count, op, at := m.findKind, max(1, m.operatorCnt)*max(1, m.count), m.operator, m.operatorAt
		before := m.snap()
		m.clearPending()
		if key.Key().Text != "" {
			m.findChar(kind, key.Key().Text, count)
			if op != 0 {
				end := position{m.row, m.col}
				text := m.rangeText(at, end, true)
				m.reg, m.regLine = text, false
				if op == 'y' {
					m.row, m.col = at.row, at.col
					return m.clipboard(text), Event{}
				}
				m.deleteRange(at, end, true)
				if op == 'c' {
					m.mode = Insert
					m.changeStart = &before
					m.changeKeys = []tea.Msg{runeKey(op), runeKey(kind), key}
				} else {
					m.undo = append(m.undo, before)
					m.redo = nil
					m.lastChange = []tea.Msg{runeKey(op), runeKey(kind), key}
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
	if m.gPending {
		m.gPending = false
		switch k {
		case "g":
			if m.count > 0 {
				m.row = min(m.count-1, len(m.lines)-1)
			} else {
				m.row = 0
			}
			m.col = 0
			m.clearPending()
			m.ensureVisible()
			return nil, Event{}
		case "e", "E":
			for range max(1, m.count) {
				m.wordBackward(k == "E")
				m.wordForward(k == "E", true)
			}
			m.clearPending()
			return nil, Event{}
		default:
			m.clearPending()
			return nil, Event{Kind: Seq, Seq: "g" + keyText(key)}
		}
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
		if m.lastFind != 0 {
			kind := m.lastFind
			if k == "," {
				kind = reverseFind(kind)
			}
			m.findChar(kind, m.lastFindKey, count)
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
		return m.lineTail(key, k[0])
	case "x", "X", "s", "S":
		if m.opts.ReadOnly {
			break
		}
		return m.simpleDelete(key, k)
	case "r":
		if !m.opts.ReadOnly {
			m.replace, m.pending = true, "r"
		}
		return nil, Event{}
	case "p", "P":
		if !m.opts.ReadOnly {
			return m.pasteRegister(key, k == "P")
		}
	case "J":
		if !m.opts.ReadOnly && m.row+1 < len(m.lines) {
			m.beginChange(key)
			m.lines[m.row] = strings.TrimRight(m.lines[m.row], " \t") + " " + strings.TrimLeft(m.lines[m.row+1], " \t")
			m.lines = append(m.lines[:m.row+1], m.lines[m.row+2:]...)
			m.finishChange()
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
		m.search(m.lastSearch, dir)
	case "*":
		if word := m.wordUnderCursor(); word != "" {
			m.lastSearch, m.lastDir = word, 1
			m.search(word, 1)
		}
	case "i", "a", "I", "A", "o", "O":
		if !m.opts.ReadOnly {
			return m.enterInsert(key, k)
		}
	case "v":
		m.mode, m.anchor = Visual, position{m.row, m.col}
	case "V":
		m.mode, m.anchor = VisualLine, position{m.row, 0}
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
	if k == "f" || k == "t" || k == "F" || k == "T" {
		m.findKind = k[0]
		m.pending += k
		return nil, Event{}
	}
	if k == "i" || k == "a" {
		m.textObject = k[0]
		m.pending += k
		return nil, Event{}
	}
	if k == string(op) {
		before := m.snap()
		m.clearPending()
		end := min(m.row+count-1, len(m.lines)-1)
		if op == '>' || op == '<' {
			m.indentLines(m.row, end, op == '>')
			m.undo = append(m.undo, before)
			m.redo = nil
			m.lastChange = []tea.Msg{runeKey(op), key}
			m.rehighlight()
			return nil, Event{Kind: Changed}
		}
		text := m.lineRange(m.row, end, op != 'y')
		m.reg, m.regLine = text, true
		cmd := m.clipboard(text)
		if op == 'y' {
			return cmd, Event{}
		}
		m.rehighlight()
		if op == 'c' && !m.opts.ReadOnly {
			m.mode = Insert
			m.changeStart = &before
			m.changeKeys = []tea.Msg{runeKey(op), key}
		} else {
			m.undo = append(m.undo, before)
			m.redo = nil
			m.lastChange = []tea.Msg{runeKey(op), key}
		}
		return cmd, Event{Kind: Changed}
	}
	start := position{m.row, m.col}
	before := m.snap()
	if !m.applyMotion(k, count) {
		m.clearPending()
		return nil, Event{Kind: Unhandled, Key: key}
	}
	end := position{m.row, m.col}
	inclusive := k == "e" || k == "E" || k == "$"
	m.clearPending()
	if op == 'y' {
		text := m.rangeText(start, end, inclusive)
		m.reg, m.regLine = text, false
		m.row, m.col = start.row, start.col
		return m.clipboard(text), Event{}
	}
	text := m.deleteRange(start, end, inclusive)
	m.reg, m.regLine = text, false
	if op == 'c' {
		m.mode = Insert
		m.changeStart = &before
		m.changeKeys = []tea.Msg{runeKey(op), key}
	} else {
		m.undo = append(m.undo, before)
		m.redo = nil
		m.lastChange = []tea.Msg{runeKey(op), key}
	}
	m.rehighlight()
	return m.clipboard(text), Event{Kind: Changed}
}

func (m *Model) finishTextObject(key tea.KeyPressMsg) (tea.Cmd, Event) {
	prefix := m.textObject
	inner, op := prefix == 'i', m.operator
	object := byte(0)
	if key.Key().Text != "" {
		object = key.Key().Text[0]
	} else if len(key.String()) == 1 {
		object = key.String()[0]
	}
	a, b, ok := m.textObjectRange(inner, object)
	m.clearPending()
	if !ok {
		return nil, Event{}
	}
	text := m.rangeText(a, b, true)
	m.reg, m.regLine = text, false
	if op == 'y' {
		return m.clipboard(text), Event{}
	}
	before := m.snap()
	m.deleteRange(a, b, true)
	if op == 'c' {
		m.mode = Insert
		m.changeStart = &before
		m.changeKeys = []tea.Msg{runeKey(op), runeKey(prefix), key}
	} else {
		m.undo = append(m.undo, before)
		m.redo = nil
		m.lastChange = []tea.Msg{runeKey(op), runeKey(prefix), key}
	}
	m.rehighlight()
	return m.clipboard(text), Event{Kind: Changed}
}

func (m *Model) textObjectRange(inner bool, object byte) (position, position, bool) {
	return m.objectRange(inner, object)
}

func (m *Model) updateVisual(key tea.KeyPressMsg) (tea.Cmd, Event) {
	k := key.String()
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
			if m.count > 0 {
				m.row = min(m.count-1, len(m.lines)-1)
			} else {
				m.row = 0
			}
			m.col = 0
			m.clearPending()
			m.ensureVisible()
			return nil, Event{}
		case "e", "E":
			for range max(1, m.count) {
				m.wordBackward(k == "E")
				m.wordForward(k == "E", true)
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
	if isDigitKey(k) {
		m.count = m.count*10 + int(k[0]-'0')
		return nil, Event{}
	}
	switch k {
	case "g":
		m.gPending, m.pending = true, "g"
		return nil, Event{}
	case "f", "t", "F", "T":
		m.findKind, m.pending = k[0], k
		return nil, Event{}
	case ";", ",":
		if m.lastFind != 0 {
			kind := m.lastFind
			if k == "," {
				kind = reverseFind(kind)
			}
			m.findChar(kind, m.lastFindKey, max(1, m.count))
		}
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
		m.search(m.lastSearch, dir)
		return nil, Event{}
	case "*":
		if word := m.wordUnderCursor(); word != "" {
			m.lastSearch, m.lastDir = word, 1
			m.search(word, 1)
		}
		return nil, Event{}
	}
	if m.applyMotion(k, max(1, m.count)) {
		m.count = 0
		return nil, Event{}
	}
	if len(k) != 1 || !strings.ContainsRune("dxcy><~J", rune(k[0])) {
		return nil, Event{Kind: Unhandled, Key: key}
	}
	if m.opts.ReadOnly && k != "y" {
		return nil, Event{}
	}
	linewise := m.mode == VisualLine
	a, b := m.anchor, position{m.row, m.col}
	if k == ">" || k == "<" {
		before := m.snap()
		m.indentLines(min(a.row, b.row), max(a.row, b.row), k == ">")
		m.undo = append(m.undo, before)
		m.redo = nil
		m.mode = Normal
		m.rehighlight()
		return nil, Event{Kind: Changed}
	}
	if k == "~" {
		before := m.snap()
		m.toggleRange(a, b, linewise)
		m.undo = append(m.undo, before)
		m.redo = nil
		m.mode = Normal
		m.rehighlight()
		return nil, Event{Kind: Changed}
	}
	if k == "J" {
		before := m.snap()
		m.joinLines(min(a.row, b.row), max(a.row, b.row))
		m.undo = append(m.undo, before)
		m.redo = nil
		m.mode = Normal
		m.rehighlight()
		return nil, Event{Kind: Changed}
	}
	var text string
	if linewise {
		text = m.lineRange(a.row, b.row, k != "y")
	} else {
		text = m.rangeText(a, b, true)
		if k != "y" {
			before := m.snap()
			m.deleteRange(a, b, true)
			m.undo = append(m.undo, before)
			m.redo = nil
		}
	}
	if k == "y" || k == "d" || k == "x" || k == "c" {
		m.reg, m.regLine = text, linewise
	}
	m.mode = Normal
	if k == "c" && !m.opts.ReadOnly {
		m.mode = Insert
	}
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
		if m.searchText != "" {
			m.lastSearch, m.lastDir = m.searchText, m.searchDir
			m.search(m.lastSearch, m.lastDir)
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
