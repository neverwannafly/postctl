package envedit

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/shubham/postctl/internal/model"
	"github.com/shubham/postctl/internal/store"
	"github.com/shubham/postctl/internal/tui"
	"github.com/shubham/postctl/internal/ui"
)

type timerMsg struct {
	due time.Time
	msg tea.Msg
}

type hydrator struct {
	tui.Backend
	secrets map[model.SecretRef]string
}

func (h hydrator) HydrateVariables(_ context.Context, vs []model.Variable) error {
	for i := range vs {
		if vs[i].IsSecret() && vs[i].SecretRef != "" {
			vs[i].Value = h.secrets[vs[i].SecretRef]
		}
	}
	return nil
}
func (hydrator) HydrateAuth(context.Context, *model.Auth) error { return nil }

type harness struct {
	t      *testing.T
	m      tui.Screen
	scr    *Model
	ws     *model.Workspace
	now    time.Time
	timers []timerMsg
	out    []tea.Msg
	closed bool
}

func workspace() *model.Workspace {
	stg := &model.Environment{ID: "e-stg", Name: "staging", Variables: []model.Variable{
		{Key: "baseUrl", Value: "https://staging.example.com", Description: "API root"},
		{Key: "token", Type: model.VarSecret, SecretRef: "env/e-stg/token"},
		{Key: "off", Value: "x", Disabled: true},
	}}
	prod := &model.Environment{ID: "e-prod", Name: "prod", Production: true, Variables: []model.Variable{{Key: "baseUrl", Value: "https://api.example.com"}}}
	c := model.NewCollection("Payments")
	c.Variables = []model.Variable{{Key: "amount", Value: "1200"}}
	return &model.Workspace{ID: "w", Name: "demo", Collections: []*model.Collection{c}, Environments: []*model.Environment{stg, prod}, Globals: &model.Globals{ID: "g", Variables: []model.Variable{{Key: "region", Value: "eu"}}}}
}

func newHarness(t *testing.T, w, h int) *harness {
	t.Helper()
	hh := &harness{t: t, ws: workspace(), now: time.Unix(1_700_000_000, 0)}
	deps := Deps{After: func(d time.Duration, msg tea.Msg) tea.Cmd {
		return func() tea.Msg { return timerMsg{due: hh.now.Add(d), msg: msg} }
	}}
	env := tui.ScreenEnv{
		PaneEnv:   tui.PaneEnv{Theme: ui.NewTheme(true, false, false), Actions: &tui.Registry{}, Backend: hydrator{secrets: map[model.SecretRef]string{"env/e-stg/token": "s3cr3t-value"}}},
		Workspace: hh.ws, ActiveEnv: "e-stg",
	}
	hh.scr = New(env, deps)
	hh.m = hh.scr
	hh.m.SetSize(w, h)
	return hh
}

func TestMouseOpensEnvironmentCellAtPointer(t *testing.T) {
	h := newHarness(t, 120, 30)
	h.scr.zone = zTable
	x := listW + 3 + 6
	h.scr.HandleMouse(tea.MouseClickMsg{X: x, Y: 4, Button: tea.MouseLeft}, x, 4)
	if !h.scr.edit || h.scr.col != 0 || h.scr.in == nil {
		t.Fatal("clicking the environment key cell did not open it")
	}
	if row, col := h.scr.in.Cursor(); row != 0 || col != 0 {
		t.Fatalf("environment cell cursor = %d,%d, want 0,0", row, col)
	}
	if got := h.scr.in.VimMode(); got != "NORMAL" {
		t.Fatalf("environment cell mode = %q, want NORMAL", got)
	}
}

func (h *harness) send(msg tea.Msg) {
	h.t.Helper()
	next, cmd := h.m.Update(msg)
	if next == nil {
		h.closed = true
	} else {
		h.m = next
	}
	h.run(cmd)
}

func (h *harness) run(cmd tea.Cmd) {
	h.t.Helper()
	if cmd == nil {
		return
	}
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	var msg tea.Msg
	select {
	case msg = <-done:
	case <-time.After(3 * time.Second):
		h.t.Fatal("command blocked")
	}
	switch m := msg.(type) {
	case nil:
	case tea.BatchMsg:
		for _, c := range m {
			h.run(c)
		}
	case timerMsg:
		h.timers = append(h.timers, m)
	case hydratedMsg:
		h.send(m)
	default:
		h.out = append(h.out, m)
	}
}

