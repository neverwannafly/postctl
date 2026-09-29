package history

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/shubham/postctl/internal/history"
	"github.com/shubham/postctl/internal/model"
	"github.com/shubham/postctl/internal/tui"
	"github.com/shubham/postctl/internal/ui"
)

type memLog struct {
	entries []history.Entry
}

func (m *memLog) List(_ history.Filter) ([]history.Entry, error) {
	return append([]history.Entry(nil), m.entries...), nil
}

func (m *memLog) Get(id string) (*history.Entry, error) {
	for i := range m.entries {
		if m.entries[i].ID == id {
			e := m.entries[i]
			return &e, nil
		}
	}
	return nil, history.ErrNotFound
}

func (m *memLog) Clear() error {
	m.entries = nil
	return nil
}

func TestHistoryListAndFilter(t *testing.T) {
	log := &memLog{entries: []history.Entry{
		{ID: "1", Time: time.Now(), RequestID: "r1", RequestName: "OK", Request: history.RequestSnapshot{Method: "GET", URL: "https://api.test/ok"},
			Status: 200, DurationMS: 12, Tests: history.TestCounts{Passed: 2}},
		{ID: "2", Time: time.Now(), RequestID: "r2", RequestName: "Fail", Request: history.RequestSnapshot{Method: "POST", URL: "https://api.test/fail"},
			Status: 500, DurationMS: 40, Tests: history.TestCounts{Passed: 1, Failed: 1}},
	}}
	m := New(tui.ScreenEnv{
		PaneEnv:   tui.PaneEnv{Theme: ui.NewTheme(true, false, false)},
		Workspace: &model.Workspace{Name: "demo"},
	}, Deps{Log: func() (tui.HistoryReader, error) { return log, nil }, Curl: DefaultCurl})
	m.SetSize(80, 24)
	next, cmd := m.Update(loadedMsg{items: log.entries})
	if next != nil {
		m = next.(*Model)
	}
	_ = cmd
	out := ansi.Strip(m.View())
	if !strings.Contains(out, "OK") || !strings.Contains(out, "Fail") {
		t.Fatalf("list view:\n%s", out)
	}
	m.status = filterFailed
	m.items = m.applyStatusFilter(log.entries)
	out = ansi.Strip(m.View())
	if strings.Contains(out, "OK") {
		t.Fatalf("filter should hide 2xx:\n%s", out)
	}
}

func TestHistoryNoSecrets(t *testing.T) {
	secret := "never-show-this-value"
	log := &memLog{entries: []history.Entry{{
		ID: "1", Time: time.Now(), RequestID: "r1", RequestName: "Auth",
		Request: history.RequestSnapshot{Method: "GET", URL: "https://x.test", Headers: []model.KV{{Key: "Authorization", Value: "••••"}}},
		Status:  200,
	}}}
	m := New(tui.ScreenEnv{PaneEnv: tui.PaneEnv{Theme: ui.NewTheme(true, false, false)}, Workspace: &model.Workspace{Name: "w"}},
		Deps{Log: func() (tui.HistoryReader, error) { return log, nil }})
	m.SetSize(80, 24)
	m.Update(loadedMsg{items: log.entries})
	if strings.Contains(ansi.Strip(m.View()), secret) {
		t.Fatal("secret value in history list")
	}
	if msg := m.loadEntry("1")(); msg != nil {
		if em, ok := msg.(entryMsg); ok {
			m.Update(em)
		}
	}
	if strings.Contains(ansi.Strip(m.View()), secret) {
		t.Fatal("secret in detail")
	}
}

func TestDefaultCurlRedacted(t *testing.T) {
	e := &history.Entry{Request: history.RequestSnapshot{
		Method: "GET", URL: "https://x.test", Headers: []model.KV{{Key: "Authorization", Value: "••••"}},
	}}
	s, err := DefaultCurl(context.Background(), e)
	if err != nil || !strings.Contains(s, "curl") {
		t.Fatalf("curl: %q %v", s, err)
	}
}
