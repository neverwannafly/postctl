package tui

import (
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/shubham/postctl/internal/model"
	"github.com/shubham/postctl/internal/ui"
)

type mouseState struct {
	lastTreeRow  model.ID
	lastTreeTime time.Time
	dragSlot     Slot
	dragging     bool
	dragX        int
	dragY        int
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
		if _, ok := a.ov.(*screenOverlay); ok {
			r.isModal = true
			r.overlay = rect{
				x: g.inset,
				y: headerRows,
				w: g.width,
				h: g.bodyH,
			}
		} else if d, ok := a.ov.(dialog); ok && d.isDialog() {
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
		return nil
	}
	a.ov = nil
	return nil
}

func (a *app) switcherSlotAt(relX int, slots []Slot, w int) (Slot, bool) {
	tabs := make([]ui.Tab, len(slots))
	for i, s := range slots {
		prefix := "  "
		if a.focus == s {
			prefix = a.theme.Glyphs.Select + " "
		}
		tabs[i] = ui.Tab{Label: prefix + "[" + strconv.Itoa(int(s)+1) + "] " + s.String()}
	}
	ranges := a.theme.TabRanges(w, tabs)
	for i, r := range ranges {
		nextStart := w
		if i+1 < len(ranges) {
			nextStart = ranges[i+1].Start
		}
		if relX >= r.Start && relX < nextStart {
			return slots[r.Index], true
		}
	}
	return SlotSidebar, false
}

func (a *app) onMouse(msg tea.MouseMsg) tea.Cmd {
	if !a.mouseEnabled() || a.phase != phaseReady {
		return nil
	}

	var isClick, isWheel, isMotion, isRelease bool
	var m tea.Mouse
	switch event := msg.(type) {
	case tea.MouseClickMsg:
		m = event.Mouse()
		if m.Button != tea.MouseLeft {
			return nil
		}
		isClick = true
		a.pend = nil
	case tea.MouseWheelMsg:
		m = event.Mouse()
		if m.Button != tea.MouseWheelUp && m.Button != tea.MouseWheelDown {
			return nil
		}
		isWheel = true
	case tea.MouseMotionMsg:
		m = event.Mouse()
		if (!a.mouse.dragging && a.ov == nil) || m.Button != tea.MouseLeft {
			return nil
		}
		isMotion = true
	case tea.MouseReleaseMsg:
		m = event.Mouse()
		if !a.mouse.dragging && a.ov == nil {
			return nil
		}
		isRelease = true
	default:
		return nil
	}

	x, y := m.X, m.Y
	if (isMotion || isRelease) && a.ov != nil {
		var paneCmd tea.Cmd
		if isRelease && a.mouse.dragging {
			switch a.mouse.dragSlot {
			case SlotRequest:
				if h, ok := a.request.(MouseHandler); ok {
					paneCmd = h.HandleMouse(msg, x-a.mouse.dragX, y-a.mouse.dragY)
				}
			case SlotResponse:
				if h, ok := a.response.(MouseHandler); ok {
					paneCmd = h.HandleMouse(msg, x-a.mouse.dragX, y-a.mouse.dragY)
				}
			}
			a.mouse.dragging = false
		}
		switch a.ov.(type) {
		case *promptOverlay, *pickerOverlay, *screenOverlay:
			return tea.Batch(paneCmd, a.updateOverlay(msg))
		default:
			return paneCmd
		}
	}
	if (isMotion || isRelease) && a.ov == nil {
		slot := a.mouse.dragSlot
		if isRelease {
			defer func() { a.mouse.dragging = false }()
		}
		switch slot {
		case SlotRequest:
			if h, ok := a.request.(MouseHandler); ok {
				return h.HandleMouse(msg, x-a.mouse.dragX, y-a.mouse.dragY)
			}
		case SlotResponse:
			if h, ok := a.response.(MouseHandler); ok {
				return h.HandleMouse(msg, x-a.mouse.dragX, y-a.mouse.dragY)
			}
		}
		return nil
	}
	regions := a.hitRegions()

	if regions.hasOverlay {
		if regions.overlay.contains(x, y) || isMotion || isRelease {
			return a.updateOverlay(msg)
		}
		if regions.isModal {
			return nil
		}
		if isClick {
			return a.closeOverlay()
		}
		return nil
	}

	if isWheel {
		delta := 1
		if m.Button == tea.MouseWheelUp {
			delta = -1
		}
		if regions.sidebar.contains(x, y) {
			oldSel := a.tree.sel
			a.tree.scroll(delta)
			if a.tree.sel != oldSel {
				return a.syncSelection(false)
			}
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
	}

	if isClick {
		if regions.switcher.contains(x, y) {
			relX := x - regions.switcher.x
			var slots []Slot
			if a.layoutKind() == ui.LayoutSingle {
				slots = []Slot{SlotSidebar, SlotRequest, SlotResponse}
			} else {
				slots = []Slot{SlotRequest, SlotResponse}
			}
			if slot, ok := a.switcherSlotAt(relX, slots, regions.switcher.w); ok {
				return a.setFocus(slot)
			}
			return nil
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
			a.mouse.dragSlot, a.mouse.dragging = SlotRequest, true
			a.mouse.dragX, a.mouse.dragY = regions.request.x, regions.request.y
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
			a.mouse.dragSlot, a.mouse.dragging = SlotResponse, true
			a.mouse.dragX, a.mouse.dragY = regions.response.x, regions.response.y
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
