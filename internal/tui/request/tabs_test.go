package request

import (
	"os"
	"os/exec"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/shubham/postctl/internal/model"
)

// ---- Body -----------------------------------------------------------------------

func TestBodyRawEditingAndValidation(t *testing.T) {
	h := newHarness(t)
	h.gotoTab(tabBody)
	h.key("down", "down", "down") // Type, Language, content (starts editing)
	if !h.p.Editing() {
		t.Fatal("moving onto the editor should focus it")
	}
	h.key("g", "g")
	h.mustHave("Valid JSON")
	h.key("G", "A")
	h.typ(",")
	if !strings.HasSuffix(h.req.Body.Raw, ",") {
		t.Fatalf("raw = %q", h.req.Body.Raw)
	}
	v := h.view()
	if !strings.Contains(v, "line 1, column") {
		t.Fatalf("no validation hint with position:\n%s", v)
	}
	h.key("esc") // insert to normal
	if h.p.VimMode() != "NORMAL" {
		t.Fatalf("mode after one esc = %q", h.p.VimMode())
	}
	h.key("esc")
	if h.p.Editing() {
		t.Fatal("a second esc should leave the editor")
	}
}

func TestBodyFormatKeepsVariables(t *testing.T) {
	h := newHarness(t)
	h.req.Body.Raw = `{"a":{{amount}},"b":"{{name}}","c":[1,2]}`
	h.p.reloadAll()
	h.gotoTab(tabBody)
	h.key("down", "down", "f")
	want := "{\n  \"a\": {{amount}},\n  \"b\": \"{{name}}\",\n  \"c\": [\n    1,\n    2\n  ]\n}"
	if h.req.Body.Raw != want {
		t.Fatalf("raw = %q", h.req.Body.Raw)
	}
	h.key("ctrl+z")
	if h.req.Body.Raw != `{"a":{{amount}},"b":"{{name}}","c":[1,2]}` {
		t.Fatalf("undo raw = %q", h.req.Body.Raw)
	}
}

func TestBodyFormatInvalidJSONExplains(t *testing.T) {
	h := newHarness(t)
	h.req.Body.Raw = `{"a": }`
	h.p.reloadAll()
	h.gotoTab(tabBody)
	h.key("down", "down")
	h.mustHave("line 1")
	acts := h.p.Actions()
	var run func() tea.Cmd
	for _, a := range acts {
		if a.ID == "request.formatJSON" {
			run = a.Run
		}
	}
	if run == nil {
		t.Fatal("no format action")
	}
	h.run(run())
	h.mustHave("Cannot format")
	if h.req.Body.Raw != `{"a": }` {
		t.Fatal("invalid JSON was modified")
	}
}

func TestBodyModeSwitchKeepsContent(t *testing.T) {
	h := newHarness(t)
	h.gotoTab(tabBody)
	h.key("down") // Type row
	h.key("right")
	if h.req.Body.Mode != model.BodyURLEncoded {
		t.Fatalf("mode = %v", h.req.Body.Mode)
	}
	h.key("down", "a")
	h.typ("k")
	h.key("tab")
	h.typ("v")
	h.key("esc", "esc")
	if len(h.req.Body.URLEncoded) != 1 || h.req.Body.URLEncoded[0].Key != "k" || h.req.Body.URLEncoded[0].Value != "v" {
		t.Fatalf("urlencoded = %v", h.req.Body.URLEncoded)
	}
	h.key("up", "left")
	if h.req.Body.Mode != model.BodyRaw || !strings.Contains(h.req.Body.Raw, "amount") {
		t.Fatalf("raw content lost: mode=%v raw=%q", h.req.Body.Mode, h.req.Body.Raw)
	}
}

func TestBodyBinaryPath(t *testing.T) {
	h := newHarness(t)
	h.req.Body = model.Body{Mode: model.BodyBinary}
	h.p.reloadAll()
	h.gotoTab(tabBody)
	h.key("down", "down", "i")
	h.typ("/tmp/a.bin")
	h.key("esc", "esc")
	if h.req.Body.File != "/tmp/a.bin" {
		t.Fatalf("file = %q", h.req.Body.File)
	}
}

