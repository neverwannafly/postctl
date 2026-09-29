package tui

import (
	"strings"
	"testing"

	"github.com/shubham/postctl/internal/model"
)

// names lists the visible tree row labels in order.
func names(h *harness) []string {
	var out []string
	for _, r := range h.a.tree.rows {
		out = append(out, r.label)
	}
	return out
}

func find(c *model.Collection, name string) model.Item {
	var found model.Item
	_ = c.Walk(func(it model.Item, _ model.Container, _ int) error {
		if it.ItemName() == name {
			found = it
		}
		return nil
	})
	return found
}

func TestTreeFoldUnfoldAndNavigation(t *testing.T) {
	h := newHarness(t, demo(), 160, 30)
	h.selectByName("Charges")
	h.keys("left") // fold
	h.mustNotHave("List charges")
	h.keys("right") // unfold
	h.mustHave("List charges")
	h.keys("right") // step into the first child
	if r, _ := h.a.tree.current(); r.label != "List charges" {
		t.Fatalf("right on an open folder steps in, cursor on %q", r.label)
	}
	h.keys("left") // to parent
	if r, _ := h.a.tree.current(); r.label != "Charges" {
		t.Fatalf("left on a request goes to its folder, cursor on %q", r.label)
	}
	h.keys("space")
	h.mustNotHave("List charges")
	h.keys("enter") // enter toggles folders too
	h.mustHave("List charges")
	h.keys("home")
	if r, _ := h.a.tree.current(); r.label != "Payments" {
		t.Fatalf("home -> %q", r.label)
	}
	h.keys("end")
	if r, _ := h.a.tree.current(); r.label != "Preflight" {
		t.Fatalf("end -> %q", r.label)
	}
	h.keys("pgup")
	if r, _ := h.a.tree.current(); r.label == "Preflight" {
		t.Fatal("pgup did not move")
	}
}

func TestTreeShowsMethodBadgesAsText(t *testing.T) {
	h := newHarness(t, demo(), 160, 30, func(o *Options) { o.NoColor = true })
	for _, want := range []string{"GET   List charges", "POST  Create charge", "DEL   Refund charge", "PATCH Update charge", "PUT   Replace customer", "OPT   Preflight"} {
		h.mustHave(want)
	}
}

func TestTreeFilter(t *testing.T) {
	h := newHarness(t, demo(), 160, 30)
	h.keys("/")
	h.typeText("cust")
	rows := names(h)
	if len(rows) != 3 {
		t.Fatalf("filter 'cust' matched %v", rows)
	}
	for _, r := range h.a.tree.rows {
		if !strings.Contains(strings.ToLower(r.label), "customer") {
			t.Errorf("unexpected match %q", r.label)
		}
	}
	// Results show path context so identical names can be told apart.
	h.mustHave("Customers › Get customer") // results carry their path (the sidebar keeps the tail)
	h.keys("down")
	if r, _ := h.a.tree.current(); !strings.HasSuffix(r.label, "Create customer") {
		t.Fatalf("down moved to %q", r.label)
	}
	sel := h.a.tree.sel
	h.keys("enter") // keep results, hand the keyboard back
	if h.a.tree.filtering {
		t.Fatal("enter leaves filter typing")
	}
	if h.a.tree.sel != sel {
		t.Fatal("selection must survive applying the filter")
	}
	h.keys("enter") // now opens the request
	if h.a.focus != SlotRequest {
		t.Fatalf("enter on a result opens it, focus=%v", h.a.focus)
	}
	h.keys("1", "esc") // clear the kept filter
	if h.a.tree.query != "" {
		t.Fatal("esc clears the filter")
	}
	if h.a.tree.sel != sel {
		t.Fatal("selection must survive clearing the filter")
	}
	if r, _ := h.a.tree.current(); r.label == "" || r.node == nil || r.node.id != sel {
		t.Fatalf("cursor should sit on the request that was selected, on %q", r.label)
	}
}

func TestTreeFilterNoMatchExplains(t *testing.T) {
	h := newHarness(t, demo(), 160, 30)
	h.keys("/")
	h.typeText("zzzz")
	h.mustHave("No matches")
	h.mustHave("Esc clears the filter")
	h.keys("esc")
	h.mustHave("List charges")
}

