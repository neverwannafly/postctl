package ui

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

var update = flag.Bool("update", false, "rewrite golden files")

// golden compares got with testdata/<name>.golden, or rewrites it with -update.
func golden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name+".golden")
	if *update {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("missing golden %s (run with -update): %v", path, err)
	}
	if string(want) != got {
		t.Errorf("%s differs from golden.\n--- got ---\n%s\n--- want ---\n%s", name, got, want)
	}
}

// gallery renders every component at width for a theme, with escape
// sequences removed, in a fixed order.
func gallery(th Theme, width int) string {
	var sb strings.Builder
	section := func(name, body string) {
		fmt.Fprintf(&sb, "## %s\n%s\n\n", name, body)
	}
	section("header staging", th.Header(width, HeaderInfo{Workspace: "payments-api", Env: EnvInfo{Name: "staging"}}))
	section("header prod", th.Header(width, HeaderInfo{Workspace: "payments-api", Env: EnvInfo{Name: "prod", Production: true}}))
	section("header none", th.Header(width, HeaderInfo{Workspace: "payments-api"}))
	section("header long", th.Header(width, HeaderInfo{Workspace: strings.Repeat("very-long-workspace-", 4), Env: EnvInfo{Name: "eu-west-live", Production: true}}))
	section("hints", th.KeyHints(width, []Hint{
		{"Ctrl-Enter", "Send", 0}, {"e", "Edit in $EDITOR", 3}, {"E", "Environments", 1},
		{"/", "Filter", 2}, {"Ctrl-P", "Palette", 1}, {"?", "Help", 0},
	}))
	section("methods", strings.Join([]string{
		th.MethodBadge("GET", 5) + " List charges",
		th.MethodBadge("POST", 5) + " Create",
		th.MethodBadge("PATCH", 5) + " Update",
		th.MethodBadge("DELETE", 5) + " Refund",
		th.MethodBadge("OPTIONS", 5) + " Preflight",
		th.MethodBadge("PURGE", 5) + " Custom",
	}, "\n"))
	section("status", strings.Join([]string{
		th.Status(StatusReady, "Ready"),
		th.Status(StatusRunning, "Waiting for response 1.2s"),
		th.Status(StatusAttention, "Slow response"),
		th.Status(StatusFailed, "Request failed"),
	}, "\n"))
	section("response chips", strings.Join([]string{
		th.ResponseChip(201, "Created", 184*time.Millisecond, 1229),
		th.ResponseChip(404, "Not Found", 2300*time.Millisecond, 92),
		th.ResponseChip(503, "Service Unavailable", 75*time.Second, 3<<20),
		th.ResponseChip(0, "connection refused", 0, 0),
	}, "\n"))
	section("kv table", th.KVTable([]KVRow{
		{Key: "Authorization", Value: "Bearer abc", Secret: true, Description: "Bearer token for the API"},
		{Key: "Content-Type", Value: "application/json"},
		{Key: "X-Debug", Value: "1", Disabled: true, Description: "Turn on verbose logs"},
	}, KVTableOptions{Width: width, Focus: 1, Header: true}))
	section("tabs", th.Tabs(width, []Tab{{Label: "Params"}, {Label: "Auth"}, {Label: "Headers", Count: 3}, {Label: "Body"}, {Label: "Pre", Dot: true}, {Label: "Tests"}}, 2, true))
	section("confirm danger", th.ConfirmView(NewConfirm("Delete request", "Delete Create charge in Payments? This removes its file from disk.", "Delete", true), min(width-4, 60)))
	section("empty", th.EmptyState("No requests yet", "This folder is empty.", "Press n to create a request.", width))
	section("error", th.ErrorState("Could not read payments/create.yaml", "Everything else in this workspace is intact.", "Fix the file in your editor; postctl reloads it when you save.", min(width, 70)))
	section("toast", th.Toast(ToastSuccess, "Environment: staging", width))
	section("busy", th.Busy("", "Running pre-request script"))
	section("section", th.Section("Collections", "12", true)+"\n"+th.Section("Request", "", false))
	return ansi.Strip(sb.String())
}

