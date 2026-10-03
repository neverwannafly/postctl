// Package runner is the collection runner screen: configure a run, watch live
// progress, inspect failures, and export results.
package runner

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"github.com/shubham/postctl/internal/engine"
	"github.com/shubham/postctl/internal/history"
	"github.com/shubham/postctl/internal/model"
	"github.com/shubham/postctl/internal/report"
	"github.com/shubham/postctl/internal/tui"
	"github.com/shubham/postctl/internal/tui/listmotion"
	"github.com/shubham/postctl/internal/tui/response"
	"github.com/shubham/postctl/internal/tui/screenfield"
	"github.com/shubham/postctl/internal/ui"
)

// Deps are injectable runner effects.
type Deps struct {
	Runner tui.CollectionRunner
}

// Factory returns a screen factory using d.
func Factory(d Deps) tui.ScreenFactory {
	return func(env tui.ScreenEnv) tui.Screen { return New(env, d) }
}

type phase int

const (
	phSetup phase = iota
	phLive
	phSummary
	phDetail
	phConfirmCancel
	phExport
)

type reqState int

const (
	stPending reqState = iota
	stRunning
	stDone
)

type row struct {
	id                   model.ID
	name                 string
	method               string
	state                reqState
	step                 string
	status               int
	errText              string
	testsPass, testsFail int
	outcome              *engine.Outcome
}

// Model is the collection runner screen.
type Model struct {
	th   ui.Theme
	env  tui.ScreenEnv
	deps Deps
	w, h int

	phase phase
	spin  spinner.Model

	coll       *model.Collection
	folderID   model.ID
	scopeLabel string

	field      int
	setupNav   listmotion.View
	failCur    int
	envID      model.ID
	iterations *screenfield.Field
	dataPath   *screenfield.Field
	delayMS    *screenfield.Field
	bail       bool
	persist    bool

	rows         []row
	cur          int
	top          int
	failuresOnly bool

	iter, iterTotal int
	summary         *engine.RunSummary
	detail          *engine.Outcome

	runCancel  context.CancelFunc
	runCh      <-chan engine.RunEvent
	runEvents  []engine.RunEvent
	exportPath *screenfield.Field
	listNav    listmotion.View
	exportXML  bool
	conf       ui.Confirm
	notice     string
	fail       bool
	elapsed    time.Duration
	runStart   time.Time
}

// New builds the runner screen.
func New(env tui.ScreenEnv, d Deps) *Model {
	iters, _ := screenfield.New(env.Theme, "1", "", false)
	data, _ := screenfield.New(env.Theme, "", "path to CSV", false)
	delay, _ := screenfield.New(env.Theme, "0", "", false)
	export, _ := screenfield.New(env.Theme, "", "export path", false)
	m := &Model{
		th: env.Theme, env: env, deps: d,
		iterations: iters,
		dataPath:   data,
		delayMS:    delay,
		exportPath: export,
		bail:       false,
		persist:    true,
		spin:       env.Theme.NewSpinner(),
	}
	if env.RunScope != nil && env.RunScope.Collection != nil {
		m.coll = env.RunScope.Collection
		m.folderID = env.RunScope.FolderID
	}
	m.envID = env.ActiveEnv
	m.scopeLabel = m.buildScopeLabel()
	return m
}

func (m *Model) buildScopeLabel() string {
	if m.coll == nil {
		return "No collection"
	}
	if m.folderID == "" {
		return m.coll.Name + " (collection)"
	}
	if f := findFolder(m.coll, m.folderID); f != nil {
		return m.coll.Name + " › " + f.Name
	}
	return m.coll.Name
}

// SetSize implements tui.Screen.
func (m *Model) SetSize(w, h int) {
	m.w, m.h = w, h
	if m.iterations != nil {
		m.iterations.SetWidth(20)
	}
	if m.dataPath != nil {
		m.dataPath.SetWidth(max(4, w-20))
	}
	if m.delayMS != nil {
		m.delayMS.SetWidth(12)
	}
	if m.exportPath != nil {
		m.exportPath.SetWidth(max(4, w-2))
	}
}

