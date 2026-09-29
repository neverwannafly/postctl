package wsx

import (
	"context"
	"errors"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/shubham/postctl/internal/model"
)

func ptr[T any](v T) *T { return &v }

// fakeHydrator fills secret variables from a map keyed by SecretRef.
type fakeHydrator struct {
	values map[model.SecretRef]string
	calls  int
	err    error
}

func (h *fakeHydrator) HydrateVariables(_ context.Context, vs []model.Variable) error {
	h.calls++
	if h.err != nil {
		return h.err
	}
	for i := range vs {
		if vs[i].IsSecret() {
			vs[i].Value = h.values[vs[i].SecretRef]
		}
	}
	return nil
}

func (h *fakeHydrator) HydrateAuth(context.Context, *model.Auth) error { return nil }

type fixture struct {
	coll *model.Collection
	req  *model.Request
	env  *model.Environment
	hyd  *fakeHydrator
}

func newFixture(rawURL string) *fixture {
	req := model.NewWebSocketRequest("Feed", rawURL)
	folder := model.NewFolder("Live")
	folder.Variables = []model.Variable{{Key: "channel", Value: "prices"}}
	folder.Items = []model.Item{req}
	coll := model.NewCollection("API")
	coll.Variables = []model.Variable{
		{Key: "host", Value: "127.0.0.1"},
		{Key: "token", Type: model.VarSecret, SecretRef: "coll/token"},
	}
	coll.Items = []model.Item{folder}
	env := &model.Environment{ID: "e", Name: "dev", Variables: []model.Variable{{Key: "port", Value: "9"}}}
	return &fixture{coll: coll, req: req, env: env, hyd: &fakeHydrator{values: map[model.SecretRef]string{"coll/token": "s3cret-token"}}}
}

func (f *fixture) in() PrepareInput {
	return PrepareInput{Collection: f.coll, Request: f.req, Environment: f.env, Hydrator: f.hyd}
}

func TestPrepareResolvesURLHeadersAuthAndSettings(t *testing.T) {
	f := newFixture("ws://{{host}}:{{port}}/{{channel}}?v=1")
	f.req.Headers = []model.KV{
		{Key: "X-Channel", Value: "{{channel}}"},
		{Key: "X-Off", Value: "no", Disabled: true},
		{Key: "Host", Value: "virtual.test"},
	}
	f.req.Auth = &model.Auth{Type: model.AuthBearer, Bearer: &model.BearerAuth{Token: "{{token}}"}}
	f.req.Settings = model.RequestSettings{VerifyTLS: ptr(false), Timeout: ptr(3 * time.Second)}
	f.req.WebSocket = &model.WebSocketSettings{
		Subprotocols:    []string{"chat.{{channel}}", " "},
		PingInterval:    ptr(15 * time.Second),
		MaxMessageBytes: ptr(int64(4096)),
	}
	before := f.req.Clone()

	p, err := Prepare(context.Background(), f.in())
	if err != nil {
		t.Fatal(err)
	}
	if p.Target.URL != "ws://127.0.0.1:9/prices?v=1" {
		t.Fatalf("URL = %q", p.Target.URL)
	}
	h := p.Target.Header
	if h.Get("X-Channel") != "prices" || h.Get("X-Off") != "" || h.Get("Host") != "" {
		t.Fatalf("headers: %v", h)
	}
	if h.Get("Authorization") != "Bearer s3cret-token" {
		t.Fatalf("auth header: %q", h.Get("Authorization"))
	}
	if p.Target.Host != "virtual.test" {
		t.Fatalf("host override = %q", p.Target.Host)
	}
	if len(p.Target.Subprotocols) != 1 || p.Target.Subprotocols[0] != "chat.prices" {
		t.Fatalf("subprotocols: %v", p.Target.Subprotocols)
	}
	o := p.Options
	if !o.InsecureSkipVerify || o.HandshakeTimeout != 3*time.Second || o.PingInterval != 15*time.Second || o.MaxMessageBytes != 4096 {
		t.Fatalf("options: %+v", o)
	}
	if got := p.Target.Display(); got != "ws://127.0.0.1:9/prices" {
		t.Fatalf("Display = %q", got)
	}
	if len(p.Warnings) != 0 {
		t.Fatalf("warnings: %v", p.Warnings)
	}
	// The request the caller holds is untouched.
	if !equalRequests(before, f.req) {
		t.Fatal("Prepare modified the request")
	}
}

