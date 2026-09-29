package engine

import (
	"context"
	"net/http"
	"sync"
	"testing"

	"github.com/shubham/postctl/internal/model"
	"github.com/shubham/postctl/internal/store"
)

type countingSecrets struct {
	store.SecretStore
	mu   sync.Mutex
	gets map[model.SecretRef]int
}

func (s *countingSecrets) Get(ctx context.Context, ref model.SecretRef) (string, error) {
	s.mu.Lock()
	s.gets[ref]++
	s.mu.Unlock()
	return s.SecretStore.Get(ctx, ref)
}

func (s *countingSecrets) count(ref model.SecretRef) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.gets[ref]
}

func TestRepeatedSendsReadEachSecretOnce(t *testing.T) {
	srv := newServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	memory := store.NewMemorySecrets()
	counting := &countingSecrets{SecretStore: memory, gets: make(map[model.SecretRef]int)}
	st, err := store.Init(t.TempDir(), "cache", store.WithSecretStore(counting))
	if err != nil {
		t.Fatal(err)
	}
	collection := model.NewCollection("API")
	request := model.NewRequest("cached", http.MethodGet, "{{secretBase}}")
	collection.Items = []model.Item{request}
	environment := &model.Environment{
		ID:   model.NewID(),
		Name: "Test",
		Variables: []model.Variable{{
			Key: "secretBase", Type: model.VarSecret, Value: srv.URL,
		}},
	}
	workspace, err := st.Load()
	if err != nil {
		t.Fatal(err)
	}
	workspace.Collections = []*model.Collection{collection}
	workspace.Environments = []*model.Environment{environment}
	if err := st.Save(t.Context(), workspace); err != nil {
		t.Fatal(err)
	}
	workspace, err = st.Load()
	if err != nil {
		t.Fatal(err)
	}
	eng, err := New(Config{Store: st, DisableHistory: true})
	if err != nil {
		t.Fatal(err)
	}
	for range 5 {
		out, sendErr := eng.Send(t.Context(), SendInput{
			Collection:  workspace.Collections[0],
			Request:     workspace.Collections[0].Items[0].(*model.Request),
			Environment: workspace.Environments[0],
		}, nil)
		if sendErr != nil || out.Err != nil || out.Response.StatusCode != http.StatusNoContent {
			t.Fatalf("send failed: err=%v outcome=%+v", sendErr, out)
		}
	}
	ref := store.SecretRefFor(store.ScopeEnvironment, environment.ID, "secretBase")
	if got := counting.count(ref); got != 1 {
		t.Fatalf("underlying secret reads = %d, want 1 for repeated sends", got)
	}
}
