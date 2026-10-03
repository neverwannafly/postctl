package wspane

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/shubham/postctl/internal/model"
	"github.com/shubham/postctl/internal/tui"
)

// opener puts the cursor in one kind of field, in normal mode.
type opener struct {
	name string
	open func(h *harness)
}

func fieldOpeners() []opener {
	return []opener{
		{"url", func(h *harness) { h.key("up") }},
		{"composer", func(h *harness) { h.key("down") }},
		{"params cell", func(h *harness) { h.gotoTab(tabParams); h.key("down", "enter") }},
		{"header cell", func(h *harness) { h.gotoTab(tabHeaders); h.key("down", "enter") }},
		{"auth row", func(h *harness) {
			h.req.Auth = &model.Auth{Type: model.AuthBearer, Bearer: &model.BearerAuth{}}
			h.gotoTab(tabAuth)
			h.key("down", "down", "enter")
		}},
		{"settings row", func(h *harness) { h.gotoTab(tabSettings); h.key("down", "enter") }},
	}
}

func (h *harness) forwarded() (keys, seqs []string) {
	for _, m := range h.out {
		switch v := m.(type) {
		case tui.ForwardKeyMsg:
			keys = append(keys, v.Key.String())
		case tui.ForwardSeqMsg:
			seqs = append(seqs, v.Seq)
		}
	}
	return
}

// In normal mode Space, ":", "q" and g+letter belong to the shell.
func TestNormalModeKeysReachTheShellFromEveryField(t *testing.T) {
	for _, o := range fieldOpeners() {
		t.Run(o.name, func(t *testing.T) {
			h := newHarness(t)
			o.open(h)
			if got := h.p.VimMode(); got != "NORMAL" {
				t.Fatalf("mode after opening = %q", got)
			}
			h.out = nil
			h.key("space", ":", "q", "g", "b")
			keys, seqs := h.forwarded()
			if strings.Join(keys, ",") != "space,:,q" || strings.Join(seqs, ",") != "gb" {
				t.Fatalf("forwarded keys %v seqs %v", keys, seqs)
			}
		})
	}
}

// In insert mode the same keys are text.
func TestInsertModeKeysAreTextInEveryField(t *testing.T) {
	for _, o := range fieldOpeners() {
		t.Run(o.name, func(t *testing.T) {
			h := newHarness(t)
			o.open(h)
			h.key("i")
			if got := h.p.VimMode(); got != "INSERT" {
				t.Fatalf("mode after i = %q", got)
			}
			h.out = nil
			h.key("space", ":", "q")
			if keys, seqs := h.forwarded(); len(keys)+len(seqs) != 0 {
				t.Fatalf("insert mode forwarded %v %v", keys, seqs)
			}
			if f := h.p.activeField(); f == nil || !strings.Contains(f.Text(), " :q") {
				t.Fatal("typed text is not in the field")
			}
		})
	}
}

func TestVimModeFollowsTheField(t *testing.T) {
	h := newHarness(t)
	if h.p.VimMode() != "" {
		t.Fatalf("tab bar mode = %q, want none", h.p.VimMode())
	}
	h.key("up")
	if h.p.VimMode() != "NORMAL" {
		t.Fatalf("url mode = %q", h.p.VimMode())
	}
	h.key("i")
	if h.p.VimMode() != "INSERT" {
		t.Fatalf("after i = %q", h.p.VimMode())
	}
	h.key("esc", "v")
	if h.p.VimMode() != "VISUAL" {
		t.Fatalf("after v = %q", h.p.VimMode())
	}
}

func TestCtrlSSendsTheComposerInEveryMode(t *testing.T) {
	h := newHarness(t)
	c := h.connect()
	h.key("down", "i")
	h.typ("hi")
	h.key("ctrl+s") // insert
	h.key("esc", "ctrl+s")
	h.key("v", "ctrl+s")
	h.waitLog(5)
	if got := c.sentTexts(); len(got) != 3 {
		t.Fatalf("sent %d messages, want 3 (insert, normal, visual)", len(got))
	}
}

func TestEnterInTheURLBarConnectsInBothModes(t *testing.T) {
	h := newHarness(t)
	h.key("up", "enter") // normal: connect
	h.waitState(stOpen)
	h.key("i", "enter") // insert: disconnect
	h.waitState(stIdle)
	if strings.Contains(h.p.url.Text(), "\n") {
		t.Fatal("Enter added a line to the URL")
	}
}

