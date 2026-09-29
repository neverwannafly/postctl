package store

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"syscall"
)

const lockName = "store.lock"

// rootMutexes holds one mutex per workspace directory for this process, so
// several Store handles (the TUI's, the engine's, a CLI helper's) over the
// same directory queue on a mutex instead of on blocking flock calls.
var (
	rootMutexesMu sync.Mutex
	rootMutexes   = map[string]*sync.Mutex{}
)

func rootMutex(root string) *sync.Mutex {
	rootMutexesMu.Lock()
	defer rootMutexesMu.Unlock()
	mu, ok := rootMutexes[root]
	if !ok {
		mu = &sync.Mutex{}
		rootMutexes[root] = mu
	}
	return mu
}

// lock takes the workspace write lock: a per-directory mutex for goroutines of
// this process plus an advisory flock on .postctl/store.lock for other
// processes (the TUI next to `postctl run`). Every method that writes
// workspace files holds it for its whole read-modify-write, so a writer never
// works from a file another writer is about to replace. It is not reentrant:
// exported writers lock and call unexported helpers that do not.
//
// The lock file lives in the gitignored, user-local state directory and is
// never deleted, which avoids the unlink race that makes lock files unsafe.
func (s *Store) lock() (unlock func(), err error) {
	mu := rootMutex(s.root)
	mu.Lock()
	dir := filepath.Join(s.root, stateDir)
	if err := os.MkdirAll(dir, dirPerm); err != nil {
		mu.Unlock()
		return nil, fmt.Errorf("store lock: %w", err)
	}
	f, err := os.OpenFile(filepath.Join(dir, lockName), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		mu.Unlock()
		return nil, fmt.Errorf("store lock: %w", err)
	}
	for {
		err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX)
		if !errors.Is(err, syscall.EINTR) {
			break
		}
	}
	if err != nil {
		_ = f.Close()
		mu.Unlock()
		return nil, fmt.Errorf("store lock: %w", err)
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
		mu.Unlock()
	}, nil
}
