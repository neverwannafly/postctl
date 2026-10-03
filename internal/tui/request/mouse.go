package request

import (
	tea "charm.land/bubbletea/v2"
	"github.com/shubham/postctl/internal/model"
	"github.com/shubham/postctl/internal/tui"
	"github.com/shubham/postctl/internal/tui/vimfield"
	"github.com/shubham/postctl/internal/tui/vimtext"
	"github.com/shubham/postctl/internal/ui"
)

// HandleMouse implements tui.MouseHandler. Coordinates are relative to the
// pane's inner body (below its header).
func (p *Pane) HandleMouse(msg tea.MouseMsg, x, y int) tea.Cmd {
	if !p.editable() || p.confirm != nil {
		return nil
	}
	switch msg := msg.(type) {
	case tea.MouseClickMsg:
		if msg.Button != tea.MouseLeft {
			return nil
		}
		p.mouse.Release()
		if f, fx, fy := p.mouseFieldAt(x, y); f != nil {
			if p.mouse.Press(f, fx, fy, p.deps.now()) {
				p.mouseX, p.mouseY = x-fx, y-fy
			}
			return nil
		}
		if y == chrome-2 || y == chrome-1 {
			tabs := p.tabList()
			width := p.w
			active := int(p.tab)
			start := 0
			for start < active {
				used := 2
				for i := start; i <= active; i++ {
					l := tabWidth(p.th, tabs[i])
					if i > start {
						l += ui.SpaceSM
					}
					used += l
				}
				if used <= width {
					break
				}
				start++
			}
			var hit int
			if start == 0 {
				hit = p.th.TabAt(width, tabs, x)
			} else {
				hit = p.th.TabAt(width-2, tabs[start:], x-2)
				if hit >= 0 {
					hit += start
				}
			}
			if hit >= 0 && hit < len(tabSections) {
				return p.onFocusSection(tui.FocusSectionMsg{Section: tabSections[hit]})
			}
		}
	case tea.MouseMotionMsg:
		if msg.Button == tea.MouseLeft && p.mouse.Active() {
			p.mouse.Motion(x-p.mouseX, y-p.mouseY)
		}
	case tea.MouseReleaseMsg:
		p.mouse.Release()
	case tea.MouseWheelMsg:
		if p.mouse.Active() {
			return nil
		}
		delta := wheelLines
		if msg.Button == tea.MouseWheelUp {
			delta = -wheelLines
		}
		if f := p.focusedFieldUnder(x, y); f != nil {
			f.ScrollLines(delta)
			return nil
		}
		switch msg.Button {
		case tea.MouseWheelUp:
			p.scrollWheel(-1)
		case tea.MouseWheelDown:
			p.scrollWheel(1)
		}
	}
	return nil
}

func (p *Pane) focusedFieldUnder(x, y int) *vimfield.Field {
	f := p.activeField()
	if f == nil {
		return nil
	}
	const urlX = 2 + methodW + 2
	if f == p.url && y == 0 && x >= urlX && x < urlX+p.urlWidth() {
		return f
	}
	if y < chrome {
		return nil
	}
	by := y - chrome
	switch v := p.views[p.tab].(type) {
	case *scriptView:
		if f == v.ed && by >= 0 && by < max(1, p.h-chrome-1) {
			return f
		}
	case *bodyView:
		if f == v.raw && by >= 2 {
			return f
		}
		if f == v.gq || f == v.gv {
			return f
		}
	case *kvView:
		if (f == v.in && by == max(0, p.h-chrome-1)) || (f == v.area && by >= 2) {
			return f
		}
	default:
		// Form fields are drawn inline; the active row is the only field.
		if by >= 0 && by < p.h-chrome {
			return f
		}
	}
	return nil
}

