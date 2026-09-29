// Package history is the send-history browser screen (H): filter recent sends,
// inspect redacted request/response detail, and re-open, re-send, or copy curl.
package history

import (
	"context"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/atotto/clipboard"
	"github.com/shubham/postctl/internal/history"
	"github.com/shubham/postctl/internal/httpx"
	"github.com/shubham/postctl/internal/model"
	"github.com/shubham/postctl/internal/tui"
	"github.com/shubham/postctl/internal/tui/response"
	"github.com/shubham/postctl/internal/tui/textin"
	"github.com/shubham/postctl/internal/ui"
)

// Deps are injectable effects for tests.
type Deps struct {
	// Log returns the workspace history log.
	Log func() (tui.HistoryReader, error)
	// Curl builds a redacted curl command for an entry.
	Curl func(ctx context.Context, e *history.Entry) (string, error)
}

// Factory returns a screen factory wired with d.
func Factory(d Deps) tui.ScreenFactory {
	return func(env tui.ScreenEnv) tui.Screen { return New(env, d) }
}

type phase int

const (
	phList phase = iota
	phDetail
	phConfirmClear
)

type statusFilter int

const (
	filterAll statusFilter = iota
	filterOK
	filterClient
	filterServer
	filterFailed
)

// Model is the history screen.
type Model struct {
	th   ui.Theme
	env  tui.ScreenEnv
	deps Deps
	w, h int

	phase phase
	items []history.Entry
	cur   int
	top   int

	filter    *textin.Input
	filtering bool
	status    statusFilter
	requestID model.ID

	detailTab int
	detail    *history.Entry
	detailErr error
	scroll    int

	conf   ui.Confirm
	notice string
	fail   bool
	loaded bool
}

// New builds the history browser.
func New(env tui.ScreenEnv, d Deps) *Model {
	m := &Model{th: env.Theme, env: env, deps: d, filter: textin.New("")}
	return m
}

// SetSize implements tui.Screen.
func (m *Model) SetSize(w, h int) { m.w, m.h = w, h }

// KeyHints implements tui.Screen.
func (m *Model) KeyHints() []ui.Hint {
	switch m.phase {
	case phConfirmClear:
		return []ui.Hint{{Key: "Enter", Label: "Choose", Priority: 1}, {Key: "←→", Label: "Switch", Priority: 2}, {Key: "Esc", Label: "Cancel", Priority: 1}}
	case phDetail:
		return []ui.Hint{
			{Key: "Tab", Label: "Section", Priority: 2},
			{Key: "o", Label: "Open request", Priority: 1},
			{Key: "s", Label: "Re-send", Priority: 1},
			{Key: "c", Label: "Copy curl", Priority: 3},
			{Key: "Esc", Label: "Back", Priority: 1},
		}
	default:
		if m.filtering {
			return []ui.Hint{{Key: "Enter", Label: "Apply", Priority: 1}, {Key: "Esc", Label: "Clear", Priority: 1}}
		}
		return []ui.Hint{
			{Key: "/", Label: "Filter", Priority: 1},
			{Key: "f", Label: "Status filter", Priority: 2},
			{Key: "Enter", Label: "Detail", Priority: 1},
			{Key: "o", Label: "Open", Priority: 2},
			{Key: "s", Label: "Re-send", Priority: 2},
			{Key: "c", Label: "Clear all", Priority: 4},
			{Key: "Esc", Label: "Close", Priority: 1},
		}
	}
}

type loadedMsg struct {
	items []history.Entry
	err   error
}

type entryMsg struct {
	entry *history.Entry
	err   error
}

type clearedMsg struct {
	err error
}

type curlMsg struct {
	text string
	err  error
}