func TestFuzzyRanking(t *testing.T) {
	cases := []struct{ q, better, worse string }{
		{"cc", "Create charge", "Cancel subscription plan"},
		{"ref", "Refund charge", "Create customer reference"},
		{"list", "List users", "Get list of things"},
	}
	for _, c := range cases {
		a, _, ok1 := fuzzyMatch(c.q, c.better)
		b, _, ok2 := fuzzyMatch(c.q, c.worse)
		if !ok1 || !ok2 || a <= b {
			t.Errorf("%q: %q (%d) should outrank %q (%d)", c.q, c.better, a, c.worse, b)
		}
	}
	if _, _, ok := fuzzyMatch("xyz", "Create charge"); ok {
		t.Error("xyz must not match")
	}
	if _, pos, ok := fuzzyMatch("crc", "Create charge"); !ok || len(pos) != 3 {
		t.Errorf("positions for highlight: %v", pos)
	}
}

func TestCreateRequestInFolderPersists(t *testing.T) {
	h := newHarness(t, demo(), 160, 30)
	h.selectByName("Charges")
	h.keys("n")
	h.mustHave("New")
	h.mustHave("in Payments › Charges")
	h.typeText("Capture charge")
	h.keys("enter")
	if h.a.ov != nil {
		t.Fatalf("prompt should close, ov=%T", h.a.ov)
	}
	coll := h.be.last[h.a.ws.Collections[0].ID]
	if coll == nil {
		t.Fatal("collection was not saved")
	}
	it := find(coll, "Capture charge")
	r, ok := it.(*model.Request)
	if !ok || r.Method != "GET" {
		t.Fatalf("saved item = %#v", it)
	}
	if got := len(coll.Items[0].(*model.Folder).Items); got != 5 {
		t.Fatalf("Charges has %d items", got)
	}
	if h.a.focus != SlotRequest {
		t.Fatalf("a new request opens for editing, focus=%v", h.a.focus)
	}
	h.mustHave("Capture charge")
	if h.a.tree.sel != r.ID {
		t.Fatal("the new request is selected")
	}
}

func TestCreateRequestAfterSelectedRequest(t *testing.T) {
	h := newHarness(t, demo(), 160, 30)
	h.selectByName("Create charge")
	h.keys("n")
	h.typeText("Right after")
	h.keys("enter")
	coll := h.be.last[h.a.ws.Collections[0].ID]
	f := coll.Items[0].(*model.Folder)
	if f.Items[2].ItemName() != "Right after" {
		t.Fatalf("new request should follow the selected one: %v", itemNames(f.Items))
	}
}

func itemNames(items []model.Item) []string {
	var out []string
	for _, it := range items {
		out = append(out, it.ItemName())
	}
	return out
}

func TestCreateFolderAndCollection(t *testing.T) {
	h := newHarness(t, demo(), 160, 30)
	h.selectByName("Payments")
	h.keys("n", "tab") // Request -> Folder
	h.mustHave("(•) Folder")
	h.typeText("Webhooks")
	h.keys("enter")
	coll := h.be.last[h.a.ws.Collections[0].ID]
	if _, ok := find(coll, "Webhooks").(*model.Folder); !ok {
		t.Fatal("folder not saved")
	}
	if h.a.focus != SlotSidebar {
		t.Fatal("creating a folder keeps focus in the tree")
	}

	h.keys("n", "tab", "tab") // -> Collection
	h.mustHave("(•) Collection")
	h.typeText("Reports")
	h.keys("enter")
	if len(h.a.ws.Collections) != 3 || h.a.ws.Collections[2].Name != "Reports" {
		t.Fatalf("collections: %v", len(h.a.ws.Collections))
	}
	if h.be.last[h.a.ws.Collections[2].ID] == nil {
		t.Fatal("new collection not saved")
	}
}

func TestPromptValidatesName(t *testing.T) {
	h := newHarness(t, demo(), 160, 30)
	h.keys("n", "enter")
	h.mustHave("Enter a name.")
	h.keys("esc")
	if h.a.ov != nil || len(h.be.saved) != 0 {
		t.Fatal("cancel must not write anything")
	}
}

