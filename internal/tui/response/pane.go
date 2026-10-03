package response

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/atotto/clipboard"
	"github.com/shubham/postctl/internal/history"
	"github.com/shubham/postctl/internal/httpx"
	"github.com/shubham/postctl/internal/model"
	"github.com/shubham/postctl/internal/tui"
	"github.com/shubham/postctl/internal/ui"
)

// New builds the Response pane for [tui.Options.NewResponsePane].
func New(env tui.PaneEnv) tui.Pane {
	p := &Pane{
		th:       env.Theme,
		byID:     map[model.ID]*storedResult{},
		tab:      tabBody,
		bodyMode: modePretty,
		spin:     env.Theme.NewSpinner(),
	}
	p.registerActions(env.Actions)
	return p
}

type tab int

const (
	tabBody tab = iota
	tabHeaders
	tabCookies
	tabTests
	tabConsole
	tabTimeline
	tabCount
)

type bodyMode int

const (
	modePretty bodyMode = iota
	modeRaw
	modeHex
)

type storedResult struct {
	result tui.SendResult
}

// Pane implements the response viewer.
type Pane struct {
	th                   ui.Theme
	w, h                 int
	focused              bool
	ref                  tui.RequestRef
	byID                 map[model.ID]*storedResult
	sendingID            model.ID
	sendStep             string
	sendStarted          time.Time
	tab                  tab
	bodyMode             bodyMode
	scroll               int
	wrap                 bool
	revealSecrets        bool
	search               textinput.Model
	searchActive         bool
	searchQuery          string
	searchIdx            int
	pathFilter           string
	pathInput            textinput.Model
	pathActive           bool
	jsonDoc              *jsonDoc
	rawIdx               lineIndex
	bodyKind             BodyKind
	bodyKey              string // cache key: request id + mode + filter
	testsFilterFailed    bool
	testScroll           int
	consoleScroll        int
	headerScroll         int
	cookieScroll         int
	spin                 spinner.Model
	lastTransportRequest model.ID
	demoInjected         bool
}

// Title implements [tui.Titled].
func (p *Pane) Title() string { return "Response" }

// SetSize implements [tui.Pane].
func (p *Pane) SetSize(w, h int) {
	p.w, p.h = w, h
	p.prepareCurrent()
}

// Focus implements [tui.Pane].
func (p *Pane) Focus() tea.Cmd {
	p.focused = true
	if p.sendingID != "" {
		return p.spin.Tick
	}
	return nil
}

// Blur implements [tui.Pane].
func (p *Pane) Blur() { p.focused = false }

// VimMode implements [tui.ModeReporter]. The search and path boxes are not
// modal yet, so editing one reports INSERT.
func (p *Pane) VimMode() string {
	if p.Editing() {
		return "INSERT"
	}
	return ""
}

// Sections implements [tui.SectionLister]: g jumps can reach the Body tab; [
// and ] step through every tab.
func (p *Pane) Sections() []string { return []string{tui.SectionRespBody} }

// Editing implements [tui.Editor].
func (p *Pane) Editing() bool { return p.searchActive || p.pathActive }

func (p *Pane) registerActions(reg *tui.Registry) {
	reg.Add(
		tui.Action{ID: "response.copyBody", Title: "Copy response body", Group: "Response", Keys: "y",
			Enabled: p.hasBody, Run: p.cmdCopyBody},
		tui.Action{ID: "response.saveBody", Title: "Save response body", Group: "Response",
			Enabled: p.hasBody, Run: p.cmdSaveBodyPrompt},
		tui.Action{ID: "response.openPager", Title: "Open body in pager", Group: "Response",
			Enabled: p.hasBody, Run: p.cmdOpenPager},
		tui.Action{ID: "response.toggleRaw", Title: "Toggle pretty or raw body", Group: "Response",
			Enabled: p.hasBody, Run: func() tea.Cmd {
				p.togglePrettyRaw()
				return nil
			}},
	)
}

func (p *Pane) hasBody() bool {
	r := p.current()
	return r != nil && r.result.Response != nil && len(r.result.Response.Body) > 0
}

// Actions implements [tui.ActionSource].
func (p *Pane) Actions() []tui.Action {
	return []tui.Action{
		{ID: "response.copyBody", Title: "Copy response body", Group: "Response", Keys: "y",
			Enabled: p.hasBody, Run: p.cmdCopyBody},
		{ID: "response.saveBody", Title: "Save response body", Group: "Response",
			Enabled: p.hasBody, Run: p.cmdSaveBodyPrompt},
		{ID: "response.openPager", Title: "Open body in pager", Group: "Response",
			Enabled: p.hasBody, Run: p.cmdOpenPager},
		{ID: "response.toggleRaw", Title: "Toggle pretty or raw body", Group: "Response",
			Enabled: p.hasBody, Run: func() tea.Cmd {
				p.togglePrettyRaw()
				return nil
			}},
	}
}