func (h *harness) advance(d time.Duration) {
	h.now = h.now.Add(d)
	for {
		fired := false
		for i, tm := range h.timers {
			if !tm.due.After(h.now) {
				h.timers = append(h.timers[:i], h.timers[i+1:]...)
				h.send(tm.msg)
				fired = true
				break
			}
		}
		if !fired {
			return
		}
	}
}

func key(s string) tea.KeyPressMsg {
	parts := strings.Split(s, "+")
	name := parts[len(parts)-1]
	var mod tea.KeyMod
	for _, p := range parts[:len(parts)-1] {
		switch p {
		case "ctrl":
			mod |= tea.ModCtrl
		case "alt":
			mod |= tea.ModAlt
		case "shift":
			mod |= tea.ModShift
		}
	}
	codes := map[string]rune{"enter": tea.KeyEnter, "esc": tea.KeyEscape, "tab": tea.KeyTab, "up": tea.KeyUp, "down": tea.KeyDown, "left": tea.KeyLeft, "right": tea.KeyRight, "backspace": tea.KeyBackspace}
	if c, ok := codes[name]; ok {
		return tea.KeyPressMsg(tea.Key{Code: c, Mod: mod})
	}
	if name == "space" {
		return tea.KeyPressMsg(tea.Key{Code: ' ', Text: " ", Mod: mod})
	}
	r := []rune(name)[0]
	k := tea.Key{Code: r, Mod: mod}
	if mod&(tea.ModCtrl|tea.ModAlt) == 0 {
		k.Text = name
	}
	return tea.KeyPressMsg(k)
}

func (h *harness) key(keys ...string) {
	h.t.Helper()
	for _, k := range keys {
		h.send(key(k))
	}
}

func (h *harness) typ(s string) {
	h.t.Helper()
	for _, r := range s {
		if r == ' ' {
			h.key("space")
		} else {
			h.key(string(r))
		}
	}
}

func (h *harness) view() string { return ansi.Strip(h.m.View()) }

func TestUnhydratedSecretEnterPreservesKeychain(t *testing.T) {
	secrets := store.NewMemorySecrets()
	ref := model.SecretRef("env/e-stg/token")
	if err := secrets.Set(context.Background(), ref, "keep-secret"); err != nil {
		t.Fatal(err)
	}
	h := newHarness(t, 80, 24)
	h.ws.Environments[0].Variables[1] = model.Variable{Key: "token", Type: model.VarSecret, SecretRef: ref}
	h.scr.reveal = false
	h.key("enter") // table
	h.key("j")     // token row
	h.key("enter") // value
	h.key("enter") // submit unchanged
	got, err := secrets.Get(context.Background(), ref)
	if err != nil || got != "keep-secret" {
		t.Fatalf("keychain value changed: err=%v", err)
	}
	v := h.ws.Environments[0].Variables[1]
	if v.SecretRef != ref {
		t.Fatal("secret ref cleared")
	}
}

func TestViewIsPure(t *testing.T) {
	h := newHarness(t, 80, 24)
	h.scr.cur, h.scr.top = 2, 1
	snapshot := func() any {
		return struct {
			Sel      [3]int
			Cur, Top int
			Tab      tabKind
			Zone     zone
		}{h.scr.sel, h.scr.cur, h.scr.top, h.scr.tab, h.scr.zone}
	}
	before := snapshot()
	first, second := h.m.View(), h.m.View()
	if first != second || !reflect.DeepEqual(before, snapshot()) {
		t.Fatalf("View mutated environment editor state: before=%+v after=%+v", before, snapshot())
	}
}

func TestViewIsPureInEditMode(t *testing.T) {
	h := newHarness(t, 80, 24)
	h.key("enter")
	h.key("j")
	h.key("enter")
	if h.scr.in == nil {
		t.Fatal("expected value field editor")
	}
	val := h.scr.in.Value()
	first, second := h.m.View(), h.m.View()
	if first != second || h.scr.in.Value() != val {
		t.Fatal("View mutated the table editor while editing")
	}
}