func TestRenamePersists(t *testing.T) {
	h := newHarness(t, demo(), 160, 30)
	h.selectByName("Create charge")
	id := h.a.tree.sel
	h.keys("r")
	h.mustHave("Rename request")
	h.keys("ctrl+a") // start of line
	h.typeText("Make ")
	h.keys("enter")
	coll := h.be.last[h.a.ws.Collections[0].ID]
	if got := find(coll, "Make Create charge"); got == nil || got.ItemID() != id {
		t.Fatal("rename not saved with the same ID")
	}
	if h.a.tree.sel != id {
		t.Fatal("selection follows the renamed item")
	}
	h.mustHave("Make Create charge")
}

func TestDuplicateGivesFreshIDs(t *testing.T) {
	h := newHarness(t, demo(), 160, 30)
	h.selectByName("Charges")
	orig := h.a.tree.sel
	h.keys("D")
	coll := h.be.last[h.a.ws.Collections[0].ID]
	cp, ok := find(coll, "Charges copy").(*model.Folder)
	if !ok {
		t.Fatal("copy missing")
	}
	if cp.ID == orig {
		t.Fatal("copy reuses the ID")
	}
	seen := map[model.ID]bool{}
	_ = coll.Walk(func(it model.Item, _ model.Container, _ int) error {
		if seen[it.ItemID()] {
			t.Errorf("duplicate ID %s (%s)", it.ItemID(), it.ItemName())
		}
		seen[it.ItemID()] = true
		return nil
	})
	if len(cp.Items) != 4 {
		t.Fatalf("copy has %d items", len(cp.Items))
	}
	if h.a.tree.sel != cp.ID {
		t.Fatal("the copy is selected")
	}
	h.mustHave("Duplicated Charges")
}

func TestMoveWithinAndAcrossCollections(t *testing.T) {
	h := newHarness(t, demo(), 160, 30)
	h.selectByName("Health")
	h.keys("m")
	h.mustHave("Move Health to")
	h.mustHave("Payments › Customers")
	h.mustNotHave("Payments  ") // the current parent is not offered
	h.typeText("custo")
	h.keys("enter")
	pay := h.be.last[h.a.ws.Collections[0].ID]
	cust := find(pay, "Customers").(*model.Folder)
	if cust.Items[len(cust.Items)-1].ItemName() != "Health" {
		t.Fatalf("Health should be last in Customers: %v", itemNames(cust.Items))
	}

	// Across collections: written destination first, then source.
	h.selectByName("Health")
	h.keys("m")
	h.typeText("Internal")
	h.keys("enter")
	if len(h.be.saved) < 2 {
		t.Fatalf("expected two saves, got %v", h.be.saved)
	}
	admin, payment := h.a.ws.Collections[1].ID, h.a.ws.Collections[0].ID
	n := len(h.be.saved)
	if h.be.saved[n-2] != admin || h.be.saved[n-1] != payment {
		t.Fatalf("destination must be written before the source: %v", h.be.saved[n-2:])
	}
	if find(h.be.last[payment], "Health") != nil || find(h.be.last[admin], "Health") == nil {
		t.Fatal("Health should have moved to Internal admin on disk")
	}
	if h.a.tree.sel == "" {
		t.Fatal("selection lost")
	}
}

func TestCannotMoveFolderIntoItself(t *testing.T) {
	h := newHarness(t, demo(), 160, 30)
	h.selectByName("Charges")
	h.keys("m")
	h.mustNotHave("Payments › Charges")
	h.keys("esc")
}

func TestReorderWithinFolder(t *testing.T) {
	h := newHarness(t, demo(), 160, 30)
	h.selectByName("Create charge")
	h.keys("alt+up")
	coll := h.be.last[h.a.ws.Collections[0].ID]
	got := itemNames(coll.Items[0].(*model.Folder).Items)
	if got[0] != "Create charge" || got[1] != "List charges" {
		t.Fatalf("order %v", got)
	}
	h.keys("alt+down", "alt+down")
	got = itemNames(h.be.last[h.a.ws.Collections[0].ID].Items[0].(*model.Folder).Items)
	if got[2] != "Create charge" {
		t.Fatalf("order %v", got)
	}
}