// Update implements [tui.Pane].
func (p *Pane) Update(msg tea.Msg) (tui.Pane, tea.Cmd) {
	var cmds []tea.Cmd
	switch msg := msg.(type) {
	case tui.SelectionMsg:
		p.ref = msg.Ref
		p.syncViewFromSelection()
		if demoEnabled() && msg.Ref.Request != nil && !p.demoInjected {
			p.demoInjected = true
			p.applyResult(demoResult(msg.Ref.Request.ID))
		}
	case tui.EnvironmentChangedMsg:
	case tui.WorkspaceReloadedMsg:
		p.remapAfterReload(msg.Workspace)
	case tui.ThemeChangedMsg:
		p.th = msg.Theme
		p.spin = p.th.NewSpinner()
	case tui.FocusSectionMsg:
		if msg.Section == tui.SectionRespBody {
			p.tab = tabBody
		}
	case tui.SectionStepMsg:
		if !p.Editing() && msg.Delta != 0 {
			p.tab = tab((int(p.tab) + msg.Delta + int(tabCount)) % int(tabCount))
		}
	case tui.SendStartedMsg:
		p.sendingID = msg.RequestID
		p.sendStep = "Starting"
		p.sendStarted = time.Now()
		if p.ref.Request != nil && p.ref.Request.ID == msg.RequestID {
			cmds = append(cmds, p.spin.Tick, tickElapsed())
		}
	case tui.SendProgressMsg:
		if msg.RequestID == p.sendingID {
			p.sendStep = msg.Step.Text
		}
	case tui.SendResultMsg:
		p.applyResult(msg.Result)
		p.sendingID = ""
	case spinner.TickMsg:
		if p.sendingID != "" && p.focused {
			var cmd tea.Cmd
			p.spin, cmd = p.spin.Update(msg)
			cmds = append(cmds, cmd)
		}
	case elapsedTickMsg:
		if p.sendingID != "" {
			cmds = append(cmds, tickElapsed())
		}
	default:
		if p.searchActive {
			var cmd tea.Cmd
			p.search, cmd = p.search.Update(msg)
			cmds = append(cmds, cmd)
			if km, ok := msg.(tea.KeyPressMsg); ok {
				switch km.String() {
				case "enter", "esc":
					p.searchActive = false
					p.searchQuery = p.search.Value()
					p.searchIdx = 0
					p.scroll = 0
				}
			}
			return p, tea.Batch(cmds...)
		}
		if p.pathActive {
			var cmd tea.Cmd
			p.pathInput, cmd = p.pathInput.Update(msg)
			cmds = append(cmds, cmd)
			if km, ok := msg.(tea.KeyPressMsg); ok {
				switch km.String() {
				case "enter":
					p.pathActive = false
					p.pathFilter = strings.TrimSpace(p.pathInput.Value())
					p.jsonDoc = nil
					p.scroll = 0
					p.prepareCurrent()
				case "esc":
					p.pathActive = false
				}
			}
			return p, tea.Batch(cmds...)
		}
		if km, ok := msg.(tea.KeyPressMsg); ok && p.focused {
			if cmd := p.onKey(km); cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
	}
	return p, tea.Batch(cmds...)
}

type elapsedTickMsg struct{}

func tickElapsed() tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return elapsedTickMsg{} })
}

func (p *Pane) onKey(msg tea.KeyPressMsg) tea.Cmd {
	s := msg.String()
	switch {
	case s == "esc":
		if p.sendingID != "" && p.ref.Request != nil {
			return func() tea.Msg { return tui.CancelSendMsg{RequestID: p.ref.Request.ID} }
		}
		return nil
	case s == "ctrl+s":
		if p.transportErr() && p.ref.Request != nil {
			return func() tea.Msg { return tui.RetrySendMsg{RequestID: p.ref.Request.ID} }
		}
		return nil
	case s == "y":
		return p.cmdCopyBody()
	case s == "w":
		p.wrap = !p.wrap
	case s >= "1" && s <= "6":
		p.tab = tab(s[0] - '1')
	case s == "/":
		p.beginSearch()
	case s == "n":
		p.searchNext(1)
	case s == "N":
		p.searchNext(-1)
	case s == ".":
		p.beginPathFilter()
	case s == "enter":
		p.toggleFoldCursor()
	case s == "left", s == "h":
		p.foldCursor(true)
	case s == "right", s == "l":
		p.foldCursor(false)
	case s == "up", s == "k":
		p.scrollUp()
	case s == "down", s == "j":
		p.scrollDown()
	case s == "pgup", s == "ctrl+u":
		p.scroll -= max(1, p.contentHeight()/2)
		p.clampScroll()
	case s == "pgdown", s == "ctrl+d":
		p.scroll += max(1, p.contentHeight()/2)
		p.clampScroll()
	case s == "home":
		p.scroll = 0
	case s == "end", s == "G":
		p.scroll = p.scrollTotal()
		p.clampScroll()
	case s == "r":
		p.togglePrettyRaw()
	case s == "f":
		if p.tab == tabTests {
			p.testsFilterFailed = !p.testsFilterFailed
		}
	case s == "u":
		p.revealSecrets = true
	}
	return nil
}

