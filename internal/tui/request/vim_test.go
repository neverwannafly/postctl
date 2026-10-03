package request

import (
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/shubham/postctl/internal/model"
	"github.com/shubham/postctl/internal/store"
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
		{"params cell", func(h *harness) { h.key("down", "enter") }},
		{"path variable cell", func(h *harness) { h.key("down", "down", "down", "enter") }},
		{"header cell", func(h *harness) { h.gotoTab(tabHeaders); h.key("down", "enter") }},
		{"header bulk", func(h *harness) { h.gotoTab(tabHeaders); h.key("down", "b") }},
		{"urlencoded cell", func(h *harness) {
			h.req.Body = model.Body{Mode: model.BodyURLEncoded, URLEncoded: []model.KV{{Key: "k", Value: "v"}}}
			h.p.reloadAll()
			h.gotoTab(tabBody)
			h.key("down", "down", "enter")
		}},
		{"form-data cell", func(h *harness) {
			h.req.Body = model.Body{Mode: model.BodyFormData, FormData: []model.FormPart{{Key: "k", Value: "v"}}}
			h.p.reloadAll()
			h.gotoTab(tabBody)
			h.key("down", "down", "enter")
		}},
		{"auth row", func(h *harness) {
			h.req.Auth = &model.Auth{Type: model.AuthBearer, Bearer: &model.BearerAuth{}}
			h.gotoTab(tabAuth)
			h.key("down", "down", "enter")
		}},
		{"settings row", func(h *harness) { h.gotoTab(tabSettings); h.key("down", "down", "enter") }},
		{"raw body", func(h *harness) { h.gotoTab(tabBody); h.key("down", "down", "down") }},
		{"graphql query", func(h *harness) {
			h.req.Body = model.Body{Mode: model.BodyGraphQL}
			h.p.reloadAll()
			h.gotoTab(tabBody)
			h.key("down", "down")
		}},
		{"pre-request script", func(h *harness) { h.gotoTab(tabPre); h.key("down") }},
		{"test script", func(h *harness) { h.gotoTab(tabTests); h.key("down") }},
	}
}

