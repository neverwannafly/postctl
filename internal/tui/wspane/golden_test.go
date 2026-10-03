package wspane

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/shubham/postctl/internal/tui"
	"github.com/shubham/postctl/internal/ui"
	"github.com/shubham/postctl/internal/wsx"
)

var update = flag.Bool("update", false, "rewrite golden files")

// golden compares got with testdata/<name>.golden, or rewrites it with -update.
func golden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name+".golden")
	got = strings.TrimRight(got, "\n") + "\n"
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
		t.Fatalf("missing golden %s (run: go test ./internal/tui/wspane -update): %v", path, err)
	}
	if string(want) != got {
		t.Errorf("%s differs from the golden file (run with -update to accept)\n--- want\n%s\n--- got\n%s", name, want, got)
	}
}

type look struct {
	name string
	th   ui.Theme
	raw  bool
}

func looks() []look {
	return []look{
		{"dark", ui.NewTheme(true, false, false), true},
		{"nocolor", ui.NewTheme(true, true, false), false},
		{"ascii", ui.NewTheme(true, true, true), false},
	}
}

var sizes = []struct {
	name string
	w, h int
}{{"80x24", 80, 24}, {"120x40", 120, 40}, {"200x60", 200, 60}}

func withLook(l look, w, h int) option {
	return func(hh *harness) { hh.theme, hh.w, hh.h = l.th, w, h }
}

func (h *harness) shot(raw bool) string {
	if raw {
		return h.p.View()
	}
	return ansi.Strip(h.p.View())
}

// feed puts a realistic exchange in the log of an open connection.
func feed(h *harness) *fakeClient {
	c := h.connect()
	c.echo = false
	c.pushIn(`{"type":"welcome","session":"a1b2c3","heartbeat":30}`)
	h.key("down", "tab") // composer -> saved list
	h.key("enter")       // subscribe
	c.pushIn(`{"type":"subscribed","channel":"btc","levels":[{"px":64210.5,"qty":0.25},{"px":64210,"qty":1.5}]}`)
	c.pushIn(`{"type":"tick","channel":"btc","price":64211.25}`)
	c.pushBinary([]byte{0x00, 0x01, 0xde, 0xad, 0xbe, 0xef, 'H', 'i'})
	h.waitLog(6)
	h.key("esc")
	return c
}

// TestGoldenStates pins the pane at the three reference sizes in the dark
// theme, NO_COLOR and ASCII. Regenerate with: go test ./internal/tui/wspane -update
func TestGoldenStates(t *testing.T) {
	states := []struct {
		name  string
		build func(h *harness)
	}{
		{"idle", func(h *harness) {}},
		{"idle-empty", func(h *harness) {
			h.req.WebSocket = nil
			h.send(tui.SelectionMsg{Ref: h.ref(), Open: true})
		}},
		{"connecting", func(h *harness) {
			h.gate = make(chan struct{})
			h.key("ctrl+s")
			h.waitState(stConnecting)
			h.advance(2 * 1e9)
		}},
		{"connected", func(h *harness) { feed(h); h.key("up", "up") }},
		{"connected-log", func(h *harness) { feed(h); h.key("tab", "tab") }},
		{"log-filtered", func(h *harness) { feed(h); h.key("tab", "tab", "f") }},
		{"log-search", func(h *harness) { feed(h); h.key("tab", "tab", "/"); h.typ("tick") }},
		{"log-search-empty", func(h *harness) { feed(h); h.key("tab", "tab", "/"); h.typ("zzz") }},
		{"detail-json", func(h *harness) { feed(h); h.key("tab", "tab", "up", "up", "up", "enter") }},
		{"detail-binary", func(h *harness) { feed(h); h.key("tab", "tab", "enter") }},
		{"composer-json", func(h *harness) { h.key("down", "ctrl+t", "i"); h.typ(`{"op":"sub","ids":[1,2]}`) }},
		{"composer-invalid", func(h *harness) { h.key("down", "ctrl+t", "i"); h.typ(`{"op":`) }},
		{"saved-list", func(h *harness) { h.key("down", "tab", "down") }},
		{"delete-confirm", func(h *harness) { h.key("down", "tab", "d") }},
		{"refused", func(h *harness) {
			h.dialErr = &wsx.HandshakeError{Status: 401, StatusText: "Unauthorized", Body: `{"error":"token expired"}`}
			h.key("ctrl+s")
			h.waitState(stIdle)
			h.waitLog(2)
		}},
		{"closed-abnormal", func(h *harness) {
			c := h.connect()
			c.pushIn("last words")
			c.pushClose(1006, "")
			h.waitState(stIdle)
		}},
		{"no-selection", func(h *harness) { h.send(tui.SelectionMsg{}) }},
	}
	for _, l := range looks() {
		for _, sz := range sizes {
			for _, st := range states {
				t.Run(l.name+"/"+sz.name+"/"+st.name, func(t *testing.T) {
					h := newHarness(t, withLook(l, sz.w, sz.h))
					st.build(h)
					golden(t, "state-"+st.name+"-"+l.name+"-"+sz.name, h.shot(l.raw))
				})
			}
		}
	}
}

// TestGoldenTabs pins the four form tabs.
func TestGoldenTabs(t *testing.T) {
	tabs := []struct {
		name string
		id   tabID
	}{{"params", tabParams}, {"headers", tabHeaders}, {"auth", tabAuth}, {"settings", tabSettings}}
	for _, l := range looks() {
		for _, sz := range sizes {
			for _, tb := range tabs {
				t.Run(l.name+"/"+sz.name+"/"+tb.name, func(t *testing.T) {
					h := newHarness(t, withLook(l, sz.w, sz.h))
					h.gotoTab(tb.id)
					h.key("down")
					golden(t, "tab-"+tb.name+"-"+l.name+"-"+sz.name, h.shot(l.raw))
				})
			}
		}
	}
}
