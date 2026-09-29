package model

import (
	"errors"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

var uuidRE = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

func TestNewIDFormatAndOrder(t *testing.T) {
	prev := ID("")
	for i := 0; i < 5000; i++ {
		id := NewID()
		if !uuidRE.MatchString(string(id)) {
			t.Fatalf("bad uuid %q", id)
		}
		if id[14] != '7' {
			t.Fatalf("not v7: %q", id)
		}
		if id <= prev {
			t.Fatalf("not strictly increasing: %q after %q", id, prev)
		}
		prev = id
	}
}

func TestDeriveIDStable(t *testing.T) {
	a := DeriveID("p", "x")
	if a != DeriveID("p", "x") || a == DeriveID("p", "y") || a == DeriveID("q", "x") {
		t.Fatal("DeriveID must be deterministic and input-sensitive")
	}
	if !uuidRE.MatchString(string(a)) {
		t.Fatalf("bad uuid %q", a)
	}
}

func sampleCollection() *Collection {
	c := &Collection{ID: "c", Name: "C"}
	c.Items = []Item{
		&Request{ID: "r1", Name: "R1", Kind: KindHTTP, Method: "GET"},
		&Folder{ID: "f1", Name: "F1", Items: []Item{
			&Request{ID: "r2", Name: "R2", Kind: KindHTTP, Method: "GET"},
			&Folder{ID: "f2", Name: "F2", Items: []Item{
				&Request{ID: "r3", Name: "R3", Kind: KindHTTP, Method: "GET"},
			}},
		}},
	}
	return c
}

func ids(items []Item) []ID {
	var out []ID
	for _, it := range items {
		out = append(out, it.ItemID())
	}
	return out
}

func TestTreeWalkFindRequests(t *testing.T) {
	c := sampleCollection()
	var order []ID
	_ = c.Walk(func(it Item, _ Container, _ int) error { order = append(order, it.ItemID()); return nil })
	if want := []ID{"r1", "f1", "r2", "f2", "r3"}; !reflect.DeepEqual(order, want) {
		t.Fatalf("walk order %v want %v", order, want)
	}
	if got := len(c.Requests()); got != 3 {
		t.Fatalf("requests %d", got)
	}
	it, parent, idx, ok := c.Find("r3")
	if !ok || it.ItemID() != "r3" || parent.ContainerID() != "f2" || idx != 0 {
		t.Fatalf("find: %v %v %d %v", it, parent, idx, ok)
	}
	if _, _, _, ok := c.Find("nope"); ok {
		t.Fatal("found missing")
	}
	// SkipChildren
	var n int
	_ = c.Walk(func(it Item, _ Container, _ int) error {
		n++
		if it.ItemID() == "f1" {
			return SkipChildren
		}
		return nil
	})
	if n != 2 {
		t.Fatalf("skip children visited %d", n)
	}
}

func TestTreeInsertRemoveMove(t *testing.T) {
	c := sampleCollection()
	if err := c.Insert("f1", 0, &Request{ID: "r4", Name: "R4", Kind: KindHTTP}); err != nil {
		t.Fatal(err)
	}
	f1, _ := c.Container("f1")
	if got := ids(f1.Children()); !reflect.DeepEqual(got, []ID{"r4", "r2", "f2"}) {
		t.Fatalf("insert: %v", got)
	}
	if err := c.Insert("", 99, &Request{ID: "r4"}); !errors.Is(err, ErrDuplicateID) {
		t.Fatalf("dup: %v", err)
	}
	if err := c.Insert("missing", 0, &Request{ID: "x"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing parent: %v", err)
	}
	if err := c.Move("r1", "f2", 0); err != nil {
		t.Fatal(err)
	}
	if got := ids(c.Items); !reflect.DeepEqual(got, []ID{"f1"}) {
		t.Fatalf("root after move: %v", got)
	}
	f2, _ := c.Container("f2")
	if got := ids(f2.Children()); !reflect.DeepEqual(got, []ID{"r1", "r3"}) {
		t.Fatalf("f2: %v", got)
	}
	if err := c.Move("f1", "f2", 0); !errors.Is(err, ErrCycle) {
		t.Fatalf("cycle: %v", err)
	}
	if err := c.Move("f1", "f1", 0); !errors.Is(err, ErrCycle) {
		t.Fatalf("self cycle: %v", err)
	}
	if _, err := c.Remove("f2"); err != nil {
		t.Fatal(err)
	}
	if _, _, _, ok := c.Find("r3"); ok {
		t.Fatal("subtree not removed")
	}
	if _, err := c.Remove("f2"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("remove twice: %v", err)
	}
}

func TestParseURL(t *testing.T) {
	u := ParseURL("https://{{host}}:8080/v1/:id/x/:id/:name?a=1&flag&b=&c=x=y#frag")
	if u.Raw != "https://{{host}}:8080/v1/:id/x/:id/:name?a=1&flag&b=&c=x=y#frag" {
		t.Fatal("raw must be verbatim")
	}
	wantQ := []KV{{Key: "a", Value: "1"}, {Key: "flag"}, {Key: "b"}, {Key: "c", Value: "x=y"}}
	if !reflect.DeepEqual(u.Query, wantQ) {
		t.Fatalf("query %#v", u.Query)
	}
	if len(u.PathVars) != 2 || u.PathVars[0].Key != "id" || u.PathVars[1].Key != "name" {
		t.Fatalf("path vars %#v", u.PathVars)
	}
	if pv := ParseURL("localhost:8080").PathVars; len(pv) != 0 {
		t.Fatalf("port colon is not a path var: %v", pv)
	}
}

func TestURLSyncRaw(t *testing.T) {
	u := ParseURL("https://x.test/p?a=1&b=2#top")
	u.Query[0].Disabled = true
	u.Query = append(u.Query, KV{Key: "c", Value: "3"})
	u.SyncRaw()
	if u.Raw != "https://x.test/p?b=2&c=3#top" {
		t.Fatalf("raw %q", u.Raw)
	}
	if len(u.Query) != 3 {
		t.Fatal("disabled params must be retained")
	}
}

func TestScriptExecRoundTrip(t *testing.T) {
	for _, lines := range [][]string{{"a"}, {"a", "b"}, {"a", ""}, {"", "a"}, {"  indented", "\ttab"}} {
		if got := ScriptFromExec(lines).Exec(); !reflect.DeepEqual(got, lines) {
			t.Fatalf("exec %q -> %q", lines, got)
		}
	}
	if !(&Script{Source: " \n"}).Empty() || (&Script{Source: "x"}).Empty() || !(*Script)(nil).Empty() {
		t.Fatal("Empty")
	}
}

func TestAuthIsInherit(t *testing.T) {
	var a *Auth
	if !a.IsInherit() || !(&Auth{Type: AuthInherit}).IsInherit() || (&Auth{Type: AuthNone}).IsInherit() {
		t.Fatal("IsInherit")
	}
}

func TestValidate(t *testing.T) {
	c := sampleCollection()
	if err := c.Validate(); err != nil {
		t.Fatalf("valid: %v", err)
	}
	c.Items = append(c.Items,
		&Request{ID: "r1", Name: "dup", Kind: KindHTTP},
		&Request{Name: "noid", Kind: KindHTTP},
		&Request{ID: "r9", Name: "nokind"},
		&Request{ID: "r10", Name: "badbody", Kind: KindHTTP, Body: Body{Mode: "weird"}},
		&Request{ID: "r11", Name: "badauth", Kind: KindHTTP, Auth: &Auth{Type: "nope"}},
	)
	err := c.Validate()
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("want ValidationError, got %v", err)
	}
	joined := strings.Join(ve.Problems, "\n")
	for _, want := range []string{"duplicate id", "missing id", "missing kind", "unknown body mode", "unknown auth type"} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing problem %q in:\n%s", want, joined)
		}
	}
}

func TestEnsureIDs(t *testing.T) {
	w := &Workspace{Name: "w", Collections: []*Collection{{Name: "c", Items: []Item{&Folder{Name: "f", Items: []Item{&Request{Name: "r", Kind: KindHTTP}}}}}}, Environments: []*Environment{{Name: "e"}}, Globals: &Globals{}}
	w.EnsureIDs()
	if err := w.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestOAuth2TokenExpired(t *testing.T) {
	var nilTok *OAuth2Token
	if nilTok.Expired(timeZero()) {
		t.Fatal("nil token is not expired")
	}
}
