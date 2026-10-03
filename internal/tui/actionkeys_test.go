package tui

import (
	"strings"
	"testing"
)

// Every Action.Keys string of the shell (built-ins and hosted screens) is
// empty or a binding the shell really dispatches to that action.
func TestShellActionKeysAreRealBindings(t *testing.T) {
	h := newHarness(t, demo(), 160, 30, func(o *Options) {
		o.NewEnvEditor = func(ScreenEnv) Screen { return nil }
		o.NewCookieManager = func(ScreenEnv) Screen { return nil }
		o.NewRunner = func(ScreenEnv) Screen { return nil }
		o.NewHistory = func(ScreenEnv) Screen { return nil }
	})
	seen := map[string]bool{}
	for _, act := range append(h.a.builtinActions(), h.a.screenActions()...) {
		seen[act.ID] = true
		if bad := UnboundActionKeys(act.ID, act.Keys); len(bad) > 0 {
			t.Errorf("action %s advertises %q, which the shell does not dispatch to it", act.ID, strings.Join(bad, ", "))
		}
	}
	for _, id := range []string{"env.edit", "runner.open", "history.open", "cookies.open", "request.find"} {
		if !seen[id] {
			t.Errorf("action %s is not registered", id)
		}
	}
}

func TestUnboundActionKeysCatchesStaleStrings(t *testing.T) {
	for _, c := range []struct {
		id, keys string
		bad      int
	}{
		{"env.edit", "Ctrl-E", 1},
		{"runner.open", "R", 1},
		{"env.edit", "Space E", 0},
		{"palette.open", "Ctrl-P / Space p", 0},
		{"palette.open", "Ctrl-P / Space z", 1},
		{"request.find", "Space f / Space Space", 0},
		{"request.tab.params", "gp", 0},
		{"request.tab.params", "gh", 1},
		{"app.quit", "q / Space q / :q", 0},
		{"help.keys", "", 0},
	} {
		if got := UnboundActionKeys(c.id, c.keys); len(got) != c.bad {
			t.Errorf("%s %q: bad = %v, want %d", c.id, c.keys, got, c.bad)
		}
	}
}