// Update implements tui.Screen.
func (m *Model) Update(msg tea.Msg) (tui.Screen, tea.Cmd) {
	switch v := msg.(type) {
	case tui.ThemeChangedMsg:
		m.th = v.Theme
	case loadedMsg:
		m.loaded = true
		m.items = v.items
		m.cur = min(m.cur, max(0, len(m.items)-1))
		if v.err != nil {
			m.notice, m.fail = "Could not load history: "+v.err.Error(), true
		}
		return m, nil
	case entryMsg:
		if v.err != nil {
			m.notice, m.fail = v.err.Error(), true
			return m, nil
		}
		m.detail = v.entry
		m.detailErr = nil
		m.phase = phDetail
		m.detailTab = 0
		m.scroll = 0
		return m, nil
	case clearedMsg:
		if v.err != nil {
			m.notice, m.fail = "Could not clear history: "+v.err.Error(), true
			return m, nil
		}
		m.phase = phList
		return m, m.reload()
	case curlMsg:
		if v.err != nil {
			m.notice, m.fail = v.err.Error(), true
			return m, nil
		}
		_ = clipboard.WriteAll(v.text)
		m.notice, m.fail = "Copied curl to clipboard.", false
		return m, nil
	case tea.KeyPressMsg:
		if m.phase == phConfirmClear {
			return m.confirmKey(v)
		}
		if m.phase == phDetail {
			return m.detailKey(v)
		}
		if m.filtering {
			return m.filterKey(v)
		}
		return m.listKey(v)
	}
	if m.phase == phList && !m.loaded && m.deps.Log != nil {
		return m, m.reload()
	}
	return m, nil
}

func (m *Model) reload() tea.Cmd {
	if m.deps.Log == nil {
		return nil
	}
	return func() tea.Msg {
		log, err := m.deps.Log()
		if err != nil {
			return loadedMsg{err: err}
		}
		f := history.Filter{Query: m.filter.Text(), Limit: 500}
		if m.requestID != "" {
			f.RequestID = m.requestID
		}
		items, err := log.List(f)
		if err != nil {
			return loadedMsg{err: err}
		}
		items = m.applyStatusFilter(items)
		return loadedMsg{items: items}
	}
}

func (m *Model) applyStatusFilter(items []history.Entry) []history.Entry {
	if m.status == filterAll {
		return items
	}
	out := items[:0]
	for _, e := range items {
		if m.matchStatus(e) {
			out = append(out, e)
		}
	}
	return out
}

func (m *Model) matchStatus(e history.Entry) bool {
	switch m.status {
	case filterOK:
		return e.Status >= 200 && e.Status < 300 && e.Error == ""
	case filterClient:
		return e.Status >= 400 && e.Status < 500
	case filterServer:
		return e.Status >= 500
	case filterFailed:
		return e.Error != "" || e.Status == 0 || e.Tests.Failed > 0 || e.ScriptError != ""
	default:
		return true
	}
}

func (m *Model) listKey(k tea.KeyPressMsg) (tui.Screen, tea.Cmd) {
	m.notice = ""
	switch k.String() {
	case "esc", "q":
		return nil, nil
	case "up":
		m.move(-1)
	case "down":
		m.move(1)
	case "pgup":
		m.move(-m.listRows())
	case "pgdown":
		m.move(m.listRows())
	case "home":
		m.cur = 0
		m.top = 0
	case "end":
		m.cur = max(0, len(m.items)-1)
		m.clampTop()
	case "/":
		m.filtering = true
	case "f":
		m.status = statusFilter((int(m.status) + 1) % 5)
		return m, m.reload()
	case "enter":
		if e := m.current(); e != nil {
			return m, m.loadEntry(e.ID)
		}
	case "o":
		if e := m.current(); e != nil {
			return nil, func() tea.Msg { return tui.SelectRequestMsg{RequestID: e.RequestID} }
		}
	case "s":
		if e := m.current(); e != nil {
			return m, func() tea.Msg { return tui.SendSavedRequestMsg{RequestID: e.RequestID} }
		}
	case "c":
		m.phase = phConfirmClear
		m.conf = ui.Confirm{
			Title:   "Clear history",
			Body:    "Delete every entry in the send history? Saved requests are not affected.",
			Choices: []string{"Cancel", "Clear all"},
			Focus:   0,
			Danger:  true,
		}
	}
	return m, nil
}

func (m *Model) filterKey(k tea.KeyPressMsg) (tui.Screen, tea.Cmd) {
	switch k.String() {
	case "esc":
		m.filtering = false
		m.filter.Set("")
		return m, m.reload()
	case "enter":
		m.filtering = false
		return m, m.reload()
	default:
		m.filter.Update(k)
		return m, nil
	}
}

func (m *Model) detailKey(k tea.KeyPressMsg) (tui.Screen, tea.Cmd) {
	switch k.String() {
	case "esc":
		m.phase = phList
	case "tab":
		m.detailTab = (m.detailTab + 1) % 4
		m.scroll = 0
	case "up":
		m.scroll = max(0, m.scroll-1)
	case "down":
		m.scroll++
	case "o":
		return m, func() tea.Msg { return tui.SelectRequestMsg{RequestID: m.detail.RequestID} }
	case "s":
		return m, func() tea.Msg { return tui.SendSavedRequestMsg{RequestID: m.detail.RequestID} }
	case "y", "c":
		return m, m.copyCurl(m.detail)
	}
	return m, nil
}

