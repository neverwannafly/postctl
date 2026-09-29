package oauth2_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/shubham/postctl/internal/model"
	"github.com/shubham/postctl/internal/oauth2"
)

const leakMarker = "super-secret-token-value"

func TestClientCredentials(t *testing.T) {
	now := time.Date(2026, 9, 29, 18, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name   string
		auth   model.OAuth2ClientAuth
		secret string
		body   bool
	}{
		{name: "basic header", auth: model.ClientAuthHeader, secret: "csecret"},
		{name: "post body", auth: model.ClientAuthBody, secret: "s e/c+ret", body: true},
		{name: "public client", auth: model.ClientAuthHeader, secret: ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			as := newFake(t)
			cfg := &model.OAuth2Config{
				GrantType:      model.GrantClientCredentials,
				AccessTokenURL: as.url("/token"),
				ClientID:       "cid",
				ClientSecret:   tc.secret,
				ClientAuth:     tc.auth,
				Scope:          "read write",
				Audience:       "api",
				Resource:       "https://api.example/res",
				Extra: map[string]any{
					"grant_extra": "x",
					"n":           2,
					"tokenRequestParams": []any{
						map[string]any{"key": "audience_extra", "value": "a"},
						map[string]any{"key": "skip", "value": "no", "disabled": true},
					},
					"client_secret": "from-extra",
				},
				Token: &model.OAuth2Token{Ref: "oauth2/req-cc"},
			}
			tok, err := oauth2.Fetch(context.Background(), cfg, oauth2.FetchOptions{
				HTTPClient: as.client(),
				Now:        func() time.Time { return now },
				Browser: oauth2.BrowserFunc(func(string) error {
					t.Fatal("browser opened for client credentials")
					return nil
				}),
			})
			if err != nil {
				t.Fatal(errString(err))
			}
			if tok.AccessToken != "atk" || tok.RefreshToken != "rtk" || tok.IDToken != "idt" {
				t.Fatal("token fields were not parsed")
			}
			if tok.TokenType != "Bearer" || tok.Scope != "read write" || tok.Ref != "oauth2/req-cc" {
				t.Fatal("token metadata was not kept")
			}
			if !tok.ExpiresAt.Equal(now.Add(time.Hour)) {
				t.Fatal("expires_in was not applied")
			}
			if cfg.Token.AccessToken != "" {
				t.Fatal("Fetch modified the caller's token")
			}
			form, hdr := as.tokenRequest()
			if form.Get("grant_type") != "client_credentials" {
				t.Fatal("grant_type was not client_credentials")
			}
			if form.Get("scope") != "read write" || form.Get("audience") != "api" || form.Get("resource") != "https://api.example/res" {
				t.Fatal("scope, audience, or resource missing")
			}
			if form.Get("grant_extra") != "x" || form.Get("n") != "2" || form.Get("audience_extra") != "a" || form.Get("skip") != "" {
				t.Fatal("extra token params were not applied")
			}
			if form.Get("client_secret") == "from-extra" {
				t.Fatal("extra client_secret was copied onto the request")
			}
			if form.Get("client_id") != "cid" {
				t.Fatal("client_id missing from the body")
			}
			if tc.body {
				if hdr != "" {
					t.Fatal("body client auth set an Authorization header")
				}
				if form.Get("client_secret") != tc.secret {
					t.Fatal("client_secret missing from the body")
				}
			} else if tc.secret == "" {
				if hdr != "" || form.Get("client_secret") != "" {
					t.Fatal("public client sent a client secret")
				}
			} else {
				user, pass := decodeBasic(t, hdr)
				if user != "cid" || pass != tc.secret {
					t.Fatal("basic client auth did not match")
				}
				if form.Get("client_secret") != "" {
					t.Fatal("header client auth also put client_secret in the body")
				}
			}
		})
	}
}

