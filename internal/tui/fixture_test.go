package tui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/shubham/postctl/internal/store"
	"github.com/shubham/postctl/internal/tui/tuitest"
)

// demoDir is the checked-in workspace for trying the UI by hand:
//
//	go run ./cmd/postctl --workspace internal/tui/testdata/demo
const demoDir = "testdata/demo"

// TestGenerateDemoFixture rewrites demoDir. Run it with:
//
//	POSTCTL_GEN_DEMO=1 go test ./internal/tui -run TestGenerateDemoFixture
func TestGenerateDemoFixture(t *testing.T) {
	if os.Getenv("POSTCTL_GEN_DEMO") == "" {
		t.Skip("set POSTCTL_GEN_DEMO=1 to regenerate testdata/demo")
	}
	if err := os.RemoveAll(demoDir); err != nil {
		t.Fatal(err)
	}
	if _, err := tuitest.Write(demoDir, tuitest.Demo()); err != nil {
		t.Fatal(err)
	}
}

// copyDir copies the fixture so a test can write to it.
func copyDir(t *testing.T, src string) string {
	t.Helper()
	dst := t.TempDir()
	err := filepath.WalkDir(src, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		if d.IsDir() {
			return os.MkdirAll(filepath.Join(dst, rel), 0o755)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dst, rel), b, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	return dst
}

// The shell works against the real store: load, create, rename, reload.
func TestShellOverRealStore(t *testing.T) {
	root := copyDir(t, demoDir)
	be := NewStoreBackend(root) // no secret store: the demo holds no secret values
	a := newApp(Options{Backend: be, NoWatch: true})
	h := &harness{t: t, a: a}
	h.msg(sizeMsg(140, 30))
	h.run(a.Init())
	h.mustHave("List charges")
	h.mustHave("Internal admin")

	h.selectByName("Charges")
	h.keys("n")
	h.typeText("Capture charge")
	h.keys("enter")
	waitSaved(t, a)

	// A fresh backend over the same files sees the new request.
	s, err := store.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	ws, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	if find(ws.Collections[0], "Capture charge") == nil {
		t.Fatal("the request was not written to disk")
	}
	h.keys("ctrl+r")
	h.mustHave("Capture charge")
}

func waitSaved(t *testing.T, a *app) {
	t.Helper()
	for i := 0; i < 200 && a.saver.busy(); i++ {
		waitMs(10)
	}
	if a.saver.busy() {
		t.Fatal("save did not finish")
	}
}
