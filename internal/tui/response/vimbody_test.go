package response

import (
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/shubham/postctl/internal/history"
	"github.com/shubham/postctl/internal/model"
	"github.com/shubham/postctl/internal/tui"
	"github.com/shubham/postctl/internal/tui/vimtext"
	"github.com/shubham/postctl/internal/ui"
)

// vimDoc is a pretty-printed body with a credential-looking field whose value
// the redactor learns only from the field name, and a plain field that holds
// the known secret.
const vimDoc = `{
  "name": "alice",
  "token": "unlisted-credential-value",
  "note": "key ` + v3Secret + ` end",
  "n": 7
}`

// vimPane shows body in a focused pane's Body tab. Clipboard writes are
// collected in the returned slice.
func vimPane(t *testing.T, body string) (*Pane, *[]string) {
	t.Helper()
	p, id, clip := v3Pane(t, nil, v3Theme())
	res := v3Result(id, body, "", history.NewRedactor([]string{v3Secret}))
	v3Pump(t, p, tui.SendResultMsg{Result: res})
	if !p.vimLive() {
		t.Fatalf("the Body tab should be a live vim buffer for a %d byte body", len(body))
	}
	return p, clip
}

func vimPos(p *Pane) (row, col int) { return p.vim.f.Cursor() }

func TestVimBodyMotionsAndSearch(t *testing.T) {
	p, _ := vimPane(t, vimDoc)
	if got := p.vim.f.Text(); got != vimDoc {
		t.Fatalf("the buffer must hold the body as sent, got %d bytes", len(got))
	}
	if p.VimMode() != "NORMAL" || !p.Editing() {
		t.Fatalf("mode %q editing %v, want NORMAL and editing", p.VimMode(), p.Editing())
	}
	vimKeys(t, p, "j", "j")
	if r, _ := vimPos(p); r != 2 {
		t.Fatalf("jj: row %d, want 2", r)
	}
	vimKeys(t, p, "G")
	if r, _ := vimPos(p); r != 5 {
		t.Fatalf("G: row %d, want the last row 5", r)
	}
	vimKeys(t, p, "g", "g")
	if r, _ := vimPos(p); r != 0 {
		t.Fatalf("gg: row %d, want 0", r)
	}
	// / n N find "al", which is in the name only; "e" repeats.
	vimKeys(t, p, "/", "a", "l", "enter")
	if r, c := vimPos(p); r != 1 || c != 11 {
		t.Fatalf("/al: at %d,%d, want 1,11", r, c)
	}
	vimKeys(t, p, "/", "e", "enter")
	r1, c1 := vimPos(p)
	vimKeys(t, p, "n")
	r2, c2 := vimPos(p)
	if r1 == r2 && c1 == c2 {
		t.Fatal("n should move to the next match")
	}
	vimKeys(t, p, "N")
	if r, c := vimPos(p); r != r1 || c != c1 {
		t.Fatalf("N should come back to %d,%d, got %d,%d", r1, c1, r, c)
	}
}

func TestVimBodyYankIsMaskedUnlessRevealed(t *testing.T) {
	p, clip := vimPane(t, vimDoc)
	yank := func(keys ...string) string {
		t.Helper()
		*clip = nil
		vimKeys(t, p, keys...)
		if len(*clip) != 1 {
			t.Fatalf("keys %v copied %d times", keys, len(*clip))
		}
		return (*clip)[0]
	}
	// yy on the credential line: the value is masked by the field name.
	got := yank("g", "g", "j", "j", "y", "y")
	if strings.Contains(got, "unlisted-credential-value") || !strings.Contains(got, `"token": "`+history.Mask+`"`) {
		t.Fatalf("yy leaked or lost the structure: %q", got)
	}
	// A selection of just the value is masked although the fragment alone
	// shows no field name.
	got = yank("g", "g", "j", "j", "/", "u", "n", "l", "i", "s", "t", "enter", "v", "e", "y")
	if got != history.Mask {
		t.Fatalf("a selection inside a credential value copied %q, want the mask", got)
	}
	// A line holding a known secret.
	got = yank("g", "g", "3", "j", "y", "y")
	if strings.Contains(got, v3Secret) || !strings.Contains(got, history.Mask) {
		t.Fatalf("known secret copied: %q", got)
	}
	// V over the whole buffer.
	got = yank("g", "g", "V", "G", "y")
	if strings.Contains(got, "unlisted-credential-value") || strings.Contains(got, v3Secret) ||
		!strings.Contains(got, `"name": "alice"`) {
		t.Fatalf("whole-buffer yank: %q", got)
	}
	if !p.lastYankMasked {
		t.Fatal("the pane should know the last copy was masked")
	}

	// u reveals: copies are raw until the tab changes.
	vimKeys(t, p, "u")
	got = yank("g", "g", "j", "j", "y", "y")
	if !strings.Contains(got, "unlisted-credential-value") {
		t.Fatalf("revealed yank should be raw, got %q", got)
	}
	v3Pump(t, p, tui.SectionStepMsg{Delta: 1})
	v3Pump(t, p, tui.SectionStepMsg{Delta: -1})
	got = yank("g", "g", "j", "j", "y", "y")
	if strings.Contains(got, "unlisted-credential-value") {
		t.Fatalf("reveal should end with the tab visit, got %q", got)
	}
}

