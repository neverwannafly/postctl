package model

// Deep copies.
//
// Clone methods return copies that share no mutable memory with the original
// (no slice, map or pointer is aliased), and keep every ID and SecretRef.
// CloneFresh methods additionally assign new IDs and clear secret refs so the
// copy is an independent node, which is what "duplicate" actions want.
//
// Secret values are only in memory after Store.Hydrate. CloneFresh keeps them
// in the copy (Variable.Value, OAuth2Token strings) so that saving the copy
// stores them under new refs; hydrate first if the copy must keep its secrets.

// Clone returns a deep copy of the request. Nil-safe.
func (r *Request) Clone() *Request {
	if r == nil {
		return nil
	}
	c := *r
	c.URL = r.URL.Clone()
	c.Headers = cloneKVs(r.Headers)
	c.Body = r.Body.Clone()
	c.Auth = r.Auth.Clone()
	c.Scripts = r.Scripts.Clone()
	c.Settings = r.Settings.Clone()
	c.Examples = cloneExamples(r.Examples)
	c.Extra = cloneAnyMap(r.Extra)
	return &c
}

// CloneFresh is [Request.Clone] with new IDs (request, examples, scripts) and
// cleared secret refs.
func (r *Request) CloneFresh() *Request {
	c := r.Clone()
	if c != nil {
		c.freshen()
	}
	return c
}

func (r *Request) freshen() {
	r.ID = NewID()
	freshenAuth(r.Auth)
	freshenVars(r.URL.PathVars)
	freshenScripts(&r.Scripts)
	for i := range r.Examples {
		e := &r.Examples[i]
		if e.ID != "" {
			e.ID = NewID()
		}
		if o := e.OriginalRequest; o != nil {
			hadID := o.ID != "" // Postman's embedded originalRequest usually has none
			o.freshen()
			if !hadID {
				o.ID = ""
			}
		}
	}
}

// Clone returns a deep copy of the folder and its subtree. Nil-safe.
func (f *Folder) Clone() *Folder {
	if f == nil {
		return nil
	}
	c := *f
	c.Auth = f.Auth.Clone()
	c.Variables = cloneVars(f.Variables)
	c.Scripts = f.Scripts.Clone()
	c.Extra = cloneAnyMap(f.Extra)
	c.Items = cloneItems(f.Items)
	return &c
}

// CloneFresh is [Folder.Clone] with new IDs for the folder and every
// descendant, and cleared secret refs.
func (f *Folder) CloneFresh() *Folder {
	c := f.Clone()
	if c != nil {
		c.freshen()
	}
	return c
}

func (f *Folder) freshen() {
	f.ID = NewID()
	freshenAuth(f.Auth)
	freshenVars(f.Variables)
	freshenScripts(&f.Scripts)
	freshenItems(f.Items)
}

// Clone returns a deep copy of the collection and its subtree. Nil-safe.
func (c *Collection) Clone() *Collection {
	if c == nil {
		return nil
	}
	n := *c
	n.Auth = c.Auth.Clone()
	n.Variables = cloneVars(c.Variables)
	n.Scripts = c.Scripts.Clone()
	n.Extra = cloneAnyMap(c.Extra)
	n.Items = cloneItems(c.Items)
	return &n
}

// CloneFresh is [Collection.Clone] with new IDs for the collection and every
// descendant, and cleared secret refs.
func (c *Collection) CloneFresh() *Collection {
	n := c.Clone()
	if n != nil {
		n.ID = NewID()
		freshenAuth(n.Auth)
		freshenVars(n.Variables)
		freshenScripts(&n.Scripts)
		freshenItems(n.Items)
	}
	return n
}

// CloneItem returns a deep copy of a *Folder or *Request. Nil-safe.
func CloneItem(it Item) Item {
	switch n := it.(type) {
	case *Folder:
		return n.Clone()
	case *Request:
		return n.Clone()
	}
	return nil
}

// CloneItemFresh is [CloneItem] with new IDs and cleared secret refs.
func CloneItemFresh(it Item) Item {
	switch n := it.(type) {
	case *Folder:
		return n.CloneFresh()
	case *Request:
		return n.CloneFresh()
	}
	return nil
}

func cloneItems(items []Item) []Item {
	if items == nil {
		return nil
	}
	out := make([]Item, len(items))
	for i, it := range items {
		out[i] = CloneItem(it)
	}
	return out
}

func freshenItems(items []Item) {
	for _, it := range items {
		switch n := it.(type) {
		case *Folder:
			n.freshen()
		case *Request:
			n.freshen()
		}
	}
}

// Clone returns a deep copy of the environment. Nil-safe.
func (e *Environment) Clone() *Environment {
	if e == nil {
		return nil
	}
	c := *e
	c.Variables = cloneVars(e.Variables)
	c.Extra = cloneAnyMap(e.Extra)
	return &c
}

// CloneFresh is [Environment.Clone] with a new ID and cleared secret refs.
func (e *Environment) CloneFresh() *Environment {
	c := e.Clone()
	if c != nil {
		c.ID = NewID()
		freshenVars(c.Variables)
	}
	return c
}

// Clone returns a deep copy of the globals. Nil-safe.
func (g *Globals) Clone() *Globals {
	if g == nil {
		return nil
	}
	c := *g
	c.Variables = cloneVars(g.Variables)
	c.Extra = cloneAnyMap(g.Extra)
	return &c
}