// Esc: insert to normal, normal to leaving the field, then cancel connecting.
func TestEscLadderEndsAtCancellingTheConnection(t *testing.T) {
	h := newHarness(t)
	h.gate = make(chan struct{})
	h.key("ctrl+s")
	h.waitState(stConnecting)
	h.key("up", "i")
	h.key("esc")
	if h.p.VimMode() != "NORMAL" || h.p.state() != stConnecting {
		t.Fatalf("first Esc: mode %q state %v", h.p.VimMode(), h.p.state())
	}
	h.key("esc")
	if h.p.Editing() || h.p.state() != stConnecting {
		t.Fatalf("second Esc: editing %v state %v", h.p.Editing(), h.p.state())
	}
	h.key("esc")
	if h.p.state() == stConnecting {
		t.Fatal("third Esc should cancel connecting")
	}
	close(h.gate)
	h.settle()
}

func TestComposerHighlightsJSONOnlyForJSONMessages(t *testing.T) {
	h := newHarness(t)
	if h.p.compLang != "" {
		t.Fatalf("text composer lexer = %q", h.p.compLang)
	}
	h.key("down", "ctrl+t")
	if h.p.compLang != "json" {
		t.Fatalf("JSON composer lexer = %q", h.p.compLang)
	}
	h.key("i")
	h.typ(`{"a": 1}`)
	if h.p.comp.Text() != `{"a": 1}` {
		t.Fatal("rebuilding the lexer lost the text")
	}
	h.key("esc", "esc", "esc") // leave the composer, back to the tab bar
	h.key("down", "ctrl+t", "ctrl+t")
	if h.p.compLang != "" || h.p.comp.Text() != `{"a": 1}` {
		t.Fatalf("lexer %q text %q", h.p.compLang, h.p.comp.Text())
	}
}

func TestUndoInsideTheComposerIsVimUndo(t *testing.T) {
	h := newHarness(t)
	h.key("down", "i")
	h.typ("hello")
	h.key("esc", "u")
	if h.p.comp.Text() != "" || h.p.draft().Body != "" {
		t.Fatalf("composer %q draft %q", h.p.comp.Text(), h.p.draft().Body)
	}
}

func TestVariableCompletionMasksSecretsInTheFrame(t *testing.T) {
	h := newHarness(t)
	h.key("down", "i")
	h.typ("{{to")
	if !h.p.comp.Completing() {
		t.Fatal("typing {{ should open the completion menu")
	}
	v := h.view()
	if !strings.Contains(v, "token") || !strings.Contains(v, h.theme.Glyphs.Mask) {
		t.Fatalf("menu does not show the masked variable:\n%s", v)
	}
	if strings.Contains(v, "s3cr3t-value") {
		t.Fatal("the secret value is on screen")
	}
}

func TestURLCompletionMenuDrawsUnderTheBar(t *testing.T) {
	h := newHarness(t)
	h.key("up", "A")
	h.typ("{{to")
	if !h.p.url.Completing() {
		t.Fatal("typing {{ should open the completion menu")
	}
	v := h.view()
	if !strings.Contains(v, "token") || strings.Contains(v, "s3cr3t-value") {
		t.Fatalf("URL menu missing or leaking:\n%s", v)
	}
}

func TestViewIsPureForEveryField(t *testing.T) {
	for _, o := range fieldOpeners() {
		t.Run(o.name, func(t *testing.T) {
			h := newHarness(t)
			o.open(h)
			h.key("i")
			h.typ("{{to")
			f := h.p.activeField()
			text := f.Text()
			r0, c0 := f.Cursor()
			first, second := h.p.View(), h.p.View()
			r1, c1 := f.Cursor()
			if first != second || text != f.Text() || r0 != r1 || c0 != c1 {
				t.Fatal("View changed the field")
			}
		})
	}
}

func TestPasteInNormalModeInsertsIntoTheURL(t *testing.T) {
	h := newHarness(t)
	h.key("up")
	h.paste("/extra")
	if !strings.Contains(h.req.URL.Raw, "/extra") {
		t.Fatal("a paste in normal mode should insert")
	}
}

// Connecting flushes the pending autosave with the connection attempt.
func TestConnectFlushesPendingAutosave(t *testing.T) {
	h := newHarness(t)
	h.key("up", "A")
	h.typ("zq")
	h.out = nil
	h.key("ctrl+s")
	if !strings.HasSuffix(h.req.URL.Raw, "zq") {
		t.Fatal("the edit is not in the request")
	}
	if got := h.saves(); len(got) != 1 || got[0].ID != h.req.ID {
		t.Fatalf("saves emitted with the connect = %d, want 1 for this request", len(got))
	}
}

