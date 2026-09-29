package script

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/shubham/postctl/internal/httpx"
	"github.com/shubham/postctl/internal/model"
	"github.com/shubham/postctl/internal/vars"
)

var update = flag.Bool("update", false, "rewrite the expected block of every compat corpus case")

// The compat corpus lives in testdata/corpus: one <name>.js script that mirrors
// a real Postman idiom, plus a <name>.json case file holding the inputs and the
// expected outcome. Regenerate the expected blocks with
//
//	go test ./internal/script -run TestCorpus -update
//
// and read the diff: the invariant the generator cannot fake is that every case
// must run without a ScriptError and with every test passing, unless its name
// starts with "failing-".
const corpusDir = "testdata/corpus"

type corpusRequest struct {
	Method   string            `json:"method,omitempty"`
	URL      string            `json:"url,omitempty"`
	Headers  map[string]string `json:"headers,omitempty"`
	BodyMode string            `json:"bodyMode,omitempty"`
	Body     string            `json:"body,omitempty"`
	// Auth is written as "type:key=value,key=value", e.g. "bearer:token=x".
	Auth string `json:"auth,omitempty"`
}

type corpusResponse struct {
	Code    int               `json:"code"`
	Status  string            `json:"status,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    string            `json:"body,omitempty"`
	TimeMs  int               `json:"timeMs,omitempty"`
	// SetCookies are the cookies the response carried, as pm.response.cookies
	// exposes them. The case-level Cookies field seeds the jar instead.
	SetCookies []model.Cookie `json:"setCookies,omitempty"`
	// Match routes this canned response to any sent request whose URL
	// contains it, for cases where the call order is not deterministic.
	Match string `json:"match,omitempty"`
}

type corpusInfo struct {
	RequestName    string `json:"requestName,omitempty"`
	RequestID      string `json:"requestId,omitempty"`
	CollectionName string `json:"collectionName,omitempty"`
	Iteration      int    `json:"iteration,omitempty"`
	IterationCount int    `json:"iterationCount,omitempty"`
}

// corpusExpect is the golden half of a case. Everything in it is rendered as
// sorted, readable strings so a diff is easy to review.
type corpusExpect struct {
	Tests        []string            `json:"tests,omitempty"`
	Console      []string            `json:"console,omitempty"`
	VarChanges   map[string][]string `json:"varChanges,omitempty"`
	Request      []string            `json:"request,omitempty"`
	SentRequests []string            `json:"sentRequests,omitempty"`
	NextRequest  *string             `json:"nextRequest,omitempty"`
	SkipRequest  bool                `json:"skipRequest,omitempty"`
	Error        string              `json:"error,omitempty"`
}

type corpusCase struct {
	Description   string                    `json:"description"`
	Event         Event                     `json:"event,omitempty"`
	Info          *corpusInfo               `json:"info,omitempty"`
	Request       *corpusRequest            `json:"request,omitempty"`
	Response      *corpusResponse           `json:"response,omitempty"`
	Vars          map[string]map[string]any `json:"vars,omitempty"`
	Cookies       []model.Cookie            `json:"cookies,omitempty"`
	SendResponses []corpusResponse          `json:"sendResponses,omitempty"`
	Expect        corpusExpect              `json:"expect"`
}

func (c corpusCase) event() Event {
	if c.Event != "" {
		return c.Event
	}
	if c.Response != nil {
		return EventTest
	}
	return EventPreRequest
}

func (c corpusCase) modelRequest() *model.Request {
	spec := c.Request
	if spec == nil {
		spec = &corpusRequest{}
	}
	method, rawURL := spec.Method, spec.URL
	if method == "" {
		method = "GET"
	}
	if rawURL == "" {
		rawURL = "https://api.example.com/v1/orders?page=1"
	}
	r := model.NewRequest("Corpus request", method, rawURL)
	r.ID = "req-corpus"
	for _, k := range sortedStringKeys(spec.Headers) {
		r.Headers = append(r.Headers, model.KV{Key: k, Value: spec.Headers[k]})
	}
	if spec.Body != "" {
		r.Body = corpusBody(model.BodyMode(spec.BodyMode), spec.Body)
	}
	if spec.Auth != "" {
		r.Auth = corpusAuth(spec.Auth)
	}
	return r
}

// corpusAuth reads "type:key=value,key=value" into the typed model auth the
// few cases that exercise pm.request.auth need.
func corpusAuth(spec string) *model.Auth {
	kind, rest, _ := strings.Cut(spec, ":")
	params := map[string]string{}
	for _, pair := range strings.Split(rest, ",") {
		k, v, _ := strings.Cut(pair, "=")
		params[k] = v
	}
	switch model.AuthType(kind) {
	case model.AuthBearer:
		return &model.Auth{Type: model.AuthBearer, Bearer: &model.BearerAuth{Token: params["token"]}}
	case model.AuthBasic:
		return &model.Auth{Type: model.AuthBasic, Basic: &model.BasicAuth{
			Username: params["username"], Password: params["password"],
		}}
	case model.AuthAPIKey:
		return &model.Auth{Type: model.AuthAPIKey, APIKey: &model.APIKeyAuth{
			Key: params["key"], Value: params["value"], In: model.APIKeyLocation(params["in"]),
		}}
	default:
		panic("corpus: unsupported auth spec " + spec)
	}
}

// corpusBody keeps the case files compact: the body is always one string, and
// the mode decides how it is read. urlencoded and formdata take "a=1&b=2",
// where a formdata value of "file:path" makes a file part.
func corpusBody(mode model.BodyMode, text string) model.Body {
	switch mode {
	case model.BodyURLEncoded:
		b := model.Body{Mode: mode}
		for _, pair := range strings.Split(text, "&") {
			k, v, _ := strings.Cut(pair, "=")
			b.URLEncoded = append(b.URLEncoded, model.KV{Key: k, Value: v})
		}
		return b
	case model.BodyFormData:
		b := model.Body{Mode: mode}
		for _, pair := range strings.Split(text, "&") {
			k, v, _ := strings.Cut(pair, "=")
			if src, ok := strings.CutPrefix(v, "file:"); ok {
				b.FormData = append(b.FormData, model.FormPart{Key: k, Type: model.FormFile, Src: src})
				continue
			}
			b.FormData = append(b.FormData, model.FormPart{Key: k, Type: model.FormText, Value: v})
		}
		return b
	case model.BodyGraphQL:
		return model.Body{Mode: mode, GraphQL: &model.GraphQLBody{Query: text}}
	case "":
		return model.Body{Mode: model.BodyRaw, Raw: text, Language: "json"}
	default:
		return model.Body{Mode: mode, Raw: text, Language: "json"}
	}
}

func (r corpusResponse) httpx() *httpx.Response {
	status := r.Status
	if status == "" {
		status = statusText(r.Code)
	}
	ms := r.TimeMs
	if ms == 0 {
		ms = 12
	}
	out := &httpx.Response{
		StatusCode: r.Code,
		Status:     status,
		Body:       []byte(r.Body),
		Cookies:    r.SetCookies,
		Timings:    httpx.Timings{Total: time.Duration(ms) * time.Millisecond},
		Size:       httpx.Size{Headers: 64, Body: int64(len(r.Body))},
	}
	headers := r.Headers
	if headers == nil {
		headers = map[string]string{"Content-Type": "application/json"}
	}
	for _, k := range sortedStringKeys(headers) {
		out.Headers = append(out.Headers, model.KV{Key: k, Value: headers[k]})
	}
	return out
}

func sortedStringKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

var corpusScopes = map[string]vars.Scope{
	"globals": vars.Global, "collection": vars.Collection,
	"environment": vars.Environment, "data": vars.Data, "local": vars.Local,
}

var corpusScopeNames = map[vars.Scope]string{
	vars.Global: "globals", vars.Collection: "collection",
	vars.Environment: "environment", vars.Data: "data", vars.Local: "local",
}

func TestCorpus(t *testing.T) {
	specs, err := filepath.Glob(filepath.Join(corpusDir, "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(specs) < 40 {
		t.Errorf("the compat corpus has %d cases, the brief requires at least 40", len(specs))
	}
	sort.Strings(specs)

	for _, spec := range specs {
		name := strings.TrimSuffix(filepath.Base(spec), ".json")
		t.Run(name, func(t *testing.T) { runCorpusCase(t, name, spec) })
	}
}

func runCorpusCase(t *testing.T, name, specPath string) {
	t.Helper()

	raw, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatal(err)
	}
	var c corpusCase
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&c); err != nil {
		t.Fatalf("%s: %v", specPath, err)
	}
	if c.Description == "" {
		t.Errorf("%s: every case needs a description", specPath)
	}

	source, err := os.ReadFile(filepath.Join(corpusDir, name+".js"))
	if err != nil {
		t.Fatal(err)
	}

	store := newStore()
	for scopeName, kv := range c.Vars {
		scope, ok := corpusScopes[scopeName]
		if !ok {
			t.Fatalf("unknown variable scope %q", scopeName)
		}
		store.with(scope, kv)
	}

	sender := &corpusSender{responses: c.SendResponses}
	jar := newJar()
	for _, ck := range c.Cookies {
		if err := jar.SetCookie("https://api.example.com/", ck); err != nil {
			t.Fatal(err)
		}
	}

	in := Input{
		Event:   c.event(),
		Request: c.modelRequest(),
		Vars:    store,
		Sender:  sender,
		Cookies: jar,
		Info:    Info{RequestName: "Corpus request", RequestID: "req-corpus", CollectionName: "Corpus", IterationCount: 1},
	}
	if c.Info != nil {
		in.Info = Info{
			RequestName: c.Info.RequestName, RequestID: c.Info.RequestID,
			CollectionName: c.Info.CollectionName,
			Iteration:      c.Info.Iteration, IterationCount: c.Info.IterationCount,
		}
	}
	if c.Response != nil {
		in.Response = c.Response.httpx()
	}

	res, err := Run(context.Background(), []*model.Script{{File: name + ".js", Source: string(source)}}, in)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	got := renderExpect(res, store, sender, in.Event)

	// The invariants that hold regardless of the golden file.
	expectFailure := strings.Contains(name, "failing")
	if !expectFailure {
		if res.Err != nil {
			t.Fatalf("unexpected script error: %v\n%s", res.Err, res.Err.Stack)
		}
		for _, tr := range res.Tests {
			if !tr.Passed {
				t.Errorf("test %q failed: %s", tr.Name, tr.Error)
			}
		}
	}

	if *update {
		c.Expect = got
		out, err := json.MarshalIndent(c, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(specPath, append(out, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}

	if !reflect.DeepEqual(got, c.Expect) {
		t.Errorf("outcome does not match %s\n got: %s\nwant: %s\n(re-run with -update after checking the diff)",
			specPath, mustJSON(got), mustJSON(c.Expect))
	}
}

func mustJSON(v any) string {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err.Error()
	}
	return string(b)
}

func renderExpect(res *Result, store *fakeStore, sender *corpusSender, event Event) corpusExpect {
	out := corpusExpect{NextRequest: res.NextRequest, SkipRequest: res.SkipRequest}

	for _, tr := range res.Tests {
		state := "PASS"
		switch {
		case tr.Skipped:
			state = "SKIP"
		case !tr.Passed:
			state = "FAIL"
		}
		line := state + " " + tr.Name
		if tr.Error != "" {
			line += " | " + firstLine(tr.Error)
		}
		out.Tests = append(out.Tests, line)
	}

	for _, e := range res.Console {
		out.Console = append(out.Console, string(e.Level)+": "+e.Message)
	}

	for scope, changes := range store.Changes() {
		if len(changes) == 0 {
			continue
		}
		lines := make([]string, 0, len(changes))
		for _, ch := range changes {
			if ch.Unset {
				lines = append(lines, "unset "+ch.Key)
				continue
			}
			lines = append(lines, "set "+ch.Key+"="+toString(ch.Value))
		}
		if out.VarChanges == nil {
			out.VarChanges = map[string][]string{}
		}
		out.VarChanges[corpusScopeNames[scope]] = lines
	}

	if event == EventPreRequest && res.Request != nil {
		out.Request = renderRequest(res.Request)
	}

	out.SentRequests = append(out.SentRequests, sender.sentRequests()...)

	if res.Err != nil {
		out.Error = res.Err.Name + ": " + res.Err.Message
	}
	return out
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// renderRequest describes a request compactly enough to review in a diff.
func renderRequest(r *model.Request) []string {
	lines := []string{r.Method + " " + r.URL.Raw}
	for _, h := range r.Headers {
		suffix := ""
		if h.Disabled {
			suffix = " (disabled)"
		}
		lines = append(lines, "header "+h.Key+": "+h.Value+suffix)
	}
	switch r.Body.Mode {
	case "", model.BodyNone:
	case model.BodyRaw:
		lines = append(lines, "body raw "+r.Body.Raw)
	case model.BodyURLEncoded:
		for _, kv := range r.Body.URLEncoded {
			lines = append(lines, "body urlencoded "+kv.Key+"="+kv.Value)
		}
	case model.BodyFormData:
		for _, p := range r.Body.FormData {
			lines = append(lines, "body formdata "+p.Key+"="+p.Value+p.Src)
		}
	case model.BodyGraphQL:
		if r.Body.GraphQL != nil {
			lines = append(lines, "body graphql variables "+r.Body.GraphQL.Variables)
		}
	default:
		lines = append(lines, "body "+string(r.Body.Mode))
	}
	if r.Auth != nil {
		lines = append(lines, "auth "+string(r.Auth.Type))
	}
	return lines
}

// corpusSender replays canned responses and records what was sent. Sends run on
// their own goroutines, so a case that fans out with Promise.all has no call
// order to rely on: such a case gives each response a "match" substring, and
// the recorded list is sorted before it reaches the golden file.
type corpusSender struct {
	mu        sync.Mutex
	responses []corpusResponse
	calls     int
	sent      []string
}

func (s *corpusSender) Send(_ context.Context, req *model.Request, _ httpx.Options) (*httpx.Response, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.sent = append(s.sent, fmt.Sprintf("%s %s", req.Method, req.URL.Raw))

	for _, r := range s.responses {
		if r.Match != "" && strings.Contains(req.URL.Raw, r.Match) {
			return r.httpx(), nil
		}
	}
	defer func() { s.calls++ }()
	if s.calls >= len(s.responses) {
		return jsonResponse(200, `{"ok":true}`), nil
	}
	return s.responses[s.calls].httpx(), nil
}

func (s *corpusSender) sentRequests() []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := append([]string(nil), s.sent...)
	sort.Strings(out)
	return out
}
