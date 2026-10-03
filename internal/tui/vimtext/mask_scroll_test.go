package vimtext

import (
	"strings"
	"testing"

	"github.com/shubham/postctl/internal/ui"
)

func TestMaskedLongValueVisibleAtNarrowWidth(t *testing.T) {
	th := ui.NewTheme(true, true, false)
	mask := "*"
	if !th.ASCII {
		cs := clusters(th.Glyphs.Mask)
		if len(cs) > 0 {
			mask = cs[0]
		}
	}
	m := New(th, Options{SingleLine: true, StartInsert: true, Mask: mask})
	m.SetValue(strings.Repeat("a", 30))
	m.SetSize(10, 1)
	_ = m.Focus()
	if strings.Count(m.View(), mask) < 3 {
		t.Fatalf("expected visible mask glyphs at width 10, got:\n%s", m.View())
	}
}