// KeyHints implements [tui.Pane].
func (p *Pane) KeyHints() []ui.Hint {
	if p.sendingID != "" {
		return []ui.Hint{{Key: "Esc", Label: "Cancel", Priority: 0}}
	}
	if p.transportErr() {
		return []ui.Hint{{Key: "Ctrl-S", Label: "Retry", Priority: 0}, {Key: "?", Label: "Help", Priority: 5}}
	}
	h := []ui.Hint{
		{Key: "[ ]", Label: "Tab", Priority: 3},
		{Key: "/", Label: "Search", Priority: 2},
		{Key: "y", Label: "Copy body", Priority: 4},
	}
	if p.tab == tabBody {
		h = append(h, ui.Hint{Key: "w", Label: "Wrap", Priority: 6})
	}
	return h
}

// View implements [tui.Pane].
func (p *Pane) View() string {
	view := *p
	view.rawIdx.offsets = append([]int(nil), p.rawIdx.offsets...)
	return view.render()
}

func (p *Pane) render() string {
	t := p.th
	w := max(10, p.w)
	if p.ref.Request == nil {
		return t.EmptyState("No response yet", "Select a request in Collections.", "Send with Ctrl-Enter when the request editor is ready.", w)
	}
	if p.sendingID != "" && p.ref.Request != nil && p.ref.Request.ID == p.sendingID {
		return p.viewSending(w)
	}
	st := p.current()
	if st == nil {
		return t.EmptyState("No response yet", "Responses appear here after a request is sent.", "Press Ctrl-Enter to send.", w)
	}
	if st.result.Skipped {
		return t.EmptyState("Request skipped", "The pre-request script called pm.execution.skipRequest().", "", w)
	}
	if st.result.Err != nil && st.result.Response == nil {
		return p.viewTransportErr(t, st.result, w)
	}
	var b strings.Builder
	b.WriteString(p.summaryLine(st.result, w))
	b.WriteByte('\n')
	b.WriteString(p.tabsBar(w))
	b.WriteByte('\n')
	b.WriteString(p.tabContent(st.result, w))
	if st.result.ScriptError != "" {
		b.WriteByte('\n')
		text := maskResultText(p, st.result, st.result.ScriptError)
		b.WriteString(t.Render(ui.StatusDanger, t.Glyphs.Warn+" Script: "+t.Truncate(text, w)))
	}
	return b.String()
}

func (p *Pane) viewSending(w int) string {
	t := p.th
	elapsed := time.Since(p.sendStarted)
	if p.sendStarted.IsZero() {
		elapsed = 0
	}
	line := t.Busy(p.spin.View(), p.sendStep)
	if elapsed > 0 {
		line += "  " + t.Render(ui.TextSecondary, ui.FormatDuration(elapsed))
	}
	line += "\n" + t.Render(ui.TextSecondary, "Esc Cancel")
	return t.EmptyState("Sending", line, "", w)
}

func (p *Pane) viewTransportErr(t ui.Theme, r tui.SendResult, w int) string {
	p.lastTransportRequest = r.RequestID
	msg := maskResultText(p, r, r.Err.Error())
	return t.ErrorState(msg,
		"Your request is unchanged.",
		"Send again with Ctrl-Enter, or press Ctrl-S to retry from here.", w)
}

func (p *Pane) transportErr() bool {
	st := p.current()
	return st != nil && st.result.Err != nil && st.result.Response == nil
}

func (p *Pane) summaryLine(r tui.SendResult, w int) string {
	t := p.th
	var chip string
	if r.Response != nil {
		size := r.Response.Size.Body + r.Response.Size.Headers
		chip = t.ResponseChip(r.Response.StatusCode, r.Response.Status, r.Response.Timings.Total, size)
	} else if r.Err != nil {
		chip = t.ResponseChip(0, shortErr(r.Err), 0, 0)
	} else {
		chip = t.ResponseChip(0, "No response", 0, 0)
	}
	passed, total := testCounts(r.Tests)
	if total > 0 {
		tests := fmt.Sprintf("Tests %d/%d", passed, total)
		tok := ui.StatusSuccess
		if passed < total {
			tok = ui.StatusDanger
		}
		chip += "  " + t.Render(tok, tests)
	}
	return t.Truncate(chip, w)
}

