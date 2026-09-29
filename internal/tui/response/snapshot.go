package response

import (
	"github.com/shubham/postctl/internal/model"
	"github.com/shubham/postctl/internal/tui"
	"github.com/shubham/postctl/internal/ui"
)

// SnapshotPart selects which response region to render without interaction.
type SnapshotPart int

// Snapshot parts for [Snapshot].
const (
	SnapshotBody SnapshotPart = iota
	SnapshotHeaders
	SnapshotTests
	SnapshotConsole
)

// Snapshot renders part of a completed send for history and runner detail views.
func Snapshot(th ui.Theme, r tui.SendResult, part SnapshotPart, w, h int) string {
	if h < 1 {
		h = 1
	}
	id := r.RequestID
	if id == "" {
		id = model.NewID()
	}
	p := &Pane{
		th:   th,
		w:    w,
		h:    h + 3,
		tab:  tabBody,
		byID: map[model.ID]*storedResult{id: {result: r}},
	}
	switch part {
	case SnapshotHeaders:
		p.tab = tabHeaders
		return p.viewHeaders(r, w, h)
	case SnapshotTests:
		p.tab = tabTests
		return p.viewTests(r, w, h)
	case SnapshotConsole:
		p.tab = tabConsole
		return p.viewConsole(r, w, h)
	default:
		return p.viewBody(r, w, h)
	}
}