func TestBodyGraphQL(t *testing.T) {
	h := newHarness(t)
	h.req.Body = model.Body{Mode: model.BodyGraphQL}
	h.p.reloadAll()
	h.gotoTab(tabBody)
	h.key("down", "down", "i") // into Query, typing
	h.typ("{me}")
	h.key("esc", "tab", "i") // Variables, from normal mode
	h.typ("{}")
	gq := h.req.Body.GraphQL
	if gq == nil || gq.Query != "{me}" || gq.Variables != "{}" {
		t.Fatalf("graphql = %+v", gq)
	}
	h.mustHave("Variables (JSON)")
}

func TestBodyNone(t *testing.T) {
	h := newHarness(t)
	h.req.Body = model.Body{}
	h.gotoTab(tabBody)
	h.mustHave("no body")
}

func TestFormDataFileToggle(t *testing.T) {
	h := newHarness(t)
	h.req.Body = model.Body{Mode: model.BodyFormData, FormData: []model.FormPart{{Key: "f", Value: "x"}}}
	h.p.reloadAll()
	h.gotoTab(tabBody)
	h.key("down", "down", "t")
	if h.req.Body.FormData[0].Type != model.FormFile || h.req.Body.FormData[0].Src != "x" {
		t.Fatalf("part = %+v", h.req.Body.FormData[0])
	}
	h.mustHave("@ x")
}

// ---- Scripts --------------------------------------------------------------------

func TestPreRequestScriptEditing(t *testing.T) {
	h := newHarness(t)
	h.gotoTab(tabPre)
	h.key("down") // enters and focuses the editor, in normal mode
	if !h.p.Editing() || h.p.VimMode() != "NORMAL" {
		t.Fatalf("script editor should be focused in normal mode, got %q", h.p.VimMode())
	}
	h.key("i")
	h.typ("x=1")
	if h.req.Scripts.PreRequest == nil || h.req.Scripts.PreRequest.Source != "x=1" {
		t.Fatalf("script = %+v", h.req.Scripts.PreRequest)
	}
	h.mustHave("1 ") // line number gutter
	h.key("esc", "esc")
	h.key("ctrl+z")
	if h.req.Scripts.PreRequest != nil && h.req.Scripts.PreRequest.Source != "" {
		t.Fatalf("undo left %+v", h.req.Scripts.PreRequest)
	}
}

func TestScriptClearedBecomesEmpty(t *testing.T) {
	h := newHarness(t)
	h.req.Scripts.Test = &model.Script{Source: "a"}
	h.p.reloadAll()
	h.gotoTab(tabTests)
	h.key("down", "x")
	if !h.req.Scripts.Test.Empty() {
		t.Fatalf("script = %+v", h.req.Scripts.Test)
	}
}

func TestScriptSnippetInsert(t *testing.T) {
	h := newHarness(t)
	h.gotoTab(tabTests)
	h.key("down", "esc") // focus then leave editing
	h.action("request.snippet")
	h.mustHave("Insert a snippet")
	h.mustHave("Status code is 200")
	h.key("enter")
	src := h.req.Scripts.Test.Source
	if !strings.Contains(src, "pm.test(\"Status code is 200\"") {
		t.Fatalf("source = %q", src)
	}
	h.mustNotHave("Insert a snippet")
}

// ---- Settings ---------------------------------------------------------------------

func TestSettingsToggleAndValidate(t *testing.T) {
	h := newHarness(t)
	h.gotoTab(tabSettings)
	h.key("down")
	h.mustHave("Follow redirects")
	h.key("space")
	if f := h.req.Settings.FollowRedirects; f == nil || *f {
		t.Fatal("follow redirects not turned off")
	}
	h.mustHave("ignored: not following")
	h.key("down", "down") // Verify TLS
	h.key("space")
	if v := h.req.Settings.VerifyTLS; v == nil || *v {
		t.Fatal("TLS not turned off")
	}
	h.mustHave("certificates are not checked")
	h.key("down", "i") // timeout
	h.typ("abc")
	h.mustHave("use 30s")
	if h.req.Settings.Timeout != nil {
		t.Fatal("invalid timeout applied")
	}
	h.key("esc", "S")
	h.typ("2m")
	if to := h.req.Settings.Timeout; to == nil || to.String() != "2m0s" {
		t.Fatalf("timeout = %v", to)
	}
	h.key("esc")
}

