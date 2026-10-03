package request

import (
	"strings"
	"testing"
	"time"

	"github.com/shubham/postctl/internal/model"
	"github.com/shubham/postctl/internal/tui"
)

func TestInitialViewShowsBarTabsAndParams(t *testing.T) {
	h := newHarness(t)
	v := h.view()
	for _, want := range []string{"POST", "{{baseUrl}}/v1/charges/:id?limit=10&debug=1", "Send", "Params(2)", "Headers(1)", "limit", "debug"} {
		if !strings.Contains(v, want) {
			t.Errorf("view lacks %q:\n%s", want, v)
		}
	}
	if !strings.Contains(v, ":id") || !strings.Contains(v, "path variable") && h.p.w < 100 {
		// path variables are listed under the query rows
		if !strings.Contains(v, ":id") {
			t.Errorf("path variable row missing:\n%s", v)
		}
	}
}

func TestURLTypingUpdatesRequestAndKeepsDisabledParams(t *testing.T) {
	h := newHarness(t)
	h.req.URL.Query = append(h.req.URL.Query, model.KV{Key: "off", Value: "x", Disabled: true})
	h.p.reloadAll()
	h.key("up") // tabs -> URL bar
	if h.p.zone != zBar || !h.p.Editing() {
		t.Fatalf("zone=%v editing=%v, want URL bar editing", h.p.zone, h.p.Editing())
	}
	h.key("end")
	h.typ("&extra=1")
	if got := h.req.URL.Raw; got != "{{baseUrl}}/v1/charges/:id?limit=10&debug=1&extra=1" {
		t.Fatalf("raw = %q", got)
	}
	var keys []string
	for _, q := range h.req.URL.Query {
		keys = append(keys, q.Key)
	}
	if strings.Join(keys, ",") != "limit,debug,off,extra" {
		t.Fatalf("query keys = %v", keys)
	}
	if !h.req.URL.Query[2].Disabled {
		t.Fatal("disabled param lost its state")
	}
	if h.req.URL.Query[1].Description != "verbose" {
		t.Fatal("description lost")
	}
}

func TestPathVariableValueSurvivesURLEdit(t *testing.T) {
	h := newHarness(t)
	h.req.URL.PathVars[0].Value = "42"
	h.key("up", "end")
	h.typ("z")
	if h.req.URL.PathVars[0].Value != "42" {
		t.Fatalf("path var value = %q", h.req.URL.PathVars[0].Value)
	}
}

func TestPreviewResolvesAndMasksSecrets(t *testing.T) {
	h := newHarness(t)
	h.req.URL.Raw = "{{baseUrl}}/v1?t={{token}}&x={{nope}}"
	h.p.reloadAll()
	h.key("up")
	v := h.view()
	if !strings.Contains(v, "https://staging.example.com/v1") {
		t.Fatalf("preview not resolved:\n%s", v)
	}
	if strings.Contains(v, "s3cr3t-value") {
		t.Fatalf("secret leaked in preview:\n%s", v)
	}
	if !strings.Contains(v, "unresolved") || !strings.Contains(v, "nope") {
		t.Fatalf("unresolved variable not flagged:\n%s", v)
	}
}

func TestUnresolvedShownWithoutFocus(t *testing.T) {
	h := newHarness(t)
	h.req.URL.Raw = "{{nope}}/x"
	h.p.reloadAll()
	h.p.setZone(zTabs)
	h.mustHave("unresolved: nope")
}

func TestMethodSelector(t *testing.T) {
	h := newHarness(t)
	h.key("m")
	if h.p.msel == nil {
		t.Fatal("selector did not open")
	}
	h.mustHave("[POST]")
	h.key("right", "enter")
	if h.req.Method != "PUT" {
		t.Fatalf("method = %s", h.req.Method)
	}
	h.key("m", "d", "enter") // typeahead to DELETE
	if h.req.Method != "DELETE" {
		t.Fatalf("method = %s", h.req.Method)
	}
	h.key("m", "esc")
	if h.req.Method != "DELETE" || h.p.msel != nil {
		t.Fatal("esc must cancel")
	}
	h.key("ctrl+z")
	if h.req.Method != "PUT" {
		t.Fatalf("undo left %s", h.req.Method)
	}
}

func TestEnterInURLBarSends(t *testing.T) {
	h := newHarness(t)
	h.key("up", "enter")
	var sent *tui.SendRequestMsg
	for _, m := range h.out {
		if s, ok := m.(tui.SendRequestMsg); ok {
			sent = &s
		}
	}
	if sent == nil {
		t.Fatal("no SendRequestMsg")
	}
	if sent.Request.Request != h.req || sent.Request.Collection != h.coll || sent.Request.Environment != h.env {
		t.Fatal("send payload wrong")
	}
}

