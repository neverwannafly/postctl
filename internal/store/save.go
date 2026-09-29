package store

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/shubham/postctl/internal/model"
	"go.yaml.in/yaml/v3"
)

// writer tracks the files a save produced so leftovers can be pruned.
type writer struct {
	expected map[string]bool
	durable  bool
}

// newWriter returns a writer. Bulk saves pass durable=false: unchanged files
// are skipped, atomic rename still prevents torn files, and fsyncing thousands
// of files on import would take many seconds.
func newWriter(durable bool) *writer { return &writer{expected: map[string]bool{}, durable: durable} }

func (w *writer) file(path string, data []byte) error {
	w.expected[path] = true
	if _, err := writeFileAtomic(path, data, w.durable); err != nil {
		return &FileError{Path: path, Err: err}
	}
	return nil
}

func (w *writer) yaml(path string, v any) error {
	data, err := marshalYAML(v)
	if err != nil {
		return &FileError{Path: path, Err: err}
	}
	return w.file(path, data)
}

// scripts writes sc's sources to sibling .js files and returns a copy whose
// File fields reference them. The input is not modified.
func (w *writer) scripts(dir, preName, testName string, sc model.Scripts) (model.Scripts, error) {
	var out model.Scripts
	var err error
	if out.PreRequest, err = w.script(dir, preName, sc.PreRequest); err != nil {
		return out, err
	}
	out.Test, err = w.script(dir, testName, sc.Test)
	return out, err
}

func (w *writer) script(dir, name string, in *model.Script) (*model.Script, error) {
	if in == nil {
		return nil, nil
	}
	out := *in
	out.File = ""
	if in.Source != "" {
		if err := w.file(filepath.Join(dir, name), []byte(in.Source+"\n")); err != nil {
			return nil, err
		}
		out.File = name
	}
	return &out, nil
}

// managed reports whether the store owns a file name inside a collection.
func managed(name string) bool {
	return strings.HasSuffix(name, ".yaml") || strings.HasSuffix(name, ".pre.js") ||
		strings.HasSuffix(name, ".test.js") || name == preScript || name == testScript
}

// prune removes managed files under root that are not in expected, then
// removes directories emptied by that.
//
// With safe set (used when saving a collection from memory) prune never
// deletes a file that a lenient load would have skipped: an unparsable request
// or folder file, or one whose script file is missing, is left in place, along
// with the scripts that belong to it. Otherwise autosaving a leniently loaded
// collection would destroy the very files the user needs to fix.
func (s *Store) prune(root string, expected map[string]bool, safe bool) error {
	var stale []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && managed(d.Name()) && !expected[path] {
			stale = append(stale, path)
		}
		return nil
	})
	if err != nil {
		return err
	}
	if safe {
		stale = s.deletable(stale, expected)
	}
	dirs := map[string]bool{}
	for _, p := range stale {
		if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
			return &FileError{Path: p, Err: err}
		}
		for d := filepath.Dir(p); d != root && strings.HasPrefix(d, root); d = filepath.Dir(d) {
			dirs[d] = true
		}
	}
	// Deepest first; os.Remove refuses non-empty directories, which is what
	// protects files the store does not own.
	sorted := make([]string, 0, len(dirs))
	for d := range dirs {
		sorted = append(sorted, d)
	}
	sort.Slice(sorted, func(i, j int) bool { return len(sorted[i]) > len(sorted[j]) })
	for _, d := range sorted {
		_ = os.Remove(d)
	}
	return nil
}

// deletable filters stale files down to those that are safe to delete: valid
// nodes that are no longer in the model, and scripts of such nodes or of
// nodes that were rewritten.
func (s *Store) deletable(stale []string, expected map[string]bool) []string {
	yamlOK := map[string]bool{}
	for _, p := range stale {
		if strings.HasSuffix(p, ".yaml") {
			yamlOK[p] = s.loadable(p) && !s.underBrokenFolder(p, expected)
		}
	}
	var out []string
	for _, p := range stale {
		if strings.HasSuffix(p, ".yaml") {
			if yamlOK[p] {
				out = append(out, p)
			}
			continue
		}
		sib := siblingYAML(p)
		if !isFile(sib) || expected[sib] || yamlOK[sib] {
			out = append(out, p)
		}
	}
	return out
}