func TestGalleryGolden(t *testing.T) {
	cases := []struct {
		name  string
		theme Theme
	}{
		{"unicode", NewTheme(true, false, false)},
		{"nocolor", NewTheme(true, true, false)},
		{"ascii", NewTheme(true, true, true)},
	}
	for _, c := range cases {
		for _, w := range []int{80, 140} {
			t.Run(fmt.Sprintf("%s-%d", c.name, w), func(t *testing.T) {
				out := gallery(c.theme, w)
				for i, line := range strings.Split(out, "\n") {
					if Width(line) > w {
						t.Errorf("line %d is %d cells, over %d: %q", i, Width(line), w, line)
					}
				}
				golden(t, fmt.Sprintf("gallery-%s-%d", c.name, w), out)
			})
		}
	}
}

var colorSeq = regexp.MustCompile(`\x1b\[[0-9;:]*[34]8[;:]`)

func TestNoColorEmitsNoColorSequences(t *testing.T) {
	th := NewTheme(true, true, false)
	raw := []string{
		th.Header(80, HeaderInfo{Workspace: "w", Env: EnvInfo{Name: "prod", Production: true}}),
		th.MethodBadge("POST", 5),
		th.ResponseChip(500, "Internal Server Error", time.Second, 10),
		th.Status(StatusFailed, "boom"),
		th.Tabs(80, []Tab{{Label: "A"}, {Label: "B"}}, 0, true),
		th.ConfirmView(NewConfirm("t", "b", "Go", true), 40),
	}
	for _, s := range raw {
		if colorSeq.MatchString(s) {
			t.Errorf("no-color output contains color escapes: %q", s)
		}
	}
}

func TestColorOutputUsesTokens(t *testing.T) {
	th := NewTheme(true, false, false)
	if !colorSeq.MatchString(th.MethodBadge("GET", 5)) {
		t.Error("expected color in a colored theme")
	}
}

func TestProductionBadgeIsTextNotColorOnly(t *testing.T) {
	for _, ascii := range []bool{false, true} {
		th := NewTheme(true, true, ascii)
		got := ansi.Strip(th.EnvBadge(EnvInfo{Name: "live", Production: true}, true))
		if !strings.Contains(got, "! live") || !strings.Contains(got, "prod") {
			t.Errorf("production badge must carry glyph and word, got %q", got)
		}
	}
}

func TestMethodLabelKeepsText(t *testing.T) {
	for _, c := range []struct {
		in   string
		w    int
		want string
	}{
		{"get", 5, "GET"}, {"DELETE", 6, "DELETE"}, {"DELETE", 5, "DEL"}, {"OPTIONS", 5, "OPT"},
		{"PURGE", 3, "PUR"}, {"", 5, "-"},
	} {
		if got := MethodLabel(c.in, c.w); got != c.want {
			t.Errorf("MethodLabel(%q,%d)=%q want %q", c.in, c.w, got, c.want)
		}
	}
}

func TestConfirmDangerDefaultsToCancel(t *testing.T) {
	c := NewConfirm("Delete", "x", "Delete", true)
	if c.Accepted() {
		t.Fatal("danger confirm must start on Cancel")
	}
	c.Move(1)
	if !c.Accepted() {
		t.Fatal("moving right must reach the action")
	}
	c.Move(5)
	if c.Focus != 1 {
		t.Fatalf("focus must clamp, got %d", c.Focus)
	}
	if !NewConfirm("Save", "x", "Save", false).Accepted() {
		t.Fatal("non-danger confirm starts on the action")
	}
}

