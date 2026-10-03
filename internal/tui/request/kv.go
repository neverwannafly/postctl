package request

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/shubham/postctl/internal/model"
	"github.com/shubham/postctl/internal/tui/vimfield"
	"github.com/shubham/postctl/internal/tui/vimtext"
	"github.com/shubham/postctl/internal/ui"
)

// kvKind picks which list of the request a key-value table edits.
type kvKind int

const (
	kindParams kvKind = iota
	kindHeaders
	kindURLEncoded
	kindFormData
)

// kvItem is one row. path rows are :name path variables (value only); file
// rows are multipart file parts.
type kvItem struct {
	model.KV
	path  bool
	file  bool
	ctype string
}

func (k kvKind) noun() (one, many string) {
	switch k {
	case kindParams:
		return "parameter", "parameters"
	case kindHeaders:
		return "header", "headers"
	case kindURLEncoded:
		return "field", "fields"
	}
	return "part", "parts"
}

func loadItems(kind kvKind, r *model.Request) []kvItem {
	var out []kvItem
	switch kind {
	case kindParams:
		for _, q := range r.URL.Query {
			out = append(out, kvItem{KV: q})
		}
		for _, v := range r.URL.PathVars {
			out = append(out, kvItem{KV: model.KV{Key: v.Key, Value: v.Value, Description: v.Description}, path: true})
		}
	case kindHeaders:
		for _, h := range r.Headers {
			out = append(out, kvItem{KV: h})
		}
	case kindURLEncoded:
		for _, f := range r.Body.URLEncoded {
			out = append(out, kvItem{KV: f})
		}
	case kindFormData:
		for _, f := range r.Body.FormData {
			it := kvItem{KV: model.KV{Key: f.Key, Value: f.Value, Description: f.Description, Disabled: f.Disabled}, ctype: f.ContentType}
			if f.Type == model.FormFile {
				it.file = true
				it.Value = f.Src
			}
			out = append(out, it)
		}
	}
	return out
}

func storeItems(kind kvKind, r *model.Request, items []kvItem) {
	var kvs []model.KV
	for _, it := range items {
		if !it.path {
			kvs = append(kvs, it.KV)
		}
	}
	switch kind {
	case kindParams:
		r.URL.Query = kvs
		j := 0
		for _, it := range items {
			if it.path && j < len(r.URL.PathVars) {
				r.URL.PathVars[j].Value = it.Value
				r.URL.PathVars[j].Description = it.Description
				j++
			}
		}
		r.URL.SyncRaw()
	case kindHeaders:
		r.Headers = kvs
	case kindURLEncoded:
		r.Body.URLEncoded = kvs
	case kindFormData:
		var parts []model.FormPart
		for _, it := range items {
			fp := model.FormPart{Key: it.Key, Description: it.Description, Disabled: it.Disabled, ContentType: it.ctype}
			if it.file {
				fp.Type = model.FormFile
				fp.Src = it.Value
			} else {
				fp.Value = it.Value
			}
			parts = append(parts, fp)
		}
		r.Body.FormData = parts
	}
}

// kvView is a key-value table tab (Params, Headers) and the table inside the
// Body tab. It edits the request directly, one row at a time.
type kvView struct {
	kind     kvKind
	cur, top int

	edit  bool
	col   int // 0 key, 1 value, 2 description
	in    *vimfield.Field
	draft bool // the current row was just added and is still empty

	bulk bool
	area *vimfield.Field
}

func newKVView(kind kvKind) *kvView {
	return &kvView{kind: kind}
}

// setTheme drops the fields so they are rebuilt with the new theme the next
// time they open.
func (k *kvView) setTheme(*Pane) {
	k.in, k.area = nil, nil
	k.edit, k.bulk = false, false
}

// field is the focused cell or bulk editor, or nil.
func (k *kvView) field() *vimfield.Field {
	switch {
	case k.bulk:
		return k.area
	case k.edit:
		return k.in
	}
	return nil
}

