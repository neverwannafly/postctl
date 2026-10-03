package wspane

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/shubham/postctl/internal/tui/vimfield"
	"github.com/shubham/postctl/internal/tui/vimtext"
	"github.com/shubham/postctl/internal/ui"
)

// rowKind is how a form row behaves.
type rowKind int

const (
	rowChoice rowKind = iota // cycles with ← → / Enter / Space
	rowText                  // single-line text
	rowInfo                  // read-only text, skipped by the cursor
)

// formRow is one line of a settings-style form. Rows are rebuilt from the
// request whenever needed, so closures may capture request sub-structs.
type formRow struct {
	label string
	kind  rowKind
	// value is the text shown (the choice label for rowChoice).
	value string
	// secret masks literal values until revealed; {{references}} always show.
	secret      bool
	placeholder string
	// note is a quiet trailing explanation; warn colors it as a warning.
	note string
	warn bool
	// set applies edited text (rowText) and reports whether it was accepted.
	set func(text string) bool
	// cycle moves a rowChoice by dir (+1/-1).
	cycle func(dir int)
}

// form is the cursor and edit state shared by the Auth and Settings tabs.
type form struct {
	cur    int
	top    int
	edit   bool
	in     *vimfield.Field
	reveal bool
}

// field is the focused row editor, or nil.
func (f *form) field() *vimfield.Field {
	if f.edit {
		return f.in
	}
	return nil
}

func (f *form) reset() {
	f.edit = false
	f.reveal = false
}

func selectable(r formRow) bool { return r.kind != rowInfo }

func (f *form) clamp(rows []formRow) {
	if len(rows) == 0 {
		f.cur = 0
		return
	}
	f.cur = min(max(f.cur, 0), len(rows)-1)
	if selectable(rows[f.cur]) {
		return
	}
	for i := f.cur; i < len(rows); i++ {
		if selectable(rows[i]) {
			f.cur = i
			return
		}
	}
	for i := f.cur; i >= 0; i-- {
		if selectable(rows[i]) {
			f.cur = i
			return
		}
	}
}

// step moves the cursor to the next selectable row in dir.
func (f *form) step(rows []formRow, dir int) bool {
	for i := f.cur + dir; i >= 0 && i < len(rows); i += dir {
		if selectable(rows[i]) {
			f.cur = i
			return true
		}
	}
	return false
}

func (f *form) maskFor(r formRow, text string) bool {
	return r.secret && !f.reveal && !strings.Contains(text, "{{")
}

// rowWidth is the width the open row editor gets.
func rowWidth(p *Pane) int {
	w, _ := p.bodySize()
	return max(4, w-2-min(20, max(10, w/3))-1)
}

// beginEdit opens a text row. start is "" for normal mode or "i", "a", "A", "I".
func (f *form) beginEdit(p *Pane, r formRow, start string) {
	f.edit = true
	f.in = p.newField(fieldSpec{single: true, secret: r.secret, placeholder: r.placeholder})
	f.in.Set(r.value)
	f.in.SetMask(f.maskFor(r, f.in.Text()))
	f.in.Fit(rowWidth(p), 1)
	f.in.Begin(start)
}

// setReveal shows or hides the secret literal being edited.
func (f *form) setReveal(on bool, r formRow) {
	f.reveal = on
	if f.edit && f.in != nil {
		f.in.SetMask(f.maskFor(r, f.in.Text()))
	}
}

