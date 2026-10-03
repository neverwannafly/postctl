package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/shubham/postctl/internal/model"
	"github.com/shubham/postctl/internal/ui"
)

// fakeScreen is a hosted screen that records what it is given. "esc" closes
// it.
type fakeScreen struct {
	w, h int
	keys []string
	msgs []tea.Msg
	env  ScreenEnv
	// onKey runs for every key before the default handling.
	onKey func(s *fakeScreen, k string)
}

func (s *fakeScreen) Update(msg tea.Msg) (Screen, tea.Cmd) {
	s.msgs = append(s.msgs, msg)
	if k, ok := msg.(tea.KeyPressMsg); ok {
		s.keys = append(s.keys, k.String())
		if s.onKey != nil {
			s.onKey(s, k.String())
		}
		if k.String() == "esc" {
			return nil, nil
		}
	}
	return s, nil
}
func (s *fakeScreen) View() string     { return fmt.Sprintf("FAKE SCREEN %03dx%03d", s.w, s.h) }
func (s *fakeScreen) SetSize(w, h int) { s.w, s.h = w, h }
func (s *fakeScreen) KeyHints() []ui.Hint {
	return []ui.Hint{{Key: "Esc", Label: "Leave fake", Priority: 1}}
}

func screenFixture(t *testing.T) (*harness, *fakeScreen) {
	t.Helper()
	s := &fakeScreen{}
	h := newHarness(t, demo(), 120, 30, func(o *Options) {
		o.NewEnvEditor = func(env ScreenEnv) Screen { s.env = env; return s }
	})
	return h, s
}

func TestCtrlEOpensTheEnvironmentEditorAsAnOverlay(t *testing.T) {
	h, s := screenFixture(t)
	h.keys("space", "E")
	h.mustHave("FAKE SCREEN")
	h.mustHave("Leave fake") // its hints are the footer
	if s.w < 110 || s.w > 120 || s.h < 20 {
		t.Fatalf("screen size = %dx%d", s.w, s.h)
	}
	if s.env.Workspace != h.a.ws || s.env.Backend == nil || s.env.Actions == nil {
		t.Fatalf("screen env = %+v", s.env)
	}
	h.msg(tea.WindowSizeMsg{Width: 90, Height: 26})
	h.mustHave("FAKE SCREEN 088x")
}

func TestScreenGetsItsKeys(t *testing.T) {
	h, s := screenFixture(t)
	h.keys("space", "E")
	h.keys("a", "j", "?", "E", "/", "tab")
	got := " " + strings.Join(s.keys, " ") + " "
	for _, want := range []string{"a", "j", "?", "E", "/", "tab"} {
		if !strings.Contains(got, " "+want+" ") {
			t.Fatalf("screen keys = %q, missing %q", got, want)
		}
	}
	h.mustHave("FAKE SCREEN") // still open: no key closed it or opened help
}

func TestEscClosesTheScreenAndBroadcastsTheEnvironment(t *testing.T) {
	pane := &msgPane{}
	s := &fakeScreen{}
	h := newHarness(t, demo(), 120, 30, func(o *Options) {
		o.NewEnvEditor = func(ScreenEnv) Screen { return s }
		o.NewRequestPane = func(PaneEnv) Pane { return pane }
	})
	h.keys("space", "e", "down", "enter") // pick an environment
	if h.a.activeEnv == "" {
		t.Fatal("no environment became active")
	}
	h.keys("space", "E", "esc")
	h.mustNotHave("FAKE SCREEN")
	found := false
	pane.mu.Lock()
	for _, m := range pane.msgs {
		if c, ok := m.(EnvironmentChangedMsg); ok && c.Env != nil && c.Env.ID == h.a.activeEnv {
			found = true
		}
	}
	pane.mu.Unlock()
	if !found {
		t.Fatal("panes were not told the environment after the screen closed")
	}
}

func TestDeletingTheActiveEnvironmentInAScreenClearsIt(t *testing.T) {
	s := &fakeScreen{onKey: func(s *fakeScreen, k string) {
		if k == "x" {
			for i, e := range s.env.Workspace.Environments {
				if e.ID == s.env.ActiveEnv {
					ws := s.env.Workspace
					ws.Environments = append(ws.Environments[:i:i], ws.Environments[i+1:]...)
					return
				}
			}
		}
	}}
	h := newHarness(t, demo(), 120, 30, func(o *Options) {
		o.NewEnvEditor = func(env ScreenEnv) Screen { s.env = env; return s }
	})
	h.keys("space", "e", "down", "enter")
	if h.a.activeEnv == "" {
		t.Fatal("no environment became active")
	}
	h.keys("space", "E", "x", "esc")
	if h.a.activeEnv != "" {
		t.Fatalf("active environment %q survived its deletion", h.a.activeEnv)
	}
	h.mustHave("The active environment was deleted")
	if h.be.savedState().ActiveEnvironment != "" {
		t.Fatal("state still names the deleted environment")
	}
}

func TestScreenReceivesSaveDoneMessages(t *testing.T) {
	h, s := screenFixture(t)
	h.keys("space", "E")
	env := h.a.ws.Environments[0]
	settle(t, h, func() tea.Msg { return SaveMsg{Kind: SaveEnvironment, ID: env.ID} })
	got := false
	for _, m := range s.msgs {
		if d, ok := m.(SaveDoneMsg); ok && d.ID == env.ID {
			got = true
		}
	}
	if !got {
		t.Fatalf("screen did not receive SaveDoneMsg; got %d messages", len(s.msgs))
	}
}