func (k *kvView) reload(p *Pane) {
	n := len(k.items(p))
	k.cur = min(k.cur, max(0, n-1))
	if k.bulk {
		k.bulkSet(p)
	}
}

func (k *kvView) items(p *Pane) []kvItem {
	if p.req == nil {
		return nil
	}
	return loadItems(k.kind, p.req)
}

func (k *kvView) editing() bool { return k.edit || k.bulk }
func (k *kvView) stop() {
	k.edit = false
	k.bulk = false
	k.draft = false
}
func (k *kvView) enter(p *Pane) {}
func (k *kvView) atTop() bool   { return !k.edit && !k.bulk && k.cur == 0 }

func (k *kvView) paste(p *Pane, m tea.PasteMsg) bool {
	switch {
	case k.bulk:
		if k.area.Paste(m).Changed {
			return k.bulkApply(p)
		}
	case k.edit:
		if k.in.Paste(m).Changed {
			return k.applyText(p)
		}
	}
	return false
}

// descShown reports whether the table has room for the description column
// (the same rule ui.KVTable uses).
func descShown(width int, items []kvItem) bool {
	keyW := 0
	for _, it := range items {
		keyW = max(keyW, ui.Width(it.Key))
	}
	keyW = min(max(keyW, 3), max(8, (width-6)*35/100))
	return width-6-keyW-ui.SpaceSM >= 40
}

func (k *kvView) cellText(it kvItem, col int) string {
	switch col {
	case 0:
		return it.Key
	case 1:
		return it.Value
	}
	return it.Description
}

func setField(it *kvItem, col int, s string) {
	switch col {
	case 0:
		it.Key = s
	case 1:
		it.Value = s
	default:
		it.Description = s
	}
}

// cellWidth is the width of the edit field under the table.
func cellWidth(p *Pane) int {
	w, _ := p.bodySize()
	return max(4, w-12)
}

// beginEdit opens the cell in col for editing. start is "" for normal mode or
// "i", "a", "A", "I" to begin that vim insert command.
func (k *kvView) beginEdit(p *Pane, items []kvItem, col int, start string) {
	if k.cur >= len(items) {
		return
	}
	if items[k.cur].path && col == 0 {
		col = 1
	}
	k.edit = true
	k.col = col
	spec := fieldSpec{single: true}
	switch col {
	case 0:
		spec.placeholder = "name"
	case 1:
		spec.placeholder = "value"
	default:
		spec.placeholder = "description"
	}
	k.in = p.newField(spec)
	k.in.Set(k.cellText(items[k.cur], col))
	k.in.Fit(cellWidth(p), 1)
	k.in.Begin(start)
}

// applyText writes the input into the current row.
func (k *kvView) applyText(p *Pane) bool {
	items := k.items(p)
	if k.cur >= len(items) {
		return false
	}
	it := &items[k.cur]
	setField(it, k.col, k.in.Text())
	if k.draft {
		// A new row stays disabled (and so out of the URL and the sent
		// request) until it has content.
		it.Disabled = it.Key == "" && it.Value == ""
	}
	storeItems(k.kind, p.req, items)
	if k.kind == kindParams {
		p.syncURL()
	}
	return true
}

// finish leaves cell editing, dropping an untouched new row.
func (k *kvView) finish(p *Pane) bool {
	changed := false
	if k.edit && k.draft {
		items := k.items(p)
		if k.cur < len(items) {
			it := items[k.cur]
			if it.Key == "" && it.Value == "" && it.Description == "" {
				items = append(items[:k.cur], items[k.cur+1:]...)
				storeItems(k.kind, p.req, items)
				k.cur = min(k.cur, max(0, len(items)-1))
				changed = true
				if k.kind == kindParams {
					p.syncURL()
				}
			} else if items[k.cur].Disabled && items[k.cur].Key != "" {
				items[k.cur].Disabled = false
				storeItems(k.kind, p.req, items)
				changed = true
			}
		}
	}
	k.edit = false
	k.draft = false
	return changed
}

