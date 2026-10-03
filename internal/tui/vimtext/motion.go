package vimtext

import "strings"

func (m *Model) applyMotion(key string, count int) bool {
	count = max(1, count)
	switch key {
	case "h", "left":
		m.moveHorizontal(-count)
	case "l", "right":
		m.moveHorizontal(count)
	case "j", "down":
		for range count {
			m.moveVertical(1)
		}
	case "k", "up":
		for range count {
			m.moveVertical(-1)
		}
	case "w", "W":
		for range count {
			m.wordForward(key == "W", false)
		}
	case "e", "E":
		for range count {
			m.wordForward(key == "E", true)
		}
	case "b", "B":
		for range count {
			m.wordBackward(key == "B")
		}
	case "0", "home":
		m.col = 0
	case "^":
		m.firstNonBlank()
	case "$", "end":
		m.col = max(0, m.lineLen(m.row)-1)
	case "G":
		if m.count > 0 {
			m.row = min(m.count-1, len(m.lines)-1)
		} else {
			m.row = len(m.lines) - 1
		}
		m.col = min(m.col, max(0, m.lineLen(m.row)-1))
	case "%":
		m.matchBracket()
	default:
		return false
	}
	m.ensureVisible()
	return true
}

func (m *Model) matchBracket() {
	type bracket struct {
		open, close string
	}
	pairs := []bracket{{"(", ")"}, {"[", "]"}, {"{", "}"}}
	cs := clusters(m.lines[m.row])
	if len(cs) == 0 {
		return
	}
	start := m.col
	for start < len(cs) {
		found := false
		for _, p := range pairs {
			if cs[start] == p.open || cs[start] == p.close {
				found = true
			}
		}
		if found {
			break
		}
		start++
	}
	if start >= len(cs) {
		return
	}
	for _, p := range pairs {
		dir, depth := 0, 0
		switch cs[start] {
		case p.open:
			dir = 1
		case p.close:
			dir = -1
		default:
			continue
		}
		for i := start; i >= 0 && i < len(cs); i += dir {
			switch cs[i] {
			case p.open:
				depth += dir
			case p.close:
				depth -= dir
			}
			if depth == 0 {
				m.col = i
				return
			}
		}
	}
}

func (m *Model) objectRange(inner bool, object byte) (position, position, bool) {
	cs := clusters(m.lines[m.row])
	if len(cs) == 0 {
		return position{}, position{}, false
	}
	if object == 'w' || object == 'W' {
		big := object == 'W'
		start := min(m.col, len(cs)-1)
		if isSpaceCluster(cs[start]) {
			for start < len(cs) && isSpaceCluster(cs[start]) {
				start++
			}
			if start == len(cs) {
				return position{}, position{}, false
			}
		}
		end := start
		for start > 0 && sameWordClass(cs[start], cs[start-1], big) {
			start--
		}
		for end+1 < len(cs) && sameWordClass(cs[end], cs[end+1], big) {
			end++
		}
		if !inner {
			if end+1 < len(cs) {
				for end+1 < len(cs) && isSpaceCluster(cs[end+1]) {
					end++
				}
			} else {
				for start > 0 && isSpaceCluster(cs[start-1]) {
					start--
				}
			}
		}
		return position{m.row, start}, position{m.row, end}, true
	}
	open, close := "", ""
	switch object {
	case '"', '\'', '`':
		open, close = string(object), string(object)
	case '(', 'b':
		open, close = "(", ")"
	case '{', 'B':
		open, close = "{", "}"
	case '[':
		open, close = "[", "]"
	default:
		return position{}, position{}, false
	}
	left := -1
	depth := 0
	for i := min(m.col, len(cs)-1); i >= 0; i-- {
		if open != close && cs[i] == close {
			depth++
		}
		if cs[i] == open {
			if depth == 0 {
				left = i
				break
			}
			depth--
		}
	}
	if left < 0 {
		return position{}, position{}, false
	}
	right := -1
	depth = 0
	for i := left + 1; i < len(cs); i++ {
		if open != close && cs[i] == open {
			depth++
		}
		if cs[i] == close {
			if depth == 0 {
				right = i
				break
			}
			depth--
		}
	}
	if right < 0 {
		return position{}, position{}, false
	}
	if inner {
		left++
		right--
	}
	if right < left {
		right = left - 1
	}
	return position{m.row, left}, position{m.row, right}, true
}

func (m *Model) search(pattern string, dir int) bool {
	if pattern == "" {
		return false
	}
	startRow, startCol := m.row, m.col
	for step := 0; step < len(m.lines); step++ {
		row := (startRow + dir*step + len(m.lines)*2) % len(m.lines)
		line := m.lines[row]
		if dir > 0 {
			from := 0
			if step == 0 {
				from = byteAt(line, startCol+1)
			}
			if i := strings.Index(line[from:], pattern); i >= 0 {
				m.row, m.col = row, graphemeAtByte(line, from+i)
				m.ensureVisible()
				return true
			}
		} else {
			to := len(line)
			if step == 0 {
				to = byteAt(line, startCol)
			}
			if i := strings.LastIndex(line[:to], pattern); i >= 0 {
				m.row, m.col = row, graphemeAtByte(line, i)
				m.ensureVisible()
				return true
			}
		}
	}
	return false
}

func byteAt(s string, col int) int {
	cs := clusters(s)
	col = min(max(0, col), len(cs))
	return len(joinClusters(cs[:col]))
}

func graphemeAtByte(s string, off int) int {
	if off <= 0 {
		return 0
	}
	n, used := 0, 0
	for _, c := range clusters(s) {
		if used >= off {
			break
		}
		used += len(c)
		n++
	}
	return n
}

func (m *Model) wordUnderCursor() string {
	cs := clusters(m.lines[m.row])
	if len(cs) == 0 {
		return ""
	}
	i := min(m.col, len(cs)-1)
	if !isSmallWord(cs[i]) {
		return ""
	}
	a, b := i, i
	for a > 0 && isSmallWord(cs[a-1]) {
		a--
	}
	for b+1 < len(cs) && isSmallWord(cs[b+1]) {
		b++
	}
	return joinClusters(cs[a : b+1])
}
