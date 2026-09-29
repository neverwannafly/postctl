package history

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/shubham/postctl/internal/model"
)

func entry(i int, url string) Entry {
	return Entry{
		RequestID:   model.ID(fmt.Sprintf("req-%d", i%3)),
		RequestName: fmt.Sprintf("Request %d", i),
		Request:     RequestSnapshot{Method: "GET", URL: url, Body: "body"},
		Status:      200,
		Response:    &ResponseSnapshot{Body: strings.Repeat("x", 100)},
	}
}

func TestAppendListGet(t *testing.T) {
	log, err := Open(t.TempDir(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for i := 0; i < 5; i++ {
		e, err := log.Append(entry(i, fmt.Sprintf("https://example.test/%d", i)))
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, e.ID)
	}
	list, err := log.List(Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 5 || list[0].ID != ids[4] || list[4].ID != ids[0] {
		t.Fatalf("want newest first, got %d entries", len(list))
	}
	if list[0].Response != nil || list[0].Request.Body != "" {
		t.Fatal("List must return summaries without bodies")
	}
	full, err := log.Get(ids[2])
	if err != nil {
		t.Fatal(err)
	}
	if full.Request.Body != "body" || full.Response == nil || len(full.Response.Body) != 100 {
		t.Fatalf("Get should return the full entry: %+v", full)
	}
	if _, err := log.Get("missing"); err == nil {
		t.Fatal("expected ErrNotFound")
	}

	if got, _ := log.List(Filter{RequestID: "req-1"}); len(got) != 2 {
		t.Fatalf("filter by request: got %d", len(got))
	}
	if got, _ := log.List(Filter{Query: "example.test/3"}); len(got) != 1 {
		t.Fatalf("filter by query: got %d", len(got))
	}
	if got, _ := log.List(Filter{Limit: 2, Offset: 1}); len(got) != 2 || got[0].ID != ids[3] {
		t.Fatalf("limit/offset: %+v", got)
	}
	if got, _ := log.List(Filter{Since: time.Now().Add(time.Hour)}); len(got) != 0 {
		t.Fatalf("since: got %d", len(got))
	}

	// A second Log over the same directory sees the same entries.
	again, err := Open(log.Dir(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if n, _ := again.Len(); n != 5 {
		t.Fatalf("reopen: got %d entries", n)
	}

	if err := log.Clear(); err != nil {
		t.Fatal(err)
	}
	if n, _ := log.Len(); n != 0 {
		t.Fatalf("after Clear: %d entries", n)
	}
	if n, _ := again.Len(); n != 0 {
		t.Fatalf("other handle after Clear: %d entries", n)
	}
}

func TestBoundedByEntries(t *testing.T) {
	dir := t.TempDir()
	log, err := Open(dir, Options{MaxEntries: 20})
	if err != nil {
		t.Fatal(err)
	}
	var last Entry
	for i := 0; i < 100; i++ {
		last, err = log.Append(entry(i, "https://example.test/"))
		if err != nil {
			t.Fatal(err)
		}
	}
	n, _ := log.Len()
	if n > 22 {
		t.Fatalf("log holds %d entries, want at most MaxEntries+slack (22)", n)
	}
	list, _ := log.List(Filter{})
	if len(list) != 20 || list[0].ID != last.ID {
		t.Fatalf("List must return the newest 20, got %d", len(list))
	}
	data, err := os.ReadFile(filepath.Join(dir, fileName))
	if err != nil {
		t.Fatal(err)
	}
	if lines := strings.Count(string(data), "\n"); lines != n {
		t.Fatalf("file has %d lines, index has %d", lines, n)
	}
	// Every kept entry is still readable after the rewrite.
	for _, e := range list {
		if _, err := log.Get(e.ID); err != nil {
			t.Fatalf("Get after compaction: %v", err)
		}
	}
}

func TestBoundedByBytes(t *testing.T) {
	dir := t.TempDir()
	log, err := Open(dir, Options{MaxBytes: 8 << 10})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 200; i++ {
		e := entry(i, "https://example.test/")
		e.Response.Body = strings.Repeat("y", 500)
		if _, err := log.Append(e); err != nil {
			t.Fatal(err)
		}
	}
	st, err := os.Stat(filepath.Join(dir, fileName))
	if err != nil {
		t.Fatal(err)
	}
	if st.Size() > 8<<10 {
		t.Fatalf("file is %d bytes, bound is %d", st.Size(), 8<<10)
	}
	if n, _ := log.Len(); n == 0 {
		t.Fatal("compaction dropped everything")
	}
}

func TestOversizedEntryIsKept(t *testing.T) {
	log, err := Open(t.TempDir(), Options{MaxBytes: 200})
	if err != nil {
		t.Fatal(err)
	}
	e, err := log.Append(entry(1, "https://example.test/"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := log.Get(e.ID); err != nil {
		t.Fatalf("the newest entry must survive compaction: %v", err)
	}
}

func TestSharedAcrossHandles(t *testing.T) {
	dir := t.TempDir()
	a, _ := Open(dir, Options{MaxEntries: 10})
	b, _ := Open(dir, Options{MaxEntries: 10})
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(l *Log) {
			defer wg.Done()
			for j := 0; j < 30; j++ {
				if _, err := l.Append(entry(j, "https://example.test/")); err != nil {
					t.Error(err)
					return
				}
			}
		}([]*Log{a, b}[i])
	}
	wg.Wait()
	la, _ := a.List(Filter{})
	lb, _ := b.List(Filter{})
	if len(la) != 10 || len(lb) != 10 || la[0].ID != lb[0].ID {
		t.Fatalf("handles disagree: %d vs %d", len(la), len(lb))
	}
	for _, e := range la {
		if _, err := a.Get(e.ID); err != nil {
			t.Fatal(err)
		}
	}
}

func TestCorruptLinesAreSkipped(t *testing.T) {
	dir := t.TempDir()
	log, _ := Open(dir, Options{})
	e, _ := log.Append(entry(1, "https://example.test/"))
	f, err := os.OpenFile(filepath.Join(dir, fileName), os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString("not json\n{\"id\":\"partial")
	_ = f.Close()
	again, err := Open(dir, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if n, _ := again.Len(); n != 1 {
		t.Fatalf("got %d entries, want the one valid line", n)
	}
	if _, err := again.Get(e.ID); err != nil {
		t.Fatal(err)
	}
}

func TestRedactor(t *testing.T) {
	const (
		secret = "s3cr3t-value-123"
		short  = "ab"
	)
	r := NewRedactor([]string{secret, short}, "X-Custom-Key")

	headers := r.Headers([]model.KV{
		{Key: "Authorization", Value: "Bearer " + secret},
		{Key: "cookie", Value: "sid=1"},
		{Key: "Set-Cookie", Value: "sid=2"},
		{Key: "X-Api-Key", Value: "abc"},
		{Key: "X-Custom-Key", Value: "custom"},
		{Key: "X-Note", Value: "prefix " + secret},
		{Key: "X-Short", Value: short},
		{Key: "Accept", Value: "application/json"},
		{Key: "X-Empty-Token", Value: ""},
	})
	want := map[string]string{
		"Authorization": Mask,
		"cookie":        Mask,
		"Set-Cookie":    Mask,
		"X-Api-Key":     Mask,
		"X-Custom-Key":  Mask,
		"X-Note":        "prefix " + Mask,
		"X-Short":       Mask,
		"Accept":        "application/json",
		"X-Empty-Token": "",
	}
	for _, h := range headers {
		if h.Value != want[h.Key] {
			t.Errorf("header %s = %q, want %q", h.Key, h.Value, want[h.Key])
		}
	}

	urls := map[string]string{
		"https://u:pw@example.test/a?access_token=zzz&q=1#frag": "https://u:" + Mask + "@example.test/a?access_token=" + Mask + "&q=1#frag",
		"https://example.test/" + secret + "?x=" + secret:       "https://example.test/" + Mask + "?x=" + Mask,
		"https://example.test/a?key=1&api_key=2&name=bob":       "https://example.test/a?key=" + Mask + "&api_key=" + Mask + "&name=bob",
		"https://example.test/a?X-Custom-Key=1":                 "https://example.test/a?X-Custom-Key=" + Mask,
		"https://example.test/plain":                            "https://example.test/plain",
	}
	for in, want := range urls {
		if got := r.URL(in); got != want {
			t.Errorf("URL(%q)\n got %q\nwant %q", in, got, want)
		}
	}

	body := r.Body("application/json", `{"user":"bob","password":"hunter2","note":"has `+secret+`","n":1}`)
	if strings.Contains(body, "hunter2") || strings.Contains(body, secret) || !strings.Contains(body, `"user":"bob"`) {
		t.Errorf("json body not redacted correctly: %s", body)
	}
	form := r.Body("application/x-www-form-urlencoded", "grant_type=password&client_secret=abc&user=bob&note="+secret)
	if form != "grant_type=password&client_secret="+Mask+"&user=bob&note="+Mask {
		t.Errorf("form body: %s", form)
	}
}

func TestSnapshotBody(t *testing.T) {
	r := NewRedactor([]string{"topsecretvalue"})
	text, cut, enc := r.SnapshotBody("text/plain", []byte(strings.Repeat("a", 100)+"topsecretvalue"+strings.Repeat("b", 100)), 110)
	if !cut || enc != "" || strings.Contains(text, "topsecret") || len(text) > 110 {
		t.Fatalf("cut=%v enc=%q text=%q", cut, enc, text)
	}
	// A cap in the middle of a multi-byte rune must not produce invalid UTF-8.
	text, cut, _ = r.SnapshotBody("text/plain", []byte(strings.Repeat("é", 10)), 5)
	if !cut || text != "éé" {
		t.Fatalf("got %q", text)
	}
	text, _, enc = r.SnapshotBody("application/octet-stream", []byte{0xff, 0xfe, 0x00}, 10)
	if enc != "base64" || text == "" {
		t.Fatalf("binary: enc=%q text=%q", enc, text)
	}
}
