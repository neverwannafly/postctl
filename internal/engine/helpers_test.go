package engine

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/shubham/postctl/internal/model"
	"github.com/shubham/postctl/internal/store"
)

// fixture is a temp workspace on disk with one collection, one environment and
// an engine over it. Tests edit the models before calling save.
type fixture struct {
	t       *testing.T
	dir     string
	store   *store.Store
	secrets *store.MemorySecrets
	ws      *model.Workspace
	col     *model.Collection
	env     *model.Environment
	eng     *Engine
	server  *server
}

// server is an httptest server that records what it saw.
type server struct {
	*httptest.Server
	mu    sync.Mutex
	paths []string
	hdrs  []http.Header
}

func (s *server) hits() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.paths...)
}

func newServer(t *testing.T, handler http.HandlerFunc) *server {
	t.Helper()
	s := &server{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.paths = append(s.paths, r.URL.RequestURI())
		s.hdrs = append(s.hdrs, r.Header.Clone())
		s.mu.Unlock()
		handler(w, r)
	}))
	t.Cleanup(s.Close)
	return s
}

// newFixture builds a workspace with collection "API" and environment "Test"
// whose baseUrl points at srv. The engine is created by save.
func newFixture(t *testing.T, srv *server) *fixture {
	t.Helper()
	dir := t.TempDir()
	secrets := store.NewMemorySecrets()
	st, err := store.Init(dir, "test", store.WithSecretStore(secrets))
	if err != nil {
		t.Fatal(err)
	}
	ws, err := st.Load()
	if err != nil {
		t.Fatal(err)
	}
	f := &fixture{t: t, dir: dir, store: st, secrets: secrets, ws: ws, server: srv}
	f.col = model.NewCollection("API")
	f.env = &model.Environment{ID: model.NewID(), Name: "Test", Variables: []model.Variable{
		{Key: "baseUrl", Value: srv.URL},
	}}
	return f
}

// save writes the models to disk, reloads them the way the app would, and
// (re)creates the engine.
func (f *fixture) save(opts ...func(*Config)) {
	f.t.Helper()
	f.ws.Collections = []*model.Collection{f.col}
	f.ws.Environments = []*model.Environment{f.env}
	if err := f.store.Save(context.Background(), f.ws); err != nil {
		f.t.Fatal(err)
	}
	ws, err := f.store.Load()
	if err != nil {
		f.t.Fatal(err)
	}
	f.ws, f.col, f.env = ws, ws.Collections[0], ws.Environments[0]
	cfg := Config{Store: f.store, Version: "test"}
	for _, o := range opts {
		o(&cfg)
	}
	f.eng, err = New(cfg)
	if err != nil {
		f.t.Fatal(err)
	}
}

// reload returns the environment as it is on disk now.
func (f *fixture) reloadEnv() *model.Environment {
	f.t.Helper()
	ws, err := f.store.Load()
	if err != nil {
		f.t.Fatal(err)
	}
	return ws.Environments[0]
}

func (f *fixture) reloadCollection() *model.Collection {
	f.t.Helper()
	ws, err := f.store.Load()
	if err != nil {
		f.t.Fatal(err)
	}
	return ws.Collections[0]
}

func (f *fixture) send(req *model.Request, progress func(Step)) *Outcome {
	f.t.Helper()
	out, err := f.eng.Send(context.Background(), SendInput{
		Collection: f.col, Request: req, Environment: f.env,
	}, progress)
	if err != nil {
		f.t.Fatal(err)
	}
	return out
}

func (f *fixture) findRequest(name string) *model.Request {
	f.t.Helper()
	for _, r := range f.col.Requests() {
		if r.Name == name {
			return r
		}
	}
	f.t.Fatalf("no request %q", name)
	return nil
}

func varValue(vars []model.Variable, key string) (string, bool) {
	for _, v := range vars {
		if v.Key == key {
			return v.Value, true
		}
	}
	return "", false
}

func js(source string) *model.Script { return &model.Script{Source: source} }

func jsonHandler(body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}
}

// filesUnder returns every regular file under dir with its contents.
func filesUnder(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || strings.HasSuffix(path, ".lock") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		out[path] = string(data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func stepTexts(steps []Step) []string {
	out := make([]string, len(steps))
	for i, s := range steps {
		out[i] = s.Text
	}
	return out
}
