package tui

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/shubham/postctl/internal/model"
	"github.com/shubham/postctl/internal/ui"
)

type mouseState struct {
	lastTreeRow  model.ID
	lastTreeTime time.Time
}

type rect struct {
	x, y, w, h int
}

func (r rect) contains(x, y int) bool {
	return x >= r.x && x < r.x+r.w && y >= r.y && y < r.y+r.h
}

type paneHitRegions struct {
	sidebar       rect
	sidebarTitle  rect
	request       rect
	requestTitle  rect
	response      rect
	responseTitle rect
	switcher      rect
	overlay       rect
	hasOverlay    bool
	isModal       bool
}

func (a *app) mouseEnabled() bool {
	return !a.noMouse
}

func (a *app) hitRegions() paneHitRegions {
	g := a.geo()
	var r paneHitRegions

	if a.ov != nil {
		r.hasOverlay = true
		if d, ok := a.ov.(dialog); ok && d.isDialog() {
			r.isModal = true
			box := a.ov.view(a, g.width, g.bodyH)
			boxLines := strings.Split(strings.Trim(box, "\n"), "\n")
			bw := 0
			for _, l := range boxLines {
				bw = max(bw, ui.Width(l))
			}
			bh := len(boxLines)
			r.overlay = rect{
				x: g.inset + max(0, (g.width-bw)/2),
				y: headerRows + max(0, (g.bodyH-bh)/3),
				w: bw,
				h: bh,
			}
		} else {
			r.overlay = rect{
				x: g.inset,
				y: headerRows,
				w: g.width,
				h: g.bodyH,
			}
		}
		return r
	}

	contentH := g.bodyH - 1
	bodyY := headerRows + 1

	switch g.layout {
	case ui.LayoutThreePane:
		r.sidebarTitle = rect{x: g.inset, y: headerRows, w: g.sidebarW, h: 1}
		r.sidebar = rect{x: g.inset, y: bodyY, w: g.sidebarW, h: contentH}

		reqX := g.inset + g.sidebarW + gapWidth
		r.requestTitle = rect{x: reqX, y: headerRows, w: g.requestW, h: 1}
		r.request = rect{x: reqX, y: bodyY, w: g.requestW, h: contentH}

		if !a.responseHidden() {
			respX := reqX + g.requestW + gapWidth
			r.responseTitle = rect{x: respX, y: headerRows, w: g.responseW, h: 1}
			r.response = rect{x: respX, y: bodyY, w: g.responseW, h: contentH}
		}
	case ui.LayoutTwoPane:
		r.sidebarTitle = rect{x: g.inset, y: headerRows, w: g.sidebarW, h: 1}
		r.sidebar = rect{x: g.inset, y: bodyY, w: g.sidebarW, h: contentH}

		rightX := g.inset + g.sidebarW + gapWidth
		r.switcher = rect{x: rightX, y: headerRows, w: g.requestW, h: 1}

		right := a.rightShow
		if a.focus != SlotSidebar {
			right = a.focus
		}
		if a.responseHidden() || right == SlotRequest {
			r.request = rect{x: rightX, y: bodyY, w: g.requestW, h: contentH}
		} else {
			r.response = rect{x: rightX, y: bodyY, w: g.requestW, h: contentH}
		}
	default:
		r.switcher = rect{x: g.inset, y: headerRows, w: g.width, h: 1}
		contentRect := rect{x: g.inset, y: bodyY, w: g.width, h: contentH}
		switch a.focus {
		case SlotSidebar:
			r.sidebar = contentRect
		case SlotRequest:
			r.request = contentRect
		case SlotResponse:
			r.response = contentRect
		}
	}
	return r
}

func (a *app) closeOverlay() tea.Cmd {
	if a.ov == nil {
		return nil
	}
	if _, ok := a.ov.(*screenOverlay); ok {
		a.ov = nil
		return a.afterScreen()
	}
	a.ov = nil
	return nil
}

