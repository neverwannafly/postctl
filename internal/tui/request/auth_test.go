package request

import (
	"context"
	"errors"
	"net/url"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shubham/postctl/internal/model"
	"github.com/shubham/postctl/internal/oauth2"
	"github.com/shubham/postctl/internal/store"
	"github.com/shubham/postctl/internal/tui"
)

func TestAuthInheritShowsEffectiveAuthAndSource(t *testing.T) {
	h := newHarness(t)
	h.gotoTab(tabAuth)
	h.mustHave("Inherit from parent")
	h.mustHave("Bearer Token")
	h.mustHave(`collection "Payments"`)
	// A folder override wins and is named.
	h.folder.Auth = &model.Auth{Type: model.AuthBasic, Basic: &model.BasicAuth{}}
	h.send(tui.SelectionMsg{Ref: h.ref()})
	h.mustHave("Basic Auth")
	h.mustHave(`folder "Charges"`)
}

func TestAuthInheritWithNothingAbove(t *testing.T) {
	h := newHarness(t)
	h.coll.Auth = nil
	h.send(tui.SelectionMsg{Ref: h.ref()})
	h.gotoTab(tabAuth)
	h.mustHave("Nothing above sets authentication")
}

func TestAuthSwitchTypeAndFillBearer(t *testing.T) {
	h := newHarness(t)
	h.gotoTab(tabAuth)
	h.key("down")           // Type row
	h.key("right", "right") // Inherit -> None -> Bearer
	if h.req.Auth == nil || h.req.Auth.Type != model.AuthBearer || h.req.Auth.Bearer == nil {
		t.Fatalf("auth = %+v", h.req.Auth)
	}
	h.key("down", "i")
	h.typ("abc123")
	h.key("esc", "esc")
	if h.req.Auth.Bearer.Token != "abc123" {
		t.Fatalf("token = %q", h.req.Auth.Bearer.Token)
	}
	// Literal secrets are masked until revealed.
	h.mustNotHave("abc123")
	h.action("request.reveal")
	h.mustHave("abc123")
	// Leaving and returning hides them again.
	h.gotoTab(tabHeaders)
	h.gotoTab(tabAuth)
	h.key("down")
	h.mustNotHave("abc123")
}

func TestAuthVariableReferencesAreShown(t *testing.T) {
	h := newHarness(t)
	h.req.Auth = &model.Auth{Type: model.AuthBearer, Bearer: &model.BearerAuth{Token: "{{token}}"}}
	h.gotoTab(tabAuth)
	h.mustHave("{{token}}")
}

func TestAuthSecretMaskedWhileTypingAndRevealWithAction(t *testing.T) {
	h := newHarness(t)
	h.req.Auth = &model.Auth{Type: model.AuthBasic, Basic: &model.BasicAuth{Username: "bob"}}
	h.gotoTab(tabAuth)
	h.key("down", "down", "down", "i") // Password
	h.typ("hunter2")
	h.mustNotHave("hunter2")
	h.key("esc", "esc")
	if h.req.Auth.Basic.Password != "hunter2" {
		t.Fatal("password not stored")
	}
	h.mustNotHave("hunter2")
	h.action("request.reveal")
	h.mustHave("hunter2")
}

func TestAuthTypeSwitchRestoresEnteredValues(t *testing.T) {
	h := newHarness(t)
	h.req.Auth = &model.Auth{Type: model.AuthBearer, Bearer: &model.BearerAuth{Token: "{{t}}"}}
	h.gotoTab(tabAuth)
	h.key("down", "right") // Bearer -> Basic
	if h.req.Auth.Type != model.AuthBasic {
		t.Fatalf("type = %v", h.req.Auth.Type)
	}
	h.key("left")
	if h.req.Auth.Type != model.AuthBearer || h.req.Auth.Bearer.Token != "{{t}}" {
		t.Fatalf("auth = %+v", h.req.Auth)
	}
}