func TestFormEncodedTokenAndProviderErrors(t *testing.T) {
	as := newFake(t)
	as.setToken(http.StatusOK, "application/x-www-form-urlencoded", "access_token=atk&token_type=bearer&expires_in=10&refresh_token=rtk&id_token=idt&scope=s")
	now := time.Date(2026, 9, 29, 18, 0, 0, 0, time.UTC)
	cfg := baseCC(as)
	tok, err := oauth2.Fetch(context.Background(), cfg, oauth2.FetchOptions{HTTPClient: as.client(), Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(errString(err))
	}
	if tok.AccessToken != "atk" || tok.TokenType != "bearer" || tok.Scope != "s" || !tok.ExpiresAt.Equal(now.Add(10*time.Second)) {
		t.Fatal("form-encoded token was not parsed")
	}

	as.setToken(http.StatusBadRequest, "application/json", `{"error":"invalid_client","error_description":"unknown client","access_token":"`+leakMarker+`"}`)
	_, err = oauth2.Fetch(context.Background(), cfg, oauth2.FetchOptions{HTTPClient: as.client()})
	var oe *oauth2.Error
	if !errors.As(err, &oe) || oe.Code != "invalid_client" || oe.Description != "unknown client" {
		t.Fatal("provider error was not mapped")
	}
	if strings.Contains(err.Error(), leakMarker) || strings.Contains(err.Error(), "atk") {
		t.Fatal("error string contains a token")
	}

	as.setToken(http.StatusBadRequest, "application/x-www-form-urlencoded", "error=invalid_grant&error_description=expired&access_token="+leakMarker)
	_, err = oauth2.Fetch(context.Background(), cfg, oauth2.FetchOptions{HTTPClient: as.client()})
	if !errors.As(err, &oe) || oe.Code != "invalid_grant" || strings.Contains(err.Error(), leakMarker) {
		t.Fatal("form error leaked or was not mapped")
	}

	as.setToken(http.StatusBadGateway, "text/html", "<html>"+leakMarker+"</html>")
	_, err = oauth2.Fetch(context.Background(), cfg, oauth2.FetchOptions{HTTPClient: as.client()})
	if err == nil || strings.Contains(err.Error(), leakMarker) {
		t.Fatal("html error body leaked or was accepted")
	}
}

func TestPasswordGrant(t *testing.T) {
	as := newFake(t)
	cfg := &model.OAuth2Config{
		GrantType:      model.GrantPassword,
		AccessTokenURL: as.url("/token"),
		ClientID:       "cid",
		ClientSecret:   "csecret",
		ClientAuth:     model.ClientAuthBody,
		Username:       "ada",
		Password:       "pw",
		Scope:          "read",
	}
	tok, err := oauth2.Fetch(context.Background(), cfg, oauth2.FetchOptions{
		HTTPClient: as.client(),
		Browser: oauth2.BrowserFunc(func(string) error {
			t.Fatal("browser opened for password grant")
			return nil
		}),
	})
	if err != nil {
		t.Fatal(errString(err))
	}
	if tok.AccessToken != "atk" {
		t.Fatal("password grant did not return a token")
	}
	form, _ := as.tokenRequest()
	if form.Get("grant_type") != "password" || form.Get("username") != "ada" || form.Get("password") != "pw" {
		t.Fatal("password grant body was wrong")
	}
	if _, err := oauth2.Fetch(context.Background(), &model.OAuth2Config{GrantType: model.GrantPassword, AccessTokenURL: as.url("/token")}, oauth2.FetchOptions{HTTPClient: as.client()}); err == nil {
		t.Fatal("missing username was accepted")
	}
}

func TestRefresh(t *testing.T) {
	as := newFake(t)
	cfg := &model.OAuth2Config{
		GrantType:       model.GrantClientCredentials,
		AccessTokenURL:  as.url("/token"),
		RefreshTokenURL: as.url("/refresh"),
		ClientID:        "cid",
		ClientSecret:    "csecret",
		Scope:           "read",
		Token:           &model.OAuth2Token{RefreshToken: "old-refresh", Ref: "oauth2/req"},
		Extra: map[string]any{
			"refreshRequestParams": []any{map[string]any{"key": "only_refresh", "value": "1"}},
			"tokenRequestParams":   []any{map[string]any{"key": "only_token", "value": "1"}},
		},
	}
	as.setToken(http.StatusOK, "application/json", `{"access_token":"atk2","token_type":"Bearer","expires_in":5}`)
	tok, err := oauth2.Fetch(context.Background(), cfg, oauth2.FetchOptions{HTTPClient: as.client(), Refresh: true})
	if err != nil {
		t.Fatal(errString(err))
	}
	if tok.AccessToken != "atk2" || tok.RefreshToken != "old-refresh" || tok.Ref != "oauth2/req" {
		t.Fatal("refresh did not keep the previous refresh token and ref")
	}
	form, _ := as.tokenRequest()
	if form.Get("grant_type") != "refresh_token" || form.Get("refresh_token") != "old-refresh" {
		t.Fatal("refresh request body was wrong")
	}
	if form.Get("only_refresh") != "1" || form.Get("only_token") != "" {
		t.Fatal("refresh extra params were not selected")
	}
	if as.tokenHitsPath() != "/refresh" {
		t.Fatal("refresh did not use the refresh URL")
	}
	cfg.Token = &model.OAuth2Token{AccessToken: "atk"}
	if _, err := oauth2.Fetch(context.Background(), cfg, oauth2.FetchOptions{HTTPClient: as.client(), Refresh: true}); err == nil || strings.Contains(err.Error(), "atk") {
		t.Fatal("missing refresh token was accepted or leaked")
	}
}

func TestAuthorizationCodeAndPKCE(t *testing.T) {
	states := map[string]bool{}
	for _, tc := range []struct {
		name   string
		grant  model.OAuth2Grant
		method string
		fixed  string
	}{
		{name: "no pkce", grant: model.GrantAuthorizationCode},
		{name: "s256", grant: model.GrantAuthorizationCodePKCE},
		{name: "plain", grant: model.GrantAuthorizationCodePKCE, method: "plain", fixed: "verifier-plain-fixed-value-0123456789abcd"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			as := newFake(t)
			ln, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			port := ln.Addr().(*net.TCPAddr).Port
			if err := ln.Close(); err != nil {
				t.Fatal(err)
			}
			redirect := "http://127.0.0.1:" + strconv.Itoa(port) + "/hooks/cb"
			if tc.name == "plain" {
				redirect = "http://localhost:" + strconv.Itoa(port) + "/hooks/cb"
			}
			cfg := &model.OAuth2Config{
				GrantType:           tc.grant,
				AuthURL:             as.url("/authorize"),
				AccessTokenURL:      as.url("/token"),
				ClientID:            "cid",
				ClientSecret:        "csecret",
				ClientAuth:          model.ClientAuthBody,
				Scope:               "read",
				Audience:            "api",
				RedirectURI:         redirect,
				State:               "fixed-state",
				CodeChallengeMethod: tc.method,
				CodeVerifier:        tc.fixed,
				Extra: map[string]any{
					"grant_extra":       "x",
					"client_secret":     "from-extra",
					"authRequestParams": []any{map[string]any{"key": "prompt", "value": "consent"}, map[string]any{"key": "nope", "value": "1", "enabled": false}},
				},
			}
			var page, authRaw, shown string
			tok, err := oauth2.Fetch(context.Background(), cfg, oauth2.FetchOptions{
				HTTPClient: as.client(),
				OnAuthURL: func(u string) {
					shown = u
				},
				Browser: oauth2.BrowserFunc(func(raw string) error {
					authRaw = raw
					if shown != raw {
						t.Fatal("auth URL callback was not invoked before the browser")
					}
					if strings.Contains(raw, "csecret") || strings.Contains(raw, "from-extra") {
						t.Fatal("authorization URL contains a secret")
					}
					u, err := url.Parse(raw)
					if err != nil {
						return err
					}
					bad, err := url.Parse(u.Query().Get("redirect_uri"))
					if err != nil {
						return err
					}
					bad.Path = "/not-the-callback"
					resp, err := http.Get(bad.String())
					if err != nil {
						return err
					}
					_, _ = io.Copy(io.Discard, resp.Body)
					_ = resp.Body.Close()
					if resp.StatusCode != http.StatusNotFound {
						return errors.New("wrong callback path was accepted")
					}
					page, err = followAuth(raw)
					return err
				}),
			})
			if err != nil {
				t.Fatal(errString(err))
			}
			if tok.AccessToken != "atk" {
				t.Fatal("auth code grant did not return a token")
			}
			if strings.Contains(page, "authcode") || strings.Contains(page, "atk") {
				t.Fatal("callback page contains a code or token")
			}
			if !strings.Contains(page, "close this window") {
				t.Fatal("callback page is missing its confirmation")
			}
			auth := as.authQuery()
			if auth.Get("response_type") != "code" || auth.Get("state") == "" ||
				auth.Get("state") == "fixed-state" || auth.Get("client_id") != "cid" {
				t.Fatal("authorize query is missing required fields")
			}
			if states[auth.Get("state")] {
				t.Fatal("OAuth state was reused across flows")
			}
			states[auth.Get("state")] = true
			if auth.Get("redirect_uri") != redirect || auth.Get("prompt") != "consent" || auth.Get("nope") != "" || auth.Get("grant_extra") != "x" {
				t.Fatal("redirect URI or auth extra params were wrong")
			}
			if auth.Get("client_secret") != "" {
				t.Fatal("client secret was sent on the authorize URL")
			}
			form, _ := as.tokenRequest()
			if form.Get("grant_type") != "authorization_code" || form.Get("code") != "authcode" || form.Get("redirect_uri") != redirect {
				t.Fatal("token exchange body was wrong")
			}
			if form.Get("scope") != "" {
				t.Fatal("scope was repeated on the authorization code exchange")
			}
			if form.Get("audience") != "api" || form.Get("grant_extra") != "x" || form.Get("prompt") != "" {
				t.Fatal("token extra params were wrong")
			}
			switch tc.grant {
			case model.GrantAuthorizationCode:
				if auth.Get("code_challenge") != "" || form.Get("code_verifier") != "" {
					t.Fatal("PKCE parameters were sent without PKCE")
				}
			case model.GrantAuthorizationCodePKCE:
				wantMethod := tc.method
				if wantMethod == "" {
					wantMethod = "S256"
				}
				if !pkceMatches(form.Get("code_verifier"), auth.Get("code_challenge"), auth.Get("code_challenge_method")) {
					t.Fatal("PKCE challenge does not match the verifier")
				}
				if auth.Get("code_challenge_method") != wantMethod {
					t.Fatal("PKCE method was not sent")
				}
				if tc.fixed != "" && form.Get("code_verifier") != tc.fixed {
					t.Fatal("configured verifier was not used")
				}
				if wantMethod == "S256" && strings.Contains(authRaw, form.Get("code_verifier")) {
					t.Fatal("S256 verifier was placed in the authorization URL")
				}
			}
			if cfg.CodeVerifier != tc.fixed || cfg.State != "fixed-state" {
				t.Fatal("Fetch modified the caller's config")
			}
			dialClosed(t, hostport(redirect))
		})
	}
}

