// Package envedit is the environment editor screen (Ctrl-E): create, rename,
// duplicate and delete environments, mark one as production, and edit the
// variables of environments, the workspace globals and collections. It plugs
// in through tui.Options.NewEnvEditor (see [Factory]).
package envedit

import (
	"context"
	"errors"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/shubham/postctl/internal/model"
	"github.com/shubham/postctl/internal/tui"
	"github.com/shubham/postctl/internal/tui/listmotion"
	"github.com/shubham/postctl/internal/tui/screenfield"
	"github.com/shubham/postctl/internal/ui"
)

// Deps are the effects the screen performs, injectable for tests.
type Deps struct {
	// After delivers msg after d; tea.Tick when nil.
	After func(d time.Duration, msg tea.Msg) tea.Cmd
	// Debounce is the autosave delay; 500 ms when zero.
	Debounce time.Duration
}

func (d Deps) after(dur time.Duration, msg tea.Msg) tea.Cmd {
	if d.After != nil {
		return d.After(dur, msg)
	}
	return tea.Tick(dur, func(time.Time) tea.Msg { return msg })
}

func (d Deps) debounce() time.Duration {
	if d.Debounce > 0 {
		return d.Debounce
	}
	return 500 * time.Millisecond
}

// Factory returns the screen factory for tui.Options.NewEnvEditor.
func Factory(d Deps) tui.ScreenFactory {
	return func(env tui.ScreenEnv) tui.Screen { return New(env, d) }
}

type tabKind int

const (
	tabEnvs tabKind = iota
	tabGlobals
	tabCollections
)

var tabNames = []string{"Environments", "Globals", "Collections"}

type zone int

const (
	zList zone = iota
	zTable
)

type mode int

const (
	modeNone    mode = iota
	modeName         // typing a name (new, rename or duplicate)
	modeConfirm      // delete confirmation
)

type nameIntent int

const (
	intentNew nameIntent = iota
	intentRename
	intentDuplicate
)

// saveKey identifies one thing to persist.
type saveKey struct {
	kind tui.SaveKind
	id   model.ID
}

type (
	saveTickMsg struct {
		key saveKey
		seq uint64
	}
	// hydratedMsg carries secret values read off the update goroutine.
	hydratedMsg struct {
		owner string
		vars  []model.Variable
		err   error
		// then says what to do next.
		then hydrateThen
	}
	noticeMsg struct{ at time.Time }
)

type hydrateThen int

const (
	thenReveal hydrateThen = iota
	thenUnsecret
	thenDuplicate
)

// Model is the environment editor screen.
type Model struct {
	th   ui.Theme
	env  tui.ScreenEnv
	deps Deps
	w, h int

	tab  tabKind
	zone zone
	sel  [3]int // selected item per tab (environment, -, collection)

	mode   mode
	intent nameIntent
	name   *screenfield.Field
	nameEr string
	dupOf  model.ID
	dupVs  []model.Variable
	conf   ui.Confirm
	confFn func() tea.Cmd

	// variables table
	cur, top      int
	edit          bool
	col           int
	in            *screenfield.Field
	draft         bool
	reveal        bool
	editBaseline  string
	editSkipValue bool
	listNav       listmotion.View

	seq    map[saveKey]uint64
	dirty  map[saveKey]bool
	failed map[saveKey]string
	saved  bool

	notice string
	noteAt time.Time
	clock  func() time.Time
}

// New builds the screen for env.
func New(env tui.ScreenEnv, d Deps) *Model {
	m := &Model{
		th: env.Theme, env: env, deps: d, zone: zList,
		seq: map[saveKey]uint64{}, dirty: map[saveKey]bool{}, failed: map[saveKey]string{},
		clock: time.Now,
	}
	m.focusInitial()
	return m
}

func (m *Model) ws() *model.Workspace { return m.env.Workspace }

func (m *Model) focusInitial() {
	id := m.env.Focus
	if id == "" {
		id = m.env.ActiveEnv
	}
	for i, e := range m.ws().Environments {
		if e.ID == id {
			m.sel[tabEnvs] = i
		}
	}
	if len(m.ws().Environments) == 0 && len(m.ws().Collections) > 0 {
		m.tab = tabCollections
	}
}