// Clone returns a deep copy of the auth. Nil-safe.
func (a *Auth) Clone() *Auth {
	if a == nil {
		return nil
	}
	c := *a
	if a.Bearer != nil {
		v := *a.Bearer
		c.Bearer = &v
	}
	if a.Basic != nil {
		v := *a.Basic
		c.Basic = &v
	}
	if a.APIKey != nil {
		v := *a.APIKey
		c.APIKey = &v
	}
	if a.Digest != nil {
		v := *a.Digest
		c.Digest = &v
	}
	c.OAuth2 = a.OAuth2.Clone()
	if a.AWSV4 != nil {
		v := *a.AWSV4
		c.AWSV4 = &v
	}
	if a.Hawk != nil {
		v := *a.Hawk
		c.Hawk = &v
	}
	if a.NTLM != nil {
		v := *a.NTLM
		c.NTLM = &v
	}
	c.Raw = cloneAnyMap(a.Raw)
	return &c
}

func freshenAuth(a *Auth) {
	if a != nil && a.OAuth2 != nil && a.OAuth2.Token != nil {
		a.OAuth2.Token.Ref = ""
	}
}

// Clone returns a deep copy of the OAuth 2.0 configuration. Nil-safe.
func (o *OAuth2Config) Clone() *OAuth2Config {
	if o == nil {
		return nil
	}
	c := *o
	if o.Token != nil {
		t := *o.Token
		c.Token = &t
	}
	c.Extra = cloneAnyMap(o.Extra)
	return &c
}

// Clone returns a deep copy of the URL.
func (u URL) Clone() URL {
	u.Query = cloneKVs(u.Query)
	u.PathVars = cloneVars(u.PathVars)
	return u
}

// Clone returns a deep copy of the body.
func (b Body) Clone() Body {
	b.URLEncoded = cloneKVs(b.URLEncoded)
	if b.FormData != nil {
		b.FormData = cloneSlice(b.FormData)
	}
	if b.GraphQL != nil {
		g := *b.GraphQL
		b.GraphQL = &g
	}
	b.Extra = cloneAnyMap(b.Extra)
	return b
}

// Clone returns a deep copy of the scripts.
func (s Scripts) Clone() Scripts {
	s.PreRequest = s.PreRequest.Clone()
	s.Test = s.Test.Clone()
	return s
}

// Clone returns a copy of the script. Nil-safe.
func (s *Script) Clone() *Script {
	if s == nil {
		return nil
	}
	c := *s
	return &c
}

func freshenScripts(s *Scripts) {
	for _, sc := range []*Script{s.PreRequest, s.Test} {
		if sc != nil {
			sc.File = ""
			if sc.ID != "" {
				sc.ID = NewID()
			}
		}
	}
}

// Clone returns a deep copy of the settings.
func (s RequestSettings) Clone() RequestSettings {
	s.FollowRedirects = clonePtr(s.FollowRedirects)
	s.MaxRedirects = clonePtr(s.MaxRedirects)
	s.FollowOriginalMethod = clonePtr(s.FollowOriginalMethod)
	s.FollowAuthHeader = clonePtr(s.FollowAuthHeader)
	s.VerifyTLS = clonePtr(s.VerifyTLS)
	s.Timeout = clonePtr(s.Timeout)
	s.DisableBodyPruning = clonePtr(s.DisableBodyPruning)
	s.DisableURLEncoding = clonePtr(s.DisableURLEncoding)
	s.DisableCookies = clonePtr(s.DisableCookies)
	s.RemoveRefererOnRedirect = clonePtr(s.RemoveRefererOnRedirect)
	s.Extra = cloneAnyMap(s.Extra)
	return s
}

// Clone returns a deep copy of the example.
func (e Example) Clone() Example {
	e.OriginalRequest = e.OriginalRequest.Clone()
	e.Headers = cloneKVs(e.Headers)
	if e.Cookies != nil {
		e.Cookies = cloneSlice(e.Cookies)
	}
	e.Extra = cloneAnyMap(e.Extra)
	return e
}

func cloneExamples(in []Example) []Example {
	if in == nil {
		return nil
	}
	out := make([]Example, len(in))
	for i, e := range in {
		out[i] = e.Clone()
	}
	return out
}

func cloneKVs(in []KV) []KV {
	if in == nil {
		return nil
	}
	return cloneSlice(in)
}

func cloneVars(in []Variable) []Variable {
	if in == nil {
		return nil
	}
	return cloneSlice(in)
}

func freshenVars(vars []Variable) {
	for i := range vars {
		vars[i].SecretRef = ""
	}
}

// cloneSlice copies a slice of flat (pointer-free) values, preserving nil.
func cloneSlice[T any](in []T) []T {
	if in == nil {
		return nil
	}
	out := make([]T, len(in))
	copy(out, in)
	return out
}

func clonePtr[T any](p *T) *T {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}

// cloneAnyMap deep-copies a map holding decoded YAML/JSON values (nested maps
// and slices are copied; scalars are immutable).
func cloneAnyMap(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = cloneAny(v)
	}
	return out
}

func cloneAny(v any) any {
	switch t := v.(type) {
	case map[string]any:
		return cloneAnyMap(t)
	case map[any]any:
		out := make(map[any]any, len(t))
		for k, x := range t {
			out[k] = cloneAny(x)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, x := range t {
			out[i] = cloneAny(x)
		}
		return out
	case []string:
		return cloneSlice(t)
	default:
		return v
	}
}