func testCounts(tests []tui.TestOutcome) (passed, total int) {
	for _, tc := range tests {
		if tc.Skipped {
			continue
		}
		total++
		if tc.Passed {
			passed++
		}
	}
	return passed, total
}

func (p *Pane) tabsBar(w int) string {
	t := p.th
	tabs := p.buildTabs()
	bar := t.Tabs(w, tabs, int(p.tab), p.focused)
	if ui.Width(bar) > 0 && strings.Count(bar, "\n") >= 1 {
		first := strings.SplitN(bar, "\n", 2)[0]
		if ui.Width(first)+10 < w {
			return bar
		}
	}
	// Collapsed selector at narrow widths.
	label := tabs[int(p.tab)].Label
	return t.Render(ui.TextSecondary, fmt.Sprintf("[%d/%d] %s  ] next", int(p.tab)+1, tabCount, label))
}

func (p *Pane) buildTabs() []ui.Tab {
	st := p.current()
	var tabs []ui.Tab
	tabs = append(tabs, ui.Tab{Label: "Body"})
	if st != nil && st.result.Response != nil {
		n := len(st.result.Response.Headers)
		tabs = append(tabs, ui.Tab{Label: "Headers", Count: n})
		tabs = append(tabs, ui.Tab{Label: "Cookies", Count: len(st.result.Response.Cookies)})
	} else {
		tabs = append(tabs, ui.Tab{Label: "Headers"}, ui.Tab{Label: "Cookies"})
	}
	_, total := 0, 0
	if st != nil {
		_, total = testCounts(st.result.Tests)
	}
	if total > 0 {
		tabs = append(tabs, ui.Tab{Label: "Tests", Count: total})
	} else {
		tabs = append(tabs, ui.Tab{Label: "Tests"})
	}
	warn, err, n := consoleStats(st)
	tb := ui.Tab{Label: "Console"}
	if n > 0 {
		tb.Count = n
		if warn+err > 0 {
			tb.Dot = true
		}
	}
	tabs = append(tabs, tb)
	tabs = append(tabs, ui.Tab{Label: "Timeline"})
	return tabs
}

func consoleStats(st *storedResult) (warn, err, n int) {
	if st == nil {
		return 0, 0, 0
	}
	for _, line := range st.result.Console {
		n++
		switch strings.ToLower(line.Level) {
		case "warn", "warning":
			warn++
		case "error":
			err++
		}
	}
	return warn, err, n
}

func (p *Pane) tabContent(r tui.SendResult, w int) string {
	h := p.contentHeight()
	switch p.tab {
	case tabBody:
		return p.viewBody(r, w, h)
	case tabHeaders:
		return p.viewHeaders(r, w, h)
	case tabCookies:
		return p.viewCookies(r, w, h)
	case tabTests:
		return p.viewTests(r, w, h)
	case tabConsole:
		return p.viewConsole(r, w, h)
	default:
		if r.Response != nil {
			return RenderTimeline(p.th, r.Response, w, p.revealSecrets, r.Redactor)
		}
		return p.th.EmptyState("No timeline", "Timing appears after a completed request.", "Send with Ctrl-S.", w)
	}
}

func (p *Pane) contentHeight() int {
	h := p.h - 3 // summary + tabs
	if p.searchActive || p.pathActive {
		h--
	}
	return max(1, h)
}

func (p *Pane) viewBody(r tui.SendResult, w, h int) string {
	var lines []string
	for _, warning := range r.Warnings {
		text := "! " + warning
		lines = append(lines, p.th.Render(ui.StatusWarning, p.th.Truncate(text, w)))
	}
	if len(lines) >= h {
		return strings.Join(lines[:h], "\n")
	}
	body := p.viewBodyContent(r, w, h-len(lines))
	if body != "" {
		lines = append(lines, body)
	}
	return strings.Join(lines, "\n")
}