// mouseFieldAt returns the editor drawn under a pane-relative cell and the
// cell relative to that editor. Opening a field uses the same pane focus path
// as the keyboard, so the previous field is blurred and its live edit kept.
func (p *Pane) mouseFieldAt(x, y int) (*vimfield.Field, int, int) {
	const urlX = 2 + methodW + 2
	if y == 0 && x >= urlX && x < urlX+p.urlWidth() {
		wasInsert := p.urlE && p.url.Mode() == vimtext.Insert
		p.setZone(zBar)
		if wasInsert {
			p.url.Focus()
		}
		p.fitURL()
		return p.url, x - urlX, 0
	}
	if y < chrome {
		return nil, 0, 0
	}
	by := y - chrome
	switch v := p.views[p.tab].(type) {
	case *scriptView:
		if by >= 0 && by < max(1, p.h-chrome-1) {
			if !v.on {
				p.setZone(zBody)
				v.enter(p)
			}
			v.size(p)
			return v.ed, x, by
		}
	case *bodyView:
		p.setZone(zBody)
		switch v.mode(p) {
		case model.BodyRaw:
			ey := by - 2
			_, h := p.bodySize()
			eh := max(1, h-4)
			if ey >= 0 && ey < eh {
				if !v.on {
					v.cur = 2
					v.focusZone(p, zkRaw, "")
				}
				v.fit(p, zkRaw)
				return v.raw, x, ey
			}
		case model.BodyGraphQL:
			_, h := p.bodySize()
			room := max(1, h-2)
			qh, vh := gqHeights(room)
			if by >= 2 && by < 2+qh {
				v.cur = 1
				v.focusZone(p, zkQuery, "")
				return v.gq, x, by - 2
			}
			vy := by - (3 + qh)
			if vy >= 0 && vy < vh {
				v.cur = 2
				v.focusZone(p, zkVars, "")
				return v.gv, x, vy
			}
		case model.BodyURLEncoded, model.BodyFormData:
			if kv := v.kvs[v.mode(p)]; kv != nil {
				return p.kvMouseField(kv, x, by-2)
			}
		}
	case *kvView:
		p.setZone(zBody)
		if v.bulk && by >= 2 {
			v.area.Fit(p.w, max(1, p.h-chrome-2))
			return v.area, x, by - 2
		}
		if v.edit && v.in != nil && by == max(0, p.h-chrome-1) {
			return v.in, x - 12, 0
		}
		return p.kvMouseField(v, x, by)
	}
	return nil, 0, 0
}

func (p *Pane) kvMouseField(v *kvView, x, y int) (*vimfield.Field, int, int) {
	items := v.items(p)
	w, h := p.bodySize()
	rowsH := max(1, h-3)
	row := y - 1 // KVTable's header occupies the first line.
	top := scrollTo(v.top, v.cur, rowsH, len(items))
	if row < 0 || top+row >= len(items) || x < 6 {
		return nil, 0, 0
	}
	v.cur = top + row
	keyW := 0
	for _, it := range items {
		keyW = max(keyW, ui.Width(it.Key))
	}
	keyW = min(max(keyW, 3), max(8, (w-6)*35/100))
	rest := w - 6 - keyW - ui.SpaceSM
	descW := 0
	if rest >= 40 {
		descW = rest * 30 / 100
	}
	valW := max(1, rest)
	if descW > 0 {
		valW = max(1, rest-descW-ui.SpaceSM)
	}
	col, start, width := 0, 6, keyW
	valueStart := 6 + keyW + ui.SpaceSM
	if x >= valueStart {
		col, start, width = 1, valueStart, valW
	}
	descStart := valueStart + valW + ui.SpaceSM
	if descW > 0 && x >= descStart {
		col, start, width = 2, descStart, descW
	}
	if x >= start+width {
		return nil, 0, 0
	}
	v.beginEdit(p, items, col, "")
	return v.in, x - start, 0
}

// wheelLines is how many lines one wheel notch scrolls a text editor.
const wheelLines = 3

// scrollWheel scrolls the tab under the pointer. A field that is being typed
// into (insert or visual mode) or a modal selector owns its input, so the wheel
// does nothing then. A script, raw body or GraphQL editor scrolls its view
// whether or not it has the cursor: ScrollLines never edits or changes the
// mode. Lists move their cursor, unless one of their cells is open.
func (p *Pane) scrollWheel(delta int) {
	if p.msel != nil || p.typing() {
		return
	}
	switch p.tab {
	case tabPre, tabTests:
		if sv, ok := p.views[p.tab].(*scriptView); ok && sv.ed != nil {
			sv.size(p) // scroll against the size view draws
			sv.ed.ScrollLines(delta * wheelLines)
			return
		}
	case tabBody:
		if bv, ok := p.views[tabBody].(*bodyView); ok {
			switch bv.mode(p) {
			case model.BodyRaw:
				if bv.raw != nil {
					bv.fit(p, zkRaw)
					bv.raw.ScrollLines(delta * wheelLines)
					return
				}
			case model.BodyGraphQL:
				if bv.gq != nil {
					bv.fit(p, zkQuery)
					bv.gq.ScrollLines(delta * wheelLines)
					return
				}
			}
		}
	}
	v := p.views[p.tab]
	if v == nil || v.editing() {
		return
	}
	key := tea.KeyDown
	if delta < 0 {
		key = tea.KeyUp
	}
	if _, changed, _ := v.key(p, tea.KeyPressMsg{Code: key}); changed {
		p.syncURL()
	}
}

// typing reports whether the focused field takes plain keys as text (insert or
// visual mode). The wheel leaves such a field alone.
func (p *Pane) typing() bool {
	f := p.activeField()
	return f != nil && f.Typing()
}