// SetSize implements tui.Screen.
func (m *Model) SetSize(w, h int) {
	m.w, m.h = w, h
	if m.name != nil {
		m.name.SetWidth(max(4, w-12))
	}
	if m.in != nil {
		m.in.SetWidth(max(4, w-15))
	}
}

// ---- what is being edited ------------------------------------------------------

func (m *Model) envs() []*model.Environment { return m.ws().Environments }

func (m *Model) curEnv() *model.Environment {
	es := m.envs()
	if len(es) == 0 {
		return nil
	}
	sel := min(max(m.sel[tabEnvs], 0), len(es)-1)
	return es[sel]
}

func (m *Model) curColl() *model.Collection {
	cs := m.ws().Collections
	if len(cs) == 0 {
		return nil
	}
	sel := min(max(m.sel[tabCollections], 0), len(cs)-1)
	return cs[sel]
}

// list is the variables being edited, or nil when there is nothing to edit.
func (m *Model) list() *[]model.Variable {
	switch m.tab {
	case tabEnvs:
		if e := m.curEnv(); e != nil {
			return &e.Variables
		}
	case tabGlobals:
		if m.ws().Globals == nil {
			return nil
		}
		return &m.ws().Globals.Variables
	case tabCollections:
		if c := m.curColl(); c != nil {
			return &c.Variables
		}
	}
	return nil
}

// ownerKey names the current list for stale-result checks.
func (m *Model) ownerKey() string {
	switch m.tab {
	case tabEnvs:
		if e := m.curEnv(); e != nil {
			return "env:" + string(e.ID)
		}
	case tabGlobals:
		return "globals"
	case tabCollections:
		if c := m.curColl(); c != nil {
			return "coll:" + string(c.ID)
		}
	}
	return ""
}

// saveTarget is what to persist after editing the current list.
func (m *Model) saveTarget() (saveKey, bool) {
	switch m.tab {
	case tabEnvs:
		if e := m.curEnv(); e != nil {
			return saveKey{tui.SaveEnvironment, e.ID}, true
		}
	case tabGlobals:
		if g := m.ws().Globals; g != nil {
			return saveKey{tui.SaveGlobals, ""}, true
		}
	case tabCollections:
		if c := m.curColl(); c != nil {
			return saveKey{tui.SaveCollection, c.ID}, true
		}
	}
	return saveKey{}, false
}

// ---- saving ------------------------------------------------------------------------

func (m *Model) touch(k saveKey) tea.Cmd {
	m.seq[k]++
	m.dirty[k] = true
	delete(m.failed, k)
	return m.deps.after(m.deps.debounce(), saveTickMsg{key: k, seq: m.seq[k]})
}

func msgCmd(msg tea.Msg) tea.Cmd { return func() tea.Msg { return msg } }

func saveMsg(k saveKey) tea.Msg {
	msg := tui.SaveMsg{Kind: k.kind, ID: k.id}
	if k.kind == tui.SaveCollection {
		msg.Collection = k.id
	}
	return msg
}

// flushAll writes everything still waiting for the debounce.
func (m *Model) flushAll() tea.Cmd {
	var cmds []tea.Cmd
	for k, d := range m.dirty {
		if d {
			cmds = append(cmds, msgCmd(saveMsg(k)))
			m.dirty[k] = false
		}
	}
	return tea.Batch(cmds...)
}

func (m *Model) pending() bool {
	for _, d := range m.dirty {
		if d {
			return true
		}
	}
	return false
}

func (m *Model) onSaveDone(d tui.SaveDoneMsg) {
	k := saveKey{d.Kind, d.ID}
	if d.Kind == tui.SaveCollection {
		k = saveKey{d.Kind, d.ID}
	}
	if d.Err != nil {
		m.failed[k] = d.Err.Error()
		return
	}
	delete(m.failed, k)
	m.saved = true
}

// saveLabel is the quiet autosave state.
func (m *Model) saveLabel() (string, bool) {
	if len(m.failed) > 0 {
		return m.th.Glyphs.Fail + " Not saved", true
	}
	if m.pending() {
		return "Unsaved", false
	}
	if m.saved {
		return "Saved", false
	}
	return "", false
}

func (m *Model) setNotice(text string) tea.Cmd {
	m.notice = text
	m.noteAt = m.clock()
	at := m.noteAt
	return m.deps.after(4*time.Second, noticeMsg{at: at})
}