func (h *harness) mustHave(sub string) {
	h.t.Helper()
	if !strings.Contains(h.view(), sub) {
		h.t.Fatalf("view lacks %q:\n%s", sub, h.view())
	}
}

func (h *harness) mustNotHave(sub string) {
	h.t.Helper()
	if strings.Contains(h.view(), sub) {
		h.t.Fatalf("view has %q:\n%s", sub, h.view())
	}
}

func (h *harness) saves() []tui.SaveMsg {
	var out []tui.SaveMsg
	for _, m := range h.out {
		if s, ok := m.(tui.SaveMsg); ok {
			out = append(out, s)
		}
	}
	return out
}

func (h *harness) env(name string) *model.Environment {
	for _, e := range h.ws.Environments {
		if e.Name == name {
			return e
		}
	}
	return nil
}

// ---- tests ---------------------------------------------------------------------------

func TestOpensOnActiveEnvironmentAndListsBadges(t *testing.T) {
	h := newHarness(t, 120, 30)
	for _, want := range []string{"staging", "prod", "PROD", "baseUrl", "https://staging.example.com", "Environments(2)", "Collections(1)"} {
		h.mustHave(want)
	}
	h.mustNotHave("s3cr3t-value")
}

func TestNarrowLayoutUsesSelector(t *testing.T) {
	h := newHarness(t, 80, 24)
	h.mustHave("Environment")
	h.mustHave("‹ staging ›")
	h.key("right")
	h.mustHave("‹ prod ›")
	h.mustHave("PROD")
	h.mustHave("https://api.example.com")
	for _, l := range strings.Split(h.view(), "\n") {
		if ui.Width(l) > 80 {
			t.Fatalf("line wider than 80: %q", l)
		}
	}
}

func TestNewEnvironment(t *testing.T) {
	h := newHarness(t, 100, 30)
	h.key("n")
	h.mustHave("New name")
	h.key("enter")
	h.mustHave("Give the environment a name")
	h.typ("dev")
	h.key("enter")
	e := h.env("dev")
	if e == nil || h.scr.curEnv() != e {
		t.Fatal("environment not created and selected")
	}
	h.advance(time.Second)
	saves := h.saves()
	if len(saves) != 1 || saves[0].Kind != tui.SaveEnvironment || saves[0].ID != e.ID {
		t.Fatalf("saves = %+v", saves)
	}
}

func TestNameMustBeUnique(t *testing.T) {
	h := newHarness(t, 100, 30)
	h.key("n")
	h.typ("PROD")
	h.key("enter")
	h.mustHave("Another environment already has that name")
	if len(h.ws.Environments) != 2 {
		t.Fatal("duplicate name accepted")
	}
	h.key("esc")
	h.mustNotHave("New name")
}

func TestRename(t *testing.T) {
	h := newHarness(t, 100, 30)
	h.key("r")
	h.mustHave("Rename to")
	for range "staging" {
		h.key("backspace")
	}
	h.typ("qa")
	h.key("enter")
	if h.ws.Environments[0].Name != "qa" {
		t.Fatalf("name = %q", h.ws.Environments[0].Name)
	}
	h.advance(time.Second)
	if s := h.saves(); len(s) != 1 || s[0].ID != "e-stg" {
		t.Fatalf("saves = %+v", s)
	}
}

func TestDuplicateCopiesSecretsWithoutSharingRefs(t *testing.T) {
	h := newHarness(t, 100, 30)
	h.key("c")
	h.mustHave("staging copy")
	h.key("enter")
	cp := h.env("staging copy")
	if cp == nil || len(cp.Variables) != 3 {
		t.Fatalf("copy = %+v", cp)
	}
	if cp.ID == "e-stg" {
		t.Fatal("copy shares the ID")
	}
	tok := cp.Variables[1]
	if tok.SecretRef != "" || tok.Value != "s3cr3t-value" || !tok.IsSecret() {
		t.Fatalf("secret in copy = %+v", tok)
	}
	if cp.Variables[2].Disabled != true {
		t.Fatal("disabled flag lost")
	}
}

