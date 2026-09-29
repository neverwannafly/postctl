package model

import (
	"fmt"
	"reflect"
	"testing"
	"time"
)

// The tests below drive clone correctness by reflection so that adding a field
// to any model type without cloning it deep fails a test:
//
//  1. fill populates every exported field (pointers, slices, maps, nested
//     Items, Extra maps holding nested maps and slices) with distinct values;
//  2. Clone must be DeepEqual to the original;
//  3. noAlias walks both values and fails if any non-empty pointer, slice or
//     map shares memory;
//  4. scribble mutates every reachable field of the clone in place, and the
//     original must still equal a freshly built twin.

var (
	timeType    = reflect.TypeOf(time.Time{})
	itemType    = reflect.TypeOf((*Item)(nil)).Elem()
	requestType = reflect.TypeOf(Request{})
	folderType  = reflect.TypeOf(Folder{})
	idType      = reflect.TypeOf(ID(""))
	secretType  = reflect.TypeOf(SecretRef(""))
	anyMapType  = reflect.TypeOf(map[string]any{})
)

type filler struct {
	n     int
	depth map[reflect.Type]int
}

func newFiller() *filler { return &filler{depth: map[reflect.Type]int{}} }

func (f *filler) str() string { f.n++; return fmt.Sprintf("v%d", f.n) }

func (f *filler) fill(v reflect.Value) {
	switch v.Type() {
	case timeType:
		f.n++
		v.Set(reflect.ValueOf(time.Date(2026, 1, 1, 0, 0, f.n, 0, time.UTC)))
		return
	case anyMapType:
		v.Set(reflect.ValueOf(map[string]any{
			f.str(): f.str(),
			f.str(): map[string]any{f.str(): []any{f.str(), map[string]any{f.str(): f.str()}}},
			f.str(): []string{f.str(), f.str()},
		}))
		return
	}
	switch v.Kind() {
	case reflect.String:
		v.SetString(f.str())
	case reflect.Bool:
		v.SetBool(true)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		f.n++
		v.SetInt(int64(f.n))
	case reflect.Ptr:
		et := v.Type().Elem()
		if (et == requestType || et == folderType) && f.depth[et] >= 2 {
			return // bound recursion (Request -> Examples -> Request, Folder -> Items)
		}
		f.depth[et]++
		defer func() { f.depth[et]-- }()
		v.Set(reflect.New(et))
		f.fill(v.Elem())
	case reflect.Slice:
		if v.Type().Elem() == itemType {
			var items []Item
			if f.depth[requestType] < 2 {
				r := &Request{}
				f.depth[requestType]++
				f.fill(reflect.ValueOf(r).Elem())
				f.depth[requestType]--
				items = append(items, r)
			}
			if f.depth[folderType] < 2 {
				fo := &Folder{}
				f.depth[folderType]++
				f.fill(reflect.ValueOf(fo).Elem())
				f.depth[folderType]--
				items = append(items, fo)
			}
			v.Set(reflect.ValueOf(items))
			return
		}
		et := v.Type().Elem()
		if (et == requestType || et == folderType) && f.depth[et] >= 2 {
			return
		}
		s := reflect.MakeSlice(v.Type(), 2, 2)
		for i := 0; i < 2; i++ {
			f.fill(s.Index(i))
		}
		v.Set(s)
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			if v.Type().Field(i).IsExported() {
				f.fill(v.Field(i))
			}
		}
	default:
		panic(fmt.Sprintf("fill: unsupported kind %s (%s)", v.Kind(), v.Type()))
	}
}

func filled[T any]() *T {
	x := new(T)
	newFiller().fill(reflect.ValueOf(x).Elem())
	return x
}

// noAlias fails when a and b share memory through any pointer, slice or map.
func noAlias(t *testing.T, path string, a, b reflect.Value) {
	t.Helper()
	if a.Type() == timeType {
		return
	}
	switch a.Kind() {
	case reflect.Ptr:
		if a.IsNil() || b.IsNil() {
			return
		}
		if a.Pointer() == b.Pointer() {
			t.Errorf("%s: pointer aliased", path)
			return
		}
		noAlias(t, path, a.Elem(), b.Elem())
	case reflect.Slice:
		if a.Len() == 0 || b.Len() == 0 {
			return
		}
		if a.Pointer() == b.Pointer() {
			t.Errorf("%s: slice aliased", path)
			return
		}
		for i := 0; i < a.Len() && i < b.Len(); i++ {
			noAlias(t, fmt.Sprintf("%s[%d]", path, i), a.Index(i), b.Index(i))
		}
	case reflect.Map:
		if a.Len() == 0 || b.Len() == 0 {
			return
		}
		if a.Pointer() == b.Pointer() {
			t.Errorf("%s: map aliased", path)
			return
		}
		for _, k := range a.MapKeys() {
			if bv := b.MapIndex(k); bv.IsValid() {
				noAlias(t, fmt.Sprintf("%s[%v]", path, k), a.MapIndex(k), bv)
			}
		}
	case reflect.Interface:
		if a.IsNil() || b.IsNil() {
			return
		}
		noAlias(t, path, a.Elem(), b.Elem())
	case reflect.Struct:
		for i := 0; i < a.NumField(); i++ {
			if a.Type().Field(i).IsExported() {
				noAlias(t, path+"."+a.Type().Field(i).Name, a.Field(i), b.Field(i))
			}
		}
	}
}

