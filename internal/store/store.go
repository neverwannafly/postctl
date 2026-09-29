package store

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/shubham/postctl/internal/model"
)

// Store reads and writes one workspace directory. It is safe to use from one
// goroutine at a time; callers serialize access (the TUI does so by running
// store calls as tea.Cmds).
type Store struct {
	root       string
	secrets    SecretStore
	migrations []Migration
}

// Option configures a Store.
type Option func(*Store)

// WithSecretStore sets where secret variable values and OAuth 2.0 tokens are
// kept. Without one, saving a secret value fails with [ErrNoSecretStore].
func WithSecretStore(ss SecretStore) Option { return func(s *Store) { s.secrets = ss } }

// WithMigrations registers schema migrations applied when reading older files.
func WithMigrations(m ...Migration) Option {
	return func(s *Store) { s.migrations = append(s.migrations, m...) }
}

// Open opens an existing workspace at root. It returns [ErrNotWorkspace] when
// root has no postctl.yaml.
func Open(root string, opts ...Option) (*Store, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	if !isFile(filepath.Join(abs, workspaceYAML)) {
		return nil, fmt.Errorf("%w: %s has no %s", ErrNotWorkspace, abs, workspaceYAML)
	}
	return newStore(abs, opts), nil
}

func newStore(abs string, opts []Option) *Store {
	s := &Store{root: abs}
	for _, o := range opts {
		o(s)
	}
	return s
}

// Init creates a new empty workspace at root (creating the directory if
// needed) and returns a Store for it. It fails with [ErrExists] if root
// already has a postctl.yaml. It also writes a .gitignore that excludes the
// local-state directory.
func Init(root, name string, opts ...Option) (*Store, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	if isFile(filepath.Join(abs, workspaceYAML)) {
		return nil, fmt.Errorf("%w: %s", ErrExists, abs)
	}
	if strings.TrimSpace(name) == "" {
		name = filepath.Base(abs)
	}
	s := newStore(abs, opts)
	for _, d := range []string{"", collectionsDir, environmentsDir} {
		if err := os.MkdirAll(filepath.Join(abs, d), dirPerm); err != nil {
			return nil, err
		}
	}
	ws := &model.Workspace{ID: model.NewID(), Name: name}
	if err := s.writeWorkspaceFile(ws, nil); err != nil {
		return nil, err
	}
	gi := filepath.Join(abs, ".gitignore")
	if !exists(gi) {
		if _, err := writeFileAtomic(gi, []byte(stateDir+"/\n"), true); err != nil {
			return nil, err
		}
	}
	return s, nil
}

// Root returns the absolute workspace directory.
func (s *Store) Root() string { return s.root }

// SecretStore returns the configured secret store, or nil.
func (s *Store) SecretStore() SecretStore { return s.secrets }

// StateDir returns the directory for local, non-versioned state (history,
// cookies, UI state). It is not created until something writes to it.
func (s *Store) StateDir() string { return filepath.Join(s.root, stateDir) }

// on-disk file shapes ------------------------------------------------------

type workspaceFile struct {
	SchemaVersion   int `yaml:"schemaVersion"`
	model.Workspace `yaml:",inline"`
	// CollectionOrder lists collection directory names in display order.
	CollectionOrder []string `yaml:"collections,omitempty"`
}

type collectionFile struct {
	SchemaVersion    int `yaml:"schemaVersion"`
	model.Collection `yaml:",inline"`
	// Order lists child entry names in display order.
	Order []string `yaml:"order,omitempty"`
}

type folderFile struct {
	model.Folder `yaml:",inline"`
	// Order lists child entry names in display order.
	Order []string `yaml:"order,omitempty"`
}

type environmentFile struct {
	SchemaVersion     int `yaml:"schemaVersion"`
	model.Environment `yaml:",inline"`
}

type globalsFile struct {
	SchemaVersion int `yaml:"schemaVersion"`
	model.Globals `yaml:",inline"`
}

// reading ------------------------------------------------------------------

// decode reads path into v. Kinds that carry their own schemaVersion read it
// from the file; others use inherit (the owning collection's version).
func (s *Store) decode(path string, kind FileKind, inherit int, v any) (version int, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, &FileError{Path: path, Err: err}
	}
	version = inherit
	if kind.carriesVersion() {
		if version, err = peekVersion(data); err != nil {
			return 0, &FileError{Path: path, Err: err}
		}
	}
	if kind.carriesVersion() || version != model.SchemaVersion {
		if data, err = s.migrate(kind, version, data); err != nil {
			return 0, &FileError{Path: path, Err: err}
		}
	}
	if err := unmarshalYAML(data, v); err != nil {
		return 0, &FileError{Path: path, Err: err}
	}
	return version, nil
}