// underBrokenFolder reports whether an ancestor directory of path (below the
// collection) has a folder.yaml that a lenient load would skip; everything
// beneath such a folder was never loaded, so it is not "gone from the model".
func (s *Store) underBrokenFolder(path string, expected map[string]bool) bool {
	for d := filepath.Dir(path); ; d = filepath.Dir(d) {
		fy := filepath.Join(d, folderYAML)
		if fy != path && isFile(fy) && !expected[fy] && !s.loadable(fy) {
			return true
		}
		if isFile(filepath.Join(d, collectionYAML)) || d == filepath.Dir(d) {
			return false
		}
	}
}

// siblingYAML is the YAML file a script file belongs to.
func siblingYAML(script string) string {
	dir, name := filepath.Split(script)
	switch {
	case name == preScript || name == testScript:
		if isFile(filepath.Join(dir, folderYAML)) {
			return filepath.Join(dir, folderYAML)
		}
		return filepath.Join(dir, collectionYAML)
	case strings.HasSuffix(name, preSuffix):
		return filepath.Join(dir, strings.TrimSuffix(name, preSuffix)+".yaml")
	default:
		return filepath.Join(dir, strings.TrimSuffix(name, testSuffix)+".yaml")
	}
}

// loadable reports whether a request or folder file would load cleanly.
func (s *Store) loadable(path string) bool {
	dir := filepath.Dir(path)
	var sc *model.Scripts
	if filepath.Base(path) == folderYAML {
		var ff folderFile
		if _, err := s.decode(path, KindFolder, model.SchemaVersion, &ff); err != nil {
			return false
		}
		sc = &ff.Scripts
	} else {
		var r model.Request
		if _, err := s.decode(path, KindRequest, model.SchemaVersion, &r); err != nil {
			return false
		}
		sc = &r.Scripts
	}
	for _, x := range []*model.Script{sc.PreRequest, sc.Test} {
		if x != nil && x.File != "" && (safeRel(x.File) != nil || !isFile(filepath.Join(dir, x.File))) {
			return false
		}
	}
	return true
}

// layout planning ------------------------------------------------------------

type nodeLoc struct {
	item   model.Item
	parent model.Container
	dir    string // directory holding the node's YAML (a folder's own dir)
	stem   string // request file stem; empty for folders
}

type layout struct {
	dir   string
	nodes map[model.ID]*nodeLoc
	order map[model.ID][]string
}

func planCollection(c *model.Collection, dir string) *layout {
	l := &layout{dir: dir, nodes: map[model.ID]*nodeLoc{}, order: map[model.ID][]string{}}
	l.plan(c, dir)
	return l
}

func (l *layout) plan(parent model.Container, dir string) {
	kids := parent.Children()
	taken := map[string]bool{}
	stems := make([]string, len(kids))
	for i, it := range kids {
		if r, ok := it.(*model.Request); ok {
			stems[i] = requestStem(i, len(kids), r.Name)
			taken[stems[i]] = true
		}
	}
	order := make([]string, 0, len(kids))
	for i, it := range kids {
		switch n := it.(type) {
		case *model.Request:
			l.nodes[n.ID] = &nodeLoc{item: n, parent: parent, dir: dir, stem: stems[i]}
			order = append(order, stems[i])
		case *model.Folder:
			slug := uniqueSlug(Slug(n.Name, "folder"), taken)
			fdir := filepath.Join(dir, slug)
			l.nodes[n.ID] = &nodeLoc{item: n, parent: parent, dir: fdir}
			order = append(order, slug)
			l.plan(n, fdir)
		}
	}
	l.order[parent.ContainerID()] = order
}

// name resolution --------------------------------------------------------------