func TestDeleteDefaultsToCancel(t *testing.T) {
	h := newHarness(t, demo(), 160, 30)
	h.selectByName("Refund charge")
	h.keys("d")
	h.mustHave("Delete request")
	h.mustHave("Refund charge")
	h.mustHave("[ Cancel ]")
	if c, ok := h.a.ov.(*confirmOverlay); !ok || c.c.Accepted() || !c.c.Danger {
		t.Fatal("a danger dialog starts on Cancel")
	}
	h.keys("enter") // Enter on Cancel does nothing destructive
	if h.a.ov != nil || len(h.be.saved) != 0 {
		t.Fatal("enter on Cancel must not delete")
	}
	if find(h.a.ws.Collections[0], "Refund charge") == nil {
		t.Fatal("request was deleted")
	}

	h.keys("d", "esc")
	if find(h.a.ws.Collections[0], "Refund charge") == nil || h.a.ov != nil {
		t.Fatal("escape cancels")
	}

	h.keys("d", "right", "enter")
	if find(h.a.ws.Collections[0], "Refund charge") != nil {
		t.Fatal("request should be gone")
	}
	saved := h.be.last[h.a.ws.Collections[0].ID]
	if find(saved, "Refund charge") != nil {
		t.Fatal("deletion not persisted")
	}
	if r, _ := h.a.tree.current(); r.label != "Update charge" {
		t.Fatalf("selection should move to the next sibling, on %q", r.label)
	}
}

func TestDeleteWithYThenEnter(t *testing.T) {
	h := newHarness(t, demo(), 160, 30)
	h.selectByName("Customers")
	h.keys("d")
	h.mustHave("and its 3 requests")
	h.keys("y")
	if find(h.a.ws.Collections[0], "Customers") == nil {
		t.Fatal("y alone must not delete")
	}
	h.keys("enter")
	if find(h.a.ws.Collections[0], "Customers") != nil {
		t.Fatal("folder should be deleted")
	}
}

func TestDeleteCollection(t *testing.T) {
	h := newHarness(t, demo(), 160, 30)
	h.selectByName("Internal admin")
	id := h.a.tree.sel
	h.keys("d", "right", "enter")
	if len(h.a.ws.Collections) != 1 {
		t.Fatal("collection should be removed")
	}
	if len(h.be.deleted) != 1 || h.be.deleted[0] != id {
		t.Fatalf("store delete not called: %v", h.be.deleted)
	}
}

func TestSaveFailureIsExplained(t *testing.T) {
	h := newHarness(t, demo(), 160, 30)
	h.be.saveErr = errBoom
	h.selectByName("Create charge")
	h.keys("D")
	h.mustHave("Could not save Payments")
	h.mustHave("disk on fire")
	h.mustHave("still on screen but not on disk")
	// An error toast is sticky until a key is pressed.
	h.keys("down")
	h.mustNotHave("Could not save Payments")
}

func TestSavesCoalesce(t *testing.T) {
	h := newHarness(t, demo(), 160, 30)
	h.be.gate = make(chan struct{})
	h.selectByName("Create charge")
	for i := 0; i < 5; i++ {
		h.a.Update(keyPress("D"))
	}
	// One save is running; the four others coalesced into one waiting op.
	if got := len(h.a.saver.ops); got != 2 {
		t.Fatalf("expected 2 queued ops (1 running + 1 coalesced), got %d", got)
	}
	close(h.be.gate)
}

func TestEditsNeverBlockOnDisk(t *testing.T) {
	h := newHarness(t, demo(), 160, 30)
	h.be.gate = make(chan struct{}) // the disk is stuck
	h.selectByName("Create charge")
	_, cmd := h.a.Update(keyPress("D"))
	if cmd == nil {
		t.Fatal("expected a save command")
	}
	// Update returned without waiting; the copy is already on screen.
	h.mustHave("Create charge copy")
	close(h.be.gate)
}