// KeyHints implements tui.Screen.
func (m *Model) KeyHints() []ui.Hint {
	switch m.phase {
	case phConfirmCancel:
		return []ui.Hint{{Key: "Enter", Label: "Choose", Priority: 1}, {Key: "Esc", Label: "Keep running", Priority: 1}}
	case phSetup:
		return []ui.Hint{{Key: "Tab", Label: "Field", Priority: 2}, {Key: "Ctrl-Enter", Label: "Start run", Priority: 1}, {Key: "Esc", Label: "Close", Priority: 1}}
	case phLive:
		return []ui.Hint{{Key: "Esc", Label: "Cancel", Priority: 1}, {Key: "Enter", Label: "Detail", Priority: 2}, {Key: "f", Label: "Failures only", Priority: 3}}
	case phExport:
		return []ui.Hint{{Key: "Tab", Label: "JSON / JUnit", Priority: 2}, {Key: "Enter", Label: "Export", Priority: 1}, {Key: "Esc", Label: "Cancel", Priority: 1}}
	case phSummary:
		return []ui.Hint{{Key: "r", Label: "Re-run", Priority: 1}, {Key: "R", Label: "Re-run failed", Priority: 2}, {Key: "e", Label: "Export", Priority: 3}, {Key: "Esc", Label: "Close", Priority: 1}}
	case phDetail:
		return []ui.Hint{{Key: "Tab", Label: "Section", Priority: 2}, {Key: "Esc", Label: "Back", Priority: 1}}
	}
	return nil
}

type runEventsMsg struct {
	events []engine.RunEvent
	done   bool
	err    error
}

// Update implements tui.Screen.
func (m *Model) Update(msg tea.Msg) (tui.Screen, tea.Cmd) {
	switch v := msg.(type) {
	case tui.ThemeChangedMsg:
		m.th = v.Theme
	case spinner.TickMsg:
		if m.phase == phLive {
			var cmd tea.Cmd
			m.spin, cmd = m.spin.Update(v)
			return m, cmd
		}
	case runEventsMsg:
		if v.err != nil {
			m.notice, m.fail = v.err.Error(), true
			m.phase = phSetup
			return m, nil
		}
		for _, ev := range v.events {
			m.applyEvent(ev)
		}
		if v.done {
			m.phase = phSummary
			m.runCancel = nil
		}
		if !v.done && m.runCh != nil {
			return m, m.waitRunEvent()
		}
		return m, nil
	case exportDoneMsg:
		m.phase = phSummary
		if v.err != nil {
			m.notice, m.fail = v.err.Error(), true
			return m, nil
		}
		return m, func() tea.Msg {
			return tui.ToastMsg{Kind: ui.ToastSuccess, Text: "Exported to " + v.path}
		}
	case tickElapsedMsg:
		m.elapsed = time.Since(m.runStart)
		if m.phase == phLive {
			return m, m.tickElapsed()
		}
		return m, nil
	case tea.KeyPressMsg:
		if m.phase == phConfirmCancel {
			return m.confirmKey(v)
		}
		switch m.phase {
		case phSetup:
			return m.setupKey(v)
		case phLive:
			return m.liveKey(v)
		case phSummary:
			return m.summaryKey(v)
		case phDetail:
			return m.detailKey(v)
		case phExport:
			return m.exportKey(v)
		}
	}
	return m, nil
}

// VimMode implements tui.ModeReporter.
func (m *Model) VimMode() string {
	switch m.phase {
	case phSetup:
		if f := m.activeField(); f != nil {
			return f.VimMode()
		}
	case phExport:
		return m.exportPath.VimMode()
	}
	return ""
}

