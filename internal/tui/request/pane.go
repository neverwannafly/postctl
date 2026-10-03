package request

import (
	"github.com/shubham/postctl/internal/tui/textin"
	"os"
	"os/exec"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/shubham/postctl/internal/model"
	"github.com/shubham/postctl/internal/oauth2"
	"github.com/shubham/postctl/internal/tui"
	"github.com/shubham/postctl/internal/ui"
)

// Deps are the effects the pane performs, injectable for tests. The zero
// value selects the real implementations.
type Deps struct {
	// After returns a command that delivers msg after d. Tests replace it to
	// drive the autosave debounce and progress ticks with a fake clock.
	After func(d time.Duration, msg tea.Msg) tea.Cmd
	// Now is the clock; time.Now when nil.
	Now func() time.Time
	// Debounce is the autosave delay after the last edit; 500 ms when zero.
	Debounce time.Duration
	// Exec runs an external program with the terminal handed over
	// (tea.ExecProcess). Tests replace it with a function that runs the
	// program synchronously.
	Exec func(cmd *exec.Cmd, done tea.ExecCallback) tea.Cmd
	// EditorCommand builds the editor command for a file; the default honours
	// $VISUAL, then $EDITOR, then vi.
	EditorCommand func(path string) *exec.Cmd
	// TempDir holds temporary files for $EDITOR round trips; os.TempDir when
	// empty.
	TempDir string
	// Clipboard puts text on the system clipboard; tea.SetClipboard when nil.
	Clipboard func(text string) tea.Cmd
	// Fetch obtains an OAuth 2.0 token; oauth2.Fetch when nil.
	Fetch oauth2.FetchFunc
	// Browser opens the authorization URL; oauth2.SystemBrowser when nil.
	Browser oauth2.Browser
	// Tokens stores fetched tokens in the secret store. Without it "Get new
	// token" explains that tokens cannot be saved.
	Tokens *oauth2.Source
}

func (d Deps) after(dur time.Duration, msg tea.Msg) tea.Cmd {
	if d.After != nil {
		return d.After(dur, msg)
	}
	return tea.Tick(dur, func(time.Time) tea.Msg { return msg })
}

func (d Deps) now() time.Time {
	if d.Now != nil {
		return d.Now()
	}
	return time.Now()
}

func (d Deps) debounce() time.Duration {
	if d.Debounce > 0 {
		return d.Debounce
	}
	return 500 * time.Millisecond
}

func (d Deps) clip(text string) tea.Cmd {
	if d.Clipboard != nil {
		return d.Clipboard(text)
	}
	return tea.SetClipboard(text)
}

func (d Deps) exec(cmd *exec.Cmd, done tea.ExecCallback) tea.Cmd {
	if d.Exec != nil {
		return d.Exec(cmd, done)
	}
	return tea.ExecProcess(cmd, done)
}

func (d Deps) editorCmd(path string) *exec.Cmd {
	if d.EditorCommand != nil {
		return d.EditorCommand(path)
	}
	ed := os.Getenv("VISUAL")
	if strings.TrimSpace(ed) == "" {
		ed = os.Getenv("EDITOR")
	}
	if strings.TrimSpace(ed) == "" {
		ed = "vi"
	}
	// $EDITOR may carry arguments ("code --wait"), so split it like a shell
	// would (without expansion).
	words, err := shellWords(ed)
	if err != nil || len(words) == 0 {
		words = []string{"vi"}
	}
	return exec.Command(words[0], append(words[1:], path)...) //nolint:gosec // the user's own editor command
}

// Factory returns the pane factory for tui.Options.NewRequestPane.
func Factory(d Deps) tui.PaneFactory {
	return func(env tui.PaneEnv) tui.Pane { return New(env, d) }
}

// tabID identifies a tab.
type tabID int

// Tabs, in display order.
const (
	tabParams tabID = iota
	tabAuth
	tabHeaders
	tabBody
	tabPre
	tabTests
	tabSettings
	numTabs
)

var (
	tabNames = [numTabs]string{"Params", "Auth", "Headers", "Body", "Pre-request", "Tests", "Settings"}
	tabShort = [numTabs]string{"Params", "Auth", "Headers", "Body", "Pre", "Tests", "Settings"}
)

// zone is which part of the pane has the cursor.
type zone int

const (
	zBar zone = iota
	zTabs
	zBody
)

// tabView is one tab's content and editing behaviour.
type tabView interface {
	// reload rebuilds widget state from the request (after selection,
	// undo or an external reload).
	reload(p *Pane)
	// view renders the content in w x h.
	view(p *Pane, w, h int) string
	// key handles a key press in the body zone. changed reports a model edit.
	key(p *Pane, k tea.KeyPressMsg) (handled, changed bool, cmd tea.Cmd)
	// paste handles pasted text while editing.
	paste(p *Pane, m tea.PasteMsg) (changed bool)
	// editing reports whether a text field has input focus.
	editing() bool
	// stop leaves any edit mode.
	stop()
	// enter is called when the cursor arrives from the tab bar.
	enter(p *Pane)
	// atTop reports whether Up should leave the content for the tab bar.
	atTop() bool
	// hints are the tab's footer hints.
	hints(p *Pane) []ui.Hint
}

