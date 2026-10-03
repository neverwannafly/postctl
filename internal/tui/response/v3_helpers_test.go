package response

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/shubham/postctl/internal/history"
	"github.com/shubham/postctl/internal/httpx"
	"github.com/shubham/postctl/internal/model"
	"github.com/shubham/postctl/internal/tui"
	"github.com/shubham/postctl/internal/ui"
)

// v3Secret is a synthetic secret used only by the V3 tests.
const v3Secret = "v3-sentinel-secret-0001"

var v3Now = time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)

// fakeLog is an in-memory history reader. entries are newest first.
type fakeLog struct {
	entries []history.Entry
	full    map[string]*history.Entry
	listErr error
	lists   int
	filters []history.Filter
}

func (f *fakeLog) List(flt history.Filter) ([]history.Entry, error) {
	f.lists++
	f.filters = append(f.filters, flt)
	if f.listErr != nil {
		return nil, f.listErr
	}
	var out []history.Entry
	for _, e := range f.entries {
		if flt.RequestID != "" && e.RequestID != flt.RequestID {
			continue
		}
		out = append(out, e)
		if flt.Limit > 0 && len(out) >= flt.Limit {
			break
		}
	}
	return out, nil
}

func (f *fakeLog) Get(id string) (*history.Entry, error) {
	if e, ok := f.full[id]; ok {
		return e, nil
	}
	return nil, history.ErrNotFound
}

func (f *fakeLog) Clear() error { return nil }

func v3Entry(id string, req model.ID, age time.Duration, status int, reason, body string) history.Entry {
	return history.Entry{
		ID: id, Time: v3Now.Add(-age), RequestID: req, RequestName: "Items",
		Request: history.RequestSnapshot{Method: "GET", URL: "https://api.example.test/items"},
		Status:  status, StatusText: reason, DurationMS: 120, SizeBytes: 2048,
		Response: &history.ResponseSnapshot{
			Headers: []model.KV{{Key: "Content-Type", Value: "application/json"}, {Key: "X-Request", Value: "one"}},
			Body:    body,
		},
	}
}

// v3Pane builds a response pane with a history log on a selected request.
func v3Pane(t *testing.T, log *fakeLog, th ui.Theme) (*Pane, model.ID, *[]string) {
	t.Helper()
	var clip []string
	deps := Deps{
		Now:       func() time.Time { return v3Now },
		Clipboard: func(s string) tea.Cmd { clip = append(clip, s); return nil },
	}
	if log != nil {
		deps.Log = func() (tui.HistoryReader, error) { return log, nil }
	}
	p := NewWithDeps(tui.PaneEnv{Theme: th, Actions: &tui.Registry{}}, deps).(*Pane)
	p.SetSize(120, 30)
	id := model.NewID()
	req := &model.Request{ID: id, Name: "Items", Method: "GET", URL: model.URL{Raw: "https://api.example.test/items"}}
	v3Pump(t, p, tui.SelectionMsg{Ref: tui.RequestRef{Request: req}})
	_ = p.Focus()
	return p, id, &clip
}

func v3Result(id model.ID, body, historyID string, redactor *history.Redactor) tui.SendResult {
	return tui.SendResult{
		RequestID: id,
		HistoryID: historyID,
		Redactor:  redactor,
		Response: &httpx.Response{
			StatusCode: 200, Status: "OK", Body: []byte(body),
			Headers: []model.KV{
				{Key: "Content-Type", Value: "application/json"},
				{Key: "X-Api-Key", Value: v3Secret},
				{Key: "X-Request", Value: "two"},
			},
			Size:    httpx.Size{Headers: 100, Body: int64(len(body))},
			Timings: httpx.Timings{Total: 90 * time.Millisecond},
		},
	}
}

// v3Pump delivers msg to p and then runs every command it returns, feeding
// pane-private results back in. Messages meant for the shell are returned.
func v3Pump(t *testing.T, p *Pane, msg tea.Msg) []tea.Msg {
	t.Helper()
	_, cmd := p.Update(msg)
	return v3Run(t, p, cmd)
}

// v3Run runs cmd and everything it leads to.
func v3Run(t *testing.T, p *Pane, cmd tea.Cmd) []tea.Msg {
	t.Helper()
	var shell []tea.Msg
	queue := []tea.Cmd{cmd}
	for steps := 0; len(queue) > 0; steps++ {
		if steps > 200 {
			t.Fatal("command loop did not settle")
		}
		c := queue[0]
		queue = queue[1:]
		if c == nil {
			continue
		}
		switch m := c().(type) {
		case nil:
		case tea.BatchMsg:
			queue = append(queue, m...)
		case tui.ToastMsg, tui.FocusPaneMsg, tui.OpenRequestHistoryMsg, tui.CancelSendMsg, tui.ForwardKeyMsg, tui.ForwardSeqMsg:
			shell = append(shell, m)
		default:
			_, next := p.Update(m)
			queue = append(queue, next)
		}
	}
	return shell
}

func v3Key(s string) tea.KeyPressMsg {
	switch s {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	}
	r := []rune(s)
	return tea.KeyPressMsg{Code: r[0], Text: s}
}

func v3Type(t *testing.T, p *Pane, text string) {
	t.Helper()
	for _, r := range text {
		v3Pump(t, p, tea.KeyPressMsg{Code: r, Text: string(r)})
	}
}

func v3Frame(p *Pane) string { return ansi.Strip(p.View()) }

func v3Toasts(msgs []tea.Msg) []string {
	var out []string
	for _, m := range msgs {
		if ts, ok := m.(tui.ToastMsg); ok {
			out = append(out, ts.Text)
		}
	}
	return out
}

func v3Action(t *testing.T, p *Pane, id string) tui.Action {
	t.Helper()
	for _, a := range p.Actions() {
		if a.ID == id {
			return a
		}
	}
	t.Fatalf("action %s is not registered", id)
	return tui.Action{}
}

// v3RunAction runs an action and then its commands.
func v3RunAction(t *testing.T, p *Pane, id string) []tea.Msg {
	t.Helper()
	a := v3Action(t, p, id)
	if a.Enabled != nil && !a.Enabled() {
		t.Fatalf("action %s is disabled", id)
	}
	return v3Run(t, p, a.Run())
}

// vimBodyScene shows a small JSON response in the Body tab of a focused pane.
func vimBodyScene(p *Pane) {
	id := model.NewID()
	p.ref = tui.RequestRef{Request: &model.Request{ID: id}}
	p.applyResult(demoResult(id))
	_ = p.Focus()
}

// vimKeys presses keys on the pane's live Body buffer, running the commands
// they return.
func vimKeys(t *testing.T, p *Pane, keys ...string) {
	t.Helper()
	for _, k := range keys {
		v3Pump(t, p, keyOf(k))
	}
}
