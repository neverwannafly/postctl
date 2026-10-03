package response

import (
	"fmt"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/shubham/postctl/internal/ui"
)

// JSON folding in the Body buffer. A pretty-printed JSON body is indexed into
// nodes (an object or array that spans lines); the vim fold keys close and
// open the node under the cursor:
//
//	za toggle   zo open   zc close   zR open all   zM close all
//
// A closed node is one line, `"items": […] 3 items,` or `{…} 12 keys`. The
// buffer holds the visible text, so motions, search and the highlighter treat
// a folded node as the single line it looks like. A search matches the visible
// text only: it does not look inside a closed node and does not open one. A
// yank copies what is on screen (a folded line yields the summary, never the
// hidden text), which keeps a copy from carrying anything the user did not
// see. The fold keys are the pane's own: the buffer never sees them, and every
// other z key is swallowed (the buffer has no scroll-to-cursor commands).

// region is a JSON node that spans lines: the lines start through end-1.
type region struct {
	end   int // index one past the closing line
	n     int // direct children
	array bool
}

// foldIndex is the pretty-printed lines of a JSON body and its nodes. It is
// built once per body and never changed, so commands may share it.
type foldIndex struct {
	src     []string
	regions map[int]region
	starts  []int // keys of regions, ascending
}

func indexFolds(src []string) *foldIndex {
	ix := &foldIndex{src: src, regions: map[int]region{}}
	var stack []int
	for i, line := range src {
		trim := strings.TrimSpace(line)
		if strings.HasSuffix(trim, "{") || strings.HasSuffix(trim, "[") {
			stack = append(stack, i)
		}
		if (strings.HasPrefix(trim, "}") || strings.HasPrefix(trim, "]")) && len(stack) > 0 {
			start := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			ix.regions[start] = region{end: i + 1, n: countChildren(src, start, i), array: strings.HasSuffix(strings.TrimSpace(src[start]), "[")}
		}
	}
	ix.starts = make([]int, 0, len(ix.regions))
	for s := range ix.regions {
		ix.starts = append(ix.starts, s)
	}
	sort.Ints(ix.starts)
	return ix
}

func indentOf(s string) int { return len(s) - len(strings.TrimLeft(s, " ")) }

// countChildren counts the lines between start and closing that sit one level
// in from start.
func countChildren(src []string, start, closing int) int {
	in, n := indentOf(src[start])+2, 0
	for i := start + 1; i < closing; i++ {
		if indentOf(src[i]) == in && !strings.HasPrefix(strings.TrimSpace(src[i]), "}") && !strings.HasPrefix(strings.TrimSpace(src[i]), "]") {
			n++
		}
	}
	return n
}

// summary is the one line a closed node shows.
func (ix *foldIndex) summary(start int) string {
	r := ix.regions[start]
	open, shut, noun := "{", "}", "key"
	if r.array {
		open, shut, noun = "[", "]", "item"
	}
	head := strings.TrimRight(ix.src[start], " ")
	head = strings.TrimSuffix(head, open)
	tail := ""
	if strings.HasSuffix(strings.TrimSpace(ix.src[r.end-1]), ",") {
		tail = ","
	}
	if r.n != 1 {
		noun += "s"
	}
	return fmt.Sprintf("%s%s…%s %d %s%s", head, open, shut, r.n, noun, tail)
}

// visible returns the buffer lines with the closed nodes collapsed, and for
// each the index of the source line it starts at.
func (ix *foldIndex) visible(closed map[int]bool) (lines []string, vis []int) {
	for i := 0; i < len(ix.src); {
		if r, ok := ix.regions[i]; ok && closed[i] {
			lines, vis = append(lines, ix.summary(i)), append(vis, i)
			i = r.end
			continue
		}
		lines, vis = append(lines, ix.src[i]), append(vis, i)
		i++
	}
	return lines, vis
}

// containing lists the nodes that hold source line s, innermost first.
func (ix *foldIndex) containing(s int) []int {
	var out []int
	for k := sort.SearchInts(ix.starts, s+1) - 1; k >= 0; k-- {
		if st := ix.starts[k]; ix.regions[st].end > s {
			out = append(out, st)
		}
	}
	return out
}

// foldBase is what a set of folds belongs to; a different base starts open.
type foldBase struct {
	st   *storedResult
	mode bodyMode
	path string
}

// foldState is the pane's fold state. closed is replaced, never edited, when
// a fold key changes it, so a command building from it cannot race.
type foldState struct {
	base    foldBase
	ver     int
	closed  map[int]bool
	line    int  // source line the cursor returns to after the rebuild
	col     int  // and its column
	restore bool // the next buffer should put the cursor at line, col
}

func (p *Pane) foldBaseNow() foldBase {
	return foldBase{st: p.current(), mode: p.bodyMode, path: p.pathFilter}
}

// foldKey handles the key after z.
func (p *Pane) foldKey(key string) tea.Cmd {
	v := p.vim
	if v == nil || v.fold == nil || key == "esc" {
		return nil
	}
	row, col := v.f.Cursor()
	if row >= len(v.vis) {
		return nil
	}
	s := v.vis[row]
	closed := map[int]bool{}
	for k := range v.closed {
		closed[k] = true
	}
	switch key {
	case "a":
		n := v.fold.containing(s)
		if len(n) == 0 {
			return nil
		}
		closed[n[0]] = !closed[n[0]]
		if !closed[n[0]] {
			delete(closed, n[0])
		}
	case "o":
		if !closed[s] {
			return nil
		}
		delete(closed, s)
	case "c":
		done := false
		for _, n := range v.fold.containing(s) {
			if !closed[n] {
				closed[n], done = true, true
				break
			}
		}
		if !done {
			return nil
		}
	case "R":
		if len(closed) == 0 {
			return nil
		}
		closed = map[int]bool{}
	case "M":
		for _, st := range v.fold.starts {
			closed[st] = true
		}
	default:
		return nil
	}
	if sameSet(closed, v.closed) {
		return nil
	}
	if p.fold.base != p.foldBaseNow() { // reset in Update; keep the new set on the current base
		p.fold = foldState{base: p.foldBaseNow()}
	}
	p.fold.closed, p.fold.ver = closed, p.fold.ver+1
	p.fold.line, p.fold.col, p.fold.restore = s, col, true
	return p.ensureVim()
}

func sameSet(a, b map[int]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if !b[k] {
			return false
		}
	}
	return true
}

// restoreFoldCursor puts the cursor back on the source line it was on: the
// row that shows it, or the closed node that hides it.
func (p *Pane) restoreFoldCursor(v *bodyVim) {
	if !p.fold.restore || v.fold == nil || v.key.fold != p.fold.ver {
		return
	}
	p.fold.restore = false
	row := sort.Search(len(v.vis), func(i int) bool { return v.vis[i] > p.fold.line }) - 1
	row = max(row, 0)
	col := p.fold.col
	if v.vis[row] != p.fold.line {
		col = 0
	}
	v.f.SetCursor(row, col)
}

// foldPendingHints are the footer hints after z.
func foldPendingHints() []ui.Hint {
	return []ui.Hint{
		{Key: "a", Label: "Toggle fold", Priority: 0},
		{Key: "o c", Label: "Open/close", Priority: 1},
		{Key: "R M", Label: "Open/close all", Priority: 2},
		{Key: "Esc", Label: "Cancel", Priority: 3},
	}
}
