package diffview

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/shubham/postctl/internal/ui"
)

func TestDiffAlgorithm(t *testing.T) {
	cases := []struct {
		name string
		a, b string
		want []LineKind
	}{
		{"empty", "", "", nil},
		{"identical", "a\nb", "a\nb", []LineKind{LineSame, LineSame}},
		{"insert", "a", "a\nb", []LineKind{LineSame, LineAdd}},
		{"delete", "a\nb", "a", []LineKind{LineSame, LineRemove}},
		{"replace", "a", "b", []LineKind{LineRemove, LineAdd}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			lines := Diff(tc.a, tc.b)
			if tc.want == nil {
				if len(lines) != 0 {
					t.Fatalf("lines = %v", lines)
				}
				return
			}
			if len(lines) != len(tc.want) {
				t.Fatalf("len = %d want %d: %v", len(lines), len(tc.want), lines)
			}
			for i, k := range tc.want {
				if lines[i].Kind != k {
					t.Fatalf("[%d] = %v want %v", i, lines[i].Kind, k)
				}
			}
		})
	}
}

func TestJSONPrettyPrintBothSides(t *testing.T) {
	a := `{"b":2,"a":1}`
	b := `{"a":1,"b":3}`
	m := New(ui.NewTheme(true, false, false),
		SideSummary{Label: "A", Status: "200"}, SideSummary{Label: "B", Status: "200"},
		a, b,
	)
	hasRemove, hasAdd := false, false
	for _, ln := range m.Lines {
		if ln.Kind == LineRemove {
			hasRemove = true
		}
		if ln.Kind == LineAdd {
			hasAdd = true
		}
	}
	if !hasRemove || !hasAdd {
		t.Fatalf("expected add/remove lines in %d rows", len(m.Lines))
	}
}

func TestCapTruncates(t *testing.T) {
	var lines string
	for i := 0; i < DefaultMaxLines+5; i++ {
		if i > 0 {
			lines += "\n"
		}
		lines += "x"
	}
	_, truncated := Cap(lines, DefaultMaxLines)
	if !truncated {
		t.Fatal("expected truncation")
	}
}

func TestHunkNavigation(t *testing.T) {
	m := New(ui.NewTheme(true, false, false),
		SideSummary{Label: "A", Status: "200"}, SideSummary{Label: "B", Status: "200"},
		"one", "two",
	)
	m.SetSize(80, 24)
	if len(m.Hunks) < 2 {
		t.Skip("need multiple hunks")
	}
	before := m.HunkIndex()
	m, _ = m.Update(tea.KeyPressMsg{Text: "n"})
	if m.HunkIndex() == before {
		t.Fatal("expected hunk index to change on n")
	}
}

func TestViewIsPure(t *testing.T) {
	m := New(ui.NewTheme(true, false, false),
		SideSummary{Label: "A", Status: "200"}, SideSummary{Label: "B", Status: "200"},
		"alpha", "beta",
	)
	m.SetSize(120, 40)
	m.scroll = 2
	m.hunk = 0
	snap := *m
	_ = m.View()
	_ = m.View()
	if m.scroll != snap.scroll || m.hunk != snap.hunk {
		t.Fatal("View mutated scroll state")
	}
}