// addRow inserts an empty, disabled row and starts typing in its name cell.
func (k *kvView) addRow(p *Pane, items []kvItem) {
	at := len(items)
	if k.kind == kindParams {
		// Path variables come last; new query rows go before them.
		at = 0
		for i, it := range items {
			if !it.path {
				at = i + 1
			}
		}
		if k.cur < len(items) && !items[k.cur].path {
			at = k.cur + 1
		}
	} else if k.cur < len(items) {
		at = k.cur + 1
	}
	row := kvItem{KV: model.KV{Disabled: true}}
	items = append(items, kvItem{})
	copy(items[at+1:], items[at:])
	items[at] = row
	storeItems(k.kind, p.req, items)
	k.cur = at
	k.draft = true
	k.beginEdit(p, items, 0, "i")
}

func (k *kvView) key(p *Pane, m tea.KeyPressMsg) (handled, changed bool, cmd tea.Cmd) {
	if k.bulk {
		return k.bulkKey(p, m)
	}
	items := k.items(p)
	if k.edit {
		return k.editKey(p, m, items)
	}
	_, h := p.bodySize()
	rowsH := max(1, h-3)
	s := m.String()
	switch s {
	case "up":
		if k.cur > 0 {
			k.cur--
			return true, false, nil
		}
		return false, false, nil
	case "down":
		k.cur = min(k.cur+1, max(0, len(items)-1))
		return true, false, nil
	case "pgup":
		k.cur = max(0, k.cur-rowsH)
		return true, false, nil
	case "pgdown":
		k.cur = min(max(0, len(items)-1), k.cur+rowsH)
		return true, false, nil
	case "home":
		k.cur = 0
		return true, false, nil
	case "end":
		k.cur = max(0, len(items)-1)
		return true, false, nil
	case "a", "insert":
		k.addRow(p, items)
		return true, true, nil
	case "enter", "i", "I", "A": // Enter edits the cell in normal mode, the rest in insert
		if len(items) == 0 {
			k.addRow(p, items)
			return true, true, nil
		}
		start := ""
		if s != "enter" {
			start = s
		}
		k.beginEdit(p, items, 1, start)
		return true, false, nil
	case "x", "space", " ":
		if k.cur < len(items) && !items[k.cur].path {
			items[k.cur].Disabled = !items[k.cur].Disabled
			storeItems(k.kind, p.req, items)
			if k.kind == kindParams {
				p.syncURL()
			}
			return true, true, nil
		}
		return true, false, nil
	case "d", "delete":
		if k.cur < len(items) && !items[k.cur].path {
			items = append(items[:k.cur], items[k.cur+1:]...)
			storeItems(k.kind, p.req, items)
			k.cur = min(k.cur, max(0, len(items)-1))
			if k.kind == kindParams {
				p.syncURL()
			}
			return true, true, nil
		}
		return true, false, nil
	case "alt+up", "shift+up":
		return true, k.move(p, items, -1), nil
	case "alt+down", "shift+down":
		return true, k.move(p, items, 1), nil
	case "b":
		if k.kind == kindFormData {
			return false, false, nil
		}
		k.bulk = true
		k.area = p.newField(fieldSpec{})
		k.bulkSet(p)
		w, h := p.bodySize()
		k.area.Fit(w, max(1, h-2))
		k.area.Begin("")
		return true, false, nil
	case "t":
		if k.kind == kindFormData && k.cur < len(items) {
			items[k.cur].file = !items[k.cur].file
			storeItems(k.kind, p.req, items)
			return true, true, nil
		}
	}
	return false, false, nil
}

