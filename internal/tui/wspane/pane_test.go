package wspane

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/shubham/postctl/internal/model"
	"github.com/shubham/postctl/internal/tui"
	"github.com/shubham/postctl/internal/ui"
	"github.com/shubham/postctl/internal/wsx"
)

func TestConnectSendReceive(t *testing.T) {
	h := newHarness(t)
	h.mustHave("Not connected")
	c := h.connect()
	c.echo = true
	h.mustHave("Connected")
	h.mustHave("Disconnect")

	h.key("down", "i") // into the composer, typing
	h.typ(`{"type":"ping"}`)
	h.key("ctrl+s")
	h.waitLog(3) // open, sent, echo

	if got := c.sentTexts(); len(got) != 1 || got[0] != `{"type":"ping"}` {
		t.Fatalf("sent %q", got)
	}
	l := h.p.peek()
	if l.sent != 1 || l.recv != 1 {
		t.Fatalf("counts sent=%d recv=%d", l.sent, l.recv)
	}
	h.mustHave("↑ 1 sent")
	h.mustHave("↓ 1 received")
	if h.p.compErr != "" {
		t.Fatalf("text format should not need validation: %q", h.p.compErr)
	}
	// The composer keeps its text so the message can be sent again.
	if h.p.comp.Text() != `{"type":"ping"}` {
		t.Fatalf("composer = %q", h.p.comp.Text())
	}
}

func TestConnectSnapshotsRequestAndPassesDeps(t *testing.T) {
	h := newHarness(t)
	h.deps.Jar = nil
	h.connect()
	if len(h.inputs) != 1 {
		t.Fatalf("Prepare called %d times", len(h.inputs))
	}
	in := h.inputs[0]
	if in.Request == h.req || in.Collection == h.coll || in.Environment == h.env {
		t.Fatal("Prepare must get copies, not the live workspace objects")
	}
	if in.Request.ID != h.req.ID || in.Collection.ID != h.coll.ID || in.Environment.ID != h.env.ID {
		t.Fatal("copies must keep their IDs so the variable chain resolves")
	}
	if in.Globals == nil {
		t.Fatal("globals missing")
	}
}

func TestConnectNeedsAURL(t *testing.T) {
	h := newHarness(t)
	h.req.URL = model.ParseURL("")
	h.send(tui.SelectionMsg{Ref: h.ref()})
	h.key("ctrl+s")
	h.mustToast("Enter a URL")
	if h.p.state() != stIdle || len(h.inputs) != 0 {
		t.Fatal("must not connect without a URL")
	}
}

func TestConnectingShowsProgressAndEscCancels(t *testing.T) {
	h := newHarness(t)
	h.gate = make(chan struct{})
	h.key("ctrl+s")
	h.waitState(stConnecting)
	h.mustHave("Connecting")
	h.mustHave("Esc cancels")
	h.advance(1500 * time.Millisecond)
	h.mustHave("Connecting 1.5 s")

	h.key("esc")
	if h.p.state() != stIdle {
		t.Fatalf("state %d after Esc", h.p.state())
	}
	h.mustHave("Connection cancelled")
	// A dial that finishes after the cancel must not leave a connection open.
	close(h.gate)
	h.settle()
	if h.p.state() != stIdle {
		t.Fatal("a cancelled attempt must stay cancelled")
	}
}

func TestLateDialAfterCancelIsAborted(t *testing.T) {
	h := newHarness(t)
	h.deps.Prepare = func(ctx context.Context, in wsx.PrepareInput) (Session, error) {
		h.inputs = append(h.inputs, in)
		h.client = newFakeClient(h.tnow)
		return &fakeSession{h: h, client: h.client}, nil
	}
	h.p.deps.Prepare = h.deps.Prepare
	h.key("ctrl+s")
	h.waitState(stOpen)
	first := h.client
	h.send(tui.FocusSectionMsg{Section: tui.SectionURL})
	h.key("enter") // disconnect from the URL bar
	h.waitState(stIdle)
	if codes := first.closeCodes(); len(codes) != 1 || codes[0] != wsx.CloseNormal {
		t.Fatalf("close codes = %v", codes)
	}
}

func TestHandshakeRefusedShowsStatusAndBody(t *testing.T) {
	h := newHarness(t)
	h.dialErr = &wsx.HandshakeError{Status: 401, StatusText: "Unauthorized", Body: `{"error":"token expired"}`}
	h.key("ctrl+s")
	h.waitState(stIdle)
	h.waitLog(2)
	h.mustHave("Refused: 401 Unauthorized")
	h.mustHave("Details are in the log")
	h.mustHave("The server refused the connection: 401 Unauthorized")

	// The body is in the detail view of the error row.
	h.key("down", "tab", "tab") // composer, saved, log
	if h.p.sect != sLog {
		t.Fatalf("section = %d", h.p.sect)
	}
	h.key("enter")
	h.mustHave("token expired")
	h.key("esc")

	// Retrying works once the server accepts.
	h.dialErr = nil
	h.key("esc", "esc", "ctrl+s")
	h.waitState(stOpen)
	h.mustHave("Connected")
}

func TestPrepareErrorIsShown(t *testing.T) {
	h := newHarness(t)
	h.prepareErr = wsx.ErrBadScheme
	h.key("ctrl+s")
	h.waitState(stIdle)
	h.mustHave("Could not connect")
	if !strings.Contains(strings.Join(h.logTexts(), "\n"), "The URL must start with ws://") {
		t.Fatalf("log = %q", h.logTexts())
	}
}

func TestServerCloseNormalAndAbnormal(t *testing.T) {
	h := newHarness(t)
	c := h.connect()
	c.pushClose(1000, "bye")
	h.waitState(stIdle)
	h.mustHave("Disconnected")
	h.mustHave("Closed 1000 normal closure: bye")
	if h.p.peek().failed {
		t.Fatal("a normal close is not a failure")
	}

	c = h.connect()
	c.pushClose(1006, "")
	h.waitState(stIdle)
	if !h.p.peek().failed {
		t.Fatal("1006 is a failure")
	}
	h.mustHave("Closed 1006 connection lost")
	h.mustHave("Details are in the log")
}

