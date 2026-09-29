package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"

	"github.com/shubham/postctl/internal/model"
)

// ErrSecretNotFound is returned by [SecretStore.Get] when the ref has no
// value.
var ErrSecretNotFound = errors.New("store: secret not found")

// SecretStore keeps secret values outside the workspace files. The macOS
// implementation is [Keychain]; [MemorySecrets] is the in-memory fake for tests
// and process-only sessions.
//
// Implementations must be safe for concurrent use, must not log values, and
// Delete must succeed when the ref is absent.
type SecretStore interface {
	// Get returns the value for ref, or [ErrSecretNotFound].
	Get(ctx context.Context, ref model.SecretRef) (string, error)
	// Set stores value under ref, replacing any existing value.
	Set(ctx context.Context, ref model.SecretRef, value string) error
	// Delete removes ref. It is not an error if ref does not exist.
	Delete(ctx context.Context, ref model.SecretRef) error
}

// MemorySecrets is an in-memory [SecretStore].
type MemorySecrets struct {
	mu sync.Mutex
	m  map[model.SecretRef]string
}

// NewMemorySecrets returns an empty in-memory secret store.
func NewMemorySecrets() *MemorySecrets {
	return &MemorySecrets{m: map[model.SecretRef]string{}}
}

// Get implements [SecretStore].
func (s *MemorySecrets) Get(_ context.Context, ref model.SecretRef) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.m[ref]
	if !ok {
		return "", ErrSecretNotFound
	}
	return v, nil
}

// Set implements [SecretStore].
func (s *MemorySecrets) Set(_ context.Context, ref model.SecretRef, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[ref] = value
	return nil
}

// Delete implements [SecretStore].
func (s *MemorySecrets) Delete(_ context.Context, ref model.SecretRef) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, ref)
	return nil
}

