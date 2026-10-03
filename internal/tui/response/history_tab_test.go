package response

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/shubham/postctl/internal/history"
	"github.com/shubham/postctl/internal/model"
	"github.com/shubham/postctl/internal/tui"
	"github.com/shubham/postctl/internal/ui"
)

func v3Theme() ui.Theme { return ui.NewTheme(true, true, false) }

// v3Log returns a log with three sends of one request (newest first) and one
// of another.
func v3Log(req model.ID) *fakeLog {
	newest := v3Entry("e3", req, 30*time.Second, 200, "OK", `{"n":3}`)
	mid := v3Entry("e2", req, 5*time.Minute, 404, "Not Found", `{"n":2}`)
	oldest := v3Entry("e1", req, 3*time.Hour, 500, "Internal Server Error", `{"n":1}`)
	other := v3Entry("x1", "other-request", time.Minute, 201, "Created", `{}`)
	return &fakeLog{
		entries: []history.Entry{newest, other, mid, oldest},
		full:    map[string]*history.Entry{"e3": &newest, "e2": &mid, "e1": &oldest},
	}
}

func v3PaneWithLog(t *testing.T) (*Pane, model.ID, *fakeLog, *[]string) {
	t.Helper()
	id := model.NewID()
	log := v3Log(id)
	// The selection is made before the log knows the ID, so build it here.
	p, _, clip := v3Pane(t, log, v3Theme())
	p.Update(tui.SelectionMsg{Ref: tui.RequestRef{Request: &model.Request{ID: id, Method: "GET"}}})
	for i := range log.entries {
		if log.entries[i].RequestID != "other-request" {
			log.entries[i].RequestID = id
		}
	}
	for _, e := range log.full {
		e.RequestID = id
	}
	return p, id, log, clip
}

func TestHistoryTabLoadsNewestFirstWithStatusReasonAndColumns(t *testing.T) {
	p, _, log, _ := v3PaneWithLog(t)
	v3Pump(t, p, tui.FocusSectionMsg{Section: tui.SectionRespHistory})
	out := v3Frame(p)
	for _, want := range []string{"History", "GET", "200 OK", "404 Not Found", "500 Internal Server Error", "120 ms", "2.0 KB", "30s ago", "5m ago", "3h ago"} {
		if !strings.Contains(out, want) {
			t.Fatalf("History tab misses %q:\n%s", want, out)
		}
	}
	if a, b, c := strings.Index(out, "200 OK"), strings.Index(out, "404 Not Found"), strings.Index(out, "500 Internal"); a >= b || b >= c {
		t.Fatalf("entries are not newest first:\n%s", out)
	}
	if strings.Contains(out, "201 Created") {
		t.Fatalf("another request's send is listed:\n%s", out)
	}
	if log.lists != 1 {
		t.Fatalf("loaded %d times, want 1", log.lists)
	}
	if got := log.filters[0]; got.RequestID == "" || got.Limit != historyLimit {
		t.Fatalf("filter = %+v, want the request ID and a limit of %d", got, historyLimit)
	}
}

func TestHistoryTabEmptyStateNamesNextAction(t *testing.T) {
	id := model.NewID()
	p, _, _ := v3Pane(t, &fakeLog{}, v3Theme())
	p.Update(tui.SelectionMsg{Ref: tui.RequestRef{Request: &model.Request{ID: id}}})
	v3Pump(t, p, tui.FocusSectionMsg{Section: tui.SectionRespHistory})
	out := v3Frame(p)
	if !strings.Contains(out, "No sends of this request yet.") || !strings.Contains(out, "Ctrl-S sends it.") {
		t.Fatalf("empty state:\n%s", out)
	}
}

func TestHistoryTabShowsLoadErrorWithNextAction(t *testing.T) {
	log := &fakeLog{listErr: errors.New("disk is on fire /secret/path?token=" + v3Secret)}
	p, _, _ := v3Pane(t, log, v3Theme())
	v3Pump(t, p, tui.FocusSectionMsg{Section: tui.SectionRespHistory})
	out := v3Frame(p)
	if !strings.Contains(out, "Could not read the history") || !strings.Contains(out, "send again") {
		t.Fatalf("error state:\n%s", out)
	}
	if strings.Contains(out, v3Secret) {
		t.Fatal("the error state echoed the error text")
	}
}

func TestHistoryTabReloadsAfterSendOfThisRequestOnly(t *testing.T) {
	p, id, log, _ := v3PaneWithLog(t)
	v3Pump(t, p, tui.FocusSectionMsg{Section: tui.SectionRespHistory})
	before := log.lists
	v3Pump(t, p, tui.SendResultMsg{Result: v3Result("another-request", `{}`, "", nil)})
	if log.lists != before {
		t.Fatal("a send of another request reloaded this request's history")
	}
	v3Pump(t, p, tui.SendResultMsg{Result: v3Result(id, `{"n":4}`, "e3", nil)})
	if log.lists != before+1 {
		t.Fatalf("lists = %d, want %d: the tab must reload after a send of its request", log.lists, before+1)
	}
}

