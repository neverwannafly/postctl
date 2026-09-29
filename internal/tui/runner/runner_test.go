package runner

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/shubham/postctl/internal/engine"
	"github.com/shubham/postctl/internal/model"
	"github.com/shubham/postctl/internal/tui"
	"github.com/shubham/postctl/internal/tui/fakerun"
	"github.com/shubham/postctl/internal/ui"
)

func testEnv(w, h int) tui.ScreenEnv {
	col := &model.Collection{ID: "c1", Name: "API", Items: []model.Item{
		model.NewRequest("r1", "GET", "{{base}}/ok"),
		model.NewRequest("r2", "POST", "{{base}}/fail"),
	}}
	ws := &model.Workspace{Name: "demo", Collections: []*model.Collection{col}}
	return tui.ScreenEnv{
		PaneEnv:   tui.PaneEnv{Theme: ui.NewTheme(true, false, false)},
		Workspace: ws,
		RunScope:  &tui.RunScope{Collection: col},
	}
}

func TestRunEventsReachSummary(t *testing.T) {
	fr := &fakerun.Runner{
		Scripts: []fakerun.Script{
			{Name: "OK", Method: "GET", Steps: []string{"Waiting for response"}, Status: 200, TestsPass: 2},
			{Name: "Bad", Method: "POST", Steps: []string{"Running tests"}, Status: 500, TestsPass: 1, TestsFail: 1},
		},
	}
	m := New(testEnv(80, 24), Deps{Runner: fr})
	m.SetSize(80, 24)
	ch, err := fr.RunAsync(context.Background(), engine.RunInput{Collection: testEnv(80, 24).RunScope.Collection})
	if err != nil {
		t.Fatal(err)
	}
	m.phase = phLive
	for ev := range ch {
		done := false
		if _, ok := ev.(engine.RunFinished); ok {
			done = true
		}
		next, _ := m.Update(runEventsMsg{events: []engine.RunEvent{ev}, done: done})
		if mm, ok := next.(*Model); ok {
			m = mm
		}
	}
	out := ansi.Strip(m.View())
	if !strings.Contains(out, "Run summary") {
		t.Fatalf("expected summary:\n%s", out)
	}
	if !strings.Contains(out, "assertion failed") && !strings.Contains(out, "Bad") {
		t.Fatalf("expected failure list:\n%s", out)
	}
}

func TestBailStopsEarly(t *testing.T) {
	fr := &fakerun.Runner{
		Scripts: []fakerun.Script{
			{Name: "A", Method: "GET", TestsFail: 1, Steps: []string{"Running tests"}, Status: 200},
			{Name: "B", Method: "GET", Steps: []string{"Waiting for response"}, Status: 200},
		},
	}
	sumCh := make(chan *engine.RunSummary, 1)
	ch, _ := fr.RunAsync(context.Background(), engine.RunInput{
		Collection: testEnv(80, 24).RunScope.Collection,
		Bail:       true,
	})
	go func() {
		for ev := range ch {
			if rf, ok := ev.(engine.RunFinished); ok {
				sumCh <- rf.Summary
			}
		}
	}()
	sum := <-sumCh
	if sum.StopReason != engine.StopBail {
		t.Fatalf("stop reason: %q", sum.StopReason)
	}
}

func TestTransportErrorEvent(t *testing.T) {
	fr := &fakerun.Runner{
		Scripts: []fakerun.Script{
			{Name: "Down", Method: "GET", Err: errors.New("connection refused"), Steps: []string{"Waiting for response"}},
		},
	}
	ch, _ := fr.RunAsync(context.Background(), engine.RunInput{Collection: testEnv(80, 24).RunScope.Collection})
	for ev := range ch {
		if rf, ok := ev.(engine.RequestFinished); ok && rf.Outcome.Err == nil {
			t.Fatal("expected transport error")
		}
	}
}

func TestCancelRun(t *testing.T) {
	fr := &fakerun.Runner{
		Scripts: []fakerun.Script{{Name: "Slow", Method: "GET", Steps: []string{"Waiting for response"}, Delay: 50 * time.Millisecond}},
	}
	ctx, cancel := context.WithCancel(context.Background())
	ch, err := fr.RunAsync(ctx, engine.RunInput{Collection: testEnv(80, 24).RunScope.Collection, Iterations: 50})
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	var sum *engine.RunSummary
	for ev := range ch {
		if rf, ok := ev.(engine.RunFinished); ok {
			sum = rf.Summary
		}
	}
	if sum == nil || sum.StopReason != engine.StopCancelled {
		t.Fatalf("cancel: %+v", sum)
	}
}

func TestNoSecretsInSetupView(t *testing.T) {
	secret := "ultra-secret-token-xyz"
	fr := &fakerun.Runner{
		Scripts: []fakerun.Script{{Name: secret, Method: "GET", Steps: []string{"Running tests"}, Status: 200}},
	}
	m := New(testEnv(80, 24), Deps{Runner: fr})
	m.SetSize(80, 24)
	if strings.Contains(ansi.Strip(m.View()), secret) {
		t.Fatal("secret leaked in setup view")
	}
}

func TestSetupCtrlEnterStartsRun(t *testing.T) {
	fr := &fakerun.Runner{Scripts: []fakerun.Script{{Name: "A", Method: "GET", Status: 200}}}
	m := New(testEnv(80, 24), Deps{Runner: fr})
	m.SetSize(80, 24)
	next, cmd := m.Update(tea.KeyPressMsg{Text: "ctrl+enter"})
	if next != nil {
		m = next.(*Model)
	}
	if m.phase != phLive {
		t.Fatalf("phase = %v", m.phase)
	}
	if cmd == nil {
		t.Fatal("expected run command")
	}
}
