package wspane

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/shubham/postctl/internal/model"
	"github.com/shubham/postctl/internal/tui/textin"
	"github.com/shubham/postctl/internal/ui"
)

// kvKind picks which list of the request a key-value table edits.
type kvKind int

const (
	kvParams kvKind = iota
	kvHeaders
)

// kvItem is one row. path rows are :name path variables (value only).
type kvItem struct {
	model.KV
	path bool
}

func (k kvKind) noun() (one, many string) {
	if k == kvParams {
		return "parameter", "parameters"
	}
	return "header", "headers"
}

func loadItems(kind kvKind, r *model.Request) []kvItem {
	var out []kvItem
	if kind == kvParams {
		for _, q := range r.URL.Query {
			out = append(out, kvItem{KV: q})
		}
		for _, v := range r.URL.PathVars {
			out = append(out, kvItem{KV: model.KV{Key: v.Key, Value: v.Value, Description: v.Description}, path: true})
		}
		return out
	}
	for _, h := range r.Headers {
		out = append(out, kvItem{KV: h})
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
	if kind == kvHeaders {
		r.Headers = kvs
		return
	}
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
}

// kvView is the Params or Headers tab: a key-value table that edits the
// request directly, one row at a time.
type kvView struct {
	kind     kvKind
	cur, top int
	edit     bool
	col      int // 0 key, 1 value
	in       *textin.Input
	draft    bool // the current row was just added and is still empty
}

func newKVView(kind kvKind) *kvView { return &kvView{kind: kind} }

func (k *kvView) items(p *Pane) []kvItem {
	if p.req == nil {
		return nil
	}
	return loadItems(k.kind, p.req)
}

func (k *kvView) reload(p *Pane) { k.cur = min(k.cur, max(0, len(k.items(p))-1)) }
func (k *kvView) editing() bool  { return k.edit }
func (k *kvView) stop()          { k.edit, k.draft = false, false }

func (k *kvView) paste(p *Pane, m tea.PasteMsg) bool {
	if !k.edit || k.in == nil {
		return false
	}
	k.in.Update(m)
	return k.applyText(p)
}

func (k *kvView) field(it kvItem, col int) string {
	if col == 0 {
		return it.Key
	}
	return it.Value
}

func (k *kvView) beginEdit(p *Pane, items []kvItem, col int) {
	if k.cur >= len(items) {
		return
	}
	if items[k.cur].path && col == 0 {
		col = 1
	}
	k.edit = true
	k.col = col
	k.in = textin.New(k.field(items[k.cur], col))
	k.in.Placeholder = [...]string{"name", "value"}[col]
	k.in.Highlight = func(s string) []textin.Span { return p.sc.highlight(s) }
}

// applyText writes the input into the current row.
func (k *kvView) applyText(p *Pane) bool {
	items := k.items(p)
	if k.cur >= len(items) {
		return false
	}
	it := &items[k.cur]
	if k.col == 0 {
		it.Key = k.in.Text()
	} else {
		it.Value = k.in.Text()
	}
	if k.draft {
		// A new row stays disabled (and so out of the URL and the request)
		// until it has content.
		it.Disabled = it.Key == "" && it.Value == ""
	}
	storeItems(k.kind, p.req, items)
	if k.kind == kvParams {
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
			switch {
			case it.Key == "" && it.Value == "" && it.Description == "":
				items = append(items[:k.cur], items[k.cur+1:]...)
				storeItems(k.kind, p.req, items)
				k.cur = min(k.cur, max(0, len(items)-1))
				changed = true
				if k.kind == kvParams {
					p.syncURL()
				}
			case it.Disabled && it.Key != "":
				items[k.cur].Disabled = false
				storeItems(k.kind, p.req, items)
				changed = true
			}
		}
	}
	k.edit, k.draft = false, false
	return changed
}