func TestAuthAPIKeyPlacement(t *testing.T) {
	h := newHarness(t)
	h.req.Auth = newAuth(model.AuthAPIKey)
	h.gotoTab(tabAuth)
	h.key("down", "down", "down", "down", "space")
	if h.req.Auth.APIKey.In != model.APIKeyQuery {
		t.Fatalf("in = %v", h.req.Auth.APIKey.In)
	}
}

func TestAuthUnsupportedIsExplained(t *testing.T) {
	h := newHarness(t)
	h.req.Auth = &model.Auth{Type: model.AuthUnsupported, OriginalType: "oauth1"}
	h.gotoTab(tabAuth)
	h.mustHave("Unsupported (oauth1)")
	h.mustHave("cannot run this scheme")
}

// ---- OAuth 2.0 ---------------------------------------------------------------------

type fakeBrowser struct{ opened atomic.Int32 }

func (b *fakeBrowser) OpenURL(string) error { b.opened.Add(1); return nil }

func oauthHarness(t *testing.T, fetch oauth2.FetchFunc) (*harness, *store.MemorySecrets, *fakeBrowser) {
	secrets := store.NewMemorySecrets()
	br := &fakeBrowser{}
	h := newHarness(t, func(h *harness) {
		h.deps.Fetch = fetch
		h.deps.Browser = br
		h.deps.Tokens = oauth2.NewSource(secrets, nil)
	})
	h.req.Auth = &model.Auth{Type: model.AuthOAuth2, OAuth2: &model.OAuth2Config{
		GrantType: model.GrantAuthorizationCodePKCE, AuthURL: "{{baseUrl}}/authorize", AccessTokenURL: "{{baseUrl}}/token",
		ClientID: "cid", ClientSecret: "{{token}}",
	}}
	h.gotoTab(tabAuth)
	return h, secrets, br
}

func TestOAuthGetNewTokenSavesAndShowsExpiry(t *testing.T) {
	var got *model.OAuth2Config
	fetch := func(_ context.Context, cfg *model.OAuth2Config, opts oauth2.FetchOptions) (*model.OAuth2Token, error) {
		got = cfg
		opts.OnAuthURL("https://idp.example.com/authorize?client_id=cid")
		_ = opts.Browser.OpenURL("https://idp.example.com/authorize?client_id=cid")
		return &model.OAuth2Token{AccessToken: "tok-abc", TokenType: "Bearer", ExpiresAt: time.Date(2026, 1, 2, 4, 4, 5, 0, time.UTC)}, nil
	}
	h, secrets, br := oauthHarness(t, fetch)
	h.key("down", "down", "down", "enter") // Grant, Token, Get new token
	if got == nil {
		t.Fatal("Fetch not called")
	}
	// Variables were resolved (with the secret revealed) before fetching.
	if got.AuthURL != "https://staging.example.com/authorize" || got.ClientSecret != "s3cr3t-value" {
		t.Fatalf("cfg not resolved: %q", got.AuthURL)
	}
	if br.opened.Load() != 1 {
		t.Fatal("browser not opened")
	}
	tok := h.req.Auth.OAuth2.Token
	if tok == nil || tok.Ref == "" {
		t.Fatalf("token not attached: %+v", tok)
	}
	if v, err := secrets.Get(context.Background(), tok.Ref); err != nil || !strings.Contains(v, "tok-abc") {
		t.Fatalf("secret store: %q %v", v, err)
	}
	h.mustHave("Valid, expires in 1h")
	if h.p.oauth != nil {
		t.Fatal("still waiting")
	}
	// The edit reaches autosave.
	h.advance(time.Second)
	if len(h.saves()) != 1 {
		t.Fatalf("saves = %d", len(h.saves()))
	}
}