func TestStateMismatchAndProviderRedirect(t *testing.T) {
	as := newFake(t)
	as.setBehavior(true, "")
	cfg := browserCfg(as)
	_, err := oauth2.Fetch(context.Background(), cfg, oauth2.FetchOptions{HTTPClient: as.client(), Browser: followBrowser()})
	var oe *oauth2.Error
	if !errors.As(err, &oe) || !strings.Contains(err.Error(), "state") || strings.Contains(err.Error(), "authcode") {
		t.Fatal("state mismatch was not reported cleanly")
	}

	as = newFake(t)
	as.setBehavior(false, "access_denied")
	_, err = oauth2.Fetch(context.Background(), browserCfg(as), oauth2.FetchOptions{HTTPClient: as.client(), Browser: followBrowser()})
	if !errors.As(err, &oe) || oe.Code != "access_denied" || strings.Contains(err.Error(), leakMarker) {
		t.Fatal("provider redirect error was not mapped")
	}
}

func TestImplicit(t *testing.T) {
	as := newFake(t)
	now := time.Date(2026, 9, 29, 18, 0, 0, 0, time.UTC)
	cfg := &model.OAuth2Config{
		GrantType: model.GrantImplicit,
		AuthURL:   as.url("/authorize"),
		ClientID:  "cid",
		Scope:     "read",
		State:     "fixed-state",
		Token:     &model.OAuth2Token{Ref: "oauth2/imp"},
	}
	var page string
	tok, err := oauth2.Fetch(context.Background(), cfg, oauth2.FetchOptions{
		Now: func() time.Time { return now },
		Browser: oauth2.BrowserFunc(func(raw string) error {
			var ferr error
			page, ferr = followAuth(raw)
			return ferr
		}),
	})
	if err != nil {
		t.Fatal(errString(err))
	}
	if tok.AccessToken != "atk-implicit" || tok.TokenType != "Bearer" || tok.Scope != "read" || tok.Ref != "oauth2/imp" {
		t.Fatal("implicit token was not parsed")
	}
	if !tok.ExpiresAt.Equal(now.Add(60 * time.Second)) {
		t.Fatal("implicit expires_in was not applied")
	}
	if strings.Contains(page, "atk-implicit") || !strings.Contains(page, "Completing sign-in") || !strings.Contains(page, "fetch(location.pathname") {
		t.Fatal("implicit page echoed the token or skipped the fragment script")
	}
	auth := as.authQuery()
	if auth.Get("response_type") != "token" || auth.Get("state") == "" || auth.Get("state") == "fixed-state" {
		t.Fatal("implicit authorize query was wrong")
	}
	dialClosed(t, hostport(auth.Get("redirect_uri")))

	as = newFake(t)
	as.setBehavior(true, "")
	_, err = oauth2.Fetch(context.Background(), &model.OAuth2Config{
		GrantType: model.GrantImplicit, AuthURL: as.url("/authorize"), ClientID: "cid",
	}, oauth2.FetchOptions{Browser: followBrowser()})
	if err == nil || !strings.Contains(err.Error(), "state") || strings.Contains(err.Error(), "atk-implicit") {
		t.Fatal("implicit state mismatch was not reported cleanly")
	}
}

