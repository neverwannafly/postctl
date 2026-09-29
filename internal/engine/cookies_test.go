package engine

import (
	"testing"
	"time"

	"github.com/shubham/postctl/internal/httpx"
	"github.com/shubham/postctl/internal/model"
)

func newJarAccess(t *testing.T) (*httpx.CookieJar, interface {
	Cookies(rawURL string) ([]model.Cookie, error)
	SetCookie(rawURL string, c model.Cookie) error
	UnsetCookie(rawURL, name string) error
	ClearCookies(rawURL string) error
}) {
	t.Helper()
	jar, err := httpx.NewCookieJar(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return jar, NewCookieAccess(jar)
}

func TestCookieAdapter(t *testing.T) {
	jar, access := newJarAccess(t)
	const site = "https://api.example.com/v1/items"

	if err := access.SetCookie(site, model.Cookie{Name: "host", Value: "1", Path: "/"}); err != nil {
		t.Fatal(err)
	}
	if err := access.SetCookie(site, model.Cookie{Name: "wide", Value: "2", Domain: "example.com", Path: "/", Secure: true, HTTPOnly: true}); err != nil {
		t.Fatal(err)
	}
	if err := access.SetCookie(site, model.Cookie{Name: "scoped", Value: "3", Path: "/admin"}); err != nil {
		t.Fatal(err)
	}
	if err := access.SetCookie("https://other.test/", model.Cookie{Name: "elsewhere", Value: "4", Path: "/"}); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(time.Hour).UTC().Format("Mon, 02 Jan 2006 15:04:05 GMT")
	if err := access.SetCookie(site, model.Cookie{Name: "dated", Value: "5", Path: "/", Expires: future}); err != nil {
		t.Fatal(err)
	}

	got, err := access.Cookies(site)
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]model.Cookie{}
	for _, c := range got {
		byName[c.Name] = c
	}
	if len(byName) != 3 || byName["host"].Value != "1" || byName["wide"].Value != "2" || byName["dated"].Value != "5" {
		t.Fatalf("cookies for %s: %+v", site, got)
	}
	if c := byName["wide"]; c.Domain != "example.com" || c.HostOnly || !c.Secure || !c.HTTPOnly || !c.Session {
		t.Fatalf("wide metadata: %+v", c)
	}
	if c := byName["host"]; !c.HostOnly || c.Domain != "api.example.com" {
		t.Fatalf("host metadata: %+v", c)
	}
	if c := byName["dated"]; c.Session || c.Expires == "" {
		t.Fatalf("dated metadata: %+v", c)
	}
	if got, _ := access.Cookies("https://api.example.com/admin/x"); len(got) != 4 {
		t.Fatalf("path-scoped cookie should apply under /admin: %+v", got)
	}
	// Scheme-less URLs are accepted, as scripts often pass them.
	if got, err := access.Cookies("api.example.com/v1"); err != nil || len(got) == 0 {
		t.Fatalf("scheme-less URL: %v %+v", err, got)
	}

	if err := access.UnsetCookie(site, "host"); err != nil {
		t.Fatal(err)
	}
	if got, _ := access.Cookies(site); len(got) != 2 {
		t.Fatalf("after unset: %+v", got)
	}

	if err := access.ClearCookies(site); err != nil {
		t.Fatal(err)
	}
	if got, _ := access.Cookies(site); len(got) != 0 {
		t.Fatalf("after clear: %+v", got)
	}
	// Clearing one site leaves the others alone, and the change is persisted.
	if got, _ := access.Cookies("https://other.test/"); len(got) != 1 {
		t.Fatalf("other site should keep its cookie: %+v", got)
	}
	if list := jar.List(""); len(list) != 1 || list[0].Name != "elsewhere" {
		t.Fatalf("jar records: %+v", list)
	}
}

func TestCookieAccessNilJar(t *testing.T) {
	if NewCookieAccess(nil) != nil {
		t.Fatal("a nil jar must give a nil CookieAccess so scripts see no jar")
	}
}