func (k *kvView) addRow(p *Pane, items []kvItem) {
	at := len(items)
	if k.kind == kvParams {
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
	items = append(items, kvItem{})
	copy(items[at+1:], items[at:])
	items[at] = kvItem{KV: model.KV{Disabled: true}}
	storeItems(k.kind, p.req, items)
	k.cur = at
	k.draft = true
	k.beginEdit(p, items, 0)
}

func (k *kvView) key(p *Pane, m tea.KeyPressMsg) (handled, changed bool) {
	items := k.items(p)
	if k.edit {
		return k.editKey(p, m, items)
	}
	_, h := p.bodySize()
	rowsH := max(1, h-3)
	switch m.String() {
	case "up":
		if k.cur > 0 {
			k.cur--
			return true, false
		}
	case "down":
		k.cur = min(k.cur+1, max(0, len(items)-1))
		return true, false
	case "pgup":
		k.cur = max(0, k.cur-rowsH)
		return true, false
	case "pgdown":
		k.cur = min(max(0, len(items)-1), k.cur+rowsH)
		return true, false
	case "home":
		k.cur = 0
		return true, false
	case "end":
		k.cur = max(0, len(items)-1)
		return true, false
	case "a", "insert":
		k.addRow(p, items)
		return true, true
	case "enter":
		if len(items) == 0 {
			k.addRow(p, items)
			return true, true
		}
		k.beginEdit(p, items, 1)
		return true, false
	case "space", " ":
		if k.cur < len(items) && !items[k.cur].path {
			items[k.cur].Disabled = !items[k.cur].Disabled
			storeItems(k.kind, p.req, items)
			if k.kind == kvParams {
				p.syncURL()
			}
			return true, true
		}
		return true, false
	case "d", "delete", "backspace":
		if k.cur < len(items) && !items[k.cur].path {
			items = append(items[:k.cur], items[k.cur+1:]...)
			storeItems(k.kind, p.req, items)
			k.cur = min(k.cur, max(0, len(items)-1))
			if k.kind == kvParams {
				p.syncURL()
			}
			return true, true
		}
		return true, false
	}
	return false, false
}

func (k *kvView) editKey(p *Pane, m tea.KeyPressMsg, items []kvItem) (bool, bool) {
	s := m.String()
	switch s {
	case "esc", "enter":
		return true, k.finish(p)
	case "tab":
		if k.col+1 < 2 {
			k.beginEdit(p, items, k.col+1)
			return true, false
		}
		// Past the last column: on to the next row, adding one at the end.
		wasDraft := k.draft
		changed := k.finish(p)
		items = k.items(p)
		if wasDraft && changed {
			return true, changed // the empty row was dropped; stay put
		}
		if k.cur+1 < len(items) {
			k.cur++
			k.beginEdit(p, items, 0)
			return true, changed
		}
		k.addRow(p, items)
		return true, true
	case "shift+tab":
		if k.col > 0 && (!items[k.cur].path || k.col != 1) {
			k.beginEdit(p, items, k.col-1)
		}
		return true, false
	case "up", "down":
		col := k.col
		changed := k.finish(p)
		items = k.items(p)
		next := k.cur + 1
		if s == "up" {
			next = k.cur - 1
		}
		if next >= 0 && next < len(items) {
			k.cur = next
			k.beginEdit(p, items, col)
		}
		return true, changed
	}
	if k.in.Update(m) {
		return true, k.applyText(p)
	}
	return true, false
}

func (k *kvView) hints() []ui.Hint {
	if k.edit {
		return []ui.Hint{
			{Key: "Esc", Label: "Done", Priority: 1},
			{Key: "Tab", Label: "Next field", Priority: 2},
			{Key: "↑↓", Label: "Row", Priority: 4},
		}
	}
	return []ui.Hint{
		{Key: "Enter", Label: "Edit", Priority: 2},
		{Key: "a", Label: "Add", Priority: 2},
		{Key: "Space", Label: "Toggle", Priority: 3},
		{Key: "d", Label: "Delete", Priority: 3},
	}
}

func (k *kvView) view(p *Pane, w, h int) string {
	th := p.th
	items := k.items(p)
	if len(items) == 0 {
		one, many := k.kind.noun()
		body := "Headers are sent with the connection request, before the first message."
		if k.kind == kvParams {
			body = "Type ?name=value in the URL, or add one here."
		}
		return th.EmptyState("No "+many, body, "Press a to add a "+one+".", w)
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
		rows = append(rows, r)
	}
	focus := -1
	if p.zone == zBody {
		focus = k.cur - top
	}
	table := th.KVTable(rows, ui.KVTableOptions{Width: w, Focus: focus, Header: true})
	tl := strings.Count(table, "\n") + 1
	pad := strings.Repeat("\n", max(0, rowsH+1-tl))
	return table + pad + "\n" + k.statusLine(p, items, w)
}

// statusLine is the edit line while a cell is being edited, otherwise a quiet
// summary.
func (k *kvView) statusLine(p *Pane, items []kvItem, w int) string {
	th := p.th
	if k.edit && k.in != nil {
		label := [...]string{"Name", "Value"}[k.col]
		return "\n" + th.Render(ui.TextSecondary, th.Fit(label, 12)) + k.in.View(th, w-12, true)
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
