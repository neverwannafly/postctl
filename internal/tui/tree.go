package tui

import (
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/shubham/postctl/internal/model"
	"github.com/shubham/postctl/internal/store"
	"github.com/shubham/postctl/internal/ui"
)

// rowKind is what a visible tree row represents.
type rowKind int

const (
	rowCollection rowKind = iota
	rowFolder
	rowRequest
	rowProblem
	rowHeading
)

// methodBadge is the text of a request's badge in the tree and palette: the
// HTTP method, or WS for a WebSocket request.
func methodBadge(r *model.Request) string {
	if r.Kind == model.KindWebSocket {
		return "WS"
	}
	return r.Method
}

// methodWidth is the fixed width of the method column in the tree; DELETE is
// shown as DEL.
const methodWidth = 5

// maxFilterResults caps the flat result list while filtering.
const maxFilterResults = 200

// smallWorkspace is the request count up to which folders start unfolded.
const smallWorkspace = 80

// node locates one item in the workspace so edits do not re-search the tree.
type node struct {
	id     model.ID
	coll   *model.Collection
	item   model.Item      // nil for the collection itself
	parent model.Container // nil for the collection itself
	path   []string        // names from the collection down to this node
	anc    []model.ID      // IDs of enclosing containers, outermost first
	kind   rowKind
}

// treeRow is one visible row.
type treeRow struct {
	kind    rowKind
	id      model.ID
	node    *node
	depth   int
	label   string // name; in filter mode the full path
	method  string
	open    bool
	count   int // requests below a container
	problem *store.FileError
	match   []int // fuzzy positions in label (filter mode)
	ctxLen  int   // runes of label that are path context (filter mode)
}

// treeState is the collection tree: workspace index, fold state, selection,
// filter and a render cache. It performs no I/O.
type treeState struct {
	th       ui.Theme
	root     string
	ws       *model.Workspace
	problems []*store.FileError
	nodes    map[model.ID]*node
	counts   map[model.ID]int
	requests int

	expanded map[model.ID]bool
	known    map[model.ID]bool
	dirty    map[model.ID]bool

	rows   []treeRow
	cursor int
	top    int
	sel    model.ID // selected row ID; survives rebuilds and reloads
	w, h   int

	filter    *pickInput
	filtering bool // the filter field has focus (insert or normal)
	query     string

	cache []string
}

func newTree(th ui.Theme) *treeState {
	tv := &treeState{
		expanded: map[model.ID]bool{},
		known:    map[model.ID]bool{},
		dirty:    map[model.ID]bool{},
		nodes:    map[model.ID]*node{},
		counts:   map[model.ID]int{},
	}
	f, _ := newPickInput(th, th.ASCII, "/ ", "filter requests")
	tv.filter = f
	tv.setTheme(th)
	return tv
}

func (tv *treeState) setTheme(th ui.Theme) {
	tv.th = th
	tv.cache = nil
}

// setSize sets the content area.
func (tv *treeState) setSize(w, h int) {
	if w != tv.w {
		tv.cache = nil
	}
	tv.w, tv.h = w, h
	tv.filter.setWidth(max(4, w-3))
	tv.ensureVisible()
}

// setWorkspace installs a (re)loaded workspace, keeping fold state and the
// selection by ID.
func (tv *treeState) setWorkspace(ws *model.Workspace, problems []*store.FileError, root string) {
	tv.ws, tv.problems, tv.root = ws, problems, root
	tv.reindex()
	tv.rebuild()
}

// reindex rebuilds the node index and applies default fold state to
// containers seen for the first time.
func (tv *treeState) reindex() {
	tv.nodes = make(map[model.ID]*node, len(tv.nodes))
	tv.counts = make(map[model.ID]int, len(tv.counts))
	tv.requests = 0
	if tv.ws == nil {
		return
	}
	for _, c := range tv.ws.Collections {
		tv.index(c)
	}
	small := tv.requests <= smallWorkspace
	for id, n := range tv.nodes {
		if n.kind == rowRequest || tv.known[id] {
			continue
		}
		tv.known[id] = true
		tv.expanded[id] = n.kind == rowCollection || small
	}
}

func (tv *treeState) index(c *model.Collection) {
	cn := &node{id: c.ID, coll: c, path: []string{c.Name}, kind: rowCollection}
	tv.nodes[c.ID] = cn
	tv.counts[c.ID] = tv.indexChildren(c, c, cn.path, []model.ID{c.ID})
}

