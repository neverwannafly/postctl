package wspane

import (
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/shubham/postctl/internal/model"
	"github.com/shubham/postctl/internal/tui"
	"github.com/shubham/postctl/internal/tui/vimfield"
	"github.com/shubham/postctl/internal/ui"
)

// tabID identifies a tab.
type tabID int

// Tabs, in display order.
const (
	tabMessages tabID = iota
	tabParams
	tabHeaders
	tabAuth
	tabSettings
	numTabs
)

var tabNames = [numTabs]string{"Messages", "Params", "Headers", "Auth", "Settings"}

// zone is which part of the pane has the cursor.
type zone int

const (
	zBar zone = iota
	zTabs
	zBody
)

// section is which part of the Messages tab has the cursor.
type section int

const (
	sComposer section = iota
	sSaved
	sLog
)

// Pane is the WebSocket request editor. Use [New] or [Factory].
type Pane struct {
	th   ui.Theme
	env  tui.PaneEnv
	deps Deps
	w, h int

	focused  bool
	enhanced bool // the terminal reports Ctrl-Enter

	ws        *model.Workspace
	activeEnv *model.Environment

	ref   tui.RequestRef
	req   *model.Request // the live request owned by the shell's workspace
	coll  *model.Collection
	chain []model.Container
	sc    *scope // preview scope: secrets masked
	scNum *scope // validation scope: secrets replaced by 0

	zone   zone
	tab    tabID
	sect   section
	url    *vimfield.Field
	urlE   bool // the URL field has input focus
	mouse  vimfield.Mouse
	mouseX int
	mouseY int

	params, headers *kvView
	auth            *authView
	settings        *settingsView

	comp     *vimfield.Field
	compLang string // the lexer comp was built with: "" or "json"
	compErr  string // why the composer's message cannot be sent, or ""
	savedCur int
	savedTop int
	rename   *vimfield.Field
	renameID model.ID

	searching bool
	search    *vimfield.Field
	detail    *detailState

	confirm *confirmState

	// editorReq is the open external-editor round trip, if any.
	editorReq *editorIntent
	lives     map[model.ID]*live
	saves     map[model.ID]*saveState

	notice     string
	noticeWarn bool
	noticeAt   time.Time
}

// New builds the pane.
func New(env tui.PaneEnv, d Deps) *Pane {
	p := &Pane{
		th: env.Theme, env: env, deps: d,
		zone:     zTabs,
		tab:      tabMessages,
		params:   newKVView(kvParams),
		headers:  newKVView(kvHeaders),
		auth:     newAuthView(),
		settings: newSettingsView(),
		lives:    map[model.ID]*live{},
		saves:    map[model.ID]*saveState{},
	}
	p.url = p.newField(fieldSpec{single: true, placeholder: "Enter a ws:// or wss:// URL"})
	p.search = p.newField(fieldSpec{single: true, plain: true, placeholder: "search messages"})
	p.buildComposer("")
	return p
}

// buildComposer creates the composer field for the current format (JSON
// highlighting for JSON messages) holding text.
func (p *Pane) buildComposer(text string) {
	p.compLang = ""
	if p.req != nil && p.format() == model.WSJSON {
		p.compLang = "json"
	}
	p.comp = p.newField(fieldSpec{lang: p.compLang, placeholder: "Type a message. Ctrl-S sends it."})
	p.comp.Set(text)
	p.fitComposer()
}

// syncComposer rebuilds the composer when the message format changed the
// lexer it needs, keeping its text.
func (p *Pane) syncComposer() {
	want := ""
	if p.req != nil && p.format() == model.WSJSON {
		want = "json"
	}
	if want != p.compLang {
		p.buildComposer(p.comp.Text())
	}
}

// applyTheme rebuilds the fields, which draw with the theme they were built
// with, keeping their text.
func (p *Pane) applyTheme() {
	text := p.url.Text()
	p.url = p.newField(fieldSpec{single: true, placeholder: "Enter a ws:// or wss:// URL"})
	p.url.Set(text)
	p.buildComposer(p.comp.Text())
	query := p.search.Text()
	p.search = p.newField(fieldSpec{single: true, plain: true, placeholder: "search messages"})
	p.search.Set(query)
	if p.rename != nil {
		name := p.rename.Text()
		p.rename = p.newField(fieldSpec{single: true, plain: true, placeholder: "name"})
		p.rename.Set(name)
	}
	p.stopEditing()
	if p.zone == zBar {
		p.zone = zTabs
	}
	p.fitURL()
}

// urlWidth is the width of the URL field in the bar.
func (p *Pane) urlWidth() int {
	return max(4, p.w-2-badgeW-2-ui.Width(p.actionLabel())-1)
}

// fitURL sizes the URL field. Sizes follow the cursor in Update paths only.
func (p *Pane) fitURL() { p.url.Fit(p.urlWidth(), 1) }