func TestEndOfStreamWithoutCloseEvent(t *testing.T) {
	h := newHarness(t)
	h.connect()
	l := h.p.peek()
	l.cancel() // the pump's context ends: Next fails without a close event
	h.waitState(stIdle)
	h.mustHave("The connection ended unexpectedly")
}

func TestReconnectAction(t *testing.T) {
	h := newHarness(t)
	first := h.connect()
	h.runAction("ws.reconnect")
	h.waitState(stOpen)
	if h.client == first {
		t.Fatal("reconnect must dial again")
	}
	if !first.wasAborted() {
		t.Fatal("the old connection must be dropped")
	}
	h.mustHave("Reconnecting")
	if len(h.inputs) != 2 {
		t.Fatalf("Prepare calls = %d", len(h.inputs))
	}
}

func (h *harness) runAction(id string) {
	h.t.Helper()
	for _, a := range h.p.Actions() {
		if a.ID != id {
			continue
		}
		if a.Enabled != nil && !a.Enabled() {
			h.t.Fatalf("action %s is disabled", id)
		}
		h.run(a.Run())
		h.settle()
		return
	}
	h.t.Fatalf("no action %s", id)
}

func TestSendIsBlockedWhenNotConnected(t *testing.T) {
	h := newHarness(t)
	h.key("down", "tab") // saved list
	h.key("enter")
	h.mustHave("Not connected")
	if h.p.state() != stIdle {
		t.Fatal("sending a saved message must not connect")
	}
}

func TestInvalidJSONIsNotSent(t *testing.T) {
	h := newHarness(t)
	c := h.connect()
	h.key("down", "ctrl+t")
	h.mustHave("JSON")
	h.key("i")
	h.typ(`{"a":`)
	if h.p.compErr == "" {
		t.Fatal("incomplete JSON must be flagged")
	}
	h.mustHave("!")
	h.key("ctrl+s")
	h.mustToast("Not sent")
	if len(c.sentTexts()) != 0 {
		t.Fatalf("sent %q", c.sentTexts())
	}
	h.typ(`1}`)
	if h.p.compErr != "" {
		t.Fatalf("valid JSON flagged: %q", h.p.compErr)
	}
	h.mustHave("valid")
	h.key("ctrl+s")
	h.waitLog(2)
	if got := c.sentTexts(); len(got) != 1 || got[0] != `{"a":1}` {
		t.Fatalf("sent %q", got)
	}
}

func TestJSONWithVariablesValidates(t *testing.T) {
	h := newHarness(t)
	h.key("down", "ctrl+t", "i")
	h.typ(`{"n": {{count}}, "id": "{{$guid}}"}`)
	if h.p.compErr != "" {
		t.Fatalf("variables must count as values: %q", h.p.compErr)
	}
	h.key("ctrl+r")
	if !strings.Contains(h.p.comp.Text(), "{{count}}") || !strings.Contains(h.p.comp.Text(), "\n") {
		t.Fatalf("format lost the variables or did nothing: %q", h.p.comp.Text())
	}
	if h.p.compErr != "" {
		t.Fatalf("formatted text flagged: %q", h.p.compErr)
	}
}

func TestBinaryFormatChecksBase64(t *testing.T) {
	h := newHarness(t)
	c := h.connect()
	h.key("down", "ctrl+t", "ctrl+t")
	h.mustHave("Binary")
	h.key("i")
	h.typ("!!!")
	if h.p.compErr == "" {
		t.Fatal("not base64")
	}
	for range 3 {
		h.key("backspace")
	}
	h.typ("aGk=")
	if h.p.compErr != "" {
		t.Fatalf("valid base64 flagged: %q", h.p.compErr)
	}
	h.key("ctrl+s")
	h.waitLog(2)
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.sent) != 1 || c.sent[0].typ != wsx.Binary || string(c.sent[0].data) != "hi" {
		t.Fatalf("sent %+v", c.sent)
	}
}

func TestEmptyMessageIsNotSent(t *testing.T) {
	h := newHarness(t)
	c := h.connect()
	h.key("down", "ctrl+s")
	h.mustToast("The message is empty")
	if len(c.sentTexts()) != 0 {
		t.Fatal("sent an empty message")
	}
}

func TestSendFailureIsReported(t *testing.T) {
	h := newHarness(t)
	c := h.connect()
	c.mu.Lock()
	c.sendErr = errors.New("the connection is closed")
	c.mu.Unlock()
	h.key("down", "i")
	h.typ("hello")
	h.key("ctrl+s")
	h.mustToast("The connection is closed")
}

func TestSavedMessageSendsInOrder(t *testing.T) {
	h := newHarness(t)
	c := h.connect()
	h.key("down", "tab")
	if h.p.sect != sSaved {
		t.Fatalf("section = %d", h.p.sect)
	}
	h.key("enter", "down", "enter")
	h.waitLog(3)
	got := c.sentTexts()
	if len(got) != 2 || got[0] != `{"type":"subscribe","channel":"btc"}` || got[1] != "ping" {
		t.Fatalf("sent %q", got)
	}
	h.mustToast(`Sent "heartbeat"`)
}

func TestSavedJSONThatIsInvalidIsNotSent(t *testing.T) {
	h := newHarness(t)
	c := h.connect()
	h.req.WebSocket.Messages[0].Body = `{"broken":`
	h.key("down", "tab", "enter")
	h.mustToast("Not sent")
	if len(c.sentTexts()) != 0 {
		t.Fatal("sent invalid JSON")
	}
}