func (k *kvView) move(p *Pane, items []kvItem, dir int) bool {
	j := k.cur + dir
	if k.cur >= len(items) || j < 0 || j >= len(items) || items[k.cur].path || items[j].path {
		return false
	}
	items[k.cur], items[j] = items[j], items[k.cur]
	storeItems(k.kind, p.req, items)
	k.cur = j
	if k.kind == kindParams {
		p.syncURL()
	}
	return true
}

// editKey handles a key while a cell is open. Tab and Shift-Tab move between
// cells, Up and Down between rows, Enter (Submit) and Esc from normal mode
// (Leave) commit the cell; what the field does not own in normal mode goes to
// the shell.
func (k *kvView) editKey(p *Pane, m tea.KeyPressMsg, items []kvItem) (bool, bool, tea.Cmd) {
	s := m.String()
	if (s == "up" || s == "down") && !k.in.Completing() {
		return k.moveRow(p, s == "up")
	}
	k.in.Fit(cellWidth(p), 1)
	res := k.in.Update(m)
	var changed bool
	if res.Changed {
		changed = k.applyText(p)
	}
	switch res.Event.Kind {
	case vimtext.Submit, vimtext.Leave:
		return true, k.finish(p) || changed, res.Cmd
	case vimtext.Unhandled:
		switch res.Event.Key.String() {
		case "tab":
			ch, cmd := k.nextCell(p, items)
			return true, changed || ch, batch(res.Cmd, cmd)
		case "shift+tab":
			if k.col > 0 && (!items[k.cur].path || k.col != 1) {
				k.beginEdit(p, items, k.col-1, k.resumeKey())
			}
			return true, changed, res.Cmd
		}
	}
	return true, changed, batch(res.Cmd, forward(k.in, res.Event))
}

// resumeKey is the vim command that reopens a neighbouring cell the way the
// current one is being used: append in insert mode, plain normal otherwise.
func (k *kvView) resumeKey() string {
	if k.in != nil && k.in.Typing() {
		return "A"
	}
	return ""
}

// nextCell is Tab: the next column, or past the last one the next row (adding
// a row at the end).
func (k *kvView) nextCell(p *Pane, items []kvItem) (bool, tea.Cmd) {
	w, _ := p.bodySize()
	cols := 2
	if descShown(w, items) {
		cols = 3
	}
	resume := k.resumeKey()
	if k.col+1 < cols {
		k.beginEdit(p, items, k.col+1, resume)
		return false, nil
	}
	// Past the last column: on to the next row, adding one at the end.
	wasDraft := k.draft
	changed := k.finish(p)
	items = k.items(p)
	if wasDraft && changed {
		// The empty row we were on was dropped; stay put.
		return changed, nil
	}
	if k.cur+1 < len(items) {
		k.cur++
		k.beginEdit(p, items, 0, resume)
		return changed, nil
	}
	k.addRow(p, items)
	return true, nil
}

// moveRow is Up or Down in a cell: commit it and open the same column of the
// neighbouring row.
func (k *kvView) moveRow(p *Pane, up bool) (bool, bool, tea.Cmd) {
	col := k.col
	resume := k.resumeKey()
	changed := k.finish(p)
	items := k.items(p)
	next := k.cur + 1
	if up {
		next = k.cur - 1
	}
	if next >= 0 && next < len(items) {
		k.cur = next
		k.beginEdit(p, items, col, resume)
	}
	return true, changed, nil
}

// ---- bulk edit ----------------------------------------------------------------

func (k *kvView) bulkSet(p *Pane) {
	if k.area == nil {
		return
	}
	k.area.Set(bulkText(k.items(p)))
}

// bulkText renders rows as "key: value" lines, disabled rows prefixed "// ".
func bulkText(items []kvItem) string {
	var b strings.Builder
	for _, it := range items {
		if it.path {
			continue
		}
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		if it.Disabled {
			b.WriteString("// ")
		}
		b.WriteString(it.Key)
		b.WriteString(": ")
		b.WriteString(it.Value)
	}
	return b.String()
}

