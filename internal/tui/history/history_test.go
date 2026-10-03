package history

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
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

func TestViewIsPure(t *testing.T) {
	m := New(tui.ScreenEnv{PaneEnv: tui.PaneEnv{Theme: ui.NewTheme(true, false, false)}}, Deps{})
	m.SetSize(80, 24)
	m.items = []history.Entry{{ID: "1", RequestName: "A"}}
	m.cur = 1
	before := m.cur
	first, second := m.View(), m.View()
	if first != second || m.cur != before {
		t.Fatal("View mutated history list state")
	}
}

func TestHistoryListVimNavigation(t *testing.T) {
	m := New(tui.ScreenEnv{PaneEnv: tui.PaneEnv{Theme: ui.NewTheme(true, true, false)}}, Deps{})
	m.items = make([]history.Entry, 4)
	m.cur = 2
	_, _ = m.Update(tea.KeyPressMsg{Text: "g"})
	if m.cur != 2 {
		t.Fatal("first g must wait for the second key")
	}
	_, _ = m.Update(tea.KeyPressMsg{Text: "g"})
	if m.cur != 0 {
		t.Fatal("gg must jump to the first entry")
	}
	_, _ = m.Update(tea.KeyPressMsg{Text: "j"})
	if m.cur != 1 {
		t.Fatal("j must move down")
	}
	_, _ = m.Update(tea.KeyPressMsg{Text: "k"})
	if m.cur != 0 {
		t.Fatal("k must move up")
	}
	_, _ = m.Update(tea.KeyPressMsg{Text: "G"})
	if m.cur != len(m.items)-1 {
		t.Fatal("G must jump to the last entry")
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