func (tv *treeState) indexChildren(c *model.Collection, parent model.Container, path []string, anc []model.ID) int {
	total := 0
	for _, it := range parent.Children() {
		p := append(append(make([]string, 0, len(path)+1), path...), it.ItemName())
		n := &node{id: it.ItemID(), coll: c, item: it, parent: parent, path: p, anc: anc}
		switch v := it.(type) {
		case *model.Folder:
			n.kind = rowFolder
			tv.nodes[n.id] = n
			cnt := tv.indexChildren(c, v, p, append(append(make([]model.ID, 0, len(anc)+1), anc...), v.ID))
			tv.counts[n.id] = cnt
			total += cnt
		case *model.Request:
			n.kind = rowRequest
			tv.nodes[n.id] = n
			tv.requests++
			total++
		}
	}
	return total
}

// pathLabel joins a path for display: Payments › Charges › Create.
func (tv *treeState) pathLabel(path []string) string {
	sep := " " + tv.th.Glyphs.Arrow + " "
	return strings.Join(path, sep)
}

// rebuild recomputes the visible rows and re-anchors the cursor on the
// selected ID; if that row is gone the cursor stays at its index.
func (tv *treeState) rebuild() {
	tv.cache = nil
	tv.rows = tv.rows[:0]
	if tv.ws != nil {
		if tv.query == "" {
			for _, c := range tv.ws.Collections {
				tv.appendContainer(c, c, 0)
			}
			if len(tv.problems) > 0 {
				tv.rows = append(tv.rows, treeRow{kind: rowHeading, id: "heading:problems", label: "Problems (" + strconv.Itoa(len(tv.problems)) + ")"})
				for _, p := range tv.problems {
					tv.rows = append(tv.rows, treeRow{kind: rowProblem, id: problemID(p), problem: p, label: tv.relPath(p.Path)})
				}
			}
		} else {
			tv.appendMatches()
		}
	}
	tv.anchor()
}

func problemID(p *store.FileError) model.ID { return model.ID("problem:" + p.Path) }

func (tv *treeState) relPath(p string) string {
	if tv.root != "" {
		if rel, err := filepath.Rel(tv.root, p); err == nil && !strings.HasPrefix(rel, "..") {
			return rel
		}
	}
	return p
}

func (tv *treeState) appendContainer(c *model.Collection, parent model.Container, depth int) {
	if depth == 0 {
		tv.rows = append(tv.rows, treeRow{kind: rowCollection, id: c.ID, node: tv.nodes[c.ID], label: c.Name,
			open: tv.expanded[c.ID], count: tv.counts[c.ID]})
		if !tv.expanded[c.ID] {
			return
		}
		depth = 1
	}
	for _, it := range parent.Children() {
		n := tv.nodes[it.ItemID()]
		switch v := it.(type) {
		case *model.Folder:
			open := tv.expanded[v.ID]
			tv.rows = append(tv.rows, treeRow{kind: rowFolder, id: v.ID, node: n, depth: depth, label: v.Name, open: open, count: tv.counts[v.ID]})
			if open {
				tv.appendContainer(c, v, depth+1)
			}
		case *model.Request:
			tv.rows = append(tv.rows, treeRow{kind: rowRequest, id: v.ID, node: n, depth: depth, label: v.Name, method: methodBadge(v)})
		}
	}
}

// appendMatches fills rows with requests matching the query, best first.
func (tv *treeState) appendMatches() {
	var cands []*node
	for _, c := range tv.ws.Collections {
		_ = c.Walk(func(it model.Item, _ model.Container, _ int) error {
			if _, ok := it.(*model.Request); ok {
				cands = append(cands, tv.nodes[it.ItemID()])
			}
			return nil
		})
	}
	labels := make([]string, len(cands))
	res := rankBy(tv.query, len(cands), func(i int) string {
		labels[i] = tv.pathLabel(cands[i].path)
		return labels[i]
	}, maxFilterResults)
	for _, r := range res {
		n := cands[r.index]
		req := n.item.(*model.Request)
		last := []rune(req.Name)
		lbl := []rune(labels[r.index])
		tv.rows = append(tv.rows, treeRow{kind: rowRequest, id: n.id, node: n, label: labels[r.index], method: methodBadge(req),
			match: r.pos, ctxLen: len(lbl) - len(last)})
	}
}