func (a *app) onMouse(msg tea.MouseMsg) tea.Cmd {
	if !a.mouseEnabled() {
		return nil
	}
	m := msg.Mouse()
	x, y := m.X, m.Y
	regions := a.hitRegions()

	if regions.hasOverlay {
		if regions.overlay.contains(x, y) {
			return a.updateOverlay(msg)
		}
		if regions.isModal {
			return nil
		}
		return a.closeOverlay()
	}

	switch m.Button {
	case tea.MouseWheelUp, tea.MouseWheelDown:
		delta := 1
		if m.Button == tea.MouseWheelUp {
			delta = -1
		}
		if regions.sidebar.contains(x, y) {
			a.tree.scroll(delta)
			return nil
		}
		if regions.request.contains(x, y) {
			if h, ok := a.request.(MouseHandler); ok {
				return h.HandleMouse(msg, x-regions.request.x, y-regions.request.y)
			}
			return nil
		}
		if regions.response.contains(x, y) {
			if h, ok := a.response.(MouseHandler); ok {
				return h.HandleMouse(msg, x-regions.response.x, y-regions.response.y)
			}
			return nil
		}
		return nil

	case tea.MouseLeft:
		if regions.switcher.contains(x, y) {
			relX := x - regions.switcher.x
			if a.layoutKind() == ui.LayoutSingle {
				if relX < 19 {
					return a.setFocus(SlotSidebar)
				}
				if relX < 34 {
					return a.setFocus(SlotRequest)
				}
				return a.setFocus(SlotResponse)
			}
			// TwoPane
			if relX < 15 {
				return a.setFocus(SlotRequest)
			}
			return a.setFocus(SlotResponse)
		}
		if regions.sidebarTitle.contains(x, y) {
			return a.setFocus(SlotSidebar)
		}
		if regions.requestTitle.contains(x, y) {
			return a.setFocus(SlotRequest)
		}
		if regions.responseTitle.contains(x, y) {
			return a.setFocus(SlotResponse)
		}
		if regions.sidebar.contains(x, y) {
			return a.handleTreeClick(x-regions.sidebar.x, y-regions.sidebar.y)
		}
		if regions.request.contains(x, y) {
			var cmds []tea.Cmd
			if a.focus != SlotRequest {
				cmds = append(cmds, a.setFocus(SlotRequest))
			}
			if h, ok := a.request.(MouseHandler); ok {
				if cmd := h.HandleMouse(msg, x-regions.request.x, y-regions.request.y); cmd != nil {
					cmds = append(cmds, cmd)
				}
			}
			return tea.Batch(cmds...)
		}
		if regions.response.contains(x, y) {
			var cmds []tea.Cmd
			if a.focus != SlotResponse {
				cmds = append(cmds, a.setFocus(SlotResponse))
			}
			if h, ok := a.response.(MouseHandler); ok {
				if cmd := h.HandleMouse(msg, x-regions.response.x, y-regions.response.y); cmd != nil {
					cmds = append(cmds, cmd)
				}
			}
			return tea.Batch(cmds...)
		}
	}
	return nil
}

func (a *app) handleTreeClick(treeX, treeY int) tea.Cmd {
	var cmds []tea.Cmd
	if a.focus != SlotSidebar {
		cmds = append(cmds, a.setFocus(SlotSidebar))
	}
	idx, row, ok := a.tree.rowAt(treeY)
	if !ok || !selectable(row) {
		return tea.Batch(cmds...)
	}
	if a.tree.foldGlyphHit(row, treeX) {
		a.tree.setOpen(row.id, !row.open)
		cmds = append(cmds, a.syncSelection(false))
		return tea.Batch(cmds...)
	}
	now := a.now()
	isDoubleClick := a.mouse.lastTreeRow == row.id && !a.mouse.lastTreeTime.IsZero() && now.Sub(a.mouse.lastTreeTime) <= 400*time.Millisecond
	alreadySelected := a.tree.cursor == idx

	if alreadySelected || isDoubleClick {
		a.tree.cursor = idx
		a.tree.commit()
		a.mouse.lastTreeRow = ""
		a.mouse.lastTreeTime = time.Time{}
		cmds = append(cmds, a.openRow())
		return tea.Batch(cmds...)
	}

	a.tree.cursor = idx
	a.tree.commit()
	a.mouse.lastTreeRow = row.id
	a.mouse.lastTreeTime = now
	cmds = append(cmds, a.syncSelection(false))
	return tea.Batch(cmds...)
}
