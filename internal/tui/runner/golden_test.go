package runner

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/shubham/postctl/internal/engine"
	"github.com/shubham/postctl/internal/tui/fakerun"
	"github.com/shubham/postctl/internal/ui"
)

func TestGoldenSetupAndSummary(t *testing.T) {
	sizes := [][2]int{{80, 24}, {140, 40}}
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
			env := testEnv(sz[0], sz[1])
			env.Theme = mode.th
			m := New(env, Deps{Runner: &fakerun.Runner{}})
			m.SetSize(sz[0], sz[1])
			setup := ansi.Strip(m.View())
			m.phase = phSummary
			m.summary = &engine.RunSummary{
				Started: time.Now(), Iterations: 1, Requests: 2,
				Tests: engine.TestCounts{Passed: 3, Failed: 1}, TotalTime: 1200 * time.Millisecond,
				Failures: []engine.Failure{{RequestName: "Bad", Kind: engine.FailureTest, Message: "assertion failed"}},
			}
			summary := ansi.Strip(m.View())
			for _, scene := range []struct{ name, body string }{
				{"setup", setup},
				{"summary", summary},
			} {
				tag := scene.name + "-" + mode.name + "-"
				if sz[0] == 80 {
					tag += "80x24"
				} else {
					tag += "140x40"
				}
				golden := filepath.Join("testdata", tag+".golden")
				if os.Getenv("UPDATE_GOLDEN") != "" {
					_ = os.MkdirAll("testdata", 0o755)
					_ = os.WriteFile(golden, []byte(scene.body), 0o644)
					continue
				}
				want, err := os.ReadFile(golden)
				if err != nil {
					t.Fatalf("missing golden %s (run with UPDATE_GOLDEN=1)", golden)
				}
				if strings.TrimSpace(string(want)) != strings.TrimSpace(scene.body) {
					t.Fatalf("golden mismatch %s", golden)
				}
			}
		}
	}
}