func TestOAuthWaitingStateShowsLinkAndCancels(t *testing.T) {
	block := make(chan struct{})
	fetch := func(ctx context.Context, _ *model.OAuth2Config, opts oauth2.FetchOptions) (*model.OAuth2Token, error) {
		opts.OnAuthURL("https://idp.example.com/authorize?state=x")
		opts.OnNote("Using http://127.0.0.1:8123/callback as the redirect URI")
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-block:
			return nil, errors.New("unreachable")
		}
	}
	h, _, _ := oauthHarness(t, fetch)
	// Start the flow without running its blocking command; feed events by hand.
	h.key("down")
	_ = h.actionCmd("request.getToken")
	run := h.p.oauth
	if run == nil {
		t.Fatal("flow not started")
	}
	// The listener command would block here, as it does in the runtime.
	h.p.Update(oauthEventsMsg{run: run, ev: oauthURLEv{url: "https://idp.example.com/authorize?state=x"}})
	h.p.Update(oauthEventsMsg{run: run, ev: oauthNoteEv{note: "Using http://127.0.0.1:8123/callback as the redirect URI"}})
	h.mustHave("Waiting for you to sign in")
	h.mustHave("https://idp.example.com/authorize?state=x")
	h.mustHave("127.0.0.1:8123")
	h.mustHave("Esc")
	hints := ""
	for _, k := range h.p.KeyHints() {
		hints += k.Key + " " + k.Label + ";"
	}
	if !strings.Contains(hints, "Esc Cancel") || !strings.Contains(hints, "c Copy link") {
		t.Fatalf("hints = %s", hints)
	}
	h.key("c")
	if len(h.clip) != 1 || h.clip[0] != "https://idp.example.com/authorize?state=x" {
		t.Fatalf("clipboard = %v", h.clip)
	}
	h.key("esc")
	if h.p.oauth != nil {
		t.Fatal("esc did not cancel")
	}
	h.mustHave("Sign-in cancelled")
	// A late event from the cancelled attempt is ignored.
	h.p.Update(oauthEventsMsg{run: run, ev: oauthDoneEv{tok: &model.OAuth2Token{AccessToken: "late"}}})
	if h.req.Auth.OAuth2.Token != nil {
		t.Fatal("late token attached")
	}
	close(block)
}

func TestOAuthErrorStatesNextAction(t *testing.T) {
	got := oauthMessage(errors.New("oauth2: provider rejected the request"))
	if !strings.Contains(got, "Try") && !strings.Contains(got, "Check") {
		t.Fatalf("OAuth error has no next action: %q", got)
	}
}

func TestOAuthFailureIsShownCalmly(t *testing.T) {
	fetch := func(context.Context, *model.OAuth2Config, oauth2.FetchOptions) (*model.OAuth2Token, error) {
		return nil, oauth2.ErrTimeout
	}
	h, _, _ := oauthHarness(t, fetch)
	h.key("down")
	h.action("request.getToken")
	h.mustHave("Timed out waiting for the browser")
	if h.req.Auth.OAuth2.Token != nil {
		t.Fatal("token set on failure")
	}
}

func TestOAuthWithoutSecretStoreExplains(t *testing.T) {
	fetch := func(context.Context, *model.OAuth2Config, oauth2.FetchOptions) (*model.OAuth2Token, error) {
		return &model.OAuth2Token{AccessToken: "x"}, nil
	}
	h := newHarness(t, func(h *harness) { h.deps.Fetch = fetch })
	h.req.Auth = &model.Auth{Type: model.AuthOAuth2, OAuth2: &model.OAuth2Config{GrantType: model.GrantClientCredentials}}
	h.gotoTab(tabAuth)
	h.key("down")
	h.action("request.getToken")
	h.mustHave("no secret store")
}

func TestOAuthTokenStatusStates(t *testing.T) {
	h := newHarness(t)
	h.req.Auth = &model.Auth{Type: model.AuthOAuth2, OAuth2: &model.OAuth2Config{GrantType: model.GrantClientCredentials}}
	h.gotoTab(tabAuth)
	h.mustHave("No token yet")
	h.req.Auth.OAuth2.Token = &model.OAuth2Token{Ref: "oauth2/x", ExpiresAt: h.now.Add(-3 * time.Minute)}
	h.mustHave("Expired 3m ago")
	h.req.Auth.OAuth2.Token = &model.OAuth2Token{Ref: "oauth2/x"}
	h.mustHave("no expiry")
}

