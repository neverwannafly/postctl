package diffview

import (
	"errors"
	"math/rand"
	"slices"
	"strconv"
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
		line := "line-" + strconv.Itoa(i)
		a.WriteString(line)
		if i%100 == 0 {
			sb.WriteString("changed-" + strconv.Itoa(i))
		} else {
			sb.WriteString(line)
		}
	}
	left, right := a.String(), sb.String()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = Diff(left, right)
	}
}

func BenchmarkDiffWorstCase5kLines(b *testing.B) {
	const n = 5000
	var left, right strings.Builder
	for i := 0; i < n; i++ {
		if i > 0 {
			left.WriteByte('\n')
			right.WriteByte('\n')
		}
		left.WriteString("left-" + strconv.Itoa(i))
		right.WriteString("right-" + strconv.Itoa(i))
	}
	a, z := left.String(), right.String()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, err := Compute(a, z)
		var tooBig DiffTooBig
		if !errors.As(err, &tooBig) {
			b.Fatal("worst-case diff did not hit edit cap")
		}
	}
}

func TestShortestScriptRandomizedIsMinimalAndReconstructs(t *testing.T) {
	rng := rand.New(rand.NewSource(73421))
	alphabet := [...]string{"a", "b", "c"}
	for test := 0; test < 2000; test++ {
		a := make([]string, rng.Intn(41))
		b := make([]string, rng.Intn(41))
		for i := range a {
			a[i] = alphabet[rng.Intn(len(alphabet))]
		}
		for i := range b {
			b[i] = alphabet[rng.Intn(len(alphabet))]
		}
		script := shortestScript(a, b)
		if script == nil {
			t.Fatalf("case %d unexpectedly exceeded cap", test)
		}
		edits := 0
		for _, op := range script {
			if op != opKeep {
				edits++
			}
		}
		want := len(a) + len(b) - 2*lcsLength(a, b)
		if edits != want {
			t.Fatalf("case %d edit count = %d, want %d", test, edits, want)
		}
		if !scriptReconstructs(a, b, script) {
			t.Fatalf("case %d script did not reconstruct right side", test)
		}
	}
}

func lcsLength(a, b []string) int {
	dp := make([]int, len(b)+1)
	for _, av := range a {
		prev := 0
		for j, bv := range b {
			old := dp[j+1]
			if av == bv {
				dp[j+1] = prev + 1
			} else {
				dp[j+1] = max(dp[j+1], dp[j])
			}
			prev = old
		}
	}
	return dp[len(b)]
}

func scriptReconstructs(a, b []string, script []editOp) bool {
	ai, bi := 0, 0
	out := make([]string, 0, len(b))
	for _, op := range script {
		switch op {
		case opKeep:
			if ai >= len(a) || bi >= len(b) || a[ai] != b[bi] {
				return false
			}
			out = append(out, a[ai])
			ai++
			bi++
		case opDel:
			if ai >= len(a) {
				return false
			}
			ai++
		case opAdd:
			if bi >= len(b) {
				return false
			}
			out = append(out, b[bi])
			bi++
		}
	}
	return ai == len(a) && bi == len(b) && slices.Equal(out, b)
}

func TestEditCapNoticeIncludesSummary(t *testing.T) {
	var left, right strings.Builder
	for i := 0; i < maxEditDistance/2+1; i++ {
		if i > 0 {
			left.WriteByte('\n')
			right.WriteByte('\n')
		}
		left.WriteString("left")
		right.WriteString("right")
	}
	m := New(ui.NewTheme(true, true, false),
		SideSummary{Label: "A", Status: "200"},
		SideSummary{Label: "B", Status: "500"},
		left.String(), right.String(),
	)
	m.SetSize(100, 30)
	view := m.View()
	if !strings.Contains(view, "Responses differ too much to diff line by line (2501 vs 2501 lines)") {
		t.Fatal("missing edit-cap notice")
	}
	if !strings.Contains(view, "A  200") || !strings.Contains(view, "B  500") {
		t.Fatal("missing header summary")
	}
	if strings.Contains(view, "No differences") {
		t.Fatal("edit-cap response must not claim there are no differences")
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
	m, _ = m.Update(tea.KeyPressMsg{Text: "N"})
	if m.HunkIndex() != before {
		t.Fatal("expected N to move to the previous hunk")
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
