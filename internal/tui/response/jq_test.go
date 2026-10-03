package response

import (
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/shubham/postctl/internal/history"
	"github.com/shubham/postctl/internal/model"
	"github.com/shubham/postctl/internal/tui"
)

const jqBody = `{"name":"alpha","items":[{"id":1,"ok":true},{"id":2,"ok":false}],"big":12345678901234567890}`

func TestRunJQ(t *testing.T) {
	cases := []struct {
		name, filter, want string
	}{
		{"identity field", ".name", `"alpha"`},
		{"iterate", ".items[].id", "1\n2"},
		{"select", ".items[] | select(.ok) | .id", "1"},
		{"large integers keep their digits", ".big", "12345678901234567890"},
		{"construct", "{n: (.items | length)}", "{\n  \"n\": 2\n}"},
		{"no output", ".items[] | select(.id > 5)", ""},
		{"env is empty", "env | length", "0"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out := runJQ(c.filter, []byte(jqBody), time.Second, jqMaxOutput)
			if out.Err != "" {
				t.Fatalf("error: %s", out.Err)
			}
			if got := strings.TrimRight(out.Text, "\n"); got != c.want {
				t.Fatalf("got %q, want %q", got, c.want)
			}
		})
	}
}

func TestRunJQErrorsAreOneLineAndNeverEchoBodyData(t *testing.T) {
	body := `{"secret":"` + v3Secret + `","list":["` + v3Secret + `"],"obj":{"k":"` + v3Secret + `"}}`
	cases := []struct{ name, filter string }{
		{"syntax", ".secret |"},
		{"bad token", ".secret ~~ 1"},
		{"undefined function", "nope(.secret)"},
		{"tonumber", ".secret | tonumber"},
		{"index string", ".secret | .a"},
		{"iterate string", ".secret | .[]"},
		{"arithmetic", ".secret + 1"},
		{"object arithmetic", ".obj - 1"},
		{"error call", "error(.secret)"},
		{"error object", "error(.obj)"},
		{"fromjson", ".secret | fromjson"},
		{"regex from body", `"x" | test(.secret + "(")`},
		{"keys of string", ".secret | keys"},
		{"join", `.list | join(.obj)`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out := runJQ(c.filter, []byte(body), time.Second, jqMaxOutput)
			if out.Err == "" {
				t.Fatalf("expected an error, got output (%d bytes)", len(out.Text))
			}
			if strings.Contains(out.Err, v3Secret) || strings.Contains(out.Err, "\n") {
				t.Fatalf("error is not one clean line: %q", out.Err)
			}
			if len(out.Err) > 140 {
				t.Fatalf("error is %d bytes long", len(out.Err))
			}
			if out.Text != "" {
				t.Fatal("an error must not return partial output")
			}
		})
	}
}

func TestRunJQTimeout(t *testing.T) {
	start := time.Now()
	out := runJQ("[range(1e9)]", []byte(`{}`), jqTimeout, jqMaxOutput)
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("filter ran for %s, the deadline was %s", elapsed, jqTimeout)
	}
	if !strings.Contains(out.Err, "Timed out") {
		t.Fatalf("err = %q", out.Err)
	}
}

func TestRunJQOutputCap(t *testing.T) {
	out := runJQ("range(400000)", []byte(`{}`), 5*time.Second, jqMaxOutput)
	if out.Err != "" {
		t.Fatalf("error: %s", out.Err)
	}
	if !out.Truncated {
		t.Fatal("output over 1 MiB was not marked truncated")
	}
	if len(out.Text) > jqMaxOutput {
		t.Fatalf("kept %d bytes, cap is %d", len(out.Text), jqMaxOutput)
	}
	if !strings.HasSuffix(out.Text, "\n") {
		t.Fatal("truncation must cut at a line boundary")
	}
	small := runJQ("range(3)", []byte(`{}`), time.Second, jqMaxOutput)
	if small.Truncated {
		t.Fatal("small output marked truncated")
	}
}