// fitComposer sizes the composer to the area the Messages tab draws it in.
func (p *Pane) fitComposer() {
	w, h := p.bodySize()
	g := msgLayout(w, h, len(p.messages()))
	p.comp.Fit(g.compW, max(1, g.compH-1))
}

// Title implements tui.Titled.
func (p *Pane) Title() string {
	if p.req == nil {
		return "WebSocket"
	}
	name := p.req.Name
	if name == "" {
		name = "Untitled"
	}
	return "WebSocket " + p.th.Glyphs.Dot + " " + name
}

// editable reports whether there is a WebSocket request to edit.
func (p *Pane) editable() bool {
	return p.req != nil && p.ref.Problem == nil && p.req.Kind == model.KindWebSocket
}

// Editing implements tui.Editor: while a text field has focus the shell sends
// every key here.
func (p *Pane) Editing() bool {
	if !p.editable() {
		return false
	}
	if p.urlE || p.confirm != nil || p.searching || p.rename != nil {
		return true
	}
	if p.zone != zBody {
		return false
	}
	switch p.tab {
	case tabMessages:
		return p.sect == sComposer
	case tabParams:
		return p.params.editing()
	case tabHeaders:
		return p.headers.editing()
	case tabAuth:
		return p.auth.editing()
	default:
		return p.settings.editing()
	}
}

// SetSize implements tui.Pane.
func (p *Pane) SetSize(w, h int) {
	p.w, p.h = w, h
	p.fitURL()
	p.fitComposer()
	p.fitPrompts()
	p.rebuildDetail()
}

// fitPrompts sizes the log search and the template name field. Like the other
// fits it runs in Update paths only.
func (p *Pane) fitPrompts() {
	w, h := p.bodySize()
	g := msgLayout(w, h, len(p.messages()))
	p.search.Fit(max(4, g.logW-2), 1)
	if p.rename != nil {
		p.rename.Fit(renameWidth(g.savedW), 1)
	}
}

// renameWidth is the width of the template name column in the saved list.
func renameWidth(savedW int) int { return min(max(8, savedW/3), 28) }

// Focus implements tui.Pane: the field that was being edited gets the
// keyboard back, in the mode it was left in.
func (p *Pane) Focus() tea.Cmd {
	p.focused = true
	p.syncFocus()
	if f := p.activeField(); f != nil {
		f.Focus()
	}
	return nil
}

// Blur implements tui.Pane. The field stops drawing its cursor; the shell
// writes unsaved edits through BlurCmd.
func (p *Pane) Blur() {
	p.focused = false
	p.syncFocus()
	if f := p.activeField(); f != nil {
		f.Blur()
	}
}

// Update implements tui.Pane.
func (p *Pane) Update(msg tea.Msg) (tui.Pane, tea.Cmd) {
	switch m := msg.(type) {
	case tui.ThemeChangedMsg:
		p.th = m.Theme
		p.applyTheme()
		p.rebuildDetail()
	case tui.EnvironmentChangedMsg:
		p.activeEnv = m.Env
		p.rebuildScope()
	case tui.WorkspaceReloadedMsg:
		p.ws = m.Workspace
		p.rebuildScope()
	case tui.SelectionMsg:
		return p, p.onSelection(m)
	case tea.KeyboardEnhancementsMsg:
		p.enhanced = m.SupportsKeyDisambiguation()
	case tui.FocusSectionMsg:
		return p, p.onFocusSection(m)
	case tui.SectionStepMsg:
		return p, p.onSectionStep(m)
	case tea.KeyPressMsg:
		return p, p.onKey(m)
	case tea.PasteMsg:
		return p, p.onPaste(m)
	case editorDoneMsg:
		return p, p.onEditorDone(m)
	case autosaveMsg:
		return p, p.onAutosave(m)
	case tui.SaveDoneMsg:
		return p, p.onSaveDone(m)
	case connectedMsg:
		return p, p.onConnected(m)
	case eventsMsg:
		return p, p.onEvents(m)
	case sentMsg:
		return p, p.onSent(m)
	case pingedMsg:
		return p, p.onPinged(m)
	case tickMsg:
		return p, p.onTick(m)
	case noticeExpireMsg:
		if p.noticeAt.Equal(time.Time(m)) {
			p.notice = ""
		}
	}
	return p, nil
}

// noticeExpireMsg clears a status-row notice.
type noticeExpireMsg time.Time

// setNotice reports an action's result. The message goes to the shell toast,
// which is drawn whichever pane has focus (a narrow layout shows only one
// pane), and stays on the status row for a few seconds. warn styles it as a
// warning. The text carries no values.
func (p *Pane) setNotice(text string, warn bool) tea.Cmd {
	p.notice, p.noticeWarn = text, warn
	p.noticeAt = p.deps.now()
	kind := ui.ToastInfo
	if warn {
		kind = ui.ToastWarning
	}
	return tea.Batch(
		p.deps.after(4*time.Second, noticeExpireMsg(p.noticeAt)),
		msgCmd(tui.ToastMsg{Kind: kind, Text: text}),
	)
}

