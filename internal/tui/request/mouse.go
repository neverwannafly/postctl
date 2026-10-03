package request

import (
	tea "charm.land/bubbletea/v2"
	"github.com/shubham/postctl/internal/model"
	"github.com/shubham/postctl/internal/tui"
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
		if y == 2 || y == 3 {
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
	case tea.MouseWheelMsg:
		switch msg.Button {
		case tea.MouseWheelUp:
			p.scrollWheel(-1)
		case tea.MouseWheelDown:
			p.scrollWheel(1)
		}
	}
	return nil
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