func TestVimBodyYankOfMultiLineSecretIsFullyMasked(t *testing.T) {
	multi := "line-one-of-secret\nline-two-of-secret"
	p, id, clip := v3Pane(t, nil, v3Theme())
	res := v3Result(id, "{\n  \"a\": \"x\",\n  \"pem\": \"-----\\nfoo\"\n}\nline-one-of-secret\nline-two-of-secret", "", history.NewRedactor([]string{multi}))
	v3Pump(t, p, tui.SendResultMsg{Result: res})
	vimKeys(t, p, "G", "y", "y")
	if len(*clip) != 1 || strings.Contains((*clip)[0], "line-two-of-secret") {
		t.Fatalf("multi-line secret leaked: %q", *clip)
	}
}

func TestVimBodyEditingKeysAreInert(t *testing.T) {
	p, clip := vimPane(t, vimDoc)
	for _, keys := range [][]string{
		{"i", "x", "esc"}, {"a", "z", "esc"}, {"o", "esc"}, {"O", "esc"}, {"A", "q", "esc"}, {"I", "q", "esc"},
		{"x"}, {"X"}, {"d", "d"}, {"D"}, {"c", "w", "esc"}, {"C", "esc"}, {"s", "esc"}, {"S", "esc"},
		{"p"}, {"P"}, {"J"}, {"~"}, {">", ">"}, {"u"}, {"ctrl+r"}, {"r", "x"},
	} {
		// Return to a known place; r and u are pane keys, so they are checked too.
		vimKeys(t, p, keys...)
		if p.vim.f.Text() != vimDoc {
			t.Fatalf("keys %v edited the buffer", keys)
		}
		if p.VimMode() != "NORMAL" && p.VimMode() != "VISUAL" {
			t.Fatalf("keys %v left mode %q", keys, p.VimMode())
		}
		if p.pathActive {
			p.pathActive = false
		}
		vimKeys(t, p, "esc")
		if got := p.vim.f.Text(); got != vimDoc || p.VimMode() != "NORMAL" {
			t.Fatalf("after %v and Esc: mode %q", keys, p.VimMode())
		}
	}
	if len(*clip) != 0 {
		t.Fatalf("an editing key copied to the clipboard: %q", *clip)
	}
}

func TestVimBodyHandsUnownedKeysToTheShell(t *testing.T) {
	p, _ := vimPane(t, vimDoc)
	forwarded := func(key string) (tui.ForwardKeyMsg, tui.ForwardSeqMsg, bool) {
		t.Helper()
		var fk tui.ForwardKeyMsg
		var fs tui.ForwardSeqMsg
		var got bool
		for _, m := range v3Pump(t, p, keyOf(key)) {
			switch m := m.(type) {
			case tui.ForwardKeyMsg:
				fk, got = m, true
			case tui.ForwardSeqMsg:
				fs, got = m, true
			}
		}
		return fk, fs, got
	}
	for _, k := range []string{"space", ":", "q", "tab", "shift+tab", "ctrl+h", "ctrl+j", "ctrl+k", "ctrl+l", "[", "]"} {
		fk, _, ok := forwarded(k)
		if !ok || fk.Key.String() != keyOf(k).String() {
			t.Errorf("key %q was not handed to the shell (got %v)", k, fk.Key.String())
		}
		vimKeys(t, p, "esc") // clear any prefix the editor kept
	}
	for _, seq := range []string{"gH", "gb", "gu", "gr"} {
		vimKeys(t, p, "g")
		_, fs, ok := func() (tui.ForwardKeyMsg, tui.ForwardSeqMsg, bool) { return forwarded(seq[1:]) }()
		if !ok || fs.Seq != seq {
			t.Errorf("sequence %q was not handed to the shell (got %q)", seq, fs.Seq)
		}
	}
	// gg stays in the buffer.
	vimKeys(t, p, "j", "j")
	vimKeys(t, p, "g")
	if _, _, ok := forwarded("g"); ok {
		t.Error("gg must stay in the buffer")
	}
	if r, _ := vimPos(p); r != 0 {
		t.Errorf("gg: row %d", r)
	}
	// Visual mode keeps its keys: Space is not a leader there.
	vimKeys(t, p, "v")
	if _, _, ok := forwarded("space"); ok {
		t.Error("Space in visual mode must not reach the shell")
	}
	vimKeys(t, p, "esc")
	if p.VimMode() != "NORMAL" {
		t.Fatalf("Esc left visual mode in %q", p.VimMode())
	}
}