func (p *Pane) viewBodyContent(r tui.SendResult, w, h int) string {
	t := p.th
	if r.Response == nil {
		return t.EmptyState("No body", "There is no response body for this request.", "", w)
	}
	body := r.Response.Body
	ct := ""
	for _, kv := range r.Response.Headers {
		if strings.EqualFold(kv.Key, "content-type") {
			ct = kv.Value
			break
		}
	}
	kind := DetectBodyKind(ct, body)
	p.bodyKind = kind
	if kind == BodyBinary && p.bodyMode != modeHex {
		p.bodyMode = modeHex
	}
	if len(body) >= largeBodyThreshold && p.bodyMode == modePretty && kind == BodyJSON {
		note := fmt.Sprintf("Body is %s — pretty-print uses lazy lines; scroll with ↑/↓.", ui.FormatSize(int64(len(body))))
		lines := p.renderBodyLines(body, kind, w, h-1)
		return note + "\n" + lines
	}
	if r.Response.BodyTruncated {
		note := t.Render(ui.StatusWarning, t.Glyphs.Warn+" Body truncated at "+ui.FormatSize(int64(len(body)))+".")
		return note + "\n" + p.renderBodyLines(body, kind, w, h-1)
	}
	out := p.renderBodyLines(body, kind, w, h)
	if p.searchActive {
		out = t.Render(ui.AccentPrimary, "Search: "+p.search.View()) + "\n" + out
	}
	if p.pathActive {
		out = t.Render(ui.AccentPrimary, "Path: "+p.pathInput.View()) + "\n" + out
	}
	return out
}

func (p *Pane) renderBodyLines(body []byte, kind BodyKind, w, h int) string {
	t := p.th
	switch p.bodyMode {
	case modeHex:
		return p.renderHex(body, w, h)
	case modeRaw:
		return p.renderRawVirtual(body, w, h)
	default:
		if kind == BodyBinary {
			return t.Render(ui.TextSecondary, fmt.Sprintf("Binary, %s", ui.FormatSize(int64(len(body))))) + "\n" + p.renderHex(body, w, h)
		}
		if kind == BodyJSON && len(body) < largeBodyThreshold {
			return p.renderJSON(body, w, h)
		}
		lang := highlightLang(kind)
		p.rawIdx.build(body)
		total := p.rawIdx.lineCount()
		p.clampScrollTotal(total)
		var lines []string
		for i := 0; i < h && p.scroll+i < total; i++ {
			line := p.rawIdx.lineAt(body, p.scroll+i)
			if lang != "" {
				line = highlightLine(t, lang, line)
			} else {
				line = t.Render(ui.TextPrimary, line)
			}
			if p.wrap {
				line = ui.Wrap(stripANSI(line), w)
			}
			lines = append(lines, fmt.Sprintf("%4d %s", p.scroll+i+1, line))
		}
		return strings.Join(lines, "\n")
	}
}

func (p *Pane) renderJSON(body []byte, w, h int) string {
	t := p.th
	key := p.cacheKey()
	if p.jsonDoc == nil || p.bodyKey != key {
		doc, err := buildJSONDoc(body, p.pathFilter)
		if err != nil {
			return t.ErrorState("Could not format JSON", err.Error(), "Switch to Raw with r.", w)
		}
		p.jsonDoc = doc
		p.bodyKey = key
	}
	total := p.jsonDoc.visibleLineCount()
	p.clampScrollTotal(total)
	var lines []string
	for i := 0; i < h && p.scroll+i < total; i++ {
		_, text, _ := p.jsonDoc.visibleLine(p.scroll + i)
		line := highlightLine(t, "json", text)
		if p.searchQuery != "" && strings.Contains(strings.ToLower(text), strings.ToLower(p.searchQuery)) {
			line = t.Render(ui.AccentPrimary, text)
		}
		lines = append(lines, fmt.Sprintf("%4d %s", p.scroll+i+1, line))
	}
	return strings.Join(lines, "\n")
}

func (p *Pane) renderRawVirtual(body []byte, w, h int) string {
	t := p.th
	p.rawIdx.build(body)
	total := p.rawIdx.lineCount()
	p.clampScrollTotal(total)
	var lines []string
	for i := 0; i < h && p.scroll+i < total; i++ {
		line := p.rawIdx.lineAt(body, p.scroll+i)
		if p.wrap {
			line = ui.Wrap(line, w-5)
		}
		lines = append(lines, fmt.Sprintf("%4d %s", p.scroll+i+1, t.Render(ui.TextPrimary, line)))
	}
	return strings.Join(lines, "\n")
}

func (p *Pane) renderHex(body []byte, w, h int) string {
	t := p.th
	const rowBytes = 16
	totalRows := (len(body) + rowBytes - 1) / rowBytes
	p.clampScrollTotal(totalRows)
	var lines []string
	for i := 0; i < h && p.scroll+i < totalRows; i++ {
		row := p.scroll + i
		start := row * rowBytes
		end := min(start+rowBytes, len(body))
		chunk := body[start:end]
		lines = append(lines, fmt.Sprintf("%08x  %s", start, t.Render(ui.TextPrimary, hex.EncodeToString(chunk))))
	}
	return strings.Join(lines, "\n")
}

