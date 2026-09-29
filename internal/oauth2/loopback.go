package oauth2

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"html/template"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/shubham/postctl/internal/model"
)

const (
	pageSignedIn = "Signed in. You can close this window and return to postctl."
	pageCode     = "Authorization received. You can close this window and return to postctl."
	pageState    = "Sign-in state did not match. Return to postctl and try again."
	pageBad      = "The sign-in response was incomplete. Return to postctl and try again."
	pageFailed   = "Sign-in failed. Return to postctl and try again."
)

// pageHTML is the calm callback page. The script flag is only set for the
// implicit grant, whose tokens arrive in the URL fragment and must be posted
// back by the page itself. No token or code is written into the HTML.
const pageHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>postctl</title>
<style>
  body { margin: 0; background: #f4f1ea; color: #1d1c19; font: 17px/1.5 Georgia, "Iowan Old Style", Palatino, serif; }
  main { max-width: 34rem; margin: 12vh auto; padding: 0 1.5rem; }
  h1 { font-size: 1.25rem; font-weight: 600; margin: 0 0 0.5rem; }
  p { margin: 0; }
</style>
</head>
<body>
<main>
  <h1>{{.Title}}</h1>
  <p>{{.Body}}</p>
</main>
{{- if .Script }}
<script>
(function () {
  var hash = location.hash.charAt(0) === "#" ? location.hash.slice(1) : "";
  history.replaceState(null, "", location.pathname + location.search);
  function show(title, text) {
    document.querySelector("h1").textContent = title;
    document.querySelector("p").textContent = text;
  }
  fetch(location.pathname, {
    method: "POST",
    headers: {"Content-Type": "application/x-www-form-urlencoded"},
    body: hash
  }).then(function (r) {
    if (!r.ok) { show("Sign-in failed", "Sign-in failed. Return to postctl and try again."); return; }
    show("Signed in", "Signed in. You can close this window and return to postctl.");
  }).catch(function () {
    show("Sign-in failed", "Sign-in failed. Return to postctl and try again.");
  });
})();
</script>
{{- end }}
</body>
</html>
`

var pageTmpl = template.Must(template.New("page").Parse(pageHTML))

type pageData struct {
	Title  string
	Body   string
	Script bool
}

type callbackResult struct {
	code  string
	token *model.OAuth2Token
	err   error
}

type loopback struct {
	addr     string
	path     string
	redirect string
	state    string
	note     string
	implicit bool
	now      time.Time
	result   chan callbackResult
	srv      *http.Server
	ln       net.Listener
	once     sync.Once
}

func listen(cfg *model.OAuth2Config, implicit bool, now time.Time) (*loopback, error) {
	spec, err := redirectSpec(cfg.RedirectURI)
	if err != nil {
		return nil, err
	}
	ln, err := net.Listen("tcp", spec.listen)
	if err != nil {
		return nil, errors.New("oauth2: could not listen on " + spec.listen)
	}
	lb := &loopback{
		addr:     ln.Addr().String(),
		path:     spec.path,
		implicit: implicit,
		now:      now,
		result:   make(chan callbackResult, 1),
		ln:       ln,
	}
	if spec.fixedURI != "" {
		lb.redirect = spec.fixedURI
	} else {
		lb.redirect = "http://" + lb.addr + lb.path
		lb.note = loopbackNote(lb.redirect, cfg.RedirectURI)
	}
	lb.state, err = randomToken(32)
	if err != nil {
		_ = ln.Close()
		return nil, errors.New("oauth2: could not prepare sign-in")
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", lb.serve)
	lb.srv = &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		MaxHeaderBytes:    1 << 16,
	}
	go func() { _ = lb.srv.Serve(ln) }()
	return lb, nil
}

type redirectPlan struct {
	listen   string
	path     string
	fixedURI string
}

func redirectSpec(redirect string) (redirectPlan, error) {
	if strings.TrimSpace(redirect) == "" {
		p, err := randomToken(16)
		if err != nil {
			return redirectPlan{}, errors.New("oauth2: could not prepare sign-in")
		}
		return redirectPlan{listen: "127.0.0.1:0", path: "/cb/" + p}, nil
	}
	u, err := url.Parse(redirect)
	if err != nil || u.Host == "" || u.Scheme == "" {
		return redirectPlan{}, errors.New("oauth2: redirect URI is not a valid URL")
	}
	if u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return redirectPlan{}, errors.New("oauth2: redirect URI must not include user info, a query, or a fragment")
	}
	host := strings.ToLower(u.Hostname())
	if u.Scheme == "http" && (host == "127.0.0.1" || host == "localhost") {
		port := u.Port()
		if port == "" {
			port = "80"
		}
		path := u.Path
		if path == "" {
			path = "/"
		}
		return redirectPlan{
			listen:   net.JoinHostPort("127.0.0.1", port),
			path:     path,
			fixedURI: redirect,
		}, nil
	}
	p, err := randomToken(16)
	if err != nil {
		return redirectPlan{}, errors.New("oauth2: could not prepare sign-in")
	}
	return redirectPlan{listen: "127.0.0.1:0", path: "/cb/" + p}, nil
}

func loopbackNote(redirect, configured string) string {
	if strings.TrimSpace(configured) == "" {
		return "The provider must allow the loopback redirect URI " + redirect + ". postctl generated it because no redirect URI is configured."
	}
	return "The provider must allow the loopback redirect URI " + redirect + ". postctl used it because the configured redirect URI is not an http://127.0.0.1 or http://localhost URL."
}

func (l *loopback) close() {
	l.once.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if l.srv != nil {
			_ = l.srv.Shutdown(ctx)
		}
		if l.ln != nil {
			_ = l.ln.Close()
		}
	})
}

func (l *loopback) serve(w http.ResponseWriter, r *http.Request) {
	if !hostOK(r.Host, l.addr) || r.URL.Path != l.path {
		writePage(w, http.StatusNotFound, "Not found", pageBad, false)
		return
	}
	switch r.Method {
	case http.MethodGet:
		if l.implicit {
			writePage(w, http.StatusOK, "Completing sign-in", "Waiting for the browser to finish.", true)
			return
		}
		l.finishCode(w, r.URL.Query())
	case http.MethodPost:
		if !l.implicit {
			writePage(w, http.StatusMethodNotAllowed, "Sign-in failed", pageBad, false)
			return
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, 32<<10))
		if err != nil {
			writePage(w, http.StatusBadRequest, "Sign-in failed", pageBad, false)
			return
		}
		q, err := url.ParseQuery(string(body))
		if err != nil {
			writePage(w, http.StatusBadRequest, "Sign-in failed", pageBad, false)
			return
		}
		l.finishImplicit(w, q)
	default:
		writePage(w, http.StatusMethodNotAllowed, "Sign-in failed", pageBad, false)
	}
}

func (l *loopback) finishCode(w http.ResponseWriter, q url.Values) {
	if !stateOK(q, l.state) {
		l.send(callbackResult{err: &Error{Code: "invalid_request", Description: "sign-in state did not match"}})
		writePage(w, http.StatusBadRequest, "Sign-in failed", pageState, false)
		return
	}
	if msg, ok := one(q, "error"); ok {
		desc, _ := one(q, "error_description")
		l.send(callbackResult{err: &Error{Code: msg, Description: desc}})
		writePage(w, http.StatusBadRequest, "Sign-in failed", pageFailed, false)
		return
	}
	code, ok := one(q, "code")
	if !ok || code == "" || len(code) > 8<<10 {
		l.send(callbackResult{err: &Error{Code: "invalid_request", Description: "the sign-in response did not include an authorization code"}})
		writePage(w, http.StatusBadRequest, "Sign-in failed", pageBad, false)
		return
	}
	l.send(callbackResult{code: code})
	writePage(w, http.StatusOK, "Authorization received", pageCode, false)
}

func (l *loopback) finishImplicit(w http.ResponseWriter, q url.Values) {
	if !stateOK(q, l.state) {
		l.send(callbackResult{err: &Error{Code: "invalid_request", Description: "sign-in state did not match"}})
		writePage(w, http.StatusBadRequest, "Sign-in failed", pageState, false)
		return
	}
	if msg, ok := one(q, "error"); ok {
		desc, _ := one(q, "error_description")
		l.send(callbackResult{err: &Error{Code: msg, Description: desc}})
		writePage(w, http.StatusBadRequest, "Sign-in failed", pageFailed, false)
		return
	}
	access, ok := one(q, "access_token")
	if !ok || access == "" || len(access) > 16<<10 {
		l.send(callbackResult{err: &Error{Code: "invalid_request", Description: "the sign-in response did not include an access token"}})
		writePage(w, http.StatusBadRequest, "Sign-in failed", pageBad, false)
		return
	}
	refresh, _ := one(q, "refresh_token")
	idTok, _ := one(q, "id_token")
	typ, _ := one(q, "token_type")
	scope, _ := one(q, "scope")
	expRaw, _ := one(q, "expires_in")
	if len(refresh) > 16<<10 || len(idTok) > 24<<10 {
		l.send(callbackResult{err: &Error{Code: "invalid_request", Description: "token response was rejected"}})
		writePage(w, http.StatusBadRequest, "Sign-in failed", pageBad, false)
		return
	}
	l.send(callbackResult{token: &model.OAuth2Token{
		AccessToken:  access,
		RefreshToken: refresh,
		IDToken:      idTok,
		TokenType:    typ,
		Scope:        scope,
		ExpiresAt:    expiry(l.now, expRaw),
	}})
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusNoContent)
}

func (l *loopback) send(res callbackResult) {
	select {
	case l.result <- res:
	default:
	}
}

func stateOK(q url.Values, want string) bool {
	got, ok := one(q, "state")
	if !ok || want == "" || len(got) != len(want) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}

func one(q url.Values, key string) (string, bool) {
	vs, ok := q[key]
	if !ok {
		return "", false
	}
	if len(vs) != 1 {
		return "", false
	}
	return vs[0], true
}

func hostOK(host, addr string) bool {
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	h, p, err := net.SplitHostPort(host)
	if err != nil {
		return false
	}
	if p != port {
		return false
	}
	return h == "127.0.0.1" || strings.EqualFold(h, "localhost")
}

func writePage(w http.ResponseWriter, status int, title, body string, script bool) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; script-src 'unsafe-inline'")
	w.WriteHeader(status)
	if err := pageTmpl.Execute(w, pageData{Title: title, Body: body, Script: script}); err != nil {
		return
	}
}

func randomToken(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