// selectable reports whether the cursor may rest on a row.
func selectable(r treeRow) bool { return r.kind != rowHeading }

// anchor places the cursor on tv.sel, or keeps the index when the ID is gone.
func (tv *treeState) anchor() {
	if len(tv.rows) == 0 {
		tv.cursor, tv.top = 0, 0
		return
	}
	for i, r := range tv.rows {
		if r.id == tv.sel {
			tv.cursor = i
			tv.ensureVisible()
			return
		}
	}
	tv.cursor = min(tv.cursor, len(tv.rows)-1)
	tv.settle(1)
	tv.sel = tv.rows[tv.cursor].id
	tv.ensureVisible()
}

// settle moves the cursor off an unselectable row in direction dir.
func (tv *treeState) settle(dir int) {
	for i := tv.cursor; i >= 0 && i < len(tv.rows); i += dir {
		if selectable(tv.rows[i]) {
			tv.cursor = i
			return
		}
	}
	for i := tv.cursor; i >= 0 && i < len(tv.rows); i -= dir {
		if selectable(tv.rows[i]) {
			tv.cursor = i
			return
		}
	}
}

// viewRows is the number of rows visible below the optional filter line.
func (tv *treeState) viewRows() int {
	n := tv.h
	if tv.filtering || tv.query != "" {
		n--
	}
	return max(1, n)
}

func (tv *treeState) ensureVisible() {
	n := tv.viewRows()
	if tv.cursor < tv.top {
		tv.top = tv.cursor
	}
	if tv.cursor >= tv.top+n {
		tv.top = tv.cursor - n + 1
	}
	tv.top = max(0, min(tv.top, max(0, len(tv.rows)-n)))
}

// current returns the selected row.
func (tv *treeState) current() (treeRow, bool) {
	if tv.cursor < 0 || tv.cursor >= len(tv.rows) {
		return treeRow{}, false
	}
	return tv.rows[tv.cursor], true
}

// scroll shifts tv.top by delta, keeping the selection within the visible range (vim Ctrl-e/Ctrl-y).
func (tv *treeState) scroll(delta int) {
	if len(tv.rows) == 0 {
		return
	}
	n := tv.viewRows()
	maxTop := max(0, len(tv.rows)-n)
	tv.top = max(0, min(tv.top+delta, maxTop))

	bottom := min(len(tv.rows)-1, tv.top+n-1)
	if tv.cursor < tv.top {
		tv.cursor = tv.top
		tv.settle(1)
		if tv.cursor > bottom {
			tv.settle(-1)
		}
		if r, ok := tv.current(); ok {
			tv.sel = r.id
		}
	} else if tv.cursor > bottom {
		tv.cursor = bottom
		tv.settle(-1)
		if tv.cursor < tv.top {
			tv.settle(1)
		}
		if r, ok := tv.current(); ok {
			tv.sel = r.id
		}
	}
}

// rowAt maps y (relative to tree body) to the visible row and row index.
func (tv *treeState) rowAt(y int) (int, treeRow, bool) {
	offset := 0
	if tv.filtering || tv.query != "" {
		offset = 1
	}
	if y < offset {
		return -1, treeRow{}, false
	}
	idx := tv.top + (y - offset)
	if idx < 0 || idx >= len(tv.rows) {
		return -1, treeRow{}, false
	}
	return idx, tv.rows[idx], true
}

// foldGlyphHit reports whether x falls on the fold glyph column for container r.
func (tv *treeState) foldGlyphHit(r treeRow, x int) bool {
	if r.kind != rowCollection && r.kind != rowFolder {
		return false
	}
	indent := min(2*r.depth, max(0, tv.w/3))
	fold := tv.th.Glyphs.Collapsed
	if r.open {
		fold = tv.th.Glyphs.Expanded
	}
	glyphW := ui.Width(fold)
	start := 2 + indent
	end := start + glyphW + 1
	return x >= start && x < end
}

// move shifts the cursor by delta selectable rows.
func (tv *treeState) move(delta int) {
	if len(tv.rows) == 0 {
		return
	}
	dir := 1
	if delta < 0 {
		dir = -1
	}
	for n := 0; n != delta; n += dir {
		next := tv.cursor + dir
		for next >= 0 && next < len(tv.rows) && !selectable(tv.rows[next]) {
			next += dir
		}
		if next < 0 || next >= len(tv.rows) {
			break
		}
		tv.cursor = next
	}
	tv.commit()
}