func (p *Pane) viewHeaders(r tui.SendResult, w, h int) string {
	t := p.th
	if r.Response == nil {
		return t.EmptyState("No headers", "Response headers appear after a completed request.", "Send with Ctrl-S.", w)
	}
	headers := r.Response.Headers
	if !p.revealSecrets {
		headers = resultRedactor(r).Headers(headers)
	}
	var rows []ui.KVRow
	for _, kv := range headers {
		val := strings.ReplaceAll(kv.Value, history.Mask, t.Glyphs.Mask)
		rows = append(rows, ui.KVRow{Key: kv.Key, Value: val, Secret: SecretHeader(kv.Key)})
	}
	table := t.KVTable(rows, ui.KVTableOptions{Width: w, Focus: -1, Header: true, Reveal: p.revealSecrets})
	return clipLines(table, p.headerScroll, h)
}

func (p *Pane) viewCookies(r tui.SendResult, w, h int) string {
	t := p.th
	if r.Response == nil || len(r.Response.Cookies) == 0 {
		return t.EmptyState("No cookies", "Set-Cookie headers will appear here.", "", w)
	}
	var lines []string
	lines = append(lines, t.Render(ui.TextSecondary, t.Fit("Name", 16)+t.Fit("Value", 20)+"Domain"))
	for _, c := range r.Response.Cookies {
		val := c.Value
		if SessionCookie(nameFlags{Name: c.Name, Session: c.Session}) && !p.revealSecrets {
			val = t.Glyphs.Mask
		}
		flags := cookieFlags(c)
		line := t.Fit(c.Name, 16) + t.Fit(val, 20) + t.Truncate(c.Domain+" "+flags, w-40)
		lines = append(lines, line)
	}
	return clipLines(strings.Join(lines, "\n"), p.cookieScroll, h)
}

func cookieFlags(c model.Cookie) string {
	var f []string
	if c.Secure {
		f = append(f, "Secure")
	}
	if c.HTTPOnly {
		f = append(f, "HttpOnly")
	}
	if c.Session {
		f = append(f, "Session")
	}
	return strings.Join(f, " ")
}

func (p *Pane) viewTests(r tui.SendResult, w, h int) string {
	t := p.th
	if len(r.Tests) == 0 {
		return t.EmptyState("No tests", "Add a test script on the request.", "", w)
	}
	var lines []string
	for _, tc := range r.Tests {
		if p.testsFilterFailed && (tc.Passed || tc.Skipped) {
			continue
		}
		var mark string
		var tok ui.Token
		switch {
		case tc.Skipped:
			mark, tok = "-", ui.TextSecondary
		case tc.Passed:
			mark, tok = t.Glyphs.Check, ui.StatusSuccess
		default:
			mark, tok = t.Glyphs.Fail, ui.StatusDanger
		}
		name := maskResultText(p, r, tc.Name)
		line := t.Render(tok, mark) + " " + t.Render(ui.TextPrimary, name)
		if !tc.Passed && !tc.Skipped && tc.Message != "" {
			message := maskResultText(p, r, tc.Message)
			line += " — " + t.Render(ui.TextSecondary, t.Truncate(message, w-ui.Width(line)-3))
		}
		lines = append(lines, line)
	}
	return clipLines(strings.Join(lines, "\n"), p.testScroll, h)
}

func (p *Pane) viewConsole(r tui.SendResult, w, h int) string {
	t := p.th
	if len(r.Console) == 0 {
		return t.EmptyState("No console output", "Script console.log lines appear here.", "", w)
	}
	var lines []string
	lastEvent := ""
	for _, line := range r.Console {
		if line.Event != lastEvent {
			lastEvent = line.Event
			title := "Pre-request"
			if line.Event == "test" {
				title = "Tests"
			}
			lines = append(lines, t.Bold().Render(title))
		}
		tok := ui.TextPrimary
		switch strings.ToLower(line.Level) {
		case "warn", "warning":
			tok = ui.StatusWarning
		case "error":
			tok = ui.StatusDanger
		}
		text := maskResultText(p, r, line.Text)
		lines = append(lines, t.Render(tok, line.Level)+": "+t.Render(ui.TextPrimary, t.Truncate(text, w-8)))
	}
	return clipLines(strings.Join(lines, "\n"), p.consoleScroll, h)
}

func clipLines(s string, scroll, h int) string {
	lines := strings.Split(s, "\n")
	if scroll >= len(lines) {
		return ""
	}
	end := min(len(lines), scroll+h)
	return strings.Join(lines[scroll:end], "\n")
}

func (p *Pane) applyResult(r tui.SendResult) {
	p.byID[r.RequestID] = &storedResult{result: r}
	p.sendingID = ""
	p.jsonDoc = nil
	p.bodyKey = ""
	p.scroll = 0
	p.prepareCurrent()
}