func TestVimBodyEscLeavesToTheRequestPane(t *testing.T) {
	p, _ := vimPane(t, vimDoc)
	vimKeys(t, p, "V")
	if msgs := v3Pump(t, p, v3Key("esc")); len(msgs) != 0 || p.VimMode() != "NORMAL" {
		t.Fatalf("Esc in visual mode: mode %q, shell messages %v", p.VimMode(), msgs)
	}
	var left bool
	for _, m := range v3Pump(t, p, v3Key("esc")) {
		if f, ok := m.(tui.FocusPaneMsg); ok && f.Slot == tui.SlotRequest {
			left = true
		}
	}
	if !left {
		t.Fatal("Esc in normal mode should leave the body for the request pane")
	}
}

func TestVimBodyRTogglesRawAndPretty(t *testing.T) {
	p, _ := vimPane(t, `{"a":1,"b":[2]}`)
	if p.vim.f.LineCount() < 3 {
		t.Fatalf("pretty body has %d lines", p.vim.f.LineCount())
	}
	vimKeys(t, p, "r")
	if p.vim.f.LineCount() != 1 || p.vim.f.Text() != `{"a":1,"b":[2]}` {
		t.Fatalf("raw body: %d lines, %q", p.vim.f.LineCount(), p.vim.f.Text())
	}
	vimKeys(t, p, "r")
	if p.vim.f.LineCount() < 3 {
		t.Fatal("r again should pretty-print")
	}
}

func TestVimBodyLargeBodyKeepsTheVirtualViewer(t *testing.T) {
	big := strings.Repeat("a line of plain text\n", largeBodyThreshold/21+10)
	p, id, _ := v3Pane(t, nil, v3Theme())
	res := v3Result(id, big, "", nil)
	res.Response.Headers = []model.KV{{Key: "Content-Type", Value: "text/plain"}}
	v3Pump(t, p, tui.SendResultMsg{Result: res})
	if p.vim != nil || p.vimLive() || p.Editing() || p.VimMode() != "" {
		t.Fatal("a body over the virtual threshold must not load into the editor")
	}
	found := false
	for _, h := range p.KeyHints() {
		if h.Key == largeBodyNote {
			found = true
		}
	}
	if !found {
		t.Fatalf("the footer should say %q, hints: %v", largeBodyNote, p.KeyHints())
	}
	v3Pump(t, p, v3Key("j"))
	if p.scroll != 1 {
		t.Fatalf("the virtual viewer keeps j: scroll %d", p.scroll)
	}
	if !strings.Contains(v3Frame(p), "a line of plain text") {
		t.Fatal("the large body is not shown")
	}
}

func TestVimBodyMediumBodyBuildsInACommand(t *testing.T) {
	medium := `{"items":[` + strings.Repeat(`{"id":"abcdef","n":1},`, vimInlineMax/20) + `{}]}`
	p, id, _ := v3Pane(t, nil, v3Theme())
	res := v3Result(id, medium, "", nil)

	// The buffer is formatted and built off the Update goroutine; until it
	// arrives the pane says so.
	_, cmd := p.Update(tui.SendResultMsg{Result: res})
	if p.vim != nil || p.vimLive() {
		t.Fatal("a medium body must not be built on the Update goroutine")
	}
	if !strings.Contains(v3Frame(p), "Formatting…") {
		t.Fatalf("the pane should say it is formatting while the editor builds:\n%s", v3Frame(p))
	}
	v3Run(t, p, cmd)
	if !p.vimLive() {
		t.Fatal("the built buffer was not adopted")
	}
}