// chooseName picks the entry name for id given the existing name->ID index. An
// existing entry keeps its name while it is still the slug or a collision
// variant (slug-2, ...) of want, so unrelated saves never rename it.
func chooseName(idx map[string]model.ID, id model.ID, want string) (name, old string) {
	taken := map[string]bool{}
	for n, other := range idx {
		if other == id && old == "" {
			old = n
			continue
		}
		taken[n] = true
	}
	if old != "" && (old == want || isCollisionVariant(old, want)) {
		return old, old
	}
	return uniqueSlug(want, taken), old
}

func isCollisionVariant(name, base string) bool {
	rest, ok := strings.CutPrefix(name, base+"-")
	if !ok {
		return false
	}
	n, err := strconv.Atoi(rest)
	return err == nil && n >= 2
}

func peekID(path string) model.ID {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var p struct {
		ID string `yaml:"id"`
	}
	_ = yaml.Unmarshal(data, &p)
	return model.ID(p.ID)
}

// collectionIndex maps every directory under collections/ to its collection ID
// ("" when it has no readable collection.yaml but still occupies the name).
func (s *Store) collectionIndex() (map[string]model.ID, error) {
	entries, err := os.ReadDir(filepath.Join(s.root, collectionsDir))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return map[string]model.ID{}, nil
		}
		return nil, err
	}
	idx := map[string]model.ID{}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		id := peekID(filepath.Join(s.root, collectionsDir, e.Name(), collectionYAML))
		if id == "" && isFile(filepath.Join(s.root, collectionsDir, e.Name(), collectionYAML)) {
			id = model.DeriveID("collection", e.Name())
		}
		idx[e.Name()] = id
	}
	return idx, nil
}

// envIndex maps environment file stems to IDs.
func (s *Store) envIndex() (map[string]model.ID, error) {
	entries, err := os.ReadDir(filepath.Join(s.root, environmentsDir))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return map[string]model.ID{}, nil
		}
		return nil, err
	}
	idx := map[string]model.ID{}
	for _, e := range entries {
		stem, ok := strings.CutSuffix(e.Name(), ".yaml")
		if e.IsDir() || !ok {
			continue
		}
		id := peekID(filepath.Join(s.root, environmentsDir, e.Name()))
		if id == "" {
			id = model.DeriveID("environment", stem)
		}
		idx[stem] = id
	}
	return idx, nil
}

// saving --------------------------------------------------------------------------

func (s *Store) writeWorkspaceFile(ws *model.Workspace, order []string) error {
	wf := workspaceFile{SchemaVersion: model.SchemaVersion, Workspace: *ws, CollectionOrder: order}
	wf.Collections, wf.Environments, wf.Globals = nil, nil, nil
	data, err := marshalYAML(&wf)
	if err != nil {
		return err
	}
	path := filepath.Join(s.root, workspaceYAML)
	if _, err := writeFileAtomic(path, data, true); err != nil {
		return &FileError{Path: path, Err: err}
	}
	return nil
}

// SaveMeta writes postctl.yaml (name, description, defaults, extra) from ws,
// keeping the collection order already on disk.
func (s *Store) SaveMeta(ws *model.Workspace) error {
	if ws.ID == "" {
		return &model.ValidationError{Problems: []string{"workspace: missing id"}}
	}
	var wf workspaceFile
	if _, err := s.decode(filepath.Join(s.root, workspaceYAML), KindWorkspace, 0, &wf); err != nil {
		return err
	}
	return s.writeWorkspaceFile(ws, wf.CollectionOrder)
}