func (p *Pane) current() *storedResult {
	if p.ref.Request == nil {
		return nil
	}
	return p.byID[p.ref.Request.ID]
}

func (p *Pane) syncViewFromSelection() {
	p.scroll = 0
	p.jsonDoc = nil
	p.prepareCurrent()
}

func (p *Pane) prepareCurrent() {
	st := p.current()
	if st == nil || st.result.Response == nil {
		return
	}
	body := st.result.Response.Body
	ct := ""
	for _, kv := range st.result.Response.Headers {
		if strings.EqualFold(kv.Key, "content-type") {
			ct = kv.Value
			break
		}
	}
	p.bodyKind = DetectBodyKind(ct, body)
	if p.bodyKind == BodyBinary && p.bodyMode != modeHex {
		p.bodyMode = modeHex
	}
	p.rawIdx.build(body)
	if p.bodyMode == modePretty && p.bodyKind == BodyJSON && len(body) < largeBodyThreshold {
		key := p.cacheKey()
		if p.jsonDoc == nil || p.bodyKey != key {
			if doc, err := buildJSONDoc(body, p.pathFilter); err == nil {
				p.jsonDoc, p.bodyKey = doc, key
			}
		}
	}
	p.clampScroll()
}

func (p *Pane) remapAfterReload(ws *model.Workspace) {
	if ws == nil {
		return
	}
	ids := map[model.ID]struct{}{}
	for _, c := range ws.Collections {
		for _, r := range c.Requests() {
			ids[r.ID] = struct{}{}
		}
	}
	for id := range p.byID {
		if _, ok := ids[id]; !ok {
			delete(p.byID, id)
		}
	}
}

func (p *Pane) cacheKey() string {
	if p.ref.Request == nil {
		return ""
	}
	return p.ref.Request.ID.String() + p.pathFilter + fmt.Sprint(p.bodyMode)
}

func (p *Pane) clampScroll() { p.clampScrollTotal(p.scrollTotal()) }

func (p *Pane) scrollTotal() int {
	if p.jsonDoc != nil && p.bodyMode == modePretty {
		return p.jsonDoc.visibleLineCount()
	}
	return p.rawIdx.lineCount()
}

func (p *Pane) clampScrollTotal(total int) {
	maxScroll := max(0, total-p.contentHeight())
	if p.scroll > maxScroll {
		p.scroll = maxScroll
	}
	if p.scroll < 0 {
		p.scroll = 0
	}
}

func (p *Pane) scrollUp() {
	p.scroll--
	p.clampScroll()
}

func (p *Pane) scrollDown() {
	p.scroll++
	p.clampScroll()
}

func (p *Pane) togglePrettyRaw() {
	if p.bodyMode == modePretty {
		p.bodyMode = modeRaw
	} else {
		p.bodyMode = modePretty
	}
	p.jsonDoc = nil
	p.scroll = 0
	p.prepareCurrent()
}

func (p *Pane) beginSearch() {
	p.searchActive = true
	p.search = textinput.New()
	p.search.SetValue(p.searchQuery)
	p.search.Focus()
}

func (p *Pane) beginPathFilter() {
	p.pathActive = true
	p.pathInput = textinput.New()
	p.pathInput.SetValue(p.pathFilter)
	p.pathInput.Placeholder = ".data.items[0].id"
	p.pathInput.Focus()
}

func (p *Pane) searchNext(dir int) {
	if p.searchQuery == "" {
		return
	}
	p.searchIdx += dir
	if p.searchIdx < 0 {
		p.searchIdx = 0
	}
}

func (p *Pane) toggleFoldCursor() {
	if p.jsonDoc == nil {
		return
	}
	src, _, ok := p.jsonDoc.visibleLine(p.scroll)
	if !ok {
		return
	}
	p.jsonDoc.toggleFold(src - 1)
}

func (p *Pane) foldCursor(collapse bool) {
	if p.jsonDoc == nil {
		return
	}
	src, _, ok := p.jsonDoc.visibleLine(p.scroll)
	if !ok {
		return
	}
	p.jsonDoc.foldAt(src-1, collapse)
}

func (p *Pane) cmdCopyBody() tea.Cmd {
	return func() tea.Msg {
		st := p.current()
		if st == nil || st.result.Response == nil {
			return tui.ToastMsg{Kind: ui.ToastWarning, Text: "Nothing to copy"}
		}
		_ = clipboard.WriteAll(string(st.result.Response.Body))
		return tui.ToastMsg{Kind: ui.ToastSuccess, Text: "Response body copied"}
	}
}

func (p *Pane) cmdSaveBodyPrompt() tea.Cmd {
	return p.cmdCopyBody() // palette stub; full path prompt lands with shell overlays
}