// key handles a key. rows returns fresh rows, because an edit can change the
// form's shape (for example when the auth type changes).
func (f *form) key(p *Pane, rows func() []formRow, m tea.KeyPressMsg) (handled, changed bool, cmd tea.Cmd) {
	rs := rows()
	f.clamp(rs)
	if len(rs) == 0 {
		return false, false, nil
	}
	s := m.String()
	if f.edit {
		return f.editKey(p, rs, m)
	}
	r := rs[f.cur]
	switch s {
	case "i", "I", "A":
		if r.kind == rowText {
			f.beginEdit(p, r, s)
			return true, false, nil
		}
		return false, false, nil
	case "up":
		return f.step(rs, -1), false, nil
	case "down":
		return f.step(rs, 1), false, nil
	case "home":
		f.cur = 0
		f.clamp(rs)
		return true, false, nil
	case "end":
		f.cur = len(rs) - 1
		f.clamp(rs)
		return true, false, nil
	case "r":
		f.reveal = !f.reveal
		return true, false, nil
	case "left", "right", "space", " ", "enter":
		if r.kind == rowChoice && r.cycle != nil {
			dir := 1
			if s == "left" {
				dir = -1
			}
			r.cycle(dir)
			return true, true, nil
		}
		if r.kind == rowText && s == "enter" {
			f.beginEdit(p, r, "")
			return true, false, nil
		}
		// Left and Right fall through so they can switch tabs.
		return s == "enter" || s == "space" || s == " ", false, nil
	}
	return false, false, nil
}

// editKey handles a key while a text row is open. Tab and Shift-Tab, Up and
// Down move to the neighbouring text row; Enter (Submit) and Esc from normal
// mode (Leave) close the row; keys the field does not own in normal mode go
// to the shell.
func (f *form) editKey(p *Pane, rs []formRow, m tea.KeyPressMsg) (handled, changed bool, cmd tea.Cmd) {
	r := rs[f.cur]
	s := m.String()
	if (s == "up" || s == "down") && !f.in.Completing() {
		return f.moveRow(p, rs, s == "up")
	}
	f.in.Fit(rowWidth(p), 1)
	res := f.in.Update(m)
	if res.Changed {
		if r.set != nil {
			changed = r.set(f.in.Text())
		}
		f.in.SetMask(f.maskFor(r, f.in.Text()))
	}
	switch res.Event.Kind {
	case vimtext.Submit, vimtext.Leave:
		f.edit = false
		return true, changed, res.Cmd
	case vimtext.Unhandled:
		switch res.Event.Key.String() {
		case "tab":
			ok, _, _ := f.moveRow(p, rs, false)
			return ok, changed, res.Cmd
		case "shift+tab":
			ok, _, _ := f.moveRow(p, rs, true)
			return ok, changed, res.Cmd
		case "ctrl+r":
			if r.secret {
				f.setReveal(!f.reveal, r)
			}
			return true, changed, res.Cmd
		}
	}
	return true, changed, batch(res.Cmd, forward(f.in, res.Event))
}

// moveRow closes the open row and opens the next (or previous) text row,
// continuing in insert mode when the row it left was being typed in.
func (f *form) moveRow(p *Pane, rs []formRow, up bool) (bool, bool, tea.Cmd) {
	start := ""
	if f.in != nil && f.in.Typing() {
		start = "A"
	}
	f.edit = false
	dir := 1
	if up {
		dir = -1
	}
	if f.step(rs, dir) && rs[f.cur].kind == rowText {
		f.beginEdit(p, rs[f.cur], start)
	}
	return true, false, nil
}

func (f *form) paste(p *Pane, rows func() []formRow, m tea.PasteMsg) bool {
	if !f.edit || f.in == nil {
		return false
	}
	rs := rows()
	f.clamp(rs)
	r := rs[f.cur]
	if !f.in.Paste(m).Changed {
		return false
	}
	changed := false
	if r.set != nil {
		changed = r.set(f.in.Text())
	}
	f.in.SetMask(f.maskFor(r, f.in.Text()))
	return changed
}

