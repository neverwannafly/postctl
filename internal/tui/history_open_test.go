package tui

import "testing"

// The history screen loads its list when it is told it opened. Space h (and
// the palette's History) must tell it, like opening one request's history
// does; otherwise the list stays empty until some unrelated message arrives.
func TestOpeningHistoryTellsTheScreenItOpened(t *testing.T) {
	s := &fakeScreen{}
	h := newHarness(t, demo(), 120, 30, func(o *Options) {
		o.NewHistory = func(env ScreenEnv) Screen { s.env = env; return s }
	})
	h.keys("space", "h")
	h.mustHave("FAKE SCREEN")
	for _, m := range s.msgs {
		if _, ok := m.(ScreenOpenedMsg); ok {
			return
		}
	}
	t.Fatal("the history screen was never told it opened, so it would not load its entries")
}
