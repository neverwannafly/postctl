package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"time"

	"github.com/shubham/postctl/internal/model"
	"github.com/shubham/postctl/internal/vars"
)

const (
	persistTimeout = 30 * time.Second
	// commitLogSize bounds the commit log. A send that overlaps more than
	// this many other commits loses the oldest ones, which only matters
	// for writes to the very same variable.
	commitLogSize = 1024
	globalsOwner  = model.ID("globals")
)

// workspace is the state shared by every Engine (and every send) working on
// one workspace directory in this process.
type workspace struct {
	// mu serialises persistence, so concurrent sends never interleave their
	// read-modify-write of the same file.
	mu  sync.Mutex
	seq uint64
	log []commit

	globalsMu   sync.Mutex
	globalsInfo os.FileInfo
	globals     *model.Globals
	globalsRead bool
}

// commit is one persisted change set. Sessions replay commits made by other
// sessions since they started, so a persist adds to what is already on disk
// instead of overwriting it with a stale snapshot.
type commit struct {
	seq     uint64
	session uint64
	scope   vars.Scope
	owner   model.ID
	changes []vars.Change
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

// currentSeq is the sequence number a new session starts from.
func (w *workspace) currentSeq() uint64 {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.seq
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
	ws, _, err := e.cfg.Store.LoadLenient()
	if err != nil {
		return nil, err
	}
	w.globals, w.globalsInfo, w.globalsRead = ws.Globals, st, true
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
	if progress != nil {
		progress(Step{Kind: StepSave, Text: stepSave})
	}
	for scope := range pending {
		s.applied[scope] = len(all[scope])
	}

	w := s.e.ws
	w.mu.Lock()
	defer w.mu.Unlock()
	pctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), persistTimeout)
	defer cancel()

	var commits []commit
	if changes, ok := pending[vars.Environment]; ok {
		if s.env == nil {
			warnings = append(warnings, "No environment is selected, so environment variable changes were not saved")
		} else if err := s.persistEnvironment(pctx, changes); err != nil {
			warnings = append(warnings, fmt.Sprintf("Could not save environment variables: %v", err))
		} else {
			saved = append(saved, "environment")
			commits = append(commits, commit{scope: vars.Environment, owner: s.env.ID, changes: changes})
		}
	}
	if changes, ok := pending[vars.Collection]; ok {
		if err := s.persistCollection(pctx, changes); err != nil {
			warnings = append(warnings, fmt.Sprintf("Could not save collection variables: %v", err))
		} else {
			saved = append(saved, "collection")
			commits = append(commits, commit{scope: vars.Collection, owner: s.collection.ID, changes: changes})
		}
	}
	if changes, ok := pending[vars.Global]; ok {
		if err := s.persistGlobals(pctx, changes); err != nil {
			warnings = append(warnings, fmt.Sprintf("Could not save global variables: %v", err))
		} else {
			saved = append(saved, "globals")
			commits = append(commits, commit{scope: vars.Global, owner: globalsOwner, changes: changes})
		}
	}
	for _, c := range commits {
		w.seq++
		c.seq, c.session = w.seq, s.id
		w.log = append(w.log, c)
	}
	if extra := len(w.log) - commitLogSize; extra > 0 {
		w.log = append([]commit(nil), w.log[extra:]...)
	}
	s.seen = w.seq
	s.refreshSecrets()
	return saved, warnings
}

// replay applies the commits other sessions made to owner's scope since this
// session last looked, so this session's own changes land on top of them. The
// caller holds w.mu.
func (s *session) replay(scope vars.Scope, owner model.ID) []vars.Change {
	var out []vars.Change
	for _, c := range s.e.ws.log {
		if c.seq > s.seen && c.session != s.id && c.scope == scope && c.owner == owner {
			out = append(out, c.changes...)
		}
	}
	return out
}

func (s *session) persistEnvironment(ctx context.Context, changes []vars.Change) error {
	before := s.env.Variables
	theirs := s.replay(vars.Environment, s.env.ID)
	vars.Apply(nil, s.env, nil, map[vars.Scope][]vars.Change{vars.Environment: theirs})
	vars.Apply(nil, s.env, nil, map[vars.Scope][]vars.Change{vars.Environment: changes})
	if reflect.DeepEqual(before, s.env.Variables) {
		return nil
	}
	out := *s.env
	out.Variables = writable(s.env.Variables, changes, theirs)
	return s.e.cfg.Store.SaveEnvironment(ctx, &out)
}

func (s *session) persistCollection(ctx context.Context, changes []vars.Change) error {
	before := s.collection.Variables
	theirs := s.replay(vars.Collection, s.collection.ID)
	vars.Apply(nil, nil, s.collection, map[vars.Scope][]vars.Change{vars.Collection: theirs})
	vars.Apply(nil, nil, s.collection, map[vars.Scope][]vars.Change{vars.Collection: changes})
	if reflect.DeepEqual(before, s.collection.Variables) {
		return nil
	}
	out := *s.collection
	out.Variables = writable(s.collection.Variables, changes, theirs)
	return s.e.cfg.Store.SaveCollection(ctx, &out)
}

func (s *session) persistGlobals(ctx context.Context, changes []vars.Change) error {
	workspace := &model.Workspace{Globals: s.globals}
	var before []model.Variable
	if s.globals != nil {
		before = s.globals.Variables
	}
	theirs := s.replay(vars.Global, globalsOwner)
	vars.Apply(workspace, nil, nil, map[vars.Scope][]vars.Change{vars.Global: theirs})
	vars.Apply(workspace, nil, nil, map[vars.Scope][]vars.Change{vars.Global: changes})
	s.globals = workspace.Globals
	if s.globals == nil || reflect.DeepEqual(before, s.globals.Variables) {
		return nil
	}
	out := *s.globals
	out.Variables = writable(s.globals.Variables, changes, theirs)
	if err := s.e.cfg.Store.SaveGlobals(ctx, &out); err != nil {
		return err
	}
	s.e.invalidateGlobals()
	return nil
}

// writable returns a copy of variables safe to hand to the store. The session
// holds hydrated secret values in memory; the store would write every one of
// them back to the Keychain on each save, which is slow and pointless. Only
// the secrets a script changed keep their value, the rest are saved by
// reference.
func writable(variables []model.Variable, changes ...[]vars.Change) []model.Variable {
	changed := map[string]bool{}
	for _, list := range changes {
		for _, c := range list {
			changed[c.Key] = true
		}
	}
	out := make([]model.Variable, len(variables))
	copy(out, variables)
	for i := range out {
		if out[i].IsSecret() && out[i].SecretRef != "" && !changed[out[i].Key] {
			out[i].Value = ""
		}
	}
	return out
}