func TestHistoryTabNotLoadedUntilVisited(t *testing.T) {
	p, id, log, _ := v3PaneWithLog(t)
	v3Pump(t, p, tui.SendResultMsg{Result: v3Result(id, `{"n":4}`, "e3", nil)})
	if log.lists != 0 {
		t.Fatalf("history was read %d times before the tab was opened", log.lists)
	}
}

func TestHistoryTabDropsStaleAndForeignLoads(t *testing.T) {
	p, id, _, _ := v3PaneWithLog(t)
	p.Update(tui.FocusSectionMsg{Section: tui.SectionRespHistory})
	first := p.histGen
	cmd := p.loadHistory() // a second, newer load
	if p.histGen == first {
		t.Fatal("a new load must advance the generation")
	}
	p.Update(histLoadedMsg{gen: p.histGen, reqID: id, items: []history.Entry{{ID: "new"}}})
	p.Update(histLoadedMsg{gen: first, reqID: id, items: []history.Entry{{ID: "stale"}, {ID: "stale2"}}})
	if len(p.hist.items) != 1 || p.hist.items[0].ID != "new" {
		t.Fatalf("a stale load replaced the newer one: %+v", p.hist.items)
	}
	p.Update(histLoadedMsg{gen: p.histGen, reqID: "another", items: []history.Entry{{ID: "foreign"}}})
	if len(p.hist.items) != 1 || p.hist.items[0].ID != "new" {
		t.Fatal("a load for another request was applied")
	}
	_ = cmd
}

func TestHistoryTabSwitchingRequestDropsListAndReloads(t *testing.T) {
	p, _, log, _ := v3PaneWithLog(t)
	v3Pump(t, p, tui.FocusSectionMsg{Section: tui.SectionRespHistory})
	before := log.lists
	next := model.NewID()
	v3Pump(t, p, tui.SelectionMsg{Ref: tui.RequestRef{Request: &model.Request{ID: next}}})
	if log.lists != before+1 {
		t.Fatalf("lists = %d, want a reload for the new request", log.lists)
	}
	if strings.Contains(v3Frame(p), "200 OK") {
		t.Fatal("the previous request's sends are still listed")
	}
}

func TestHistoryTabKeysMoveOpenAndDiff(t *testing.T) {
	p, id, _, _ := v3PaneWithLog(t)
	v3Pump(t, p, tui.SendResultMsg{Result: v3Result(id, `{"n":3}`, "e3", nil)})
	v3Pump(t, p, tui.FocusSectionMsg{Section: tui.SectionRespHistory})
	v3Pump(t, p, v3Key("j"))
	if p.hist.cur != 1 {
		t.Fatalf("j moved the cursor to %d", p.hist.cur)
	}
	shell := v3Pump(t, p, v3Key("enter"))
	if len(shell) != 1 || !reflect.DeepEqual(shell[0], tui.OpenRequestHistoryMsg{RequestID: id, EntryID: "e2"}) {
		t.Fatalf("Enter sent %+v", shell)
	}
	v3Pump(t, p, v3Key("d"))
	if !p.diff.open || p.diff.body == nil {
		t.Fatal("d did not diff the entry with the current response")
	}
	v3Pump(t, p, v3Key("esc"))
	if p.diff.open {
		t.Fatal("Esc did not close the diff")
	}
	v3Pump(t, p, v3Key("G"))
	if p.hist.cur != 2 {
		t.Fatalf("G moved the cursor to %d", p.hist.cur)
	}
	v3Pump(t, p, tui.SelectionMsg{Ref: p.ref}) // same request: keeps the list
	if len(p.hist.items) != 3 {
		t.Fatal("selecting the same request dropped the list")
	}
}

func TestHistoryTabMarksCurrentResponseInWords(t *testing.T) {
	p, id, _, _ := v3PaneWithLog(t)
	v3Pump(t, p, tui.SendResultMsg{Result: v3Result(id, `{"n":3}`, "e3", nil)})
	v3Pump(t, p, tui.FocusSectionMsg{Section: tui.SectionRespHistory})
	if !strings.Contains(v3Frame(p), "this response") {
		t.Fatalf("current response is not marked:\n%s", v3Frame(p))
	}
}

func TestHistoryTabWithoutLogIsAbsent(t *testing.T) {
	p, _, _ := v3Pane(t, nil, v3Theme())
	if got := p.Sections(); !reflect.DeepEqual(got, []string{tui.SectionRespBody}) {
		t.Fatalf("sections = %v", got)
	}
	for _, a := range p.Actions() {
		if a.ID == "history.request" || a.ID == "response.diff" {
			t.Fatalf("action %s advertised without a history log", a.ID)
		}
	}
	for _, tb := range p.buildTabs() {
		if tb.Label == "History" {
			t.Fatal("History tab shown without a log")
		}
	}
}

