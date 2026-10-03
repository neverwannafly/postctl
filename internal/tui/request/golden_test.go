package request

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/shubham/postctl/internal/model"
	"github.com/shubham/postctl/internal/tui"
	"github.com/shubham/postctl/internal/ui"
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
		t.Fatalf("missing golden %s (run: go test ./internal/tui/request -update): %v", path, err)
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

// TestGoldenTabs pins every tab at the three reference sizes in the dark
// theme, NO_COLOR and ASCII. Regenerate with: go test ./internal/tui/request -update
func TestGoldenTabs(t *testing.T) {
	tabs := []struct {
		name string
		id   tabID
	}{{"params", tabParams}, {"auth", tabAuth}, {"headers", tabHeaders}, {"body", tabBody}, {"pre", tabPre}, {"tests", tabTests}, {"settings", tabSettings}}
	for _, l := range looks() {
		for _, sz := range sizes {
			for _, tb := range tabs {
				t.Run(l.name+"/"+sz.name+"/"+tb.name, func(t *testing.T) {
					h := newHarness(t, withLook(l, sz.w, sz.h))
					h.req.Scripts = model.Scripts{
						PreRequest: &model.Script{Source: "pm.variables.set('now', Date.now());\npm.request.headers.add({key: 'X-Now', value: pm.variables.get('now')});"},
						Test:       &model.Script{Source: "pm.test('created', () => pm.response.to.have.status(201));"},
					}
					h.send(tui.SelectionMsg{Ref: h.ref(), Open: true})
					h.gotoTab(tb.id)
					golden(t, "tab-"+tb.name+"-"+l.name+"-"+sz.name, h.shot(l.raw))
				})
			}
		}
	}
}

// TestGoldenStates pins the states around the tabs.
func TestGoldenStates(t *testing.T) {
	states := []struct {
		name  string
		build func(h *harness)
	}{
		{"editing-url", func(h *harness) { h.key("up", "A"); h.typ("/x") }},
		{"method-selector", func(h *harness) { h.key("up", "shift+tab") }},
		{"sending", func(h *harness) {
			h.send(tui.SendStartedMsg{RequestID: h.req.ID})
			h.send(tui.SendProgressMsg{RequestID: h.req.ID, Step: tui.SendStep{Text: "Waiting for response"}})
		}},
		{"params-editing-row", func(h *harness) { h.gotoTab(tabParams); h.key("down", "a"); h.typ("page") }},
		{"body-invalid-json", func(h *harness) {
			h.gotoTab(tabBody)
			h.req.Body.Raw = `{"amount": ,}`
			h.send(tui.SelectionMsg{Ref: h.ref(), Open: true})
			h.gotoTab(tabBody)
		}},
		{"auth-bearer-secret", func(h *harness) {
			h.req.Auth = &model.Auth{Type: model.AuthBearer, Bearer: &model.BearerAuth{Token: "hunter2-literal"}}
			h.send(tui.SelectionMsg{Ref: h.ref(), Open: true})
			h.gotoTab(tabAuth)
		}},
		{"import-curl", func(h *harness) { h.key("up"); h.paste(pastedCurl) }},
		{"no-selection", func(h *harness) { h.send(tui.SelectionMsg{}) }},
	}
	for _, l := range looks() {
		for _, sz := range []int{0, 2} {
			for _, st := range states {
				t.Run(l.name+"/"+sizes[sz].name+"/"+st.name, func(t *testing.T) {
					h := newHarness(t, withLook(l, sizes[sz].w, sizes[sz].h))
					st.build(h)
					golden(t, "state-"+st.name+"-"+l.name+"-"+sizes[sz].name, h.shot(l.raw))
				})
			}
		}
	}
}

func TestGoldenViewsFitTheirSize(t *testing.T) {
	for _, sz := range sizes {
		for _, tb := range []tabID{tabParams, tabAuth, tabHeaders, tabBody, tabPre, tabTests, tabSettings} {
			h := newHarness(t, withLook(looks()[1], sz.w, sz.h))
			h.gotoTab(tb)
			lines := strings.Split(h.shot(false), "\n")
			if len(lines) != sz.h {
				t.Errorf("%s tab %d: %d lines, want %d", sz.name, tb, len(lines), sz.h)
			}
			for _, l := range lines {
				if ui.Width(l) > sz.w {
					t.Errorf("%s tab %d: line wider than %d: %q", sz.name, tb, sz.w, l)
				}
			}
		}
	}
}
