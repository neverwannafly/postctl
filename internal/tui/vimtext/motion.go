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
		m.preferred = 0
	case "^":
		m.firstNonBlank()
	case "$", "end":
		m.col = max(0, m.lineLen(m.row)-1)
		m.preferred = -1
	case "G":
		want := m.preferred
		if m.count > 0 {
			m.row = min(m.count-1, len(m.lines)-1)
		} else {
			m.row = len(m.lines) - 1
		}
		m.col = columnAtDisplay(m.lines[m.row], max(0, want), false, m.opts.TabText)
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
	cells := m.bufferCells()
	start := -1
	for i, cell := range cells {
		if cell.pos.row != m.row || cell.pos.col < m.col || cell.text == "\n" {
			continue
		}
		found := false
		for _, p := range pairs {
			if cell.text == p.open || cell.text == p.close {
				found = true
			}
		}
		if found {
			start = i
			break
		}
	}
	if start < 0 {
		return
	}
	for _, p := range pairs {
		dir, depth := 0, 0
		switch cells[start].text {
		case p.open:
			dir = 1
		case p.close:
			dir = -1
		default:
			continue
		}
		for i := start; i >= 0 && i < len(cells); i += dir {
			switch cells[i].text {
			case p.open:
				depth += dir
			case p.close:
				depth -= dir
			}
			if depth == 0 {
				m.setMotionPosition(cells[i].pos.row, cells[i].pos.col)
				return
			}
		}
	}
}

type bufferCell struct {
	text string
	pos  position
}

func (m *Model) bufferCells() []bufferCell {
	var out []bufferCell
	for row, line := range m.lines {
		for col, text := range clusters(line) {
			out = append(out, bufferCell{text: text, pos: position{row, col}})
		}
		if row+1 < len(m.lines) {
			out = append(out, bufferCell{text: "\n", pos: position{row, m.lineLen(row)}})
		}
	}
	return out
}

func (m *Model) objectRange(inner bool, object byte, count int) (position, position, bool, bool, bool) {
	cs := clusters(m.lines[m.row])
	if len(cs) == 0 {
		if object != '(' && object != ')' && object != 'b' && object != '{' && object != '}' &&
			object != 'B' && object != '[' && object != ']' {
			return position{}, position{}, false, false, false
		}
	}
	if object == 'w' || object == 'W' {
		big := object == 'W'
		start := min(m.col, len(cs)-1)
		if isSpaceCluster(cs[start]) {
			end := start
			for start > 0 && isSpaceCluster(cs[start-1]) {
				start--
			}
			for end+1 < len(cs) && isSpaceCluster(cs[end+1]) {
				end++
			}
			if !inner && end+1 < len(cs) {
				end++
				for end+1 < len(cs) && sameWordClass(cs[end], cs[end+1], big) {
					end++
				}
			}
			return position{m.row, start}, position{m.row, end}, true, false, false
		}
		end := start
		for start > 0 && sameWordClass(cs[start], cs[start-1], big) {
			start--
		}
		for end+1 < len(cs) && sameWordClass(cs[end], cs[end+1], big) {
			end++
		}
		for range max(1, count) - 1 {
			next := end + 1
			for next < len(cs) && isSpaceCluster(cs[next]) {
				next++
			}
			if next >= len(cs) {
				break
			}
			for end = next; end+1 < len(cs) && sameWordClass(cs[end], cs[end+1], big); end++ {
			}
		}
		if !inner {
			if end+1 < len(cs) && isSpaceCluster(cs[end+1]) {
				for end+1 < len(cs) && isSpaceCluster(cs[end+1]) {
					end++
				}
			} else {
				leading := start
				for leading > 0 && isSpaceCluster(cs[leading-1]) {
					leading--
				}
				if leading > 0 {
					start = leading
				}
			}
		}
		return position{m.row, start}, position{m.row, end}, true, false, false
	}
	open, close := "", ""
	switch object {
	case '"', '\'', '`':
		open, close = string(object), string(object)
	case '(', ')', 'b':
		open, close = "(", ")"
	case '{', '}', 'B':
		open, close = "{", "}"
	case '[', ']':
		open, close = "[", "]"
	default:
		return position{}, position{}, false, false, false
	}
	if open == close {
		left, right := -1, -1
		for i := 0; i < len(cs); {
			if cs[i] != open || quoteInsideWord(cs, i, object) {
				i++
				continue
			}
			j := i + 1
			for j < len(cs) && (cs[j] != close || quoteInsideWord(cs, j, object)) {
				j++
			}
			if j >= len(cs) {
				break
			}
			if m.col >= i && m.col <= j || i >= m.col {
				left, right = i, j
				break
			}
			i = j + 1
		}
		if left < 0 {
			return position{}, position{}, false, false, false
		}
		if inner {
			return position{m.row, left + 1}, position{m.row, right - 1}, true, right == left+1, false
		}
		end := right
		for end+1 < len(cs) && isSpaceCluster(cs[end+1]) {
			end++
		}
		if end == right {
			for left > 0 && isSpaceCluster(cs[left-1]) {
				left--
			}
		}
		return position{m.row, left}, position{m.row, end}, true, false, false
	}
	cells := m.bufferCells()
	cursor := -1
	for i, cell := range cells {
		if cell.pos == (position{m.row, m.col}) {
			cursor = i
			break
		}
	}
	if cursor < 0 {
		return position{}, position{}, false, false, false
	}
	left, right := -1, -1
	searchAt := cursor
	if cells[cursor].text == close {
		searchAt--
	}
	for range max(1, count) {
		depth := 0
		left = -1
	leftScan:
		for i := searchAt; i >= 0; i-- {
			switch cells[i].text {
			case close:
				depth++
			case open:
				if depth == 0 {
					left = i
					break leftScan
				}
				depth--
			}
		}
		if left < 0 {
			for i := cursor + 1; i < len(cells); i++ {
				if cells[i].text == open {
					left = i
					break
				}
			}
			if left < 0 {
				return position{}, position{}, false, false, false
			}
		}
		depth = 0
		right = -1
	rightScan:
		for i := left + 1; i < len(cells); i++ {
			switch cells[i].text {
			case open:
				depth++
			case close:
				if depth == 0 {
					right = i
					break rightScan
				}
				depth--
			}
		}
		if right < 0 || cursor > right {
			return position{}, position{}, false, false, false
		}
		searchAt = left - 1
	}
	a, b := cells[left].pos, cells[right].pos
	if inner {
		if a.row != b.row && b.row-a.row > 1 {
			innerRow := a.row + 1
			startCol := firstWordColumn(m.lines[innerRow])
			endRow := b.row - 1
			return position{innerRow, startCol}, position{endRow, m.lineLen(endRow)}, true, false, true
		}
		a = nextPosition(m, a)
		b = prevPosition(m, b)
	}
	empty := less(b, a)
	return a, b, true, empty, false
}