func TestSecretsAreMasked(t *testing.T) {
	for _, ascii := range []bool{false, true} {
		th := NewTheme(true, true, ascii)
		out := ansi.Strip(th.KVTable([]KVRow{{Key: "token", Value: "hunter2", Secret: true}}, KVTableOptions{Width: 60, Focus: -1}))
		if strings.Contains(out, "hunter2") {
			t.Fatalf("secret leaked: %q", out)
		}
		if !strings.Contains(out, th.Glyphs.Mask) {
			t.Fatalf("mask missing: %q", out)
		}
		rev := ansi.Strip(th.KVTable([]KVRow{{Key: "token", Value: "hunter2", Secret: true}}, KVTableOptions{Width: 60, Focus: -1, Reveal: true}))
		if !strings.Contains(rev, "hunter2") {
			t.Fatalf("reveal should show the value: %q", rev)
		}
	}
}

func TestTruncateAndFit(t *testing.T) {
	th := NewTheme(true, true, false)
	if got := th.Truncate("abcdefghij", 5); got != "abcd…" {
		t.Errorf("Truncate = %q", got)
	}
	if got := th.Fit("ab", 5); got != "ab   " {
		t.Errorf("Fit = %q", got)
	}
	a := NewTheme(true, true, true)
	if got := a.Truncate("abcdefghij", 6); got != "abc..." {
		t.Errorf("ascii Truncate = %q", got)
	}
	if got := th.Block("a\nb", 3, 3); got != "a  \nb  \n   " {
		t.Errorf("Block = %q", got)
	}
}

func TestSpinnerHasFrames(t *testing.T) {
	if NewTheme(true, false, false).NewSpinner().View() == "" {
		t.Error("spinner has no frame")
	}
	if !strings.ContainsAny(ansi.Strip(NewTheme(true, true, true).NewSpinner().View()), `|/-\`) {
		t.Error("ascii spinner must use ASCII frames")
	}
}

// A hint without a label is a status (the vim mode): the footer shows the bare
// key, with no trailing space or empty label.
func TestKeyHintWithoutLabelRendersTheKeyOnly(t *testing.T) {
	th := NewTheme(true, true, false)
	got := ansi.Strip(th.KeyHints(80, []Hint{{Key: "INSERT"}, {Key: "Esc", Label: "Done"}}))
	if !strings.HasPrefix(got, "INSERT") || !strings.Contains(got, "Esc Done") {
		t.Fatalf("hints = %q", got)
	}
}

func TestTabRangesAndTabAt(t *testing.T) {
	th := NewTheme(true, false, false)
	tabs := []Tab{
		{Label: "Params"},
		{Label: "Headers", Count: 3},
		{Label: "Auth", Dot: true},
	}
	ranges := th.TabRanges(80, tabs)
	if len(ranges) != 3 {
		t.Fatalf("got %d ranges, want 3", len(ranges))
	}
	// First tab: "Params" -> length 6
	if ranges[0].Start != 0 || ranges[0].End != 6 || ranges[0].Index != 0 {
		t.Errorf("ranges[0] = %+v, want Start 0, End 6", ranges[0])
	}
	// Check TabAt hit testing
	if hit := th.TabAt(80, tabs, 3); hit != 0 {
		t.Errorf("TabAt(3) = %d, want 0", hit)
	}
	// In the gap between tab 0 and tab 1 (columns 6 and 7):
	if hit := th.TabAt(80, tabs, 6); hit != -1 {
		t.Errorf("TabAt(6) in gap = %d, want -1", hit)
	}
	// Second tab: starts at 6 + 2 = 8, label "Headers(3)" len 10 -> [8, 18)
	if hit := th.TabAt(80, tabs, 10); hit != 1 {
		t.Errorf("TabAt(10) = %d, want 1", hit)
	}
	// Beyond visible width
	if hit := th.TabAt(80, tabs, 200); hit != -1 {
		t.Errorf("TabAt(200) = %d, want -1", hit)
	}
	if hit := th.TabAt(80, tabs, -1); hit != -1 {
		t.Errorf("TabAt(-1) = %d, want -1", hit)
	}
}