func (m *Model) setupKey(k tea.KeyPressMsg) (tui.Screen, tea.Cmd) {
	m.setupNav.Cursor = m.field
	if r := listmotion.Key(k, listmotion.Config{Count: 7, Page: 7}, &m.setupNav); r.Handled {
		if r.Moved {
			m.field = m.setupNav.Cursor
		}
		return m, nil
	}
	switch k.String() {
	case "esc", "q":
		if f := m.activeField(); f != nil {
			if f.Inserting() {
				_, _, cmd, _, _ := f.UpdateEx(k)
				return m, cmd
			}
			f.Blur()
			return m, nil
		}
		return nil, nil
	case "tab":
		m.field = (m.field + 1) % 7
	case "shift+tab":
		m.field = (m.field + 6) % 7
	case " ":
		switch m.field {
		case 5:
			m.bail = !m.bail
		case 6:
			m.persist = !m.persist
		}
	case "ctrl+enter":
		return m, m.startRun()
	case "enter":
		if m.field == 6 {
			return m, m.startRun()
		}
		if f := m.activeField(); f != nil {
			if !f.Inserting() {
				if f.VimMode() == "" {
					return m, f.Focus()
				}
				_, _, cmd, leave, normal := f.UpdateEx(k)
				if normal && !leave {
					return m, tea.Batch(cmd, f.Focus())
				}
				return m, cmd
			}
			_, _, cmd, _, _ := f.UpdateEx(k)
			return m, cmd
		}
	default:
		if f := m.activeField(); f != nil {
			_, _, cmd, _, _ := f.UpdateEx(k)
			return m, cmd
		}
	}
	return m, nil
}

func (m *Model) activeField() *screenfield.Field {
	switch m.field {
	case 2:
		return m.iterations
	case 3:
		return m.dataPath
	case 4:
		return m.delayMS
	default:
		return nil
	}
}

func (m *Model) liveKey(k tea.KeyPressMsg) (tui.Screen, tea.Cmd) {
	rows := m.visibleRows()
	m.listNav.Cursor, m.listNav.Top = m.cur, m.top
	if r := listmotion.Key(k, listmotion.Config{Count: len(rows), Page: max(1, m.h-8)}, &m.listNav); r.Handled {
		if r.Moved {
			m.cur, m.top = m.listNav.Cursor, m.listNav.Top
		}
		return m, nil
	}
	switch k.String() {
	case "esc":
		m.phase = phConfirmCancel
		m.conf = ui.Confirm{Title: "Cancel run", Body: "Stop the collection run?", Choices: []string{"Keep running", "Cancel run"}, Focus: 0, Danger: true}
	case "f":
		m.failuresOnly = !m.failuresOnly
	case "enter":
		if r := m.currentRow(); r != nil && r.outcome != nil {
			m.detail = r.outcome
			m.phase = phDetail
		}
	case "up":
		m.move(-1)
	case "down":
		m.move(1)
	}
	return m, nil
}

func (m *Model) summaryKey(k tea.KeyPressMsg) (tui.Screen, tea.Cmd) {
	failN := 0
	if m.summary != nil {
		failN = len(m.summary.Failures)
	}
	m.listNav.Cursor = m.failCur
	if r := listmotion.Key(k, listmotion.Config{Count: failN, Page: max(1, m.h-12)}, &m.listNav); r.Handled {
		if r.Moved {
			m.failCur = m.listNav.Cursor
		}
		return m, nil
	}
	switch k.String() {
	case "esc", "q":
		return nil, nil
	case "r":
		return m, m.startRun()
	case "R":
		return m, m.startRunFailedOnly()
	case "e":
		return m.beginExport()
	}
	return m, nil
}

func (m *Model) exportKey(k tea.KeyPressMsg) (tui.Screen, tea.Cmd) {
	switch k.String() {
	case "esc":
		if m.exportPath.Inserting() {
			_, _, cmd, _, _ := m.exportPath.UpdateEx(k)
			return m, cmd
		}
		if m.exportPath.VimMode() == "NORMAL" {
			m.exportPath.Blur()
			return m, nil
		}
		m.phase = phSummary
	case "enter":
		if !m.exportPath.Inserting() {
			if m.exportPath.VimMode() == "" {
				return m, m.exportPath.Focus()
			}
			if m.exportPath.VimMode() == "NORMAL" {
				_, _, cmd, _, _ := m.exportPath.UpdateEx(k)
				return m, tea.Batch(cmd, m.exportPath.Focus())
			}
		}
		return m, m.confirmExport()
	case "tab":
		m.exportXML = !m.exportXML
		m.exportPath, _ = screenfield.New(m.th, swapExportExtension(m.exportPath.Value(), m.exportXML), "export path", false)
	default:
		_, _, cmd, _, _ := m.exportPath.UpdateEx(k)
		return m, cmd
	}
	return m, nil
}