// Save synchronizes the whole workspace to disk: it writes every collection,
// environment and globals, and removes collections and environments (and
// their secrets) that are no longer in ws. Unchanged files are not rewritten.
// ws is not modified.
func (s *Store) Save(ctx context.Context, ws *model.Workspace) error {
	if err := ws.Validate(); err != nil {
		return err
	}
	for _, d := range []string{collectionsDir, environmentsDir} {
		if err := os.MkdirAll(filepath.Join(s.root, d), dirPerm); err != nil {
			return err
		}
	}

	// Remove what is gone first so its names are free for new nodes.
	keepC := map[model.ID]bool{}
	for _, c := range ws.Collections {
		keepC[c.ID] = true
	}
	cidx, err := s.collectionIndex()
	if err != nil {
		return err
	}
	for name, id := range cidx {
		// A collection.yaml that does not parse is a file the user has to
		// fix, not a deleted collection: leave it (LoadLenient reports it).
		if id != "" && !keepC[id] && s.collectionParses(name) {
			if err := s.removeCollectionDir(ctx, name); err != nil {
				return err
			}
		}
	}
	keepE := map[model.ID]bool{}
	for _, e := range ws.Environments {
		keepE[e.ID] = true
	}
	eidx, err := s.envIndex()
	if err != nil {
		return err
	}
	for stem, id := range eidx {
		if !keepE[id] && s.envParses(stem) {
			if err := s.removeEnvFile(ctx, stem); err != nil {
				return err
			}
		}
	}

	var order []string
	for _, c := range ws.Collections {
		name, _, err := s.saveCollectionFiles(ctx, c)
		if err != nil {
			return err
		}
		order = append(order, name)
	}
	for _, e := range ws.Environments {
		if err := s.SaveEnvironment(ctx, e); err != nil {
			return err
		}
	}
	if ws.Globals != nil {
		if err := s.SaveGlobals(ctx, ws.Globals); err != nil {
			return err
		}
	} else if isFile(filepath.Join(s.root, globalsYAML)) {
		if err := s.removeGlobals(ctx); err != nil {
			return err
		}
	}
	return s.writeWorkspaceFile(ws, order)
}

func (s *Store) collectionParses(dirName string) bool {
	var cf collectionFile
	_, err := s.decode(filepath.Join(s.root, collectionsDir, dirName, collectionYAML), KindCollection, 0, &cf)
	return err == nil
}

func (s *Store) envParses(stem string) bool {
	var ef environmentFile
	_, err := s.decode(filepath.Join(s.root, environmentsDir, stem+".yaml"), KindEnvironment, 0, &ef)
	return err == nil
}

// SaveCollection writes one collection, including every folder, request and
// script, and removes files of items that are no longer in it. A renamed
// collection moves to its new directory. The workspace's collection order gains
// the collection if it is new.
func (s *Store) SaveCollection(ctx context.Context, c *model.Collection) error {
	if err := c.Validate(); err != nil {
		return err
	}
	name, old, err := s.saveCollectionFiles(ctx, c)
	if err != nil {
		return err
	}
	return s.updateCollectionOrder(func(order []string) []string { return replaceOrAppend(order, old, name) })
}

func replaceOrAppend(order []string, old, name string) []string {
	for i, n := range order {
		if n == old || n == name {
			order[i] = name
			return dedupe(order)
		}
	}
	return append(order, name)
}

