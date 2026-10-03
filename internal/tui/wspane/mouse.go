package wspane

import (
	tea "charm.land/bubbletea/v2"
	"github.com/shubham/postctl/internal/tui"
	"github.com/shubham/postctl/internal/tui/vimfield"
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
			hit := p.th.TabAt(p.w, tabs, x)
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
		if f := p.focusedFieldUnder(x, y); f != nil {
			delta := wheelLines
			if msg.Button == tea.MouseWheelUp {
				delta = -wheelLines
			}
			f.ScrollLines(delta)
			return nil
		}
		if p.activeField() != nil {
			return nil
		}
		switch msg.Button {
		case tea.MouseWheelUp:
			return p.scrollWheel(-1, x, y)
		case tea.MouseWheelDown:
			return p.scrollWheel(1, x, y)
		}
	}
	return nil
}

func (p *Pane) focusedFieldUnder(x, y int) *vimfield.Field {
	f := p.activeField()
	if f == nil {
		return nil
	}
	if f == p.url && y == 0 && x >= urlX && x < urlX+p.urlWidth() {
		return f
	}
	if p.tab == tabMessages {
		w, h := p.bodySize()
		g := msgLayout(w, h, len(p.messages()))
		if f == p.comp && p.overComposer(x, y) {
			p.fitComposer()
			return f
		}
		if f == p.search {
			sx, sy := 2, chrome+1
			if g.wide {
				sx += g.compW + gapWidth
			} else {
				sy += g.compH + g.savedH
			}
			if x >= sx && x < sx+max(4, g.logW-2) && y == sy {
				return f
			}
		}
		if f == p.rename {
			rows := max(1, g.savedH-1)
			top := scrollTo(p.savedTop, p.savedCur, rows, len(p.messages()))
			sy := chrome + g.compH + 1 + p.savedCur - top
			if x >= 2 && x < 2+renameWidth(g.savedW) && y == sy {
				return f
			}
		}
		return nil
	}
	if y < chrome {
		return nil
	}
	by := y - chrome
	switch p.tab {
	case tabParams:
		if f == p.params.in && by == max(0, p.h-chrome-1) {
			return f
		}
	case tabHeaders:
		if f == p.headers.in && by == max(0, p.h-chrome-1) {
			return f
		}
	case tabAuth:
		if f == p.auth.f.in && p.formFieldUnder(&p.auth.f, p.auth.rows(p), x, by, p.w, max(1, p.h-chrome)) {
			return f
		}
	case tabSettings:
		if f == p.settings.f.in && p.formFieldUnder(&p.settings.f, p.settings.rows(p), x, by, p.w, max(1, p.h-chrome)) {
			return f
		}
	}
	return nil
}

func (p *Pane) formFieldUnder(f *form, rows []formRow, x, y, w, h int) bool {
	if !f.edit || f.in == nil || len(rows) == 0 {
		return false
	}
	labelW := min(20, max(10, w/3))
	valueX := 2 + labelW + 1
	top := scrollTo(f.top, f.cur, h, len(rows))
	return x >= valueX && x < w && y == f.cur-top
}

func (p *Pane) mouseFieldAt(x, y int) (*vimfield.Field, int, int) {
	if y == 0 && x >= urlX && x < urlX+p.urlWidth() {
		if p.zone != zBar {
			p.stopEditing()
			p.setZone(zBar)
		}
		p.fitURL()
		return p.url, x - urlX, 0
	}
	if p.tab == tabMessages {
		w, h := p.bodySize()
		g := msgLayout(w, h, len(p.messages()))
		if p.searching {
			sx, sy := 2, chrome+1
			if g.wide {
				sx += g.compW + gapWidth
			} else {
				sy += g.compH + g.savedH
			}
			if x >= sx && x < sx+max(4, g.logW-2) && y == sy {
				p.fitPrompts()
				return p.search, x - sx, 0
			}
		}
		if p.rename != nil {
			rows := max(1, g.savedH-1)
			top := scrollTo(p.savedTop, p.savedCur, rows, len(p.messages()))
			sy := chrome + g.compH + 1 + p.savedCur - top
			if x >= 2 && x < 2+renameWidth(g.savedW) && y == sy {
				p.fitPrompts()
				return p.rename, x - 2, 0
			}
		}
		if y >= chrome+1 && y < chrome+g.compH && (!g.wide || x < g.compW) {
			if p.activeField() != p.comp {
				p.stopEditing()
				p.sect = sComposer
				p.setZone(zBody)
			}
			p.fitComposer()
			return p.comp, x, y - chrome - 1
		}
	}
	if y >= chrome {
		by := y - chrome
		switch p.tab {
		case tabParams:
			p.setZone(zBody)
			if p.params.edit && p.params.in != nil && by == max(0, p.h-chrome-1) {
				return p.params.in, x - 12, 0
			}
			return p.kvMouseField(p.params, x, by)
		case tabHeaders:
			p.setZone(zBody)
			if p.headers.edit && p.headers.in != nil && by == max(0, p.h-chrome-1) {
				return p.headers.in, x - 12, 0
			}
			return p.kvMouseField(p.headers, x, by)
		case tabAuth:
			p.setZone(zBody)
			return p.formMouseField(&p.auth.f, p.auth.rows(p), x, by, p.w, max(1, p.h-chrome))
		case tabSettings:
			p.setZone(zBody)
			return p.formMouseField(&p.settings.f, p.settings.rows(p), x, by, p.w, max(1, p.h-chrome))
		}
	}
	return nil, 0, 0
}