// ---- binding to the selected request -------------------------------------------

// onSelection binds the pane to the selected request. The shell sends the
// message again after a workspace reload (pointers change), which rebinds the
// same request without losing unsaved edits.
func (p *Pane) onSelection(m tui.SelectionMsg) tea.Cmd {
	ref := m.Ref
	old := p.req
	if old != nil && ref.Request != nil && old.ID == ref.Request.ID {
		p.rebind(ref)
		return nil
	}
	var cmd tea.Cmd
	if old != nil {
		cmd = p.flush(old.ID)
	}
	p.stopEditing()
	p.ref = ref
	p.req = ref.Request
	p.coll = ref.Collection
	p.zone, p.tab, p.sect = zTabs, tabMessages, sComposer
	p.savedCur, p.savedTop = 0, 0
	p.searching, p.rename, p.detail, p.confirm = false, nil, nil, nil
	p.notice = ""
	p.refreshChain()
	p.reloadAll()
	if m.Open && p.req != nil && p.req.URL.Raw == "" {
		// A brand-new request: start at the URL, typing.
		p.setZone(zBar)
		p.url.Begin("i")
		p.syncFocus()
	}
	return cmd
}

// rebind swaps in a fresh pointer for the same request after a reload.
// Unsaved edits win over what the reload found on disk.
func (p *Pane) rebind(ref tui.RequestRef) {
	if ref.Request != p.req && p.dirtyFor(p.req.ID) {
		*ref.Request = *p.req.Clone()
	}
	changed := ref.Request != p.req
	p.ref = ref
	p.req = ref.Request
	p.coll = ref.Collection
	p.refreshChain()
	if changed && !p.Editing() {
		p.reloadAll()
	}
}

func (p *Pane) refreshChain() {
	p.chain = nil
	if p.coll != nil && p.req != nil {
		p.chain = chainOf(p.coll, p.req.ID)
	}
	p.rebuildScope()
}

// rebuildScope rebuilds the variable scopes after the environment, workspace
// or a variable changed.
func (p *Pane) rebuildScope() {
	p.sc = newScope(p.ws, p.chain, p.activeEnv, p.th.Glyphs.Mask)
	p.scNum = newScope(p.ws, p.chain, p.activeEnv, "0")
	p.revalidate()
}

// stopEditing leaves every edit mode.
func (p *Pane) stopEditing() {
	p.urlE = false
	p.url.Blur()
	p.params.stop()
	p.headers.stop()
	p.auth.stop()
	p.settings.stop()
	p.comp.Blur()
	// Leaving for another section ends the prompts: a search keeps its
	// results, a rename is cancelled.
	p.searching = false
	p.search.Blur()
	p.rename = nil
}

// reloadAll refreshes every widget from the request.
func (p *Pane) reloadAll() {
	if p.req == nil {
		p.url.Set("")
		p.comp.Set("")
		return
	}
	p.url.Set(p.req.URL.Raw)
	p.fitURL()
	p.params.reload(p)
	p.headers.reload(p)
	p.auth.reload()
	p.settings.reload()
	p.syncComposer()
	p.comp.Set(p.draft().Body)
	p.fitComposer()
	p.revalidate()
	p.clampSaved()
}

// syncURL refreshes the URL field after a Params edit changed the URL.
func (p *Pane) syncURL() {
	if p.req != nil && !p.urlE {
		p.url.Set(p.req.URL.Raw)
	}
}

// ---- model helpers ---------------------------------------------------------------

// ensureWS returns the request's WebSocket settings, creating them.
func (p *Pane) ensureWS() *model.WebSocketSettings {
	if p.req.WebSocket == nil {
		p.req.WebSocket = &model.WebSocketSettings{}
	}
	return p.req.WebSocket
}

// trimWS drops an empty WebSocket section so untouched requests stay small.
func (p *Pane) trimWS() {
	if p.req != nil && p.req.WebSocket != nil && p.req.WebSocket.Empty() {
		p.req.WebSocket = nil
	}
}

// messages are the saved message templates.
func (p *Pane) messages() []model.WSMessage {
	if p.req == nil || p.req.WebSocket == nil {
		return nil
	}
	return p.req.WebSocket.Messages
}

// draft is the composer content stored in the request.
func (p *Pane) draft() model.WSMessage {
	if p.req == nil || p.req.WebSocket == nil || p.req.WebSocket.Draft == nil {
		return model.WSMessage{}
	}
	return *p.req.WebSocket.Draft
}

func (p *Pane) setDraft(m model.WSMessage) {
	if m.Body == "" && (m.Format == "" || m.Format == model.WSText) {
		if p.req.WebSocket != nil {
			p.req.WebSocket.Draft = nil
		}
		return
	}
	m.ID, m.Name = "", ""
	p.ensureWS().Draft = &m
}

// format is the composer's format, never empty.
func (p *Pane) format() model.WSFormat {
	if f := p.draft().Format; f != "" {
		return f
	}
	return model.WSText
}