func equalRequests(a, b *model.Request) bool {
	return a.URL.Raw == b.URL.Raw && len(a.Headers) == len(b.Headers) && a.Auth.Bearer.Token == b.Auth.Bearer.Token &&
		a.WebSocket.Subprotocols[0] == b.WebSocket.Subprotocols[0]
}

func TestPrepareInheritsAuthFromTheFolderChain(t *testing.T) {
	f := newFixture("ws://x.test/a")
	f.coll.Items[0].(*model.Folder).Auth = &model.Auth{Type: model.AuthAPIKey, APIKey: &model.APIKeyAuth{Key: "api_key", Value: "{{token}}", In: model.APIKeyQuery}}
	f.req.Auth = &model.Auth{Type: model.AuthInherit}
	p, err := Prepare(context.Background(), f.in())
	if err != nil {
		t.Fatal(err)
	}
	if p.Target.URL != "ws://x.test/a?api_key=s3cret-token" {
		t.Fatalf("URL = %q", p.Target.URL)
	}
	if strings.Contains(p.Target.Display(), "s3cret") || strings.Contains(p.Target.Display(), "?") {
		t.Fatalf("Display leaks the query: %q", p.Target.Display())
	}
}

func TestPrepareHeaderAPIKeyAndBasic(t *testing.T) {
	f := newFixture("wss://x.test/a")
	f.req.Auth = &model.Auth{Type: model.AuthAPIKey, APIKey: &model.APIKeyAuth{Key: "X-Api", Value: "{{token}}", In: model.APIKeyHeader}}
	p, err := Prepare(context.Background(), f.in())
	if err != nil || p.Target.Header.Get("X-Api") != "s3cret-token" {
		t.Fatalf("api key header: %v %v", p, err)
	}
	f.req.Auth = &model.Auth{Type: model.AuthBasic, Basic: &model.BasicAuth{Username: "u", Password: "p"}}
	p, err = Prepare(context.Background(), f.in())
	if err != nil || !strings.HasPrefix(p.Target.Header.Get("Authorization"), "Basic ") {
		t.Fatalf("basic: %v %v", p, err)
	}
}

func TestPrepareErrors(t *testing.T) {
	tests := []struct {
		name  string
		setup func(f *fixture)
		want  error
	}{
		{"no url", func(f *fixture) { f.req.URL.Raw = "" }, ErrNoURL},
		{"blank url", func(f *fixture) { f.req.URL.Raw = "  " }, ErrNoURL},
		{"bad scheme", func(f *fixture) { f.req.URL.Raw = "example.com/x" }, ErrBadScheme},
		{"digest", func(f *fixture) {
			f.req.Auth = &model.Auth{Type: model.AuthDigest, Digest: &model.DigestAuth{Username: "u"}}
		}, ErrUnsupportedAuth},
		{"ntlm", func(f *fixture) { f.req.Auth = &model.Auth{Type: model.AuthNTLM, NTLM: &model.NTLMAuth{}} }, ErrUnsupportedAuth},
		{"hydrate", func(f *fixture) { f.hyd.err = errors.New("keychain locked") }, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture("ws://x.test/a")
			tt.setup(f)
			_, err := Prepare(context.Background(), f.in())
			if err == nil {
				t.Fatal("expected an error")
			}
			if tt.want != nil && !errors.Is(err, tt.want) {
				t.Fatalf("err = %v, want %v", err, tt.want)
			}
		})
	}
	f := newFixture("ws://x.test")
	f.req.Kind = model.KindHTTP
	if _, err := Prepare(context.Background(), f.in()); err == nil {
		t.Fatal("an HTTP request must be rejected")
	}
	if _, err := Prepare(context.Background(), PrepareInput{}); err == nil {
		t.Fatal("no request must be rejected")
	}
}

