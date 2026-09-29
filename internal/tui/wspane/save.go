package wspane

import (
	"strconv"

	tea "charm.land/bubbletea/v2"
	"github.com/shubham/postctl/internal/model"
	"github.com/shubham/postctl/internal/tui"
)

func itoa(n int) string { return strconv.Itoa(n) }

func msgCmd(m tea.Msg) tea.Cmd { return func() tea.Msg { return m } }

// saveState tracks autosave for one request.
type saveState struct {
	coll    model.ID
	edits   uint64 // edits made
	flushed uint64 // edits covered by the last save request
	dirty   bool
	failed  bool
	saved   bool // at least one save completed this session
}

// autosaveMsg fires when the debounce elapses; it is stale if edits moved on.
type autosaveMsg struct {
	id    model.ID
	edits uint64
}

func (p *Pane) saveFor(id model.ID) *saveState {
	s := p.saves[id]
	if s == nil {
		s = &saveState{}
		p.saves[id] = s
	}
	return s
}

// touched marks the current request as edited: the tree shows the unsaved
// marker and autosave fires after the debounce.
func (p *Pane) touched() tea.Cmd {
	if p.req == nil || p.coll == nil {
		return nil
	}
	p.trimWS()
	id := p.req.ID
	s := p.saveFor(id)
	s.coll = p.coll.ID
	s.edits++
	s.failed = false
	var cmds []tea.Cmd
	if !s.dirty {
		s.dirty = true
		cmds = append(cmds, msgCmd(tui.DirtyMsg{ID: id, Dirty: true}))
	}
	cmds = append(cmds, p.deps.after(p.deps.debounce(), autosaveMsg{id: id, edits: s.edits}))
	return tea.Batch(cmds...)
}

func (p *Pane) onAutosave(m autosaveMsg) tea.Cmd {
	s := p.saves[m.id]
	if s == nil || !s.dirty || s.edits != m.edits || s.flushed == s.edits {
		return nil
	}
	return p.flush(m.id)
}

// flush asks the shell to write the request now.
func (p *Pane) flush(id model.ID) tea.Cmd {
	s := p.saves[id]
	if s == nil || !s.dirty {
		return nil
	}
	s.flushed = s.edits
	return msgCmd(tui.SaveMsg{Kind: tui.SaveRequest, Collection: s.coll, ID: id})
}

// BlurCmd is called by the shell when focus leaves the pane: unsaved edits are
// written immediately rather than waiting for the debounce.
func (p *Pane) BlurCmd() tea.Cmd {
	if p.req == nil {
		return nil
	}
	return p.flush(p.req.ID)
}

func (p *Pane) onSaveDone(m tui.SaveDoneMsg) tea.Cmd {
	if m.Kind != tui.SaveRequest {
		return nil
	}
	s := p.saves[m.ID]
	if s == nil {
		return nil
	}
	if m.Err != nil {
		s.failed = true
		return nil
	}
	s.failed = false
	s.saved = true
	if s.edits == s.flushed {
		s.dirty = false
		return msgCmd(tui.DirtyMsg{ID: m.ID, Dirty: false})
	}
	return nil
}

// dirtyFor reports whether a request has edits not yet on disk.
func (p *Pane) dirtyFor(id model.ID) bool {
	s := p.saves[id]
	return s != nil && s.dirty
}

// saveLabel is the quiet autosave state for the status row.
func (p *Pane) saveLabel() (text string, failed bool) {
	if p.req == nil {
		return "", false
	}
	s := p.saves[p.req.ID]
	switch {
	case s == nil:
		return "", false
	case s.failed:
		return p.th.Glyphs.Fail + " Not saved", true
	case s.dirty && s.flushed == s.edits:
		return p.th.Glyphs.Ellipsis + " Saving", false
	case s.dirty:
		return "Unsaved", false
	case s.saved:
		return "Saved", false
	}
	return "", false
}