func (m *Model) confirmKey(k tea.KeyPressMsg) (tui.Screen, tea.Cmd) {
	switch k.String() {
	case "esc":
		m.phase = phList
	case "left", "h":
		m.conf.Focus = 0
	case "right", "l":
		m.conf.Focus = 1
	case "enter":
		if m.conf.Focus == 1 {
			return m, m.clearAll()
		}
		m.phase = phList
	}
	return m, nil
}

func (m *Model) loadEntry(id string) tea.Cmd {
	return func() tea.Msg {
		log, err := m.deps.Log()
		if err != nil {
			return entryMsg{err: err}
		}
		e, err := log.Get(id)
		return entryMsg{entry: e, err: err}
	}
}

func (m *Model) clearAll() tea.Cmd {
	return func() tea.Msg {
		log, err := m.deps.Log()
		if err != nil {
			return clearedMsg{err: err}
		}
		return clearedMsg{err: log.Clear()}
	}
}

func (m *Model) copyCurl(e *history.Entry) tea.Cmd {
	if m.deps.Curl == nil {
		return func() tea.Msg { return curlMsg{err: fmt.Errorf("curl export is not available")} }
	}
	return func() tea.Msg {
		text, err := m.deps.Curl(context.Background(), e)
		return curlMsg{text: text, err: err}
	}
}

func (m *Model) current() *history.Entry {
	if m.cur < 0 || m.cur >= len(m.items) {
		return nil
	}
	return &m.items[m.cur]
}

func (m *Model) move(d int) {
	if len(m.items) == 0 {
		return
	}
	m.cur = clamp(m.cur+d, 0, len(m.items)-1)
	m.clampTop()
}

func (m *Model) clampTop() {
	rows := m.listRows()
	if m.cur < m.top {
		m.top = m.cur
	}
	if m.cur >= m.top+rows {
		m.top = m.cur - rows + 1
	}
}

func (m *Model) listRows() int { return max(1, m.h-6) }

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// View implements tui.Screen.
func (m *Model) View() string {
	switch m.phase {
	case phConfirmClear:
		return m.viewConfirm()
	case phDetail:
		return m.viewDetail()
	default:
		return m.viewList()
	}
}

func (m *Model) viewConfirm() string {
	t := m.th
	body := m.screenHeader("History")
	body += t.ConfirmView(m.conf, m.w)
	return body
}

func (m *Model) viewList() string {
	t := m.th
	title := "History"
	if m.filter.Text() != "" {
		title += "  /" + m.filter.Text()
	}
	body := m.screenHeader(title)
	if m.filtering {
		body += t.Render(ui.AccentPrimary, "Filter  "+m.filter.View(t, m.w-8, true)) + "\n"
	}
	if len(m.items) == 0 {
		body += t.EmptyState("No history yet", "Send a request with Ctrl-S; entries appear here.", "Ctrl-S Send", m.w)
	} else {
		body += m.renderList(m.w, m.listRows())
	}
	if m.notice != "" {
		tok := ui.TextSecondary
		if m.fail {
			tok = ui.StatusDanger
		}
		body += "\n" + t.Render(tok, m.notice)
	}
	body += "\n" + t.Render(ui.TextQuiet, statusFilterLabel(m.status))
	return body
}

func statusFilterLabel(f statusFilter) string {
	switch f {
	case filterOK:
		return "Filter  2xx"
	case filterClient:
		return "Filter  4xx"
	case filterServer:
		return "Filter  5xx"
	case filterFailed:
		return "Filter  failed"
	default:
		return "Filter  all"
	}
}