func TestVimBodyStaleBuildIsDropped(t *testing.T) {
	medium := func(n string) string {
		return `{"id":"` + n + `","items":[` + strings.Repeat(`{"id":"abcdef","n":1},`, vimInlineMax/20) + `{}]}`
	}
	p, id, _ := v3Pane(t, nil, v3Theme())
	_, first := p.Update(tui.SendResultMsg{Result: v3Result(id, medium("one"), "", nil)})
	_, second := p.Update(tui.SendResultMsg{Result: v3Result(id, medium("two"), "", nil)})
	v3Run(t, p, first) // arrives after the newer result: dropped
	if p.vim != nil {
		t.Fatal("a stale build was adopted")
	}
	v3Run(t, p, second)
	if !p.vimLive() || !strings.Contains(p.vim.f.Text(), `"two"`) {
		t.Fatal("the current build should win")
	}
}

func TestVimBodyWheelScrollsTheBuffer(t *testing.T) {
	p, id, _ := v3Pane(t, nil, v3Theme())
	var b strings.Builder
	for i := 0; i < 100; i++ {
		b.WriteString("line of text\n")
	}
	res := v3Result(id, b.String(), "", nil)
	res.Response.Headers = []model.KV{{Key: "Content-Type", Value: "text/plain"}}
	v3Pump(t, p, tui.SendResultMsg{Result: res})
	if !p.vimLive() {
		t.Fatal("expected a live buffer")
	}
	p.HandleMouse(tea.MouseWheelMsg{X: 10, Y: 10, Button: tea.MouseWheelDown}, 10, 10)
	if got := p.vim.f.Top(); got != 3 {
		t.Fatalf("wheel down: top %d, want 3", got)
	}
	if p.scroll != 0 {
		t.Fatalf("the wheel must not move the virtual viewer, scroll %d", p.scroll)
	}
	if row, _ := vimPos(p); row < 3 {
		t.Fatalf("the cursor must stay in view, row %d", row)
	}
	p.HandleMouse(tea.MouseWheelMsg{X: 10, Y: 10, Button: tea.MouseWheelUp}, 10, 10)
	if got := p.vim.f.Top(); got != 0 {
		t.Fatalf("wheel up: top %d, want 0", got)
	}
	if p.VimMode() != "NORMAL" {
		t.Fatal("the wheel must not change the mode")
	}
}

func TestVimBodyViewIsPure(t *testing.T) {
	p, _ := vimPane(t, vimDoc)
	vimKeys(t, p, "j", "V", "j")
	state := func() any {
		row, col := vimPos(p)
		return []any{row, col, p.vim.f.Top(), p.VimMode(), p.vim.f.Text(), p.vim, p.scroll, p.revealSecrets, p.lastYankMasked}
	}
	before := state()
	first, second := p.View(), p.View()
	if first != second || !reflect.DeepEqual(before, state()) {
		t.Fatal("View changed the pane or the buffer")
	}
}

func TestVimBodyShowsServerBodyOnScreen(t *testing.T) {
	// The Body tab keeps showing what the server sent (INT4); only copies are masked.
	p, _ := vimPane(t, vimDoc)
	frame := v3Frame(p)
	if !strings.Contains(frame, "unlisted-credential-value") {
		t.Fatalf("the on-screen body should be what the server sent:\n%s", frame)
	}
}

func TestVimBodyFooterHintsFollowTheMode(t *testing.T) {
	p, _ := vimPane(t, vimDoc)
	has := func(key string) bool {
		for _, h := range p.KeyHints() {
			if h.Key == key {
				return true
			}
		}
		return false
	}
	if !has("/") || !has("y") || !has("u") || has("w") || has(".") {
		t.Fatalf("normal hints: %v", p.KeyHints())
	}
	vimKeys(t, p, "v")
	if has("/") || !has("y") || !has("Esc") {
		t.Fatalf("visual hints: %v", p.KeyHints())
	}
	vimKeys(t, p, "esc", "/")
	if !has("Enter") {
		t.Fatalf("search hints: %v", p.KeyHints())
	}
	vimKeys(t, p, "esc")
	vimKeys(t, p, "u")
	if !has("Copies unmasked") {
		t.Fatalf("revealed hints: %v", p.KeyHints())
	}
}