// ---- Update ---------------------------------------------------------------------------

// Update implements tui.Screen.
func (m *Model) Update(msg tea.Msg) (tui.Screen, tea.Cmd) {
	switch v := msg.(type) {
	case tea.KeyPressMsg:
		return m.onKey(v)
	case tea.PasteMsg:
		switch {
		case m.mode == modeName:
			_, _, cmd, _, _ := m.name.UpdateEx(v)
			m.nameEr = ""
			return m, cmd
		case m.edit:
			changed, cmd := m.in.Update(v)
			if changed {
				return m, tea.Batch(cmd, m.applyText())
			}
			return m, cmd
		}
		return m, nil
	case saveTickMsg:
		if m.seq[v.key] == v.seq && m.dirty[v.key] {
			m.dirty[v.key] = false
			return m, msgCmd(saveMsg(v.key))
		}
		return m, nil
	case tui.SaveDoneMsg:
		m.onSaveDone(v)
		return m, nil
	case hydratedMsg:
		return m, m.onHydrated(v)
	case noticeMsg:
		if m.noteAt.Equal(v.at) {
			m.notice = ""
		}
		return m, nil
	case tui.ThemeChangedMsg:
		m.th = v.Theme
	}
	return m, nil
}

func (m *Model) close() (tui.Screen, tea.Cmd) {
	return nil, m.flushAll()
}

func (m *Model) onKey(k tea.KeyPressMsg) (tui.Screen, tea.Cmd) {
	m.notice = ""
	switch {
	case m.mode == modeConfirm:
		return m, m.confirmKey(k)
	case m.mode == modeName:
		return m, m.nameKey(k)
	case m.edit:
		return m, m.editKey(k)
	}
	s := k.String()
	switch s {
	case "[":
		m.switchTab(-1)
		return m, nil
	case "]":
		m.switchTab(1)
		return m, nil
	case "1", "2", "3":
		m.setTab(tabKind(s[0] - '1'))
		return m, nil
	}
	if m.zone == zList && m.tab != tabGlobals {
		return m.listKey(k)
	}
	return m.tableKey(k)
}

func (m *Model) setTab(t tabKind) {
	if t == m.tab {
		return
	}
	m.tab = t
	m.cur, m.top = 0, 0
	m.reveal = false
	m.draft = false
	if t == tabGlobals {
		m.zone = zTable
	} else {
		m.zone = zList
	}
}

func (m *Model) switchTab(dir int) {
	m.setTab(tabKind((int(m.tab) + dir + 3) % 3))
}

// selectItem changes the highlighted environment or collection.
func (m *Model) selectItem(delta int) {
	n := len(m.envs())
	if m.tab == tabCollections {
		n = len(m.ws().Collections)
	}
	if n == 0 {
		return
	}
	m.sel[m.tab] = (m.sel[m.tab] + delta + n) % n
	m.cur, m.top = 0, 0
	m.reveal = false
}

// VimMode implements tui.ModeReporter for the footer mode label.
func (m *Model) VimMode() string {
	if m.mode == modeName && m.name != nil {
		return m.name.VimMode()
	}
	if m.edit && m.in != nil {
		return m.in.VimMode()
	}
	return ""
}