func TestRunJQRejectsNonJSONAndOversizedInput(t *testing.T) {
	if out := runJQ(".", []byte("<html>"), time.Second, jqMaxOutput); out.Err == "" {
		t.Fatal("non-JSON input was accepted")
	}
	if out := runJQ(".", make([]byte, jqMaxInput+1), time.Second, jqMaxOutput); !strings.Contains(out.Err, "too large") {
		t.Fatalf("err = %q", out.Err)
	}
}

func v3JQPane(t *testing.T, body, contentType string) (*Pane, model.ID, *[]string) {
	t.Helper()
	p, id, clip := v3Pane(t, nil, v3Theme())
	res := v3Result(id, body, "", history.NewRedactor([]string{v3Secret}))
	res.Response.Headers = []model.KV{{Key: "Content-Type", Value: contentType}}
	v3Pump(t, p, tui.SendResultMsg{Result: res})
	return p, id, clip
}

// v3ApplyFilter opens the input with Space j, types the filter and presses Enter.
func v3ApplyFilter(t *testing.T, p *Pane, filter string) {
	t.Helper()
	v3RunAction(t, p, "response.jq")
	if !p.Editing() {
		t.Fatal("Space j did not open the filter input")
	}
	v3Type(t, p, filter)
	v3Pump(t, p, v3Key("enter"))
}

