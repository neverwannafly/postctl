package oauth2

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/shubham/postctl/internal/model"
	"github.com/shubham/postctl/internal/store"
)

// ErrNoToken is returned by [Source.Token] when no usable cached token is
// available. The caller decides whether to [Fetch]; Token never does.
var ErrNoToken = errors.New("oauth2: no token")

// FetchFunc obtains a token. [Fetch] is a FetchFunc. [Source.Token] does not
// call it.
type FetchFunc func(context.Context, *model.OAuth2Config, FetchOptions) (*model.OAuth2Token, error)

// Source is a cached OAuth 2.0 token source. Token returns a stored token
// that has not expired. Save writes token strings to the SecretStore using
// the same JSON shape as store.HydrateAuth (model rule 6).
//
// Source.Token has the signature httpx will require of TokenSource. This
// package does not import httpx.
type Source struct {
	secrets store.SecretStore
	fetch   FetchFunc
}

// storedToken is the JSON kept under an OAuth2Token secret ref. The keys
// match the unexported store token secret.
type storedToken struct {
	Access  string `json:"access,omitempty"`
	Refresh string `json:"refresh,omitempty"`
	ID      string `json:"id,omitempty"`
}

// NewSource returns a token source that reads and writes secrets.
// fetch is kept for an explicit [Source.Request] and is not used by Token.
func NewSource(secrets store.SecretStore, fetch FetchFunc) *Source {
	return &Source{secrets: secrets, fetch: fetch}
}

// Token implements the forthcoming httpx.TokenSource method. It returns a
// copy of the cached token when one is present and not expired. An empty
// access token with a secret ref is loaded from the SecretStore. Otherwise
// Token returns [ErrNoToken]. It does not refresh and does not call fetch.
func (s *Source) Token(ctx context.Context, cfg *model.OAuth2Config) (*model.OAuth2Token, error) {
	if cfg == nil || cfg.Token == nil {
		return nil, ErrNoToken
	}
	tok := *cfg.Token
	if err := s.fill(ctx, &tok); err != nil {
		return nil, err
	}
	if tok.AccessToken == "" || tok.Expired(time.Now()) {
		return nil, ErrNoToken
	}
	return &tok, nil
}

// Request runs the fetch function passed to [NewSource]. It does not store
// the result; call [Source.Save]. Token never calls Request.
func (s *Source) Request(ctx context.Context, cfg *model.OAuth2Config, opts FetchOptions) (*model.OAuth2Token, error) {
	if s == nil || s.fetch == nil {
		return nil, errors.New("oauth2: no fetch function")
	}
	return s.fetch(ctx, cfg, opts)
}

// Save writes the access, refresh, and ID token strings to the SecretStore.
// When token.Ref is empty, owner is the collection, folder, or request ID and
// Ref is set to oauth2/<owner>, matching [store.SecretRefFor]. In-memory
// secret fields are left in place so the caller can keep using the token;
// the workspace save clears them from YAML.
func (s *Source) Save(ctx context.Context, owner model.ID, token *model.OAuth2Token) error {
	if token == nil {
		return errors.New("oauth2: no token to store")
	}
	if token.AccessToken == "" && token.RefreshToken == "" && token.IDToken == "" {
		return errors.New("oauth2: token has no secret to store")
	}
	if s == nil || s.secrets == nil {
		return errors.New("oauth2: no secret store")
	}
	if token.Ref == "" {
		if owner == "" {
			return errors.New("oauth2: token has no secret ref and no owner id")
		}
		token.Ref = store.SecretRefFor(store.ScopeOAuth2, owner, "")
	}
	blob, err := json.Marshal(storedToken{Access: token.AccessToken, Refresh: token.RefreshToken, ID: token.IDToken})
	if err != nil {
		return errors.New("oauth2: could not store the token")
	}
	if err := s.secrets.Set(ctx, token.Ref, string(blob)); err != nil {
		return fmt.Errorf("oauth2: store token: %w", err)
	}
	return nil
}

func (s *Source) fill(ctx context.Context, tok *model.OAuth2Token) error {
	if tok.AccessToken != "" || tok.Ref == "" {
		return nil
	}
	if s == nil || s.secrets == nil {
		return errors.New("oauth2: no secret store")
	}
	blob, err := s.secrets.Get(ctx, tok.Ref)
	if errors.Is(err, store.ErrSecretNotFound) {
		return ErrNoToken
	}
	if err != nil {
		return errors.New("oauth2: could not read the stored token")
	}
	var st storedToken
	if err := json.Unmarshal([]byte(blob), &st); err != nil {
		return fmt.Errorf("oauth2: stored token %s is not valid", tok.Ref)
	}
	tok.AccessToken = st.Access
	tok.RefreshToken = st.Refresh
	tok.IDToken = st.ID
	return nil
}

// Confirm Source.Token matches the httpx.TokenSource signature WS2 will add.
var _ interface {
	Token(context.Context, *model.OAuth2Config) (*model.OAuth2Token, error)
} = (*Source)(nil)