func quoteInsideWord(cs []string, i int, quote byte) bool {
	return quote == '\'' && i > 0 && i+1 < len(cs) && isSmallWord(cs[i-1]) && isSmallWord(cs[i+1])
}

func nextPosition(m *Model, p position) position {
	if p.col < m.lineLen(p.row) {
		return position{p.row, p.col + 1}
	}
	if p.row+1 < len(m.lines) {
		return position{p.row + 1, 0}
	}
	return p
}

func prevPosition(m *Model, p position) position {
	if p.col > 0 {
		return position{p.row, p.col - 1}
	}
	if p.row > 0 {
		return position{p.row - 1, m.lineLen(p.row - 1)}
	}
	return p
}

func (m *Model) search(pattern string, dir int) bool {
	if pattern == "" {
		return false
	}
	startRow, startCol := m.row, m.col
	for pass := 0; pass < 2; pass++ {
		for step := 0; step < len(m.lines); step++ {
			row := (startRow + dir*step + len(m.lines)*2) % len(m.lines)
			if pass == 1 && row != startRow {
				continue
			}
			line := m.lines[row]
			if dir > 0 {
				from, to := 0, len(line)
				if step == 0 && pass == 0 {
					from = byteAt(line, startCol+1)
				}
				if pass == 1 {
					to = byteAt(line, startCol+1)
				}
				if i := strings.Index(line[from:to], pattern); i >= 0 {
					m.setMotionPosition(row, graphemeAtByte(line, from+i))
					m.ensureVisible()
					return true
				}
			} else {
				from, to := 0, len(line)
				if step == 0 && pass == 0 {
					to = byteAt(line, startCol)
				}
				if pass == 1 {
					from = byteAt(line, startCol)
				}
				if i := strings.LastIndex(line[from:to], pattern); i >= 0 {
					m.setMotionPosition(row, graphemeAtByte(line, from+i))
					m.ensureVisible()
					return true
				}
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
		for i < len(cs) && !isSmallWord(cs[i]) {
			i++
		}
		if i >= len(cs) {
			return ""
		}
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

func (m *Model) searchWholeWord(pattern string, dir int) bool {
	for range len(m.bufferCells()) + 1 {
		if !m.search(pattern, dir) {
			return false
		}
		cs := clusters(m.lines[m.row])
		beforeOK := m.col == 0 || !isSmallWord(cs[m.col-1])
		end := m.col + len(clusters(pattern))
		afterOK := end >= len(cs) || !isSmallWord(cs[end])
		if beforeOK && afterOK {
			return true
		}
	}
	return false
}
