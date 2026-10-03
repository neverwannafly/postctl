package vimtext

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/shubham/postctl/internal/ui"
)

func scrollModel(lines int) *Model {
	th := ui.NewTheme(true, true, false)
	m := New(th, Options{})
	var b strings.Builder
	for i := 0; i < lines; i++ {
		fmt.Fprintf(&b, "line %02d\n", i)
	}
	m.SetValue(strings.TrimSuffix(b.String(), "\n"))
	m.SetSize(20, 10)
	_ = m.FocusNormal()
	m.SetCursor(0, 3)
	return m
}

// ScrollLines moves the window like Ctrl-e and Ctrl-y: the cursor is pushed
// into the view, the text and the mode never change.
func TestScrollLinesMovesTheWindowAndKeepsTheCursorInside(t *testing.T) {
	m := scrollModel(50)
	before := m.Value()
	m.ScrollLines(5)
	if m.Top() != 5 {
		t.Fatalf("top after ScrollLines(5) = %d", m.Top())
	}
	row, col := m.Cursor()
	if row < m.Top()+2 || row > m.Top()+9-2 {
		t.Fatalf("cursor row %d is outside the view starting at %d", row, m.Top())
	}
	if col != 3 {
		t.Fatalf("the column drifted to %d", col)
	}
	m.ScrollLines(1000)
	if want := 50 - 10; m.Top() != want {
		t.Fatalf("top at the end = %d, want %d", m.Top(), want)
	}
	if row, _ := m.Cursor(); row < m.Top() {
		t.Fatalf("cursor row %d above the view", row)
	}
	m.ScrollLines(-1000)
	if m.Top() != 0 {
		t.Fatalf("top at the start = %d", m.Top())
	}
	if m.Value() != before {
		t.Fatal("scrolling edited the text")
	}
	if m.Mode() != Normal {
		t.Fatalf("scrolling changed the mode to %v", m.Mode())
	}
}

func TestScrollLinesKeepsVisualMode(t *testing.T) {
	m := scrollModel(50)
	m.Update(tea.KeyPressMsg{Code: 'v', Text: "v"})
	if m.Mode() != Visual {
		t.Fatal("v should start visual mode")
	}
	m.ScrollLines(20)
	if m.Mode() != Visual {
		t.Fatalf("mode after ScrollLines = %v, want visual", m.Mode())
	}
	if m.Top() != 20 {
		t.Fatalf("top = %d", m.Top())
	}
}

func TestScrollLinesNeverMovesAShortBuffer(t *testing.T) {
	m := scrollModel(4)
	m.ScrollLines(3)
	if m.Top() != 0 {
		t.Fatalf("a buffer that fits scrolled to %d", m.Top())
	}
	if row, _ := m.Cursor(); row != 0 {
		t.Fatalf("cursor moved to %d", row)
	}
	m.ScrollLines(0)
}

func TestScrollLinesDoesNotChangeWhatTheViewDrawsBesidesTheWindow(t *testing.T) {
	m := scrollModel(50)
	m.ScrollLines(7)
	v := m.View()
	if !strings.Contains(v, "line 08") || strings.Contains(v, "line 02") {
		t.Fatalf("the window did not follow the scroll:\n%s", v)
	}
	if m.Top() != 7 {
		t.Fatalf("drawing moved the window to %d", m.Top())
	}
}