func dedupe(in []string) []string {
	seen := map[string]bool{}
	out := in[:0]
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

func (s *Store) updateCollectionOrder(mut func([]string) []string) error {
	var wf workspaceFile
	path := filepath.Join(s.root, workspaceYAML)
	if _, err := s.decode(path, KindWorkspace, 0, &wf); err != nil {
		return err
	}
	wf.CollectionOrder = mut(wf.CollectionOrder)
	wf.SchemaVersion = model.SchemaVersion
	data, err := marshalYAML(&wf)
	if err != nil {
		return err
	}
	if _, err := writeFileAtomic(path, data, true); err != nil {
		return &FileError{Path: path, Err: err}
	}
	return nil
}

func (s *Store) saveCollectionFiles(ctx context.Context, c *model.Collection) (name, old string, err error) {
	if err := os.MkdirAll(filepath.Join(s.root, collectionsDir), dirPerm); err != nil {
		return "", "", err
	}
	idx, err := s.collectionIndex()
	if err != nil {
		return "", "", err
	}
	name, old = chooseName(idx, c.ID, Slug(c.Name, "collection"))
	base := filepath.Join(s.root, collectionsDir)
	if old != "" && old != name {
		if err := os.Rename(filepath.Join(base, old), filepath.Join(base, name)); err != nil {
			return "", "", &FileError{Path: filepath.Join(base, old), Err: err}
		}
	}
	dir := filepath.Join(base, name)
	lay := planCollection(c, dir)
	w := newWriter(false)
	sw := newSecretWriter(ctx, s.secrets)

	if err := s.writeContainerFile(w, sw, c, lay); err != nil {
		return "", "", err
	}
	var werr error
	_ = c.Walk(func(it model.Item, _ model.Container, _ int) error {
		switch n := it.(type) {
		case *model.Folder:
			werr = s.writeContainerFile(w, sw, n, lay)
		case *model.Request:
			werr = s.writeRequest(w, sw, n, lay.nodes[n.ID])
		}
		return werr
	})
	if werr != nil {
		return "", "", werr
	}
	if err := s.prune(dir, w.expected, true); err != nil {
		return "", "", err
	}
	return name, old, nil
}

// writeContainerFile writes collection.yaml or folder.yaml plus the container's
// scripts.
func (s *Store) writeContainerFile(w *writer, sw *secretWriter, ct model.Container, lay *layout) error {
	switch n := ct.(type) {
	case *model.Collection:
		cf := collectionFile{SchemaVersion: model.SchemaVersion, Collection: *n, Order: lay.order[n.ID]}
		cf.Items = nil
		var err error
		if cf.Variables, err = sw.vars(ScopeCollection, n.ID, n.Variables); err != nil {
			return err
		}
		if cf.Auth, err = sw.auth(n.ID, n.Auth); err != nil {
			return err
		}
		if cf.Scripts, err = w.scripts(lay.dir, preScript, testScript, n.Scripts); err != nil {
			return err
		}
		return w.yaml(filepath.Join(lay.dir, collectionYAML), &cf)
	case *model.Folder:
		loc := lay.nodes[n.ID]
		ff := folderFile{Folder: *n, Order: lay.order[n.ID]}
		ff.Items = nil
		var err error
		if ff.Variables, err = sw.vars(ScopeFolder, n.ID, n.Variables); err != nil {
			return err
		}
		if ff.Auth, err = sw.auth(n.ID, n.Auth); err != nil {
			return err
		}
		if ff.Scripts, err = w.scripts(loc.dir, preScript, testScript, n.Scripts); err != nil {
			return err
		}
		return w.yaml(filepath.Join(loc.dir, folderYAML), &ff)
	}
	return fmt.Errorf("store: unsupported container %T", ct)
}

func (s *Store) writeRequest(w *writer, sw *secretWriter, r *model.Request, loc *nodeLoc) error {
	rc := *r
	var err error
	if rc.Auth, err = sw.auth(r.ID, r.Auth); err != nil {
		return err
	}
	if rc.Scripts, err = w.scripts(loc.dir, loc.stem+preSuffix, loc.stem+testSuffix, r.Scripts); err != nil {
		return err
	}
	return w.yaml(filepath.Join(loc.dir, loc.stem+".yaml"), &rc)
}

// SaveRequest persists a content-only edit of one request (and its scripts)
// without rewriting the rest of the collection; it is the cheap path for
// autosave. If the request is new, renamed, or moved (its planned file does not
// exist yet) it falls back to [Store.SaveCollection].
func (s *Store) SaveRequest(ctx context.Context, c *model.Collection, id model.ID) error {
	it, _, _, ok := c.Find(id)
	r, isReq := it.(*model.Request)
	if !ok || !isReq {
		return fmt.Errorf("%w: request %q", model.ErrNotFound, id)
	}
	if err := r.Validate(); err != nil {
		return err
	}
	idx, err := s.collectionIndex()
	if err != nil {
		return err
	}
	var dirName string
	for n, cid := range idx {
		if cid == c.ID {
			dirName = n
		}
	}
	if dirName == "" {
		return s.SaveCollection(ctx, c)
	}
	lay := planCollection(c, filepath.Join(s.root, collectionsDir, dirName))
	loc := lay.nodes[id]
	if !isFile(filepath.Join(loc.dir, loc.stem+".yaml")) {
		return s.SaveCollection(ctx, c)
	}
	w := newWriter(true)
	if err := s.writeRequest(w, newSecretWriter(ctx, s.secrets), r, loc); err != nil {
		return err
	}
	for _, name := range []string{loc.stem + preSuffix, loc.stem + testSuffix} {
		p := filepath.Join(loc.dir, name)
		if !w.expected[p] && isFile(p) {
			if err := os.Remove(p); err != nil {
				return &FileError{Path: p, Err: err}
			}
		}
	}
	return nil
}

// PathOf returns the absolute path of the YAML file for a node of a collection
// that has been saved: collection.yaml for the collection itself, folder.yaml
// for folders, and the request file for requests. Use it to open the file in
// $EDITOR; a script's file is Dir(path) joined with Script.File.
func (s *Store) PathOf(c *model.Collection, id model.ID) (string, error) {
	idx, err := s.collectionIndex()
	if err != nil {
		return "", err
	}
	for name, cid := range idx {
		if cid != c.ID {
			continue
		}
		dir := filepath.Join(s.root, collectionsDir, name)
		if id == c.ID {
			return filepath.Join(dir, collectionYAML), nil
		}
		loc, ok := planCollection(c, dir).nodes[id]
		if !ok {
			return "", fmt.Errorf("%w: %q", model.ErrNotFound, id)
		}
		if loc.stem == "" {
			return filepath.Join(loc.dir, folderYAML), nil
		}
		return filepath.Join(loc.dir, loc.stem+".yaml"), nil
	}
	return "", fmt.Errorf("%w: collection %q is not saved", model.ErrNotFound, c.ID)
}

// DeleteCollection removes a collection's files and its secrets. Files the
// store does not own are left in place.
func (s *Store) DeleteCollection(ctx context.Context, id model.ID) error {
	idx, err := s.collectionIndex()
	if err != nil {
		return err
	}
	for name, cid := range idx {
		if cid == id {
			if err := s.removeCollectionDir(ctx, name); err != nil {
				return err
			}
			return s.updateCollectionOrder(func(order []string) []string {
				out := order[:0]
				for _, n := range order {
					if n != name {
						out = append(out, n)
					}
				}
				return out
			})
		}
	}
	return fmt.Errorf("%w: collection %q", model.ErrNotFound, id)
}

func (s *Store) removeCollectionDir(ctx context.Context, name string) error {
	dir := filepath.Join(s.root, collectionsDir, name)
	// Secrets first, while the files that name them still exist. Cleanup is
	// best effort: an unreadable file or locked Keychain must not block a
	// delete.
	if c, err := s.loadCollection(dir, name); err == nil {
		s.deleteRefs(ctx, collectionRefs(c), nil)
	}
	if err := s.prune(dir, map[string]bool{}, false); err != nil {
		return err
	}
	_ = os.Remove(dir)
	return nil
}

func collectionRefs(c *model.Collection) []model.SecretRef {
	refs := append(refsInVars(c.Variables), refsInAuth(c.Auth)...)
	_ = c.Walk(func(it model.Item, _ model.Container, _ int) error {
		switch n := it.(type) {
		case *model.Folder:
			refs = append(refs, refsInVars(n.Variables)...)
			refs = append(refs, refsInAuth(n.Auth)...)
		case *model.Request:
			refs = append(refs, refsInAuth(n.Auth)...)
		}
		return nil
	})
	return refs
}

// deleteRefs removes refs from the SecretStore except those in keep.
func (s *Store) deleteRefs(ctx context.Context, refs []model.SecretRef, keep map[model.SecretRef]bool) {
	if s.secrets == nil {
		return
	}
	for _, r := range refs {
		if !keep[r] {
			_ = s.secrets.Delete(ctx, r)
		}
	}
}

// environments and globals -----------------------------------------------------------

// SaveEnvironment writes one environment. Secret values go to the SecretStore.
// A renamed environment moves to its new file name, and secrets of variables
// that were removed are deleted.
func (s *Store) SaveEnvironment(ctx context.Context, e *model.Environment) error {
	if err := e.Validate(); err != nil {
		return err
	}
	idx, err := s.envIndex()
	if err != nil {
		return err
	}
	name, old := chooseName(idx, e.ID, Slug(e.Name, "environment"))
	dir := filepath.Join(s.root, environmentsDir)
	var oldRefs []model.SecretRef
	if old != "" {
		var prev environmentFile
		if _, err := s.decode(filepath.Join(dir, old+".yaml"), KindEnvironment, 0, &prev); err == nil {
			oldRefs = refsInVars(prev.Variables)
		}
	}
	sw := newSecretWriter(ctx, s.secrets)
	ef := environmentFile{SchemaVersion: model.SchemaVersion, Environment: *e}
	if ef.Variables, err = sw.vars(ScopeEnvironment, e.ID, e.Variables); err != nil {
		return err
	}
	w := newWriter(true)
	if err := w.yaml(filepath.Join(dir, name+".yaml"), &ef); err != nil {
		return err
	}
	if old != "" && old != name {
		if err := os.Remove(filepath.Join(dir, old+".yaml")); err != nil && !errors.Is(err, os.ErrNotExist) {
			return &FileError{Path: filepath.Join(dir, old+".yaml"), Err: err}
		}
	}
	s.deleteRefs(ctx, oldRefs, sw.used)
	return nil
}

// DeleteEnvironment removes an environment file and its secrets.
func (s *Store) DeleteEnvironment(ctx context.Context, id model.ID) error {
	idx, err := s.envIndex()
	if err != nil {
		return err
	}
	for stem, eid := range idx {
		if eid == id {
			return s.removeEnvFile(ctx, stem)
		}
	}
	return fmt.Errorf("%w: environment %q", model.ErrNotFound, id)
}

func (s *Store) removeEnvFile(ctx context.Context, stem string) error {
	path := filepath.Join(s.root, environmentsDir, stem+".yaml")
	var prev environmentFile
	if _, err := s.decode(path, KindEnvironment, 0, &prev); err == nil {
		s.deleteRefs(ctx, refsInVars(prev.Variables), nil)
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return &FileError{Path: path, Err: err}
	}
	return nil
}

// SaveGlobals writes globals.yaml. Secret values go to the SecretStore.
func (s *Store) SaveGlobals(ctx context.Context, g *model.Globals) error {
	if err := g.Validate(); err != nil {
		return err
	}
	path := filepath.Join(s.root, globalsYAML)
	var oldRefs []model.SecretRef
	var prev globalsFile
	if isFile(path) {
		if _, err := s.decode(path, KindGlobals, 0, &prev); err == nil {
			oldRefs = refsInVars(prev.Variables)
		}
	}
	sw := newSecretWriter(ctx, s.secrets)
	gf := globalsFile{SchemaVersion: model.SchemaVersion, Globals: *g}
	var err error
	if gf.Variables, err = sw.vars(ScopeGlobals, g.ID, g.Variables); err != nil {
		return err
	}
	if err := newWriter(true).yaml(path, &gf); err != nil {
		return err
	}
	s.deleteRefs(ctx, oldRefs, sw.used)
	return nil
}

func (s *Store) removeGlobals(ctx context.Context) error {
	path := filepath.Join(s.root, globalsYAML)
	var prev globalsFile
	if _, err := s.decode(path, KindGlobals, 0, &prev); err == nil {
		s.deleteRefs(ctx, refsInVars(prev.Variables), nil)
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return &FileError{Path: path, Err: err}
	}
	return nil
}