// Load reads the whole workspace. It never touches the SecretStore: secret
// variables come back with an empty Value and their SecretRef set; call
// [Store.Hydrate] to fill values in.
//
// Load is strict: a malformed file fails the load with a *FileError naming it.
// Use [Store.LoadLenient] to load everything that is readable. Nodes without
// an id (hand-written files) get a deterministic derived ID that is persisted
// on the next save.
func (s *Store) Load() (*model.Workspace, error) {
	ws, _, err := (&loader{s: s}).workspace()
	return ws, err
}

// LoadLenient is [Store.Load] for interactive clients: a file that fails to
// read, parse or migrate is skipped and reported instead of failing the load,
// and everything else loads. Each problem is a *FileError naming the file.
//
// What is skipped: a broken request file, a broken folder.yaml (the folder and
// its subtree), a broken collection.yaml (the collection and its subtree), a
// broken environment or globals file, and a request whose script file is
// missing. Problems are returned in load order. The error result is non-nil
// only when the workspace itself cannot be loaded (postctl.yaml is missing or
// malformed, or a directory cannot be listed); the returned workspace is then
// nil.
func (s *Store) LoadLenient() (*model.Workspace, []*FileError, error) {
	return (&loader{s: s, lenient: true}).workspace()
}

// loader carries the strict/lenient mode and collected problems through one
// load.
type loader struct {
	s        *Store
	lenient  bool
	problems []*FileError
}

// skip reports whether err is a per-file problem that lenient mode records
// and moves past. Other errors, and every error in strict mode, propagate.
func (l *loader) skip(err error) bool {
	var fe *FileError
	if l.lenient && errors.As(err, &fe) {
		l.problems = append(l.problems, fe)
		return true
	}
	return false
}

func (l *loader) workspace() (*model.Workspace, []*FileError, error) {
	s := l.s
	var wf workspaceFile
	if _, err := s.decode(filepath.Join(s.root, workspaceYAML), KindWorkspace, 0, &wf); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil, fmt.Errorf("%w: %s", ErrNotWorkspace, s.root)
		}
		return nil, nil, err
	}
	ws := &wf.Workspace
	if ws.ID == "" {
		ws.ID = model.DeriveID("workspace", filepath.Base(s.root))
	}

	cdirs, err := s.collectionDirs()
	if err != nil {
		return nil, nil, err
	}
	for _, name := range orderEntries(cdirs, wf.CollectionOrder) {
		c, err := l.collection(filepath.Join(s.root, collectionsDir, name), name)
		if err != nil {
			if l.skip(err) {
				continue
			}
			return nil, nil, err
		}
		ws.Collections = append(ws.Collections, c)
	}

	envs, err := l.environments()
	if err != nil {
		return nil, nil, err
	}
	ws.Environments = envs

	gpath := filepath.Join(s.root, globalsYAML)
	if isFile(gpath) {
		var gf globalsFile
		if _, err := s.decode(gpath, KindGlobals, 0, &gf); err != nil {
			if !l.skip(err) {
				return nil, nil, err
			}
		} else {
			g := gf.Globals
			if g.ID == "" {
				g.ID = model.DeriveID(ws.ID, "globals")
			}
			ws.Globals = &g
		}
	}
	return ws, l.problems, nil
}

// LoadCollection reads a single collection by ID without loading the rest of
// the workspace. It returns [model.ErrNotFound] if there is none.
func (s *Store) LoadCollection(id model.ID) (*model.Collection, error) {
	idx, err := s.collectionIndex()
	if err != nil {
		return nil, err
	}
	for dir, cid := range idx {
		if cid == id {
			return s.loadCollection(filepath.Join(s.root, collectionsDir, dir), dir)
		}
	}
	return nil, fmt.Errorf("%w: collection %q", model.ErrNotFound, id)
}

func (s *Store) collectionDirs() ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(s.root, collectionsDir))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() && !strings.HasPrefix(e.Name(), ".") &&
			isFile(filepath.Join(s.root, collectionsDir, e.Name(), collectionYAML)) {
			out = append(out, e.Name())
		}
	}
	return out, nil
}

// loadCollection strictly loads one collection directory.
func (s *Store) loadCollection(dir, dirName string) (*model.Collection, error) {
	return (&loader{s: s}).collection(dir, dirName)
}

func (l *loader) collection(dir, dirName string) (*model.Collection, error) {
	s := l.s
	var cf collectionFile
	ver, err := s.decode(filepath.Join(dir, collectionYAML), KindCollection, 0, &cf)
	if err != nil {
		return nil, err
	}
	c := &cf.Collection
	if c.ID == "" {
		c.ID = model.DeriveID("collection", dirName)
	}
	if err := loadScripts(dir, preScript, testScript, &c.Scripts); err != nil {
		return nil, err
	}
	c.Items, err = l.items(dir, c.ID, cf.Order, ver)
	if err != nil {
		return nil, err
	}
	return c, nil
}