func TestSavedNavigationReachesLogAndBack(t *testing.T) {
	h := newHarness(t)
	h.key("down", "tab", "down", "down", "down")
	if h.p.sect != sLog {
		t.Fatalf("Down past the last saved message should reach the log, section = %d", h.p.sect)
	}
	h.key("esc")
	if h.p.sect != sComposer {
		t.Fatalf("Esc in the log returns to the composer, section = %d", h.p.sect)
	}
	h.key("esc")
	if h.p.zone != zTabs {
		t.Fatalf("Esc in the composer returns to the tabs, zone = %d", h.p.zone)
	}
}

func TestSaveTemplateAndRename(t *testing.T) {
	h := newHarness(t)
	h.req.WebSocket = nil
	h.send(tui.SelectionMsg{Ref: h.ref()})
	h.key("down", "i")
	h.typ(`{"type":"subscribe","channel":"eth"}`)
	h.key("ctrl+x")
	ms := h.p.messages()
	if len(ms) != 1 || ms[0].Name != "subscribe" || ms[0].ID == "" {
		t.Fatalf("saved = %+v", ms)
	}
	if !h.p.Editing() {
		t.Fatal("naming a message is text entry")
	}
	h.key("esc", "c", "c") // normal mode, then change the whole suggested name
	h.typ("Subscribe ETH")
	h.key("enter")
	if got := h.p.messages()[0].Name; got != "Subscribe ETH" {
		t.Fatalf("name = %q", got)
	}
	if h.p.Editing() {
		t.Fatal("still editing after Enter")
	}
	h.advance(time.Second)
	if len(h.saves()) == 0 {
		t.Fatal("no autosave request")
	}
}

func TestSaveTemplateNamesFromFieldOrNumber(t *testing.T) {
	h := newHarness(t)
	if got := h.p.suggestName(`{"action":"join"}`); got != "join" {
		t.Fatalf("name = %q", got)
	}
	if got := h.p.suggestName(`plain text`); got != "Message 4" {
		t.Fatalf("name = %q", got)
	}
}

func TestSaveEmptyTemplateRefused(t *testing.T) {
	h := newHarness(t)
	h.key("down", "ctrl+x")
	h.mustToast("Nothing to save")
	if len(h.p.messages()) != 3 {
		t.Fatal("saved an empty message")
	}
}

func TestRenameCancelKeepsName(t *testing.T) {
	h := newHarness(t)
	h.key("down", "tab", "r")
	h.typ("zzz")
	h.key("esc")
	if h.p.messages()[0].Name != "subscribe" {
		t.Fatalf("name = %q", h.p.messages()[0].Name)
	}
}

func TestEditSavedLoadsIntoComposer(t *testing.T) {
	h := newHarness(t)
	h.key("down", "tab", "down", "e")
	if h.p.sect != sComposer || h.p.comp.Text() != "ping" || h.p.format() != model.WSText {
		t.Fatalf("section %d text %q format %q", h.p.sect, h.p.comp.Text(), h.p.format())
	}
	h.key("esc", "down", "tab", "home", "e") // the JSON one
	if h.p.format() != model.WSJSON || !strings.Contains(h.p.comp.Text(), "subscribe") {
		t.Fatalf("format %q text %q", h.p.format(), h.p.comp.Text())
	}
	if d := h.p.req.WebSocket.Draft; d == nil || d.Format != model.WSJSON {
		t.Fatalf("draft = %+v", d)
	}
}

func TestDeleteSavedAsksFirst(t *testing.T) {
	h := newHarness(t)
	h.key("down", "tab", "d")
	h.mustHave("Delete saved message")
	h.key("esc")
	if len(h.p.messages()) != 3 {
		t.Fatal("Esc must cancel the delete")
	}
	h.key("d")
	h.key("enter") // the safe choice is focused first
	if len(h.p.messages()) != 3 {
		t.Fatal("Enter on the safe choice must not delete")
	}
	h.key("d", "right", "enter")
	if ms := h.p.messages(); len(ms) != 2 || ms[0].Name != "heartbeat" {
		t.Fatalf("saved = %+v", ms)
	}
}

func TestReorderSaved(t *testing.T) {
	h := newHarness(t)
	h.key("down", "tab", "alt+down")
	if ms := h.p.messages(); ms[0].Name != "heartbeat" || ms[1].Name != "subscribe" {
		t.Fatalf("saved = %+v", ms)
	}
	if h.p.savedCur != 1 {
		t.Fatalf("cursor = %d", h.p.savedCur)
	}
}

func TestComposerDraftPersistsAndAutosaves(t *testing.T) {
	h := newHarness(t)
	h.key("down", "i")
	h.typ("hi")
	if d := h.p.req.WebSocket.Draft; d == nil || d.Body != "hi" {
		t.Fatalf("draft = %+v", d)
	}
	h.advance(600 * time.Millisecond)
	saves := h.saves()
	if len(saves) != 1 || saves[0].ID != h.req.ID || saves[0].Collection != h.coll.ID {
		t.Fatalf("saves = %+v", saves)
	}
	// Erasing the draft removes it, so an untouched request stays small.
	h.key("backspace", "backspace")
	if h.p.req.WebSocket.Draft != nil {
		t.Fatalf("an empty text draft should be dropped: %+v", h.p.req.WebSocket.Draft)
	}
	// Reselecting shows the stored draft again.
	h.typ("x")
	h.send(tui.SelectionMsg{Ref: h.ref()})
	if h.p.comp.Text() != "x" {
		t.Fatalf("composer = %q", h.p.comp.Text())
	}
}

func TestBlurFlushesUnsavedEdits(t *testing.T) {
	h := newHarness(t)
	h.key("down", "i")
	h.typ("hi")
	h.run(h.p.BlurCmd())
	h.settle()
	if len(h.saves()) != 1 {
		t.Fatalf("saves = %+v", h.saves())
	}
	h.send(tui.SaveDoneMsg{Kind: tui.SaveRequest, ID: h.req.ID})
	found := false
	for _, m := range h.out {
		if d, ok := m.(tui.DirtyMsg); ok && !d.Dirty {
			found = true
		}
	}
	if !found {
		t.Fatal("dirty marker not cleared after save")
	}
	h.mustHave("Saved")
}

