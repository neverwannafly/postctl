package response

import (
	"strings"
	"testing"

	"github.com/shubham/postctl/internal/history"
	"github.com/shubham/postctl/internal/tui"
)

// foldDoc pretty-prints to:
//
//	0 {
//	1   "a": {
//	2     "b": 1,
//	3     "c": 2
//	4   },
//	5   "d": [
//	6     1,
//	7     2,
//	8     3
//	9   ],
//	10  "e": "tail"
//	11 }
const foldDoc = `{"a":{"b":1,"c":2},"d":[1,2,3],"e":"tail"}`

func foldPane(t *testing.T) (*Pane, *[]string) {
	t.Helper()
	return vimPane(t, foldDoc)
}

func foldText(p *Pane) string { return p.vim.f.Text() }

func TestFoldZCClosesTheNodeUnderTheCursorAndZOOpensIt(t *testing.T) {
	p, _ := foldPane(t)
	full := foldText(p)
	vimKeys(t, p, "j", "j") // on "b": 1, inside node a
	vimKeys(t, p, "z", "c")
	got := foldText(p)
	if !strings.Contains(got, `  "a": {…} 2 keys,`) || strings.Contains(got, `"b": 1`) {
		t.Fatalf("zc should fold node a into one line, got:\n%s", got)
	}
	if row, _ := vimPos(p); row != 1 {
		t.Fatalf("the cursor should land on the folded line, row %d", row)
	}
	vimKeys(t, p, "z", "o")
	if foldText(p) != full {
		t.Fatalf("zo should open it again, got:\n%s", foldText(p))
	}
	if row, _ := vimPos(p); row != 1 {
		t.Fatalf("zo keeps the cursor on the node's first line, row %d", row)
	}
}

func TestFoldZAToggles(t *testing.T) {
	p, _ := foldPane(t)
	full := foldText(p)
	vimKeys(t, p, "j", "z", "a")
	if !strings.Contains(foldText(p), "{…} 2 keys,") {
		t.Fatalf("za on the first line of a node should fold it:\n%s", foldText(p))
	}
	vimKeys(t, p, "z", "a")
	if foldText(p) != full {
		t.Fatal("za on a folded line should open it")
	}
	// On the closing line the node is the same node.
	vimKeys(t, p, "j", "j", "j", "z", "a")
	if !strings.Contains(foldText(p), "{…} 2 keys,") {
		t.Fatalf("za on the closing brace should fold the node:\n%s", foldText(p))
	}
}

func TestFoldArraysCountItems(t *testing.T) {
	p, _ := foldPane(t)
	vimKeys(t, p, "5", "j", "z", "c")
	if !strings.Contains(foldText(p), `  "d": […] 3 items,`) {
		t.Fatalf("an array folds to its item count:\n%s", foldText(p))
	}
}

func TestFoldZCOnAFoldedNodeClosesTheEnclosingOne(t *testing.T) {
	p, _ := foldPane(t)
	vimKeys(t, p, "j", "z", "c") // node a folded, cursor on it
	vimKeys(t, p, "z", "c")      // the enclosing node is the root
	if foldText(p) != "{…} 3 keys" {
		t.Fatalf("zc on a folded node closes its parent, got:\n%s", foldText(p))
	}
}

func TestFoldZMAndZR(t *testing.T) {
	p, _ := foldPane(t)
	full := foldText(p)
	vimKeys(t, p, "j", "j", "z", "M")
	if got := foldText(p); strings.Count(got, "\n") != 0 || !strings.Contains(got, "{…}") {
		t.Fatalf("zM closes every node, leaving the root summary:\n%s", got)
	}
	if row, _ := vimPos(p); row != 0 {
		t.Fatalf("the cursor sits on the only line, row %d", row)
	}
	vimKeys(t, p, "z", "R")
	if foldText(p) != full {
		t.Fatalf("zR opens every node, got:\n%s", foldText(p))
	}
}

func TestFoldYankCopiesTheVisibleTextAndStaysMasked(t *testing.T) {
	body := `{"a":{"b":1,"token":"unlisted-credential-value"},"e":"` + v3Secret + `"}`
	p, clip := vimPane(t, body)
	vimKeys(t, p, "j", "z", "c")
	*clip = nil
	vimKeys(t, p, "y", "y")
	if len(*clip) != 1 || !strings.Contains((*clip)[0], "{…} 2 keys") || strings.Contains((*clip)[0], "token") {
		t.Fatalf("yy on a folded line copies what is shown, got %q", *clip)
	}
	*clip = nil
	vimKeys(t, p, "z", "R", "g", "g", "V", "G", "y")
	if len(*clip) != 1 || strings.Contains((*clip)[0], "unlisted-credential-value") || strings.Contains((*clip)[0], v3Secret) {
		t.Fatalf("yank after opening leaked: %q", *clip)
	}
	if !strings.Contains((*clip)[0], history.Mask) {
		t.Fatal("expected the mask in the yank")
	}
}

func TestFoldKeysAreNotForwardedAndOtherZKeysAreInert(t *testing.T) {
	p, _ := foldPane(t)
	for _, k := range []string{"z", "x", "z", "z", "z", "t", "z", "esc"} {
		msgs := v3Pump(t, p, keyOf(k))
		for _, m := range msgs {
			if _, ok := m.(tui.ForwardKeyMsg); ok {
				t.Fatalf("key %q after z reached the shell", k)
			}
		}
	}
	if foldText(p) != p.vim.doc || strings.Contains(foldText(p), "…") {
		t.Fatal("unknown z keys must not fold")
	}
	if p.VimMode() != "NORMAL" {
		t.Fatalf("mode %q", p.VimMode())
	}
}

func TestFoldDoesNotApplyToRawOrOtherBodies(t *testing.T) {
	p, _ := foldPane(t)
	vimKeys(t, p, "r") // raw: one line
	before := foldText(p)
	vimKeys(t, p, "z", "M", "z", "c")
	if foldText(p) != before {
		t.Fatal("raw text has no nodes to fold")
	}
	vimKeys(t, p, "r")
	if strings.Contains(foldText(p), "…") {
		t.Fatal("returning to pretty must not bring old folds back")
	}
}

func TestFoldStateResetsWithANewResponse(t *testing.T) {
	p, _ := foldPane(t)
	vimKeys(t, p, "z", "M")
	id := p.ref.Request.ID
	v3Pump(t, p, tui.SendResultMsg{Result: v3Result(id, foldDoc, "", nil)})
	if strings.Contains(foldText(p), "…") {
		t.Fatal("a new response starts unfolded")
	}
}

func TestFoldPendingZIsShownAndEscClearsIt(t *testing.T) {
	p, _ := foldPane(t)
	vimKeys(t, p, "z")
	if !p.foldPending {
		t.Fatal("z should wait for its second key")
	}
	vimKeys(t, p, "esc")
	if p.foldPending {
		t.Fatal("Esc should drop the prefix")
	}
	// And Esc did not also leave the body.
	vimKeys(t, p, "z")
	for _, m := range v3Pump(t, p, v3Key("esc")) {
		if _, ok := m.(tui.FocusPaneMsg); ok {
			t.Fatal("the Esc that cancels z must not leave the body")
		}
	}
}

func TestFoldViewIsPure(t *testing.T) {
	p, _ := foldPane(t)
	vimKeys(t, p, "j", "z", "c", "z")
	a, b := p.View(), p.View()
	if a != b {
		t.Fatal("View changed between calls")
	}
}
