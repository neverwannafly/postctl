package tui

import (
	tea "charm.land/bubbletea/v2"
	"github.com/shubham/postctl/internal/ui"
)

// Direction is where Ctrl-h, Ctrl-j, Ctrl-k and Ctrl-l move focus.
type Direction int

// Directions, in the order of the keys h, j, k, l.
const (
	// DirLeft is Ctrl-h.
	DirLeft Direction = iota
	// DirDown is Ctrl-j.
	DirDown
	// DirUp is Ctrl-k.
	DirUp
	// DirRight is Ctrl-l.
	DirRight
)

// String returns the lower-case direction name.
func (d Direction) String() string {
	switch d {
	case DirLeft:
		return "left"
	case DirDown:
		return "down"
	case DirUp:
		return "up"
	default:
		return "right"
	}
}

// neighbor returns the slot next to from in direction dir for the layout in
// effect. The geometry is: three panes sit side by side (Sidebar, Request,
// Response); in the two-pane layout the sidebar is on the left and Request
// sits above Response on the right (only one is drawn at a time, but they are
// neighbours); the single-pane layout treats the sidebar overlay as the left
// neighbour of Request, with Response below it.
func (a *app) neighbor(from Slot, dir Direction) (Slot, bool) {
	hasResp := !a.responseHidden()
	switch a.layoutKind() {
	case ui.LayoutThreePane:
		order := []Slot{SlotSidebar, SlotRequest}
		if hasResp {
			order = append(order, SlotResponse)
		}
		return stepIn(order, from, dir, DirLeft, DirRight)
	default:
		// Two-pane and single-pane layouts share one topology.
		switch from {
		case SlotSidebar:
			if dir != DirRight {
				return from, false
			}
			return a.rightTarget(), true
		case SlotRequest:
			switch dir {
			case DirLeft:
				return SlotSidebar, true
			case DirDown:
				return SlotResponse, hasResp
			}
		case SlotResponse:
			switch dir {
			case DirLeft:
				return SlotSidebar, true
			case DirUp:
				return SlotRequest, true
			}
		}
		return from, false
	}
}

// rightTarget is where moving right from the sidebar lands in the stacked
// layouts: the pane that was on screen before the sidebar took focus.
func (a *app) rightTarget() Slot {
	if a.layoutKind() == ui.LayoutTwoPane {
		return a.rightShow
	}
	if a.prevFocus != SlotSidebar && (a.prevFocus != SlotResponse || !a.responseHidden()) {
		return a.prevFocus
	}
	return SlotRequest
}

// stepIn moves one place along order for the two horizontal directions.
func stepIn(order []Slot, from Slot, dir, back, fwd Direction) (Slot, bool) {
	for i, s := range order {
		if s != from {
			continue
		}
		switch dir {
		case back:
			if i > 0 {
				return order[i-1], true
			}
		case fwd:
			if i+1 < len(order) {
				return order[i+1], true
			}
		}
	}
	return from, false
}

// movePane moves focus to the neighbouring pane, or hands the move to
// Options.OnEdge when postctl has no pane there.
func (a *app) movePane(dir Direction) tea.Cmd {
	if next, ok := a.neighbor(a.focus, dir); ok {
		return a.setFocus(next)
	}
	if a.opts.OnEdge != nil {
		return a.opts.OnEdge(dir)
	}
	return nil
}