// view draws the rows in w x h.
func (f *form) view(p *Pane, rows []formRow, w, h int) string {
	th := p.th
	view := *f
	view.clamp(rows)
	labelW := min(20, max(10, w/3))
	valW := max(4, w-2-labelW-1)
	top := scrollTo(view.top, view.cur, h, len(rows))
	end := min(len(rows), top+h)
	var out []string
	for i := top; i < end; i++ {
		out = append(out, view.rowView(p, rows[i], i == view.cur, labelW, valW))
	}
	if (top > 0 || end < len(rows)) && len(out) > 0 && h > 1 {
		more := th.Render(ui.TextQuiet, th.Glyphs.Ellipsis+" "+itoa(len(rows))+" fields")
		out[len(out)-1] = th.Fit(out[len(out)-1], w-ui.Width(more)-1) + " " + more
	}
	if view.edit && view.in != nil && view.cur >= top && view.cur < end {
		// The completion menu of the row draws over the rows next to it.
		out = vimfield.Overlay(out, view.cur-top, 2+labelW+1, view.in.Popup(valW))
	}
	return strings.Join(out, "\n")
}

func (f *form) rowView(p *Pane, r formRow, focused bool, labelW, valW int) string {
	th := p.th
	marker := "  "
	if focused && p.focused {
		marker = th.Render(ui.AccentPrimary, th.Glyphs.Select) + " "
	}
	labelTok := ui.TextSecondary
	if r.kind == rowInfo {
		labelTok = ui.TextQuiet
	}
	label := th.Render(labelTok, th.Fit(r.label, labelW))
	var val string
	editNote := ""
	if focused && f.edit && r.warn && r.note != "" {
		editNote = " " + r.note
		valW = max(4, valW-ui.Width(editNote))
	}
	switch r.kind {
	case rowChoice:
		if focused && p.focused {
			l, rt := "‹", "›"
			if th.ASCII {
				l, rt = "<", ">"
			}
			val = th.Render(ui.TextQuiet, l) + " " + th.Render(ui.TextPrimary, r.value) + " " + th.Render(ui.TextQuiet, rt)
		} else {
			val = th.Render(ui.TextPrimary, r.value)
		}
	case rowText:
		switch {
		case focused && f.edit && f.in != nil:
			val = f.in.Render(valW, 1)
		case r.value == "":
			val = th.Render(ui.TextQuiet, th.Truncate(r.placeholder, valW))
		case r.secret && !f.reveal && !strings.Contains(r.value, "{{"):
			val = th.Render(ui.TextPrimary, th.Glyphs.Mask)
		default:
			val = th.Render(ui.TextPrimary, th.Truncate(r.value, valW))
			if !r.secret || f.reveal || strings.Contains(r.value, "{{") {
				if hl := p.sc.highlight(r.value); len(hl) > 0 {
					val = renderSpans(th, r.value, hl, valW)
				}
			}
		}
	case rowInfo:
		if r.label == "" {
			// A sentence, not a field: use the label column too.
			return marker + th.Render(ui.TextSecondary, th.Truncate(r.value, valW+labelW+1))
		}
		val = th.Render(ui.TextSecondary, th.Truncate(r.value, valW))
	}
	line := marker + label + " " + val
	if editNote != "" {
		line += th.Render(ui.StatusWarning, editNote)
	}
	if r.note != "" && (!focused || !f.edit) {
		room := valW - ui.Width(val)
		note := " " + r.note
		if room >= ui.Width(note)+1 {
			tok := ui.TextQuiet
			if r.warn {
				tok = ui.StatusWarning
			}
			line += th.Render(tok, note)
		}
	}
	return line
}

// renderSpans draws s with highlight spans, truncated to width.
func renderSpans(th ui.Theme, s string, spans []span, width int) string {
	rs := []rune(s)
	var b strings.Builder
	pos := 0
	for _, sp := range spans {
		if sp.From > pos {
			b.WriteString(th.Render(ui.TextPrimary, string(rs[pos:sp.From])))
		}
		b.WriteString(th.Render(sp.Tok, string(rs[sp.From:min(sp.To, len(rs))])))
		pos = sp.To
	}
	if pos < len(rs) {
		b.WriteString(th.Render(ui.TextPrimary, string(rs[pos:])))
	}
	return th.Truncate(b.String(), width)
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