func TestEphemeralLoopbackNote(t *testing.T) {
	as := newFake(t)
	var note, redirect string
	cfg := browserCfg(as)
	cfg.RedirectURI = "https://app.example/callback"
	_, err := oauth2.Fetch(context.Background(), cfg, oauth2.FetchOptions{
		HTTPClient: as.client(),
		Browser:    followBrowser(),
		OnNote: func(s string) {
			note = s
		},
	})
	if err != nil {
		t.Fatal(errString(err))
	}
	redirect = as.authQuery().Get("redirect_uri")
	if !strings.HasPrefix(redirect, "http://127.0.0.1:") || strings.Contains(redirect, "app.example") {
		t.Fatal("ephemeral redirect was not a loopback URI")
	}
	if !strings.Contains(note, "must allow") || !strings.Contains(note, redirect) {
		t.Fatal("loopback note was missing")
	}
	dialClosed(t, hostport(redirect))
}

func TestTimeoutAndCancellationCloseTheListener(t *testing.T) {
	cfg := &model.OAuth2Config{
		GrantType:      model.GrantAuthorizationCode,
		AuthURL:        "https://idp.example/authorize",
		AccessTokenURL: "https://idp.example/token",
		ClientID:       "cid",
	}
	var host string
	_, err := oauth2.Fetch(context.Background(), cfg, oauth2.FetchOptions{
		Timeout: 80 * time.Millisecond,
		Browser: oauth2.BrowserFunc(func(raw string) error {
			u, _ := url.Parse(raw)
			host = hostport(u.Query().Get("redirect_uri"))
			return nil
		}),
	})
	if !errors.Is(err, oauth2.ErrTimeout) {
		t.Fatal("timeout was not reported")
	}
	dialClosed(t, host)

	ctx, cancel := context.WithCancel(context.Background())
	_, err = oauth2.Fetch(ctx, cfg, oauth2.FetchOptions{
		Browser: oauth2.BrowserFunc(func(raw string) error {
			u, _ := url.Parse(raw)
			host = hostport(u.Query().Get("redirect_uri"))
			cancel()
			return nil
		}),
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation was not reported")
	}
	dialClosed(t, host)
}

func TestCanceledBeforeRequest(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := oauth2.Fetch(ctx, &model.OAuth2Config{GrantType: model.GrantClientCredentials, AccessTokenURL: "https://idp.example/token"}, oauth2.FetchOptions{})
	if !errors.Is(err, context.Canceled) {
		t.Fatal("canceled context was not reported")
	}
}

func TestTokenRequestCanceled(t *testing.T) {
	started := make(chan struct{})
	var once sync.Once
	tokenURL := serveLocal(t, func(w http.ResponseWriter, r *http.Request) {
		once.Do(func() { close(started) })
		<-r.Context().Done()
	})
	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	go func() {
		_, err := oauth2.Fetch(ctx, &model.OAuth2Config{
			GrantType:      model.GrantClientCredentials,
			AccessTokenURL: tokenURL,
			ClientID:       "cid",
			ClientSecret:   leakMarker,
		}, oauth2.FetchOptions{})
		errc <- err
	}()
	<-started
	cancel()
	err := <-errc
	if !errors.Is(err, context.Canceled) || strings.Contains(err.Error(), leakMarker) {
		t.Fatal("in-flight cancel leaked or was not reported")
	}
}

func TestRedirectNotFollowedAndRejectedInput(t *testing.T) {
	tokenHits := 0
	tokenURL := serveLocal(t, func(w http.ResponseWriter, r *http.Request) {
		tokenHits++
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"access_token":"atk","token_type":"Bearer"}`)
	})
	redirURL := serveLocal(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, tokenURL, http.StatusFound)
	})
	_, err := oauth2.Fetch(context.Background(), &model.OAuth2Config{
		GrantType:      model.GrantClientCredentials,
		AccessTokenURL: redirURL,
		ClientID:       "cid",
		ClientSecret:   "csecret",
	}, oauth2.FetchOptions{})
	if err == nil || tokenHits != 0 || strings.Contains(err.Error(), "atk") {
		t.Fatal("token redirect was followed or leaked")
	}

	if _, err := oauth2.Fetch(context.Background(), nil, oauth2.FetchOptions{}); err == nil {
		t.Fatal("nil config accepted")
	}
	if _, err := oauth2.Fetch(context.Background(), &model.OAuth2Config{GrantType: "device_code"}, oauth2.FetchOptions{}); err == nil {
		t.Fatal("unknown grant accepted")
	}
	if _, err := oauth2.Fetch(context.Background(), &model.OAuth2Config{GrantType: model.GrantClientCredentials}, oauth2.FetchOptions{}); err == nil {
		t.Fatal("missing token URL accepted")
	}
	if _, err := oauth2.Fetch(context.Background(), &model.OAuth2Config{
		GrantType: model.GrantAuthorizationCode, AccessTokenURL: "https://idp.example/token", ClientID: "cid",
	}, oauth2.FetchOptions{Browser: followBrowser()}); err == nil {
		t.Fatal("missing authorization URL accepted")
	}
	if _, err := oauth2.Fetch(context.Background(), &model.OAuth2Config{
		GrantType:           model.GrantAuthorizationCodePKCE,
		AuthURL:             "https://idp.example/authorize",
		AccessTokenURL:      "https://idp.example/token",
		ClientID:            "cid",
		CodeChallengeMethod: "S512",
	}, oauth2.FetchOptions{Browser: oauth2.BrowserFunc(func(string) error {
		t.Fatal("browser opened for an invalid challenge method")
		return nil
	})}); err == nil {
		t.Fatal("invalid challenge method accepted")
	}
}

type fakeAS struct {
	t          *testing.T
	srv        *http.Server
	urlBase    string
	mu         sync.Mutex
	auth       url.Values
	form       url.Values
	hdr        string
	hitPath    string
	status     int
	body       string
	ct         string
	badState   bool
	authError  string
	seenMethod string
	seenChal   string
}

func newFake(t *testing.T) *fakeAS {
	t.Helper()
	f := &fakeAS{
		t:      t,
		status: http.StatusOK,
		ct:     "application/json",
		body:   `{"access_token":"atk","token_type":"Bearer","expires_in":3600,"refresh_token":"rtk","id_token":"idt","scope":"read write"}`,
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	f.srv = &http.Server{Handler: http.HandlerFunc(f.serve), ReadHeaderTimeout: time.Second}
	go func() { _ = f.srv.Serve(ln) }()
	f.urlBase = "http://" + ln.Addr().String()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = f.srv.Shutdown(ctx)
	})
	return f
}

func (f *fakeAS) url(path string) string { return f.urlBase + path }

func (f *fakeAS) client() *http.Client {
	return &http.Client{Timeout: 5 * time.Second}
}

func (f *fakeAS) setBehavior(badState bool, authErr string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.badState = badState
	f.authError = authErr
}

func (f *fakeAS) setToken(status int, ct, body string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.status, f.ct, f.body = status, ct, body
}

func (f *fakeAS) authQuery() url.Values {
	f.mu.Lock()
	defer f.mu.Unlock()
	return cloneValues(f.auth)
}

func (f *fakeAS) tokenRequest() (url.Values, string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return cloneValues(f.form), f.hdr
}

func (f *fakeAS) tokenHitsPath() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.hitPath
}

func (f *fakeAS) serve(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/authorize":
		f.authorize(w, r)
	case "/token", "/refresh":
		f.token(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (f *fakeAS) authorize(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f.mu.Lock()
	f.auth = cloneValues(q)
	f.seenMethod = q.Get("code_challenge_method")
	f.seenChal = q.Get("code_challenge")
	badState, authErr := f.badState, f.authError
	f.mu.Unlock()
	state := q.Get("state")
	if badState {
		state = "wrong-state"
	}
	redir := q.Get("redirect_uri")
	if authErr != "" {
		v := url.Values{}
		v.Set("error", authErr)
		v.Set("error_description", "denied by user")
		v.Set("state", state)
		http.Redirect(w, r, redir+"?"+v.Encode(), http.StatusFound)
		return
	}
	if q.Get("response_type") == "token" {
		frag := url.Values{}
		frag.Set("access_token", "atk-implicit")
		frag.Set("token_type", "Bearer")
		frag.Set("expires_in", "60")
		frag.Set("scope", "read")
		frag.Set("state", state)
		http.Redirect(w, r, redir+"#"+frag.Encode(), http.StatusFound)
		return
	}
	v := url.Values{}
	v.Set("code", "authcode")
	v.Set("state", state)
	http.Redirect(w, r, redir+"?"+v.Encode(), http.StatusFound)
}

func (f *fakeAS) token(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	f.mu.Lock()
	f.form = cloneValues(r.PostForm)
	f.hdr = r.Header.Get("Authorization")
	f.hitPath = r.URL.Path
	method, chal := f.seenMethod, f.seenChal
	status, ct, body := f.status, f.ct, f.body
	f.mu.Unlock()
	if r.PostForm.Get("grant_type") == "authorization_code" && chal != "" {
		if !pkceMatches(r.PostForm.Get("code_verifier"), chal, method) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"error":"invalid_grant","error_description":"pkce mismatch"}`)
			return
		}
	}
	w.Header().Set("Content-Type", ct)
	w.WriteHeader(status)
	_, _ = io.WriteString(w, body)
}