func (tv *treeState) home() {
	tv.cursor = 0
	tv.settle(1)
	tv.commit()
}

func (tv *treeState) end() {
	tv.cursor = max(0, len(tv.rows)-1)
	tv.settle(-1)
	tv.commit()
}

func (tv *treeState) commit() {
	if r, ok := tv.current(); ok {
		tv.sel = r.id
	}
	tv.ensureVisible()
}

// expand opens a folded container or steps into an open one.
func (tv *treeState) expand() {
	r, ok := tv.current()
	if !ok || (r.kind != rowCollection && r.kind != rowFolder) {
		return
	}
	if !r.open {
		tv.setOpen(r.id, true)
		return
	}
	if tv.cursor+1 < len(tv.rows) && tv.rows[tv.cursor+1].depth > r.depth {
		tv.move(1)
	}
}

// collapse folds an open container, otherwise jumps to the parent row.
func (tv *treeState) collapse() {
	r, ok := tv.current()
	if !ok {
		return
	}
	if (r.kind == rowCollection || r.kind == rowFolder) && r.open {
		tv.setOpen(r.id, false)
		return
	}
	if tv.query != "" || r.depth == 0 {
		return
	}
	for i := tv.cursor - 1; i >= 0; i-- {
		if tv.rows[i].depth < r.depth {
			tv.cursor = i
			tv.commit()
			return
		}
	}
}

func (tv *treeState) toggle() {
	if r, ok := tv.current(); ok && (r.kind == rowCollection || r.kind == rowFolder) {
		tv.setOpen(r.id, !r.open)
	}
}

func (tv *treeState) setOpen(id model.ID, open bool) {
	tv.expanded[id] = open
	tv.rebuild()
}

// reveal selects the node with the given ID, unfolding its ancestors and
// clearing any filter so the row is visible. It reports whether the node
// exists.
func (tv *treeState) reveal(id model.ID) bool {
	if p := tv.problemByID(id); p != nil {
		tv.clearFilter()
		tv.sel = id
		tv.rebuild()
		return true
	}
	n, ok := tv.nodes[id]
	if !ok {
		return false
	}
	tv.clearFilter()
	for _, a := range n.anc {
		tv.expanded[a] = true
	}
	tv.sel = id
	tv.rebuild()
	return true
}

func (tv *treeState) problemByID(id model.ID) *store.FileError {
	for _, p := range tv.problems {
		if problemID(p) == id {
			return p
		}
	}
	return nil
}

// clearFilter drops the filter without touching the selection.
func (tv *treeState) clearFilter() {
	tv.filter.ed.SetValue("")
	tv.filter.ed.Blur()
	tv.filtering = false
	if tv.query != "" {
		tv.query = ""
	}
}

// startFilter focuses the filter input.
func (tv *treeState) startFilter() tea.Cmd {
	tv.filtering = true
	tv.ensureVisible()
	return tv.filter.ed.Focus()
}

func (tv *treeState) filterInserting() bool {
	return tv.filtering && tv.filter.inserting()
}

func (tv *treeState) filterVimMode() string {
	if !tv.filtering {
		return ""
	}
	return tv.filter.vimMode()
}

// filterKey handles a key while the filter input has focus. It returns
// whether the selection changed.
func (tv *treeState) filterKey(msg tea.KeyPressMsg) (cmd tea.Cmd, moved bool) {
	page := max(1, tv.viewRows())
	cmd, nav := tv.filter.update(msg, page)
	switch nav {
	case pickNavClose:
		tv.filtering = false
		tv.filter.ed.Blur()
		return cmd, false
	case pickNavSubmit:
		tv.filtering = false
		tv.filter.ed.Blur()
		if tv.query == "" {
			tv.rebuild()
		}
		return cmd, false
	case pickNavUp:
		tv.move(-1)
		return cmd, true
	case pickNavDown:
		tv.move(1)
		return cmd, true
	case pickNavPageUp:
		tv.move(-max(1, page-1))
		return cmd, true
	case pickNavPageDown:
		tv.move(max(1, page-1))
		return cmd, true
	case pickNavChanged:
		if q := tv.filter.value(); q != tv.query {
			tv.query = q
			tv.cursor = 0
			tv.top = 0
			tv.sel = ""
			tv.rebuild()
			if r, ok := tv.current(); ok {
				tv.sel = r.id
			}
			return cmd, true
		}
	}
	return cmd, false
}