// Same rule as the request pane: only a pure {{reference}} is shown.
func TestSecretRowMasksEverythingButAPureReference(t *testing.T) {
	r := formRow{secret: true}
	f := &form{}
	for _, c := range []struct {
		text string
		want bool
	}{
		{"{{token}}", false},
		{" {{ token }} ", false},
		{"ab{{cd", true},
		{"Bearer {{token}}", true},
		{"plain-literal", true},
	} {
		if got := f.maskFor(r, c.text); got != c.want {
			t.Errorf("maskFor(%d chars) = %v, want %v", len(c.text), got, c.want)
		}
	}
}

// ---- $EDITOR ---------------------------------------------------------------------------

func editorDeps(script string) option {
	return func(h *harness) {
		h.deps.EditorCommand = func(path string) *exec.Cmd {
			return exec.Command("sh", "-c", script, "sh", path)
		}
		h.deps.Exec = func(cmd *exec.Cmd, done tea.ExecCallback) tea.Cmd {
			return func() tea.Msg { return done(cmd.Run()) }
		}
	}
}

func TestEditorRoundTripFromEveryTextField(t *testing.T) {
	const edited = "edited-synthetic"
	cases := []struct {
		name string
		open func(h *harness)
		got  func(h *harness) string
	}{
		{"url", func(h *harness) { h.key("up") }, func(h *harness) string { return h.req.URL.Raw }},
		{"composer", func(h *harness) { h.key("down") }, func(h *harness) string { return h.p.comp.Text() }},
		{"params cell", func(h *harness) { h.gotoTab(tabParams); h.key("down", "enter") }, func(h *harness) string {
			return h.req.URL.Query[0].Key + "|" + h.req.URL.Query[0].Value
		}},
		{"header cell", func(h *harness) { h.gotoTab(tabHeaders); h.key("down", "enter") }, func(h *harness) string {
			return h.req.Headers[0].Key + "|" + h.req.Headers[0].Value
		}},
		{"auth row", func(h *harness) {
			h.req.Auth = &model.Auth{Type: model.AuthBasic, Basic: &model.BasicAuth{Username: "u", Password: "p"}}
			h.p.reloadAll()
			h.gotoTab(tabAuth)
			h.key("down", "down", "enter")
		}, func(h *harness) string { return h.req.Auth.Basic.Username }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := newHarness(t, editorDeps(`printf 'edited-synthetic\n' > "$1"`))
			c.open(h)
			if h.p.VimMode() != "NORMAL" {
				t.Fatalf("mode before the editor = %q", h.p.VimMode())
			}
			h.out = nil
			h.editor()
			if got := c.got(h); !strings.Contains(got, edited) {
				t.Fatalf("the editor's text did not arrive in the %s", c.name)
			}
			if len(h.saves()) == 0 && !h.touchedDirty() {
				t.Fatal("the edit was not marked for autosave")
			}
		})
	}
}

// editor runs "Edit in $EDITOR" and waits for the editor process to come back.
func (h *harness) editor() {
	h.t.Helper()
	h.runAction("ws.editor")
	for deadline := time.Now().Add(5 * time.Second); h.p.editorReq != nil && time.Now().Before(deadline); {
		time.Sleep(5 * time.Millisecond)
		h.settle()
	}
	if h.p.editorReq != nil {
		h.t.Fatal("the editor never came back")
	}
}

// touchedDirty reports whether an unsaved marker was emitted.
func (h *harness) touchedDirty() bool {
	for _, m := range h.out {
		if d, ok := m.(tui.DirtyMsg); ok && d.Dirty {
			return true
		}
	}
	return false
}

func TestEditorKeepsSingleLineFieldsOnOneLine(t *testing.T) {
	h := newHarness(t, editorDeps(`printf 'one\ntwo\n' > "$1"`))
	h.key("up")
	h.editor()
	if strings.Contains(h.req.URL.Raw, "\n") || !strings.Contains(h.req.URL.Raw, "one two") {
		t.Fatal("the URL must stay on one line")
	}
	if h.p.zone != zBar || h.p.VimMode() != "NORMAL" {
		t.Fatalf("back in the URL field expected: zone %v mode %q", h.p.zone, h.p.VimMode())
	}
}

