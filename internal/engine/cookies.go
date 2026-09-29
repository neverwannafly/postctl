package engine

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/shubham/postctl/internal/httpx"
	"github.com/shubham/postctl/internal/model"
	"github.com/shubham/postctl/internal/script"
)

// NewCookieAccess adapts the workspace cookie jar to the [script.CookieAccess]
// interface that backs pm.cookies and pm.cookies.jar(). The jar itself does
// not satisfy it (it has no per-URL clear), so the adapter builds every method
// from the jar's public API: Get, Set, List and Delete.
//
// The adapter is safe for concurrent use because the jar is. A nil jar yields
// an adapter whose reads are empty and whose writes fail the same way a nil
// [script.CookieAccess] would.
func NewCookieAccess(jar *httpx.CookieJar) script.CookieAccess {
	if jar == nil {
		return nil
	}
	return cookieAdapter{jar: jar}
}

type cookieAdapter struct {
	jar *httpx.CookieJar
}

var _ script.CookieAccess = cookieAdapter{}

// normalizeURL accepts the scheme-less URLs that scripts often pass
// ("example.com/path") as well as absolute ones.
func normalizeURL(raw string) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	return url.Parse(raw)
}

// Cookies returns the cookies the jar would send to rawURL, with the metadata
// the jar keeps for them (domain, path, expiry and flags).
func (a cookieAdapter) Cookies(rawURL string) ([]model.Cookie, error) {
	target, err := normalizeURL(rawURL)
	if err != nil {
		return nil, err
	}
	sending := a.jar.Cookies(target)
	records := a.jar.List("")
	out := make([]model.Cookie, 0, len(sending))
	for _, c := range sending {
		cookie := model.Cookie{Name: c.Name, Value: c.Value, Session: true}
		for _, rec := range records {
			if rec.Name != c.Name || !recordAppliesTo(rec, target, true) {
				continue
			}
			cookie.Domain = rec.Domain
			cookie.Path = rec.Path
			cookie.HostOnly = rec.HostOnly
			cookie.HTTPOnly = rec.HTTPOnly
			cookie.Secure = rec.Secure
			if !rec.Expires.IsZero() {
				cookie.Expires = rec.Expires.UTC().Format(http.TimeFormat)
				cookie.Session = false
			}
			if rec.MaxAge != 0 {
				cookie.MaxAge = strconv.Itoa(rec.MaxAge)
				cookie.Session = false
			}
			break
		}
		out = append(out, cookie)
	}
	return out, nil
}

// SetCookie stores c as though rawURL had sent it in a Set-Cookie header.
func (a cookieAdapter) SetCookie(rawURL string, c model.Cookie) error {
	target, err := normalizeURL(rawURL)
	if err != nil {
		return err
	}
	cookie := &http.Cookie{
		Name:     c.Name,
		Value:    c.Value,
		Domain:   c.Domain,
		Path:     c.Path,
		Secure:   c.Secure,
		HttpOnly: c.HTTPOnly,
	}
	if c.HostOnly {
		cookie.Domain = ""
	}
	if c.Expires != "" {
		if t, err := http.ParseTime(c.Expires); err == nil {
			cookie.Expires = t
		}
	}
	if c.MaxAge != "" {
		if n, err := strconv.Atoi(c.MaxAge); err == nil {
			cookie.MaxAge = n
			if n == 0 {
				cookie.MaxAge = -1
			}
		}
	}
	return a.jar.Set(target.String(), cookie)
}

// UnsetCookie removes every cookie called name whose domain matches rawURL.
func (a cookieAdapter) UnsetCookie(rawURL, name string) error {
	return a.remove(rawURL, func(rec httpx.StoredCookie) bool { return rec.Name == name })
}

// ClearCookies removes every cookie whose domain matches rawURL.
func (a cookieAdapter) ClearCookies(rawURL string) error {
	return a.remove(rawURL, func(httpx.StoredCookie) bool { return true })
}

func (a cookieAdapter) remove(rawURL string, keep func(httpx.StoredCookie) bool) error {
	target, err := normalizeURL(rawURL)
	if err != nil {
		return err
	}
	for _, rec := range a.jar.List("") {
		if !keep(rec) || !recordAppliesTo(rec, target, false) {
			continue
		}
		if err := a.jar.Delete(rec.Domain, rec.Path, rec.Name); err != nil {
			return err
		}
	}
	return nil
}

// recordAppliesTo reports whether the stored cookie belongs to target's
// domain and, when matchPath is set, its path.
func recordAppliesTo(rec httpx.StoredCookie, target *url.URL, matchPath bool) bool {
	host := strings.ToLower(target.Hostname())
	domain := strings.ToLower(strings.TrimPrefix(rec.Domain, "."))
	if host != domain && (rec.HostOnly || !strings.HasSuffix(host, "."+domain)) {
		return false
	}
	if !matchPath {
		return true
	}
	path := target.EscapedPath()
	if path == "" {
		path = "/"
	}
	cp := rec.Path
	if cp == "" {
		cp = "/"
	}
	if path == cp {
		return true
	}
	return strings.HasPrefix(path, cp) && (strings.HasSuffix(cp, "/") || path[len(cp)] == '/')
}
