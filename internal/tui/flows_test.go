package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/shubham/postctl/internal/model"
	"github.com/shubham/postctl/internal/store"
)

func envByName(ws *model.Workspace, name string) *model.Environment {
	for _, e := range ws.Environments {
		if e.Name == name {
			return e
		}
	}
	return nil
}

func TestEnvironmentSwitcher(t *testing.T) {
	h := newHarness(t, demo(), 140, 30)
	h.mustHave("env  none")
	h.keys("E")
	h.mustHave("Environments")
	h.mustHave("No environment")
	for _, n := range []string{"dev", "local", "prod", "staging"} {
		h.mustHave(n)
	}
	h.mustHave("production") // the production badge
	h.keys("down", "down", "down", "enter")
	prod := envByName(h.a.ws, "prod")
	if h.a.activeEnv != prod.ID {
		t.Fatalf("active env %q, want prod", h.a.activeEnv)
	}
	h.mustHave("prod") // header
	h.mustHave("Environment: prod (production)")
	if got := h.be.savedState().ActiveEnvironment; got != prod.ID {
		t.Fatalf("choice was not persisted: %q", got)
	}

	// Reopening puts the cursor on the current one, and "No environment"
	// is reachable and persisted.
	h.keys("E")
	h.mustHave("active")
	h.keys("home", "enter")
	if h.a.activeEnv != "" || h.be.savedState().ActiveEnvironment != "" {
		t.Fatal("No environment should clear the choice")
	}
	h.mustHave("env  none")
}

func TestEnvironmentRestoredFromState(t *testing.T) {
	ws := demo()
	prod := envByName(ws, "prod")
	be := newFake(ws)
	be.state = store.State{ActiveEnvironment: prod.ID}
	h := newHarnessWith(t, be, 140, 30)
	if h.a.activeEnv != prod.ID {
		t.Fatal("state not restored")
	}
	h.mustHave("prod")

	// An environment deleted on disk falls back to none rather than dangling.
	be2 := newFake(demo())
	be2.state = store.State{ActiveEnvironment: "env-that-is-gone"}
	h2 := newHarnessWith(t, be2, 140, 30)
	if h2.a.activeEnv != "" {
		t.Fatal("dangling environment should be dropped")
	}
	h2.mustHave("env  none")
}

func TestEnvironmentSecretsAreMasked(t *testing.T) {
	for _, ascii := range []bool{false, true} {
		ws := demo()
		prod := envByName(ws, "prod")
		for i := range prod.Variables {
			if prod.Variables[i].Type == model.VarSecret {
				prod.Variables[i].Value = "hunter2-do-not-print"
			}
		}
		h := newHarness(t, ws, 140, 30, func(o *Options) { o.ASCII = ascii })
		h.keys("E", "down", "down", "down")
		mask := "••••"
		if ascii {
			mask = "****"
		}
		h.mustHave("apiKey")
		h.mustHave(mask)
		h.mustNotHave("hunter2")
		h.keys("enter")
		h.mustNotHave("hunter2")
		if strings.Contains(h.rawScreen(), "hunter2") {
			t.Fatal("secret leaked into the raw frame")
		}
	}
}

func TestPaletteFindsRequestsByPath(t *testing.T) {
	h := newHarness(t, demo(), 140, 30)
	h.keys("ctrl+p")
	h.mustHave("Command palette")
	h.typeText("admin ban")
	h.mustHave("Internal admin › Ban user")
	h.keys("enter")
	if h.a.ov != nil {
		t.Fatal("palette should close")
	}
	if r, _ := h.a.tree.current(); r.label != "Ban user" {
		t.Fatalf("cursor on %q", r.label)
	}
	if h.a.focus != SlotRequest {
		t.Fatalf("choosing a request opens it, focus=%v", h.a.focus)
	}
	h.mustHave("Ban user")
}

func TestPaletteRunsActionsAndSwitchesEnvironment(t *testing.T) {
	h := newHarness(t, demo(), 140, 30)
	h.keys("ctrl+p")
	h.typeText("use environment: stag")
	h.keys("enter")
	stg := envByName(h.a.ws, "staging")
	if h.a.activeEnv != stg.ID || h.be.savedState().ActiveEnvironment != stg.ID {
		t.Fatal("palette did not switch environment")
	}

	h.keys("ctrl+p")
	h.typeText("help")
	h.keys("enter")
	if _, ok := h.a.ov.(*helpOverlay); !ok {
		t.Fatalf("palette action should open help, ov=%T", h.a.ov)
	}
	h.keys("esc")

	h.keys("ctrl+p")
	h.keys("ctrl+p") // ctrl+p again closes
	if h.a.ov != nil {
		t.Fatal("ctrl+p toggles the palette")
	}
}

