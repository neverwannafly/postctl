package qa_test

import (
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