func TestPrepareWarnsAboutUnresolvedVariables(t *testing.T) {
	f := newFixture("ws://x.test/{{nope}}/{{$guid}}")
	p, err := Prepare(context.Background(), f.in())
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Warnings) != 1 || !strings.Contains(p.Warnings[0], "{{nope}}") || strings.Contains(p.Warnings[0], "guid") {
		t.Fatalf("warnings: %v", p.Warnings)
	}
}

func TestPrepareDefaultsAndCookieOptOut(t *testing.T) {
	f := newFixture("ws://x.test/a")
	jar, _ := cookiejar.New(nil)
	in := f.in()
	in.Jar = jar
	in.Defaults = model.RequestSettings{VerifyTLS: ptr(false), Timeout: ptr(7 * time.Second)}
	p, err := Prepare(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if !p.Options.InsecureSkipVerify || p.Options.HandshakeTimeout != 7*time.Second || p.Options.Jar == nil {
		t.Fatalf("options: %+v", p.Options)
	}
	f.req.Settings.DisableCookies = ptr(true)
	f.req.Settings.VerifyTLS = ptr(true)
	p, _ = Prepare(context.Background(), in)
	if p.Options.Jar != nil || p.Options.InsecureSkipVerify {
		t.Fatalf("request settings must win over defaults: %+v", p.Options)
	}
	p, _ = Prepare(context.Background(), f.in())
	if p.Options.HandshakeTimeout != DefaultHandshakeTimeout {
		t.Fatalf("timeout = %v", p.Options.HandshakeTimeout)
	}
}

// TestPrepareThenDialSendsResolvedHandshake is the end-to-end path: the server
// sees the resolved headers, auth and the jar's cookie, and hands a cookie back.
func TestPrepareThenDialSendsResolvedHandshake(t *testing.T) {
	var mu sync.Mutex
	var seen http.Header
	srv := serve(t, nil, func(ctx context.Context, c *websocket.Conn, r *http.Request) {
		mu.Lock()
		seen = r.Header.Clone()
		mu.Unlock()
		echo(ctx, c, r)
	})
	f := newFixture("ws://{{host}}:1/x")
	f.req.URL.Raw = strings.Replace(wsURL(srv), "127.0.0.1", "{{host}}", 1) + "/x"
	f.req.Headers = []model.KV{{Key: "X-Channel", Value: "{{channel}}"}}
	f.req.Auth = &model.Auth{Type: model.AuthBearer, Bearer: &model.BearerAuth{Token: "{{token}}"}}

	jar, _ := cookiejar.New(nil)
	u, _ := url.Parse(srv.URL)
	jar.SetCookies(u, []*http.Cookie{{Name: "sid", Value: "abc", Path: "/"}})
	in := f.in()
	in.Jar = jar

	ctx := testCtx(t)
	p, err := Prepare(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	c, err := p.Dial(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Abort()
	next(t, ctx, c) // open (the handshake has completed)
	mu.Lock()
	defer mu.Unlock()
	if seen.Get("Authorization") != "Bearer s3cret-token" || seen.Get("X-Channel") != "prices" {
		t.Fatalf("server saw: %v", seen)
	}
	if !strings.Contains(seen.Get("Cookie"), "sid=abc") {
		t.Fatalf("cookie from the jar not sent: %q", seen.Get("Cookie"))
	}
}

func TestHandshakeSetCookieReachesTheJar(t *testing.T) {
	// A server that sets a cookie on the 101 response.
	set := serveWithCookie(t)
	jar, _ := cookiejar.New(nil)
	ctx := testCtx(t)
	c := dial(t, ctx, wsURL(set), Options{Jar: jar})
	next(t, ctx, c)
	u, _ := url.Parse(set.URL)
	if got := jar.Cookies(u); len(got) != 1 || got[0].Name != "session" {
		t.Fatalf("jar: %v", got)
	}
}

func TestFrames(t *testing.T) {
	f := newFixture("ws://x.test/a")
	f.coll.Variables = append(f.coll.Variables, model.Variable{Key: "sym", Value: "EURUSD"})
	p, err := Prepare(context.Background(), f.in())
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name     string
		msg      model.WSMessage
		wantType MessageType
		wire     string
		display  string
		wantErr  error
	}{
		{"text default", model.WSMessage{Body: "hi {{sym}}"}, Text, "hi EURUSD", "hi EURUSD", nil},
		{"json", model.WSMessage{Format: model.WSJSON, Body: `{"s":"{{sym}}"}`}, Text, `{"s":"EURUSD"}`, `{"s":"EURUSD"}`, nil},
		{"json invalid", model.WSMessage{Format: model.WSJSON, Body: "{\n  \"a\": ,\n}"}, 0, "", "", ErrInvalidJSON},
		{"json empty", model.WSMessage{Format: model.WSJSON, Body: " "}, 0, "", "", ErrInvalidJSON},
		{"binary", model.WSMessage{Format: model.WSBinaryBase64, Body: "AQID\n"}, Binary, "\x01\x02\x03", "AQID", nil},
		{"binary unpadded", model.WSMessage{Format: model.WSBinaryBase64, Body: "AQI"}, Binary, "\x01\x02", "AQI", nil},
		{"binary invalid", model.WSMessage{Format: model.WSBinaryBase64, Body: "!!!"}, 0, "", "", ErrInvalidBase64},
		{"secret masked in display only", model.WSMessage{Body: "auth {{token}} ok"}, Text, "auth s3cret-token ok", "auth **** ok", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fr, err := p.Frame(tt.msg)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("err = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if fr.Type != tt.wantType || string(fr.Data) != tt.wire || string(fr.Display) != tt.display {
				t.Fatalf("frame = %+v (%q / %q)", fr, fr.Data, fr.Display)
			}
		})
	}
}

func TestCheckJSONReportsPosition(t *testing.T) {
	err := CheckJSON("{\n  \"a\": 1,\n  \"b\" 2\n}")
	if !errors.Is(err, ErrInvalidJSON) || !strings.Contains(err.Error(), "line 3") {
		t.Fatalf("err = %v", err)
	}
	if err := CheckJSON(`{"a": [1, 2, {"b": null}]}`); err != nil {
		t.Fatal(err)
	}
	if err := CheckJSON(`"just a string"`); err != nil {
		t.Fatalf("a JSON string is valid: %v", err)
	}
}

func TestMaskSecrets(t *testing.T) {
	got := maskSecrets("a longsecret-1 and longsecret b", []string{"longsecret", "longsecret-1"})
	if got != "a **** and **** b" {
		t.Fatalf("got %q", got)
	}
	if maskSecrets("abc", nil) != "abc" {
		t.Fatal("no secrets must leave the text alone")
	}
}

func TestNormalizeScheme(t *testing.T) {
	for in, want := range map[string]string{
		"ws://a.test/x":      "ws://a.test/x",
		"WSS://a.test":       "wss://a.test",
		"http://a.test/x":    "ws://a.test/x",
		"https://a.test?q=1": "wss://a.test?q=1",
	} {
		got, err := normalizeScheme(in)
		if err != nil || got != want {
			t.Errorf("normalizeScheme(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	if _, err := normalizeScheme("ws://"); err == nil {
		t.Error("a URL without a host must fail")
	}
}
