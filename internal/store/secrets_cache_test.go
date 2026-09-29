package store

import (
	"context"
	"errors"
	"testing"

	"github.com/shubham/postctl/internal/model"
)

type readCountingSecrets struct {
	SecretStore
	gets int
}

func (s *readCountingSecrets) Get(ctx context.Context, ref model.SecretRef) (string, error) {
	s.gets++
	return s.SecretStore.Get(ctx, ref)
}

func TestCachingSecretsInvalidatesOnWrite(t *testing.T) {
	ctx := t.Context()
	ref := model.SecretRef("test/ref")
	memory := NewMemorySecrets()
	if err := memory.Set(ctx, ref, "first"); err != nil {
		t.Fatal(err)
	}
	underlying := &readCountingSecrets{SecretStore: memory}
	cached := NewCachingSecrets(underlying)

	for range 3 {
		if got, err := cached.Get(ctx, ref); err != nil || got != "first" {
			t.Fatalf("cached Get = %q, %v", got, err)
		}
	}
	if underlying.gets != 1 {
		t.Fatalf("reads = %d, want 1", underlying.gets)
	}
	if err := cached.Set(ctx, ref, "second"); err != nil {
		t.Fatal(err)
	}
	if got, err := cached.Get(ctx, ref); err != nil || got != "second" {
		t.Fatalf("Get after Set = %q, %v", got, err)
	}
	if underlying.gets != 2 {
		t.Fatalf("reads after Set = %d, want 2", underlying.gets)
	}
	if err := cached.Delete(ctx, ref); err != nil {
		t.Fatal(err)
	}
	if _, err := cached.Get(ctx, ref); !errors.Is(err, ErrSecretNotFound) {
		t.Fatalf("Get after Delete error = %v", err)
	}
	if underlying.gets != 3 {
		t.Fatalf("reads after Delete = %d, want 3", underlying.gets)
	}
}
