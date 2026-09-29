package request

import (
	"errors"
	"testing"
	"time"

	"github.com/shubham/postctl/internal/tui"
)

func TestAutosaveDebounce(t *testing.T) {
	h := newHarness(t)
	h.key("up", "end")
	h.typ("a")
	if n, last := h.dirty(); n != 1 || !last {
		t.Fatalf("dirty msgs = %d last=%v, want one dirty=true", n, last)
	}
	h.advance(300 * time.Millisecond)
	h.typ("b") // restarts the debounce
	h.advance(300 * time.Millisecond)
	if len(h.saves()) != 0 {
		t.Fatal("saved before the debounce elapsed")
	}
	h.advance(250 * time.Millisecond)
	saves := h.saves()
	if len(saves) != 1 {
		t.Fatalf("saves = %d, want 1", len(saves))
	}
	if saves[0].Kind != tui.SaveRequest || saves[0].ID != h.req.ID || saves[0].Collection != h.coll.ID {
		t.Fatalf("save = %+v", saves[0])
	}
	// Still dirty until the shell reports the write.
	h.send(tui.SaveDoneMsg{Kind: tui.SaveRequest, ID: h.req.ID})
	if n, last := h.dirty(); n != 2 || last {
		t.Fatalf("dirty msgs = %d last=%v, want cleared", n, last)
	}
	h.mustHave("Saved")
}

func TestAutosaveOnBlur(t *testing.T) {
	h := newHarness(t)
	h.key("up", "end")
	h.typ("a")
	h.p.Blur()
	h.run(h.p.BlurCmd())
	if len(h.saves()) != 1 {
		t.Fatalf("blur saves = %d", len(h.saves()))
	}
	// The pending timer must not save a second time.
	h.advance(time.Second)
	if len(h.saves()) != 1 {
		t.Fatalf("saves after timer = %d", len(h.saves()))
	}
}

func TestSelectionChangeFlushesEdits(t *testing.T) {
	h := newHarness(t)
	h.key("up", "end")
	h.typ("a")
	other := h.req.Clone()
	other.ID = "other"
	h.send(tui.SelectionMsg{Ref: tui.RequestRef{Collection: h.coll, Request: other}})
	saves := h.saves()
	if len(saves) != 1 || saves[0].ID != h.req.ID {
		t.Fatalf("saves = %+v", saves)
	}
}

func TestSaveFailureIsShown(t *testing.T) {
	h := newHarness(t)
	h.key("up", "end")
	h.typ("a")
	h.advance(time.Second)
	h.send(tui.SaveDoneMsg{Kind: tui.SaveRequest, ID: h.req.ID, Err: errors.New("disk full")})
	h.mustHave("Not saved")
	if n, last := h.dirty(); n != 1 || !last {
		t.Fatal("request must stay dirty after a failed save")
	}
}

func TestEditsAfterFlushSaveAgain(t *testing.T) {
	h := newHarness(t)
	h.key("up", "end")
	h.typ("a")
	h.advance(time.Second)
	h.typ("b")
	h.send(tui.SaveDoneMsg{Kind: tui.SaveRequest, ID: h.req.ID}) // covers only "a"
	if _, last := h.dirty(); !last {
		t.Fatal("dirty cleared although a newer edit is pending")
	}
	h.advance(time.Second)
	if len(h.saves()) != 2 {
		t.Fatalf("saves = %d, want 2", len(h.saves()))
	}
}

func TestUndoRedoStructuralSteps(t *testing.T) {
	h := newHarness(t)
	orig := h.req.URL.Raw
	h.key("down", "space") // disable limit
	h.key("down", "d")     // delete debug
	if len(h.req.URL.Query) != 1 {
		t.Fatalf("query = %v", h.req.URL.Query)
	}
	h.key("ctrl+z")
	if len(h.req.URL.Query) != 2 || !h.req.URL.Query[0].Disabled {
		t.Fatalf("after 1st undo: %v", h.req.URL.Query)
	}
	h.key("ctrl+z")
	if h.req.URL.Raw != orig || h.req.URL.Query[0].Disabled {
		t.Fatalf("after 2nd undo raw=%q query=%v", h.req.URL.Raw, h.req.URL.Query)
	}
	h.key("ctrl+y")
	if !h.req.URL.Query[0].Disabled {
		t.Fatal("redo did not re-disable")
	}
	h.key("ctrl+y")
	if len(h.req.URL.Query) != 1 {
		t.Fatal("redo did not re-delete")
	}
	h.key("ctrl+y")
	h.mustHave("Nothing to redo")
}

func TestUndoCoalescesTyping(t *testing.T) {
	h := newHarness(t)
	h.key("up", "end")
	h.typ("abc")
	h.key("ctrl+z")
	if h.req.URL.Raw != "{{baseUrl}}/v1/charges/:id?limit=10&debug=1" {
		t.Fatalf("one undo should remove the whole burst, raw = %q", h.req.URL.Raw)
	}
	if h.p.url.Text() != h.req.URL.Raw {
		t.Fatalf("URL field %q out of sync", h.p.url.Text())
	}
	h.advance(2 * time.Second)
	h.typ("x")
	h.advance(2 * time.Second)
	h.typ("y")
	h.key("ctrl+z")
	if got := h.req.URL.Raw; got != "{{baseUrl}}/v1/charges/:id?limit=10&debug=1x" {
		t.Fatalf("pauses split undo steps, raw = %q", got)
	}
}

func TestUndoMarksDirtyAndSaves(t *testing.T) {
	h := newHarness(t)
	h.key("down", "space")
	h.advance(time.Second)
	h.send(tui.SaveDoneMsg{Kind: tui.SaveRequest, ID: h.req.ID})
	h.key("ctrl+z")
	h.advance(time.Second)
	if len(h.saves()) != 2 {
		t.Fatalf("undo must autosave, saves = %d", len(h.saves()))
	}
}

func TestNothingToUndo(t *testing.T) {
	h := newHarness(t)
	h.key("ctrl+z")
	h.mustHave("Nothing to undo")
}

func TestExternalReloadKeepsUnsavedEdits(t *testing.T) {
	h := newHarness(t)
	h.key("down", "space") // unsaved edit: limit disabled
	fresh := h.req.Clone()
	fresh.Method = "GET"
	fresh.URL = newRequest().URL // the disk version, without the edit
	h.req = fresh
	h.send(tui.WorkspaceReloadedMsg{Workspace: h.ws})
	h.send(tui.SelectionMsg{Ref: h.ref()})
	if !fresh.URL.Query[0].Disabled {
		t.Fatal("unsaved edit lost on reload")
	}
	if h.p.req != fresh {
		t.Fatal("pane still holds the old pointer")
	}
}

func TestReloadWithoutEditsTakesNewContent(t *testing.T) {
	h := newHarness(t)
	fresh := newRequest()
	fresh.ID = h.req.ID
	fresh.Method = "PUT"
	h.req = fresh
	h.send(tui.SelectionMsg{Ref: h.ref()})
	h.mustHave("PUT")
}
