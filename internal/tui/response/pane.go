package response

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
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

// New builds the Response pane for [tui.Options.NewResponsePane], without a
// history log (no History tab, no diff). [Factory] builds one with Deps.
func New(env tui.PaneEnv) tui.Pane { return NewWithDeps(env, Deps{}) }

// NewWithDeps builds the Response pane with the effects in d.
func NewWithDeps(env tui.PaneEnv, d Deps) tui.Pane {
	p := &Pane{
		deps:     d,
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
	// tabHistory lists past sends of the request; it exists only when the
	// pane has a history log.
	tabHistory
)

// baseTabs are the tabs every response pane has, in order.
var baseTabs = []tab{tabBody, tabHeaders, tabCookies, tabTests, tabConsole, tabTimeline}

// tabList returns the tabs this pane offers, in display order.
func (p *Pane) tabList() []tab {
	if p.historyEnabled() {
		return append(append([]tab(nil), baseTabs...), tabHistory)
	}
	return baseTabs
}

// tabIndex is the position of the active tab in [Pane.tabList].
func (p *Pane) tabIndex() int {
	for i, tb := range p.tabList() {
		if tb == p.tab {
			return i
		}
	}
	return 0
}

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
	deps                 Deps
	hist                 histState
	histGen              int
	diff                 diffState
	diffGen              int
	jq                   jqState
	vim                  *bodyVim // read-only buffer of the Body tab; see vimbody.go
	vimWant              vimKey   // buffer being built in a command, if any
	vimGen               int
	themeGen             int
	lastYankMasked       bool
	foldPending          bool // z was pressed in the Body buffer; its second key folds
	fold                 foldState
	vimLatest            *atomic.Int64 // generation of the newest wanted build; older builds stop early
	yanked               bool          // the buffer wrote to the clipboard during this key
	th                   ui.Theme
	w, h                 int
	focused              bool
	ref                  tui.RequestRef
	byID                 map[model.ID]*storedResult
	sendingID            model.ID
	sendStep             string
	sendStarted          time.Time
	sendElapsed          time.Duration // stamped by Update; View never reads the clock
	tab                  tab
	bodyMode             bodyMode
	scroll               int
	wrap                 bool
	revealSecrets        bool
	pathFilter           string
	pathInput            textinput.Model
	pathActive           bool
	rawIdx               lineIndex
	bodyKind             BodyKind
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
	p.ensureVimInline()
	p.fitVim()
	p.sizeDiff()
	if p.jq.input != nil {
		p.jq.input.SetSize(max(1, w-4), 1)
	}
	p.clampHist()
}

// Focus implements [tui.Pane].
func (p *Pane) Focus() tea.Cmd {
	p.focused = true
	p.syncVimFocus()
	if p.sendingID != "" {
		return p.spin.Tick
	}
	return nil
}

// Blur implements [tui.Pane].
func (p *Pane) Blur() {
	p.focused = false
	p.revealSecrets = false
	p.syncVimFocus()
}

// VimMode implements [tui.ModeReporter]. The jq input and the Body tab's
// buffer are vim editors and report their mode; the path box is not modal yet,
// so editing it reports INSERT.
func (p *Pane) VimMode() string {
	if p.jq.open && p.jq.input != nil {
		return p.jq.input.Mode().String()
	}
	if p.inputOpen() {
		return "INSERT"
	}
	if p.vimLive() {
		return p.vim.f.Mode().String()
	}
	return ""
}

// Sections implements [tui.SectionLister]: g jumps can reach the Body tab; [
// and ] step through every tab.
func (p *Pane) Sections() []string {
	if p.historyEnabled() {
		return []string{tui.SectionRespBody, tui.SectionRespHistory}
	}
	return []string{tui.SectionRespBody}
}

// Editing implements [tui.Editor]: a text box is open, or the Body tab is a
// live vim buffer (which hands the shell the keys it does not own).
func (p *Pane) Editing() bool { return p.inputOpen() || p.vimLive() }

// inputOpen reports whether the path or jq text box is open.
func (p *Pane) inputOpen() bool { return p.pathActive || p.jq.open }