func TestEmptyWebSocketSectionIsTrimmed(t *testing.T) {
	h := newHarness(t)
	h.req.WebSocket = nil
	h.send(tui.SelectionMsg{Ref: h.ref()})
	h.key("down", "i")
	h.typ("a")
	h.key("backspace")
	if h.p.req.WebSocket != nil {
		t.Fatalf("empty websocket section left behind: %+v", h.p.req.WebSocket)
	}
}

// ---- log --------------------------------------------------------------------------

func TestLogFilterCyclesDirections(t *testing.T) {
	h := newHarness(t)
	c := h.connect()
	c.pushIn("alpha")
	c.pushIn("beta")
	h.key("down", "i")
	h.typ("gamma")
	h.key("ctrl+s")
	h.waitLog(5)
	h.key("esc", "tab", "tab", "f") // saved list exists: composer -> saved -> log
	if h.p.sect != sLog {
		t.Fatalf("section = %d", h.p.sect)
	}
	l := h.p.peek()
	if l.log.dir != filterIn || l.log.count() != 4 { // connecting + open + alpha + beta
		t.Fatalf("filter %v count %d", l.log.dir, l.log.count())
	}
	h.mustHave("Received")
	h.mustNotHave("text  gamma")
	h.key("f")
	if l.log.dir != filterOut || l.log.count() != 3 { // connecting + open + gamma
		t.Fatalf("filter %v count %d", l.log.dir, l.log.count())
	}
	h.key("f")
	if l.log.dir != filterAll || l.log.count() != 5 {
		t.Fatalf("filter %v count %d", l.log.dir, l.log.count())
	}
}

func TestLogSearch(t *testing.T) {
	h := newHarness(t)
	c := h.connect()
	c.pushIn(`{"symbol":"BTC","price":101}`)
	c.pushIn(`{"symbol":"ETH","price":9}`)
	h.waitLog(3)
	h.key("down", "tab", "tab", "/")
	if !h.p.Editing() {
		t.Fatal("search is text entry")
	}
	h.typ("eth")
	l := h.p.peek()
	if l.log.count() != 1 || !strings.Contains(string(l.log.at(0).data), "ETH") {
		t.Fatalf("matches = %d", l.log.count())
	}
	h.key("enter")
	if h.p.Editing() {
		t.Fatal("Enter keeps the results and leaves the field")
	}
	h.mustHave("/ eth")
	h.mustHave("1 of 4")
	h.key("esc") // clears the search
	if l.log.query != "" || l.log.count() != 4 {
		t.Fatalf("query %q count %d", l.log.query, l.log.count())
	}
}

func TestLogSearchNoMatchExplains(t *testing.T) {
	h := newHarness(t)
	c := h.connect()
	c.pushIn("hello")
	h.waitLog(2)
	h.key("down", "tab", "tab", "/")
	h.typ("zzz")
	h.mustHave("Nothing matches")
	h.key("esc")
}

func TestLogDetailShowsPrettyJSON(t *testing.T) {
	h := newHarness(t)
	c := h.connect()
	c.pushIn(`{"symbol":"BTC","levels":[1,2],"ok":true,"none":null}`)
	h.waitLog(2)
	h.key("down", "tab", "tab", "enter")
	if h.p.detail == nil || h.p.detail.kind != detailJSON {
		t.Fatalf("detail = %+v", h.p.detail)
	}
	h.mustHave(`"symbol": "BTC"`)
	h.mustHave(`"levels": [`)
	h.mustHave("JSON")
	h.key("left")
	if h.p.detail == nil || h.p.detail.kind == detailJSON {
		t.Fatalf("Left should move to the open event, detail = %+v", h.p.detail)
	}
	h.mustHave("Connected")
	h.key("right")
	if h.p.detail == nil || h.p.detail.kind != detailJSON {
		t.Fatal("Right should return to the message")
	}
	h.key("c")
	if len(h.clip) != 1 || !strings.Contains(h.clip[0], `"symbol":"BTC"`) {
		t.Fatalf("clipboard = %q", h.clip)
	}
	h.mustToast("Copied the message")
	h.key("esc")
	if h.p.detail != nil {
		t.Fatal("Esc closes the detail view")
	}
}

func TestLogDetailBinaryHexDump(t *testing.T) {
	h := newHarness(t)
	c := h.connect()
	c.pushBinary([]byte{0x00, 0x01, 0xde, 0xad, 0xbe, 0xef, 'H', 'i'})
	h.waitLog(2)
	h.key("down", "tab", "tab", "enter")
	h.mustHave("Binary")
	h.mustHave("00000000  00 01 de ad be ef 48 69")
	h.mustHave("|......Hi|")
	h.key("c")
	if len(h.clip) != 1 || h.clip[0] != "AAHerb7vSGk=" {
		t.Fatalf("clipboard = %q", h.clip)
	}
}

func TestLogFollowsUnlessScrolledBack(t *testing.T) {
	h := newHarness(t)
	c := h.connect()
	for i := range 40 {
		c.pushIn(strings.Repeat("x", i%5+1))
	}
	h.waitLog(41)
	l := h.p.peek()
	if !l.log.follow || l.log.sel != len(l.log.entries)-1 {
		t.Fatal("the log should follow new messages")
	}
	h.key("down", "tab", "tab", "up", "up")
	if l.log.follow {
		t.Fatal("moving up must stop following")
	}
	sel := l.log.current().id
	c.pushIn("new one")
	h.waitLog(42)
	if l.log.current().id != sel {
		t.Fatal("the selection must stay on its message while scrolled back")
	}
	h.mustHave("paused")
	h.key("end")
	if !l.log.follow {
		t.Fatal("End resumes following")
	}
}

