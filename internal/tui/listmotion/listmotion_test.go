package listmotion

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestKeyGgAndG(t *testing.T) {
	v := View{Cursor: 5}
	gKey := func(ch rune) tea.KeyPressMsg {
		s := string(ch)
		return tea.KeyPressMsg{Code: ch, Text: s}
	}
	if r := Key(gKey('g'), Config{Count: 10, Page: 3}, &v); !r.Handled {
		t.Fatal("first g should be handled")
	}
	if r := Key(gKey('g'), Config{Count: 10, Page: 3}, &v); !r.Moved || v.Cursor != 0 {
		t.Fatalf("gg -> cursor %d", v.Cursor)
	}
	if r := Key(gKey('G'), Config{Count: 10, Page: 3}, &v); !r.Moved || v.Cursor != 9 {
		t.Fatalf("G -> cursor %d", v.Cursor)
	}
}