func TestOAuthGrantChangesFields(t *testing.T) {
	h := newHarness(t)
	h.req.Auth = newAuth(model.AuthOAuth2)
	h.gotoTab(tabAuth)
	h.mustHave("Auth URL")
	h.req.Auth.OAuth2.GrantType = model.GrantClientCredentials
	h.mustNotHave("Auth URL")
	h.req.Auth.OAuth2.GrantType = model.GrantPassword
	h.mustHave("Username")
}

// ---- copy as curl ---------------------------------------------------------------------

type hydrator struct{ tui.Backend }

func (hydrator) HydrateVariables(_ context.Context, vs []model.Variable) error {
	for i := range vs {
		if vs[i].IsSecret() && vs[i].Value == "" {
			vs[i].Value = "s3cr3t-value"
		}
	}
	return nil
}
func (hydrator) HydrateAuth(context.Context, *model.Auth) error { return nil }

func curlHarness(t *testing.T) *harness {
	h := newHarness(t, func(h *harness) { h.be = hydrator{} })
	h.req.URL.PathVars[0].Value = "42"
	// As in the real app, secret values are not in memory until hydrated.
	h.env.Variables[1].Value = ""
	h.p.rebuildScope()
	return h
}

func TestCopyCurlMasksSecretsByDefault(t *testing.T) {
	h := curlHarness(t)
	h.action("request.curl")
	if len(h.clip) != 1 {
		t.Fatalf("clipboard = %v", h.clip)
	}
	c := h.clip[0]
	if strings.Contains(c, "s3cr3t-value") {
		t.Fatalf("secret in default copy: %s", c)
	}
	for _, want := range []string{"--request 'POST'", "https://staging.example.com/v1/charges/42?limit=10&debug=1", "Accept: application/json", `{"amount": 1200}`, "Authorization: Bearer ****"} {
		if !strings.Contains(c, want) {
			t.Errorf("curl lacks %q:\n%s", want, c)
		}
	}
	if strings.Contains(c, "X-Trace") {
		t.Error("disabled header included")
	}
	h.mustHave("secrets masked")
}

func TestCopyCurlWithSecrets(t *testing.T) {
	h := curlHarness(t)
	h.action("request.curl.reveal")
	if len(h.clip) != 1 || !strings.Contains(h.clip[0], "Authorization: Bearer s3cr3t-value") {
		t.Fatalf("clipboard = %v", h.clip)
	}
	h.mustHave("with secrets")
}

func TestCopyCurlMasksLiteralAuthCredentials(t *testing.T) {
	h := curlHarness(t)
	h.req.Auth = &model.Auth{Type: model.AuthBasic, Basic: &model.BasicAuth{Username: "bob", Password: "hunter2"}}
	h.action("request.curl")
	c := h.clip[0]
	if strings.Contains(c, "hunter2") || !strings.Contains(c, "--user 'bob:****'") {
		t.Fatalf("curl = %s", c)
	}
	h.action("request.curl.reveal")
	if !strings.Contains(h.clip[1], "--user 'bob:hunter2'") {
		t.Fatalf("curl = %s", h.clip[1])
	}
}

func TestCopyCurlAPIKeyInQuery(t *testing.T) {
	h := curlHarness(t)
	h.req.Auth = &model.Auth{Type: model.AuthAPIKey, APIKey: &model.APIKeyAuth{Key: "k", Value: "v1", In: model.APIKeyQuery}}
	h.action("request.curl.reveal")
	m := regexp.MustCompile(`--url '([^']*)'`).FindStringSubmatch(h.clip[0])
	if m == nil {
		t.Fatalf("curl = %s", h.clip[0])
	}
	u, _ := url.Parse(m[1])
	if u.Query().Get("k") != "v1" {
		t.Fatalf("curl = %s", h.clip[0])
	}
}