func TestEnvSwitcherEditKeyFocusesTheEnvironment(t *testing.T) {
	h, s := screenFixture(t)
	h.keys("space", "e", "down", "down", "e")
	h.mustHave("FAKE SCREEN")
	if s.env.Focus == "" {
		t.Fatal("the highlighted environment was not passed as Focus")
	}
}

func TestScreenActionsAppearOnlyWhenSupplied(t *testing.T) {
	h := newHarness(t, demo(), 120, 30)
	h.keys("ctrl+p")
	h.typeText("cookies")
	h.mustNotHave("Cookies")
	h.keys("esc", "ctrl+e")
	h.mustNotHave("FAKE SCREEN")

	opened := 0
	h2 := newHarness(t, demo(), 120, 30, func(o *Options) {
		o.NewCookieManager = func(ScreenEnv) Screen { opened++; return &fakeScreen{} }
	})
	h2.keys("ctrl+p")
	h2.typeText("cookies")
	h2.mustHave("Cookies")
	h2.keys("enter")
	h2.mustHave("FAKE SCREEN")
	if opened != 1 {
		t.Fatalf("cookie manager opened %d times", opened)
	}
}

func TestNilScreenFromFactoryDoesNothing(t *testing.T) {
	h := newHarness(t, demo(), 120, 30, func(o *Options) {
		o.NewEnvEditor = func(ScreenEnv) Screen { return nil }
	})
	h.keys("space", "E")
	h.mustNotHave("FAKE SCREEN")
	h.keys("down") // the tree still works
}

func TestEnvEditorNeedsAWorkspace(t *testing.T) {
	h := newHarness(t, nil, 120, 30, func(o *Options) {
		o.NewEnvEditor = func(ScreenEnv) Screen { return &fakeScreen{} }
	})
	h.keys("space", "E")
	h.mustNotHave("FAKE SCREEN")
}

// Ctrl-P closes a hosted screen through the same lifecycle as Esc: after an
// environment is renamed or deleted in the editor, the active environment and
// the panes are synced exactly the same way.
func TestCtrlPClosesTheScreenLikeEsc(t *testing.T) {
	type result struct {
		activeEnv model.ID
		lastEnv   string // name the pane was last told, "-" for none
		toast     bool
		saved     model.ID
	}
	run := func(closeKey, edit string) result {
		pane := &msgPane{}
		s := &fakeScreen{onKey: func(s *fakeScreen, k string) {
			if k != edit {
				return
			}
			ws := s.env.Workspace
			for i, e := range ws.Environments {
				if e.ID != s.env.ActiveEnv {
					continue
				}
				if k == "x" {
					ws.Environments = append(ws.Environments[:i:i], ws.Environments[i+1:]...)
				} else {
					e.Name = "renamed-env"
				}
				return
			}
		}}
		h := newHarness(t, demo(), 120, 30, func(o *Options) {
			o.NewEnvEditor = func(env ScreenEnv) Screen { s.env = env; return s }
			o.NewRequestPane = func(PaneEnv) Pane { return pane }
		})
		h.keys("space", "e", "down", "enter")
		if h.a.activeEnv == "" {
			t.Fatal("no environment became active")
		}
		h.keys("space", "E", edit, closeKey)
		h.mustNotHave("FAKE SCREEN")
		var r result
		r.activeEnv = h.a.activeEnv
		r.toast = strings.Contains(h.screen(), "The active environment was deleted")
		r.saved = h.be.savedState().ActiveEnvironment
		r.lastEnv = "?"
		pane.mu.Lock()
		for _, m := range pane.msgs {
			if c, ok := m.(EnvironmentChangedMsg); ok {
				r.lastEnv = "-"
				if c.Env != nil {
					r.lastEnv = c.Env.Name
				}
			}
		}
		pane.mu.Unlock()
		return r
	}
	for _, edit := range []string{"x", "r"} {
		viaEsc, viaCtrlP := run("esc", edit), run("ctrl+p", edit)
		if edit == "r" && viaEsc.lastEnv != "renamed-env" {
			t.Fatalf("the pane must learn the renamed environment, got %q", viaEsc.lastEnv)
		}
		if edit == "x" && (viaEsc.activeEnv != "" || !viaEsc.toast || viaEsc.lastEnv != "-") {
			t.Fatalf("deleting the active environment after Esc: %+v", viaEsc)
		}
		// Environment IDs are random per run, so compare what they mean.
		if (viaEsc.activeEnv == "") != (viaCtrlP.activeEnv == "") || (viaEsc.saved == "") != (viaCtrlP.saved == "") ||
			viaEsc.lastEnv != viaCtrlP.lastEnv || viaEsc.toast != viaCtrlP.toast {
			t.Errorf("edit %q: Ctrl-P left different state than Esc: esc=%+v ctrl-p=%+v", edit, viaEsc, viaCtrlP)
		}
	}
}

// modeScreen is a fakeScreen that reports a vim mode.
type modeScreen struct{ fakeScreen }

func (s *modeScreen) VimMode() string { return "INSERT" }

func (s *modeScreen) Update(msg tea.Msg) (Screen, tea.Cmd) {
	next, cmd := s.fakeScreen.Update(msg)
	if next == nil {
		return nil, cmd
	}
	return s, cmd
}

// The footer names the mode of a screen's text field once, not twice.
func TestScreenModeAppearsOnceInTheFooter(t *testing.T) {
	s := &modeScreen{}
	h := newHarness(t, demo(), 120, 30, func(o *Options) {
		o.NewEnvEditor = func(ScreenEnv) Screen { return s }
	})
	h.keys("space", "E")
	h.mustHave("Leave fake")
	if n := strings.Count(h.screen(), "INSERT"); n != 1 {
		t.Fatalf("the INSERT badge appears %d times in the frame, want 1", n)
	}
}