func (m *Model) beginExport() (tui.Screen, tea.Cmd) {
	if m.summary == nil {
		return m, nil
	}
	m.phase = phExport
	m.exportXML = false
	m.exportPath, _ = screenfield.New(m.th, defaultExportPath(m.env, false), "export path", false)
	return m, nil
}

func defaultExportPath(env tui.ScreenEnv, xml bool) string {
	ext := ".json"
	if xml {
		ext = ".xml"
	}
	name := fmt.Sprintf("postctl-run-%d%s", time.Now().Unix(), ext)
	dir, err := os.UserHomeDir()
	if err != nil {
		dir = "."
	}
	if env.Backend != nil {
		dir = filepath.Join(env.Backend.Root(), ".postctl")
	}
	return filepath.Join(dir, name)
}

func (m *Model) detailKey(k tea.KeyPressMsg) (tui.Screen, tea.Cmd) {
	if k.String() == "esc" {
		m.phase = phSummary
		if m.summary == nil {
			m.phase = phLive
		}
	}
	return m, nil
}

func (m *Model) confirmKey(k tea.KeyPressMsg) (tui.Screen, tea.Cmd) {
	switch k.String() {
	case "esc":
		m.phase = phLive
	case "enter":
		if m.conf.Focus == 1 && m.runCancel != nil {
			m.runCancel()
			m.phase = phSummary
		} else {
			m.phase = phLive
		}
	case "left", "h":
		m.conf.Focus = 0
	case "right", "l":
		m.conf.Focus = 1
	}
	return m, nil
}

func (m *Model) startRun() tea.Cmd {
	return m.startRunWith(nil)
}

func (m *Model) startRunWith(only []model.ID) tea.Cmd {
	if m.deps.Runner == nil || m.coll == nil {
		m.notice, m.fail = "Runner is not available.", true
		return nil
	}
	in, err := m.buildInput(only)
	if err != nil {
		m.notice, m.fail = err.Error(), true
		return nil
	}
	m.resetLive(in)
	ctx, cancel := context.WithCancel(context.Background())
	m.runCancel = cancel
	ch, err := m.deps.Runner.RunAsync(ctx, in)
	if err != nil {
		m.notice, m.fail = err.Error(), true
		return nil
	}
	m.runCh = ch
	return tea.Batch(m.waitRunEvent(), m.spin.Tick, m.tickElapsed())
}

type tickElapsedMsg struct{}

func (m *Model) tickElapsed() tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return tickElapsedMsg{} })
}

func (m *Model) waitRunEvent() tea.Cmd {
	ch := m.runCh
	if ch == nil {
		return nil
	}
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return runEventsMsg{done: true}
		}
		done := false
		if _, ok := ev.(engine.RunFinished); ok {
			done = true
		}
		return runEventsMsg{events: []engine.RunEvent{ev}, done: done}
	}
}

func (m *Model) startRunFailedOnly() tea.Cmd {
	if m.summary == nil {
		return m.startRunWith(nil)
	}
	seen := map[model.ID]struct{}{}
	var only []model.ID
	for _, f := range m.summary.Failures {
		if f.RequestID == "" {
			continue
		}
		if _, ok := seen[f.RequestID]; ok {
			continue
		}
		seen[f.RequestID] = struct{}{}
		only = append(only, f.RequestID)
	}
	if len(only) == 0 {
		m.notice, m.fail = "No failed requests to re-run.", false
		return nil
	}
	return m.startRunWith(only)
}