func TestPaletteNoMatchAndEsc(t *testing.T) {
	h := newHarness(t, demo(), 140, 30)
	h.keys("ctrl+p")
	h.typeText("qqqqzzzz")
	h.mustHave("No matches")
	h.keys("esc")
	h.mustNotHave("Command palette")
}

func TestFirstRunWhenNoWorkspace(t *testing.T) {
	h := newHarness(t, nil, 100, 30)
	h.mustHave("Welcome to postctl.")
	h.mustHave("postctl import postman-local")
	h.mustHave("postctl import postman <file")
	h.mustHave("Create a request")
	h.mustNotHave("Something went wrong")
	h.keys("enter") // primary action
	h.mustHave("New")
	h.typeText("Ping")
	if h.be.inited {
		t.Fatal("nothing is written until the name is confirmed")
	}
	h.keys("enter")
	if !h.be.inited {
		t.Fatal("creating the first request sets up the workspace")
	}
	if len(h.a.ws.Collections) != 1 {
		t.Fatalf("collections: %d", len(h.a.ws.Collections))
	}
	if find(h.a.ws.Collections[0], "Ping") == nil {
		t.Fatal("first request missing")
	}
	h.mustNotHave("Welcome to postctl.")
	h.mustHave("Ping")
	if len(h.be.saved) == 0 {
		t.Fatal("first request not saved")
	}
}

func TestFirstRunForEmptyWorkspace(t *testing.T) {
	h := newHarness(t, &model.Workspace{ID: model.NewID(), Name: "empty"}, 100, 30)
	h.mustHave("Welcome to postctl.")
	h.mustHave("Create a request")
	if h.be.inited {
		t.Fatal("an existing workspace is not re-initialized")
	}
	h.keys("n")
	h.typeText("Ping")
	h.keys("enter")
	h.mustNotHave("Welcome to postctl.")
	h.mustHave("Ping")
}

func TestFirstRunImportRowCopiesCommand(t *testing.T) {
	h := newHarness(t, nil, 100, 30)
	h.keys("down", "enter")
	h.mustHave("Copied: postctl import postman-local")
	h.keys("q")
	if !h.quit {
		t.Fatal("q quits from the guide")
	}
}

func TestReloadKeepsSelectionAcrossRename(t *testing.T) {
	h := newHarness(t, demo(), 160, 30)
	h.selectByName("Create charge")
	id := h.a.tree.sel
	// Someone renames the request and adds another above it, on disk.
	ws := deepCopy(h.be.ws)
	f := ws.Collections[0].Items[0].(*model.Folder)
	f.Items[1].(*model.Request).Name = "Make charge"
	f.Items = append([]model.Item{model.NewRequest("Brand new", "GET", "x")}, f.Items...)
	h.be.ws = ws
	h.msg(ReloadMsg{})
	if h.a.tree.sel != id {
		t.Fatal("selection is tracked by ID")
	}
	if r, _ := h.a.tree.current(); r.label != "Make charge" {
		t.Fatalf("cursor on %q", r.label)
	}
	h.mustHave("Brand new")
}

func TestReloadWhenSelectedRequestVanishes(t *testing.T) {
	h := newHarness(t, demo(), 160, 30)
	h.selectByName("Refund charge")
	ws := deepCopy(h.be.ws)
	f := ws.Collections[0].Items[0].(*model.Folder)
	f.Items = append(f.Items[:2], f.Items[3:]...)
	h.be.ws = ws
	h.msg(ReloadMsg{})
	h.mustNotHave("Refund charge")
	r, ok := h.a.tree.current()
	if !ok || r.label != "Update charge" {
		t.Fatalf("cursor should land on a neighbour, on %q", r.label)
	}
}

func TestReloadKeepsFoldState(t *testing.T) {
	h := newHarness(t, demo(), 160, 30)
	h.selectByName("Customers")
	h.keys("left")
	h.mustNotHave("Get customer")
	h.msg(ReloadMsg{})
	h.mustNotHave("Get customer")
	h.mustHave("Customers")
}