func TestSettingsMaxRedirects(t *testing.T) {
	h := newHarness(t)
	h.gotoTab(tabSettings)
	h.key("down", "down", "i")
	h.typ("5")
	h.key("esc", "esc")
	if m := h.req.Settings.MaxRedirects; m == nil || *m != 5 {
		t.Fatalf("max = %v", m)
	}
}

func TestParseTimeout(t *testing.T) {
	for in, ok := range map[string]bool{"30": true, "1.5": true, "30s": true, "2m": true, "1h30m": true, "": false, "x": false, "-1": false} {
		if _, got := parseTimeout(in); got != ok {
			t.Errorf("parseTimeout(%q) = %v, want %v", in, got, ok)
		}
	}
}

// ---- $EDITOR ------------------------------------------------------------------------

// editorDeps runs the "editor" synchronously: a shell one-liner that rewrites
// the file, standing in for a real editor session.
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

func TestEditorRoundTripRawBody(t *testing.T) {
	h := newHarness(t, editorDeps(`printf '{"edited": true}\n' > "$1"`))
	h.gotoTab(tabBody)
	h.key("down", "down")
	h.action("request.editor")
	if h.req.Body.Raw != `{"edited": true}` {
		t.Fatalf("raw = %q", h.req.Body.Raw)
	}
	h.mustHave("Updated from the editor")
	h.key("ctrl+z")
	if !strings.Contains(h.req.Body.Raw, "amount") {
		t.Fatalf("undo raw = %q", h.req.Body.Raw)
	}
	left, _ := os.ReadDir(h.deps.TempDir)
	if len(left) != 0 {
		t.Fatalf("temporary files left behind: %v", left)
	}
}

func TestEditorActionOpensCurrentRequestTab(t *testing.T) {
	h := newHarness(t, editorDeps(`printf '{"edited with e": true}\n' > "$1"`))
	h.gotoTab(tabBody)
	h.action("request.editor")
	if h.req.Body.Raw != `{"edited with e": true}` {
		t.Fatalf("e did not open the current tab in $EDITOR: %q", h.req.Body.Raw)
	}
}

func TestEditorSeesCurrentTextAndTempFileIsPrivate(t *testing.T) {
	var mode os.FileMode
	var seen string
	h := newHarness(t, func(h *harness) {
		h.deps.EditorCommand = func(path string) *exec.Cmd {
			st, _ := os.Stat(path)
			mode = st.Mode().Perm()
			b, _ := os.ReadFile(path)
			seen = string(b)
			return exec.Command("true")
		}
		h.deps.Exec = func(cmd *exec.Cmd, done tea.ExecCallback) tea.Cmd {
			return func() tea.Msg { return done(cmd.Run()) }
		}
	})
	h.gotoTab(tabPre)
	h.req.Scripts.PreRequest = &model.Script{Source: "console.log(1)"}
	h.p.reloadAll()
	h.action("request.editor")
	if seen != "console.log(1)" {
		t.Fatalf("editor saw %q", seen)
	}
	if mode != 0o600 {
		t.Fatalf("temp file mode = %v", mode)
	}
	h.mustHave("No changes")
}

func TestEditorFailureChangesNothing(t *testing.T) {
	h := newHarness(t, editorDeps(`printf 'x' > "$1"; exit 3`))
	h.gotoTab(tabBody)
	h.key("down", "down")
	before := h.req.Body.Raw
	h.action("request.editor")
	if h.req.Body.Raw != before {
		t.Fatal("body changed after a failed editor")
	}
	h.mustHave("nothing changed")
}

func TestEditorNothingToOpenOnAuthTab(t *testing.T) {
	h := newHarness(t, editorDeps("true"))
	h.gotoTab(tabAuth)
	h.action("request.editor")
	h.mustHave("Nothing to open")
}

func TestEditorBulkHeaders(t *testing.T) {
	h := newHarness(t, editorDeps(`printf 'A: 1\nB: 2\n' > "$1"`))
	h.gotoTab(tabHeaders)
	h.key("down")
	h.action("request.editor")
	if len(h.req.Headers) != 2 || h.req.Headers[1].Key != "B" {
		t.Fatalf("headers = %v", h.req.Headers)
	}
}

func TestEditorCommandHonoursArguments(t *testing.T) {
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "code --wait")
	cmd := Deps{}.editorCmd("/tmp/x.json")
	if got := strings.Join(cmd.Args, " "); got != "code --wait /tmp/x.json" {
		t.Fatalf("args = %q", got)
	}
}