func (m *Model) buildInput(only []model.ID) (engine.RunInput, error) {
	iters, _ := strconv.Atoi(strings.TrimSpace(m.iterations.Value()))
	if iters <= 0 {
		iters = 1
	}
	delay, _ := strconv.Atoi(strings.TrimSpace(m.delayMS.Value()))
	var data []map[string]any
	if p := strings.TrimSpace(m.dataPath.Value()); p != "" {
		rows, err := engine.LoadDataFile(p)
		if err != nil {
			return engine.RunInput{}, err
		}
		data = rows
	}
	var env *model.Environment
	if m.env.Workspace != nil && m.envID != "" {
		env = m.env.Workspace.Environment(m.envID)
	}
	var envClone *model.Environment
	if env != nil {
		envClone = env.Clone()
	}
	return engine.RunInput{
		Collection:   m.coll.Clone(),
		FolderID:     m.folderID,
		Environment:  envClone,
		Iterations:   iters,
		Data:         data,
		Delay:        time.Duration(delay) * time.Millisecond,
		Bail:         m.bail,
		NoPersist:    !m.persist,
		OnlyRequests: append([]model.ID(nil), only...),
	}, nil
}

func (m *Model) resetLive(in engine.RunInput) {
	m.phase = phLive
	m.rows = nil
	m.summary = nil
	m.detail = nil
	m.runEvents = nil
	m.cur, m.top = 0, 0
	m.runStart = time.Now()
	m.planRows(in)
}

func (m *Model) planRows(in engine.RunInput) {
	// Flatten requests in scope for display; live events update rows.
	c := m.coll
	if c == nil {
		return
	}
	var walk func(items []model.Item, path []string)
	allowed := map[model.ID]struct{}{}
	for _, id := range in.OnlyRequests {
		allowed[id] = struct{}{}
	}
	filter := len(in.OnlyRequests) > 0
	walk = func(items []model.Item, path []string) {
		for _, it := range items {
			switch v := it.(type) {
			case *model.Request:
				if filter {
					if _, ok := allowed[v.ID]; !ok {
						continue
					}
				}
				m.rows = append(m.rows, row{id: v.ID, name: v.Name, method: v.Method})
			case *model.Folder:
				if in.FolderID != "" && v.ID != in.FolderID && !folderContains(v, in.FolderID) {
					continue
				}
				walk(v.Items, append(path, v.Name))
			}
		}
	}
	walk(c.Items, nil)
}

func folderContains(f *model.Folder, id model.ID) bool {
	for _, it := range f.Items {
		if v, ok := it.(*model.Folder); ok && (v.ID == id || folderContains(v, id)) {
			return true
		}
	}
	return false
}

func (m *Model) applyEvent(ev engine.RunEvent) {
	m.runEvents = append(m.runEvents, ev)
	switch e := ev.(type) {
	case engine.RunStarted:
		m.iterTotal = e.Iterations
	case engine.IterationStarted:
		m.iter = e.Iteration
	case engine.RequestStarted:
		m.ensureRow(e.RequestID, e.RequestName, "")
		m.setRowState(e.RequestID, stRunning, "")
	case engine.RequestProgress:
		m.setRowStep(e.RequestID, e.Step.Text)
	case engine.RequestFinished:
		m.finishRow(e.Outcome)
	case engine.RunFinished:
		m.summary = e.Summary
		m.elapsed = e.Summary.TotalTime
	}
}

func (m *Model) ensureRow(id model.ID, name, method string) {
	for i := range m.rows {
		if m.rows[i].id == id {
			return
		}
	}
	m.rows = append(m.rows, row{id: id, name: name, method: method, state: stPending})
}

func (m *Model) setRowState(id model.ID, st reqState, step string) {
	for i := range m.rows {
		if m.rows[i].id == id {
			m.rows[i].state = st
			m.rows[i].step = step
			return
		}
	}
}

func (m *Model) setRowStep(id model.ID, step string) {
	for i := range m.rows {
		if m.rows[i].id == id {
			m.rows[i].step = step
			return
		}
	}
}