func (p *Pane) registerActions(reg *tui.Registry) {
	reg.Add(
		tui.Action{ID: "response.copyBody", Title: "Copy response body", Group: "Response",
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

func (p *Pane) hasRequest() bool { return p.ref.Request != nil }

// Actions implements [tui.ActionSource].
func (p *Pane) Actions() []tui.Action {
	acts := p.baseActions()
	acts = append(acts, tui.Action{ID: "response.jq", Title: "jq filter on the response", Group: "Response", Keys: "Space j",
		Enabled: p.hasRequest, Run: p.cmdOpenJQ})
	if p.historyEnabled() {
		acts = append(acts,
			tui.Action{ID: "history.request", Title: "History of this request", Group: "Response", Keys: "Space H",
				Enabled: p.hasRequest, Run: p.cmdShowHistory},
			tui.Action{ID: "response.diff", Title: "Diff response with previous", Group: "Response", Keys: "Space x",
				Enabled: p.hasRequest, Run: p.cmdDiffPrevious},
		)
	}
	// An action can change what the Body tab shows; the buffer must give up the
	// keyboard right away, not at the next message.
	for i := range acts {
		run := acts[i].Run
		acts[i].Run = func() tea.Cmd {
			cmd := run()
			p.syncVimFocus()
			return cmd
		}
	}
	return acts
}

// cmdShowHistory is Space H: show the History tab of the open request.
func (p *Pane) cmdShowHistory() tea.Cmd {
	p.closeDiff()
	p.tab = tabHistory
	p.revealSecrets = false
	return tea.Batch(focusResponse(), p.ensureHistory())
}

func (p *Pane) baseActions() []tui.Action {
	return []tui.Action{
		{ID: "response.copyBody", Title: "Copy response body", Group: "Response",
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
	_, cmd := p.update(msg)
	if p.tab == tabHistory {
		p.hist.now = p.deps.now()
	}
	if p.sendingID != "" && !p.sendStarted.IsZero() {
		p.sendElapsed = time.Since(p.sendStarted)
	}
	cmd = tea.Batch(cmd, p.ensureVim())
	p.syncVimFocus()
	return p, cmd
}

func (p *Pane) update(msg tea.Msg) (tui.Pane, tea.Cmd) {
	var cmds []tea.Cmd
	switch msg := msg.(type) {
	case tui.SelectionMsg:
		prev := p.requestID()
		p.ref = msg.Ref
		p.revealSecrets = false
		p.syncViewFromSelection()
		if p.requestID() != prev {
			cmds = append(cmds, p.resetForRequest())
		}
		if demoEnabled() && msg.Ref.Request != nil && !p.demoInjected {
			p.demoInjected = true
			p.applyResult(demoResult(msg.Ref.Request.ID))
		}
	case tui.EnvironmentChangedMsg:
	case tui.WorkspaceReloadedMsg:
		p.remapAfterReload(msg.Workspace)
	case vimBuiltMsg:
		p.onVimBuilt(msg)
	case tui.ThemeChangedMsg:
		p.th = msg.Theme
		p.themeGen++
		p.spin = p.th.NewSpinner()
		if p.jq.open && p.jq.input != nil {
			p.newJQInput(p.jq.input.Value())
		}
	case tui.FocusSectionMsg:
		switch {
		case msg.Section == tui.SectionRespBody:
			p.closeDiff()
			p.tab = tabBody
			p.revealSecrets = false
		case msg.Section == tui.SectionRespHistory && p.historyEnabled():
			p.closeDiff()
			p.tab = tabHistory
			p.revealSecrets = false
			cmds = append(cmds, p.ensureHistory())
		}
	case tui.SectionStepMsg:
		if !p.inputOpen() && msg.Delta != 0 {
			list := p.tabList()
			p.tab = list[(p.tabIndex()+msg.Delta+len(list))%len(list)]
			p.revealSecrets = false // reveal lasts for one tab visit
			p.closeDiff()
			if p.tab == tabHistory {
				cmds = append(cmds, p.ensureHistory())
			}
		}
	case copyReadyMsg:
		cmds = append(cmds, p.writeClipboard(msg.text), toast(ui.ToastSuccess, msg.label))
	case histLoadedMsg:
		p.onHistoryLoaded(msg)
	case diffReadyMsg:
		cmds = append(cmds, p.onDiffReady(msg))
	case jqDoneMsg:
		p.onJQDone(msg)
	case tui.SendStartedMsg:
		p.sendingID = msg.RequestID
		p.sendStep = "Starting"
		p.sendStarted = time.Now()
		p.sendElapsed = 0
		if p.ref.Request != nil && p.ref.Request.ID == msg.RequestID {
			cmds = append(cmds, p.spin.Tick, tickElapsed())
		}
	case tui.SendProgressMsg:
		if msg.RequestID == p.sendingID {
			p.sendStep = msg.Step.Text
		}
	case tui.SendResultMsg:
		p.revealSecrets = false
		p.applyResult(msg.Result)
		p.sendingID = ""
		if msg.Result.RequestID == p.requestID() {
			p.closeDiff()
			p.jq.dirty = true
			cmds = append(cmds, p.refreshHistory())
		}
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
		if p.jq.open && p.jq.input != nil {
			switch msg.(type) {
			case tea.KeyPressMsg, tea.PasteMsg:
				cmds = append(cmds, p.jqUpdate(msg))
				cmds = append(cmds, p.jqSync())
				return p, tea.Batch(cmds...)
			}
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
	// The jq output must always match the response and reveal state on screen.
	cmds = append(cmds, p.jqSync())
	return p, tea.Batch(cmds...)
}

// resetForRequest drops per-request state when another request is selected.
func (p *Pane) resetForRequest() tea.Cmd {
	p.histGen++
	p.hist = histState{}
	p.closeDiff()
	p.jqClear()
	if p.tab == tabHistory {
		return p.ensureHistory()
	}
	return nil
}

type elapsedTickMsg struct{}

func tickElapsed() tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return elapsedTickMsg{} })
}

func (p *Pane) onKey(msg tea.KeyPressMsg) tea.Cmd {
	s := msg.String()
	if p.diff.open {
		return p.diffKey(s, msg)
	}
	if p.tab == tabHistory && p.sendingID == "" {
		if cmd, ok := p.historyKey(s); ok {
			return cmd
		}
	}
	if p.vimLive() {
		return p.vimKeyPress(msg)
	}
	switch s {
	case "esc":
		if p.sendingID != "" && p.ref.Request != nil {
			return func() tea.Msg { return tui.CancelSendMsg{RequestID: p.ref.Request.ID} }
		}
		if p.tab == tabBody && p.jq.filter != "" {
			p.jqClear() // Esc backs out of the jq filter
		}
		return nil
	case "ctrl+s":
		if p.transportErr() && p.ref.Request != nil {
			return func() tea.Msg { return tui.RetrySendMsg{RequestID: p.ref.Request.ID} }
		}
		return nil
	case "y":
		return p.cmdCopyBody()
	case "w":
		p.wrap = !p.wrap
	case ".":
		p.beginPathFilter()
	case "up", "k":
		p.scrollUp()
	case "down", "j":
		p.scrollDown()
	case "pgup", "ctrl+u":
		p.scroll -= max(1, p.contentHeight()/2)
		p.clampScroll()
	case "pgdown", "ctrl+d":
		p.scroll += max(1, p.contentHeight()/2)
		p.clampScroll()
	case "home":
		p.scroll = 0
	case "end", "G":
		p.scroll = p.scrollTotal()
		p.clampScroll()
	case "r":
		p.togglePrettyRaw()
	case "f":
		if p.tab == tabTests {
			p.testsFilterFailed = !p.testsFilterFailed
		}
	case "u":
		p.revealSecrets = true
	}
	return nil
}

// KeyHints implements [tui.Pane].
func (p *Pane) KeyHints() []ui.Hint {
	if p.sendingID != "" {
		return []ui.Hint{{Key: "Esc", Label: "Cancel", Priority: 0}}
	}
	if p.jq.open {
		return []ui.Hint{{Key: "Enter", Label: "Apply", Priority: 0}, {Key: "Esc", Label: "Normal, then clear", Priority: 1}}
	}
	if p.diff.open {
		return []ui.Hint{
			{Key: "n/N", Label: "Hunk", Priority: 2},
			{Key: "h", Label: "Headers/body", Priority: 2},
			{Key: "j/k", Label: "Scroll", Priority: 3},
			{Key: "Esc", Label: "Back", Priority: 0},
		}
	}
	if p.tab == tabHistory {
		return []ui.Hint{
			{Key: "j/k", Label: "Move", Priority: 2},
			{Key: "Enter", Label: "Open", Priority: 0},
			{Key: "d", Label: "Diff with current", Priority: 1},
			{Key: "[ ]", Label: "Tab", Priority: 3},
		}
	}
	if p.transportErr() {
		return []ui.Hint{{Key: "Ctrl-S", Label: "Retry", Priority: 0}, {Key: "?", Label: "Help", Priority: 5}}
	}
	if p.vimLive() {
		return p.vimHints()
	}
	h := []ui.Hint{
		{Key: "[ ]", Label: "Tab", Priority: 3},
		{Key: "y", Label: "Copy body", Priority: 4},
	}
	if p.tab == tabBody {
		note := p.limitedNote()
		if note != binaryBodyNote && note != jqOutputNote { // the hex dump and the jq view do not wrap
			h = append(h, ui.Hint{Key: "w", Label: "Wrap", Priority: 6})
		}
		if p.jq.filter != "" {
			h = append(h, ui.Hint{Key: "Esc", Label: "Clear jq", Priority: 1})
		}
		if note != "" {
			h = append(h, ui.Hint{Key: note, Priority: 0})
		}
	}
	return h
}

// limitedNote says why the Body tab is not the vim buffer, when it has a body
// and is not: the footer shows it so the missing keys are not a surprise.
func (p *Pane) limitedNote() string {
	st := p.current()
	if st == nil || st.result.Response == nil || len(st.result.Response.Body) == 0 {
		return ""
	}
	switch {
	case p.jq.filter != "":
		return jqOutputNote
	case p.bodyMode == modeHex || p.bodyKind == BodyBinary:
		return binaryBodyNote
	case p.largeBody():
		return largeBodyNote
	}
	return ""
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
	if p.diff.open {
		return p.viewDiff(w)
	}
	st := p.current()
	onHistory := p.tab == tabHistory && p.historyEnabled()
	if !onHistory {
		if st == nil {
			next := "Press Ctrl-Enter to send."
			if p.historyEnabled() {
				next = "Press Ctrl-Enter to send, or Space H to see past sends."
			}
			return t.EmptyState("No response yet", "Responses appear here after a request is sent.", next, w)
		}
		if st.result.Skipped {
			return t.EmptyState("Request skipped", "The pre-request script called pm.execution.skipRequest().", "", w)
		}
		if st.result.Err != nil && st.result.Response == nil {
			return p.viewTransportErr(t, st.result, w)
		}
	}
	var b strings.Builder
	switch {
	case st == nil:
		b.WriteString(t.Truncate(t.Render(ui.TextSecondary, "No response yet in this session. Ctrl-S sends this request."), w))
	case st.result.Skipped:
		b.WriteString(t.Truncate(t.Render(ui.TextSecondary, "Request skipped by its pre-request script."), w))
	default:
		b.WriteString(p.summaryLine(st.result, w))
	}
	b.WriteByte('\n')
	b.WriteString(p.tabsBar(w))
	b.WriteByte('\n')
	switch {
	case onHistory:
		b.WriteString(p.viewHistory(w, p.contentHeight()))
	case st != nil:
		b.WriteString(p.tabContent(st.result, w))
	}
	if st != nil && st.result.ScriptError != "" {
		b.WriteByte('\n')
		text := maskResultText(p, st.result, st.result.ScriptError)
		b.WriteString(t.Render(ui.StatusDanger, t.Glyphs.Warn+" Script: "+t.Truncate(text, w)))
	}
	return b.String()
}

func (p *Pane) viewSending(w int) string {
	t := p.th
	elapsed := p.sendElapsed
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
	bar := t.Tabs(w, tabs, p.tabIndex(), p.focused)
	if ui.Width(bar) > 0 && strings.Count(bar, "\n") >= 1 {
		first := strings.SplitN(bar, "\n", 2)[0]
		if ui.Width(first)+10 < w {
			return bar
		}
	}
	// Collapsed selector at narrow widths.
	label := tabs[p.tabIndex()].Label
	return t.Render(ui.TextSecondary, fmt.Sprintf("[%d/%d] %s  ] next", p.tabIndex()+1, len(tabs), label))
}

func (p *Pane) buildTabs() []ui.Tab {
	st := p.current()
	var tabs []ui.Tab
	body := ui.Tab{Label: "Body"}
	if p.jq.filter != "" {
		body.Label = "Body (jq)"
	}
	tabs = append(tabs, body)
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
	if p.historyEnabled() {
		hist := ui.Tab{Label: "History"}
		if p.hist.loaded && !p.hist.err {
			hist.Count = len(p.hist.items)
		}
		tabs = append(tabs, hist)
	}
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
		if p.jqBarVisible() {
			return p.viewJQ(r, w, h)
		}
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
	if p.pathActive {
		h--
	}
	if p.jqBarVisible() {
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
	if v := p.readyVim(); v != nil && p.current() != nil && p.current().result.Response == r.Response {
		out := v.f.Render(w, p.vimRows(r))
		if r.Response.BodyTruncated {
			note := t.Render(ui.StatusWarning, t.Glyphs.Warn+" Body truncated at "+ui.FormatSize(int64(len(body)))+".")
			out = note + "\n" + out
		}
		if p.pathActive {
			out = t.Render(ui.AccentPrimary, "Path: "+p.pathInput.View()) + "\n" + out
		}
		return out
	}
	if p.vimPending() && p.bodyMode == modePretty && kind == BodyJSON {
		return t.EmptyState("Formatting…", "Preparing the response body for viewing.", "", w)
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
	p.scroll = 0
	p.prepareCurrent()
	p.ensureVimInline()
}

func (p *Pane) current() *storedResult {
	if p.ref.Request == nil {
		return nil
	}
	return p.byID[p.ref.Request.ID]
}

func (p *Pane) syncViewFromSelection() {
	p.scroll = 0
	p.prepareCurrent()
	p.ensureVimInline()
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
	p.rawIdx.reset()
	if !p.vimEligible(st) || len(body) <= vimInlineMax {
		// A body the editor will show (the big ones are built in a command)
		// needs no line index; the plain viewer indexes it itself if it must
		// show it meanwhile.
		p.rawIdx.build(body)
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

func (p *Pane) clampScroll() { p.clampScrollTotal(p.scrollTotal()) }

func (p *Pane) scrollTotal() int {
	if p.jqShowing() {
		return len(p.jq.lines)
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
	p.scroll = 0
	p.prepareCurrent()
	p.ensureVimInline()
}

func (p *Pane) beginPathFilter() {
	p.pathActive = true
	p.pathInput = textinput.New()
	p.pathInput.SetValue(p.pathFilter)
	p.pathInput.Placeholder = ".data.items[0].id"
	p.pathInput.Focus()
}

// copyReadyMsg carries text a command prepared for the clipboard.
type copyReadyMsg struct {
	text  string
	label string
}

// cmdCopyBody copies what the Body tab shows: the jq output while a filter is
// applied, otherwise the body. Both are masked unless the pane is revealed;
// masking runs in the command, off the Update goroutine.
func (p *Pane) cmdCopyBody() tea.Cmd {
	st := p.current()
	if st == nil || st.result.Response == nil {
		return toast(ui.ToastWarning, "Nothing to copy")
	}
	if p.jqShowing() {
		text := p.jqText()
		return func() tea.Msg { return copyReadyMsg{text: text, label: "jq output copied"} }
	}
	res, reveal := st.result, p.revealSecrets
	return func() tea.Msg {
		return copyReadyMsg{text: string(pagerBody(res, reveal)), label: "Response body copied"}
	}
}

// writeClipboard puts text on the clipboard.
func (p *Pane) writeClipboard(text string) tea.Cmd {
	if p.deps.Clipboard != nil {
		return p.deps.Clipboard(text)
	}
	return func() tea.Msg {
		_ = clipboard.WriteAll(text)
		return nil
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
