package wspane

import (
	tea "charm.land/bubbletea/v2"
	"github.com/shubham/postctl/internal/tui"
	"github.com/shubham/postctl/internal/tui/vimfield"
	"github.com/shubham/postctl/internal/tui/vimtext"
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
		if p.activeField() == p.comp && p.overComposer(x, y) {
			delta := wheelLines
			if msg.Button == tea.MouseWheelUp {
				delta = -wheelLines
			}
			p.fitComposer()
			p.comp.ScrollLines(delta)
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

func (p *Pane) mouseFieldAt(x, y int) (*vimfield.Field, int, int) {
	const urlX = 2 + badgeW + 2
	if y == 0 && x >= urlX && x < urlX+p.urlWidth() {
		wasInsert := p.urlE && p.url.Mode() == vimtext.Insert
		p.stopEditing()
		p.setZone(zBar)
		if wasInsert {
			p.url.Focus()
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
			p.stopEditing()
			p.sect = sComposer
			p.setZone(zBody)
			p.fitComposer()
			return p.comp, x, y - chrome - 1
		}
	}
	return nil, 0, 0
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