func (p *Pane) formMouseField(f *form, rows []formRow, x, y, w, h int) (*vimfield.Field, int, int) {
	labelW := min(20, max(10, w/3))
	valueX := 2 + labelW + 1
	if x < valueX || x >= w || y < 0 || y >= h || len(rows) == 0 {
		return nil, 0, 0
	}
	f.clamp(rows)
	top := scrollTo(f.top, f.cur, h, len(rows))
	row := top + y
	if row >= len(rows) || rows[row].kind != rowText {
		return nil, 0, 0
	}
	if !f.edit || f.cur != row || f.in == nil {
		f.edit = false
		f.cur = row
		f.beginEdit(p, rows[row], "")
	}
	return f.in, x - valueX, 0
}

func (p *Pane) kvMouseField(k *kvView, x, y int) (*vimfield.Field, int, int) {
	items := k.items(p)
	w, h := p.bodySize()
	rowsH := max(1, h-3)
	row := y - 1
	top := scrollTo(k.top, k.cur, rowsH, len(items))
	if row < 0 || top+row >= len(items) || x < 6 {
		return nil, 0, 0
	}
	target := top + row
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
	valueW := max(1, rest)
	if descW > 0 {
		valueW = max(1, rest-descW-ui.SpaceSM)
	}
	valueX := 6 + keyW + ui.SpaceSM
	if x < valueX {
		col := 0
		if items[target].path {
			col = 1
		}
		if k.edit && k.in != nil && k.cur == target && k.col == col {
			return k.in, x - 6, 0
		}
		k.cur = target
		k.beginEdit(p, items, 0, "")
		return k.in, x - 6, 0
	}
	if x >= valueX+valueW {
		return nil, 0, 0
	}
	if k.edit && k.in != nil && k.cur == target && k.col == 1 {
		return k.in, x - valueX, 0
	}
	k.cur = target
	k.beginEdit(p, items, 1, "")
	return k.in, x - valueX, 0
}

// wheelLines is how many lines one wheel notch scrolls the composer.
const wheelLines = 3

// scrollWheel scrolls what is under the pointer. A field being typed into
// (insert or visual mode), an open prompt (log search, template name) and the
// confirm dialog own their input, so the wheel does nothing then: it never
// moves or commits a field. The composer scrolls its view whether or not it has
// the cursor (ScrollLines never edits or changes the mode); the log, the
// message detail and the other tabs' lists move as the arrow keys do, unless
// one of their cells is open.
func (p *Pane) scrollWheel(delta, x, y int) tea.Cmd {
	if p.confirm != nil || p.searching || p.rename != nil || p.typing() {
		return nil
	}
	key := tea.KeyDown
	if delta < 0 {
		key = tea.KeyUp
	}
	km := tea.KeyPressMsg{Code: key}
	if p.tab == tabMessages {
		if p.overComposer(x, y) {
			p.fitComposer()
			p.comp.ScrollLines(delta * wheelLines)
			return nil
		}
		if p.detail != nil {
			return p.detailKey(km)
		}
		return p.logKey(km)
	}
	if p.cellOpen() {
		return nil
	}
	return p.bodyKey(km)
}

// typing reports whether the focused field takes plain keys as text.
func (p *Pane) typing() bool {
	f := p.activeField()
	return f != nil && f.Typing()
}

// cellOpen reports whether the current tab has a cell open for editing.
func (p *Pane) cellOpen() bool {
	switch p.tab {
	case tabParams:
		return p.params.editing()
	case tabHeaders:
		return p.headers.editing()
	case tabAuth:
		return p.auth.editing()
	case tabSettings:
		return p.settings.editing()
	}
	return false
}

// overComposer reports whether the pointer (pane coordinates) is on the
// composer of the Messages tab.
func (p *Pane) overComposer(x, y int) bool {
	w, h := p.bodySize()
	g := msgLayout(w, h, len(p.messages()))
	by := y - chrome
	return by >= 0 && by < g.compH && (!g.wide || x < g.compW)
}