// ---- curl paste ---------------------------------------------------------------------------

const pastedCurl = `curl -X PUT 'https://api.example.com/v1/things?id=9' -H 'Content-Type: application/json' -H 'X-Token: abc' --data-raw '{"a":1}'`

func TestPasteCurlOffersImport(t *testing.T) {
	h := newHarness(t)
	h.key("up") // URL bar
	h.paste(pastedCurl)
	h.mustHave("Import as request")
	h.mustHave("things?id=9")
	h.mustHave("2 headers")
	if h.req.Method != "POST" {
		t.Fatal("import applied before confirmation")
	}
	h.key("enter")
	if h.req.Method != "PUT" || h.req.URL.Raw != "https://api.example.com/v1/things?id=9" || len(h.req.Headers) != 2 {
		t.Fatalf("request = %+v", h.req)
	}
	if h.req.Body.Mode != model.BodyRaw || h.req.Body.Raw != `{"a":1}` {
		t.Fatalf("body = %+v", h.req.Body)
	}
	if h.req.Name != "Create charge" {
		t.Fatal("import must keep the request name")
	}
	if h.p.url.Text() != h.req.URL.Raw {
		t.Fatal("URL field not refreshed")
	}
	h.key("ctrl+z")
	if h.req.Method != "POST" || h.req.Body.Raw != `{"amount": {{amount}}}` {
		t.Fatalf("undo failed: %+v", h.req)
	}
}

func TestPasteCurlCancelAndPasteAsText(t *testing.T) {
	h := newHarness(t)
	h.key("up")
	h.paste(pastedCurl)
	h.key("esc")
	if h.p.confirm != nil || h.req.Method != "POST" || strings.Contains(h.req.URL.Raw, "example.com/v1/things") {
		t.Fatal("esc must cancel without changes")
	}
	h.paste(pastedCurl)
	h.key("left", "enter") // "Paste as text"
	if !strings.Contains(h.req.URL.Raw, "curl -X PUT") {
		t.Fatalf("raw = %q", h.req.URL.Raw)
	}
}

func TestPastePlainTextGoesToURL(t *testing.T) {
	h := newHarness(t)
	h.key("up", "A")
	h.paste("/extra")
	if !strings.HasSuffix(h.req.URL.Raw, "/extra") || h.p.confirm != nil {
		t.Fatalf("raw = %q", h.req.URL.Raw)
	}
}

func TestPasteCurlIntoBodyEditorIsPlainText(t *testing.T) {
	h := newHarness(t)
	h.gotoTab(tabBody)
	h.key("down", "down", "down")
	h.paste(pastedCurl)
	if h.p.confirm != nil {
		t.Fatal("curl pasted into a text editor must not trigger import")
	}
	if !strings.Contains(h.req.Body.Raw, "curl -X PUT") {
		t.Fatal("paste lost")
	}
}

// Enter on the Get new token row starts the flow (the real key path; the
// action id is for the palette), and the error says how to try again without
// naming a key that is gone.
func TestOAuthGetTokenWithEnterOnTheRow(t *testing.T) {
	fetch := func(context.Context, *model.OAuth2Config, oauth2.FetchOptions) (*model.OAuth2Token, error) {
		return nil, oauth2.ErrTimeout
	}
	h, _, _ := oauthHarness(t, fetch)
	h.key("down", "down", "down", "enter") // Type, Grant type, Get new token (the status row is skipped)
	h.mustHave("Timed out waiting for the browser")
	for _, err := range []error{oauth2.ErrTimeout, errors.New("x")} {
		msg := oauthMessage(err)
		if !strings.Contains(msg, "select Get new token and press Enter") && !strings.Contains(msg, "Select Get new token and press Enter") {
			t.Errorf("OAuth error does not say how to retry: %q", msg)
		}
		if strings.Contains(msg, "press g") || strings.Contains(msg, "Press g") {
			t.Errorf("OAuth error names a key that no longer exists: %q", msg)
		}
	}
}