func (m *Model) listKey(k tea.KeyPressMsg) (tui.Screen, tea.Cmd) {
	n := len(m.envs())
	if m.tab == tabCollections {
		n = len(m.ws().Collections)
	}
	m.listNav.Cursor = m.sel[m.tab]
	if r := listmotion.Key(k, listmotion.Config{Count: n, Page: max(1, m.h-6)}, &m.listNav); r.Handled {
		if r.Moved && n > 0 {
			m.sel[m.tab] = m.listNav.Cursor
			m.cur, m.top = 0, 0
			m.reveal = false
		}
		return m, nil
	}
	switch k.String() {
	case "esc", "q":
		return m.close()
	case "up", "left":
		m.selectItem(-1)
	case "down", "right":
		m.selectItem(1)
	case "tab", "enter":
		if m.list() != nil {
			m.zone = zTable
		}
	case "n":
		if m.tab == tabEnvs {
			m.beginName(intentNew, "")
		}
	case "r", "f2":
		if e := m.curEnv(); m.tab == tabEnvs && e != nil {
			m.beginName(intentRename, e.Name)
		}
	case "c":
		if e := m.curEnv(); m.tab == tabEnvs && e != nil {
			return m, m.startDuplicate(e)
		}
	case "d", "delete":
		if e := m.curEnv(); m.tab == tabEnvs && e != nil {
			n := 0
			for _, v := range e.Variables {
				if v.IsSecret() {
					n++
				}
			}
			body := "Delete “" + e.Name + "”?"
			if m.th.ASCII {
				body = `Delete "` + e.Name + `"?`
			}
			body += " Its variables"
			if n > 0 {
				body += " and " + itoa(n) + " secret " + map[bool]string{true: "value", false: "values"}[n == 1]
			}
			body += " are removed. This cannot be undone."
			m.conf = ui.NewConfirm("Delete environment", body, "Delete", true)
			m.confFn = m.deleteEnv
			m.mode = modeConfirm
		}
	case "p":
		if e := m.curEnv(); m.tab == tabEnvs && e != nil {
			e.Production = !e.Production
			note := "Marked as production"
			if !e.Production {
				note = "No longer marked as production"
			}
			return m, tea.Batch(m.touch(saveKey{tui.SaveEnvironment, e.ID}), m.setNotice(note))
		}
	}
	return m, nil
}

// ---- names: new, rename, duplicate ------------------------------------------------

func (m *Model) beginName(in nameIntent, initial string) {
	m.mode = modeName
	m.intent = in
	f, _ := screenfield.New(m.th, initial, "Environment name", false)
	m.name = f
	m.name.SetWidth(max(4, m.w-12))
	m.nameEr = ""
}

func (m *Model) nameTaken(name string, except model.ID) bool {
	for _, e := range m.envs() {
		if e.ID != except && strings.EqualFold(e.Name, name) {
			return true
		}
	}
	return false
}

func (m *Model) nameKey(k tea.KeyPressMsg) tea.Cmd {
	submit, changed, cmd, leave, normal := m.name.UpdateEx(k)
	if leave || (k.String() == "esc" && normal) {
		m.mode = modeNone
		m.dupVs = nil
		return cmd
	}
	if submit || (k.String() == "enter" && normal) {
		name := strings.TrimSpace(m.name.Value())
		if name == "" {
			m.nameEr = "Give the environment a name."
			return cmd
		}
		var except model.ID
		if m.intent == intentRename {
			except = m.curEnv().ID
		}
		if m.nameTaken(name, except) {
			m.nameEr = "Another environment already has that name."
			return cmd
		}
		m.mode = modeNone
		return tea.Batch(cmd, m.applyName(name))
	}
	if changed {
		m.nameEr = ""
	}
	return cmd
}

func (m *Model) applyName(name string) tea.Cmd {
	ws := m.ws()
	switch m.intent {
	case intentRename:
		e := m.curEnv()
		e.Name = name
		return m.touch(saveKey{tui.SaveEnvironment, e.ID})
	case intentNew:
		e := &model.Environment{ID: model.NewID(), Name: name}
		ws.Environments = append(ws.Environments, e)
		m.sel[tabEnvs] = len(ws.Environments) - 1
		m.cur, m.top = 0, 0
		return m.touch(saveKey{tui.SaveEnvironment, e.ID})
	default:
		src := m.dupOf
		vars := m.dupVs
		m.dupVs = nil
		e := &model.Environment{ID: model.NewID(), Name: name, Variables: vars}
		for _, o := range ws.Environments {
			if o.ID == src {
				e.Production = o.Production
				e.Description = o.Description
			}
		}
		ws.Environments = append(ws.Environments, e)
		m.sel[tabEnvs] = len(ws.Environments) - 1
		m.cur, m.top = 0, 0
		return m.touch(saveKey{tui.SaveEnvironment, e.ID})
	}
}