func TestDeleteNeedsExplicitChoice(t *testing.T) {
	h := newHarness(t, 100, 30)
	h.key("d")
	h.mustHave("Delete environment")
	h.mustHave("1 secret value")
	h.key("enter") // focus starts on Cancel
	if len(h.ws.Environments) != 2 {
		t.Fatal("Enter on the default choice deleted the environment")
	}
	h.key("d", "right", "enter")
	if h.env("staging") != nil || len(h.ws.Environments) != 1 {
		t.Fatal("environment not deleted")
	}
	var del *tui.SaveMsg
	for _, s := range h.saves() {
		if s.Kind == tui.DeleteEnvironment {
			s := s
			del = &s
		}
	}
	if del == nil || del.ID != "e-stg" {
		t.Fatalf("saves = %+v", h.saves())
	}
}

func TestProductionToggle(t *testing.T) {
	h := newHarness(t, 100, 30)
	h.key("p")
	if !h.ws.Environments[0].Production {
		t.Fatal("not marked production")
	}
	h.mustHave("Marked as production")
	h.key("p")
	if h.ws.Environments[0].Production {
		t.Fatal("still production")
	}
}

func TestAddEditVariable(t *testing.T) {
	h := newHarness(t, 100, 30)
	h.key("enter", "a") // into table, add
	h.typ("amount")
	h.key("tab")
	h.typ("42")
	h.key("tab") // description
	h.typ("cents")
	h.key("esc")
	vs := h.env("staging").Variables
	if len(vs) != 4 {
		t.Fatalf("vars = %+v", vs)
	}
	got := vs[1]
	if got.Key != "amount" || got.Value != "42" || got.Description != "cents" || got.Disabled {
		t.Fatalf("var = %+v", got)
	}
}

func TestEmptyNewRowIsDropped(t *testing.T) {
	h := newHarness(t, 100, 30)
	h.key("enter", "a", "esc", "esc")
	if n := len(h.env("staging").Variables); n != 3 {
		t.Fatalf("vars = %d", n)
	}
}

func TestToggleDeleteMove(t *testing.T) {
	h := newHarness(t, 100, 30)
	h.key("enter", "space")
	if !h.env("staging").Variables[0].Disabled {
		t.Fatal("not disabled")
	}
	h.key("alt+down")
	if h.env("staging").Variables[1].Key != "baseUrl" {
		t.Fatal("not moved")
	}
	h.key("d")
	if len(h.env("staging").Variables) != 2 {
		t.Fatal("not deleted")
	}
}

func TestSecretsAreMaskedUntilRevealAndRevealHydrates(t *testing.T) {
	h := newHarness(t, 100, 30)
	h.key("enter")
	h.mustNotHave("s3cr3t-value")
	h.mustHave("1 secret (hidden")
	if h.env("staging").Variables[1].Value != "" {
		t.Fatal("secret should not be in memory before reveal")
	}
	h.key("r")
	h.mustHave("s3cr3t-value")
	h.mustHave("1 secret (shown)")
	h.key("r")
	h.mustNotHave("s3cr3t-value")
	// Leaving the variables hides them again.
	h.key("r", "esc", "down")
	h.mustNotHave("s3cr3t-value")
}

func TestEditingSecretValueIsMasked(t *testing.T) {
	h := newHarness(t, 100, 30)
	h.key("enter", "j", "enter") // token row, edit value (hydrates for editing)
	h.mustHave("Value (secret)")
	h.mustNotHave("s3cr3t-value")
	h.key("esc", "shift+S") // normal, then change the whole line
	h.typ("hunter2")
	h.mustNotHave("hunter2")
	if h.env("staging").Variables[1].Value != "hunter2" {
		t.Fatal("value not stored")
	}
	h.key("ctrl+r")
	h.mustHave("hunter2")
	h.key("esc", "esc")
}

func TestMakeSecretAndMakePlain(t *testing.T) {
	h := newHarness(t, 100, 30)
	h.key("enter", "s") // baseUrl -> secret
	v := h.env("staging").Variables[0]
	if !v.IsSecret() || v.Value != "https://staging.example.com" {
		t.Fatalf("var = %+v", v)
	}
	h.mustNotHave("https://staging.example.com")
	h.key("s") // back: must confirm
	h.mustHave("Store as plain text?")
	h.key("esc")
	if !h.env("staging").Variables[0].IsSecret() {
		t.Fatal("cancel changed the variable")
	}
	h.key("s", "right", "enter")
	if h.env("staging").Variables[0].IsSecret() {
		t.Fatal("not made plain")
	}
}