// external implements externalEditable: the rows as bulk text.
func (k *kvView) external(p *Pane) (string, string, bool) {
	if k.kind == kindFormData {
		return "", "", false
	}
	return ".txt", bulkText(k.items(p)), true
}

func (k *kvView) applyExternal(p *Pane, text string) {
	old := k.items(p)
	rows := parseBulk(text, old)
	for _, it := range old {
		if it.path {
			rows = append(rows, it)
		}
	}
	storeItems(k.kind, p.req, rows)
}

// parseBulk turns "key: value" lines back into rows, keeping the description
// of any row whose key it recognises.
func parseBulk(text string, old []kvItem) []kvItem {
	descs := map[string][]string{}
	for _, it := range old {
		if !it.path && it.Description != "" {
			descs[it.Key] = append(descs[it.Key], it.Description)
		}
	}
	var out []kvItem
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		dis := false
		if t := strings.TrimSpace(line); strings.HasPrefix(t, "//") {
			dis = true
			line = strings.TrimPrefix(t, "//")
		}
		key, val, _ := strings.Cut(line, ":")
		it := kvItem{KV: model.KV{Key: strings.TrimSpace(key), Value: strings.TrimSpace(val), Disabled: dis}}
		if d := descs[it.Key]; len(d) > 0 {
			it.Description = d[0]
			descs[it.Key] = d[1:]
		}
		out = append(out, it)
	}
	return out
}

func (k *kvView) bulkApply(p *Pane) bool {
	old := k.items(p)
	rows := parseBulk(k.area.Text(), old)
	for _, it := range old {
		if it.path {
			rows = append(rows, it)
		}
	}
	storeItems(k.kind, p.req, rows)
	if k.kind == kindParams {
		p.syncURL()
	}
	k.cur = min(k.cur, max(0, len(rows)-1))
	return true
}

func (k *kvView) bulkKey(p *Pane, m tea.KeyPressMsg) (bool, bool, tea.Cmd) {
	w, h := p.bodySize()
	k.area.Fit(w, max(1, h-2))
	res := k.area.Update(m)
	changed := false
	if res.Changed {
		changed = k.bulkApply(p)
	}
	if res.Event.Kind == vimtext.Leave {
		k.bulk = false
		k.area.Blur()
		return true, changed, res.Cmd
	}
	return true, changed, batch(res.Cmd, forward(k.area, res.Event))
}

// ---- view ---------------------------------------------------------------------

func (k *kvView) hints(p *Pane) []ui.Hint {
	switch {
	case k.bulk:
		return fieldHints(k.area, "Done")
	case k.edit:
		return append(fieldHints(k.in, "Done"),
			ui.Hint{Key: "Tab", Label: "Next cell", Priority: 2},
			ui.Hint{Key: "↑↓", Label: "Row", Priority: 4})
	}
	hs := []ui.Hint{
		{Key: "Enter", Label: "Edit", Priority: 2},
		{Key: "i", Label: "Insert", Priority: 3},
		{Key: "a", Label: "Add", Priority: 2},
		{Key: "x", Label: "Toggle", Priority: 3},
		{Key: "d", Label: "Delete", Priority: 3},
		{Key: "Alt-↑↓", Label: "Move", Priority: 5},
	}
	if k.kind != kindFormData {
		hs = append(hs, ui.Hint{Key: "b", Label: "Bulk edit", Priority: 4})
	} else {
		hs = append(hs, ui.Hint{Key: "t", Label: "Text/File", Priority: 4})
	}
	return hs
}