// scribble mutates every value reachable from v, in place.
func scribble(v reflect.Value) {
	if v.Type() == timeType {
		v.Set(reflect.ValueOf(time.Date(1999, 1, 1, 0, 0, 0, 0, time.UTC)))
		return
	}
	switch v.Kind() {
	case reflect.String:
		v.SetString(v.String() + "!")
	case reflect.Bool:
		v.SetBool(!v.Bool())
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		v.SetInt(v.Int() + 1000)
	case reflect.Ptr:
		if !v.IsNil() {
			scribble(v.Elem())
		}
	case reflect.Slice:
		for i := 0; i < v.Len(); i++ {
			scribble(v.Index(i))
		}
	case reflect.Map:
		for _, k := range v.MapKeys() {
			e := reflect.New(v.Type().Elem()).Elem()
			e.Set(v.MapIndex(k))
			scribble(e)
			v.SetMapIndex(k, e)
		}
	case reflect.Interface:
		if v.IsNil() {
			return
		}
		e := reflect.New(v.Elem().Type()).Elem()
		e.Set(v.Elem())
		scribble(e)
		v.Set(e)
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			if v.Type().Field(i).IsExported() {
				scribble(v.Field(i))
			}
		}
	}
}

// deepCloneCase checks Clone for one type.
func deepCloneCase[T any](t *testing.T, name string, clone func(*T) *T) {
	t.Helper()
	t.Run(name, func(t *testing.T) {
		orig, twin := filled[T](), filled[T]()
		if !reflect.DeepEqual(orig, twin) {
			t.Fatal("filler is not deterministic")
		}
		c := clone(orig)
		if !reflect.DeepEqual(orig, c) {
			t.Fatalf("clone differs from original:\n orig=%+v\nclone=%+v", orig, c)
		}
		noAlias(t, name, reflect.ValueOf(orig), reflect.ValueOf(c))
		scribble(reflect.ValueOf(c))
		if reflect.DeepEqual(orig, c) {
			t.Fatal("scribble changed nothing; test is vacuous")
		}
		if !reflect.DeepEqual(orig, twin) {
			t.Fatalf("mutating the clone changed the original")
		}
	})
}

func TestCloneDeepNoAliasing(t *testing.T) {
	deepCloneCase(t, "Request", (*Request).Clone)
	deepCloneCase(t, "Folder", (*Folder).Clone)
	deepCloneCase(t, "Collection", (*Collection).Clone)
	deepCloneCase(t, "Environment", (*Environment).Clone)
	deepCloneCase(t, "Globals", (*Globals).Clone)
	deepCloneCase(t, "Auth", (*Auth).Clone)
	deepCloneCase(t, "OAuth2Config", (*OAuth2Config).Clone)
	deepCloneCase(t, "Script", (*Script).Clone)
	deepCloneCase(t, "Body", func(b *Body) *Body { c := b.Clone(); return &c })
	deepCloneCase(t, "URL", func(u *URL) *URL { c := u.Clone(); return &c })
	deepCloneCase(t, "Scripts", func(s *Scripts) *Scripts { c := s.Clone(); return &c })
	deepCloneCase(t, "RequestSettings", func(s *RequestSettings) *RequestSettings { c := s.Clone(); return &c })
	deepCloneCase(t, "Example", func(e *Example) *Example { c := e.Clone(); return &c })
}

func TestCloneNilAndEmpty(t *testing.T) {
	if (*Request)(nil).Clone() != nil || (*Folder)(nil).Clone() != nil || (*Collection)(nil).Clone() != nil ||
		(*Environment)(nil).Clone() != nil || (*Globals)(nil).Clone() != nil || (*Auth)(nil).Clone() != nil ||
		(*Script)(nil).Clone() != nil || CloneItem(nil) != nil {
		t.Fatal("nil must clone to nil")
	}
	// Zero values, and nil vs empty slices/maps, are preserved.
	r := &Request{ID: "r", Name: "n", Kind: KindHTTP, Headers: []KV{}, Extra: map[string]any{}}
	c := r.Clone()
	if !reflect.DeepEqual(r, c) || c.Headers == nil || c.Extra == nil || c.Examples != nil {
		t.Fatalf("empty/nil not preserved: %#v", c)
	}
}

func TestCloneKeepsIDsAndSecretRefs(t *testing.T) {
	c := filled[Collection]()
	cl := c.Clone()
	if !reflect.DeepEqual(idsAndRefs(c), idsAndRefs(cl)) {
		t.Fatal("Clone must keep IDs and secret refs")
	}
	if len(idsAndRefs(c)) == 0 {
		t.Fatal("vacuous")
	}
}