func TestVimBodyYieldsToJQAndDiff(t *testing.T) {
	p, id, _ := v3Pane(t, nil, v3Theme())
	v3Pump(t, p, tui.SendResultMsg{Result: v3Result(id, vimDoc, "", history.NewRedactor([]string{v3Secret}))})
	v3ApplyFilter(t, p, ".name")
	if p.vimLive() || !strings.Contains(v3Frame(p), "Body (jq)") {
		t.Fatalf("the jq view should replace the buffer:\n%s", v3Frame(p))
	}
	if p.Editing() {
		t.Fatal("the jq view is not an editor")
	}
	v3Pump(t, p, v3Key("esc")) // clears the filter
	if !p.vimLive() {
		t.Fatal("clearing the filter should bring the buffer back")
	}
	if p.vim.f.Focused() != true {
		t.Fatal("the buffer should have the keyboard again")
	}
}

func TestVimBodyBlursWhileJQInputIsOpen(t *testing.T) {
	p, id, _ := v3Pane(t, nil, v3Theme())
	v3Pump(t, p, tui.SendResultMsg{Result: v3Result(id, vimDoc, "", nil)})
	v3RunAction(t, p, "response.jq")
	if p.vim.f.Focused() {
		t.Fatal("two cursors: the body must be blurred while the jq input has the keyboard")
	}
	if p.VimMode() != "INSERT" {
		t.Fatalf("the jq input reports its mode, got %q", p.VimMode())
	}
}

func TestVimBodySelectionIsDistinguishableWithoutColour(t *testing.T) {
	th := ui.NewTheme(true, true, false) // NO_COLOR
	p, id, _ := v3Pane(t, nil, th)
	v3Pump(t, p, tui.SendResultMsg{Result: v3Result(id, vimDoc, "", nil)})
	plain := p.View()
	vimKeys(t, p, "j", "V", "j")
	if p.View() == plain {
		t.Fatal("a visual selection must change the frame even without colour")
	}
}

func TestVimBodyBinaryAndEmptyBodiesKeepTheirViews(t *testing.T) {
	p, id, _ := v3Pane(t, nil, v3Theme())
	res := v3Result(id, "\x00\x01\x02binary", "", nil)
	res.Response.Headers = []model.KV{{Key: "Content-Type", Value: "application/octet-stream"}}
	v3Pump(t, p, tui.SendResultMsg{Result: res})
	if p.vimLive() || p.Editing() {
		t.Fatal("a binary body shows as a hex dump, not in the editor")
	}
	empty := v3Result(id, "", "", nil)
	v3Pump(t, p, tui.SendResultMsg{Result: empty})
	if p.vimLive() {
		t.Fatal("an empty body has no editor")
	}
}

func TestVimBodyThemeChangeRebuilds(t *testing.T) {
	p, _ := vimPane(t, vimDoc)
	old := p.vim
	v3Pump(t, p, tui.ThemeChangedMsg{Theme: ui.NewTheme(false, false, false)})
	if p.vim == old || !p.vimLive() {
		t.Fatal("a theme change should rebuild the buffer with the new styles")
	}
}

func TestVimBodyModeLabelFollowsTheEditor(t *testing.T) {
	p, _ := vimPane(t, vimDoc)
	var _ tui.ModeReporter = p
	for keys, want := range map[string]string{"v": "VISUAL", "esc": "NORMAL", "V": "VISUAL LINE"} {
		vimKeys(t, p, keys)
		if p.VimMode() != want {
			t.Fatalf("after %q: mode %q, want %q", keys, p.VimMode(), want)
		}
	}
	if vimtext.Normal.String() != "NORMAL" {
		t.Fatal("unexpected mode name")
	}
}

func TestVimBodyNeedsAResponse(t *testing.T) {
	p, _, _ := v3Pane(t, nil, v3Theme())
	if p.vimLive() || p.Editing() || p.VimMode() != "" {
		t.Fatal("no response, no editor")
	}
}

func keyOf(s string) tea.KeyPressMsg {
	switch s {
	case "space":
		return tea.KeyPressMsg{Code: ' ', Text: " "}
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}
	case "shift+tab":
		return tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}
	}
	if strings.HasPrefix(s, "ctrl+") {
		return tea.KeyPressMsg{Code: []rune(s)[5], Mod: tea.ModCtrl}
	}
	return v3Key(s)
}

