package vimtext

import (
	"time"

	tea "charm.land/bubbletea/v2"
)

// Mouse tracks click counts and one drag directly on a Model. It is useful to
// small wrappers that do not need vimfield's rendering helpers.
type Mouse struct {
	active               *Model
	anchorRow, anchorCol int
	last                 *Model
	lastRow, lastCol     int
	lastClick            time.Time
	clicks               int
	moved                bool
}

// Active reports whether a press is awaiting release.
func (m *Mouse) Active() bool { return m.active != nil }

// Press places the cursor and begins a possible drag.
func (m *Mouse) Press(ed *Model, x, y int, now time.Time) bool {
	row, col, ok := ed.PosAt(x, y)
	if !ok {
		return false
	}
	if m.last == ed && m.lastRow == row && m.lastCol == col &&
		!m.lastClick.IsZero() && now.Sub(m.lastClick) <= 400*time.Millisecond {
		m.clicks++
		if m.clicks > 3 {
			m.clicks = 1
		}
	} else {
		m.clicks = 1
	}
	m.last, m.lastRow, m.lastCol, m.lastClick = ed, row, col, now
	m.active, m.anchorRow, m.anchorCol, m.moved = ed, row, col, false
	keepInsert := ed.Focused() && ed.Mode() == Insert && m.clicks == 1
	if !keepInsert {
		_ = ed.FocusNormal()
	} else {
		_ = ed.Focus()
	}
	ed.SetCursor(row, col)
	switch m.clicks {
	case 2:
		_, _ = ed.Update(tea.KeyPressMsg(tea.Key{Code: 'v', Text: "v"}))
		_, _ = ed.Update(tea.KeyPressMsg(tea.Key{Code: 'i', Text: "i"}))
		_, _ = ed.Update(tea.KeyPressMsg(tea.Key{Code: 'w', Text: "w"}))
	case 3:
		_, _ = ed.Update(tea.KeyPressMsg(tea.Key{Code: 'V', Text: "V"}))
	}
	return true
}

// Motion extends a single-click press as a charwise selection.
func (m *Mouse) Motion(x, y int) bool {
	ed := m.active
	if ed == nil || m.clicks != 1 {
		return false
	}
	if ed.opts.SingleLine {
		if x < 0 {
			ed.ScrollColumns(-1)
		} else if x >= ed.width {
			ed.ScrollColumns(1)
		}
	} else if y < 0 {
		ed.ScrollLines(-1)
	} else if y >= ed.height {
		ed.ScrollLines(1)
	}
	x = min(max(0, x), ed.width-1)
	y = min(max(0, y), ed.height-1)
	row, col, ok := ed.PosAt(x, y)
	if !ok {
		return false
	}
	if !m.moved && row == m.anchorRow && col == m.anchorCol {
		return true
	}
	m.moved = true
	ed.Select(m.anchorRow, m.anchorCol, row, col)
	return true
}

// Release ends the gesture and keeps any visual selection.
func (m *Mouse) Release() bool {
	if m.active == nil {
		return false
	}
	m.active = nil
	return true
}