func TestEditorRefusesAHiddenSecretAndTempFileIsPrivate(t *testing.T) {
	out := filepath.Join(t.TempDir(), "mode")
	h := newHarness(t, editorDeps(`stat -f %Lp "$1" > `+out+`; printf 'x\n' > "$1"`))
	h.req.Auth = &model.Auth{Type: model.AuthBearer, Bearer: &model.BearerAuth{Token: "hidden-literal"}}
	h.p.reloadAll()
	h.gotoTab(tabAuth)
	h.key("down", "down", "enter") // the token row, hidden
	h.editor()
	if h.req.Auth.Bearer.Token != "hidden-literal" {
		t.Fatal("a hidden secret went to the editor")
	}
	if _, err := os.Stat(out); err == nil {
		t.Fatal("the editor ran for a hidden secret")
	}
	h.mustToast("Reveal the secret first")

	h.key("esc", "esc")
	h.key("up")
	h.editor()
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("the editor never ran: %v", err)
	}
	if strings.TrimSpace(string(got)) != "600" {
		t.Fatalf("temp file mode = %s, want 600", strings.TrimSpace(string(got)))
	}
}

// Space o reaches the pane through the shell's leader table.
func TestLeaderOpensTheEditorForAWebSocketRequest(t *testing.T) {
	h := newHarness(t)
	found := false
	for _, a := range h.p.Actions() {
		if a.ID == "ws.editor" {
			found = a.Keys == "Space o" && a.Enabled != nil && a.Enabled()
		}
	}
	if !found {
		t.Fatal("ws.editor is not offered")
	}
}

// ---- log search and template name prompts -------------------------------------------------

func (h *harness) openSearch() {
	h.t.Helper()
	c := h.connect()
	c.pushIn(`{"symbol":"BTC","price":101}`)
	c.pushIn(`{"symbol":"ETH","price":9}`)
	h.waitLog(3)
	h.key("down", "tab", "tab", "/")
}

func TestSearchPromptIsAModalEditor(t *testing.T) {
	h := newHarness(t)
	h.openSearch()
	if !h.p.searching || h.p.VimMode() != "INSERT" {
		t.Fatalf("searching=%v mode=%q", h.p.searching, h.p.VimMode())
	}
	h.typ("a b:q")
	if h.p.cur().log.query != "a b:q" {
		t.Fatal("Space, ':' and 'q' are text in insert mode")
	}
	h.key("esc")
	if !h.p.searching || h.p.VimMode() != "NORMAL" {
		t.Fatalf("first Esc must only leave insert mode: searching=%v mode=%q", h.p.searching, h.p.VimMode())
	}
	h.out = nil
	h.key("space", "q")
	if keys, _ := h.forwarded(); strings.Join(keys, ",") != "space,q" {
		t.Fatalf("normal mode forwards Space and q to the shell, got %v", keys)
	}
	h.key("esc") // normal mode: clear and close
	if h.p.searching || h.p.cur().log.query != "" {
		t.Fatal("Esc in normal mode clears the search")
	}
}

func TestSearchPromptEnterKeepsTheResultsAndPasteIsText(t *testing.T) {
	h := newHarness(t)
	h.openSearch()
	h.paste("tick")
	if h.p.cur().log.query != "tick" {
		t.Fatalf("paste did not reach the search (%d characters)", len(h.p.cur().log.query))
	}
	h.key("enter")
	if h.p.searching || h.p.cur().log.query != "tick" {
		t.Fatal("Enter keeps the results and closes the prompt")
	}
}

func TestRenamePromptIsAModalEditor(t *testing.T) {
	h := newHarness(t)
	h.key("down", "tab") // the saved list
	id := h.p.messages()[0].ID
	h.key("r")
	if h.p.rename == nil || h.p.VimMode() != "INSERT" {
		t.Fatalf("rename=%v mode=%q", h.p.rename != nil, h.p.VimMode())
	}
	h.typ(" b:q")
	if !strings.HasSuffix(h.p.rename.Text(), " b:q") {
		t.Fatal("Space, ':' and 'q' are text in insert mode")
	}
	h.key("esc")
	if h.p.rename == nil || h.p.VimMode() != "NORMAL" {
		t.Fatal("first Esc only leaves insert mode")
	}
	before := h.p.messages()[0].Name
	h.key("esc") // cancel
	if h.p.rename != nil || h.p.messages()[0].Name != before || h.p.messages()[0].ID != id {
		t.Fatal("Esc in normal mode cancels the rename")
	}
	h.key("r", "ctrl+a")
	h.key("enter")
	if h.p.rename != nil {
		t.Fatal("Enter closes the prompt")
	}
}

func TestPromptViewsArePure(t *testing.T) {
	h := newHarness(t)
	h.openSearch()
	h.typ("tick")
	text, view := h.p.search.Text(), h.p.View()
	if h.p.View() != view || h.p.search.Text() != text {
		t.Fatal("View changed the search prompt")
	}
}
