package response

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/shubham/postctl/internal/history"
	"github.com/shubham/postctl/internal/model"
	"github.com/shubham/postctl/internal/tui"
)

func TestDiffWithoutResponseIsRefusedWithReason(t *testing.T) {
	p, _, _, _ := v3PaneWithLog(t)
	shell := v3RunAction(t, p, "response.diff")
	toasts := v3Toasts(shell)
	if len(toasts) != 1 || !strings.Contains(toasts[0], "send the request first") {
		t.Fatalf("toasts = %q", toasts)
	}
	if p.diff.open {
		t.Fatal("the diff opened without a response")
	}
}

func TestDiffWithoutPreviousEntryIsRefusedWithReason(t *testing.T) {
	p, id, log, _ := v3PaneWithLog(t)
	log.entries = log.entries[:1] // only the send that produced the response
	v3Pump(t, p, tui.SendResultMsg{Result: v3Result(id, `{"n":3}`, "e3", nil)})
	shell := v3RunAction(t, p, "response.diff")
	toasts := v3Toasts(shell)
	if len(toasts) != 1 || !strings.Contains(toasts[0], "No earlier send") {
		t.Fatalf("toasts = %q", toasts)
	}
	if p.diff.open {
		t.Fatal("the diff stayed open with nothing to compare")
	}
}

func TestDiffWithoutPreviousKnownFromLoadedTabAnswersSynchronously(t *testing.T) {
	p, id, log, _ := v3PaneWithLog(t)
	log.entries = log.entries[:1]
	v3Pump(t, p, tui.SendResultMsg{Result: v3Result(id, `{"n":3}`, "e3", nil)})
	v3Pump(t, p, tui.FocusSectionMsg{Section: tui.SectionRespHistory})
	before := log.lists
	shell := v3RunAction(t, p, "response.diff")
	if log.lists != before {
		t.Fatal("a loaded History tab should answer without reading the log again")
	}
	if toasts := v3Toasts(shell); len(toasts) != 1 || !strings.Contains(toasts[0], "No earlier send") {
		t.Fatalf("toasts = %q", toasts)
	}
}

func TestDiffWithPreviousShowsChangesAndRedactsBothSides(t *testing.T) {
	p, id, log, _ := v3PaneWithLog(t)
	// History is stored redacted; the live body carries the secret value.
	log.full["e2"].Response.Body = `{"token":"` + history.Mask + `","n":2}`
	log.full["e2"].Response.Headers = []model.KV{{Key: "X-Api-Key", Value: history.Mask}, {Key: "X-Request", Value: "one"}}
	redactor := history.NewRedactor([]string{v3Secret})
	live := `{"token":"` + v3Secret + `","note":"` + v3Secret + `","n":3}`
	v3Pump(t, p, tui.SendResultMsg{Result: v3Result(id, live, "e3", redactor)})
	// e3 is the current send, so e2 is the previous one.
	v3RunAction(t, p, "response.diff")
	if !p.diff.open || p.diff.body == nil {
		t.Fatal("the diff did not open")
	}
	for _, reveal := range []bool{false, true} {
		p.revealSecrets = reveal
		for _, headers := range []bool{false, true} {
			p.diff.showBody = !headers
			out := v3Frame(p)
			if strings.Contains(out, v3Secret) {
				t.Fatalf("reveal=%v headers=%v: the diff frame shows a secret", reveal, headers)
			}
			if !strings.Contains(out, "Previous") || !strings.Contains(out, "Current") {
				t.Fatalf("diff summary lacks side labels:\n%s", out)
			}
		}
	}
	p.revealSecrets = false
	p.diff.showBody = true
	out := v3Frame(p)
	if !strings.Contains(out, "n") || !strings.Contains(out, "- ") || !strings.Contains(out, "+ ") {
		t.Fatalf("diff shows no changed lines:\n%s", out)
	}
	if !strings.Contains(out, "****") && !strings.Contains(out, "••••") {
		t.Fatalf("diff shows no mask glyph, so the redaction is not visible:\n%s", out)
	}
	p.diff.showBody = false
	if hdr := v3Frame(p); !strings.Contains(hdr, "headers") || !strings.Contains(hdr, "X-Request") {
		t.Fatalf("header diff:\n%s", hdr)
	}
}

func TestDiffUsesGlyphOfTheLook(t *testing.T) {
	p, id, log, _ := v3PaneWithLog(t)
	p.th = themeASCII()
	log.full["e2"].Response.Body = `{"token":"` + history.Mask + `"}`
	redactor := history.NewRedactor([]string{v3Secret})
	v3Pump(t, p, tui.SendResultMsg{Result: v3Result(id, `{"token":"`+v3Secret+`"}`, "e3", redactor)})
	v3RunAction(t, p, "response.diff")
	out := v3Frame(p)
	if strings.Contains(out, history.Mask) {
		t.Fatalf("ASCII look shows the Unicode mask:\n%s", out)
	}
}

func TestDiffDropsStaleGeneration(t *testing.T) {
	p, id, _, _ := v3PaneWithLog(t)
	v3Pump(t, p, tui.SendResultMsg{Result: v3Result(id, `{"n":3}`, "e3", nil)})
	p.Update(tui.FocusSectionMsg{Section: tui.SectionRespBody})
	cmd := p.startDiff("e2")
	stale := p.diffGen
	msg := cmd()
	p.closeDiff() // user pressed Esc: the result is now stale
	p.Update(msg)
	if p.diff.open || p.diff.body != nil {
		t.Fatal("a result for a closed diff reopened it")
	}
	_ = p.startDiff("e1")
	if p.diffGen == stale {
		t.Fatal("starting a diff must advance the generation")
	}
	p.Update(msg) // the first diff's result arrives late
	if p.diff.body != nil {
		t.Fatal("a stale result replaced the newer diff")
	}
}

