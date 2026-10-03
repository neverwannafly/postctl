package qa_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestLeakCheckFindsSentinelAfterANSIStripWhenRawIsSplit(t *testing.T) {
	sentinel := "postctl-ansi-split-sentinel"
	raw := "before \x1b[7mpostctl-ansi-split-\x1b[0m\x1b[7msentinel\x1b[0m after"
	if strings.Contains(raw, sentinel) {
		t.Fatal("fixture must split the sentinel in raw bytes")
	}
	if !strings.Contains(ansi.Strip(raw), sentinel) {
		t.Fatal("stripped text must contain the sentinel")
	}
}

// frameShape describes a frame by size only. Failure messages use it instead
// of printing the frame, which could hold a leaked secret.
func frameShape(frame string) string {
	lines := strings.Split(frame, "\n")
	w := 0
	for _, l := range lines {
		w = max(w, ansi.StringWidth(l))
	}
	return fmt.Sprintf("%d lines, widest %d cells", len(lines), w)
}

func TestFrameShapeReportsSizeNotContent(t *testing.T) {
	got := frameShape("secret-one\nsecret-two-longer")
	if strings.Contains(got, "secret") || !strings.Contains(got, "2 lines") {
		t.Fatalf("frameShape = %q", got)
	}
}
