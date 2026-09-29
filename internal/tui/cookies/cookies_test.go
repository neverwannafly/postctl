package cookies

import (
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/shubham/postctl/internal/httpx"
	"github.com/shubham/postctl/internal/tui"
	"github.com/shubham/postctl/internal/ui"
)

type screen struct {
	t      *testing.T
	m      tui.Screen
	closed bool
}

func newScreen(t *testing.T, jar Jar, w, h int) *screen {
	t.Helper()
	s := &screen{t: t}
	s.m = Factory(jar)(tui.ScreenEnv{PaneEnv: tui.PaneEnv{Theme: ui.NewTheme(true, false, false)}})
	s.m.SetSize(w, h)
	return s
}

func k(s string) tea.KeyPressMsg {
	codes := map[string]rune{"enter": tea.KeyEnter, "esc": tea.KeyEscape, "down": tea.KeyDown, "up": tea.KeyUp, "right": tea.KeyRight}
	if c, ok := codes[s]; ok {
		return tea.KeyPressMsg(tea.Key{Code: c})
	}
	return tea.KeyPressMsg(tea.Key{Code: []rune(s)[0], Text: s})
}

func (s *screen) key(keys ...string) {
	s.t.Helper()
	for _, key := range keys {
		next, cmd := s.m.Update(k(key))
		if next == nil {
			s.closed = true
			return
		}
		s.m = next
		if cmd != nil {
			next, _ = s.m.Update(cmd())
			s.m = next
		}
	}
}

func (s *screen) view() string { return ansi.Strip(s.m.View()) }

func (s *screen) has(sub string) {
	s.t.Helper()
	if !strings.Contains(s.view(), sub) {
		s.t.Fatalf("view lacks %q:\n%s", sub, s.view())
	}
}

func (s *screen) not(sub string) {
	s.t.Helper()
	if strings.Contains(s.view(), sub) {
		s.t.Fatalf("view has %q:\n%s", sub, s.view())
	}
}

func jarWithCookies(t *testing.T) *httpx.CookieJar {
	t.Helper()
	jar, err := httpx.NewCookieJar(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	set := func(raw string, c *http.Cookie) {
		u, _ := url.Parse(raw)
		if err := jar.Set(u.String(), c); err != nil {
			t.Fatal(err)
		}
	}
	exp := time.Now().Add(72 * time.Hour)
	set("https://api.example.com/", &http.Cookie{Name: "session", Value: "abc123-secret", Path: "/", Secure: true, HttpOnly: true, Expires: exp})
	set("https://api.example.com/", &http.Cookie{Name: "theme", Value: "dark", Path: "/"})
	set("https://other.test/", &http.Cookie{Name: "id", Value: "77", Path: "/"})
	return jar
}

func TestViewIsPure(t *testing.T) {
	s := newScreen(t, jarWithCookies(t), 80, 24)
	m := s.m.(*Model)
	m.cur, m.top = 1, 1
	snapshot := func() any {
		return struct {
			Cur, Top int
			Reveal   bool
			Mode     mode
		}{m.cur, m.top, m.reveal, m.mode}
	}
	before := snapshot()
	first, second := s.m.View(), s.m.View()
	if first != second || !reflect.DeepEqual(before, snapshot()) {
		t.Fatalf("View mutated cookie manager state: before=%+v after=%+v", before, snapshot())
	}
}

func TestListsGroupedByDomainWithMaskedValues(t *testing.T) {
	s := newScreen(t, jarWithCookies(t), 120, 30)
	for _, want := range []string{"3 cookies in 2 domains", "api.example.com", "other.test", "session", "theme", "HttpOnly", "Secure"} {
		s.has(want)
	}
	s.not("abc123-secret")
	s.key("r")
	s.has("abc123-secret")
	s.key("r")
	s.not("abc123-secret")
	s.key("r", "down")
	s.not("dark") // moving hides a revealed value again
}

func TestNarrowStacksDetailUnderList(t *testing.T) {
	s := newScreen(t, jarWithCookies(t), 80, 24)
	s.has("session")
	s.has("Domain")
	s.has("Value")
	for _, l := range strings.Split(s.view(), "\n") {
		if ui.Width(l) > 80 {
			t.Fatalf("too wide: %q", l)
		}
	}
	if n := len(strings.Split(s.view(), "\n")); n != 24 {
		t.Fatalf("height = %d", n)
	}
}

func TestDeleteOneCookie(t *testing.T) {
	jar := jarWithCookies(t)
	s := newScreen(t, jar, 120, 30)
	s.key("d")
	s.has("Deleted session")
	if got := jar.List(""); len(got) != 2 {
		t.Fatalf("cookies = %+v", got)
	}
	s.not("abc123")
}

func TestClearDomainNeedsConfirmation(t *testing.T) {
	jar := jarWithCookies(t)
	s := newScreen(t, jar, 120, 30)
	s.key("x")
	s.has("Clear cookies for")
	s.has("2 cookies")
	s.key("enter") // default is Cancel
	if len(jar.List("")) != 3 {
		t.Fatal("Enter on the default choice cleared cookies")
	}
	s.key("x", "right", "enter")
	got := jar.List("")
	if len(got) != 1 || got[0].Name != "id" {
		t.Fatalf("cookies = %+v", got)
	}
	s.has("Cleared")
}

func TestClearAll(t *testing.T) {
	jar := jarWithCookies(t)
	s := newScreen(t, jar, 120, 30)
	s.key("X")
	s.has("3 cookies from every domain")
	s.key("esc")
	if len(jar.List("")) != 3 {
		t.Fatal("cancel cleared cookies")
	}
	s.key("X", "right", "enter")
	if len(jar.List("")) != 0 {
		t.Fatal("not cleared")
	}
	s.has("No cookies")
}

func TestEscClosesAndEmptyJar(t *testing.T) {
	s := newScreen(t, nil, 80, 24)
	s.has("No cookies")
	s.key("d", "x", "X", "r") // no-ops on an empty jar
	s.key("esc")
	if !s.closed {
		t.Fatal("did not close")
	}
}

func TestCursorScrollsLongLists(t *testing.T) {
	jar, _ := httpx.NewCookieJar(t.TempDir())
	for i := range 40 {
		_ = jar.Set("https://a.example.com/", &http.Cookie{Name: "c" + string(rune('a'+i%26)) + string(rune('a'+i/26)), Value: "v", Path: "/"})
	}
	s := newScreen(t, jar, 80, 24)
	for range 39 {
		s.key("down")
	}
	s.has("Domain") // detail still drawn
	if !strings.Contains(s.view(), "›") {
		t.Fatalf("cursor scrolled out of view:\n%s", s.view())
	}
}