func TestCtrlSSendsFromAnywhere(t *testing.T) {
	h := newHarness(t)
	h.key("down", "a") // editing a param key
	h.key("ctrl+s")
	found := false
	for _, m := range h.out {
		if _, ok := m.(tui.SendRequestMsg); ok {
			found = true
		}
	}
	if !found {
		t.Fatal("Ctrl-S did not send while editing")
	}
}

func TestEmptyURLDoesNotSend(t *testing.T) {
	h := newHarness(t)
	h.req.URL = model.ParseURL("")
	h.p.reloadAll()
	h.key("ctrl+s")
	for _, m := range h.out {
		if _, ok := m.(tui.SendRequestMsg); ok {
			t.Fatal("sent with empty URL")
		}
	}
	h.mustHave("Enter a URL first")
}

func TestSendingShowsProgressAndEscCancels(t *testing.T) {
	h := newHarness(t)
	h.send(tui.SendStartedMsg{RequestID: h.req.ID})
	h.send(tui.SendProgressMsg{RequestID: h.req.ID, Step: tui.SendStep{Text: "Waiting for response"}})
	h.advance(1500 * time.Millisecond)
	h.mustHave("Waiting for response")
	h.mustHave("Sending")
	hints := ""
	for _, k := range h.p.KeyHints() {
		hints += k.Key + " " + k.Label + ";"
	}
	if !strings.Contains(hints, "Esc Cancel") {
		t.Fatalf("hints = %s", hints)
	}
	h.key("esc")
	cancelled := false
	for _, m := range h.out {
		if c, ok := m.(tui.CancelSendMsg); ok && c.RequestID == h.req.ID {
			cancelled = true
		}
	}
	if !cancelled {
		t.Fatal("Esc did not cancel")
	}
	h.send(tui.SendResultMsg{Result: tui.SendResult{RequestID: h.req.ID}})
	h.mustNotHave("Waiting for response")
}

func TestSendKeyHintFollowsKeyboardSupport(t *testing.T) {
	h := newHarness(t)
	find := func() string {
		for _, k := range h.p.KeyHints() {
			if k.Label == "Send" {
				return k.Key
			}
		}
		return ""
	}
	if find() != "Ctrl-S" {
		t.Fatalf("default send key = %q", find())
	}
	h.p.enhanced = true
	if find() != "Ctrl-Enter" {
		t.Fatalf("enhanced send key = %q", find())
	}
}

// ---- key-value tables -----------------------------------------------------------

func TestParamsAddRowStartsAtKeyAndTabToValue(t *testing.T) {
	h := newHarness(t)
	h.key("down") // into Params
	h.key("a")
	if !h.p.Editing() {
		t.Fatal("a should start editing the new row")
	}
	// The new row stays out of the URL until it has content.
	if strings.Contains(h.req.URL.Raw, "=&") || strings.HasSuffix(h.req.URL.Raw, "=") {
		t.Fatalf("empty row leaked into the URL: %q", h.req.URL.Raw)
	}
	h.typ("page")
	h.key("tab")
	h.typ("2")
	h.key("esc")
	// New rows go after the focused one.
	if got := h.req.URL.Raw; got != "{{baseUrl}}/v1/charges/:id?limit=10&page=2&debug=1" {
		t.Fatalf("raw = %q", got)
	}
	if n := len(h.req.URL.Query); n != 3 {
		t.Fatalf("%d query rows", n)
	}
}

func TestParamsAddRowThenEscDropsEmptyRow(t *testing.T) {
	h := newHarness(t)
	h.key("down", "a", "esc")
	if n := len(h.req.URL.Query); n != 2 {
		t.Fatalf("empty draft row kept: %d rows", n)
	}
	if h.req.URL.Raw != "{{baseUrl}}/v1/charges/:id?limit=10&debug=1" {
		t.Fatalf("raw = %q", h.req.URL.Raw)
	}
}

func TestParamsToggleRewritesURL(t *testing.T) {
	h := newHarness(t)
	h.key("down", "space")
	if h.req.URL.Raw != "{{baseUrl}}/v1/charges/:id?debug=1" {
		t.Fatalf("raw = %q", h.req.URL.Raw)
	}
	if !h.req.URL.Query[0].Disabled {
		t.Fatal("row not disabled")
	}
	h.key("space")
	if h.req.URL.Raw != "{{baseUrl}}/v1/charges/:id?limit=10&debug=1" {
		t.Fatalf("raw = %q", h.req.URL.Raw)
	}
}

