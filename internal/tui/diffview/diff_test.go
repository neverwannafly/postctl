package diffview

import (
	"strings"
	"testing"
	"time"

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
		{"insert", "a", "a\nb", []LineKind{LineHunkHeader, LineSame, LineAdd}},
		{"delete", "a\nb", "a", []LineKind{LineHunkHeader, LineSame, LineRemove}},
		{"replace", "a", "b", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			lines := Diff(tc.a, tc.b)
			if tc.name == "replace" {
				if len(lines) != 3 || lines[0].Kind != LineHunkHeader {
					t.Fatalf("replace: %v", lines)
				}
				var add, remove int
				for _, ln := range lines[1:] {
					switch ln.Kind {
					case LineAdd:
						add++
					case LineRemove:
						remove++
					}
				}
				if add != 1 || remove != 1 {
					t.Fatalf("replace kinds: %v", lines)
				}
				return
			}
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

func TestDiffLargeInputsBounded(t *testing.T) {
	const n = 3000
	var a, b strings.Builder
	for i := 0; i < n; i++ {
		if i > 0 {
			a.WriteByte('\n')
			b.WriteByte('\n')
		}
		a.WriteString("line")
		if i == n/2 {
			b.WriteString("LINE")
		} else {
			b.WriteString("line")
		}
	}
	start := time.Now()
	if allocs := testing.AllocsPerRun(3, func() {
		_ = Diff(a.String(), b.String())
	}); allocs > 500_000 {
		t.Fatalf("allocs = %v, want well below O(n*m)", allocs)
	}
	if elapsed := time.Since(start); elapsed > 30*time.Second {
		t.Fatalf("diff took %v", elapsed)
	}
}

func BenchmarkDiff20kLines(b *testing.B) {
	const n = 20000
	var a, sb strings.Builder
	for i := 0; i < n; i++ {
		if i > 0 {
			a.WriteByte('\n')
			sb.WriteByte('\n')
		}
		a.WriteString("line")
		if i == n/2 {
			sb.WriteString("LINE")
		} else {
			sb.WriteString("line")
		}
	}
	left, right := a.String(), sb.String()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = Diff(left, right)
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

func TestHunkHeaderCoordinates(t *testing.T) {
	cases := []struct {
		name string
		a, b string
		want string
	}{
		{
			name: "start",
			a:    "a\nb\nc",
			b:    "A\nb\nc",
			want: "@@ -1,",
		},
		{
			name: "middle",
			a:    strings.Join([]string{"l1", "l2", "l3", "l4", "l5", "keep", "tail"}, "\n"),
			b:    strings.Join([]string{"l1", "l2", "l3", "l4", "L5", "keep", "tail"}, "\n"),
			want: "@@ -2,",
		},
		{
			name: "after_omitted_prefix",
			a:    strings.Repeat("same\n", 10) + "old\nmore",
			b:    strings.Repeat("same\n", 10) + "new\nmore",
			want: "@@ -8,",
		},
		{
			name: "end",
			a:    "a\nb\nc",
			b:    "a\nb\nC",
			want: "@@ -1,",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, ln := range Diff(tc.a, tc.b) {
				if ln.Kind == LineHunkHeader && strings.Contains(ln.Text, tc.want) {
					return
				}
			}
			var hdrs []string
			for _, ln := range Diff(tc.a, tc.b) {
				if ln.Kind == LineHunkHeader {
					hdrs = append(hdrs, ln.Text)
				}
			}
			t.Fatalf("missing hunk header prefix %q in %v", tc.want, hdrs)
		})
	}
}

func TestHunkNavigation(t *testing.T) {
	// Two hunks separated by unchanged lines (no context merge).
	var a, b strings.Builder
	for i := 0; i < 20; i++ {
		if i > 0 {
			a.WriteByte('\n')
			b.WriteByte('\n')
		}
		a.WriteString("same")
		b.WriteString("same")
	}
	a.WriteString("\nfirst-old\n")
	b.WriteString("\nfirst-new\n")
	for i := 0; i < 10; i++ {
		a.WriteString("same\n")
		b.WriteString("same\n")
	}
	a.WriteString("second-old")
	b.WriteString("second-new")
	m := New(ui.NewTheme(true, false, false),
		SideSummary{Label: "A", Status: "200"}, SideSummary{Label: "B", Status: "200"},
		a.String(), b.String(),
	)
	m.SetSize(80, 24)
	if len(m.Hunks) < 2 {
		t.Fatalf("need multiple hunks, got %d", len(m.Hunks))
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