func TestClearLog(t *testing.T) {
	h := newHarness(t)
	c := h.connect()
	c.pushIn("a")
	h.waitLog(2)
	h.key("down", "tab", "tab", "x")
	if len(h.p.peek().log.entries) != 0 {
		t.Fatal("log not cleared")
	}
	h.mustToast("Cleared 3 entries")
	h.mustHave("No messages yet")
}

func TestPingShowsRoundTrip(t *testing.T) {
	h := newHarness(t)
	h.connect()
	h.runAction("ws.ping")
	h.waitLog(3)
	h.mustToast("Pong in 12 ms")
	texts := strings.Join(h.logTexts(), "|")
	if !strings.Contains(texts, "12 ms") {
		t.Fatalf("log = %s", texts)
	}
}

func TestLogIsBounded(t *testing.T) {
	l := newLog()
	for i := range maxEntries + 100 {
		l.add(entry{kind: ekMessage, dir: wsx.DirIn, data: []byte{byte(i)}, size: 1})
	}
	if len(l.entries) != maxEntries || l.trimmed != 100 {
		t.Fatalf("entries %d trimmed %d", len(l.entries), l.trimmed)
	}
	if !l.follow || l.sel != maxEntries-1 {
		t.Fatalf("follow %v sel %d", l.follow, l.sel)
	}
	// A cursor scrolled back keeps its message as old ones fall off.
	l.follow = false
	l.sel = 500
	id := l.current().id
	l.add(entry{kind: ekMessage, data: []byte("x"), size: 1})
	if l.current().id != id {
		t.Fatal("cursor moved to another message")
	}
}

func TestLogTrimKeepsFilteredView(t *testing.T) {
	l := newLog()
	l.dir = filterIn
	for i := range maxEntries + 10 {
		d := wsx.DirIn
		if i%2 == 1 {
			d = wsx.DirOut
		}
		l.add(entry{kind: ekMessage, dir: d, data: []byte("m"), size: 1})
	}
	l.refilter()
	for i := 0; i < l.count(); i++ {
		if l.at(i).dir != wsx.DirIn {
			t.Fatal("filter leaked an entry")
		}
	}
	if l.count() != maxEntries/2 {
		t.Fatalf("visible = %d", l.count())
	}
}

func TestLogByteBudget(t *testing.T) {
	l := newLog()
	big := make([]byte, maxEntryBytes)
	for range 100 {
		l.add(entry{kind: ekMessage, dir: wsx.DirIn, data: big, size: len(big)})
	}
	if l.size > maxLogBytes+maxEntryBytes {
		t.Fatalf("log holds %d bytes", l.size)
	}
	l.add(entry{kind: ekMessage, dir: wsx.DirIn, data: make([]byte, maxEntryBytes+5), size: maxEntryBytes + 5})
	last := l.entries[len(l.entries)-1]
	if !last.truncated || len(last.data) != maxEntryBytes {
		t.Fatalf("truncated=%v len=%d", last.truncated, len(last.data))
	}
}

// ---- other tabs -----------------------------------------------------------------------

func TestURLEditUpdatesParams(t *testing.T) {
	h := newHarness(t)
	h.key("up", "A") // URL bar, appending
	if !h.p.Editing() {
		t.Fatal("the URL bar is text entry")
	}
	h.typ("&depth=5")
	if got := h.req.URL.Raw; !strings.HasSuffix(got, "&depth=5") {
		t.Fatalf("url = %q", got)
	}
	found := false
	for _, q := range h.req.URL.Query {
		if q.Key == "depth" && q.Value == "5" {
			found = true
		}
	}
	if !found {
		t.Fatalf("query = %+v", h.req.URL.Query)
	}
	h.advance(time.Second)
	if len(h.saves()) != 1 {
		t.Fatalf("saves = %d", len(h.saves()))
	}
}

func TestURLBarEnterConnectsAndDisconnects(t *testing.T) {
	h := newHarness(t)
	h.key("up", "enter")
	h.waitState(stOpen)
	h.key("enter")
	h.waitState(stIdle)
	h.mustHave("Disconnected")
}

func TestParamsTabEditsQuery(t *testing.T) {
	h := newHarness(t)
	h.gotoTab(tabParams)
	h.key("down", "a")
	h.typ("depth")
	h.key("tab")
	h.typ("7")
	h.key("esc", "esc")
	if !strings.Contains(h.req.URL.Raw, "depth=7") {
		t.Fatalf("url = %q", h.req.URL.Raw)
	}
	h.mustHave("depth")
	h.key("space") // toggle off
	if strings.Contains(h.req.URL.Raw, "depth=7") {
		t.Fatalf("a disabled row must leave the URL: %q", h.req.URL.Raw)
	}
}

func TestHeadersTabEditsHeaders(t *testing.T) {
	h := newHarness(t)
	h.gotoTab(tabHeaders)
	h.key("down")
	h.mustHave("X-Client")
	h.key("a")
	h.typ("Authorization")
	h.key("tab")
	h.typ("Bearer {{token}}")
	h.key("esc", "esc")
	if got := h.req.Headers; len(got) != 3 || got[1].Key != "Authorization" || got[1].Value != "Bearer {{token}}" || got[1].Disabled {
		t.Fatalf("headers = %+v", got)
	}
	h.key("d")
	if len(h.req.Headers) != 2 {
		t.Fatalf("headers = %+v", h.req.Headers)
	}
}

func TestHeadersTabHidesNothingAndShowsCount(t *testing.T) {
	h := newHarness(t)
	h.mustHave("Headers(1)")
	h.mustHave("Messages(3)")
	h.mustHave("Params(1)")
}

