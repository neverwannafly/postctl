package response

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
		then  func(*testing.T, *Pane) // runs after SetSize
	}{
		{name: "empty", setup: func(p *Pane) {
			p.ref = tui.RequestRef{Request: &model.Request{ID: model.NewID()}}
		}},
		{name: "json-body", setup: func(p *Pane) {
			id := model.NewID()
			p.ref = tui.RequestRef{Request: &model.Request{ID: id}}
			p.applyResult(demoResult(id))
		}},
		{name: "tests-tab", setup: func(p *Pane) {
			id := model.NewID()
			p.ref = tui.RequestRef{Request: &model.Request{ID: id}}
			p.applyResult(demoResult(id))
			p.tab = tabTests
		}},
		{name: "timeline-tab", setup: func(p *Pane) {
			id := model.NewID()
			p.ref = tui.RequestRef{Request: &model.Request{ID: id}}
			p.applyResult(demoResult(id))
			p.tab = tabTimeline
		}},
		{name: "history-tab", setup: func(p *Pane) {
			id := model.NewID()
			p.ref = tui.RequestRef{Request: &model.Request{ID: id}}
			p.deps = goldenDeps()
			res := demoResult(id)
			res.HistoryID = "e3"
			p.applyResult(res)
			p.hist = histState{loaded: true, items: goldenHistory(id), cur: 1, now: goldenDeps().now()}
			p.tab = tabHistory
		}},
		{name: "history-empty", setup: func(p *Pane) {
			p.ref = tui.RequestRef{Request: &model.Request{ID: model.NewID()}}
			p.deps = goldenDeps()
			p.hist = histState{loaded: true}
			p.tab = tabHistory
		}},
		{name: "diff-body", setup: func(p *Pane) {
			id := model.NewID()
			p.ref = tui.RequestRef{Request: &model.Request{ID: id}}
			p.deps = goldenDeps()
			res := demoResult(id)
			res.Response.Body = []byte(`{"data":{"items":[{"id":"ch_1","amount":1500},{"id":"ch_2","amount":300}],"total":2},"ok":true}`)
			p.applyResult(res)
			prev := goldenHistory(id)[1]
			prev.Response = &history.ResponseSnapshot{
				Headers: []model.KV{{Key: "Content-Type", Value: "application/json"}},
				Body:    `{"data":{"items":[{"id":"ch_1","amount":1200}],"total":1},"ok":true}`,
			}
			ready := buildDiff(1, p.th, 0, 0, "Previous", &prev, res)
			p.diff = diffState{open: true, body: ready.body, headers: ready.headers, showBody: true}
		}},
		{name: "jq-output", setup: func(p *Pane) {
			id := model.NewID()
			p.ref = tui.RequestRef{Request: &model.Request{ID: id}}
			p.applyResult(demoResult(id))
			p.jq = jqState{filter: ".data.items[]", started: true, have: true, lines: []string{
				"{", `  "id": "ch_1",`, `  "amount": 1200`, "}",
			}}
		}},
		{name: "jq-open", setup: func(p *Pane) {
			id := model.NewID()
			p.ref = tui.RequestRef{Request: &model.Request{ID: id}}
			p.applyResult(demoResult(id))
			p.w = 80
			p.newJQInput(".data.items[]")
			p.jq.open = true
		}},
		{name: "vim-body", setup: vimBodyScene, then: func(t *testing.T, p *Pane) {
			vimKeys(t, p, "j", "j", "l", "l")
		}},
		{name: "vim-visual", setup: vimBodyScene, then: func(t *testing.T, p *Pane) {
			vimKeys(t, p, "j", "V", "j")
		}},
		{name: "vim-search", setup: vimBodyScene, then: func(t *testing.T, p *Pane) {
			vimKeys(t, p, "/", "i", "d", "enter")
		}},
		{name: "sending", setup: func(p *Pane) {
			id := model.NewID()
			p.ref = tui.RequestRef{Request: &model.Request{ID: id}}
			p.sendingID = id
			p.sendStep = "Waiting for response"
			p.sendStarted = time.Now().Add(-2 * time.Second)
		}},
		{name: "transport-err", setup: func(p *Pane) {
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
				if sc.then != nil {
					sc.then(t, p)
				}
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

func goldenDeps() Deps {
	return Deps{
		Log: func() (tui.HistoryReader, error) { return nil, nil },
		Now: func() time.Time { return v3Now },
	}
}

// goldenHistory lists three sends of id, newest first: a success, a client
// error and a transport failure.
func goldenHistory(id model.ID) []history.Entry {
	return []history.Entry{
		v3Entry("e3", id, 12*time.Second, 200, "OK", `{}`),
		v3Entry("e2", id, 7*time.Minute, 404, "Not Found", `{}`),
		{ID: "e1", Time: v3Now.Add(-26 * time.Hour), RequestID: id,
			Request: history.RequestSnapshot{Method: "GET", URL: "https://api.example.test/items"},
			Error:   "connection refused", DurationMS: 31},
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