func TestReloadFailureKeepsScreen(t *testing.T) {
	h := newHarness(t, demo(), 160, 30)
	h.be.loadErr = errBoom
	h.msg(ReloadMsg{})
	h.mustHave("Could not reload the workspace")
	h.mustHave("The copy on screen is unchanged")
	h.mustHave("List charges")
}

func TestManualReloadWaitsForSaves(t *testing.T) {
	h := newHarness(t, demo(), 160, 30)
	h.be.gate = make(chan struct{})
	h.selectByName("Create charge")
	h.a.Update(keyPress("D"))
	loads := h.be.loads
	h.keys("ctrl+r")
	if h.be.loads != loads {
		t.Fatal("reload must not race in-flight saves")
	}
	h.mustHave("Still saving")
	close(h.be.gate)
}

func TestAccessibleMode(t *testing.T) {
	h := newHarness(t, demo(), 120, 40, func(o *Options) { o.Accessible = true })
	v := h.a.View()
	if v.AltScreen {
		t.Fatal("accessible mode stays on the normal screen")
	}
	if raw := v.Content; strings.Contains(raw, "\x1b[38") || strings.Contains(raw, "\x1b[48") {
		t.Fatal("accessible mode emits no color")
	}
	for _, r := range h.screen() {
		if r > 127 && r != '\n' {
			t.Fatalf("accessible mode is ASCII only, found %q", r)
		}
	}
	// One column at a time, whatever the width.
	if strings.Contains(h.screen(), " | ") {
		t.Fatal("no side-by-side panes in accessible mode")
	}
	h.mustHave("List charges")
	h.selectByName("List charges")
	h.keys("enter")
	h.mustHave("GET")
	if h.a.View().AltScreen {
		t.Fatal("alt screen")
	}
}

func TestNoColorAndASCII(t *testing.T) {
	h := newHarness(t, demo(), 140, 30, func(o *Options) { o.NoColor = true })
	if strings.Contains(h.rawScreen(), "\x1b[38") || strings.Contains(h.rawScreen(), "\x1b[48") {
		t.Fatal("NO_COLOR must not emit colors")
	}
	h.mustHave("▾") // unicode is still on
	h.selectByName("Refund charge")
	h.mustHave("DEL")

	a := newHarness(t, demo(), 140, 30, func(o *Options) { o.ASCII = true })
	for _, r := range a.screen() {
		if r > 127 && r != '\n' {
			t.Fatalf("ascii mode leaked %q:\n%s", r, a.screen())
		}
	}
	a.keys("E")
	a.mustHave("Environments")
}

func TestLightAndDarkBothRender(t *testing.T) {
	d := newHarness(t, demo(), 120, 30)
	l := newHarness(t, demo(), 120, 30, func(o *Options) { o.Light = true })
	if d.screen() != l.screen() {
		t.Fatal("themes must differ only in color, not layout")
	}
	if d.rawScreen() == l.rawScreen() {
		t.Fatal("light and dark should use different colors")
	}
}

func TestFirstFrameWithLargeWorkspaceIsFast(t *testing.T) {
	if testing.Short() {
		t.Skip("timing test")
	}
	limit := 150 * time.Millisecond
	if raceEnabled {
		limit *= 6 // the race detector is slow; the real budget is checked without it
	}
	ws := largeWorkspace()
	best := time.Hour
	for i := 0; i < 3; i++ {
		start := time.Now()
		h := newHarness(t, ws, 200, 60)
		_ = h.a.View()
		if d := time.Since(start); d < best {
			best = d
		}
	}
	t.Logf("cold start to first frame (1000 requests): %v", best)
	if best > limit {
		t.Fatalf("first frame took %v, budget %v", best, limit)
	}
}

func BenchmarkColdStart1000(b *testing.B) {
	ws := largeWorkspace()
	for i := 0; i < b.N; i++ {
		be := newFake(ws)
		a := newApp(Options{Backend: be, NoWatch: true})
		a.Update(sizeMsg(200, 60))
		if m, ok := a.loadCmd(nil)().(loadedMsg); ok {
			a.Update(m)
		}
		_ = a.View()
	}
}

func BenchmarkKeyRepeatInTree1000(b *testing.B) {
	h := newHarness(b, largeWorkspace(), 200, 60)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		h.a.Update(keyPress("down"))
		_ = h.a.View()
	}
}
