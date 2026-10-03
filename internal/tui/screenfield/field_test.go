package screenfield

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/shubham/postctl/internal/tui/vimtext"
	"github.com/shubham/postctl/internal/ui"
)

func TestMousePlacesCursorAndKeepsInsertMode(t *testing.T) {
	f, _ := New(ui.NewTheme(true, true, false), "abcdef", "", false)
	f.SetWidth(10)
	now := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	if !f.HandleMouse(tea.MouseClickMsg{X: 3, Y: 0, Button: tea.MouseLeft}, 3, 0, now) {
		t.Fatal("click was not handled")
	}
	if f.Mode() != vimtext.Insert {
		t.Fatalf("click changed mode to %v", f.Mode())
	}
	f.HandleMouse(tea.MouseMotionMsg{X: 5, Y: 0, Button: tea.MouseLeft}, 5, 0, now)
	f.HandleMouse(tea.MouseReleaseMsg{X: 5, Y: 0, Button: tea.MouseLeft}, 5, 0, now)
	if f.Mode() != vimtext.Visual {
		t.Fatalf("drag mode = %v", f.Mode())
	}
}

func TestViewIsPureInEditMode(t *testing.T) {
	th := ui.NewTheme(true, true, false)
	f, _ := New(th, "hello", "", false)
	f.SetWidth(40)
	_ = f.Focus()
	_, _ = f.Update(tea.KeyPressMsg{Code: 'x', Text: "x"})
	val := f.Value()
	first, second := f.View(), f.View()
	if first != second || f.Value() != val {
		t.Fatal("View mutated the editor while editing")
	}
}

func TestYankMaskedSecretUsesFixedMask(t *testing.T) {
	th := ui.NewTheme(true, true, false)
	want := fixedMask(th)
	var clips []string
	f, _ := New(th, strings.Repeat("s", 30), "", true)
	f.SetWidth(40)
	f.SetClipboard(func(s string) tea.Cmd {
		clips = append(clips, s)
		return nil
	})
	_ = f.Focus()
	_, _ = f.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	_, _ = f.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	_, _ = f.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	if len(clips) != 1 || clips[0] != want {
		t.Fatalf("yank copied %q, want fixed mask", clips[0])
	}
}

func TestNarrowMaskedShowsGlyphsNotOnlyCursor(t *testing.T) {
	th := ui.NewTheme(true, true, false)
	g := maskGlyph(th)
	f, _ := New(th, strings.Repeat("a", 30), "", true)
	f.SetWidth(10)
	_ = f.Focus()
	view := f.View()
	if strings.Count(view, g) < 3 {
		t.Fatalf("narrow masked field showed %d mask glyphs, want several:\n%s", strings.Count(view, g), view)
	}
}
