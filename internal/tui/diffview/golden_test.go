package diffview

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/shubham/postctl/internal/ui"
)

var update = flag.Bool("update", false, "rewrite golden files")

func TestGoldenDiffView(t *testing.T) {
	sizes := [][2]int{{80, 24}, {120, 40}, {200, 60}}
	modes := []struct {
		name string
		th   ui.Theme
	}{
		{"dark", ui.NewTheme(true, false, false)},
		{"nocolor", ui.NewTheme(true, true, false)},
		{"ascii", ui.NewTheme(true, false, true)},
	}
	for _, sz := range sizes {
		for _, mode := range modes {
			m := New(mode.th,
				SideSummary{Label: "Marked", Status: "200", DurationMS: 12, SizeBytes: 100},
				SideSummary{Label: "Current", Status: "404", DurationMS: 40, SizeBytes: 80},
				`{"ok":true,"n":1}`, `{"ok":false,"n":2}`,
			)
			m.SetSize(sz[0], sz[1])
			body := ansi.Strip(m.View())
			tag := mode.name + "-" + sizeTag(sz[0])
			golden := filepath.Join("testdata", "diff-"+tag+".golden")
			if *update {
				_ = os.MkdirAll("testdata", 0o755)
				_ = os.WriteFile(golden, []byte(body), 0o644)
				continue
			}
			want, err := os.ReadFile(golden)
			if err != nil {
				t.Fatalf("missing %s (go test ./internal/tui/diffview -update)", golden)
			}
			if strings.TrimSpace(string(want)) != strings.TrimSpace(body) {
				t.Fatalf("golden mismatch %s", golden)
			}
		}
	}
}