// startDuplicate copies the variables (reading secret values first, so the
// copy gets its own secrets) and then asks for the new name.
func (m *Model) startDuplicate(e *model.Environment) tea.Cmd {
	m.dupOf = e.ID
	vs := cloneVars(e.Variables)
	for i := range vs {
		vs[i].SecretRef = "" // the copy must not share the original's secret
	}
	needs := false
	orig := cloneVars(e.Variables)
	for _, v := range orig {
		if v.IsSecret() && v.Value == "" && v.SecretRef != "" {
			needs = true
		}
	}
	if !needs {
		m.dupVs = vs
		m.beginName(intentDuplicate, e.Name+" copy")
		return nil
	}
	return m.hydrate("dup:"+string(e.ID), orig, thenDuplicate)
}

func cloneVars(in []model.Variable) []model.Variable {
	return append([]model.Variable(nil), in...)
}

func (m *Model) confirmKey(k tea.KeyPressMsg) tea.Cmd {
	switch k.String() {
	case "left", "shift+tab", "up":
		m.conf.Move(-1)
	case "right", "tab", "down":
		m.conf.Move(1)
	case "esc", "n":
		m.mode = modeNone
	case "enter", "y":
		m.mode = modeNone
		if k.String() == "y" || m.conf.Accepted() {
			return m.confFn()
		}
	}
	return nil
}

func (m *Model) deleteEnv() tea.Cmd {
	e := m.curEnv()
	if e == nil {
		return nil
	}
	ws := m.ws()
	for i, o := range ws.Environments {
		if o.ID == e.ID {
			ws.Environments = append(ws.Environments[:i:i], ws.Environments[i+1:]...)
			break
		}
	}
	delete(m.dirty, saveKey{tui.SaveEnvironment, e.ID})
	m.sel[tabEnvs] = min(m.sel[tabEnvs], max(0, len(ws.Environments)-1))
	m.cur, m.top = 0, 0
	return tea.Batch(msgCmd(tui.SaveMsg{Kind: tui.DeleteEnvironment, ID: e.ID}), m.setNotice("Deleted “"+e.Name+"”"))
}

// ---- hydration -------------------------------------------------------------------------

// hydrate reads secret values for vs in a command, on a copy.
func (m *Model) hydrate(owner string, vs []model.Variable, then hydrateThen) tea.Cmd {
	h, _ := m.env.Backend.(tui.Hydrator)
	return func() tea.Msg {
		if h == nil {
			return hydratedMsg{owner: owner, vars: vs, then: then, err: errors.New("no secret store is available")}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		err := h.HydrateVariables(ctx, vs)
		return hydratedMsg{owner: owner, vars: vs, then: then, err: err}
	}
}

func (m *Model) onHydrated(h hydratedMsg) tea.Cmd {
	switch h.then {
	case thenDuplicate:
		if !strings.HasPrefix(h.owner, "dup:") || model.ID(strings.TrimPrefix(h.owner, "dup:")) != m.dupOf {
			return nil
		}
		vs := h.vars
		for i := range vs {
			vs[i].SecretRef = ""
		}
		var note tea.Cmd
		if h.err != nil {
			// Keep the secret variables, without their values.
			for i := range vs {
				if vs[i].IsSecret() {
					vs[i].Value = ""
				}
			}
			note = m.setNotice("Secret values could not be read, so the copy has empty secrets")
		}
		m.dupVs = vs
		src := ""
		for _, e := range m.envs() {
			if e.ID == m.dupOf {
				src = e.Name
			}
		}
		m.beginName(intentDuplicate, src+" copy")
		return note
	case thenReveal, thenUnsecret:
		if h.owner != m.ownerKey() {
			return nil
		}
		l := m.list()
		if l == nil {
			return nil
		}
		if h.err != nil {
			m.reveal = false
			return m.setNotice("Could not read secret values: " + short(h.err))
		}
		cur := *l
		if len(cur) != len(h.vars) {
			return nil
		}
		for i := range cur {
			if cur[i].Key == h.vars[i].Key && cur[i].IsSecret() && cur[i].Value == "" {
				cur[i].Value = h.vars[i].Value
			}
		}
		if h.then == thenUnsecret && m.cur < len(cur) && cur[m.cur].IsSecret() {
			cur[m.cur].Type = model.VarDefault
			cur[m.cur].SecretRef = ""
			if k, ok := m.saveTarget(); ok {
				return m.touch(k)
			}
		}
	}
	return nil
}

func short(err error) string {
	s := err.Error()
	if len(s) > 80 {
		s = s[:80]
	}
	return s
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