// filterPaste applies pasted text to the filter field.
func (tv *treeState) filterPaste(p tea.PasteMsg) (tea.Cmd, bool) {
	p.Content = collapsePaste(p.Content)
	cmd, nav := tv.filter.update(p, max(1, tv.viewRows()))
	if nav == pickNavChanged {
		if q := tv.filter.value(); q != tv.query {
			tv.query = q
			tv.cursor = 0
			tv.top = 0
			tv.sel = ""
			tv.rebuild()
			if r, ok := tv.current(); ok {
				tv.sel = r.id
			}
			return cmd, true
		}
	}
	return cmd, false
}

// view renders the tree into exactly h lines of w cells.
func (tv *treeState) view(focused bool) string {
	t := tv.th
	var lines []string
	if tv.filtering || tv.query != "" {
		lines = append(lines, tv.filter.view(tv.th))
	}
	n := tv.viewRows()
	if len(tv.rows) == 0 {
		lines = append(lines, tv.emptyView())
		return t.Block(strings.Join(lines, "\n"), tv.w, tv.h)
	}
	end := min(len(tv.rows), tv.top+n)
	for i := tv.top; i < end; i++ {
		if i == tv.cursor {
			lines = append(lines, tv.renderRow(tv.rows[i], true, focused))
			continue
		}
		row := ""
		if i < len(tv.cache) {
			row = tv.cache[i]
		}
		if row == "" {
			row = tv.renderRow(tv.rows[i], false, false)
		}
		lines = append(lines, row)
	}
	return t.Block(strings.Join(lines, "\n"), tv.w, tv.h)
}

func (tv *treeState) emptyView() string {
	t := tv.th
	if tv.query != "" {
		return t.EmptyState("No matches", "Nothing matches \""+tv.query+"\".", "Esc clears the filter.", tv.w)
	}
	return t.EmptyState("No collections", "Create one with n.", "", tv.w)
}

// seg accumulates styled segments while tracking width.
type seg struct {
	sb strings.Builder
	w  int
}

func (s *seg) add(style lipgloss.Style, text string) {
	if text == "" {
		return
	}
	s.sb.WriteString(style.Render(text))
	s.w += ui.Width(text)
}

// renderRow renders one row to exactly tv.w cells. sel paints the marker;
// focused additionally paints the selection background.
func (tv *treeState) renderRow(r treeRow, sel, focused bool) string {
	t := tv.th
	g := t.Glyphs
	bg := sel && focused
	on := func(tok ui.Token) lipgloss.Style { return t.StyleOn(tok, bg) }
	var s seg

	// Marker: shape and color, so selection reads without color.
	switch {
	case sel && focused:
		s.add(on(ui.AccentPrimary), g.Select+" ")
	case sel:
		s.add(on(ui.TextSecondary), g.Select+" ")
	default:
		s.add(on(ui.TextPrimary), "  ")
	}

	w := tv.w
	switch r.kind {
	case rowHeading:
		s.add(on(ui.StatusWarning), g.Warn+" "+r.label)
	case rowProblem:
		s.add(on(ui.StatusDanger), g.Fail+" ")
		// The file name matters most, so a long path loses its start.
		s.add(on(ui.TextPrimary), truncateLeft(t, r.label, max(4, w-s.w)))
		if reason := firstLine(r.problem.Err.Error()); reason != "" && w-s.w > 12 {
			s.add(on(ui.TextQuiet), "  "+t.Truncate(reason, w-s.w-2))
		}
	case rowCollection, rowFolder:
		indent := min(2*r.depth, max(0, w/3))
		s.add(on(ui.TextPrimary), strings.Repeat(" ", indent))
		fold := g.Collapsed
		if r.open {
			fold = g.Expanded
		}
		s.add(on(ui.TextSecondary), fold+" ")
		name := r.label
		count := ""
		if r.count > 0 {
			count = strconv.Itoa(r.count)
		}
		room := w - s.w
		if count != "" && room > ui.Width(count)+8 {
			room -= ui.Width(count) + 2
		} else {
			count = ""
		}
		nameStyle := on(ui.TextPrimary)
		if r.kind == rowCollection {
			nameStyle = nameStyle.Bold(true)
		}
		name = t.Truncate(name, max(1, room))
		s.add(nameStyle, name)
		if count != "" {
			s.add(on(ui.TextQuiet), strings.Repeat(" ", max(2, w-s.w-ui.Width(count)))+count)
		}
	case rowRequest:
		indent := 0
		if tv.query == "" {
			indent = min(2*r.depth, max(0, w/3)) + 2
		}
		s.add(on(ui.TextPrimary), strings.Repeat(" ", indent))
		s.add(on(ui.MethodToken(r.method)), ui.MethodLabel(r.method, methodWidth)+strings.Repeat(" ", max(0, methodWidth-len(ui.MethodLabel(r.method, methodWidth)))))
		s.add(on(ui.TextPrimary), " ")
		dirty := ""
		if tv.dirty[r.id] {
			dirty = " " + g.Dirty
		}
		room := max(1, w-s.w-ui.Width(dirty))
		if tv.query == "" {
			s.add(on(ui.TextPrimary), t.Truncate(r.label, room))
		} else {
			tv.addMatched(&s, r, room, on)
		}
		s.add(on(ui.StatusWarning), dirty)
	}

	if pad := w - s.w; pad > 0 {
		s.add(on(ui.TextPrimary), strings.Repeat(" ", pad))
	}
	return s.sb.String()
}

