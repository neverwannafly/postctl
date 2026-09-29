package httpbin_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shubham/postctl/internal/cli"
	"github.com/shubham/postctl/internal/store"
)

func workspaceDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs(".")
	if err != nil {
		t.Fatalf("abs dir: %v", err)
	}
	return dir
}

func TestStoreLoadExample(t *testing.T) {
	dir := workspaceDir(t)
	st, err := store.Open(dir)
	if err != nil {
		t.Fatalf("store.Open failed on examples/httpbin: %v", err)
	}
	ws, err := st.Load()
	if err != nil {
		t.Fatalf("store.Load failed on examples/httpbin: %v", err)
	}
	if ws.Name != "httpbin" {
		t.Errorf("expected workspace name %q, got %q", "httpbin", ws.Name)
	}
	if len(ws.Collections) != 1 {
		t.Fatalf("expected 1 collection, got %d", len(ws.Collections))
	}
	col := ws.Collections[0]
	if col.Name != "HTTPBin" {
		t.Errorf("expected collection name %q, got %q", "HTTPBin", col.Name)
	}
	if len(ws.Environments) != 1 {
		t.Fatalf("expected 1 environment, got %d", len(ws.Environments))
	}
	env := ws.Environments[0]
	if env.Name != "httpbin" {
		t.Errorf("expected environment name %q, got %q", "httpbin", env.Name)
	}

	// Verify that secret variables exist with secretRef and empty Value on disk
	var foundSecretToken bool
	for _, v := range env.Variables {
		if v.Key == "token" && v.IsSecret() && v.SecretRef != "" {
			foundSecretToken = true
			if v.Value != "" {
				t.Errorf("expected secret token value to be empty on disk, got %q", v.Value)
			}
		}
	}
	if !foundSecretToken {
		t.Errorf("expected secret variable 'token' with secretRef in environment")
	}
}

func TestCLILs(t *testing.T) {
	dir := workspaceDir(t)
	var out, errOut bytes.Buffer
	opts := cli.Options{
		Workspace: dir,
		Stdout:    &out,
		Stderr:    &errOut,
	}
	code := cli.Execute(context.Background(), opts, []string{"ls"})
	if code != cli.ExitOK || errOut.String() != "" {
		t.Fatalf("ls failed: code=%d err=%q", code, errOut.String())
	}
	output := out.String()
	for _, want := range []string{
		"HTTPBin",
		"Methods",
		"GET with query",
		"POST JSON body",
		"POST form urlencoded",
		"POST multipart form",
		"Authentication",
		"Basic auth",
		"Bearer auth",
		"HMAC signature",
		"Runner flow",
		"Step start",
		"Step skip",
		"Step finish",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("ls output missing %q:\n%s", want, output)
		}
	}
}

