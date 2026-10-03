package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/shubham/postctl/internal/model"
	"github.com/shubham/postctl/internal/ui"
)

func TestCleanURLPath(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{"leading template with path and query", "{{baseUrl}}/a/b?x={{t}}", "/a/b"},
		{"scheme with userinfo host path query fragment", "https://user:pass@host.example.test/p?q=1#f", "/p"},
		{"schemeless host path with query", "host.example.test/p?q", "/p"},
		{"localhost with port and path", "localhost:8080/p", "/p"},
		{"localhost schemeless path", "localhost/p", "/p"},
		{"template only with query", "{{baseUrl}}?token=X", "/"},
		{"template only", "{{baseUrl}}", "/"},
		{"empty input", "", ""},
		{"whitespace input", "   ", ""},
		{"path with template variable", "/p/{{id}}/x", "/p/{{id}}/x"},
		{"schemeless host multi-segment path", "host.example.test/p/q?x=1", "/p/q"},
		{"schemeless host only", "host.example.test", "/"},
		{"localhost port only", "localhost:8080", "/"},
		{"template with ? inside variable", "{{base}}/p/{{a?b}}/c?x=1", "/p/{{a?b}}/c"},
		{"template with # inside variable", "{{base}}/p/{{a#b}}/c#frag", "/p/{{a#b}}/c"},
		{"schemeless host/path", "host/path", "/path"},
		{"schemeless localhost/p", "localhost/p", "/p"},
		{"schemeless api/v1", "api/v1", "/v1"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := cleanURLPath(tc.raw)
			if got != tc.want {
				t.Errorf("cleanURLPath(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

func TestSanitizeTextWithEmbeddedESC(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"ANSI color escapes", "\x1b[31mCharge\x1b[0m", "Charge"},
		{"OSC title sequence", "\x1b]0;title\x07Clean", "Clean"},
		{"C0 control characters", "User\x00Name\r\n", "UserName"},
		{"ANSI clear escape", "Bad\x1b[0mName", "BadName"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := sanitizeText(tc.in)
			if got != tc.want {
				t.Errorf("sanitizeText(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}

	// Verify sanitized text behaves properly inside switcher
	req := model.NewRequest("\x1b[31mDangerous\x1b[0m Name", "\x1b[35mPOST\x1b[0m", "https://api.test/\x1b[32musers\x1b[0m")
	ws := &model.Workspace{
		ID:   "ws-esc",
		Name: "ESC Test",
		Collections: []*model.Collection{
			{ID: "c1", Name: "\x1b[34mFolder\x1b[0m", Items: []model.Item{req}},
		},
	}
	hh := newHarness(t, ws, 80, 24)
	hh.msg(OpenSwitcherMsg{})
	screen := hh.screen()
	if strings.Contains(screen, "\x1b") {
		t.Fatalf("rendered switcher contains escape sequences: %q", screen)
	}
	if !strings.Contains(screen, "Dangerous Name") {
		t.Fatalf("rendered switcher missing stripped name: %q", screen)
	}
	if !strings.Contains(screen, "POST") {
		t.Fatalf("rendered switcher missing stripped method: %q", screen)
	}
	if !strings.Contains(screen, "/users") {
		t.Fatalf("rendered switcher missing stripped path: %q", screen)
	}

	// Method search matches sanitized method
	hh.keys("P", "O", "S", "T")
	screen = hh.screen()
	if !strings.Contains(screen, "1 of 1") {
		t.Fatalf("searching for method 'POST' should match, got:\n%s", screen)
	}
}

func TestSwitcherUnnamedRequest(t *testing.T) {
	reqEmpty := model.NewRequest("", "GET", "https://api.test/v1/empty")
	reqEscOnly := model.NewRequest("\x1b[31m\x1b[0m", "POST", "https://api.test/v1/esc")
	ws := &model.Workspace{
		ID:   "ws-unnamed",
		Name: "Unnamed Test",
		Collections: []*model.Collection{
			{ID: "c1", Name: "Requests", Items: []model.Item{reqEmpty, reqEscOnly}},
		},
	}
	hh := newHarness(t, ws, 80, 24)
	hh.msg(OpenSwitcherMsg{})
	screen := hh.screen()
	if !strings.Contains(screen, "(unnamed request)") {
		t.Fatalf("expected '(unnamed request)' in screen, got:\n%s", screen)
	}

	hh.keys("u", "n", "n", "a", "m", "e", "d")
	screen = hh.screen()
	if !strings.Contains(screen, "2 of 2") {
		t.Fatalf("expected '2 of 2' matching 'unnamed', got:\n%s", screen)
	}
}

func TestSwitcherRanking(t *testing.T) {
	req1 := model.NewRequest("List invoices", "GET", "https://api.test/v1/invoices")
	req2 := model.NewRequest("Pay invoice", "POST", "https://api.test/v1/invoices/pay")
	req3 := model.NewRequest("Cancel plan", "DELETE", "https://api.test/v1/plans/cancel")
	req4 := &model.Request{ID: model.NewID(), Name: "Stream events", Kind: model.KindWebSocket, URL: model.ParseURL("ws://api.test/events")}
	req5 := model.NewRequest("Get customer", "GET", "https://api.test/v1/customer")
	req6 := model.NewRequest("Put customer", "PUT", "https://api.test/v1/customer")

	ws := &model.Workspace{
		ID:   "ws1",
		Name: "Test Workspace",
		Collections: []*model.Collection{
			{
				ID:   "c1",
				Name: "Billing",
				Items: []model.Item{
					&model.Folder{
						ID:    "f1",
						Name:  "Invoices",
						Items: []model.Item{req1, req2},
					},
					&model.Folder{
						ID:    "f2",
						Name:  "Subscriptions",
						Items: []model.Item{req3, req4},
					},
				},
			},
			{
				ID:   "c2",
				Name: "Customers",
				Items: []model.Item{
					req5,
					req6,
				},
			},
		},
	}

	tests := []struct {
		name      string
		query     string
		recent    []model.ID
		wantTopID model.ID
		wantTopN  []model.ID
	}{
		{
			name:      "empty query preserves MRU order followed by tree order",
			query:     "",
			recent:    []model.ID{req3.ID, req5.ID},
			wantTopN:  []model.ID{req3.ID, req5.ID, req1.ID, req2.ID, req4.ID, req6.ID},
			wantTopID: req3.ID,
		},
		{
			name:      "matches by request name",
			query:     "Pay",
			wantTopID: req2.ID,
		},
		{
			name:      "matches by HTTP method",
			query:     "DELETE",
			wantTopID: req3.ID,
		},
		{
			name:      "matches by WebSocket method label",
			query:     "WS",
			wantTopID: req4.ID,
		},
		{
			name:      "matches by URL path segment",
			query:     "plans",
			wantTopID: req3.ID,
		},
		{
			name:      "matches by folder name",
			query:     "Invoices",
			wantTopID: req1.ID,
		},
		{
			name:      "MRU acts as tie-breaker for equal scores",
			query:     "customer",
			recent:    []model.ID{req6.ID},
			wantTopID: req6.ID,
		},
		{
			name:      "MRU acts as tie-breaker flipped",
			query:     "customer",
			recent:    []model.ID{req5.ID},
			wantTopID: req5.ID,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			hh := newHarness(t, ws, 80, 24)
			hh.a.recent = tc.recent
			hh.msg(OpenSwitcherMsg{})
			sw, ok := hh.a.ov.(*switcherOverlay)
			if !ok {
				t.Fatalf("expected *switcherOverlay, got %T", hh.a.ov)
			}
			if tc.query != "" {
				hh.typeText(tc.query)
			}
			if len(sw.res) == 0 {
				t.Fatalf("no results for query %q", tc.query)
			}
			topID := sw.all[sw.res[0].index].id
			if topID != tc.wantTopID {
				t.Errorf("top result = %v, want %v", topID, tc.wantTopID)
			}
			if len(tc.wantTopN) > 0 {
				if len(sw.res) < len(tc.wantTopN) {
					t.Fatalf("got %d results, want at least %d", len(sw.res), len(tc.wantTopN))
				}
				for i, wantID := range tc.wantTopN {
					gotID := sw.all[sw.res[i].index].id
					if gotID != wantID {
						t.Errorf("result[%d] = %v, want %v", i, gotID, wantID)
					}
				}
			}
		})
	}
}

func TestSwitcherOpenBehavior(t *testing.T) {
	hh := newHarness(t, demo(), 80, 24)

	// Open the switcher
	hh.msg(OpenSwitcherMsg{})
	if hh.a.ov == nil {
		t.Fatal("expected switcher overlay to be open")
	}

	// Filter down to "Refund charge"
	hh.typeText("Refund")
	hh.keys("enter")

	// Overlay must close, focus moves to Request pane, selection is revealed and updated
	if hh.a.ov != nil {
		t.Errorf("expected overlay to close, but is still open: %T", hh.a.ov)
	}
	if hh.a.focus != SlotRequest {
		t.Errorf("focus = %v, want SlotRequest", hh.a.focus)
	}
	ref := hh.a.currentRef()
	if ref.Request == nil || ref.Request.Name != "Refund charge" {
		t.Fatalf("selected request = %+v, want 'Refund charge'", ref.Request)
	}
	if len(hh.a.recent) == 0 || hh.a.recent[0] != ref.Request.ID {
		t.Errorf("recent[0] = %v, want %v", hh.a.recent, ref.Request.ID)
	}
}

func TestSwitcherTreeCursorMovesLeaveMRUUnchanged(t *testing.T) {
	hh := newHarness(t, demo(), 80, 24)
	if len(hh.a.recent) != 0 {
		t.Fatalf("initial recent length = %d, want 0", len(hh.a.recent))
	}

	// Move cursor in tree
	hh.keys("down", "down", "down", "up")
	if len(hh.a.recent) != 0 {
		t.Fatalf("tree cursor movement must leave MRU unchanged, got %v", hh.a.recent)
	}

	// Pressing Enter opens the request and updates MRU
	hh.keys("enter")
	if len(hh.a.recent) != 1 {
		t.Fatalf("explicit open with Enter must record MRU, got %v", hh.a.recent)
	}
}

func TestSwitcherLenientLoadProblemsExcluded(t *testing.T) {
	be := newFake(demo())
	be.problems = append(be.problems, brokenFile(be.root, "collections/payments/charges/broken.yaml", "syntax error"))
	hh := newHarnessWith(t, be, 80, 24)

	hh.msg(OpenSwitcherMsg{})
	sw, ok := hh.a.ov.(*switcherOverlay)
	if !ok {
		t.Fatalf("expected *switcherOverlay, got %T", hh.a.ov)
	}
	for _, it := range sw.all {
		if strings.Contains(it.name, "broken") || strings.Contains(it.urlPath, "broken") {
			t.Fatalf("broken file request appeared in switcher items: %+v", it)
		}
	}
}

func TestSwitcherDeletedRequestPrunedFromMRU(t *testing.T) {
	hh := newHarness(t, demo(), 80, 24)
	deletedID := model.ID("deleted-request-id-999")
	validID := hh.a.ws.Collections[0].Requests()[0].ID

	hh.a.recent = []model.ID{deletedID, validID}

	// Opening switcher must prune deleted ID
	hh.msg(OpenSwitcherMsg{})
	if containsID(hh.a.recent, deletedID) {
		t.Fatalf("deleted ID not pruned from recent: %v", hh.a.recent)
	}
	if !containsID(hh.a.recent, validID) {
		t.Fatalf("valid ID unexpectedly removed from recent: %v", hh.a.recent)
	}

	// recordRecent with a non-existent ID also ignores/prunes it
	hh.a.recordRecent(deletedID)
	if containsID(hh.a.recent, deletedID) {
		t.Fatalf("recordRecent accepted non-existent ID: %v", hh.a.recent)
	}
}

func TestSwitcherScrolling(t *testing.T) {
	// Create workspace with 30 requests
	var reqs []model.Item
	for i := 1; i <= 30; i++ {
		reqs = append(reqs, model.NewRequest(fmt.Sprintf("Request %02d", i), "GET", fmt.Sprintf("/v1/items/%02d", i)))
	}
	ws := &model.Workspace{
		ID:   "ws-scroll",
		Name: "Scroll Test",
		Collections: []*model.Collection{
			{ID: "c1", Name: "Items", Items: reqs},
		},
	}

	hh := newHarness(t, ws, 80, 15) // small height so lastPage is small
	hh.msg(OpenSwitcherMsg{})
	sw, ok := hh.a.ov.(*switcherOverlay)
	if !ok {
		t.Fatalf("expected *switcherOverlay, got %T", hh.a.ov)
	}

	// Move down past page boundary
	for range 20 {
		hh.keys("down")
	}
	if sw.cursor != 20 {
		t.Fatalf("cursor = %d, want 20", sw.cursor)
	}
	if sw.cursor < sw.top || sw.cursor >= sw.top+sw.lastPage {
		t.Fatalf("cursor %d out of visible window [%d, %d)", sw.cursor, sw.top, sw.top+sw.lastPage)
	}

	// Test PageUp
	hh.keys("pgup")
	if sw.cursor < sw.top || sw.cursor >= sw.top+sw.lastPage {
		t.Fatalf("after pgup, cursor %d out of visible window [%d, %d)", sw.cursor, sw.top, sw.top+sw.lastPage)
	}

	// Test PageDown
	hh.keys("pgdown")
	if sw.cursor < sw.top || sw.cursor >= sw.top+sw.lastPage {
		t.Fatalf("after pgdown, cursor %d out of visible window [%d, %d)", sw.cursor, sw.top, sw.top+sw.lastPage)
	}

	// Render view should not panic and should stay bounded
	view := sw.view(hh.a, 80, 15)
	lines := strings.Split(view, "\n")
	if len(lines) > 15 {
		t.Fatalf("rendered lines count %d exceeds height 15", len(lines))
	}
}

func TestSwitcherLongNameTruncatesNoWrap(t *testing.T) {
	longName := "Very Long Request Name That Exceeds The Width Of A Standard Eighty Column Terminal By Far And Should Not Wrap Under Any Circumstances"
	longFolder := "Deeply › Nested › Folder › Hierarchy › With › Many › Levels › Exceeding › Column › Limits"
	longPath := "/v1/api/super/long/nested/path/resource/with/identifiers/and/subresources/never/wrap"

	req := model.NewRequest(longName, "POST", "https://api.test"+longPath)
	ws := &model.Workspace{
		ID:   "ws-long",
		Name: "Long Test",
		Collections: []*model.Collection{
			{ID: "c1", Name: longFolder, Items: []model.Item{req}},
		},
	}

	hh := newHarness(t, ws, 80, 24)
	hh.msg(OpenSwitcherMsg{})
	sw, ok := hh.a.ov.(*switcherOverlay)
	if !ok {
		t.Fatalf("expected *switcherOverlay, got %T", hh.a.ov)
	}

	view := sw.view(hh.a, 80, 24)
	lines := strings.Split(view, "\n")
	for i, l := range lines {
		w := ui.Width(l)
		if w > 80 {
			t.Errorf("line %d width = %d > 80: %q", i, w, l)
		}
	}
}

func TestSwitcherMRUDedupeAndDepth(t *testing.T) {
	a := &app{}

	// Initial inserts
	a.recordRecent("r1")
	a.recordRecent("r2")
	a.recordRecent("r3")
	want := []model.ID{"r3", "r2", "r1"}
	if !equalIDs(a.recent, want) {
		t.Fatalf("recent = %v, want %v", a.recent, want)
	}

	// Deduplication: r2 moved to front
	a.recordRecent("r2")
	want = []model.ID{"r2", "r3", "r1"}
	if !equalIDs(a.recent, want) {
		t.Fatalf("after dedupe, recent = %v, want %v", a.recent, want)
	}

	// Empty ID ignored
	a.recordRecent("")
	if !equalIDs(a.recent, want) {
		t.Fatalf("after empty recordRecent, recent = %v, want %v", a.recent, want)
	}

	// Capped at depth 50
	for i := 0; i < 55; i++ {
		a.recordRecent(model.ID(fmt.Sprintf("req-%02d", i)))
	}
	if len(a.recent) != 50 {
		t.Fatalf("len(recent) = %d, want 50", len(a.recent))
	}
	if a.recent[0] != "req-54" {
		t.Errorf("recent[0] = %v, want req-54", a.recent[0])
	}
	if a.recent[49] != "req-05" {
		t.Errorf("recent[49] = %v, want req-05", a.recent[49])
	}
}

func equalIDs(a, b []model.ID) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestSwitcherSentinelNeverAppears(t *testing.T) {
	const sentinel = "SECRET_TOKEN_SENTINEL_12345"
	const secretHost = "secret-internal-host.corp"

	req := model.NewRequest("Test Secret", "GET", "https://"+secretHost+":8443/v1/private/items?token="+sentinel+"&user=alice#frag")
	ws := &model.Workspace{
		ID:   "ws-sec",
		Name: "Security Test",
		Collections: []*model.Collection{
			{
				ID:    "c1",
				Name:  "SecColl",
				Items: []model.Item{req},
			},
		},
	}

	hh := newHarness(t, ws, 80, 24)
	hh.msg(OpenSwitcherMsg{})
	sw, ok := hh.a.ov.(*switcherOverlay)
	if !ok {
		t.Fatalf("expected *switcherOverlay, got %T", hh.a.ov)
	}

	// Check frames at 80x24, 120x40, 200x60
	sizes := []struct{ w, h int }{{80, 24}, {120, 40}, {200, 60}}
	for _, sz := range sizes {
		viewText := sw.view(hh.a, sz.w, sz.h)
		if strings.Contains(viewText, sentinel) {
			t.Fatalf("view at %dx%d contains sentinel token!", sz.w, sz.h)
		}
		if strings.Contains(viewText, secretHost) {
			t.Fatalf("view at %dx%d contains secret host!", sz.w, sz.h)
		}
		if !strings.Contains(viewText, "/v1/private/items") {
			t.Fatalf("view at %dx%d missing path '/v1/private/items'", sz.w, sz.h)
		}
	}

	// Querying sentinel should produce no matches and never leak
	hh.typeText(sentinel)
	viewAfterQuery := sw.view(hh.a, 80, 24)
	if strings.Contains(viewAfterQuery, "/v1/private/items") {
		t.Fatalf("querying sentinel matched url path unexpectedly")
	}
}

func TestSwitcherPureView(t *testing.T) {
	hh := newHarness(t, demo(), 80, 24)
	hh.msg(OpenSwitcherMsg{})
	sw, ok := hh.a.ov.(*switcherOverlay)
	if !ok {
		t.Fatalf("expected *switcherOverlay, got %T", hh.a.ov)
	}

	v1 := sw.view(hh.a, 80, 24)
	v2 := sw.view(hh.a, 80, 24)
	if v1 != v2 {
		t.Fatalf("view is not pure:\nv1:\n%s\nv2:\n%s", v1, v2)
	}

	// Cursor and scroll position must not mutate during view
	beforeCursor, beforeTop := sw.cursor, sw.top
	_ = sw.view(hh.a, 120, 40)
	if sw.cursor != beforeCursor || sw.top != beforeTop {
		t.Fatalf("view mutated state: cursor %d->%d, top %d->%d", beforeCursor, sw.cursor, beforeTop, sw.top)
	}
}

func TestSwitcherEmptyWorkspace(t *testing.T) {
	ws := &model.Workspace{ID: "empty-ws", Name: "Empty"}
	for _, mode := range []struct {
		name string
		opt  harnessOpt
	}{
		{"dark", func(o *Options) {}},
		{"nocolor", func(o *Options) { o.NoColor = true }},
		{"ascii", func(o *Options) { o.ASCII = true }},
	} {
		t.Run(mode.name, func(t *testing.T) {
			hh := newHarness(t, ws, 80, 24, mode.opt)
			hh.msg(OpenSwitcherMsg{})
			hh.mustHave("No requests yet. Press Esc, then n to create one.")
			hh.mustHave("0 of 0")
		})
	}
}

func TestSwitcherPaletteAction(t *testing.T) {
	hh := newHarness(t, demo(), 80, 24)
	var findAct *Action
	for _, act := range hh.a.registry.All() {
		if act.ID == "request.find" {
			findAct = &act
			break
		}
	}
	if findAct == nil {
		t.Fatal("action 'request.find' not found in registry")
	}
	if findAct.Title != "Find request" {
		t.Errorf("title = %q, want 'Find request'", findAct.Title)
	}
	if findAct.Group != "Navigation" {
		t.Errorf("group = %q, want 'Navigation'", findAct.Group)
	}
	if findAct.Keys != "" {
		t.Errorf("keys = %q, want '' (empty until V0 binds it)", findAct.Keys)
	}

	// Running the action opens the switcher
	cmd := findAct.Run()
	hh.run(cmd)
	if hh.a.ov == nil {
		t.Fatal("running 'request.find' did not open switcher")
	}
	if _, ok := hh.a.ov.(*switcherOverlay); !ok {
		t.Fatalf("expected *switcherOverlay, got %T", hh.a.ov)
	}
}

func TestGoldenSwitcher(t *testing.T) {
	modes := []struct {
		name string
		opt  harnessOpt
		raw  bool
	}{
		{"dark", func(o *Options) {}, true},
		{"nocolor", func(o *Options) { o.NoColor = true }, false},
		{"ascii", func(o *Options) { o.ASCII = true }, false},
	}
	for _, m := range modes {
		for _, sz := range goldenSizes {
			t.Run(m.name+"/"+sz.name, func(t *testing.T) {
				hh := newHarness(t, demo(), sz.w, sz.h, m.opt)
				hh.msg(OpenSwitcherMsg{})
				out := hh.screen()
				if m.raw {
					out = hh.rawScreen()
				}
				golden(t, "switcher-"+m.name+"-"+sz.name, out)
			})
		}
	}
}
