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
			p.scrollWheel(-1, msg)
		case tea.MouseWheelDown:
			p.scrollWheel(1, msg)
		}
	}
	return nil
}

func (p *Pane) scrollWheel(delta int, msg tea.MouseMsg) {
	if p.Editing() {
		return
	}
	var key rune
	if delta < 0 {
		key = tea.KeyUp
	} else {
		key = tea.KeyDown
	}
	switch p.tab {
	case tabPre, tabTests:
		if sv, ok := p.views[p.tab].(*scriptView); ok && sv.ed != nil {
			sv.ed.update(msg)
			return
		}
	case tabBody:
		if bv, ok := p.views[tabBody].(*bodyView); ok {
			mode := bv.mode(p)
			if mode == model.BodyRaw && bv.raw != nil {
				bv.raw.update(msg)
				return
			}
			if mode == model.BodyGraphQL {
				if bv.gq != nil {
					bv.gq.update(msg)
					return
				}
			}
		}
	}
	if v := p.views[p.tab]; v != nil {
		_, changed, _ := v.key(p, tea.KeyPressMsg{Code: key})
		if changed {
			p.syncURL()
		}
	}
}
