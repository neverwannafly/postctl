package httpx

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/shubham/postctl/internal/model"
)

func TestCookieJarPersistenceRoundTripAndManagement(t *testing.T) {
	t.Parallel()
	workspace := t.TempDir()
	jar, err := NewCookieJar(workspace)
	if err != nil {
		t.Fatal(err)
	}
	if err := jar.Set("https://api.example.test/a/b", &http.Cookie{
		Name:     "session",
		Value:    "synthetic",
		Path:     "/a",
		Secure:   true,
		HttpOnly: true,
	}); err != nil {
		t.Fatal(err)
	}
	if err := jar.Set("https://api.example.test/", &http.Cookie{
		Name:   "other",
		Value:  "value",
		Domain: "example.test",
		Path:   "/",
	}); err != nil {
		t.Fatal(err)
	}

	reloaded, err := NewCookieJar(workspace)
	if err != nil {
		t.Fatal(err)
	}
	cookies, err := reloaded.Get("https://api.example.test/a/resource")
	if err != nil {
		t.Fatal(err)
	}
	if len(cookies) != 2 {
		t.Fatalf("reloaded cookies = %d", len(cookies))
	}
	if listed := reloaded.List("api.example.test"); len(listed) != 1 || !listed[0].HostOnly {
		t.Fatalf("host-only listing = %#v", listed)
	}
	if err := reloaded.Delete("api.example.test", "/a", "session"); err != nil {
		t.Fatal(err)
	}
	cookies, err = reloaded.Get("https://api.example.test/a/resource")
	if err != nil || len(cookies) != 1 {
		t.Fatalf("cookies after delete = %d, err = %v", len(cookies), err)
	}
	if err := reloaded.Clear("example.test"); err != nil {
		t.Fatal(err)
	}
	cookies, err = reloaded.Get("https://api.example.test/")
	if err != nil || len(cookies) != 0 {
		t.Fatalf("cookies after clear = %d, err = %v", len(cookies), err)
	}
}

func TestCookieJarRejectsPublicSuffixDomain(t *testing.T) {
	t.Parallel()
	jar, err := NewCookieJar(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := jar.Set("https://example.com/", &http.Cookie{
		Name:   "bad",
		Value:  "value",
		Domain: "com",
	}); err != nil {
		t.Fatal(err)
	}
	cookies, err := jar.Get("https://example.com/")
	if err != nil {
		t.Fatal(err)
	}
	if len(cookies) != 0 {
		t.Fatal("public-suffix cookie was accepted")
	}
}

func TestClientUsesPersistentCookieJar(t *testing.T) {
	t.Parallel()
	jar, err := NewCookieJar(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls++
		if calls == 1 {
			http.SetCookie(writer, &http.Cookie{Name: "seen", Value: "yes", Path: "/"})
		} else if cookie, err := request.Cookie("seen"); err != nil || cookie.Value != "yes" {
			t.Error("stored cookie was not sent")
		}
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	request := model.NewRequest("cookies", http.MethodGet, server.URL)
	client := NewClient("test")
	if _, err := client.Send(t.Context(), request, Options{CookieJar: jar}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Send(t.Context(), request, Options{CookieJar: jar}); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("server calls = %d", calls)
	}
}
