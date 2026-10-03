package vimtext

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
)

func (m *Model) enterInsert(key tea.KeyPressMsg, kind string, count int) (tea.Cmd, Event) {
	if m.opts.SingleLine && (kind == "o" || kind == "O") {
		return nil, Event{}
	}
	before := m.snap()
	m.changeStart = &before
	m.changeKeys = countedKeys(count, kind[0])
	m.insertCount, m.insertKind, m.insertSession = max(1, count), kind, ""
	switch kind {
	case "a":
		m.col = min(m.col+1, m.lineLen(m.row))
	case "I":
		m.firstNonBlank()
	case "A":
		m.col = m.lineLen(m.row)
	case "o":
		m.lines = append(m.lines[:m.row+1], append([]string{""}, m.lines[m.row+1:]...)...)
		m.row++
		m.col = 0
	case "O":
		m.lines = append(m.lines[:m.row], append([]string{""}, m.lines[m.row:]...)...)
		m.col = 0
	}
	m.mode = Insert
	m.ensureVisible()
	return nil, Event{}
}

func (m *Model) lineTail(key tea.KeyPressMsg, kind byte) (tea.Cmd, Event) {
	if kind == 'Y' {
		text := m.lineRange(m.row, m.row, false)
		m.reg, m.regLine = text, true
		return m.clipboard(text), Event{}
	}
	cs := clusters(m.lines[m.row])
	if len(cs) == 0 || m.col >= len(cs) {
		if kind == 'C' {
			return m.enterInsert(key, "A", 1)
		}
		return nil, Event{}
	}
	text := joinClusters(cs[m.col:])
	m.reg, m.regLine = text, false
	m.beginChange(key)
	m.lines[m.row] = joinClusters(cs[:m.col])
	m.col = min(m.col, max(0, m.lineLen(m.row)-1))
	if kind == 'C' {
		m.col = m.lineLen(m.row)
		m.mode = Insert
	} else {
		m.finishChange()
	}
	return m.clipboard(text), Event{Kind: Changed}
}

func (m *Model) simpleDelete(key tea.KeyPressMsg, kind string, count int) (tea.Cmd, Event) {
	if kind == "S" {
		before := m.snap()
		return m.applyLineOperator('c', m.row, min(len(m.lines)-1, m.row+count-1), before, countedKeys(count, 'S'))
	}
	before := m.snap()
	var text string
	if kind == "X" {
		if m.col == 0 {
			return nil, Event{}
		}
		cs := clusters(m.lines[m.row])
		from := max(0, m.col-count)
		text = joinClusters(cs[from:m.col])
		m.lines[m.row] = joinClusters(append(cs[:from], cs[m.col:]...))
		m.col = from
	} else {
		cs := clusters(m.lines[m.row])
		if m.col >= len(cs) {
			return nil, Event{}
		}
		end := min(len(cs), m.col+count)
		text = joinClusters(cs[m.col:end])
		m.lines[m.row] = joinClusters(append(cs[:m.col], cs[end:]...))
		m.col = min(m.col, max(0, m.lineLen(m.row)-1))
	}
	m.reg, m.regLine = text, false
	if kind == "s" {
		m.mode = Insert
		m.changeStart = &before
		m.changeKeys = countedKeys(count, 's')
		m.insertCount, m.insertKind, m.insertSession = 1, "s", ""
	} else {
		m.pushUndo(before)
		m.redo = nil
		m.lastChange = countedKeys(count, kind[0])
		m.rehighlightRow(m.row)
	}
	return m.clipboard(text), Event{Kind: Changed}
}

func (m *Model) pasteRegister(key tea.KeyPressMsg, before bool, count int) (tea.Cmd, Event) {
	if m.reg == "" {
		return nil, Event{}
	}
	m.beginChange(key)
	for range max(1, count) {
		if m.regLine && !m.opts.SingleLine {
			text := strings.TrimSuffix(m.reg, "\n")
			parts := strings.Split(text, "\n")
			at := m.row + 1
			if before {
				at = m.row
			}
			m.lines = append(m.lines[:at], append(parts, m.lines[at:]...)...)
			m.row, m.col = at, 0
		} else {
			text := strings.TrimSuffix(m.reg, "\n")
			if !before && m.lineLen(m.row) > 0 {
				m.col++
			}
			m.insertText(text)
			m.col = max(0, m.col-1)
		}
	}
	m.finishChange()
	m.lastChange = countedKeys(count, map[bool]byte{true: 'P', false: 'p'}[before])
	return nil, Event{Kind: Changed}
}