func (m *Model) renderList(w, rows int) string {
	t := m.th
	var lines []string
	day := ""
	for i, e := range m.items {
		if i < m.top {
			continue
		}
		if len(lines) >= rows {
			break
		}
		d := e.Time.Format("2006-01-02")
		if d != day {
			day = d
			lines = append(lines, t.Bold().Render(day))
		}
		mark := "  "
		tok := ui.TextPrimary
		if i == m.cur {
			mark = t.Render(ui.AccentPrimary, t.Glyphs.Select+" ")
			tok = ui.AccentPrimary
		}
		line := mark + t.MethodBadge(e.Request.Method, 5) + " "
		line += t.Render(tok, t.Truncate(e.RequestName, 18)) + "  "
		line += statusChip(t, e.Status, e.Error) + "  "
		line += t.Render(ui.TextSecondary, t.Truncate(e.Request.URL, w-ui.Width(line)-12)) + "  "
		meta := fmt.Sprintf("%d ms", e.DurationMS)
		if tot := e.Tests.Passed + e.Tests.Failed; tot > 0 {
			meta += fmt.Sprintf("  Tests %d/%d", e.Tests.Passed, tot)
		}
		line += t.Render(ui.TextQuiet, meta)
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

func statusChip(t ui.Theme, code int, err string) string {
	if err != "" || code == 0 {
		return t.Render(ui.StatusDanger, "× Failed")
	}
	text := fmt.Sprintf("%d", code)
	tok := ui.StatusInfo
	switch {
	case code >= 200 && code < 300:
		tok = ui.StatusSuccess
	case code >= 400 && code < 500:
		tok = ui.StatusWarning
	case code >= 500:
		tok = ui.StatusDanger
	}
	return t.Render(tok, text)
}

func (m *Model) viewDetail() string {
	t := m.th
	e := m.detail
	if e == nil {
		return m.screenHeader("History detail")
	}
	body := m.screenHeader("History detail")
	reqLine := t.MethodBadge(e.Request.Method, 5) + " " + t.Render(ui.TextPrimary, t.Truncate(e.Request.URL, m.w-8))
	body += reqLine + "\n"
	tabs := []string{"Request", "Body", "Headers", "Tests"}
	var tabRow []string
	for i, name := range tabs {
		if i == m.detailTab {
			tabRow = append(tabRow, t.Bold().Render(name))
		} else {
			tabRow = append(tabRow, t.Render(ui.TextSecondary, name))
		}
	}
	body += strings.Join(tabRow, "  ") + "\n"
	contentH := max(1, m.h-7)
	res := entryToSendResult(e)
	switch m.detailTab {
	case 0:
		body += m.viewRequestSnapshot(e, m.w, contentH)
	case 1:
		body += response.Snapshot(t, res, response.SnapshotBody, m.w, contentH)
	case 2:
		body += response.Snapshot(t, res, response.SnapshotHeaders, m.w, contentH)
	default:
		body += response.Snapshot(t, res, response.SnapshotTests, m.w, contentH)
	}
	if m.notice != "" {
		body += "\n" + t.Render(ui.TextSecondary, m.notice)
	}
	return body
}

func (m *Model) viewRequestSnapshot(e *history.Entry, w, h int) string {
	t := m.th
	var lines []string
	lines = append(lines, t.Render(ui.TextSecondary, "Method  ")+t.Render(ui.TextPrimary, e.Request.Method))
	lines = append(lines, t.Render(ui.TextSecondary, "URL  ")+t.Render(ui.TextPrimary, t.Truncate(e.Request.URL, w-6)))
	for _, kv := range e.Request.Headers {
		lines = append(lines, t.Render(ui.TextSecondary, kv.Key+"  ")+t.Render(ui.TextPrimary, t.Truncate(kv.Value, w-10)))
	}
	if e.Request.Body != "" {
		lines = append(lines, "")
		lines = append(lines, t.Render(ui.TextPrimary, t.Truncate(e.Request.Body, w)))
	}
	start := min(m.scroll, max(0, len(lines)-h))
	end := min(len(lines), start+h)
	return strings.Join(lines[start:end], "\n")
}

func (m *Model) screenHeader(title string) string {
	env := ui.EnvInfo{}
	ws := title
	if m.env.Workspace != nil {
		if m.env.Workspace.Name != "" {
			ws = m.env.Workspace.Name + " — " + title
		}
		if m.env.ActiveEnv != "" {
			if e := m.env.Workspace.Environment(m.env.ActiveEnv); e != nil {
				env = ui.EnvInfo{Name: e.Name, Production: e.Production}
			}
		}
	}
	return m.th.Header(m.w, ui.HeaderInfo{Workspace: ws, Env: env})
}

// DefaultCurl builds a redacted curl command from a history entry.
func DefaultCurl(_ context.Context, e *history.Entry) (string, error) {
	req := &model.Request{
		Method:  e.Request.Method,
		URL:     model.URL{Raw: e.Request.URL},
		Headers: append([]model.KV(nil), e.Request.Headers...),
	}
	if e.Request.Body != "" {
		req.Body = model.Body{Mode: model.BodyRaw, Raw: e.Request.Body}
	}
	return httpx.Curl(req), nil
}
