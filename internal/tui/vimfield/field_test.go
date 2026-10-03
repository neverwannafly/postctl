package vimfield

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/shubham/postctl/internal/tui/vimtext"
	"github.com/shubham/postctl/internal/ui"
)

func theme() ui.Theme { return ui.NewTheme(true, true, false) }

func text(s string) tea.KeyPressMsg {
	r := []rune(s)
	return tea.KeyPressMsg(tea.Key{Code: r[0], Text: s})
}

func named(code rune) tea.KeyPressMsg { return tea.KeyPressMsg(tea.Key{Code: code}) }

func TestBeginStartsInNormalOrInsert(t *testing.T) {
	for _, tc := range []struct {
		start string
		mode  string
		typed string
		want  string
	}{
		{"", "NORMAL", "", "abc"},
		{"i", "INSERT", "X", "Xabc"},
		{"a", "INSERT", "X", "aXbc"},
		{"A", "INSERT", "X", "abcX"},
		{"I", "INSERT", "X", "Xabc"},
	} {
		f := New(theme(), vimtext.Options{SingleLine: true})
		f.Set("abc")
		f.Fit(20, 1)
		f.Begin(tc.start)
		if f.ModeName() != tc.mode {
			t.Fatalf("start %q: mode %q, want %q", tc.start, f.ModeName(), tc.mode)
		}
		if tc.typed != "" {
			f.Update(text(tc.typed))
		}
		if f.Text() != tc.want {
			t.Fatalf("start %q: text %q, want %q", tc.start, f.Text(), tc.want)
		}
	}
}

func TestTypingIsModeAware(t *testing.T) {
	f := New(theme(), vimtext.Options{SingleLine: true})
	if f.Typing() || f.ModeName() != "" {
		t.Fatal("an unfocused field is not typing")
	}
	f.Begin("")
	if f.Typing() {
		t.Fatal("normal mode must not count as typing")
	}
	f.Update(text("i"))
	if !f.Typing() || f.ModeName() != "INSERT" {
		t.Fatalf("insert: typing=%v mode=%q", f.Typing(), f.ModeName())
	}
	f.Update(named(tea.KeyEscape))
	f.Update(text("v"))
	if !f.Typing() || f.ModeName() != "VISUAL" {
		t.Fatalf("visual: typing=%v mode=%q", f.Typing(), f.ModeName())
	}
}

func TestUpdateReportsChangesAndEvents(t *testing.T) {
	f := New(theme(), vimtext.Options{SingleLine: true})
	f.Begin("i")
	if r := f.Update(text("a")); !r.Changed {
		t.Fatal("typing must report a change")
	}
	f.Update(named(tea.KeyEscape))
	if r := f.Update(named(tea.KeyEnter)); r.Event.Kind != vimtext.Submit || r.Changed {
		t.Fatalf("enter in a single-line field: %+v", r)
	}
	if r := f.Update(named(tea.KeyEscape)); r.Event.Kind != vimtext.Leave {
		t.Fatalf("esc in normal: %+v", r)
	}
	if r := f.Update(text(" ")); r.Event.Kind != vimtext.Unhandled {
		t.Fatalf("space in normal must be handed to the parent: %+v", r)
	}
	f.Update(text("g"))
	if r := f.Update(text("u")); r.Event.Kind != vimtext.Seq || r.Event.Seq != "gu" {
		t.Fatalf("g u: %+v", r)
	}
}

func TestRenderIsPure(t *testing.T) {
	f := New(theme(), vimtext.Options{LineNumbers: true})
	f.Set(strings.Repeat("line of text\n", 40))
	f.Fit(30, 5)
	f.Begin("")
	f.MoveEnd()
	row, col := f.Cursor()
	first, second := f.Render(30, 5), f.Render(30, 5)
	// A different size must not leak back into the field either.
	_ = f.Render(10, 2)
	third := f.Render(30, 5)
	if first != second || first != third {
		t.Fatal("rendering is not repeatable")
	}
	if r, c := f.Cursor(); r != row || c != col || f.Text() != strings.Repeat("line of text\n", 40) {
		t.Fatal("rendering changed the field")
	}
}

func TestMaskHidesText(t *testing.T) {
	f := New(theme(), vimtext.Options{SingleLine: true})
	f.Set("hunter2-sentinel")
	f.Fit(30, 1)
	f.SetMask(true)
	if out := ansi.Strip(f.View()); strings.Contains(out, "hunter2") {
		t.Fatalf("masked field leaked: %q", out)
	}
	f.SetMask(false)
	if out := ansi.Strip(f.View()); !strings.Contains(out, "hunter2") {
		t.Fatalf("unmasked field lost the text: %q", out)
	}
}

func TestVarHighlighterFlagsUnresolvedNames(t *testing.T) {
	th := ui.NewTheme(true, false, false)
	h := VarHighlighter(th, nil, func(name string) bool { return name == "known" })
	spans := h.Highlight([]string{"{{known}}/{{missing}}/{{$guid}}", "plain"})
	if len(spans) != 2 || len(spans[0]) != 3 || len(spans[1]) != 0 {
		t.Fatalf("spans = %v", spans)
	}
	warn := th.Style(ui.StatusWarning).Render("x")
	key := th.Style(ui.SyntaxKey).Render("x")
	got := func(i int) string { return spans[0][i].Style.Render("x") }
	if got(0) != key || got(1) != warn || got(2) != key {
		t.Fatal("known and dynamic names must be keys, unknown names warnings")
	}
	if spans[0][1].Start != len("{{known}}/") || spans[0][1].End != len("{{known}}/{{missing}}") {
		t.Fatalf("span offsets are not bytes within the line: %+v", spans[0][1])
	}
}

func TestReplacePutsTheCursorAtTheEnd(t *testing.T) {
	f := New(theme(), vimtext.Options{})
	f.Replace("a\nbc")
	if row, col := f.Cursor(); row != 1 || col != 1 {
		t.Fatalf("cursor = %d,%d", row, col)
	}
	if f.LineCount() != 2 {
		t.Fatalf("lines = %d", f.LineCount())
	}
}

func TestPasteInNormalModeInserts(t *testing.T) {
	f := New(theme(), vimtext.Options{SingleLine: true})
	f.Begin("")
	if r := f.Paste(tea.PasteMsg{Content: "https://example.test/a"}); !r.Changed || f.Text() != "https://example.test/a" {
		t.Fatalf("paste in normal mode: %+v text %q", r, f.Text())
	}
	if f.ModeName() != "INSERT" {
		t.Fatalf("mode after paste = %q", f.ModeName())
	}
}

func TestPureRef(t *testing.T) {
	for text, want := range map[string]bool{
		"{{token}}":        true,
		"  {{ token }}\t":  true,
		"{{$guid}}":        true,
		"":                 false,
		"{{}}":             false,
		"{{ }}":            false,
		"ab{{cd":           false,
		"ab{{cd}}":         false,
		"{{cd}}ab":         false,
		"{{a}}{{b}}":       false,
		"{{a}} {{b}}":      false,
		"{{a":              false,
		"a}}":              false,
		"Bearer {{token}}": false,
		"{{a{{b}}":         false,
	} {
		if got := PureRef(text); got != want {
			t.Errorf("PureRef(%d chars, %q) = %v, want %v", len(text), "shape", got, want)
		}
	}
}