func followBrowser() oauth2.Browser {
	return oauth2.BrowserFunc(func(raw string) error {
		_, err := followAuth(raw)
		return err
	})
}

func followAuth(raw string) (string, error) {
	c := &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	resp, err := c.Get(raw)
	if err != nil {
		return "", err
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	loc := resp.Header.Get("Location")
	if loc == "" {
		return "", errors.New("authorization server did not redirect")
	}
	u, err := url.Parse(loc)
	if err != nil {
		return "", err
	}
	if u.Fragment != "" {
		base := *u
		base.Fragment = ""
		base.RawFragment = ""
		page, err := c.Get(base.String())
		if err != nil {
			return "", err
		}
		body, err := slurp(page)
		if err != nil {
			return "", err
		}
		post, err := c.Post(base.String(), "application/x-www-form-urlencoded", strings.NewReader(u.Fragment))
		if err != nil {
			return body, err
		}
		_, _ = slurp(post)
		if post.StatusCode >= 400 {
			return body, errors.New("fragment post failed")
		}
		return body, nil
	}
	next, err := c.Get(u.String())
	if err != nil {
		return "", err
	}
	body, err := slurp(next)
	if err != nil {
		return "", err
	}
	if next.StatusCode >= 400 {
		return body, errors.New("callback failed")
	}
	return body, nil
}

func slurp(resp *http.Response) (string, error) {
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return string(b), err
}

func pkceMatches(verifier, challenge, method string) bool {
	if verifier == "" || challenge == "" {
		return false
	}
	switch method {
	case "S256":
		sum := sha256.Sum256([]byte(verifier))
		return base64.RawURLEncoding.EncodeToString(sum[:]) == challenge
	case "plain":
		return verifier == challenge
	default:
		return false
	}
}

func decodeBasic(t *testing.T, header string) (string, string) {
	t.Helper()
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(header, "Basic "))
	if err != nil {
		t.Fatal("basic header was not decodable")
	}
	user, pass, ok := strings.Cut(string(raw), ":")
	if !ok {
		t.Fatal("basic header had no separator")
	}
	user, err = url.QueryUnescape(user)
	if err != nil {
		t.Fatal(err)
	}
	pass, err = url.QueryUnescape(pass)
	if err != nil {
		t.Fatal(err)
	}
	return user, pass
}

