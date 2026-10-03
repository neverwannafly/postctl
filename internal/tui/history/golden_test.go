package history

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/shubham/postctl/internal/history"
	"github.com/shubham/postctl/internal/model"
	"github.com/shubham/postctl/internal/tui"
	"github.com/shubham/postctl/internal/ui"
)

var update = flag.Bool("update", false, "rewrite golden files")

func TestGoldenHistoryMarkAndDiff(t *testing.T) {
	sizes := [][2]int{{80, 24}, {120, 40}, {200, 60}}
	modes := []struct {
		name string
		th   ui.Theme
	}{
		{"dark", ui.NewTheme(true, false, false)},
		{"nocolor", ui.NewTheme(true, true, false)},
		{"ascii", ui.NewTheme(true, false, true)},
	}
	log := &memLog{entries: []history.Entry{
		{ID: "a", Time: time.Now(), RequestID: "r1", RequestName: "First",
			Request: history.RequestSnapshot{Method: "GET", URL: "https://api.test/a"},
			Status:  200, DurationMS: 10, Response: &history.ResponseSnapshot{Body: `{"a":1}`}},
		{ID: "b", Time: time.Now(), RequestID: "r2", RequestName: "Second",
			Request: history.RequestSnapshot{Method: "GET", URL: "https://api.test/b"},
			Status:  404, DurationMS: 20, Response: &history.ResponseSnapshot{Body: `{"a":2}`}},
	}}
	for _, sz := range sizes {
		for _, mode := range modes {
			m := New(tui.ScreenEnv{
				PaneEnv: tui.PaneEnv{Theme: mode.th}, Workspace: &model.Workspace{Name: "demo"},
			}, Deps{Log: func() (tui.HistoryReader, error) { return log, nil }})
			m.SetSize(sz[0], sz[1])
			m.items = log.entries
			m.loaded = true
			m.markID = "a"
			m.cur = 1
			list := ansi.Strip(m.View())
			m.phase = phDiff
			m.onDiffPair(diffPairMsg{left: &log.entries[0], right: &log.entries[1]})
			diff := ansi.Strip(m.View())
			for _, scene := range []struct{ name, body string }{
				{"list-marked", list},
				{"diff", diff},
			} {
				tag := scene.name + "-" + mode.name + "-" + sizeTag(sz[0])
				golden := filepath.Join("testdata", tag+".golden")
				if *update {
					_ = os.MkdirAll("testdata", 0o755)
					_ = os.WriteFile(golden, []byte(scene.body), 0o644)
					continue
				}
				want, err := os.ReadFile(golden)
				if err != nil {
					t.Fatalf("missing %s (go test ./internal/tui/history -update)", golden)
				}
				if strings.TrimSpace(string(want)) != strings.TrimSpace(scene.body) {
					t.Fatalf("golden mismatch %s", golden)
				}
			}
		}
	}
}
