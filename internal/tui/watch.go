package tui

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/fsnotify/fsnotify"
)

// watchDebounce is the quiet period after the last change before the shell is
// told. Editors and `git pull` touch many files in a burst.
const watchDebounce = 250 * time.Millisecond

// pollInterval is used only when the OS watcher cannot cover the tree.
const pollInterval = 2 * time.Second

// watcher reports bursts of workspace file changes as single events.
type watcher interface {
	// Events delivers one value per debounced burst. It is closed by Close.
	Events() <-chan struct{}
	// Close stops watching.
	Close() error
}

// fsChangedMsg reports a debounced burst of file changes.
type fsChangedMsg struct{ w watcher }

// watchReadyMsg reports that watching started (or why it could not).
type watchReadyMsg struct {
	w   watcher
	err error
}

// startWatchCmd creates the watcher off the Update goroutine (adding watches
// touches every directory).
func startWatchCmd(root string) tea.Cmd {
	return func() tea.Msg {
		w, err := newWatcher(root)
		return watchReadyMsg{w: w, err: err}
	}
}

// waitWatchCmd blocks in a command until the next change, then re-arms from
// the handler. This is the standard Bubble Tea subscription pattern.
func waitWatchCmd(w watcher) tea.Cmd {
	return func() tea.Msg {
		if _, ok := <-w.Events(); !ok {
			return nil // closed on quit
		}
		return fsChangedMsg{w: w}
	}
}

// relevantPath reports whether a changed path can affect the workspace:
// YAML and JS files, and anything without an extension (directories). Dot
// files cover editor swap files, the store's temp files and .postctl/ state.
func relevantPath(name string) bool {
	for _, part := range strings.Split(filepath.ToSlash(name), "/") {
		if strings.HasPrefix(part, ".") && part != "." && part != ".." {
			return false
		}
	}
	base := filepath.Base(name)
	if strings.HasSuffix(base, "~") {
		return false
	}
	switch strings.ToLower(filepath.Ext(base)) {
	case ".yaml", ".yml", ".js", "":
		return true
	}
	return false
}

// newWatcher watches root with fsnotify. If the OS refuses to watch the whole
// tree (kqueue holds one descriptor per file), it falls back to polling so
// reloads still happen.
func newWatcher(root string) (watcher, error) {
	if _, err := os.Stat(root); err != nil {
		return nil, err
	}
	w, err := newFSWatcher(root)
	if err == nil {
		return w, nil
	}
	return newPollWatcher(root), nil
}

// ---- fsnotify ---------------------------------------------------------------

type fsWatcher struct {
	fw   *fsnotify.Watcher
	root string
	out  chan struct{}
	done chan struct{}
	once sync.Once
}

func newFSWatcher(root string) (*fsWatcher, error) {
	fw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	w := &fsWatcher{fw: fw, root: root, out: make(chan struct{}, 1), done: make(chan struct{})}
	if err := w.addTree(root); err != nil {
		_ = fw.Close()
		return nil, err
	}
	go w.loop()
	return w, nil
}

// addTree watches root and every directory below it, skipping dot directories.
func (w *fsWatcher) addTree(root string) error {
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil // vanished while walking
			}
			return err
		}
		if !d.IsDir() {
			return nil
		}
		if path != root && strings.HasPrefix(d.Name(), ".") {
			return fs.SkipDir
		}
		return w.fw.Add(path)
	})
}

func (w *fsWatcher) loop() {
	defer close(w.out) // the only closer, so a send can never hit a closed channel
	timer := time.NewTimer(time.Hour)
	timer.Stop()
	pending := false
	for {
		select {
		case <-w.done:
			timer.Stop()
			return
		case ev, ok := <-w.fw.Events:
			if !ok {
				return
			}
			if ev.Op == fsnotify.Chmod || !relevantPath(strings.TrimPrefix(ev.Name, w.root)) {
				continue
			}
			if ev.Has(fsnotify.Create) {
				if st, err := os.Stat(ev.Name); err == nil && st.IsDir() {
					_ = w.addTree(ev.Name) // a new folder arrived (git pull)
				}
			}
			pending = true
			timer.Reset(watchDebounce)
		case <-w.fw.Errors:
			// Overflow or a vanished directory: reload to be safe.
			pending = true
			timer.Reset(watchDebounce)
		case <-timer.C:
			if pending {
				pending = false
				select {
				case w.out <- struct{}{}:
				default:
				}
			}
		}
	}
}

// Events implements the interface it is used through.
func (w *fsWatcher) Events() <-chan struct{} { return w.out }

// Close implements the interface it is used through.
func (w *fsWatcher) Close() error {
	var err error
	w.once.Do(func() {
		close(w.done)
		err = w.fw.Close()
	})
	return err
}

// ---- polling fallback ---------------------------------------------------------

type pollWatcher struct {
	root string
	out  chan struct{}
	done chan struct{}
	once sync.Once
}

func newPollWatcher(root string) *pollWatcher {
	p := &pollWatcher{root: root, out: make(chan struct{}, 1), done: make(chan struct{})}
	go p.loop()
	return p
}

// signature summarizes relevant files by path, size and modification time.
func signature(root string) (n int, sum int64) {
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if path != root && strings.HasPrefix(d.Name(), ".") {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() || !relevantPath(d.Name()) {
			return nil
		}
		if info, err := d.Info(); err == nil {
			n++
			sum += info.ModTime().UnixNano()/1000 + info.Size()
			for _, c := range path {
				sum += int64(c)
			}
		}
		return nil
	})
	return n, sum
}

func (p *pollWatcher) loop() {
	defer close(p.out)
	tick := time.NewTicker(pollInterval)
	defer tick.Stop()
	n, sum := signature(p.root)
	for {
		select {
		case <-p.done:
			return
		case <-tick.C:
			n2, sum2 := signature(p.root)
			if n2 != n || sum2 != sum {
				n, sum = n2, sum2
				select {
				case p.out <- struct{}{}:
				default:
				}
			}
		}
	}
}

// Events implements the interface it is used through.
func (p *pollWatcher) Events() <-chan struct{} { return p.out }

// Close implements the interface it is used through.
func (p *pollWatcher) Close() error {
	p.once.Do(func() {
		close(p.done)
	})
	return nil
}