// Refs returns the stored refs in sorted order. Values are never exposed.
func (s *MemorySecrets) Refs() []model.SecretRef {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]model.SecretRef, 0, len(s.m))
	for r := range s.m {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// Secret scopes used in refs.
const (
	// ScopeEnvironment is the ref scope for environment variables.
	ScopeEnvironment = "env"
	// ScopeGlobals is the ref scope for global variables.
	ScopeGlobals = "globals"
	// ScopeCollection is the ref scope for collection variables.
	ScopeCollection = "collection"
	// ScopeFolder is the ref scope for folder variables.
	ScopeFolder = "folder"
	// ScopeOAuth2 is the ref scope for OAuth 2.0 token sets; ownerID is the
	// ID of the collection, folder or request holding the auth.
	ScopeOAuth2 = "oauth2"
)

// SecretRefFor returns the deterministic ref the store assigns to a secret
// variable (scope, owner ID, key) or, for [ScopeOAuth2], to an owner's token
// set (key empty). Refs are keyed by node ID so renaming a node or its file
// never orphans its secrets.
func SecretRefFor(scope string, ownerID model.ID, key string) model.SecretRef {
	if key == "" {
		return model.SecretRef(scope + "/" + string(ownerID))
	}
	return model.SecretRef(scope + "/" + string(ownerID) + "/" + key)
}

// tokenSecret is the JSON stored under an OAuth2Token.Ref.
type tokenSecret struct {
	Access  string `json:"access,omitempty"`
	Refresh string `json:"refresh,omitempty"`
	ID      string `json:"id,omitempty"`
}

// secretWriter turns in-memory secrets into refs during a save.
type secretWriter struct {
	ctx   context.Context
	store SecretStore
	// used collects every ref written into the files being saved.
	used map[model.SecretRef]bool
}

func newSecretWriter(ctx context.Context, ss SecretStore) *secretWriter {
	return &secretWriter{ctx: ctx, store: ss, used: map[model.SecretRef]bool{}}
}

// vars returns a copy of vars safe to serialize: secret values are moved to
// the SecretStore and replaced by a ref.
func (w *secretWriter) vars(scope string, owner model.ID, vars []model.Variable) ([]model.Variable, error) {
	if len(vars) == 0 {
		return nil, nil
	}
	out := make([]model.Variable, len(vars))
	for i, v := range vars {
		if !v.IsSecret() {
			v.SecretRef = ""
			out[i] = v
			continue
		}
		ref := v.SecretRef
		if ref == "" {
			ref = SecretRefFor(scope, owner, v.Key)
		}
		if v.Value != "" {
			if w.store == nil {
				return nil, fmt.Errorf("%w (variable %q)", ErrNoSecretStore, v.Key)
			}
			if err := w.store.Set(w.ctx, ref, v.Value); err != nil {
				return nil, fmt.Errorf("store secret for variable %q: %w", v.Key, err)
			}
		}
		w.used[ref] = true
		v.Value = ""
		v.SecretRef = ref
		out[i] = v
	}
	return out, nil
}

// auth returns a copy of a with OAuth 2.0 token secrets moved out. It never
// mutates a.
func (w *secretWriter) auth(owner model.ID, a *model.Auth) (*model.Auth, error) {
	if a == nil || a.OAuth2 == nil || a.OAuth2.Token == nil {
		return a, nil
	}
	cfg := *a.OAuth2
	tok := *a.OAuth2.Token
	if tok.Ref == "" {
		tok.Ref = SecretRefFor(ScopeOAuth2, owner, "")
	}
	if tok.AccessToken != "" || tok.RefreshToken != "" || tok.IDToken != "" {
		if w.store == nil {
			return nil, fmt.Errorf("%w (oauth2 token)", ErrNoSecretStore)
		}
		blob, err := json.Marshal(tokenSecret{Access: tok.AccessToken, Refresh: tok.RefreshToken, ID: tok.IDToken})
		if err != nil {
			return nil, err
		}
		if err := w.store.Set(w.ctx, tok.Ref, string(blob)); err != nil {
			return nil, fmt.Errorf("store oauth2 token: %w", err)
		}
	}
	w.used[tok.Ref] = true
	tok.AccessToken, tok.RefreshToken, tok.IDToken = "", "", ""
	cfg.Token = &tok
	out := *a
	out.OAuth2 = &cfg
	return &out, nil
}

// Hydrate fills in secret values from the SecretStore for every secret
// variable and OAuth 2.0 token in ws, in place. Values already set are kept.
// Refs with no stored value are skipped and returned in missing so the UI can
// prompt; other failures (for example a locked Keychain) are returned as err.
//
// Load never reads secrets, so opening a workspace never prompts for Keychain
// access; call Hydrate (or the narrower helpers) when values are needed.
func (s *Store) Hydrate(ctx context.Context, ws *model.Workspace) (missing []model.SecretRef, err error) {
	h := &hydrator{ctx: ctx, ss: s.secrets}
	for _, c := range ws.Collections {
		h.collection(c)
	}
	for _, e := range ws.Environments {
		h.vars(e.Variables)
	}
	if ws.Globals != nil {
		h.vars(ws.Globals.Variables)
	}
	return h.missing, h.err
}

// HydrateVariables fills in secret values for vars in place; see [Store.Hydrate].
func (s *Store) HydrateVariables(ctx context.Context, vars []model.Variable) (missing []model.SecretRef, err error) {
	h := &hydrator{ctx: ctx, ss: s.secrets}
	h.vars(vars)
	return h.missing, h.err
}

// HydrateAuth fills in OAuth 2.0 token secrets for a in place; see [Store.Hydrate].
func (s *Store) HydrateAuth(ctx context.Context, a *model.Auth) (missing []model.SecretRef, err error) {
	h := &hydrator{ctx: ctx, ss: s.secrets}
	h.auth(a)
	return h.missing, h.err
}

type hydrator struct {
	ctx     context.Context
	ss      SecretStore
	missing []model.SecretRef
	err     error
}

func (h *hydrator) collection(c *model.Collection) {
	h.vars(c.Variables)
	h.auth(c.Auth)
	_ = c.Walk(func(it model.Item, _ model.Container, _ int) error {
		switch n := it.(type) {
		case *model.Folder:
			h.vars(n.Variables)
			h.auth(n.Auth)
		case *model.Request:
			h.auth(n.Auth)
		}
		return nil
	})
}

func (h *hydrator) get(ref model.SecretRef) (string, bool) {
	if h.err != nil || ref == "" {
		return "", false
	}
	if h.ss == nil {
		h.err = ErrNoSecretStore
		return "", false
	}
	v, err := h.ss.Get(h.ctx, ref)
	switch {
	case errors.Is(err, ErrSecretNotFound):
		h.missing = append(h.missing, ref)
		return "", false
	case err != nil:
		h.err = fmt.Errorf("read secret: %w", err)
		return "", false
	}
	return v, true
}

func (h *hydrator) vars(vars []model.Variable) {
	for i := range vars {
		if !vars[i].IsSecret() || vars[i].Value != "" {
			continue
		}
		if v, ok := h.get(vars[i].SecretRef); ok {
			vars[i].Value = v
		}
	}
}

func (h *hydrator) auth(a *model.Auth) {
	if a == nil || a.OAuth2 == nil || a.OAuth2.Token == nil {
		return
	}
	t := a.OAuth2.Token
	if t.AccessToken != "" || t.RefreshToken != "" || t.IDToken != "" {
		return
	}
	blob, ok := h.get(t.Ref)
	if !ok {
		return
	}
	var ts tokenSecret
	if err := json.Unmarshal([]byte(blob), &ts); err != nil {
		h.err = fmt.Errorf("decode oauth2 token secret %s: %w", t.Ref, err)
		return
	}
	t.AccessToken, t.RefreshToken, t.IDToken = ts.Access, ts.Refresh, ts.ID
}

// refsInVars returns the secret refs referenced by vars.
func refsInVars(vars []model.Variable) []model.SecretRef {
	var out []model.SecretRef
	for _, v := range vars {
		if v.IsSecret() && v.SecretRef != "" {
			out = append(out, v.SecretRef)
		}
	}
	return out
}

// refsInAuth returns the token secret ref of a, if any.
func refsInAuth(a *model.Auth) []model.SecretRef {
	if a != nil && a.OAuth2 != nil && a.OAuth2.Token != nil && a.OAuth2.Token.Ref != "" {
		return []model.SecretRef{a.OAuth2.Token.Ref}
	}
	return nil
}