// Pane is the request editor. Use [New] or [Factory].
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
	sc    *scope

	zone zone
	tab  tabID
	url  *textin.Input
	urlE bool // the URL field has input focus
	msel *methodSel

	views [numTabs]tabView

	confirm *importOffer

	hist  map[model.ID]*history
	saves map[model.ID]*saveState

	send sendState

	notice   string // a short quiet message on the status row
	noticeAt time.Time

	editorReq *editorIntent

	oauth    *oauthRun
	oauthErr string
}

// New builds the pane.
func New(env tui.PaneEnv, d Deps) *Pane {
	p := &Pane{
		th: env.Theme, env: env, deps: d,
		zone:  zTabs,
		tab:   tabParams,
		hist:  map[model.ID]*history{},
		saves: map[model.ID]*saveState{},
	}
	p.url = textin.New("")
	p.url.Placeholder = "Enter request URL"
	p.views = [numTabs]tabView{
		tabParams:   newKVView(kindParams),
		tabAuth:     newAuthView(),
		tabHeaders:  newKVView(kindHeaders),
		tabBody:     newBodyView(env.Theme),
		tabPre:      newScriptView(true, env.Theme),
		tabTests:    newScriptView(false, env.Theme),
		tabSettings: newSettingsView(),
	}
	p.applyTheme()
	return p
}

func (p *Pane) applyTheme() {
	p.url.Highlight = func(s string) []textin.Span { return p.sc.highlight(s) }
	for _, v := range p.views {
		if t, ok := v.(interface{ setTheme(ui.Theme) }); ok {
			t.setTheme(p.th)
		}
	}
}

// Title implements tui.Titled.
func (p *Pane) Title() string {
	if p.req == nil {
		return "Request"
	}
	name := p.req.Name
	if name == "" {
		name = "Untitled"
	}
	return "Request " + p.th.Glyphs.Dot + " " + name
}

// Editing implements tui.Editor: while a text field has focus the shell sends
// every key here.
func (p *Pane) Editing() bool {
	if p.req == nil {
		return false
	}
	return p.urlE || p.msel != nil || p.confirm != nil || p.views[p.tab].editing()
}

// SetSize implements tui.Pane.
func (p *Pane) SetSize(w, h int) { p.w, p.h = w, h }

// Focus implements tui.Pane.
func (p *Pane) Focus() tea.Cmd {
	p.focused = true
	return nil
}

// Blur implements tui.Pane: unsaved edits are written at once.
func (p *Pane) Blur() {
	p.focused = false
}

// hasRequest reports whether a request is open for editing.

// Update implements tui.Pane.
func (p *Pane) Update(msg tea.Msg) (tui.Pane, tea.Cmd) {
	switch m := msg.(type) {
	case tui.ThemeChangedMsg:
		p.th = m.Theme
		p.applyTheme()
		return p, nil
	case tui.EnvironmentChangedMsg:
		p.activeEnv = m.Env
		p.rebuildScope()
		return p, nil
	case tui.WorkspaceReloadedMsg:
		return p, p.onReloaded(m.Workspace)
	case tui.SelectionMsg:
		return p, p.onSelection(m)
	case tea.KeyboardEnhancementsMsg:
		p.enhanced = m.SupportsKeyDisambiguation()
		return p, nil
	case tui.FocusSectionMsg:
		return p, p.onFocusSection(m)
	case tui.SectionStepMsg:
		return p, p.onSectionStep(m)
	case tui.CancelOAuthMsg:
		return p, p.cancelOAuth()
	case tea.KeyPressMsg:
		return p, p.onKey(m)
	case tea.PasteMsg:
		return p, p.onPaste(m)
	case autosaveMsg:
		return p, p.onAutosave(m)
	case tui.SaveDoneMsg:
		return p, p.onSaveDone(m)
	case tui.SendStartedMsg:
		return p, p.onSendStarted(m)
	case tui.SendProgressMsg:
		p.onSendProgress(m)
		return p, nil
	case tui.SendResultMsg:
		return p, p.onSendResult(m)
	case sendTickMsg:
		return p, p.onSendTick(m)
	case editorDoneMsg:
		return p, p.onEditorDone(m)
	case oauthEventsMsg:
		return p, p.onOAuthEvents(m)
	case oauthSavedMsg:
		return p, p.onOAuthSaved(m)
	case curlDoneMsg:
		return p, p.onCurlDone(m)
	case noticeExpireMsg:
		if p.noticeAt.Equal(time.Time(m)) {
			p.notice = ""
		}
		return p, nil
	}
	return p, nil
}

// setNotice shows a short message on the status row for a few seconds.
func (p *Pane) setNotice(text string) tea.Cmd {
	p.notice = text
	p.noticeAt = p.deps.now()
	at := p.noticeAt
	return p.deps.after(4*time.Second, noticeExpireMsg(at))
}

type noticeExpireMsg time.Time