type childEntry struct {
	name  string
	isDir bool
}

func (l *loader) items(dir string, parent model.ID, order []string, ver int) ([]model.Item, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, &FileError{Path: dir, Err: err}
	}
	byKey := map[string][]childEntry{}
	var keys []string
	add := func(key string, e childEntry) {
		if _, ok := byKey[key]; !ok {
			keys = append(keys, key)
		}
		byKey[key] = append(byKey[key], e)
	}
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		switch {
		case e.IsDir():
			if isFile(filepath.Join(dir, name, folderYAML)) {
				add(name, childEntry{name: name, isDir: true})
			}
		case strings.HasSuffix(name, ".yaml") && name != collectionYAML && name != folderYAML:
			stem := strings.TrimSuffix(name, ".yaml")
			add(stem, childEntry{name: stem})
		}
	}
	var items []model.Item
	for _, key := range orderEntries(keys, order) {
		group := byKey[key]
		sort.SliceStable(group, func(i, j int) bool { return group[i].isDir && !group[j].isDir })
		for _, ce := range group {
			var it model.Item
			var err error
			if ce.isDir {
				it, err = l.folder(filepath.Join(dir, ce.name), parent, ce.name, ver)
			} else {
				it, err = l.request(dir, parent, ce.name, ver)
			}
			if err != nil {
				if l.skip(err) {
					continue
				}
				return nil, err
			}
			items = append(items, it)
		}
	}
	return items, nil
}

func (l *loader) folder(dir string, parent model.ID, name string, ver int) (*model.Folder, error) {
	s := l.s
	var ff folderFile
	if _, err := s.decode(filepath.Join(dir, folderYAML), KindFolder, ver, &ff); err != nil {
		return nil, err
	}
	f := &ff.Folder
	if f.ID == "" {
		f.ID = model.DeriveID(parent, name)
	}
	if err := loadScripts(dir, preScript, testScript, &f.Scripts); err != nil {
		return nil, err
	}
	var err error
	f.Items, err = l.items(dir, f.ID, ff.Order, ver)
	return f, err
}

func (l *loader) request(dir string, parent model.ID, stem string, ver int) (*model.Request, error) {
	s := l.s
	var r model.Request
	if _, err := s.decode(filepath.Join(dir, stem+".yaml"), KindRequest, ver, &r); err != nil {
		return nil, err
	}
	if r.ID == "" {
		r.ID = model.DeriveID(parent, stem)
	}
	if r.Kind == "" {
		r.Kind = model.KindHTTP
	}
	if err := loadScripts(dir, stem+preSuffix, stem+testSuffix, &r.Scripts); err != nil {
		return nil, err
	}
	return &r, nil
}

// loadScripts reads the .js files behind sc. A script entry without a file
// reference adopts the conventional sibling file when it exists, so a
// hand-dropped pre.js works without editing YAML.
func loadScripts(dir, preName, testName string, sc *model.Scripts) error {
	var err error
	if sc.PreRequest, err = loadScript(dir, preName, sc.PreRequest); err != nil {
		return err
	}
	sc.Test, err = loadScript(dir, testName, sc.Test)
	return err
}

func loadScript(dir, conventional string, sc *model.Script) (*model.Script, error) {
	name := conventional
	if sc != nil && sc.File != "" {
		if err := safeRel(sc.File); err != nil {
			return nil, &FileError{Path: dir, Err: err}
		}
		name = sc.File
	} else if sc != nil && sc.Src != "" || !isFile(filepath.Join(dir, conventional)) {
		return sc, nil
	}
	path := filepath.Join(dir, name)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, &FileError{Path: path, Err: err}
	}
	if sc == nil {
		sc = &model.Script{}
	}
	sc.File = name
	sc.Source = strings.TrimSuffix(string(data), "\n")
	return sc, nil
}

func (l *loader) environments() ([]*model.Environment, error) {
	s := l.s
	entries, err := os.ReadDir(filepath.Join(s.root, environmentsDir))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var out []*model.Environment
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".yaml") || strings.HasPrefix(name, ".") {
			continue
		}
		var ef environmentFile
		if _, err := s.decode(filepath.Join(s.root, environmentsDir, name), KindEnvironment, 0, &ef); err != nil {
			if l.skip(err) {
				continue
			}
			return nil, err
		}
		env := ef.Environment
		if env.ID == "" {
			env.ID = model.DeriveID("environment", strings.TrimSuffix(name, ".yaml"))
		}
		out = append(out, &env)
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := strings.ToLower(out[i].Name), strings.ToLower(out[j].Name)
		if a != b {
			return a < b
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}
