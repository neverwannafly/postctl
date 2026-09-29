package response

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/shubham/postctl/internal/model"
	"github.com/shubham/postctl/internal/tui"
	"github.com/shubham/postctl/internal/ui"
)

var update = flag.Bool("update", false, "rewrite golden files")

func TestGoldenRenders(t *testing.T) {
	sizes := [][2]int{{80, 24}, {120, 40}, {200, 60}}
	modes := []struct {
		name string
		th   ui.Theme
	}{
		{"dark", ui.NewTheme(true, false, false)},
		{"nocolor", ui.NewTheme(true, true, false)},
		{"ascii", ui.NewTheme(true, false, true)},
	}
	scenes := []struct {
		name  string
		setup func(*Pane)
	}{
		{"empty", func(p *Pane) {
			p.ref = tui.RequestRef{Request: &model.Request{ID: model.NewID()}}
		}},
		{"json-body", func(p *Pane) {
			id := model.NewID()
			p.ref = tui.RequestRef{Request: &model.Request{ID: id}}
			p.applyResult(demoResult(id))
		}},
		{"tests-tab", func(p *Pane) {
			id := model.NewID()
			p.ref = tui.RequestRef{Request: &model.Request{ID: id}}
			p.applyResult(demoResult(id))
			p.tab = tabTests
		}},
		{"timeline-tab", func(p *Pane) {
			id := model.NewID()
			p.ref = tui.RequestRef{Request: &model.Request{ID: id}}
			p.applyResult(demoResult(id))
			p.tab = tabTimeline
		}},
		{"sending", func(p *Pane) {
			id := model.NewID()
			p.ref = tui.RequestRef{Request: &model.Request{ID: id}}
			p.sendingID = id
			p.sendStep = "Waiting for response"
			p.sendStarted = time.Now().Add(-2 * time.Second)
		}},
		{"transport-err", func(p *Pane) {
			id := model.NewID()
			p.ref = tui.RequestRef{Request: &model.Request{ID: id}}
			p.applyResult(tui.SendResult{RequestID: id, Err: httpxErr("connection refused to api.local:8443")})
		}},
	}
	dir := filepath.Join("testdata", "golden")
	for _, sc := range scenes {
		for _, sz := range sizes {
			for _, m := range modes {
				if m.name == "nocolor" && sc.name == "sending" {
					continue
				}
				p := &Pane{th: m.th, byID: map[model.ID]*storedResult{}, spin: m.th.NewSpinner()}
				sc.setup(p)
				p.SetSize(sz[0], sz[1]-3)
				got := blockView(p.View(), sz[0], sz[1]-3)
				file := filepath.Join(dir, sc.name+"-"+m.name+"-"+itoa(sz[0])+"x"+itoa(sz[1])+".golden")
				if *update {
					_ = os.MkdirAll(dir, 0o755)
					if err := os.WriteFile(file, []byte(got), 0o644); err != nil {
						t.Fatal(err)
					}
					continue
				}
				want, err := os.ReadFile(file)
				if err != nil {
					t.Fatalf("missing golden %s (run with -update): %v", file, err)
				}
				if strings.TrimRight(got, "\n") != strings.TrimRight(string(want), "\n") {
					t.Fatalf("golden mismatch %s\n--- got ---\n%s\n--- want ---\n%s", file, got, want)
				}
			}
		}
	}
}

func httpxErr(s string) error { return &simpleErr{s} }

type simpleErr struct{ s string }

func (e *simpleErr) Error() string { return e.s }

func blockView(s string, w, h int) string {
	th := ui.NewTheme(true, false, false)
	return ansi.Strip(th.Block(s, w, h))
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [12]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
