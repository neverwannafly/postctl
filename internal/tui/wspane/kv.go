package wspane

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	hist "github.com/shubham/postctl/internal/history"
	"github.com/shubham/postctl/internal/model"
	"github.com/shubham/postctl/internal/tui/vimfield"
	"github.com/shubham/postctl/internal/tui/vimtext"
	"github.com/shubham/postctl/internal/ui"
)

var headerSecrets = hist.NewRedactor(nil)

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
	in       *vimfield.Field
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

// field is the focused cell editor, or nil.
func (k *kvView) field() *vimfield.Field {
	if k.edit {
		return k.in
	}
	return nil
}

// cellWidth is the width of the edit field under the table.
func cellWidth(p *Pane) int {
	w, _ := p.bodySize()
	return max(4, w-12)
}
func (k *kvView) stop() { k.edit, k.draft = false, false }

func (k *kvView) paste(p *Pane, m tea.PasteMsg) bool {
	if !k.edit || k.in == nil {
		return false
	}
	if !k.in.Paste(m).Changed {
		return false
	}
	return k.applyText(p)
}

func (k *kvView) cellText(it kvItem, col int) string {
	if col == 0 {
		return it.Key
	}
	return it.Value
}

func (k *kvView) secretCell(it kvItem, col int) bool {
	return k.kind == kvHeaders && col == 1 && headerSecrets.Sensitive(it.Key)
}

// beginEdit opens a cell. start is "" for normal mode or "i", "a", "A", "I".
func (k *kvView) beginEdit(p *Pane, items []kvItem, col int, start string) {
	if k.cur >= len(items) {
		return
	}
	if items[k.cur].path && col == 0 {
		col = 1
	}
	k.edit = true
	k.col = col
	secret := k.secretCell(items[k.cur], col)
	k.in = p.newField(fieldSpec{single: true, secret: secret, placeholder: [...]string{"name", "value"}[col]})
	k.in.Set(k.cellText(items[k.cur], col))
	k.in.SetMask(secret)
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
	k.beginEdit(p, items, 0, "i")
}

func (k *kvView) key(p *Pane, m tea.KeyPressMsg) (handled, changed bool, cmd tea.Cmd) {
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
			return true, false, nil
		}
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
		if m.String() != "enter" {
			start = m.String()
		}
		k.beginEdit(p, items, 1, start)
		return true, false, nil
	case "x", "space", " ":
		if k.cur < len(items) && !items[k.cur].path {
			items[k.cur].Disabled = !items[k.cur].Disabled
			storeItems(k.kind, p.req, items)
			if k.kind == kvParams {
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
			if k.kind == kvParams {
				p.syncURL()
			}
			return true, true, nil
		}
		return true, false, nil
	}
	return false, false, nil
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

// nextCell moves to the next cell, the next row after the last column, adding
// a row at the end.
func (k *kvView) nextCell(p *Pane, items []kvItem) (bool, tea.Cmd) {
	start := k.resumeKey()
	if k.col+1 < 2 {
		k.beginEdit(p, items, k.col+1, start)
		return false, nil
	}
	wasDraft := k.draft
	changed := k.finish(p)
	items = k.items(p)
	if wasDraft && changed {
		return changed, nil // the empty row was dropped; stay put
	}
	if k.cur+1 < len(items) {
		k.cur++
		k.beginEdit(p, items, 0, start)
		return changed, nil
	}
	k.addRow(p, items)
	return true, nil
}

// moveRow commits the cell and opens the same column of the next (or
// previous) row.
func (k *kvView) moveRow(p *Pane, up bool) (bool, bool, tea.Cmd) {
	col, start := k.col, k.resumeKey()
	changed := k.finish(p)
	items := k.items(p)
	next := k.cur + 1
	if up {
		next = k.cur - 1
	}
	if next >= 0 && next < len(items) {
		k.cur = next
		k.beginEdit(p, items, col, start)
	}
	return true, changed, nil
}

func (k *kvView) hints() []ui.Hint {
	if k.edit {
		return append(fieldHints(k.in, "Done"),
			ui.Hint{Key: "Tab", Label: "Next field", Priority: 2},
			ui.Hint{Key: "↑↓", Label: "Row", Priority: 4})
	}
	return []ui.Hint{
		{Key: "Enter", Label: "Edit", Priority: 2},
		{Key: "a", Label: "Add", Priority: 2},
		{Key: "x", Label: "Toggle", Priority: 3},
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
		r.Secret = k.secretCell(it, 1)
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
		label := [...]string{"Name", "Value"}[k.col]
		return "\n" + th.Render(ui.TextSecondary, th.Fit(label, 12)) + k.in.Render(w-12, 1)
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