func TestAuthTabTypesAndInheritance(t *testing.T) {
	h := newHarness(t)
	h.coll.Auth = &model.Auth{Type: model.AuthBearer, Bearer: &model.BearerAuth{Token: "abc"}}
	h.send(tui.SelectionMsg{Ref: h.ref()})
	h.gotoTab(tabAuth)
	h.key("down")
	h.mustHave("Inherit from parent")
	h.mustHave("Bearer Token")
	h.mustHave(`collection "Trading"`)

	h.key("right", "right") // No Auth, Bearer
	if h.req.Auth == nil || h.req.Auth.Type != model.AuthBearer {
		t.Fatalf("auth = %+v", h.req.Auth)
	}
	h.key("down", "i")
	h.typ("hunter2-literal")
	h.key("esc", "esc")
	if h.req.Auth.Bearer.Token != "hunter2-literal" {
		t.Fatalf("token = %q", h.req.Auth.Bearer.Token)
	}
	h.mustNotHave("hunter2")
	h.key("r") // reveal
	h.mustHave("hunter2-literal")
	h.key("r")
	h.mustNotHave("hunter2")

	// Switching type and back restores what was typed.
	h.key("up", "right", "left")
	if h.req.Auth.Bearer == nil || h.req.Auth.Bearer.Token != "hunter2-literal" {
		t.Fatalf("stash lost: %+v", h.req.Auth)
	}
}

func TestAuthTabWarnsAboutUnsupportedScheme(t *testing.T) {
	h := newHarness(t)
	h.req.Auth = &model.Auth{Type: model.AuthDigest, Digest: &model.DigestAuth{}}
	h.send(tui.SelectionMsg{Ref: h.ref()})
	h.gotoTab(tabAuth)
	h.key("down")
	h.mustHave("Digest Auth")
	h.mustHave("cannot use this scheme")
}

func TestSettingsTabEditsWebSocketSettings(t *testing.T) {
	h := newHarness(t)
	h.gotoTab(tabSettings)
	h.key("down", "i")
	h.typ("graphql-transport-ws, v2")
	h.key("esc")
	if got := h.req.WebSocket.Subprotocols; len(got) != 2 || got[0] != "graphql-transport-ws" || got[1] != "v2" {
		t.Fatalf("subprotocols = %q", got)
	}
	h.key("down", "i")
	h.typ("30s")
	h.key("esc")
	if pi := h.req.WebSocket.PingInterval; pi == nil || *pi != 30*time.Second {
		t.Fatalf("ping = %v", pi)
	}
	h.key("down", "i")
	h.typ("2 MB")
	h.key("esc")
	if mb := h.req.WebSocket.MaxMessageBytes; mb == nil || *mb != 2<<20 {
		t.Fatalf("max = %v", mb)
	}
	h.key("down", "i")
	h.typ("45s")
	h.key("esc")
	if to := h.req.Settings.Timeout; to == nil || *to != 45*time.Second {
		t.Fatalf("timeout = %v", to)
	}
	h.key("down", "space") // verify TLS off
	if v := h.req.Settings.VerifyTLS; v == nil || *v {
		t.Fatalf("verify = %v", v)
	}
	h.mustHave("certificates are not checked")
	h.key("down", "space") // cookies off
	if v := h.req.Settings.DisableCookies; v == nil || !*v {
		t.Fatalf("disable cookies = %v", v)
	}
	h.mustHave("Pre-request and test scripts do not run")
}

func TestSettingsRejectBadValues(t *testing.T) {
	h := newHarness(t)
	h.gotoTab(tabSettings)
	h.key("down", "down", "i")
	h.typ("soon")
	h.mustHave("use 30s or 1m")
	h.key("esc")
	if h.req.WebSocket.PingInterval != nil {
		t.Fatal("a bad value must not be stored")
	}
	h.key("down", "i")
	h.typ("lots")
	h.mustHave("use 2 MB or 512 KB")
	h.key("esc")
	if h.req.WebSocket.MaxMessageBytes != nil {
		t.Fatal("a bad value must not be stored")
	}
}

func TestParseSizeAndTimeout(t *testing.T) {
	for in, want := range map[string]int64{"1024": 1024, "2mb": 2 << 20, "512 KB": 512 << 10, "1.5MiB": 3 << 19, "1gb": 1 << 30, "10b": 10} {
		if got, ok := parseSize(in); !ok || got != want {
			t.Errorf("parseSize(%q) = %d, %v", in, got, ok)
		}
	}
	for _, in := range []string{"", "0", "-1", "abc", "mb"} {
		if _, ok := parseSize(in); ok {
			t.Errorf("parseSize(%q) accepted", in)
		}
	}
	if d, ok := parseTimeout("90"); !ok || d != 90*time.Second {
		t.Errorf("parseTimeout(90) = %v %v", d, ok)
	}
	if _, ok := parseTimeout("-3s"); ok {
		t.Error("negative timeout accepted")
	}
}

func TestUnresolvedVariableIsFlagged(t *testing.T) {
	h := newHarness(t)
	h.req.URL = model.ParseURL("{{host}}/stream")
	h.send(tui.SelectionMsg{Ref: h.ref()})
	h.mustHave("unresolved: host")
}

func TestPreviewNeverShowsSecrets(t *testing.T) {
	h := newHarness(t)
	h.req.URL = model.ParseURL("{{baseUrl}}/stream?token={{token}}")
	h.send(tui.SelectionMsg{Ref: h.ref()})
	h.key("up")
	h.mustHave("wss://staging.example.test/stream?token=")
	h.mustNotHave("s3cr3t-value")
}

// ---- selection and shell integration -----------------------------------------------------

func TestConnectionSurvivesSelectionChange(t *testing.T) {
	h := newHarness(t)
	first := h.connect()

	other := model.NewWebSocketRequest("Other", "wss://other.example.test/x")
	h.folder.Items = append(h.folder.Items, other)
	h.send(tui.SelectionMsg{Ref: tui.RequestRef{Collection: h.coll, Request: other}})
	h.mustHave("Not connected")
	if h.p.state() != stIdle {
		t.Fatal("the other request has no connection")
	}
	first.pushIn("while away")
	deadline := time.Now().Add(2 * time.Second)
	for {
		h.settle()
		l := h.p.lives[h.req.ID]
		if len(l.log.entries) >= 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("events stopped arriving for the request that is not selected")
		}
	}
	h.send(tui.SelectionMsg{Ref: h.ref()})
	h.mustHave("Connected")
	h.mustHave("while away")
}