func TestJQFilterAppliesAndLabelsTab(t *testing.T) {
	p, _, _ := v3JQPane(t, jqBody, "application/json")
	v3ApplyFilter(t, p, ".items[].id")
	if p.Editing() {
		t.Fatal("Enter should close the input")
	}
	out := v3Frame(p)
	for _, want := range []string{"Body (jq)", "jq .items[].id", "   1 1", "   2 2"} {
		if !strings.Contains(out, want) {
			t.Fatalf("frame lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "alpha") {
		t.Fatal("the unfiltered body is still visible")
	}
}

func TestJQEscClearsAndRestoresBody(t *testing.T) {
	p, _, _ := v3JQPane(t, jqBody, "application/json")
	v3ApplyFilter(t, p, ".items[].id")
	v3Pump(t, p, v3Key("esc"))
	out := v3Frame(p)
	if strings.Contains(out, "Body (jq)") || !strings.Contains(out, "alpha") {
		t.Fatalf("Esc did not restore the plain body:\n%s", out)
	}
}

func TestJQInputEscLeavesInsertThenClears(t *testing.T) {
	p, _, _ := v3JQPane(t, jqBody, "application/json")
	v3RunAction(t, p, "response.jq")
	v3Type(t, p, ".name")
	if p.VimMode() != "INSERT" {
		t.Fatalf("mode = %q, want INSERT", p.VimMode())
	}
	v3Pump(t, p, v3Key("esc"))
	if p.VimMode() != "NORMAL" || !p.Editing() {
		t.Fatalf("first Esc: mode %q editing %v, want NORMAL and still editing", p.VimMode(), p.Editing())
	}
	v3Pump(t, p, v3Key("esc"))
	if p.Editing() || p.jq.filter != "" {
		t.Fatal("Esc in normal mode must clear the filter and close the input")
	}
}

func TestJQEmptyEnterClears(t *testing.T) {
	p, _, _ := v3JQPane(t, jqBody, "application/json")
	v3ApplyFilter(t, p, ".name")
	v3RunAction(t, p, "response.jq")
	for range 10 {
		v3Pump(t, p, tea.KeyPressMsg{Code: tea.KeyBackspace})
	}
	v3Pump(t, p, v3Key("enter"))
	if p.jq.filter != "" || p.Editing() {
		t.Fatal("an empty filter should clear jq")
	}
}

func TestJQRequiresJSONBody(t *testing.T) {
	p, _, _ := v3JQPane(t, "<html>hello</html>", "text/html")
	shell := v3RunAction(t, p, "response.jq")
	if toasts := v3Toasts(shell); len(toasts) != 1 || toasts[0] != "jq needs a JSON body." {
		t.Fatalf("toasts = %q", toasts)
	}
	if p.Editing() {
		t.Fatal("the input opened on a non-JSON body")
	}
}

func TestJQRequiresResponse(t *testing.T) {
	p, _, _ := v3Pane(t, nil, v3Theme())
	shell := v3RunAction(t, p, "response.jq")
	if toasts := v3Toasts(shell); len(toasts) != 1 || !strings.Contains(toasts[0], "send the request first") {
		t.Fatalf("toasts = %q", toasts)
	}
}

func TestJQSyntaxErrorShowsOneLineWithoutBodyData(t *testing.T) {
	p, _, _ := v3JQPane(t, `{"secret":"`+v3Secret+`"}`, "application/json")
	v3ApplyFilter(t, p, ".secret |")
	out := v3Frame(p)
	if !strings.Contains(out, "Syntax error") || !strings.Contains(out, "No jq output") {
		t.Fatalf("frame:\n%s", out)
	}
	if strings.Contains(out, v3Secret) {
		t.Fatal("the error state shows body data")
	}
	if !strings.Contains(out, "Esc") {
		t.Fatalf("the error state names no next action:\n%s", out)
	}
}

func TestJQTimeoutShowsInBar(t *testing.T) {
	p, _, _ := v3JQPane(t, `{}`, "application/json")
	v3ApplyFilter(t, p, "[range(1e9)]")
	if out := v3Frame(p); !strings.Contains(out, "Timed out") {
		t.Fatalf("frame:\n%s", out)
	}
}

func TestJQRunsOnMaskedJSONUntilRevealed(t *testing.T) {
	body := `{"token":"` + v3Secret + `","note":"` + v3Secret + `","name":"alpha"}`
	p, _, clip := v3JQPane(t, body, "application/json")
	v3ApplyFilter(t, p, ".token, .note, .name")
	out := v3Frame(p)
	if strings.Contains(out, v3Secret) {
		t.Fatalf("jq output shows a secret:\n%s", out)
	}
	if !strings.Contains(out, history.Mask) || !strings.Contains(out, "alpha") {
		t.Fatalf("expected masked values and the plain field:\n%s", out)
	}
	v3Pump(t, p, v3Key("y"))
	if len(*clip) != 1 || strings.Contains((*clip)[0], v3Secret) || !strings.Contains((*clip)[0], "alpha") {
		t.Fatalf("yank must copy the masked filtered view, got %d copies", len(*clip))
	}
	// Reveal shows the real values only for this visit, never from a stale run.
	v3Pump(t, p, v3Key("u"))
	if !strings.Contains(v3Frame(p), v3Secret) {
		t.Fatalf("reveal did not show the value:\n%s", v3Frame(p))
	}
	v3Pump(t, p, tui.SectionStepMsg{Delta: 1})
	v3Pump(t, p, tui.SectionStepMsg{Delta: -1})
	if strings.Contains(v3Frame(p), v3Secret) {
		t.Fatal("jq output stayed revealed after changing tabs")
	}
}

func TestJQNeverShowsRevealedOutputWhileRevealIsOff(t *testing.T) {
	body := `{"token":"` + v3Secret + `"}`
	p, _, _ := v3JQPane(t, body, "application/json")
	v3ApplyFilter(t, p, ".token")
	// Simulate reveal turning on and the revealed run finishing.
	p.Update(v3Key("u"))
	p.Update(jqDoneMsg{gen: p.jq.gen, reqID: p.requestID(), reveal: true, out: jqOutput{Text: "\"" + v3Secret + "\"\n"}})
	if !strings.Contains(v3Frame(p), v3Secret) {
		t.Fatal("setup: revealed output should be visible while revealed")
	}
	p.revealSecrets = false // any path that resets reveal
	out := v3Frame(p)
	if strings.Contains(out, v3Secret) {
		t.Fatalf("revealed output survived the reset:\n%s", out)
	}
	if !strings.Contains(out, "Filtering") {
		t.Fatalf("the pane should wait for a masked run:\n%s", out)
	}
}

func TestJQDropsStaleRuns(t *testing.T) {
	p, id, _ := v3JQPane(t, jqBody, "application/json")
	v3ApplyFilter(t, p, ".name")
	gen := p.jq.gen
	p.Update(jqDoneMsg{gen: gen - 1, reqID: id, out: jqOutput{Text: "\"stale\"\n"}})
	p.Update(jqDoneMsg{gen: gen, reqID: "another", out: jqOutput{Text: "\"foreign\"\n"}})
	out := v3Frame(p)
	if strings.Contains(out, "stale") || strings.Contains(out, "foreign") {
		t.Fatalf("a stale run was applied:\n%s", out)
	}
}

func TestJQReappliesAfterNewResponseAndResetsOnOtherRequest(t *testing.T) {
	p, id, _ := v3JQPane(t, `{"n":1}`, "application/json")
	v3ApplyFilter(t, p, ".n")
	v3Pump(t, p, tui.SendResultMsg{Result: v3Result(id, `{"n":42}`, "", nil)})
	if out := v3Frame(p); !strings.Contains(out, "42") || !strings.Contains(out, "Body (jq)") {
		t.Fatalf("the filter was not re-applied to the new response:\n%s", out)
	}
	v3Pump(t, p, tui.SelectionMsg{Ref: tui.RequestRef{Request: &model.Request{ID: model.NewID()}}})
	if p.jq.filter != "" {
		t.Fatal("jq filter survived selecting another request")
	}
}

func TestJQNonJSONNewResponseShowsReason(t *testing.T) {
	p, id, _ := v3JQPane(t, `{"n":1}`, "application/json")
	v3ApplyFilter(t, p, ".n")
	res := v3Result(id, "plain", "", nil)
	res.Response.Headers = []model.KV{{Key: "Content-Type", Value: "text/plain"}}
	v3Pump(t, p, tui.SendResultMsg{Result: res})
	if out := v3Frame(p); !strings.Contains(out, "The body is not JSON.") {
		t.Fatalf("frame:\n%s", out)
	}
}

func TestJQEditingKeysStayInTheInput(t *testing.T) {
	p, _, _ := v3JQPane(t, jqBody, "application/json")
	v3RunAction(t, p, "response.jq")
	v3Type(t, p, "q g [ ] u y ")
	if !p.Editing() || p.jq.filter != "" {
		t.Fatal("typing in the input leaked into pane commands")
	}
	if got := p.jq.input.Value(); got != "q g [ ] u y " {
		t.Fatalf("input = %q", got)
	}
}

func TestJQBarKeyHintsAndFits(t *testing.T) {
	p, _, _ := v3JQPane(t, jqBody, "application/json")
	v3RunAction(t, p, "response.jq")
	var keys []string
	for _, h := range p.KeyHints() {
		keys = append(keys, h.Key)
	}
	if !reflect.DeepEqual(keys, []string{"Enter", "Esc"}) {
		t.Fatalf("hints while typing = %v", keys)
	}
	v3Type(t, p, ".items")
	v3Pump(t, p, v3Key("enter"))
	found := false
	for _, h := range p.KeyHints() {
		if h.Key == "Esc" && h.Label == "Clear jq" {
			found = true
		}
	}
	if !found {
		t.Fatal("no Esc hint while a filter is applied")
	}
	for _, size := range [][2]int{{80, 24}, {120, 40}, {40, 10}} {
		p.SetSize(size[0], size[1])
		lines := strings.Split(v3Frame(p), "\n")
		if len(lines) > size[1] {
			t.Fatalf("%v: %d lines", size, len(lines))
		}
	}
}

func TestJQViewIsPure(t *testing.T) {
	p, _, _ := v3JQPane(t, jqBody, "application/json")
	v3RunAction(t, p, "response.jq")
	v3Type(t, p, ".items")
	first := p.View()
	second := p.View()
	if first != second {
		t.Fatal("View with the open input is not deterministic")
	}
	v3Pump(t, p, v3Key("enter"))
	scroll, jq := p.scroll, p.jq
	_, _ = p.View(), p.View()
	if p.scroll != scroll || !reflect.DeepEqual(p.jq.lines, jq.lines) || p.jq.gen != jq.gen {
		t.Fatal("View changed the jq state")
	}
}

func TestJQWorksWithoutHistoryLog(t *testing.T) {
	p, _, _ := v3JQPane(t, jqBody, "application/json")
	if p.historyEnabled() {
		t.Fatal("setup: no history log expected")
	}
	v3ApplyFilter(t, p, ".name")
	if !strings.Contains(v3Frame(p), `"alpha"`) {
		t.Fatalf("frame:\n%s", v3Frame(p))
	}
}