// idsAndRefs collects every ID and SecretRef value reachable from v, in walk
// order.
func idsAndRefs(x any) []string {
	var out []string
	var walk func(v reflect.Value)
	walk = func(v reflect.Value) {
		if v.Type() == timeType {
			return
		}
		switch v.Kind() {
		case reflect.String:
			if v.Type() == idType || v.Type() == secretType {
				out = append(out, v.Type().Name()+":"+v.String())
			}
		case reflect.Ptr, reflect.Interface:
			if !v.IsNil() {
				walk(v.Elem())
			}
		case reflect.Slice:
			for i := 0; i < v.Len(); i++ {
				walk(v.Index(i))
			}
		case reflect.Struct:
			for i := 0; i < v.NumField(); i++ {
				if v.Type().Field(i).IsExported() {
					walk(v.Field(i))
				}
			}
		}
	}
	walk(reflect.ValueOf(x))
	return out
}

func checkFresh(t *testing.T, orig, fresh any) {
	t.Helper()
	before := idsAndRefs(orig)
	seen := map[string]bool{}
	for _, s := range before {
		seen[s] = true
	}
	for _, s := range idsAndRefs(fresh) {
		switch {
		case len(s) > 3 && s[:3] == "ID:":
			if seen[s] {
				t.Errorf("CloneFresh reused %s", s)
			}
		case len(s) > 10 && s[:10] == "SecretRef:":
			if s != "SecretRef:" {
				t.Errorf("CloneFresh kept a secret ref: %s", s)
			}
		}
	}
	// IDs in the fresh copy are unique among themselves.
	ids := map[string]int{}
	for _, s := range idsAndRefs(fresh) {
		if len(s) > 3 && s[:3] == "ID:" {
			ids[s]++
		}
	}
	for id, n := range ids {
		if n > 1 {
			t.Errorf("duplicate fresh id %s x%d", id, n)
		}
	}
	if len(ids) == 0 {
		t.Error("no IDs found; vacuous")
	}
}

func TestCloneFresh(t *testing.T) {
	t.Run("Request", func(t *testing.T) {
		orig, twin := filled[Request](), filled[Request]()
		fresh := orig.CloneFresh()
		checkFresh(t, orig, fresh)
		noAlias(t, "Request", reflect.ValueOf(orig), reflect.ValueOf(fresh))
		scribble(reflect.ValueOf(fresh))
		if !reflect.DeepEqual(orig, twin) {
			t.Fatal("original changed")
		}
	})
	t.Run("Folder", func(t *testing.T) {
		orig, twin := filled[Folder](), filled[Folder]()
		fresh := orig.CloneFresh()
		checkFresh(t, orig, fresh)
		noAlias(t, "Folder", reflect.ValueOf(orig), reflect.ValueOf(fresh))
		if !reflect.DeepEqual(orig, twin) {
			t.Fatal("original changed")
		}
	})
	t.Run("Collection", func(t *testing.T) {
		orig, twin := filled[Collection](), filled[Collection]()
		fresh := orig.CloneFresh()
		checkFresh(t, orig, fresh)
		noAlias(t, "Collection", reflect.ValueOf(orig), reflect.ValueOf(fresh))
		if !reflect.DeepEqual(orig, twin) {
			t.Fatal("original changed")
		}
		// Content other than IDs, refs and script file names is unchanged.
		if fresh.Name != orig.Name || len(fresh.Items) != len(orig.Items) {
			t.Fatal("content must be preserved")
		}
	})
	t.Run("Environment", func(t *testing.T) {
		orig, twin := filled[Environment](), filled[Environment]()
		fresh := orig.CloneFresh()
		checkFresh(t, orig, fresh)
		if fresh.Variables[0].Value != orig.Variables[0].Value {
			t.Fatal("secret values must be kept in memory")
		}
		if !reflect.DeepEqual(orig, twin) {
			t.Fatal("original changed")
		}
	})
	t.Run("CloneItemFresh", func(t *testing.T) {
		r := &Request{ID: "a", Name: "n", Kind: KindHTTP}
		f := &Folder{ID: "b", Items: []Item{&Request{ID: "c"}}}
		for _, it := range []Item{r, f} {
			n := CloneItemFresh(it)
			if n.ItemID() == it.ItemID() || n == it {
				t.Fatal("fresh item must differ")
			}
		}
		if f.Items[0].ItemID() != "c" {
			t.Fatal("original changed")
		}
	})
}

func TestCloneFreshKeepsOAuthTokenValues(t *testing.T) {
	r := &Request{ID: "r", Auth: &Auth{Type: AuthOAuth2, OAuth2: &OAuth2Config{
		Token: &OAuth2Token{AccessToken: "a", RefreshToken: "r", Ref: "oauth2/r"},
	}}}
	f := r.CloneFresh()
	tok := f.Auth.OAuth2.Token
	if tok.Ref != "" || tok.AccessToken != "a" || tok.RefreshToken != "r" {
		t.Fatalf("token: %+v", tok)
	}
	if r.Auth.OAuth2.Token.Ref != "oauth2/r" {
		t.Fatal("original token ref changed")
	}
}
