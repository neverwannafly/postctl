package screenfield

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/shubham/postctl/internal/tui/vimtext"
	"github.com/shubham/postctl/internal/ui"
)

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
	ed := vimtext.New(th, vimtext.Options{
		SingleLine:  true,
		StartInsert: false,
		Mask:        maskGlyph(th),
		Redact: func(s string) string {
			if s == "" {
				return ""
			}
			return fixedMask(th)
		},
		Clipboard: func(s string) tea.Cmd {
			clips = append(clips, s)
			return nil
		},
	})
	ed.SetValue(strings.Repeat("s", 30))
	ed.SetSize(40, 1)
	_ = ed.Focus()
	_, _ = ed.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	_, _ = ed.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
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