func TestSelectionOfOtherKindAndNothingIsHandled(t *testing.T) {
	h := newHarness(t)
	h.send(tui.SelectionMsg{})
	h.mustHave("No WebSocket request selected")
	if h.p.Editing() {
		t.Fatal("nothing to edit")
	}
	h.key("ctrl+s", "down", "enter") // must not panic
	http := model.NewRequest("Get", "GET", "https://x.test")
	h.send(tui.SelectionMsg{Ref: tui.RequestRef{Collection: h.coll, Request: http}})
	h.mustHave("Not a WebSocket request")
	if len(h.p.Actions()) == 0 {
		t.Fatal("actions still listed")
	}
	for _, a := range h.p.Actions() {
		if a.Enabled != nil && a.Enabled() {
			t.Fatalf("action %s enabled with no WebSocket request", a.ID)
		}
	}
}

func TestSelectionProblemFile(t *testing.T) {
	h := newHarness(t)
	h.send(tui.SelectionMsg{Ref: tui.RequestRef{Problem: nil}})
	h.mustHave("No WebSocket request selected")
}

func TestReloadKeepsUnsavedEdits(t *testing.T) {
	h := newHarness(t)
	h.key("down", "i")
	h.typ("draft text")
	fresh := h.req.Clone()
	fresh.WebSocket = &model.WebSocketSettings{}
	f := h.folder
	f.Items = []model.Item{fresh}
	h.send(tui.WorkspaceReloadedMsg{Workspace: h.ws})
	h.send(tui.SelectionMsg{Ref: tui.RequestRef{Collection: h.coll, Request: fresh}})
	if fresh.WebSocket == nil || fresh.WebSocket.Draft == nil || fresh.WebSocket.Draft.Body != "draft text" {
		t.Fatalf("unsaved draft lost after reload: %+v", fresh.WebSocket)
	}
	if h.p.comp.Text() != "draft text" {
		t.Fatalf("composer = %q", h.p.comp.Text())
	}
}

func TestThemeChangeRestylesEverything(t *testing.T) {
	h := newHarness(t)
	h.connect()
	h.send(tui.ThemeChangedMsg{Theme: ui.NewTheme(false, true, true)})
	h.mustHave(">") // ASCII selection marker
	h.mustNotHave("›")
}

func TestPasteIntoURLAndComposer(t *testing.T) {
	h := newHarness(t)
	h.key("up", "A")
	h.paste("/extra")
	if !strings.HasSuffix(h.req.URL.Raw, "/extra") {
		t.Fatalf("url = %q", h.req.URL.Raw)
	}
	h.key("esc", "esc", "down")
	h.paste("line one\nline two")
	if h.p.comp.Text() != "line one\nline two" {
		t.Fatalf("composer = %q", h.p.comp.Text())
	}
}

func TestEditingReportsTextEntry(t *testing.T) {
	h := newHarness(t)
	if h.p.Editing() {
		t.Fatal("tab bar is not text entry")
	}
	h.key("down")
	if !h.p.Editing() {
		t.Fatal("the composer is text entry")
	}
	h.key("esc")
	if h.p.Editing() {
		t.Fatal("Esc leaves text entry")
	}
}

func TestKeyHintsFollowState(t *testing.T) {
	h := newHarness(t)
	find := func(label string) bool {
		for _, k := range h.p.KeyHints() {
			if strings.HasPrefix(k.Label, label) {
				return true
			}
		}
		return false
	}
	if !find("Connect") {
		t.Fatalf("hints = %+v", h.p.KeyHints())
	}
	h.connect()
	if !find("Send") || find("Connect") && !find("Disconnect") {
		t.Fatalf("hints = %+v", h.p.KeyHints())
	}
	h.p.enhanced = true
	if h.p.KeyHints()[len(h.p.KeyHints())-1].Key != "Ctrl-Enter" {
		t.Fatalf("hints = %+v", h.p.KeyHints())
	}
	h.key("down")
	for _, want := range []string{"Format", "Save message"} {
		if !find(want) {
			t.Fatalf("composer hints lack %q: %+v", want, h.p.KeyHints())
		}
	}
	h.gate = make(chan struct{})
	h.key("esc")
	h.key("ctrl+s") // already open: sends
	h.mustToast("The message is empty")
}

func TestActionsAreEnabledByState(t *testing.T) {
	h := newHarness(t)
	enabled := func(id string) bool {
		for _, a := range h.p.Actions() {
			if a.ID == id {
				return a.Enabled == nil || a.Enabled()
			}
		}
		t.Fatalf("no action %s", id)
		return false
	}
	if !enabled("ws.connect") || enabled("ws.disconnect") || enabled("ws.send") || enabled("ws.ping") || enabled("ws.copyMessage") {
		t.Fatal("idle state actions wrong")
	}
	h.connect()
	if enabled("ws.connect") || !enabled("ws.disconnect") || !enabled("ws.send") || !enabled("ws.ping") || !enabled("ws.copyMessage") {
		t.Fatal("open state actions wrong")
	}
	h.runAction("ws.disconnect")
	h.waitState(stIdle)
	h.runAction("ws.searchLog")
	if !h.p.searching || h.p.sect != sLog {
		t.Fatal("search action should focus the log's search field")
	}
	var focused bool
	for _, m := range h.out {
		if f, ok := m.(tui.FocusPaneMsg); ok && f.Slot == tui.SlotRequest {
			focused = true
		}
	}
	if !focused {
		t.Fatal("actions must focus the pane")
	}
	h.key("esc")
	h.runAction("ws.tab.headers")
	if h.p.tab != tabHeaders || h.p.zone != zBody {
		t.Fatalf("tab %d zone %d", h.p.tab, h.p.zone)
	}
}