// fakeHttpbinServer implements local httpbin endpoints for testing without network calls.
func newFakeHttpbinServer() *httptest.Server {
	mux := http.NewServeMux()

	mux.HandleFunc("/get", func(w http.ResponseWriter, r *http.Request) {
		args := make(map[string]string)
		for k, v := range r.URL.Query() {
			if len(v) > 0 {
				args[k] = v[0]
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"args": args,
			"url":  r.URL.String(),
		})
	})

	mux.HandleFunc("/post", func(w http.ResponseWriter, r *http.Request) {
		ct := r.Header.Get("Content-Type")
		resp := map[string]any{
			"url":     r.URL.String(),
			"headers": r.Header,
		}

		switch {
		case strings.HasPrefix(ct, "application/json"):
			body, _ := io.ReadAll(r.Body)
			var parsed any
			_ = json.Unmarshal(body, &parsed)
			resp["json"] = parsed
		case strings.HasPrefix(ct, "multipart/form-data"):
			_ = r.ParseMultipartForm(10 << 20)
			formVals := make(map[string]string)
			for k, v := range r.MultipartForm.Value {
				if len(v) > 0 {
					formVals[k] = v[0]
				}
			}
			files := make(map[string]string)
			for k := range r.MultipartForm.File {
				files[k] = "uploaded"
			}
			resp["form"] = formVals
			resp["files"] = files
		default: // urlencoded
			_ = r.ParseForm()
			formVals := make(map[string]string)
			for k, v := range r.PostForm {
				if len(v) > 0 {
					formVals[k] = v[0]
				}
			}
			resp["form"] = formVals
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})

	mux.HandleFunc("/basic-auth/postctl/secretpassword", func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok || user != "postctl" || pass != "secretpassword" {
			w.Header().Set("WWW-Authenticate", `Basic realm="fake"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"authenticated": true,
			"user":          user,
		})
	})

	mux.HandleFunc("/bearer", func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "Bearer ") {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		token := strings.TrimPrefix(auth, "Bearer ")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"authenticated": true,
			"token":         token,
		})
	})

	mux.HandleFunc("/headers", func(w http.ResponseWriter, r *http.Request) {
		headers := make(map[string]string)
		for k, v := range r.Header {
			if len(v) > 0 {
				headers[k] = v[0]
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"headers": headers,
		})
	})

	return httptest.NewServer(mux)
}

func TestCLIExecutionAgainstLocalFake(t *testing.T) {
	srv := newFakeHttpbinServer()
	defer srv.Close()

	srcDir := workspaceDir(t)
	tempDir := t.TempDir()
	if err := os.CopyFS(tempDir, os.DirFS(srcDir)); err != nil {
		t.Fatalf("copy workspace: %v", err)
	}

	secrets := store.NewMemorySecrets()
	_ = secrets.Set(context.Background(), "env/env-httpbin/token", "fake-secret-token")
	_ = secrets.Set(context.Background(), "env/env-httpbin/hmacSecret", "fake-hmac-secret")

	st, err := store.Open(tempDir, store.WithSecretStore(secrets))
	if err != nil {
		t.Fatalf("store.Open temp: %v", err)
	}
	ws, err := st.Load()
	if err != nil {
		t.Fatalf("st.Load temp: %v", err)
	}
	for i, v := range ws.Environments[0].Variables {
		if v.Key == "baseUrl" {
			ws.Environments[0].Variables[i].Value = srv.URL
		}
	}
	if err := st.SaveEnvironment(context.Background(), ws.Environments[0]); err != nil {
		t.Fatalf("save env: %v", err)
	}
	if err := st.SaveState(store.State{ActiveEnvironment: ws.Environments[0].ID}); err != nil {
		t.Fatalf("save state: %v", err)
	}

	tests := []struct {
		name string
		args []string
	}{
		{
			name: "send get query",
			args: []string{"send", "HTTPBin/Methods/GET with query", "--no-history"},
		},
		{
			name: "send post json",
			args: []string{"send", "HTTPBin/Methods/POST JSON body", "--no-history"},
		},
		{
			name: "send post form",
			args: []string{"send", "HTTPBin/Methods/POST form urlencoded", "--no-history"},
		},
		{
			name: "send post multipart",
			args: []string{"send", "HTTPBin/Methods/POST multipart form", "--no-history"},
		},
		{
			name: "send basic auth",
			args: []string{"send", "HTTPBin/Authentication/Basic auth", "--no-history"},
		},
		{
			name: "send bearer auth",
			args: []string{"send", "HTTPBin/Authentication/Bearer auth", "--no-history"},
		},
		{
			name: "send hmac auth with crypto-js",
			args: []string{"send", "HTTPBin/Authentication/HMAC signature", "--no-history"},
		},
		{
			name: "run methods folder",
			args: []string{"run", "HTTPBin/Methods", "--no-history"},
		},
		{
			name: "run runner flow folder",
			args: []string{"run", "HTTPBin/Runner flow", "--no-history"},
		},
		{
			name: "run whole collection",
			args: []string{"run", "HTTPBin", "--no-history"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var out, errOut bytes.Buffer
			opts := cli.Options{
				Workspace: tempDir,
				Stdout:    &out,
				Stderr:    &errOut,
				Store:     st,
			}
			code := cli.Execute(context.Background(), opts, tc.args)
			if code != cli.ExitOK {
				t.Fatalf("command failed: code=%d err=%q\nout:\n%s", code, errOut.String(), out.String())
			}
		})
	}
}
