package listmotion

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestEmptyListConsumesMotion(t *testing.T) {
	v := &View{Cursor: 3, Top: 2}
	r := Key(tea.KeyPressMsg{Code: 'j', Text: "j"}, Config{Count: 0, Page: 5}, v)
	if !r.Handled || r.Moved {
		t.Fatalf("empty list: handled=%v moved=%v", r.Handled, r.Moved)
	}
}

func TestCursorClampedAfterShrink(t *testing.T) {
	v := &View{Cursor: 9, Top: 8}
	r := Key(tea.KeyPressMsg{Code: 'j', Text: "j"}, Config{Count: 3, Page: 2}, v)
	if v.Cursor != 2 {
		t.Fatalf("cursor=%d want 2 after shrink", v.Cursor)
	}
	if !r.Moved {
		t.Fatal("shrink clamp must report Moved")
	}
}