func TestTitleNamesTheRequest(t *testing.T) {
	h := newHarness(t)
	if got := h.p.Title(); !strings.Contains(got, "Live prices") || !strings.HasPrefix(got, "WebSocket") {
		t.Fatalf("title = %q", got)
	}
}

func TestViewFitsItsSize(t *testing.T) {
	for _, sz := range sizes {
		h := newHarness(t, withLook(looks()[1], sz.w, sz.h))
		c := h.connect()
		for i := range 30 {
			c.pushIn(`{"i":` + strings.Repeat("9", i%12+1) + `,"text":"` + strings.Repeat("long ", 30) + `"}`)
		}
		h.waitLog(31)
		for _, tb := range []tabID{tabMessages, tabParams, tabHeaders, tabAuth, tabSettings} {
			h.gotoTab(tb)
			lines := strings.Split(h.view(), "\n")
			if len(lines) != sz.h {
				t.Errorf("%s tab %d: %d lines, want %d", sz.name, tb, len(lines), sz.h)
			}
			for _, l := range lines {
				if ui.Width(l) > sz.w {
					t.Errorf("%s tab %d: line wider than %d: %q", sz.name, tb, sz.w, l)
				}
			}
		}
		h.gotoTab(tabMessages)
		h.key("down", "tab", "tab", "enter")
		for _, l := range strings.Split(h.view(), "\n") {
			if ui.Width(l) > sz.w {
				t.Errorf("%s detail: line wider than %d: %q", sz.name, sz.w, l)
			}
		}
	}
}

func TestSmallSizesDoNotPanic(t *testing.T) {
	for _, sz := range [][2]int{{40, 8}, {50, 10}, {60, 12}, {99, 14}, {100, 9}, {140, 30}, {20, 5}, {1, 1}} {
		h := newHarness(t, withLook(looks()[1], sz[0], sz[1]))
		c := h.connect()
		c.pushIn("hello")
		h.waitLog(2)
		for _, tb := range []tabID{tabMessages, tabParams, tabHeaders, tabAuth, tabSettings} {
			h.gotoTab(tb)
			_ = h.p.View()
			h.key("down", "tab", "tab", "enter", "esc", "esc", "esc")
			_ = h.p.View()
		}
	}
}

func TestDirectionIsNotOnlyColor(t *testing.T) {
	h := newHarness(t, withLook(looks()[1], 100, 30))
	c := h.connect()
	c.pushIn("from server")
	h.key("down", "i")
	h.typ("from me")
	h.key("ctrl+s")
	h.waitLog(3)
	v := h.view()
	if !strings.Contains(v, "↓") || !strings.Contains(v, "↑") {
		t.Fatalf("direction arrows missing without color:\n%s", v)
	}
	h = newHarness(t, withLook(looks()[2], 100, 30))
	c = h.connect()
	c.pushIn("from server")
	h.waitLog(2)
	v = h.view()
	if strings.Contains(v, "↓") || strings.Contains(v, "●") {
		t.Fatalf("ASCII mode drew Unicode glyphs:\n%s", v)
	}
	if !strings.Contains(v, "< ") || !strings.Contains(v, "text") {
		t.Fatalf("ASCII direction glyph or type label missing:\n%s", v)
	}
}

func TestMessageEventsAreBatched(t *testing.T) {
	h := newHarness(t)
	c := h.connect()
	for i := range 1000 {
		c.pushIn(strings.Repeat("m", i%7+1))
	}
	h.waitLog(1001)
	if h.p.peek().recv != 1000 {
		t.Fatalf("recv = %d", h.p.peek().recv)
	}
}

var _ = tea.Batch

func TestSectionsFocusAndStepTabs(t *testing.T) {
	h := newHarness(t)
	for i, name := range tabSections {
		h.send(tui.FocusSectionMsg{Section: name})
		if h.p.tab != tabID(i) {
			t.Errorf("%s: tab = %v, want %v", name, h.p.tab, tabID(i))
		}
	}
	h.send(tui.SectionStepMsg{Delta: 1})
	if h.p.tab != 0 {
		t.Fatalf("] after the last tab -> %v", h.p.tab)
	}
	h.send(tui.FocusSectionMsg{Section: tui.SectionURL})
	if h.p.zone != zBar {
		t.Fatalf("gu: zone = %v", h.p.zone)
	}
	if len(h.p.Sections()) != 1+len(tabSections) {
		t.Fatalf("Sections = %v", h.p.Sections())
	}
}

// Every advertised Action.Keys alternative is a shell binding for the action,
// or one of the pane's own keys listed here (handled in the pane's Update and
// covered by its key tests).
func TestActionKeysAreRealBindings(t *testing.T) {
	own := map[string]bool{"Ctrl-R": true, "Ctrl-T": true, "Ctrl-X": true, "c": true, "f": true, "/": true, "x": true}
	h := newHarness(t)
	for _, act := range h.p.Actions() {
		for _, bad := range tui.UnboundActionKeys(act.ID, act.Keys) {
			if !own[bad] {
				t.Errorf("%s advertises %q, which nothing dispatches to it", act.ID, bad)
			}
		}
	}
}

// i on a key-value row starts editing its cell in insert, exactly like Enter.
func TestIOnAKVRowEditsLikeEnter(t *testing.T) {
	h := newHarness(t)
	h.gotoTab(tabHeaders)
	h.key("down", "i")
	if !h.p.Editing() {
		t.Fatal("i must start editing the header cell")
	}
	h.typ("X")
	h.key("esc")
	if got := h.req.Headers[0].Value; !strings.Contains(got, "X") {
		t.Fatalf("typed text did not reach the cell: %+v", h.req.Headers)
	}
}