func dialClosed(t *testing.T, host string) {
	t.Helper()
	conn, err := net.DialTimeout("tcp", host, 200*time.Millisecond)
	if err == nil {
		_ = conn.Close()
		t.Fatal("loopback listener is still accepting connections")
	}
}

func browserCfg(as *fakeAS) *model.OAuth2Config {
	return &model.OAuth2Config{
		GrantType:      model.GrantAuthorizationCode,
		AuthURL:        as.url("/authorize"),
		AccessTokenURL: as.url("/token"),
		ClientID:       "cid",
		ClientSecret:   "csecret",
		State:          "fixed-state",
	}
}

func baseCC(as *fakeAS) *model.OAuth2Config {
	return &model.OAuth2Config{
		GrantType:      model.GrantClientCredentials,
		AccessTokenURL: as.url("/token"),
		ClientID:       "cid",
		ClientSecret:   "csecret",
		Scope:          "read",
	}
}

func hostport(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return u.Host
}

func cloneValues(v url.Values) url.Values {
	out := url.Values{}
	for k, vs := range v {
		out[k] = append([]string(nil), vs...)
	}
	return out
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	if strings.Contains(err.Error(), leakMarker) || strings.Contains(err.Error(), "csecret") || strings.Contains(err.Error(), "atk") {
		return "error contains secret material"
	}
	return err.Error()
}

func serveLocal(t *testing.T, h http.HandlerFunc) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: h, ReadHeaderTimeout: time.Second}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	})
	return "http://" + ln.Addr().String()
}