func (p *Pane) cmdOpenPager() tea.Cmd {
	st := p.current()
	if st == nil || st.result.Response == nil {
		return nil
	}
	body := pagerBody(st.result, p.revealSecrets)
	pager := os.Getenv("PAGER")
	if pager == "" {
		pager = "less"
	}
	cmd, err := pagerCommand(pager, body)
	if err != nil {
		return func() tea.Msg { return tui.ToastMsg{Kind: ui.ToastError, Text: err.Error()} }
	}
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		if err != nil {
			return tui.ToastMsg{Kind: ui.ToastError, Text: err.Error()}
		}
		return tui.ToastMsg{Kind: ui.ToastInfo, Text: "Pager closed"}
	})
}

func pagerBody(r tui.SendResult, reveal bool) []byte {
	if r.Response == nil {
		return nil
	}
	body := append([]byte(nil), r.Response.Body...)
	if reveal {
		return body
	}
	return MaskBodyForExternalOutput(r)
}

// MaskBodyForExternalOutput returns a response body with credential-looking
// fields and every known secret value masked. It is used before bytes leave
// the live in-memory response viewer, such as through the pager.
func MaskBodyForExternalOutput(r tui.SendResult) []byte {
	if r.Response == nil {
		return nil
	}
	return []byte(resultRedactor(r).Body(
		history.ContentType(r.Response.Headers), string(r.Response.Body)))
}

func resultRedactor(r tui.SendResult) *history.Redactor {
	if r.Redactor != nil {
		return r.Redactor
	}
	return history.NewRedactor(nil)
}

func maskResultText(p *Pane, r tui.SendResult, text string) string {
	if p.revealSecrets {
		return text
	}
	return strings.ReplaceAll(resultRedactor(r).Text(text), history.Mask, p.th.Glyphs.Mask)
}

func pagerCommand(spec string, body []byte) (*exec.Cmd, error) {
	words := strings.Fields(spec)
	if len(words) == 0 {
		return nil, fmt.Errorf("PAGER is empty; set it to less, more, bat, or cat")
	}
	name := strings.ToLower(filepath.Base(words[0]))
	switch name {
	case "less", "more", "most", "bat", "batcat", "cat", "lv", "pg":
		// Known pagers only: stdin contains response content and must never be
		// handed to a shell or general-purpose interpreter.
	default:
		return nil, fmt.Errorf("PAGER %q is not supported; use less, more, bat, or cat", name)
	}
	cmd := exec.Command(words[0], words[1:]...)
	cmd.Stdin = bytes.NewReader(body)
	return cmd, nil
}

func shortErr(err error) string {
	if err == nil {
		return ""
	}
	s := err.Error()
	if i := strings.Index(s, "\n"); i >= 0 {
		s = s[:i]
	}
	return s
}

func stripANSI(s string) string {
	var b strings.Builder
	in := false
	for _, r := range s {
		if r == '\x1b' {
			in = true
			continue
		}
		if in {
			if r == 'm' {
				in = false
			}
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func demoEnabled() bool { return os.Getenv("POSTCTL_DEMO_RESPONSE") == "1" }

func demoResult(id model.ID) tui.SendResult {
	body := []byte(`{"data":{"items":[{"id":"ch_1","amount":1200}],"total":1},"ok":true}`)
	return tui.SendResult{
		RequestID: id,
		Response: &httpx.Response{
			StatusCode: 200,
			Status:     "OK",
			Headers:    []model.KV{{Key: "Content-Type", Value: "application/json"}},
			Body:       body,
			Size:       httpx.Size{Headers: 120, Body: int64(len(body))},
			Timings:    httpx.Timings{DNS: 2e6, Connect: 5e6, TTFB: 50e6, Download: 10e6, Total: 67e6},
			RemoteAddr: "127.0.0.1:443",
			Sent: httpx.SentRequest{
				Method: "GET",
				URL:    "https://api.example.com/v1/items",
				Headers: []model.KV{
					{Key: "Authorization", Value: "Bearer secret"},
					{Key: "Accept", Value: "application/json"},
				},
			},
		},
		Tests: []tui.TestOutcome{
			{Name: "status is 200", Passed: true},
			{Name: "has items", Passed: true},
			{Name: "amount positive", Passed: false, Message: "expected amount > 0"},
		},
		Console: []tui.ConsoleLine{
			{Level: "log", Text: "fetching items", Event: "prerequest"},
			{Level: "warn", Text: "slow mock", Event: "test"},
		},
	}
}

// Ensure Pane implements optional interfaces at compile time.
var (
	_ tui.Titled       = (*Pane)(nil)
	_ tui.ActionSource = (*Pane)(nil)
	_ tui.Editor       = (*Pane)(nil)
)