func TestDiffClosedBySendOfSameRequestAndSelectionChange(t *testing.T) {
	p, id, _, _ := v3PaneWithLog(t)
	v3Pump(t, p, tui.SendResultMsg{Result: v3Result(id, `{"n":3}`, "e3", nil)})
	v3RunAction(t, p, "response.diff")
	if !p.diff.open {
		t.Fatal("diff not open")
	}
	v3Pump(t, p, tui.SendResultMsg{Result: v3Result(id, `{"n":4}`, "e4", nil)})
	if p.diff.open {
		t.Fatal("a new response must close the diff of the old one")
	}
	v3RunAction(t, p, "response.diff")
	v3Pump(t, p, tui.SelectionMsg{Ref: tui.RequestRef{Request: &model.Request{ID: model.NewID()}}})
	if p.diff.open {
		t.Fatal("selecting another request must close the diff")
	}
}

func TestDiffTooBigShowsMessage(t *testing.T) {
	p, id, log, _ := v3PaneWithLog(t)
	var a, b strings.Builder
	for i := 0; i < 3500; i++ {
		fmt.Fprintf(&a, "left line %d\n", i)
		fmt.Fprintf(&b, "right line %d\n", i)
	}
	log.full["e2"].Response.Body = a.String()
	res := v3Result(id, b.String(), "e3", nil)
	res.Response.Headers = []model.KV{{Key: "Content-Type", Value: "text/plain"}}
	v3Pump(t, p, tui.SendResultMsg{Result: res})
	v3RunAction(t, p, "response.diff")
	out := v3Frame(p)
	if !strings.Contains(out, "differ too much") {
		t.Fatalf("no too-big message:\n%s", out)
	}
	lines := strings.Split(out, "\n")
	if len(lines) > p.h {
		t.Fatalf("diff frame has %d lines for a pane of %d", len(lines), p.h)
	}
}

func TestDiffCapMessage(t *testing.T) {
	p, id, log, _ := v3PaneWithLog(t)
	var a strings.Builder
	for i := 0; i < 20010; i++ {
		fmt.Fprintf(&a, "same line %d\n", i)
	}
	log.full["e2"].Response.Body = a.String() + "left\n"
	res := v3Result(id, a.String()+"right\n", "e3", nil)
	res.Response.Headers = []model.KV{{Key: "Content-Type", Value: "text/plain"}}
	v3Pump(t, p, tui.SendResultMsg{Result: res})
	v3RunAction(t, p, "response.diff")
	if out := v3Frame(p); !strings.Contains(out, "truncated at 20,000 lines") {
		t.Fatalf("no cap message:\n%s", out)
	}
}

func TestDiffKeysScrollToggleAndClose(t *testing.T) {
	p, id, _, _ := v3PaneWithLog(t)
	v3Pump(t, p, tui.SendResultMsg{Result: v3Result(id, `{"n":3}`, "e3", nil)})
	v3RunAction(t, p, "response.diff")
	v3Pump(t, p, v3Key("h"))
	if p.diff.showBody {
		t.Fatal("h did not switch to headers")
	}
	v3Pump(t, p, v3Key("h"))
	if !p.diff.showBody {
		t.Fatal("h did not switch back to the body")
	}
	hints := p.KeyHints()
	var labels []string
	for _, h := range hints {
		labels = append(labels, h.Key)
	}
	if !reflect.DeepEqual(labels, []string{"n/N", "h", "j/k", "Esc"}) {
		t.Fatalf("diff hints = %v", labels)
	}
	v3Pump(t, p, v3Key("esc"))
	if p.diff.open {
		t.Fatal("Esc did not close")
	}
}

func TestDiffPreviousWhenCurrentWasNotRecordedUsesNewestEntry(t *testing.T) {
	items := []history.Entry{{ID: "a"}, {ID: "b"}}
	if e, ok := previousEntry(items, ""); !ok || e.ID != "a" {
		t.Fatalf("unrecorded current: %v %v", e.ID, ok)
	}
	if e, ok := previousEntry(items, "a"); !ok || e.ID != "b" {
		t.Fatalf("current a: %v %v", e.ID, ok)
	}
	if _, ok := previousEntry(items, "b"); ok {
		t.Fatal("the oldest entry has no previous")
	}
	if _, ok := previousEntry(items, "gone"); ok {
		t.Fatal("an entry missing from the log has no known previous")
	}
	if _, ok := previousEntry(nil, ""); ok {
		t.Fatal("an empty log has no previous")
	}
}

func TestDiffViewIsPure(t *testing.T) {
	p, id, _, _ := v3PaneWithLog(t)
	v3Pump(t, p, tui.SendResultMsg{Result: v3Result(id, `{"n":3}`, "e3", nil)})
	v3RunAction(t, p, "response.diff")
	scroll, hunk := p.diff.body.Scroll(), p.diff.body.HunkIndex()
	first, second := p.View(), p.View()
	if first != second || p.diff.body.Scroll() != scroll || p.diff.body.HunkIndex() != hunk {
		t.Fatal("View changed the diff")
	}
}

func TestDiffSideSummaryUsesFailedForErrors(t *testing.T) {
	s := diffSide("Previous", 0, true, 5, 6)
	if s.Status != "Failed" {
		t.Fatalf("status = %q", s.Status)
	}
}
