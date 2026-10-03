package wspane

import (
	tea "charm.land/bubbletea/v2"
	"github.com/shubham/postctl/internal/tui"
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
		if y == 2 || y == 3 {
			tabs := p.tabList()
			hit := p.th.TabAt(p.w, tabs, x)
			if hit >= 0 && hit < len(tabSections) {
				return p.onFocusSection(tui.FocusSectionMsg{Section: tabSections[hit]})
			}
		}
	case tea.MouseWheelMsg:
		switch msg.Button {
		case tea.MouseWheelUp:
			return p.scrollWheel(-1, x, y)
		case tea.MouseWheelDown:
			return p.scrollWheel(1, x, y)
		}
	}
	return nil
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
