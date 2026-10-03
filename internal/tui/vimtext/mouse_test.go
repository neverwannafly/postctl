package vimtext

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/shubham/postctl/internal/ui"
)

func TestPosAt(t *testing.T) {
	tests := []struct {
		name       string
		opts       Options
		value      string
		sizeW      int
		sizeH      int
		cursorRow  int
		cursorCol  int
		scrollDown int
		x, y       int
		wantRow    int
		wantCol    int
		wantOK     bool
	}{
		{name: "plain", value: "abcd", sizeW: 8, sizeH: 1, x: 2, y: 0, wantCol: 2, wantOK: true},
		{name: "gutter", opts: Options{LineNumbers: true}, value: "abc\ndef", sizeW: 8, sizeH: 2, x: 3, y: 1, wantRow: 1, wantCol: 1, wantOK: true},
		{name: "gutter itself", opts: Options{LineNumbers: true}, value: "abc", sizeW: 8, sizeH: 1, x: 1, y: 0},
		{name: "wide first cell", value: "界x", sizeW: 8, sizeH: 1, x: 0, y: 0, wantCol: 0, wantOK: true},
		{name: "wide second cell", value: "界x", sizeW: 8, sizeH: 1, x: 1, y: 0, wantCol: 0, wantOK: true},
		{name: "tab", opts: Options{TabText: "    "}, value: "\tx", sizeW: 8, sizeH: 1, x: 3, y: 0, wantCol: 0, wantOK: true},
		{name: "mask one cell", opts: Options{Mask: "*"}, value: "界x", sizeW: 8, sizeH: 1, x: 1, y: 0, wantCol: 1, wantOK: true},
		{name: "normal clamps past eol", value: "abc", sizeW: 8, sizeH: 1, x: 7, y: 0, wantCol: 2, wantOK: true},
		{name: "insert clamps past eol", opts: Options{StartInsert: true}, value: "abc", sizeW: 8, sizeH: 1, x: 7, y: 0, wantCol: 3, wantOK: true},
		{name: "placeholder", opts: Options{Placeholder: "type"}, value: "", sizeW: 8, sizeH: 1, x: 3, y: 0, wantOK: true},
		{name: "vertical scroll", value: "aa\nbb\ncc\ndd", sizeW: 8, sizeH: 2, cursorRow: 3, scrollDown: 1, x: 1, y: 0, wantRow: 2, wantCol: 1, wantOK: true},
		{name: "horizontal scroll", value: "abcdef", sizeW: 3, sizeH: 1, cursorCol: 5, x: 0, y: 0, wantCol: 3, wantOK: true},
		{name: "outside x", value: "abc", sizeW: 3, sizeH: 1, x: 3, y: 0},
		{name: "outside y", value: "abc", sizeW: 3, sizeH: 1, x: 0, y: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := New(ui.NewTheme(true, true, false), tt.opts)
			m.SetValue(tt.value)
			m.SetSize(tt.sizeW, tt.sizeH)
			m.SetCursor(tt.cursorRow, tt.cursorCol)
			if tt.scrollDown != 0 {
				m.ScrollLines(tt.scrollDown)
			}
			row, col, ok := m.PosAt(tt.x, tt.y)
			if row != tt.wantRow || col != tt.wantCol || ok != tt.wantOK {
				t.Fatalf("PosAt(%d,%d) = (%d,%d,%v), want (%d,%d,%v)",
					tt.x, tt.y, row, col, ok, tt.wantRow, tt.wantCol, tt.wantOK)
			}
		})
	}
}

func TestMouseEdgeDragScrollsExactlyOneLinePerMotion(t *testing.T) {
	m := New(ui.NewTheme(true, true, false), Options{})
	m.SetValue(strings.Repeat("x\n", 20) + "x")
	m.SetSize(4, 4)
	m.SetCursor(8, 0)
	var mouse Mouse
	if !mouse.Press(m, 0, 1, time.Unix(1, 0)) {
		t.Fatal("press was not handled")
	}
	start := m.Top()
	for i := 1; i <= 3; i++ {
		mouse.Motion(0, 4)
		if got := m.Top(); got != start+i {
			t.Fatalf("downward motion %d moved top from %d to %d", i, start, got)
		}
	}
	for i := 1; i <= 3; i++ {
		mouse.Motion(0, -1)
		if got := m.Top(); got != start+3-i {
			t.Fatalf("upward motion %d moved top from %d to %d", i, start+3, got)
		}
	}
}

func TestPosAtCachesHugeSingleLineClusters(t *testing.T) {
	m := New(ui.NewTheme(true, true, false), Options{})
	m.SetValue(strings.Repeat("x", 100_000))
	m.SetSize(80, 1)
	m.PosAt(20, 0) // warm the hit-map cache
	if allocs := testing.AllocsPerRun(20, func() { m.PosAt(20, 0) }); allocs > 1 {
		t.Fatalf("cached PosAt allocated %.1f times per call", allocs)
	}
}

func TestViewIsPureAfterMousePressAndDrag(t *testing.T) {
	m := New(ui.NewTheme(true, true, false), Options{})
	m.SetValue("one two\nthree")
	m.SetSize(12, 2)
	var mouse Mouse
	mouse.Press(m, 1, 0, time.Unix(1, 0))
	m.opts.Clipboard, m.opts.Redact = nil, nil
	assertPure := func(t *testing.T) {
		t.Helper()
		beforeModel, beforeMouse := deepCopyModel(m), mouse
		first, second := m.View(), m.View()
		if first != second || !reflect.DeepEqual(beforeModel, *m) || !reflect.DeepEqual(beforeMouse, mouse) {
			t.Fatal("View mutated click or drag state")
		}
	}
	t.Run("press", assertPure)
	mouse.Motion(3, 1)
	t.Run("mid-drag", assertPure)
}
