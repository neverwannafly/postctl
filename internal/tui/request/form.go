package request

import (
	"github.com/shubham/postctl/internal/tui/textin"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/shubham/postctl/internal/ui"
)

// rowKind is how a form row behaves.
type rowKind int

const (
	rowChoice rowKind = iota // cycles through choices with ← → / Enter / Space
	rowText                  // single-line text
	rowAction                // runs a command on Enter
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
	// action runs a rowAction.
	action func() tea.Cmd
}

// form is the cursor and edit state shared by the settings-style tabs.
type form struct {
	cur    int
	top    int
	edit   bool
	in     *textin.Input
	reveal bool
}

func (f *form) reset() {
	f.edit = false
	f.reveal = false
}

// selectable reports whether the cursor may rest on the row.
func selectable(r formRow) bool { return r.kind != rowInfo }

func (f *form) clamp(rows []formRow) {
	if len(rows) == 0 {
		f.cur = 0
		return
	}
	f.cur = min(max(f.cur, 0), len(rows)-1)
	if !selectable(rows[f.cur]) {
		// Land on the nearest selectable row, preferring downward.
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

func (f *form) beginEdit(p *Pane, r formRow) {
	f.edit = true
	f.in = textin.New(r.textValue())
	f.in.Placeholder = r.placeholder
	f.in.Mask = f.maskFor(r, f.in.Text())
	if !r.secret {
		f.in.Highlight = func(s string) []textin.Span { return p.sc.highlight(s) }
	}
}

// textValue is the editable text of a rowText (its value without notes).
func (r formRow) textValue() string { return r.value }

// key handles a key. rebuild returns fresh rows (after an edit the model may
// have changed shape, for example when the auth type changes).
func (f *form) key(p *Pane, rows func() []formRow, m tea.KeyPressMsg) (handled, changed bool, cmd tea.Cmd) {
	rs := rows()
	f.clamp(rs)
	if len(rs) == 0 {
		return false, false, nil
	}
	s := m.String()
	if f.edit {
		r := rs[f.cur]
		switch s {
		case "esc", "enter":
			f.edit = false
			return true, false, nil
		case "tab", "down", "shift+tab", "up":
			f.edit = false
			dir := 1
			if s == "shift+tab" || s == "up" {
				dir = -1
			}
			if f.step(rs, dir) && rs[f.cur].kind == rowText {
				f.beginEdit(p, rs[f.cur])
			}
			return true, false, nil
		}
		if f.in.Update(m) {
			if r.set != nil {
				changed = r.set(f.in.Text())
			}
			f.in.Mask = f.maskFor(r, f.in.Text())
			return true, changed, nil
		}
		return true, false, nil
	}
	r := rs[f.cur]
	switch s {
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
	case "left", "right", "space", " ", "enter":
		switch r.kind {
		case rowChoice:
			if r.cycle == nil {
				return true, false, nil
			}
			dir := 1
			if s == "left" {
				dir = -1
			}
			r.cycle(dir)
			return true, true, nil
		case rowText:
			if s == "enter" {
				f.beginEdit(p, r)
				return true, false, nil
			}
		case rowAction:
			if s == "enter" && r.action != nil {
				return true, false, r.action()
			}
		}
		// Left and Right fall through so they can switch tabs.
		return s == "enter" || s == "space" || s == " ", false, nil
	}
	return false, false, nil
}

func (f *form) paste(p *Pane, rows func() []formRow, m tea.PasteMsg) bool {
	if !f.edit || f.in == nil {
		return false
	}
	rs := rows()
	f.clamp(rs)
	r := rs[f.cur]
	f.in.Update(m)
	changed := false
	if r.set != nil {
		changed = r.set(f.in.Text())
	}
	f.in.Mask = f.maskFor(r, f.in.Text())
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
	if top > 0 || end < len(rows) {
		// A quiet scroll cue on the last visible line.
		if len(out) > 0 && h > 1 {
			more := th.Render(ui.TextQuiet, th.Glyphs.Ellipsis+" "+itoa(len(rows))+" fields")
			out[len(out)-1] = th.Fit(out[len(out)-1], w-ui.Width(more)-1) + " " + more
		}
	}
	return strings.Join(out, "\n")
}

func (f *form) rowView(p *Pane, r formRow, focused bool, labelW, valW int) string {
	th := p.th
	marker := "  "
	if focused && p.zone == zBody {
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
		if focused && p.zone == zBody {
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
			val = f.in.View(th, valW, true)
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
	case rowAction:
		val = th.Render(ui.AccentPrimary, r.value)
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
func renderSpans(th ui.Theme, s string, spans []textin.Span, width int) string {
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