func TestHistoryTabBeforeAnyResponseStillListsPastSends(t *testing.T) {
	p, _, _, _ := v3PaneWithLog(t)
	if !strings.Contains(v3Frame(p), "Space H") {
		t.Fatalf("the empty response state does not point to Space H:\n%s", v3Frame(p))
	}
	shell := v3RunAction(t, p, "history.request")
	if len(shell) == 0 {
		t.Fatal("history.request did not ask the shell to focus the response pane")
	}
	out := v3Frame(p)
	if !strings.Contains(out, "200 OK") || !strings.Contains(out, "No response yet") {
		t.Fatalf("past sends should show without a current response:\n%s", out)
	}
}

func TestHistoryTabScrollsAndFits(t *testing.T) {
	id := model.NewID()
	log := &fakeLog{}
	for i := 0; i < 80; i++ {
		log.entries = append(log.entries, v3Entry("e"+string(rune('A'+i%26))+string(rune('a'+i/26)), id, time.Duration(i)*time.Minute, 200, "OK", `{}`))
	}
	p, _, _ := v3Pane(t, log, v3Theme())
	p.Update(tui.SelectionMsg{Ref: tui.RequestRef{Request: &model.Request{ID: id}}})
	for i := range log.entries {
		log.entries[i].RequestID = id
	}
	for _, size := range [][2]int{{80, 24}, {120, 40}, {40, 12}} {
		p.SetSize(size[0], size[1])
		v3Pump(t, p, tui.FocusSectionMsg{Section: tui.SectionRespHistory})
		for range 60 {
			v3Pump(t, p, v3Key("j"))
		}
		lines := strings.Split(v3Frame(p), "\n")
		if len(lines) > size[1] {
			t.Fatalf("%dx%d: %d lines", size[0], size[1], len(lines))
		}
		for _, l := range lines {
			if ui.Width(l) > size[0] {
				t.Fatalf("%dx%d: a line is %d cells wide", size[0], size[1], ui.Width(l))
			}
		}
	}
	if p.hist.top == 0 {
		t.Fatal("the list did not scroll with the cursor")
	}
}

func TestHistoryActionsAreRegistered(t *testing.T) {
	p, _, _, _ := v3PaneWithLog(t)
	for _, id := range []string{"history.request", "response.diff", "response.jq"} {
		a := v3Action(t, p, id)
		if a.Enabled == nil || !a.Enabled() {
			t.Fatalf("%s should be enabled with a request selected", id)
		}
	}
	empty := NewWithDeps(tui.PaneEnv{Theme: v3Theme(), Actions: &tui.Registry{}}, Deps{Log: func() (tui.HistoryReader, error) { return nil, nil }}).(*Pane)
	for _, a := range empty.Actions() {
		if (a.ID == "response.diff" || a.ID == "response.jq" || a.ID == "history.request") && a.Enabled() {
			t.Fatalf("%s is enabled with no request selected", a.ID)
		}
	}
}

func TestHistoryTabViewIsPure(t *testing.T) {
	p, id, _, _ := v3PaneWithLog(t)
	v3Pump(t, p, tui.SendResultMsg{Result: v3Result(id, `{"n":3}`, "e3", nil)})
	v3Pump(t, p, tui.FocusSectionMsg{Section: tui.SectionRespHistory})
	v3Pump(t, p, v3Key("j"))
	before := *p
	first := p.View()
	second := p.View()
	if first != second {
		t.Fatal("View is not deterministic")
	}
	after := *p
	after.rawIdx.offsets, before.rawIdx.offsets = nil, nil
	if !reflect.DeepEqual(before.hist, after.hist) || before.scroll != after.scroll || before.tab != after.tab {
		t.Fatal("View changed the pane")
	}
}

func themeASCII() ui.Theme { return ui.NewTheme(true, true, true) }

func TestHistoryTabViewDoesNotReadTheClock(t *testing.T) {
	calls := 0
	log := &fakeLog{}
	p, id, _ := v3Pane(t, log, v3Theme())
	p.deps.Now = func() time.Time { calls++; return v3Now }
	log.entries = []history.Entry{v3Entry("e1", id, time.Minute, 200, "OK", `{}`)}
	v3Pump(t, p, tui.FocusSectionMsg{Section: tui.SectionRespHistory})
	if calls == 0 {
		t.Fatal("Update should stamp the clock when it shows the History tab")
	}
	before := calls
	_ = p.View()
	_ = p.View()
	if calls != before {
		t.Fatalf("View read the clock %d times", calls-before)
	}
}