func (m *Model) finishRow(out *engine.Outcome) {
	if out == nil {
		return
	}
	m.ensureRow(out.RequestID, "", "")
	for i := range m.rows {
		if m.rows[i].id != out.RequestID {
			continue
		}
		m.rows[i].state = stDone
		m.rows[i].outcome = out
		if out.Response != nil {
			m.rows[i].status = out.Response.StatusCode
		}
		if out.Err != nil {
			m.rows[i].errText = out.Err.Error()
		}
		for _, t := range out.Tests {
			if t.Passed || t.Skipped {
				m.rows[i].testsPass++
			} else {
				m.rows[i].testsFail++
			}
		}
		return
	}
}

type exportDoneMsg struct {
	path string
	err  error
}

func (m *Model) confirmExport() tea.Cmd {
	path := strings.TrimSpace(m.exportPath.Value())
	if path == "" {
		return func() tea.Msg { return exportDoneMsg{err: fmt.Errorf("choose a file path")} }
	}
	ext := strings.ToLower(filepath.Ext(path))
	if ext != ".json" && ext != ".xml" {
		return func() tea.Msg { return exportDoneMsg{err: fmt.Errorf("export path must end in .json or .xml")} }
	}
	events := append([]engine.RunEvent(nil), m.runEvents...)
	mask := m.reportMask()
	return func() tea.Msg {
		abs, err := filepath.Abs(path)
		if err != nil {
			return exportDoneMsg{err: err}
		}
		if err := os.MkdirAll(filepath.Dir(abs), 0o700); err != nil {
			return exportDoneMsg{err: err}
		}
		f, err := os.OpenFile(abs, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
		if err != nil {
			return exportDoneMsg{err: err}
		}
		opts := report.Options{Mask: mask}
		var rep report.Reporter
		if ext == ".xml" {
			rep = report.NewJUnit(f, opts)
		} else {
			rep = report.NewJSON(f, opts)
		}
		writeErr := report.Write(rep, events)
		closeErr := f.Close()
		if writeErr != nil {
			return exportDoneMsg{err: writeErr}
		}
		return exportDoneMsg{path: abs, err: closeErr}
	}
}

func swapExportExtension(path string, xml bool) string {
	ext := ".json"
	if xml {
		ext = ".xml"
	}
	old := filepath.Ext(path)
	if strings.EqualFold(old, ".json") || strings.EqualFold(old, ".xml") {
		return strings.TrimSuffix(path, old) + ext
	}
	return path + ext
}

func (m *Model) reportMask() func(string) string {
	var values []string
	add := func(vars []model.Variable) {
		for _, v := range vars {
			if v.IsSecret() && v.Value != "" {
				values = append(values, v.Value)
			}
		}
	}
	if ws := m.env.Workspace; ws != nil {
		if ws.Globals != nil {
			add(ws.Globals.Variables)
		}
		for _, env := range ws.Environments {
			add(env.Variables)
		}
		for _, coll := range ws.Collections {
			add(coll.Variables)
			_ = coll.Walk(func(it model.Item, _ model.Container, _ int) error {
				if folder, ok := it.(*model.Folder); ok {
					add(folder.Variables)
				}
				return nil
			})
		}
	}
	redactor := history.NewRedactor(values)
	return redactor.Text
}

func (m *Model) currentRow() *row {
	if m.cur < 0 || m.cur >= len(m.rows) {
		return nil
	}
	return &m.rows[m.cur]
}

func (m *Model) move(d int) {
	visible := m.visibleRows()
	if len(visible) == 0 {
		return
	}
	m.cur = clamp(m.cur+d, 0, len(visible)-1)
	if m.cur < m.top {
		m.top = m.cur
	}
	if m.cur >= m.top+m.listRows() {
		m.top = m.cur - m.listRows() + 1
	}
}

func (m *Model) visibleRows() []row {
	if !m.failuresOnly {
		return m.rows
	}
	out := make([]row, 0, len(m.rows))
	for _, r := range m.rows {
		if r.testsFail > 0 || r.errText != "" {
			out = append(out, r)
		}
	}
	return out
}

func (m *Model) listRows() int { return max(1, m.h-8) }

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
	case phConfirmCancel:
		return m.viewConfirm()
	case phExport:
		return m.viewExport()
	case phSetup:
		return m.viewSetup()
	case phLive:
		return m.viewLive()
	case phDetail:
		return m.viewDetail()
	default:
		return m.viewSummary()
	}
}

