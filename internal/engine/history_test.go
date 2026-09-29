package engine

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/shubham/postctl/internal/history"
	"github.com/shubham/postctl/internal/model"
	"github.com/shubham/postctl/internal/store"
)

// Nothing that identifies a credential may reach the history files: not the
// value of a secret variable wherever it appears, not credential headers, not
// credential-looking fields, and not an API key sent in the query string.
func TestHistoryHoldsNoSecrets(t *testing.T) {
	const (
		bearer   = "bearer-secret-value-7f3a"
		apiKey   = "api-key-secret-value-91bc"
		password = "literal-password-hunter2"
		cookie   = "session-cookie-value-55aa"
		echoed   = "echoed-token-value-cc12"
	)
	srv := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		http.SetCookie(w, &http.Cookie{Name: "session", Value: cookie, Path: "/"})
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"received":` + string(rune('0'+len(body)%10)) + `,"echo":"` + bearer + `","access_token":"` + echoed + `","fine":"visible"}`))
	})
	f := newFixture(t, srv)
	f.env.Variables = append(f.env.Variables,
		model.Variable{Key: "bearer", Value: bearer, Type: model.VarSecret},
		model.Variable{Key: "apiKey", Value: apiKey, Type: model.VarSecret})
	req := model.NewRequest("Login", "POST", "{{baseUrl}}/login?trace=abc")
	req.Auth = &model.Auth{Type: model.AuthBearer, Bearer: &model.BearerAuth{Token: "{{bearer}}"}}
	req.Headers = []model.KV{
		{Key: "X-Api-Key", Value: "{{apiKey}}"},
		{Key: "Content-Type", Value: "application/json"},
		{Key: "X-Public", Value: "hello"},
	}
	req.Body = model.Body{Mode: model.BodyRaw, Raw: `{"user":"bob","password":"` + password + `","note":"{{apiKey}}"}`}
	f.col.Items = append(f.col.Items, req)
	f.save()

	out := f.send(f.findRequest("Login"), nil)
	if out.Err != nil {
		t.Fatal(out.Err)
	}
	log := f.eng.cfg.History.(*history.Log)
	entry, err := log.Get(out.HistoryID)
	if err != nil {
		t.Fatal(err)
	}

	headers := map[string]string{}
	for _, h := range entry.Request.Headers {
		headers[strings.ToLower(h.Key)] = h.Value
	}
	if headers["authorization"] != history.Mask || headers["x-api-key"] != history.Mask {
		t.Fatalf("credential headers not masked: %v", headers)
	}
	if headers["x-public"] != "hello" {
		t.Fatalf("harmless header was altered: %v", headers)
	}
	if !strings.Contains(entry.Request.Body, `"user":"bob"`) || strings.Contains(entry.Request.Body, password) {
		t.Fatalf("request body: %s", entry.Request.Body)
	}
	if !strings.HasSuffix(entry.Request.URL, "/login?trace=abc") {
		t.Fatalf("url: %s", entry.Request.URL)
	}
	if entry.Response == nil || !strings.Contains(entry.Response.Body, `"fine":"visible"`) ||
		strings.Contains(entry.Response.Body, echoed) || strings.Contains(entry.Response.Body, bearer) {
		t.Fatalf("response body: %+v", entry.Response)
	}
	for _, h := range entry.Response.Headers {
		if strings.EqualFold(h.Key, "Set-Cookie") && h.Value != history.Mask {
			t.Fatalf("Set-Cookie not masked: %q", h.Value)
		}
	}

	for path, content := range filesUnder(t, f.dir+"/.postctl/history") {
		for _, secret := range []string{bearer, apiKey, password, cookie, echoed} {
			if strings.Contains(content, secret) {
				t.Errorf("a secret value is present in %s", strings.TrimPrefix(path, f.dir))
			}
		}
	}
}

func TestHistoryDisabled(t *testing.T) {
	srv := newServer(t, jsonHandler(`{}`))
	f := newFixture(t, srv)
	f.col.Items = append(f.col.Items, model.NewRequest("R", "GET", "{{baseUrl}}/x"))
	f.save(func(c *Config) { c.DisableHistory = true })
	out := f.send(f.findRequest("R"), nil)
	if out.HistoryID != "" || out.Err != nil {
		t.Fatalf("outcome: %+v", out)
	}
}

func TestHistoryMasksKeychainOAuthTokensByValue(t *testing.T) {
	const (
		access  = "synthetic-oauth-access-value"
		refresh = "synthetic-oauth-refresh-value"
		idToken = "synthetic-oauth-id-value"
	)
	srv := newServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jwt":"` + access + `","renew":"` + refresh + `","identity":"` + idToken + `"}`))
	})
	f := newFixture(t, srv)
	req := model.NewRequest("OAuth history", "GET", "{{baseUrl}}/safe")
	ref := store.SecretRefFor(store.ScopeOAuth2, req.ID, "")
	req.Auth = &model.Auth{Type: model.AuthOAuth2, OAuth2: &model.OAuth2Config{
		GrantType:  model.GrantClientCredentials,
		AddTokenTo: model.TokenInQuery,
		Token:      &model.OAuth2Token{Ref: ref, TokenType: "Bearer"},
	}}
	if err := f.secrets.Set(context.Background(), ref,
		`{"access":"`+access+`","refresh":"`+refresh+`","id":"`+idToken+`"}`); err != nil {
		t.Fatal(err)
	}
	f.col.Items = append(f.col.Items, req)
	f.save()

	unsaved := f.findRequest("OAuth history").Clone()
	unsaved.URL = model.ParseURL(srv.URL + "/echo/" + access)
	out := f.send(unsaved, nil)
	if out.Err != nil {
		t.Fatal(out.Err)
	}
	entry, err := f.eng.cfg.History.(*history.Log).Get(out.HistoryID)
	if err != nil {
		t.Fatal(err)
	}
	raw := entry.Request.URL
	if entry.Response != nil {
		raw += entry.Response.Body
	}
	for _, secret := range []string{access, refresh, idToken} {
		if strings.Contains(raw, secret) {
			t.Errorf("history exposed a Keychain-hydrated OAuth token")
		}
	}
}