// addMatched writes a filter-result label: path context in secondary text,
// the request name in primary, matched letters in bold accent. When the label
// is too long the start is cut so the request name stays visible.
func (tv *treeState) addMatched(s *seg, r treeRow, room int, on func(ui.Token) lipgloss.Style) {
	runes := []rune(r.label)
	match := map[int]bool{}
	for _, p := range r.match {
		match[p] = true
	}
	offset := 0
	if len(runes) > room {
		cut := len(runes) - (room - 1)
		runes = append([]rune(tv.th.Glyphs.Ellipsis), runes[cut:]...)
		offset = cut - 1
	}
	// Classify each rune: 0 path context, 1 request name, 2 matched letter.
	class := func(i int) int {
		orig := i + offset
		switch {
		case i > 0 || offset == 0:
			if match[orig] {
				return 2
			}
			fallthrough
		default:
			if orig < r.ctxLen {
				return 0
			}
			return 1
		}
	}
	styles := [3]lipgloss.Style{on(ui.TextSecondary), on(ui.TextPrimary), on(ui.AccentPrimary).Bold(true)}
	start := 0
	for i := 1; i <= len(runes); i++ {
		if i == len(runes) || class(i) != class(start) {
			s.add(styles[class(start)], string(runes[start:i]))
			start = i
		}
	}
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

// truncateLeft shortens s to w cells by cutting from the start, keeping the
// end (for paths, the file name).
func truncateLeft(t ui.Theme, s string, w int) string {
	if ui.Width(s) <= w {
		return s
	}
	r := []rune(s)
	tail := []rune(t.Glyphs.Ellipsis)
	for len(r) > 0 && ui.Width(string(tail))+ui.Width(string(r)) > w {
		r = r[1:]
	}
	return string(tail) + string(r)
}

// foldedIDs lists the containers that are currently folded, sorted, for
// persistence (plan §0, D6).
func (tv *treeState) foldedIDs() []model.ID {
	var out []model.ID
	for id, n := range tv.nodes {
		if n.kind != rowRequest && tv.known[id] && !tv.expanded[id] {
			out = append(out, id)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// seedFolds applies a persisted fold state before the first workspace is
// indexed: listed containers start folded and every other container starts
// open, so the user's last layout returns exactly. An empty list keeps the
// size-based default.
func (tv *treeState) seedFolds(folded []model.ID, ws *model.Workspace) {
	if len(folded) == 0 || ws == nil {
		return
	}
	closed := make(map[model.ID]bool, len(folded))
	for _, id := range folded {
		closed[id] = true
	}
	mark := func(id model.ID) {
		tv.known[id] = true
		tv.expanded[id] = !closed[id]
	}
	for _, c := range ws.Collections {
		mark(c.ID)
		_ = c.Walk(func(it model.Item, _ model.Container, _ int) error {
			if f, ok := it.(*model.Folder); ok {
				mark(f.ID)
			}
			return nil
		})
	}
}
