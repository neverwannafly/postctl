package tui

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/shubham/postctl/internal/model"
	"github.com/shubham/postctl/internal/tui/tuitest"
)

func TestRelevantPath(t *testing.T) {
	for path, want := range map[string]bool{
		"/collections/a/b.yaml":     true,
		"/collections/a/b.yml":      true,
		"/collections/a/pre.js":     true,
		"/collections/a/folder":     true, // directories have no extension
		"/collections/a/.b.yaml.sw": false,
		"/collections/a/b.yaml~":    false,
		"/collections/a/notes.md":   false,
		"/.git/index":               false,
		"/.postctl/state.json":      false,
	} {
		if got := relevantPath(path); got != want {
			t.Errorf("relevantPath(%q) = %v, want %v", path, got, want)
		}
	}
}

func waitEvent(t *testing.T, w watcher, within time.Duration) bool {
	t.Helper()
	select {
	case _, ok := <-w.Events():
		return ok
	case <-time.After(within):
		return false
	}
}

func TestFSWatcherDebouncesBursts(t *testing.T) {
	root := t.TempDir()
	if _, err := tuitest.Write(root, tuitest.Demo()); err != nil {
		t.Fatal(err)
	}
	w, err := newFSWatcher(root)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	dir := filepath.Join(root, "collections")
	entries, _ := os.ReadDir(dir)
	if len(entries) == 0 {
		t.Fatal("fixture wrote no collections")
	}
	// A burst of writes is one event.
	for i := 0; i < 8; i++ {
		f := filepath.Join(dir, entries[0].Name(), "burst.yaml")
		if err := os.WriteFile(f, []byte("x: "+string(rune('a'+i))+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if !waitEvent(t, w, 3*time.Second) {
		t.Fatal("no event after a burst of writes")
	}
	if waitEvent(t, w, watchDebounce+300*time.Millisecond) {
		t.Fatal("a burst must collapse into one event")
	}

	// Irrelevant files and dot directories are ignored.
	if err := os.MkdirAll(filepath.Join(root, ".postctl"), 0o755); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(root, ".postctl", "state.json"), []byte("{}"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "notes.md"), []byte("hi"), 0o644)
	if waitEvent(t, w, watchDebounce+300*time.Millisecond) {
		t.Fatal("irrelevant changes must not trigger a reload")
	}

	// A folder created later is watched too.
	nd := filepath.Join(dir, entries[0].Name(), "newfolder")
	if err := os.Mkdir(nd, 0o755); err != nil {
		t.Fatal(err)
	}
	if !waitEvent(t, w, 3*time.Second) {
		t.Fatal("no event for a new folder")
	}
	time.Sleep(50 * time.Millisecond) // let the watch on nd be added
	_ = os.WriteFile(filepath.Join(nd, "r.yaml"), []byte("a: b\n"), 0o644)
	if !waitEvent(t, w, 3*time.Second) {
		t.Fatal("files in a new folder are not watched")
	}
}

func TestFSWatcherCloseIsIdempotentAndEndsEvents(t *testing.T) {
	w, err := newFSWatcher(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	_ = w.Close()
	select {
	case _, ok := <-w.Events():
		if ok {
			t.Fatal("unexpected event")
		}
	case <-time.After(time.Second):
		t.Fatal("events channel should close")
	}
}

func TestPollWatcherNoticesChanges(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "collections"), 0o755); err != nil {
		t.Fatal(err)
	}
	p := newPollWatcher(root)
	defer p.Close()
	time.Sleep(50 * time.Millisecond)
	_ = os.WriteFile(filepath.Join(root, "collections", "a.yaml"), []byte("a: 1\n"), 0o644)
	if !waitEvent(t, p, pollInterval+2*time.Second) {
		t.Fatal("poll watcher missed a new file")
	}
}

// A change from the file watcher reloads the tree, and our own saves do not.
func TestWatcherEventReloadsButOwnSavesDoNot(t *testing.T) {
	h := newHarness(t, demo(), 160, 30)
	h.selectByName("Create charge")
	id := h.a.tree.sel
	fw := &fakeWatcher{ch: make(chan struct{}, 1)}

	ws := deepCopy(h.be.ws)
	ws.Collections[0].Items[0].(*model.Folder).Items[1].(*model.Request).Name = "Charge it"
	h.be.ws = ws
	loads := h.be.loads
	h.a.onWatchReady(watchReadyMsg{w: fw})
	h.msg(fsChangedMsg{w: fw})
	if h.be.loads != loads+1 {
		t.Fatalf("external change should reload once, loads %d -> %d", loads, h.be.loads)
	}
	h.mustHave("Charge it")
	if h.a.tree.sel != id {
		t.Fatal("selection lost across watcher reload")
	}

	// Right after one of our own saves the watcher echo is ignored.
	h.keys("space", "D")
	loads = h.be.loads
	h.msg(fsChangedMsg{w: fw})
	if h.be.loads != loads {
		t.Fatal("echo of our own save must not reload")
	}
}

type fakeWatcher struct{ ch chan struct{} }

func (f *fakeWatcher) Events() <-chan struct{} { return f.ch }
func (f *fakeWatcher) Close() error            { return nil }

func TestDeepCopyIsIndependent(t *testing.T) {
	ws := demo()
	cp := deepCopy(ws)
	if cp == ws || cp.Collections[0] == ws.Collections[0] {
		t.Fatal("pointers shared")
	}
	cp.Collections[0].Items[0].(*model.Folder).Items[0].(*model.Request).Name = "changed"
	cp.Environments[2].Variables[0].Value = "changed"
	if ws.Collections[0].Items[0].(*model.Folder).Items[0].ItemName() == "changed" || ws.Environments[2].Variables[0].Value == "changed" {
		t.Fatal("copy aliases the original")
	}
	if n := len(cp.Collections[0].Items[0].(*model.Folder).Items); n != 4 {
		t.Fatalf("items lost: %d", n)
	}
}