func TestMakePlainReadsSecretFirst(t *testing.T) {
	h := newHarness(t, 100, 30)
	h.key("enter", "down", "s", "right", "enter")
	v := h.env("staging").Variables[1]
	if v.IsSecret() || v.Value != "s3cr3t-value" || v.SecretRef != "" {
		t.Fatalf("var = %+v", v)
	}
}

func TestGlobalsTab(t *testing.T) {
	h := newHarness(t, 100, 30)
	h.key("2")
	h.mustHave("region")
	h.key("a")
	h.typ("zone")
	h.key("tab")
	h.typ("a")
	h.key("esc")
	if len(h.ws.Globals.Variables) != 2 {
		t.Fatalf("globals = %+v", h.ws.Globals.Variables)
	}
	h.advance(time.Second)
	if s := h.saves(); len(s) != 1 || s[0].Kind != tui.SaveGlobals {
		t.Fatalf("saves = %+v", s)
	}
}

func TestGlobalsCreatedOnFirstVariable(t *testing.T) {
	h := newHarness(t, 100, 30)
	h.ws.Globals = nil
	h.key("2", "a")
	h.typ("k")
	h.key("esc")
	if h.ws.Globals == nil || len(h.ws.Globals.Variables) != 1 || h.ws.Globals.ID == "" {
		t.Fatalf("globals = %+v", h.ws.Globals)
	}
}

func TestCollectionVariables(t *testing.T) {
	h := newHarness(t, 100, 30)
	h.key("3")
	h.mustHave("amount")
	h.mustNotHave("New")
	h.key("enter", "enter") // edit value
	h.key("backspace")
	h.typ("9")
	h.key("esc", "esc") // insert → normal → leave cell
	if got := h.ws.Collections[0].Variables[0].Value; got != "120"+"9" {
		t.Fatalf("value = %q", got)
	}
	h.advance(time.Second)
	s := h.saves()
	if len(s) != 1 || s[0].Kind != tui.SaveCollection || s[0].Collection != h.ws.Collections[0].ID {
		t.Fatalf("saves = %+v", s)
	}
	// Collections have no create/rename/delete here.
	h.key("esc", "n")
	h.mustNotHave("New name")
}

func TestAutosaveDebouncesAndCloseFlushes(t *testing.T) {
	h := newHarness(t, 100, 30)
	h.key("enter", "space")
	h.advance(200 * time.Millisecond)
	h.key("space")
	h.advance(400 * time.Millisecond)
	if len(h.saves()) != 0 {
		t.Fatal("saved before the debounce")
	}
	h.key("esc", "esc") // back to the list, then close
	if !h.closed {
		t.Fatal("screen did not close")
	}
	if len(h.saves()) != 1 {
		t.Fatalf("close must flush, saves = %d", len(h.saves()))
	}
	h.advance(time.Second)
	if len(h.saves()) != 1 {
		t.Fatal("timer saved a second time")
	}
}

func TestSaveFailureShown(t *testing.T) {
	h := newHarness(t, 100, 30)
	h.key("p")
	h.advance(time.Second)
	h.send(tui.SaveDoneMsg{Kind: tui.SaveEnvironment, ID: "e-stg", Err: context.DeadlineExceeded})
	h.mustHave("Not saved")
	h.send(tui.SaveDoneMsg{Kind: tui.SaveEnvironment, ID: "e-stg"})
	h.advance(5 * time.Second) // the notice fades and the label shows
	h.mustHave("Saved")
}

func TestEmptyWorkspaceShowsGuidance(t *testing.T) {
	h := newHarness(t, 100, 30)
	h.ws.Environments = nil
	h.mustHave("No environments yet")
	h.mustHave("Press n to create one")
}

func TestFocusOption(t *testing.T) {
	ws := workspace()
	s := New(tui.ScreenEnv{PaneEnv: tui.PaneEnv{Theme: ui.NewTheme(true, false, false)}, Workspace: ws, Focus: "e-prod", ActiveEnv: "e-stg"}, Deps{})
	if s.curEnv().ID != "e-prod" {
		t.Fatalf("focus = %s", s.curEnv().ID)
	}
}
