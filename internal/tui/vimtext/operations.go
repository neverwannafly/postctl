package vimtext

import (
	"strings"
	"unicode"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
)

func (m *Model) enterInsert(key tea.KeyPressMsg, kind string) (tea.Cmd, Event) {
	m.beginChange(key)
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
			return m.enterInsert(key, "A")
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

func (m *Model) simpleDelete(key tea.KeyPressMsg, kind string) (tea.Cmd, Event) {
	if kind == "S" {
		before := m.snap()
		text := m.lineRange(m.row, m.row, true)
		m.reg, m.regLine = text, true
		m.mode = Insert
		m.changeStart = &before
		m.changeKeys = []tea.Msg{key}
		return m.clipboard(text), Event{Kind: Changed}
	}
	m.beginChange(key)
	var text string
	if kind == "X" {
		if m.col == 0 {
			m.changeStart = nil
			return nil, Event{}
		}
		cs := clusters(m.lines[m.row])
		text = cs[m.col-1]
		m.col--
		m.lines[m.row] = joinClusters(append(cs[:m.col], cs[m.col+1:]...))
	} else {
		cs := clusters(m.lines[m.row])
		if m.col >= len(cs) {
			m.changeStart = nil
			return nil, Event{}
		}
		text = cs[m.col]
		m.lines[m.row] = joinClusters(append(cs[:m.col], cs[m.col+1:]...))
		m.col = min(m.col, max(0, len(cs)-2))
	}
	m.reg, m.regLine = text, false
	if kind == "s" {
		m.mode = Insert
	} else {
		m.finishChange()
	}
	return m.clipboard(text), Event{Kind: Changed}
}

func (m *Model) pasteRegister(key tea.KeyPressMsg, before bool) (tea.Cmd, Event) {
	if m.reg == "" {
		return nil, Event{}
	}
	m.beginChange(key)
	if m.regLine {
		text := strings.TrimSuffix(m.reg, "\n")
		parts := strings.Split(text, "\n")
		at := m.row + 1
		if before {
			at = m.row
		}
		m.lines = append(m.lines[:at], append(parts, m.lines[at:]...)...)
		m.row, m.col = at, 0
	} else {
		if !before && m.lineLen(m.row) > 0 {
			m.col++
		}
		m.insertText(m.reg)
		m.col = max(0, m.col-1)
	}
	m.finishChange()
	return nil, Event{Kind: Changed}
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
	joined := strings.TrimRight(m.lines[from], " \t")
	for row := from + 1; row <= to; row++ {
		joined += " " + strings.TrimLeft(m.lines[row], " \t")
	}
	m.lines[from] = joined
	m.lines = append(m.lines[:from+1], m.lines[to+1:]...)
	m.SetCursor(from, min(m.col, max(0, m.lineLen(from)-1)))
}
