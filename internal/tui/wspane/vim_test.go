package wspane

import (
	"strings"
	"testing"

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