func (m *Model) header(title string) string {
	env := ui.EnvInfo{}
	if m.env.Workspace != nil && m.envID != "" {
		if e := m.env.Workspace.Environment(m.envID); e != nil {
			env = ui.EnvInfo{Name: e.Name, Production: e.Production}
		}
	}
	wsName := title
	if m.env.Workspace != nil && m.env.Workspace.Name != "" {
		wsName = m.env.Workspace.Name + " — " + title
	}
	return m.th.Header(m.w, ui.HeaderInfo{Workspace: wsName, Env: env})
}

func (m *Model) viewSetup() string {
	t := m.th
	body := m.header("Run collection")
	lines := []string{
		fieldLine(t, m.field == 0, "Scope", m.scopeLabel),
		fieldLine(t, m.field == 1, "Environment", m.envLabel()),
		fieldLine(t, m.field == 2, "Iterations", runnerField(m.iterations, 20)),
		fieldLine(t, m.field == 3, "Data file", runnerField(m.dataPath, m.w-20)),
		fieldLine(t, m.field == 4, "Delay (ms)", runnerField(m.delayMS, 12)),
		fieldLine(t, m.field == 5, "Bail on failure", checkbox(m.bail)),
		fieldLine(t, m.field == 6, "Persist variables", checkbox(m.persist)),
	}
	body += strings.Join(lines, "\n")
	body += "\n" + t.Render(ui.TextSecondary, "Review  Environment  "+m.envLabel())
	if m.notice != "" {
		body += "\n" + t.Render(ui.StatusDanger, m.notice)
	}
	body += "\n" + t.Render(ui.TextQuiet, "[ Ctrl-Enter ] Start run")
	return body
}

func checkbox(on bool) string {
	if on {
		return "[x]"
	}
	return "[ ]"
}

func fieldLine(t ui.Theme, active bool, label, value string) string {
	prefix := "  "
	if active {
		prefix = t.Render(ui.AccentPrimary, t.Glyphs.Select+" ")
	}
	return prefix + t.Render(ui.TextSecondary, label+":  ") + t.Render(ui.TextPrimary, value)
}

func (m *Model) envLabel() string {
	if m.env.Workspace == nil || m.envID == "" {
		return "none"
	}
	if e := m.env.Workspace.Environment(m.envID); e != nil {
		if e.Production {
			return "! " + e.Name
		}
		return e.Name
	}
	return "none"
}

func (m *Model) viewLive() string {
	t := m.th
	title := fmt.Sprintf("Running  iteration %d/%d", m.iter+1, max(1, m.iterTotal))
	body := m.header(title)
	rows := m.visibleRows()
	listH := m.listRows()
	for i, r := range rows {
		if i < m.top {
			continue
		}
		if i-m.top >= listH {
			break
		}
		body += m.renderRow(r, i == m.cur) + "\n"
	}
	sum := fmt.Sprintf("Requests %d  Elapsed %s", len(m.rows), m.elapsed.Truncate(time.Second))
	body += "\n" + t.Render(ui.TextSecondary, sum)
	return body
}

func (m *Model) renderRow(r row, sel bool) string {
	t := m.th
	mark := "  "
	if sel {
		mark = t.Render(ui.AccentPrimary, t.Glyphs.Select+" ")
	}
	line := mark + t.MethodBadge(r.method, 5) + " " + t.Render(ui.TextPrimary, t.Truncate(r.name, 20))
	switch r.state {
	case stRunning:
		spin := t.Render(ui.StatusInfo, "… "+t.Truncate(r.step, 30))
		line += "  " + spin
	case stDone:
		if r.outcome != nil && r.outcome.Skipped {
			label := "Skipped"
			if r.outcome.SkipReason != "" {
				label += ": " + r.outcome.SkipReason
			}
			line += "  " + t.Render(ui.StatusWarning, t.Truncate(label, 42))
		} else if r.errText != "" {
			line += "  " + t.Render(ui.StatusDanger, "× Failed")
		} else if r.status > 0 {
			line += "  " + t.Render(ui.StatusSuccess, fmt.Sprintf("%d", r.status))
		}
		if tot := r.testsPass + r.testsFail; tot > 0 {
			if r.testsFail > 0 {
				line += "  " + t.Render(ui.StatusDanger, fmt.Sprintf("× %d failed", r.testsFail))
			} else {
				line += "  " + t.Render(ui.StatusSuccess, fmt.Sprintf("Tests %d/%d", r.testsPass, tot))
			}
		}
	}
	return line
}

