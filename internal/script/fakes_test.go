package script

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/shubham/postctl/internal/httpx"
	"github.com/shubham/postctl/internal/model"
	"github.com/shubham/postctl/internal/vars"
)

// fakeStore is an in-package stand-in for WS2's vars.Store: enough of the
// contract to exercise every pm variable API without waiting on that branch.
type fakeStore struct {
	values  map[vars.Scope]map[string]any
	order   map[vars.Scope][]string
	changes map[vars.Scope][]vars.Change
}

func newStore() *fakeStore {
	return &fakeStore{
		values:  map[vars.Scope]map[string]any{},
		order:   map[vars.Scope][]string{},
		changes: map[vars.Scope][]vars.Change{},
	}
}

func (s *fakeStore) with(scope vars.Scope, kv map[string]any) *fakeStore {
	for _, k := range sortedKeys(kv) {
		s.Set(scope, k, kv[k])
	}
	s.changes = map[vars.Scope][]vars.Change{}
	return s
}

func sortedKeys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

func (s *fakeStore) Get(key string) (any, vars.Scope, bool) {
	for i := len(allScopes) - 1; i >= 0; i-- {
		if v, ok := s.GetIn(allScopes[i], key); ok {
			return v, allScopes[i], true
		}
	}
	return nil, vars.Global, false
}

func (s *fakeStore) GetIn(scope vars.Scope, key string) (any, bool) {
	m := s.values[scope]
	if m == nil {
		return nil, false
	}
	v, ok := m[key]
	return v, ok
}

func (s *fakeStore) Set(scope vars.Scope, key string, value any) {
	if s.values[scope] == nil {
		s.values[scope] = map[string]any{}
	}
	if _, seen := s.values[scope][key]; !seen {
		s.order[scope] = append(s.order[scope], key)
	}
	s.values[scope][key] = value
	s.changes[scope] = append(s.changes[scope], vars.Change{Key: key, Value: value})
}

func (s *fakeStore) Unset(scope vars.Scope, key string) {
	delete(s.values[scope], key)
	kept := s.order[scope][:0]
	for _, k := range s.order[scope] {
		if k != key {
			kept = append(kept, k)
		}
	}
	s.order[scope] = kept
	s.changes[scope] = append(s.changes[scope], vars.Change{Key: key, Unset: true})
}

func (s *fakeStore) Clear(scope vars.Scope) {
	for _, k := range append([]string(nil), s.order[scope]...) {
		s.Unset(scope, k)
	}
}

func (s *fakeStore) Keys(scope vars.Scope) []string {
	return append([]string(nil), s.order[scope]...)
}

var refPattern = regexp.MustCompile(`\{\{([^{}]+)\}\}`)

func (s *fakeStore) Replace(template string) string {
	return refPattern.ReplaceAllStringFunc(template, func(ref string) string {
		name := ref[2 : len(ref)-2]
		if name == "$timestamp" {
			return "1700000000"
		}
		if v, _, ok := s.Get(name); ok {
			return toString(v)
		}
		return ref
	})
}

func (s *fakeStore) Changes() map[vars.Scope][]vars.Change { return s.changes }

// fakeSender records what pm.sendRequest asked for and replies with a canned
// response.
type fakeSender struct {
	mu       sync.Mutex
	sent     []*model.Request
	respond  func(*model.Request) (*httpx.Response, error)
	delay    time.Duration
	failWith error
}

func (f *fakeSender) Send(ctx context.Context, req *model.Request, _ httpx.Options) (*httpx.Response, error) {
	if f.delay > 0 {
		select {
		case <-time.After(f.delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	f.mu.Lock()
	f.sent = append(f.sent, req)
	f.mu.Unlock()
	if f.failWith != nil {
		return nil, f.failWith
	}
	if f.respond != nil {
		return f.respond(req)
	}
	return jsonResponse(200, `{"ok":true}`), nil
}

func (f *fakeSender) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.sent)
}

func jsonResponse(code int, body string) *httpx.Response {
	return &httpx.Response{
		StatusCode: code,
		Status:     statusText(code),
		Headers:    []model.KV{{Key: "Content-Type", Value: "application/json"}},
		Body:       []byte(body),
		Timings:    httpx.Timings{Total: 12 * time.Millisecond},
		Size:       httpx.Size{Headers: 64, Body: int64(len(body))},
	}
}

func statusText(code int) string {
	if text := http.StatusText(code); text != "" {
		return text
	}
	return fmt.Sprintf("Status %d", code)
}

// fakeJar is a URL-keyed cookie store standing in for WS2's jar.
type fakeJar struct {
	cookies map[string][]model.Cookie
	err     error
}

func newJar() *fakeJar { return &fakeJar{cookies: map[string][]model.Cookie{}} }

func jarKey(rawURL string) string {
	u := strings.TrimPrefix(strings.TrimPrefix(rawURL, "https://"), "http://")
	if i := strings.IndexAny(u, "/?#"); i >= 0 {
		u = u[:i]
	}
	return u
}

func (j *fakeJar) Cookies(rawURL string) ([]model.Cookie, error) {
	if j.err != nil {
		return nil, j.err
	}
	return j.cookies[jarKey(rawURL)], nil
}

func (j *fakeJar) SetCookie(rawURL string, c model.Cookie) error {
	if j.err != nil {
		return j.err
	}
	key := jarKey(rawURL)
	for i, existing := range j.cookies[key] {
		if existing.Name == c.Name {
			j.cookies[key][i] = c
			return nil
		}
	}
	j.cookies[key] = append(j.cookies[key], c)
	return nil
}

func (j *fakeJar) UnsetCookie(rawURL, name string) error {
	if j.err != nil {
		return j.err
	}
	key := jarKey(rawURL)
	kept := j.cookies[key][:0]
	for _, c := range j.cookies[key] {
		if c.Name != name {
			kept = append(kept, c)
		}
	}
	j.cookies[key] = kept
	return nil
}

func (j *fakeJar) ClearCookies(rawURL string) error {
	if j.err != nil {
		return j.err
	}
	delete(j.cookies, jarKey(rawURL))
	return nil
}

// Compile-time checks that the fakes satisfy the contracts WS2 implements.
var (
	_ vars.Store   = (*fakeStore)(nil)
	_ httpx.Sender = (*fakeSender)(nil)
	_ CookieAccess = (*fakeJar)(nil)
)
