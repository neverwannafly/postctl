package tui

import "testing"

func TestFoldStatePersistsAndRestores(t *testing.T) {
	h := newHarness(t, demo(), 140, 30)
	h.selectByName("Charges")
	h.mustHave("List charges")
	h.keys("left") // fold Charges
	h.mustNotHave("List charges")
	if h.a.foldSeq == 0 {
		t.Fatal("folding did not schedule a persist")
	}
	h.msg(foldPersistMsg{seq: h.a.foldSeq})
	saved := h.be.savedState()
	if len(saved.Folded) != 1 {
		t.Fatalf("Folded = %v, want the one folded folder", saved.Folded)
	}

	// A new session starting from that state has Charges folded and Customers open.
	be := newFake(demo())
	be.ws = h.be.ws
	be.state = saved
	h2 := newHarnessWith(t, be, 140, 30)
	h2.mustNotHave("List charges")
	h2.mustHave("Create customer")
}

func TestFoldPersistIsDebounced(t *testing.T) {
	h := newHarness(t, demo(), 140, 30)
	h.selectByName("Charges")
	h.keys("left")
	first := h.a.foldSeq
	h.keys("right", "left")
	if h.a.foldSeq == first {
		t.Fatal("expected a newer debounce sequence")
	}
	h.msg(foldPersistMsg{seq: first}) // stale timer: ignored
	if len(h.be.savedState().Folded) != 0 {
		t.Fatal("a stale timer must not write")
	}
}