func countedKeys(count int, key byte) []tea.Msg {
	var out []tea.Msg
	if count > 1 {
		for _, r := range fmt.Sprint(count) {
			out = append(out, tea.KeyPressMsg(tea.Key{Code: r, Text: string(r)}))
		}
	}
	return append(out, runeKey(key))
}

func (m *Model) toggleCase(key tea.KeyPressMsg, count int) (tea.Cmd, Event) {
	m.beginChange(key)
	cs := clusters(m.lines[m.row])
	changed := false
	for i := m.col; i < len(cs) && i < m.col+count; i++ {
		r, n := utf8.DecodeRuneInString(cs[i])
		switch {
		case unicode.IsLower(r):
			cs[i] = string(unicode.ToUpper(r)) + cs[i][n:]
		case unicode.IsUpper(r):
			cs[i] = string(unicode.ToLower(r)) + cs[i][n:]
		default:
			continue
		}
		changed = true
	}
	if !changed {
		m.changeStart = nil
		return nil, Event{}
	}
	m.lines[m.row] = joinClusters(cs)
	m.col = min(m.col+count, max(0, len(cs)-1))
	m.finishChange()
	return nil, Event{Kind: Changed}
}

func (m *Model) repeatLast() (tea.Cmd, Event) {
	if len(m.lastChange) == 0 {
		return nil, Event{}
	}
	keys := append([]tea.Msg(nil), m.lastChange...)
	m.replaying = true
	var cmds []tea.Cmd
	changed := false
	for _, msg := range keys {
		cmd, ev := m.Update(msg)
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
		changed = changed || ev.Kind == Changed
	}
	if m.mode == Insert {
		_, _ = m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEscape}))
	}
	m.replaying = false
	if changed {
		return tea.Batch(cmds...), Event{Kind: Changed}
	}
	return tea.Batch(cmds...), Event{}
}

func (m *Model) finishCountedInsert() {
	count := max(1, m.insertCount)
	if count <= 1 || m.insertSession == "" {
		m.insertCount, m.insertKind, m.insertSession = 0, "", ""
		return
	}
	switch m.insertKind {
	case "o":
		for range count - 1 {
			m.lines = append(m.lines[:m.row+1], append([]string{m.insertSession}, m.lines[m.row+1:]...)...)
			m.row++
			m.col = len(clusters(m.insertSession))
		}
	case "O":
		for range count - 1 {
			m.lines = append(m.lines[:m.row], append([]string{m.insertSession}, m.lines[m.row:]...)...)
			m.row++
			m.col = len(clusters(m.insertSession))
		}
	default:
		m.insertText(strings.Repeat(m.insertSession, count-1))
	}
	m.insertCount, m.insertKind, m.insertSession = 0, "", ""
}

func (m *Model) afterEdit() {
	m.rehighlightRow(m.row)
	m.refreshCompletion()
	m.ensureVisible()
}

func (m *Model) refreshCompletion() {
	if m.opts.Completer == nil || m.mode != Insert {
		m.completions = nil
		return
	}
	line := m.lines[m.row]
	before := line[:byteAt(line, m.col)]
	m.completions, m.completeAt = m.opts.Completer.Complete(before)
	m.completeAt = min(max(0, m.completeAt), len(before))
	if m.completeSel >= len(m.completions) {
		m.completeSel = 0
	}
}

func (m *Model) acceptCompletion() {
	if len(m.completions) == 0 {
		return
	}
	line := m.lines[m.row]
	at := byteAt(line, m.col)
	start := max(0, at-m.completeAt)
	item := m.completions[m.completeSel]
	m.lines[m.row] = line[:start] + item.Insert + line[at:]
	m.col = graphemeAtByte(m.lines[m.row], start+len(item.Insert))
	m.completions = nil
}