func TestParamsDeleteAndReorder(t *testing.T) {
	h := newHarness(t)
	h.key("down", "alt+down")
	if h.req.URL.Query[0].Key != "debug" || h.req.URL.Query[1].Key != "limit" {
		t.Fatalf("order = %v", h.req.URL.Query)
	}
	if h.req.URL.Raw != "{{baseUrl}}/v1/charges/:id?debug=1&limit=10" {
		t.Fatalf("raw = %q", h.req.URL.Raw)
	}
	h.key("d") // the moved row (limit) is still focused
	if len(h.req.URL.Query) != 1 || h.req.URL.Raw != "{{baseUrl}}/v1/charges/:id?debug=1" {
		t.Fatalf("after delete: %v %q", h.req.URL.Query, h.req.URL.Raw)
	}
}

func TestParamsEditValueInline(t *testing.T) {
	h := newHarness(t)
	h.key("down", "enter") // edit value of "limit"
	h.key("ctrl+a")
	h.key("ctrl+k")
	h.typ("50")
	h.key("enter")
	if h.req.URL.Query[0].Value != "50" || !strings.Contains(h.req.URL.Raw, "limit=50") {
		t.Fatalf("query = %v raw = %q", h.req.URL.Query, h.req.URL.Raw)
	}
}

func TestPathVariablesEditValueOnly(t *testing.T) {
	h := newHarness(t)
	h.key("down", "down", "down") // limit, debug, :id
	h.key("d")                    // cannot delete a path variable
	if len(h.req.URL.PathVars) != 1 {
		t.Fatal("path variable deleted")
	}
	h.key("enter")
	h.typ("7")
	h.key("enter")
	if h.req.URL.PathVars[0].Value != "7" {
		t.Fatalf("path var = %q", h.req.URL.PathVars[0].Value)
	}
}

func TestHeadersAddToggleBulk(t *testing.T) {
	h := newHarness(t)
	h.gotoTab(tabHeaders)
	h.key("down", "a")
	h.typ("X-Id")
	h.key("tab")
	h.typ("9")
	h.key("esc")
	if len(h.req.Headers) != 3 || h.req.Headers[1].Key != "X-Id" || h.req.Headers[1].Value != "9" {
		t.Fatalf("headers = %v", h.req.Headers)
	}
	h.key("b")
	if !h.p.Editing() {
		t.Fatal("bulk edit should take input focus")
	}
	h.mustHave("Accept: application/json")
	h.mustHave("// X-Trace: 1")
	h.key("esc")
	if h.p.Editing() {
		t.Fatal("esc should leave bulk edit")
	}
}

func TestBulkEditParsesLinesAndKeepsDisabled(t *testing.T) {
	old := []kvItem{{KV: model.KV{Key: "a", Value: "1", Description: "first"}}}
	got := parseBulk("a: 10\n// b: 2\n\nc:3", old)
	if len(got) != 3 {
		t.Fatalf("rows = %v", got)
	}
	if got[0].Value != "10" || got[0].Description != "first" || got[1].Key != "b" || !got[1].Disabled || got[2].Value != "3" {
		t.Fatalf("rows = %+v", got)
	}
}

func TestBulkEditLive(t *testing.T) {
	h := newHarness(t)
	h.gotoTab(tabHeaders)
	h.key("down", "b")
	h.key("ctrl+a") // start of line (textarea)
	h.typ("X-New: v")
	h.key("enter")
	found := false
	for _, k := range h.req.Headers {
		if k.Key == "X-New" && k.Value == "v" {
			found = true
		}
	}
	if !found {
		t.Fatalf("headers = %v", h.req.Headers)
	}
	h.key("esc")
	if h.p.Editing() {
		t.Fatal("esc should leave bulk edit")
	}
}

func TestTabCountsAndDots(t *testing.T) {
	h := newHarness(t)
	h.mustHave("Params(2)")
	h.mustHave("Headers(1)")
	h.req.Scripts.Test = &model.Script{Source: "pm.test('x', () => {})"}
	h.mustHave("Tests ●")
	h.req.Auth = &model.Auth{Type: model.AuthBasic, Basic: &model.BasicAuth{}}
	h.mustHave("Auth ●")
}

func TestNarrowTabBarScrollsToActive(t *testing.T) {
	h := newHarness(t)
	h.p.SetSize(40, 20)
	h.p.tab = tabSettings
	h.mustHave("Settings")
	h.mustHave("‹")
}

// i on a key-value row starts editing its cell in insert, exactly like Enter.
func TestIOnAKVRowEditsLikeEnter(t *testing.T) {
	h := newHarness(t)
	h.key("down", "i")
	if !h.p.Editing() || h.p.VimMode() != "INSERT" {
		t.Fatalf("i must start editing in insert (editing=%v mode=%q)", h.p.Editing(), h.p.VimMode())
	}
	h.key("ctrl+a")
	h.key("ctrl+k")
	h.typ("50")
	h.key("enter")
	if h.req.URL.Query[0].Value != "50" {
		t.Fatalf("query = %v", h.req.URL.Query)
	}
}
