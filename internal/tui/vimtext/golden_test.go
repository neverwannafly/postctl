package vimtext

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/shubham/postctl/internal/ui"
)

var updateGoldens = flag.Bool("update", false, "rewrite vimtext golden files")

func TestGoldenViews(t *testing.T) {
	looks := []struct {
		name string
		th   ui.Theme
	}{
		{"dark", ui.NewTheme(true, false, false)},
		{"nocolor", ui.NewTheme(true, true, false)},
		{"ascii", ui.NewTheme(true, true, true)},
	}
	sizes := []struct {
		name string
		w, h int
	}{
		{"40x5", 40, 5},
		{"80x12", 80, 12},
	}
	for _, look := range looks {
		for _, size := range sizes {
			t.Run(look.name+"/"+size.name, func(t *testing.T) {
				got := trimGolden(renderGoldenStates(t, look.th, size.w, size.h))
				path := filepath.Join("testdata", "golden", look.name+"_"+size.name+".golden")
				if *updateGoldens {
					if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
						t.Fatal(err)
					}
				}
				want, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if got != string(want) {
					t.Fatalf("golden mismatch for %s (run go test ./internal/tui/vimtext -update)", path)
				}
			})
		}
	}
}

func trimGolden(s string) string {
	lines := strings.Split(s, "\n")
	for i := range lines {
		lines[i] = strings.TrimRight(lines[i], " ")
	}
	return strings.Join(lines, "\n")
}

func renderGoldenStates(t *testing.T, th ui.Theme, w, h int) string {
	t.Helper()
	type state struct {
		name string
		make func() *Model
	}
	base := func(opts Options, value string) *Model {
		m := New(th, opts)
		m.SetValue(value)
		m.SetSize(w, h)
		return m
	}
	states := []state{
		{"normal", func() *Model {
			m := base(Options{}, "alpha beta\ngamma delta")
			_ = m.Focus()
			m.SetCursor(0, 6)
			return m
		}},
		{"insert", func() *Model {
			m := base(Options{StartInsert: true}, "editable text")
			_ = m.Focus()
			m.SetCursor(0, 4)
			return m
		}},
		{"visual", func() *Model {
			m := base(Options{}, "select this range")
			_ = m.Focus()
			m.Select(0, 0, 0, 5)
			return m
		}},
		{"line numbers", func() *Model {
			m := base(Options{LineNumbers: true}, "first\nsecond\nthird")
			_ = m.Focus()
			m.SetCursor(1, 2)
			return m
		}},
		{"highlight", func() *Model {
			m := base(Options{Highlighter: ChromaHighlighter(th, "javascript")}, "const answer = 42;\nreturn answer;")
			_ = m.Focus()
			return m
		}},
		{"placeholder", func() *Model {
			return base(Options{Placeholder: "Type to filter"}, "")
		}},
		{"completion", func() *Model {
			m := base(Options{StartInsert: true, Completer: fakeCompleter{}}, "")
			_ = m.Focus()
			for _, msg := range parseKeys(t, "pr") {
				_, _ = m.Update(msg)
			}
			return m
		}},
	}
	var out strings.Builder
	for i, state := range states {
		if i > 0 {
			out.WriteString("\n\n")
		}
		out.WriteString("== " + state.name + " ==\n")
		out.WriteString(state.make().View())
	}
	return out.String()
}

func TestChromaHighlighterNoColorAndByteSpans(t *testing.T) {
	plain := ChromaHighlighter(ui.NewTheme(true, true, false), "json")
	if got := plain.Highlight([]string{`{"n": 1}`}); len(got) != 1 || len(got[0]) != 0 {
		t.Fatal("NO_COLOR highlighter returned spans")
	}
	color := ChromaHighlighter(ui.NewTheme(true, false, false), "json")
	got := color.Highlight([]string{`{"é": 1}`})
	if len(got) != 1 || len(got[0]) == 0 {
		t.Fatal("color highlighter returned no spans")
	}
}

func TestHorizontalAndVerticalScrollingKeepsCursorVisible(t *testing.T) {
	m := New(ui.NewTheme(true, true, false), Options{})
	m.SetValue(strings.Repeat("wide", 20) + "\n" + strings.Repeat("line\n", 10))
	m.SetSize(10, 4)
	_ = m.Focus()
	m.SetCursor(0, 60)
	if m.left == 0 {
		t.Fatal("horizontal offset did not advance")
	}
	_, _ = m.Update(tea.KeyPressMsg(tea.Key{Code: 'G', Text: "G"}))
	if m.top == 0 {
		t.Fatal("vertical offset did not advance")
	}
}