func TestVimBodyDotOpensThePathFilter(t *testing.T) {
	p, _ := vimPane(t, vimDoc)
	// The path box is a textinput whose blink command never ends, so the
	// commands are not run here.
	p.Update(keyOf("."))
	if !p.pathActive || p.VimMode() != "INSERT" {
		t.Fatalf("path box: active %v mode %q", p.pathActive, p.VimMode())
	}
	for _, r := range ".name" {
		p.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	p.Update(v3Key("enter"))
	if p.pathActive || p.vim.f.Text() != `"alice"` {
		t.Fatalf("the buffer should hold the filtered value, got %q", p.vim.f.Text())
	}
}

// mediumJSON is a JSON body above vimInlineMax and below the large-body
// threshold.
func mediumJSON(tag string) string {
	return `{"id":"` + tag + `","items":[` + strings.Repeat(`{"id":"abcdef","n":1},`, vimInlineMax/20) + `{}]}`
}

func TestVimBodyMediumJSONIsFormattedInTheCommand(t *testing.T) {
	p, id, _ := v3Pane(t, nil, v3Theme())
	_, cmd := p.Update(tui.SendResultMsg{Result: v3Result(id, mediumJSON("one"), "", nil)})
	frame := v3Frame(p)
	if !strings.Contains(frame, "Formatting…") {
		t.Fatalf("a waiting state should say the body is being formatted:\n%s", frame)
	}
	v3Run(t, p, cmd)
	if !p.vimLive() || strings.Contains(v3Frame(p), "Formatting…") {
		t.Fatal("the formatted buffer should replace the waiting state")
	}
}

func TestVimBodySupersededBuildStopsBeforeTheHighlighter(t *testing.T) {
	p, id, _ := v3Pane(t, nil, v3Theme())
	_, first := p.Update(tui.SendResultMsg{Result: v3Result(id, mediumJSON("one"), "", nil)})
	_, second := p.Update(tui.SendResultMsg{Result: v3Result(id, mediumJSON("two"), "", nil)})
	built, ok := first().(vimBuiltMsg)
	if !ok || built.v != nil {
		t.Fatalf("a superseded build must return without a buffer, got %+v ok=%v", built.v != nil, ok)
	}
	if built, ok := second().(vimBuiltMsg); !ok || built.v == nil {
		t.Fatal("the current build must produce its buffer")
	}
}

func footerHas(p *Pane, key string) bool {
	for _, h := range p.KeyHints() {
		if h.Key == key {
			return true
		}
	}
	return false
}

func TestVimBodyFooterSaysVimKeysAreLimitedInEveryFallback(t *testing.T) {
	// Binary body: hex dump.
	p, id, _ := v3Pane(t, nil, v3Theme())
	res := v3Result(id, "\x00\x01\x02binary", "", nil)
	res.Response.Headers = []model.KV{{Key: "Content-Type", Value: "application/octet-stream"}}
	v3Pump(t, p, tui.SendResultMsg{Result: res})
	if !footerHas(p, "Binary body: vim keys limited") {
		t.Fatalf("binary footer: %v", p.KeyHints())
	}
	if footerHas(p, "w") {
		t.Fatal("a hex dump has no wrap; the footer must not offer w")
	}

	// jq output.
	q, qid, _ := v3Pane(t, nil, v3Theme())
	v3Pump(t, q, tui.SendResultMsg{Result: v3Result(qid, vimDoc, "", nil)})
	v3ApplyFilter(t, q, ".name")
	if !footerHas(q, "jq output: vim keys limited") {
		t.Fatalf("jq footer: %v", q.KeyHints())
	}
	if footerHas(q, "w") {
		t.Fatal("the jq view does not wrap; the footer must not offer w")
	}

	// Large text body keeps its note and may wrap.
	big := strings.Repeat("a line of plain text\n", largeBodyThreshold/21+10)
	r, rid, _ := v3Pane(t, nil, v3Theme())
	lres := v3Result(rid, big, "", nil)
	lres.Response.Headers = []model.KV{{Key: "Content-Type", Value: "text/plain"}}
	v3Pump(t, r, tui.SendResultMsg{Result: lres})
	if !footerHas(r, largeBodyNote) || !footerHas(r, "w") {
		t.Fatalf("large footer: %v", r.KeyHints())
	}
}

func TestSendingElapsedIsStampedInUpdate(t *testing.T) {
	p, id, _ := v3Pane(t, nil, v3Theme())
	p.Update(tui.SendStartedMsg{RequestID: id})
	p.sendStarted = p.sendStarted.Add(-3 * time.Second)
	p.Update(elapsedTickMsg{})
	if p.sendElapsed < 3*time.Second {
		t.Fatalf("the tick should stamp the elapsed time, got %v", p.sendElapsed)
	}
	before := p.View()
	time.Sleep(150 * time.Millisecond)
	if p.View() != before {
		t.Fatal("View must not read the clock")
	}
}