func (k *kvView) view(p *Pane, w, h int) string {
	th := p.th
	if k.bulk {
		return k.bulkView(p, w, h)
	}
	items := k.items(p)
	if len(items) == 0 {
		one, many := k.kind.noun()
		title := "No " + many
		body := "Paste a list in bulk edit (b), or add rows one at a time."
		if k.kind == kindParams {
			body = "Type ?name=value in the URL, or add one here."
		}
		return th.EmptyState(title, body, "Press a to add a "+one+".", w)
	}
	rowsH := max(1, h-3)
	top := scrollTo(k.top, k.cur, rowsH, len(items))
	end := min(len(items), top+rowsH)
	rows := make([]ui.KVRow, 0, end-top)
	for _, it := range items[top:end] {
		r := ui.KVRow{Key: it.Key, Value: it.Value, Description: it.Description, Disabled: it.Disabled}
		if it.path {
			r.Key = ":" + it.Key
			if r.Description == "" {
				r.Description = "path variable"
			}
		}
		if it.file {
			r.Value = "@ " + it.Value
		}
		rows = append(rows, r)
	}
	focus := -1
	if p.zone == zBody {
		focus = k.cur - top
	}
	table := th.KVTable(rows, ui.KVTableOptions{Width: w, Focus: focus, Header: true})
	tl := strings.Count(table, "\n") + 1
	pad := strings.Repeat("\n", max(0, rowsH+1-tl))
	out := table + pad + "\n" + k.statusLine(p, items, w)
	if k.edit && k.in != nil {
		// The completion menu of the cell draws over the rows above the edit line.
		if pop := k.in.Popup(max(4, w-12)); len(pop) > 0 {
			lines := strings.Split(out, "\n")
			out = strings.Join(vimfield.Overlay(lines, len(lines)-1, 12, pop), "\n")
		}
	}
	return out
}

// statusLine is the edit line while a cell is being edited, otherwise a quiet
// summary.
func (k *kvView) statusLine(p *Pane, items []kvItem, w int) string {
	th := p.th
	if k.edit && k.in != nil {
		label := [...]string{"Name", "Value", "Description"}[k.col]
		if k.cur < len(items) && items[k.cur].file && k.col == 1 {
			label = "File path"
		}
		return "\n" + th.Render(ui.TextSecondary, th.Fit(label, 12)) + k.in.Render(max(4, w-12), 1)
	}
	off, n := 0, 0
	for _, it := range items {
		if it.path {
			continue
		}
		n++
		if it.Disabled {
			off++
		}
	}
	one, many := k.kind.noun()
	noun := many
	if n == 1 {
		noun = one
	}
	text := itoa(n) + " " + noun
	if off > 0 {
		text += " " + th.Glyphs.Dot + " " + itoa(off) + " disabled"
	}
	return "\n" + th.Render(ui.TextQuiet, th.Truncate(text, w))
}

func (k *kvView) bulkView(p *Pane, w, h int) string {
	th := p.th
	head := th.Render(ui.TextSecondary, th.Truncate("Bulk edit "+th.Glyphs.Dot+" one key: value per line, // disables a row", w))
	return head + "\n\n" + k.area.Render(w, max(1, h-2))
}

// scrollTo returns the first visible row so cur stays in a window of size
// rows.
func scrollTo(top, cur, rows, n int) int {
	if cur < top {
		top = cur
	}
	if cur >= top+rows {
		top = cur - rows + 1
	}
	return max(0, min(top, max(0, n-rows)))
}

// fieldExternal implements fieldEditable for the cell being edited. Bulk mode
// edits all rows through external instead.
func (k *kvView) fieldExternal(p *Pane) (string, func(*Pane, string), string) {
	if !k.edit || k.bulk || k.in == nil {
		return "", nil, ""
	}
	row, col, draft := k.cur, k.col, k.draft
	return k.in.Text(), func(p *Pane, text string) {
		items := k.items(p)
		if row >= len(items) {
			return
		}
		it := &items[row]
		setField(it, col, text)
		if draft {
			it.Disabled = it.Key == "" && it.Value == ""
		}
		storeItems(k.kind, p.req, items)
		if k.kind == kindParams {
			p.syncURL()
		}
	}, ""
}