func (m *Model) viewSummary() string {
	t := m.th
	body := m.header("Run summary")
	if m.summary == nil {
		body += t.EmptyState("No summary", "The run did not finish.", "", m.w)
		return body
	}
	s := m.summary
	lines := []string{
		fmt.Sprintf("Iterations  %d", s.Iterations),
		fmt.Sprintf("Requests    %d", s.Requests),
		fmt.Sprintf("Skipped     %d", s.Skipped),
		fmt.Sprintf("Tests       %d passed, %d failed", s.Tests.Passed, s.Tests.Failed),
		fmt.Sprintf("Duration    %s", s.TotalTime.Truncate(time.Millisecond)),
	}
	if s.StopReason != "" {
		lines = append(lines, "Stopped     "+string(s.StopReason))
	}
	body += t.Render(ui.TextPrimary, strings.Join(lines, "\n"))
	if len(s.Failures) > 0 {
		body += "\n\n" + t.Bold().Render("Failures")
		for _, f := range s.Failures {
			body += "\n" + t.Render(ui.StatusDanger, "× "+f.RequestName+": "+t.Truncate(f.Message, m.w-10))
		}
	}
	body += "\n" + t.Render(ui.TextQuiet, "r Re-run  R Re-run failed  e Export  Esc Close")
	return body
}

func (m *Model) viewDetail() string {
	t := m.th
	body := m.header("Request detail")
	if m.detail == nil {
		return body
	}
	body += response.Snapshot(t, outcomeResult(m.detail), response.SnapshotBody, m.w, max(1, m.h-4))
	return body
}

func outcomeResult(out *engine.Outcome) tui.SendResult {
	mask := func(text string) string {
		if out.Redactor == nil {
			return text
		}
		return out.Redactor.Text(text)
	}
	res := tui.SendResult{
		RequestID: out.RequestID, Response: out.Response, Err: out.Err, Redactor: out.Redactor,
	}
	for _, t := range out.Tests {
		res.Tests = append(res.Tests, tui.TestOutcome{
			Name: mask(t.Name), Passed: t.Passed, Skipped: t.Skipped, Message: mask(t.Message),
		})
	}
	for _, c := range out.Console {
		res.Console = append(res.Console, tui.ConsoleLine{Level: c.Level, Text: mask(c.Text), Event: c.Event})
	}
	return res
}

func findFolder(c *model.Collection, id model.ID) *model.Folder {
	var walk func(items []model.Item) *model.Folder
	walk = func(items []model.Item) *model.Folder {
		for _, it := range items {
			if f, ok := it.(*model.Folder); ok {
				if f.ID == id {
					return f
				}
				if sub := walk(f.Items); sub != nil {
					return sub
				}
			}
		}
		return nil
	}
	return walk(c.Items)
}

func (m *Model) viewConfirm() string {
	body := m.header("Run collection")
	body += m.th.ConfirmView(m.conf, m.w)
	return body
}

func (m *Model) viewExport() string {
	body := m.header("Export run summary")
	format := "JSON (NDJSON)"
	if m.exportXML {
		format = "JUnit XML"
	}
	body += m.th.Render(ui.TextSecondary, "Format") + "  " + m.th.Render(ui.AccentPrimary, format) + m.th.Render(ui.TextQuiet, "  Tab toggles") + "\n"
	body += m.th.Render(ui.TextSecondary, "File path") + "\n"
	body += m.th.Render(ui.AccentPrimary, runnerField(m.exportPath, m.w-2))
	return body
}

func runnerField(f *screenfield.Field, _ int) string {
	if f == nil {
		return ""
	}
	return f.View()
}