func (m *Model) indentLines(from, to int, right bool) {
	for row := from; row <= to; row++ {
		if right {
			if m.lines[row] == "" {
				continue
			}
			m.lines[row] = m.opts.TabText + m.lines[row]
			continue
		}
		remove := 0
		for remove < len(m.lines[row]) && remove < len(m.opts.TabText) && m.lines[row][remove] == ' ' {
			remove++
		}
		if remove == 0 && strings.HasPrefix(m.lines[row], "\t") {
			remove = 1
		}
		m.lines[row] = m.lines[row][remove:]
	}
}

func (m *Model) lineOperatorText(from, to int) string {
	from, to = min(from, to), max(from, to)
	text := strings.Join(m.lines[from:to+1], "\n")
	if !m.opts.SingleLine {
		text += "\n"
	}
	return text
}

func (m *Model) applyLineOperator(op byte, from, to int, before snapshot, keys []tea.Msg) (tea.Cmd, Event) {
	from, to = min(from, to), max(from, to)
	text := m.lineOperatorText(from, to)
	m.reg, m.regLine = text, !m.opts.SingleLine
	switch op {
	case 'y':
		m.SetCursor(from, firstWordColumn(m.lines[from]))
		return m.clipboard(text), Event{}
	case '>', '<':
		m.indentLines(from, to, op == '>')
		m.pushUndo(before)
		m.redo = nil
		m.lastChange = append([]tea.Msg(nil), keys...)
		m.SetCursor(from, firstWordColumn(m.lines[from]))
		m.rehighlight()
		return nil, Event{Kind: Changed}
	case 'c':
		m.lines[from] = ""
		m.lines = append(m.lines[:from+1], m.lines[to+1:]...)
		m.SetCursor(from, 0)
		m.mode = Insert
		m.changeStart = &before
		m.changeKeys = append([]tea.Msg(nil), keys...)
		m.rehighlight()
		return m.clipboard(text), Event{Kind: Changed}
	default:
		m.lines = append(m.lines[:from], m.lines[to+1:]...)
		if len(m.lines) == 0 {
			m.lines = []string{""}
		}
		m.SetCursor(min(from, len(m.lines)-1), 0)
		m.firstNonBlank()
		m.pushUndo(before)
		m.redo = nil
		m.lastChange = append([]tea.Msg(nil), keys...)
		m.rehighlight()
		return m.clipboard(text), Event{Kind: Changed}
	}
}

func (m *Model) toggleRange(a, b position, linewise bool) {
	a, b = ordered(a, b)
	for row := a.row; row <= b.row; row++ {
		cs := clusters(m.lines[row])
		from, to := 0, len(cs)-1
		if !linewise {
			if row == a.row {
				from = a.col
			}
			if row == b.row {
				to = min(b.col, len(cs)-1)
			}
		}
		for i := from; i <= to && i < len(cs); i++ {
			r, n := utf8.DecodeRuneInString(cs[i])
			if unicode.IsLower(r) {
				cs[i] = string(unicode.ToUpper(r)) + cs[i][n:]
			} else if unicode.IsUpper(r) {
				cs[i] = string(unicode.ToLower(r)) + cs[i][n:]
			}
		}
		m.lines[row] = joinClusters(cs)
	}
}

func (m *Model) joinLines(from, to int) {
	to = min(to, len(m.lines)-1)
	if from >= to {
		return
	}
	joined := m.lines[from]
	joinCol := -1
	for row := from + 1; row <= to; row++ {
		next := strings.TrimLeft(m.lines[row], " \t")
		separator := ""
		if next != "" && !strings.HasPrefix(next, ")") && !strings.HasSuffix(joined, " ") && !strings.HasSuffix(joined, "\t") {
			separator = " "
		}
		joinCol = len(clusters(joined))
		joined += separator + next
	}
	m.lines[from] = joined
	m.lines = append(m.lines[:from+1], m.lines[to+1:]...)
	m.SetCursor(from, min(max(0, joinCol), max(0, m.lineLen(from)-1)))
}
