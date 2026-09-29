package oauth2_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/shubham/postctl/internal/model"
	"github.com/shubham/postctl/internal/oauth2"
	"github.com/shubham/postctl/internal/store"
)

func TestSourceCache(t *testing.T) {
	var fetched int
	src := oauth2.NewSource(nil, func(context.Context, *model.OAuth2Config, oauth2.FetchOptions) (*model.OAuth2Token, error) {
		fetched++
		return nil, errors.New("fetch was called")
	})
	ctx := context.Background()
	cfg := &model.OAuth2Config{Token: &model.OAuth2Token{
		AccessToken: "atk",
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(time.Hour),
	}}
	got, err := src.Token(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if got.AccessToken != "atk" || fetched != 0 {
		t.Fatal("cached token was not returned")
	}
	got.AccessToken = "changed"
	if cfg.Token.AccessToken != "atk" {
		t.Fatal("Token returned the caller's struct")
	}

	cfg.Token.ExpiresAt = time.Time{}
	if _, err := src.Token(ctx, cfg); err != nil || fetched != 0 {
		t.Fatal("a token with unknown expiry was rejected")
	}

	cfg.Token.ExpiresAt = time.Now().Add(-time.Second)
	cfg.Token.RefreshToken = "rtk"
	_, err = src.Token(ctx, cfg)
	if !errors.Is(err, oauth2.ErrNoToken) || fetched != 0 || strings.Contains(err.Error(), "atk") || strings.Contains(err.Error(), "rtk") {
		t.Fatal("expired token was returned, fetched, or leaked")
	}

	if _, err := src.Token(ctx, &model.OAuth2Config{}); !errors.Is(err, oauth2.ErrNoToken) || fetched != 0 {
		t.Fatal("missing token was not ErrNoToken")
	}
	if _, err := src.Token(ctx, nil); !errors.Is(err, oauth2.ErrNoToken) {
		t.Fatal("nil config was not ErrNoToken")
	}
}

func TestSaveAndHydrate(t *testing.T) {
	ms := store.NewMemorySecrets()
	st, err := store.Init(t.TempDir(), "w", store.WithSecretStore(ms))
	if err != nil {
		t.Fatal(err)
	}
	src := oauth2.NewSource(ms, nil)
	tok := &model.OAuth2Token{AccessToken: "atk", RefreshToken: "rtk", IDToken: "idt", TokenType: "Bearer", Scope: "read"}
	ctx := context.Background()
	if err := src.Save(ctx, "req-1", tok); err != nil {
		t.Fatal(err)
	}
	if tok.AccessToken != "atk" || tok.RefreshToken != "rtk" || tok.IDToken != "idt" {
		t.Fatal("Save cleared in-memory secrets")
	}
	wantRef := store.SecretRefFor(store.ScopeOAuth2, "req-1", "")
	if tok.Ref != wantRef {
		t.Fatal("secret ref was not assigned from the owner id")
	}
	raw, err := ms.Get(ctx, tok.Ref)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(raw, "Bearer") || strings.Contains(raw, "read") {
		t.Fatal("token metadata was written into the secret")
	}
	auth := &model.Auth{Type: model.AuthOAuth2, OAuth2: &model.OAuth2Config{
		Token: &model.OAuth2Token{Ref: tok.Ref, TokenType: "Bearer", Scope: "read", ExpiresAt: time.Now().Add(time.Hour)},
	}}
	if _, err := st.HydrateAuth(ctx, auth); err != nil {
		t.Fatal(err)
	}
	if auth.OAuth2.Token.AccessToken != "atk" || auth.OAuth2.Token.RefreshToken != "rtk" || auth.OAuth2.Token.IDToken != "idt" {
		t.Fatal("HydrateAuth could not read the stored token")
	}

	cfg := &model.OAuth2Config{Token: &model.OAuth2Token{Ref: wantRef, TokenType: "Bearer", ExpiresAt: time.Now().Add(time.Hour)}}
	got, err := src.Token(ctx, cfg)
	if err != nil || got.AccessToken != "atk" || got.RefreshToken != "rtk" || got.IDToken != "idt" {
		t.Fatal("Token did not load the stored secret")
	}

	if err := oauth2.NewSource(nil, nil).Save(ctx, "req-1", tok); err == nil || strings.Contains(err.Error(), "atk") {
		t.Fatal("missing secret store was accepted or leaked")
	}
	if err := src.Save(ctx, "", &model.OAuth2Token{}); err == nil {
		t.Fatal("empty token was stored")
	}
}