func (h *harness) forwardedKeys() (keys []string, seqs []string) {
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

func (h *harness) sends() (n int) {
	for _, m := range h.out {
		if _, ok := m.(tui.SendRequestMsg); ok {
			n++
		}
	}
	return
}

// In normal mode Space, ":", "q" and g+letter belong to the shell: the field
// hands them back (and the shell's typing gate lets the leader through).
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
			keys, seqs := h.forwardedKeys()
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
			if keys, seqs := h.forwardedKeys(); len(keys)+len(seqs) != 0 {
				t.Fatalf("insert mode forwarded %v %v", keys, seqs)
			}
			f := h.p.activeField()
			if f == nil || !strings.Contains(f.Text(), " :q") {
				t.Fatalf("typed text not in the field: %v", f != nil)
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

func TestCtrlSSendsInInsertNormalAndVisual(t *testing.T) {
	for _, o := range []string{"url", "raw body", "pre-request script", "params cell"} {
		t.Run(o, func(t *testing.T) {
			h := newHarness(t)
			for _, op := range fieldOpeners() {
				if op.name == o {
					op.open(h)
				}
			}
			h.key("ctrl+s") // normal
			h.key("i", "ctrl+s")
			h.key("esc", "v", "ctrl+s")
			if n := h.sends(); n != 3 {
				t.Fatalf("sends = %d, want 3 (normal, insert, visual)", n)
			}
		})
	}
}

func TestEnterInTheURLBarSendsInBothModes(t *testing.T) {
	h := newHarness(t)
	h.key("up", "enter")
	h.key("i", "enter")
	if n := h.sends(); n != 2 {
		t.Fatalf("sends = %d, want 2", n)
	}
	if strings.Contains(h.p.url.Text(), "\n") {
		t.Fatal("Enter added a line to the URL")
	}
}

// Esc: insert to normal, normal to leaving the field, and only then cancel a
// running send.
func TestEscLadderEndsAtCancellingTheSend(t *testing.T) {
	h := newHarness(t)
	h.send(tui.SendStartedMsg{RequestID: h.req.ID})
	cancelled := func() bool {
		for _, m := range h.out {
			if _, ok := m.(tui.CancelSendMsg); ok {
				return true
			}
		}
		return false
	}
	h.key("up", "i")
	h.key("esc")
	if h.p.VimMode() != "NORMAL" || cancelled() {
		t.Fatalf("first Esc: mode %q cancelled %v", h.p.VimMode(), cancelled())
	}
	h.key("esc")
	if h.p.Editing() || cancelled() {
		t.Fatalf("second Esc: editing %v cancelled %v", h.p.Editing(), cancelled())
	}
	h.key("esc")
	if !cancelled() {
		t.Fatal("third Esc should cancel the send")
	}
}

// ---- undo, autosave -------------------------------------------------------------------

func TestEachFieldVisitIsOneRequestLevelUndoStep(t *testing.T) {
	h := newHarness(t)
	orig := h.req.Clone()
	h.key("down", "enter") // limit's value, normal
	h.key("C")
	h.typ("50")
	h.advance(2 * time.Second)
	h.typ("1")
	h.key("esc", "esc")
	if h.req.URL.Query[0].Value != "501" {
		t.Fatalf("value = %q", h.req.URL.Query[0].Value)
	}
	h.key("u") // outside a field: request-level undo
	if !reflect.DeepEqual(h.req.URL.Query, orig.URL.Query) {
		t.Fatalf("one undo should revert the whole visit: %+v", h.req.URL.Query)
	}
}

func TestUInsideAFieldIsVimUndo(t *testing.T) {
	h := newHarness(t)
	orig := h.req.URL.Raw
	h.key("up", "A")
	h.typ("zz")
	h.key("esc", "u") // vim's undo inside the field
	if h.req.URL.Raw != orig {
		t.Fatalf("raw = %q", h.req.URL.Raw)
	}
	if !h.p.Editing() {
		t.Fatal("u must not leave the field")
	}
}

func TestBodyEditsAutosaveAndFlushOnBlur(t *testing.T) {
	h := newHarness(t)
	h.gotoTab(tabBody)
	h.key("down", "down", "down", "A")
	h.typ(" ")
	if n, last := h.dirty(); n != 1 || !last {
		t.Fatalf("dirty msgs = %d last=%v", n, last)
	}
	h.advance(time.Second)
	if len(h.saves()) != 1 {
		t.Fatalf("saves after the debounce = %d", len(h.saves()))
	}
	h.typ("x")
	h.run(h.p.BlurCmd())
	if len(h.saves()) != 2 {
		t.Fatalf("BlurCmd should flush: saves = %d", len(h.saves()))
	}
}

// ---- $EDITOR -------------------------------------------------------------------------

func TestEditorRoundTripForTheURLBar(t *testing.T) {
	h := newHarness(t, editorDeps(`printf 'https://api.example.test/edited\n' > "$1"`))
	h.key("up")
	h.action("request.editor")
	if h.req.URL.Raw != "https://api.example.test/edited" {
		t.Fatal("the URL did not take the editor's text")
	}
	if h.p.url.Text() != h.req.URL.Raw {
		t.Fatal("the URL field is out of sync")
	}
	if h.p.zone != zBar || h.p.VimMode() != "NORMAL" {
		t.Fatalf("back in the URL field expected: zone %v mode %q", h.p.zone, h.p.VimMode())
	}
	h.key("ctrl+z")
	if !strings.Contains(h.req.URL.Raw, "baseUrl") {
		t.Fatal("undo should restore the URL")
	}
}

func TestEditorForASingleLineCellKeepsOneLine(t *testing.T) {
	h := newHarness(t, editorDeps(`printf 'one\ntwo\n' > "$1"`))
	h.key("down", "enter")
	h.action("request.editor")
	if got := h.req.URL.Query[0].Value; got != "one two" {
		t.Fatalf("value = %q", got)
	}
	if h.req.URL.Query[0].Key != "limit" {
		t.Fatal("the other cells changed")
	}
}

func TestEditorRefusesAHiddenSecret(t *testing.T) {
	h := newHarness(t, editorDeps(`printf 'x\n' > "$1"`))
	h.req.Auth = &model.Auth{Type: model.AuthBearer, Bearer: &model.BearerAuth{Token: "literal-secret-value"}}
	h.gotoTab(tabAuth)
	h.key("down", "down", "enter")
	h.action("request.editor")
	h.mustHave("Reveal the secret")
	if h.req.Auth.Bearer.Token != "literal-secret-value" {
		t.Fatal("a hidden secret was sent to the editor")
	}
}

// ---- completion ------------------------------------------------------------------------

func TestVariableCompletionMasksSecretsInTheFrame(t *testing.T) {
	h := newHarness(t)
	h.key("up", "A")
	h.typ("{{to")
	f := h.p.url
	if !f.Completing() {
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

func TestCompletionWorksInScriptsWithPM(t *testing.T) {
	h := newHarness(t)
	h.gotoTab(tabTests)
	h.key("down", "i")
	h.typ("pm.respo")
	if !h.p.activeField().Completing() {
		t.Fatal("pm. completion did not open")
	}
	h.mustHave("response")
}

// ---- purity ---------------------------------------------------------------------------

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

func TestLongScriptViewDoesNotMoveTheWindow(t *testing.T) {
	h := newHarness(t)
	h.req.Scripts.Test = &model.Script{Source: strings.Repeat("pm.test('x', () => {});\n", 80)}
	h.p.reloadAll()
	h.gotoTab(tabTests)
	h.key("down", "G")
	f := h.p.activeField()
	before := f.Render(40, 5)
	_ = h.p.View()
	if f.Render(40, 5) != before {
		t.Fatal("View scrolled the editor")
	}
}

// Blurring the pane ends the field session: edits before and after separate
// visits are separate request-level undo steps, even in the same field.
func TestBlurEndsTheFieldSessionForUndo(t *testing.T) {
	h := newHarness(t)
	orig := h.req.URL.Raw
	h.key("up", "A")
	h.typ("1")
	h.p.Blur()
	_ = h.p.Focus()
	if !h.p.activeField().Typing() {
		h.key("A")
	}
	h.typ("2")
	h.key("esc", "esc") // leave the field
	if h.req.URL.Raw != orig+"12" {
		t.Fatalf("URL after both visits has %d characters, want %d", len(h.req.URL.Raw), len(orig)+2)
	}
	h.key("u")
	if h.req.URL.Raw != orig+"1" {
		t.Fatalf("first undo should only revert the second visit (%d characters)", len(h.req.URL.Raw))
	}
	h.key("u")
	if h.req.URL.Raw != orig {
		t.Fatal("second undo should revert the first visit")
	}
}

// Sending flushes the pending autosave: the request that goes out and the
// request on disk are the same one.
func TestSendFlushesPendingAutosave(t *testing.T) {
	h := newHarness(t)
	h.key("up", "A")
	h.typ("zq")
	h.out = nil
	h.key("ctrl+s")
	var sent *tui.SendRequestMsg
	for _, m := range h.out {
		if s, ok := m.(tui.SendRequestMsg); ok {
			sent = &s
		}
	}
	if sent == nil || !strings.HasSuffix(sent.Request.Request.URL.Raw, "zq") {
		t.Fatal("the sent request lacks the edit")
	}
	if len(h.saves()) != 1 || h.saves()[0].ID != h.req.ID {
		t.Fatalf("saves emitted with the send = %d, want 1 for this request", len(h.saves()))
	}
}

// A secret row shows its text unmasked only when the whole value is one
// {{reference}}; any literal text keeps it masked.
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
		{"{{a}}tail", true},
		{"plain-literal", true},
		{"", true},
	} {
		if got := f.maskFor(r, c.text); got != c.want {
			t.Errorf("maskFor(%d chars) = %v, want %v", len(c.text), got, c.want)
		}
	}
	f.reveal = true
	if f.maskFor(r, "literal") {
		t.Error("a revealed row must not be masked")
	}
	if (&form{}).maskFor(formRow{}, "literal") {
		t.Error("a plain row is never masked")
	}
}

// ---- save round trip next to a hidden secret --------------------------------------------

// Editing the user name beside a hidden password must save the password
// unchanged: no mask glyph reaches the model or the YAML, and a yank of the
// hidden password is the fixed mask, never the text.
func TestSavingNextToAHiddenSecretKeepsItIntact(t *testing.T) {
	const password = "pw-sentinel-synthetic-0001"
	r := newShellRigWith(t, "", func(req *model.Request) {
		req.Auth = &model.Auth{Type: model.AuthBasic, Basic: &model.BasicAuth{Username: "alice", Password: password}}
	})
	r.keys("g", "a")        // the Auth tab
	r.keys("down", "enter") // the user name row
	r.mustHave("NORMAL")
	r.keys("A")
	r.keys("2")
	r.keys("esc", "esc")
	r.keys("down", "enter") // the password row
	r.mustHave("NORMAL")
	if strings.Contains(r.screen(), password) {
		t.Fatal("the hidden password is on screen")
	}
	r.keys("y", "y") // yank the whole line
	for _, c := range r.clip {
		if strings.Contains(c, password) || strings.Trim(c, "•*") != "" {
			t.Fatalf("a yank of the hidden password was %d characters of something other than the mask", len(c))
		}
	}
	if len(r.clip) == 0 {
		t.Fatal("the yank copied nothing, so the mask is unproven")
	}
	r.keys("esc", "esc")
	r.keys("1") // focus leaves the pane: unsaved edits are written
	r.pump(600 * time.Millisecond)

	st, err := store.Open(r.dir, store.WithSecretStore(store.NewMemorySecrets()))
	if err != nil {
		t.Fatal(err)
	}
	ws, err := st.Load()
	if err != nil {
		t.Fatal(err)
	}
	var saved *model.Request
	for _, req := range ws.Collections[0].Requests() {
		if req.Name == "Create charge" {
			saved = req
		}
	}
	if saved == nil || saved.Auth == nil || saved.Auth.Basic == nil {
		t.Fatal("the saved request lost its basic auth")
	}
	if saved.Auth.Basic.Username != "alice2" {
		t.Fatalf("the user name was not saved (%d characters)", len(saved.Auth.Basic.Username))
	}
	if saved.Auth.Basic.Password != password {
		t.Fatal("the stored password changed")
	}
	err = filepath.WalkDir(r.dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		raw, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		for _, glyph := range []string{"••••", "****"} {
			if strings.Contains(string(raw), glyph) {
				t.Errorf("%s holds the mask glyph %q", filepath.Base(path), glyph)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// The temporary file handed to $EDITOR is private to the user.
func TestEditorTempFileIsPrivate(t *testing.T) {
	out := filepath.Join(t.TempDir(), "mode")
	h := newHarness(t, editorDeps(`stat -f %Lp "$1" > `+out+`; printf 'x\n' > "$1"`))
	h.key("up")
	h.action("request.editor")
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("the editor never ran: %v", err)
	}
	if strings.TrimSpace(string(got)) != "600" {
		t.Fatalf("temp file mode = %s, want 600", strings.TrimSpace(string(got)))
	}
}
