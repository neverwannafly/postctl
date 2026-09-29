package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/shubham/postctl/internal/model"
	"github.com/shubham/postctl/internal/vars"
)

const persistTimeout = 30 * time.Second

// workspace is the state shared by every Engine (and every send) working on
// one workspace directory in this process. Writes are serialised by the store
// (see store.Store.UpdateEnvironmentVariables), not here.
type workspace struct {
	globalsMu   sync.Mutex
	globalsInfo os.FileInfo
	globals     *model.Globals
	globalsRead bool
}

var (
	workspacesMu sync.Mutex
	workspaces   = map[string]*workspace{}
)

func workspaceFor(root string) *workspace {
	workspacesMu.Lock()
	defer workspacesMu.Unlock()
	w, ok := workspaces[root]
	if !ok {
		w = &workspace{}
		workspaces[root] = w
	}
	return w
}

// loadGlobals returns a copy of the globals on disk, or nil when the
// workspace has none. The file is re-read only when its size or modification
// time changes.
func (e *Engine) loadGlobals() (*model.Globals, error) {
	w := e.ws
	w.globalsMu.Lock()
	defer w.globalsMu.Unlock()
	path := filepath.Join(e.cfg.Store.Root(), "globals.yaml")
	st, err := os.Stat(path)
	if os.IsNotExist(err) {
		w.globals, w.globalsInfo, w.globalsRead = nil, nil, true
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if w.globalsRead && w.globalsInfo != nil &&
		w.globalsInfo.Size() == st.Size() && w.globalsInfo.ModTime().Equal(st.ModTime()) {
		if w.globals == nil {
			return nil, nil
		}
		return w.globals.Clone(), nil
	}
	globals, err := e.cfg.Store.LoadGlobals()
	if err != nil {
		return nil, err
	}
	w.globals, w.globalsInfo, w.globalsRead = globals, st, true
	if w.globals == nil {
		return nil, nil
	}
	return w.globals.Clone(), nil
}

// invalidateGlobals forgets the cached globals after the engine wrote the
// file itself; the next send reads them back (unhydrated).
func (e *Engine) invalidateGlobals() {
	e.ws.globalsMu.Lock()
	defer e.ws.globalsMu.Unlock()
	e.ws.globalsRead = false
}

// persist writes the Global, Collection and Environment changes scripts made
// since the last persist (plan §0 R3). Nothing is written when nothing
// changed. It reports the scopes it wrote, and problems as warnings: a failed
// save never fails the send, the values stay live for the rest of the run.
func (s *session) persist(ctx context.Context, progress func(Step)) (saved, warnings []string) {
	all := s.store.Changes()
	pending := map[vars.Scope][]vars.Change{}
	for _, scope := range []vars.Scope{vars.Global, vars.Collection, vars.Environment} {
		if n := s.applied[scope]; n < len(all[scope]) {
			pending[scope] = all[scope][n:]
		}
	}
	if len(pending) == 0 {
		return nil, nil
	}
	for scope := range pending {
		s.applied[scope] = len(all[scope])
	}
	if s.noPersist {
		s.applyPendingInMemory(pending)
		return nil, nil
	}
	if progress != nil {
		progress(Step{Kind: StepSave, Text: stepSave})
	}

	pctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), persistTimeout)
	defer cancel()

	if changes, ok := pending[vars.Environment]; ok {
		if s.env == nil {
			warnings = append(warnings, "No environment is selected, so environment variable changes were not saved")
		} else if err := s.persistEnvironment(pctx, changes); err != nil {
			warnings = append(warnings, fmt.Sprintf("Could not save environment variables: %v", err))
		} else {
			saved = append(saved, "environment")
		}
	}
	if changes, ok := pending[vars.Collection]; ok {
		if err := s.persistCollection(pctx, changes); err != nil {
			warnings = append(warnings, fmt.Sprintf("Could not save collection variables: %v", err))
		} else {
			saved = append(saved, "collection")
		}
	}
	if changes, ok := pending[vars.Global]; ok {
		if err := s.persistGlobals(pctx, changes); err != nil {
			warnings = append(warnings, fmt.Sprintf("Could not save global variables: %v", err))
		} else {
			saved = append(saved, "globals")
		}
	}
	s.refreshSecrets()
	return saved, warnings
}

// The persist functions write only the keys a send changed. Each one hands
// the store a mutation, and the store applies it to the variables it has just
// re-read from disk under its workspace lock. The session's clone (taken when
// the send started) is never written back, so a variable a TUI edit or a
// concurrent `postctl run` added in the meantime survives. The session's own
// clone is updated in memory so later steps and the history redactor see the
// written values. Writes to the same key are last-writer-wins.

func (s *session) persistEnvironment(ctx context.Context, changes []vars.Change) error {
	vars.Apply(nil, s.env, nil, map[vars.Scope][]vars.Change{vars.Environment: changes})
	return s.e.cfg.Store.UpdateEnvironmentVariables(ctx, s.env.ID, func(current []model.Variable) []model.Variable {
		env := &model.Environment{Variables: current}
		vars.Apply(nil, env, nil, map[vars.Scope][]vars.Change{vars.Environment: changes})
		return env.Variables
	})
}

func (s *session) persistCollection(ctx context.Context, changes []vars.Change) error {
	vars.Apply(nil, nil, s.collection, map[vars.Scope][]vars.Change{vars.Collection: changes})
	return s.e.cfg.Store.UpdateCollectionVariables(ctx, s.collection.ID, func(current []model.Variable) []model.Variable {
		c := &model.Collection{Variables: current}
		vars.Apply(nil, nil, c, map[vars.Scope][]vars.Change{vars.Collection: changes})
		return c.Variables
	})
}

func (s *session) persistGlobals(ctx context.Context, changes []vars.Change) error {
	workspace := &model.Workspace{Globals: s.globals}
	vars.Apply(workspace, nil, nil, map[vars.Scope][]vars.Change{vars.Global: changes})
	s.globals = workspace.Globals
	if err := s.e.cfg.Store.UpdateGlobalsVariables(ctx, func(current []model.Variable) []model.Variable {
		g := &model.Workspace{Globals: &model.Globals{Variables: current}}
		vars.Apply(g, nil, nil, map[vars.Scope][]vars.Change{vars.Global: changes})
		return g.Globals.Variables
	}); err != nil {
		return err
	}
	s.e.invalidateGlobals()
	return nil
}

// applyPendingInMemory merges script variable writes into the session's model
// clones without touching disk. The live vars store already holds the values
// for the next request; this keeps env/collection/globals snapshots aligned.
func (s *session) applyPendingInMemory(pending map[vars.Scope][]vars.Change) {
	if changes, ok := pending[vars.Environment]; ok && s.env != nil {
		vars.Apply(nil, s.env, nil, map[vars.Scope][]vars.Change{vars.Environment: changes})
	}
	if changes, ok := pending[vars.Collection]; ok {
		vars.Apply(nil, nil, s.collection, map[vars.Scope][]vars.Change{vars.Collection: changes})
	}
	if changes, ok := pending[vars.Global]; ok {
		workspace := &model.Workspace{Globals: s.globals}
		vars.Apply(workspace, nil, nil, map[vars.Scope][]vars.Change{vars.Global: changes})
		s.globals = workspace.Globals
	}
}
