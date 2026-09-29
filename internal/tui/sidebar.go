package tui

import (
	"context"
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/shubham/postctl/internal/model"
	"github.com/shubham/postctl/internal/ui"
)

// itemKind is what the "new" prompt creates.
type itemKind int

const (
	kindRequest itemKind = iota
	kindFolder
	kindCollection
	kindWebSocket
)

func (k itemKind) String() string {
	return [...]string{"Request", "Folder", "Collection", "WebSocket request"}[k]
}

// hasKindPane reports whether a pane is registered for requests of kind, so
// creating one would open in an editor rather than the placeholder.
func (a *app) hasKindPane(kind model.Kind) bool {
	r, ok := a.request.(*kindRouter)
	if !ok {
		return false
	}
	_, ok = r.byKind[kind]
	return ok
}

// sidebarKey handles a key while the collection tree has focus.
func (a *app) sidebarKey(msg tea.KeyPressMsg) tea.Cmd {
	tv := a.tree
	if tv.filtering {
		cmd, moved := tv.filterKey(msg)
		if moved {
			return tea.Batch(cmd, a.syncSelection(false))
		}
		return cmd
	}
	k := a.keys
	switch {
	case key.Matches(msg, k.Up):
		tv.move(-1)
	case key.Matches(msg, k.Down):
		tv.move(1)
	case key.Matches(msg, k.PageUp):
		tv.move(-max(1, tv.viewRows()-1))
	case key.Matches(msg, k.PageDown):
		tv.move(max(1, tv.viewRows()-1))
	case key.Matches(msg, k.Home):
		tv.home()
	case key.Matches(msg, k.End):
		tv.end()
	case key.Matches(msg, k.Unfold):
		tv.expand()
	case key.Matches(msg, k.Fold):
		tv.collapse()
	case key.Matches(msg, k.Toggle):
		tv.toggle()
	case key.Matches(msg, k.Open):
		return a.openRow()
	case key.Matches(msg, k.Back):
		return a.sidebarBack()
	case key.Matches(msg, k.New):
		return a.openNew()
	case key.Matches(msg, k.Rename):
		return a.openRename()
	case key.Matches(msg, k.Duplicate):
		return a.duplicateSelected()
	case key.Matches(msg, k.Move):
		return a.openMove()
	case key.Matches(msg, k.Delete):
		return a.openDelete()
	case key.Matches(msg, k.MoveUp):
		return a.reorder(-1)
	case key.Matches(msg, k.MoveDown):
		return a.reorder(1)
	case key.Matches(msg, k.RunCollection):
		return a.openRunnerFromTree()
	default:
		return nil
	}
	return a.syncSelection(false)
}

// sidebarBack is Esc in the tree: clear a kept filter, or close the
// single-pane overlay.
func (a *app) sidebarBack() tea.Cmd {
	if a.tree.query != "" {
		a.tree.clearFilter()
		a.tree.rebuild()
		return a.syncSelection(false)
	}
	if a.layoutKind() == ui.LayoutSingle {
		return a.toggleSidebar()
	}
	return nil
}

// openRow is Enter on the tree.
func (a *app) openRow() tea.Cmd {
	r, ok := a.tree.current()
	if !ok {
		return nil
	}
	switch r.kind {
	case rowRequest, rowProblem:
		return a.syncSelection(true)
	case rowFolder, rowCollection:
		a.tree.toggle()
		return a.syncSelection(false)
	}
	return nil
}

// selected returns the node under the cursor, or nil for problem rows and an
// empty tree.
func (a *app) selected() *node {
	r, ok := a.tree.current()
	if !ok {
		return nil
	}
	return r.node
}

// ---- create ---------------------------------------------------------------------

func (a *app) openNew() tea.Cmd {
	kinds := []string{"Request", "Folder", "Collection"}
	note := "in " + a.newTargetLabel()
	if len(a.collections()) == 0 {
		kinds = []string{"Request", "Collection"}
		note = "in a new collection"
	}
	if a.hasKindPane(model.KindWebSocket) {
		kinds = append(kinds[:1:1], append([]string{"WebSocket"}, kinds[1:]...)...)
	}
	p, cmd := newPrompt(a, "New", note, "", kinds, func(a *app, name string, sel int) (string, tea.Cmd) {
		kind := kindRequest
		switch kinds[sel] {
		case "Folder":
			kind = kindFolder
		case "Collection":
			kind = kindCollection
		case "WebSocket":
			kind = kindWebSocket
		}
		return a.create(kind, name)
	})
	a.ov = p
	return cmd
}

// openNewWebSocket asks for a name and creates a WebSocket request.
func (a *app) openNewWebSocket() tea.Cmd {
	note := "in " + a.newTargetLabel()
	if len(a.collections()) == 0 {
		note = "in a new collection"
	}
	p, cmd := newPrompt(a, "New WebSocket request", note, "", nil, func(a *app, name string, _ int) (string, tea.Cmd) {
		return a.create(kindWebSocket, name)
	})
	a.ov = p
	return cmd
}

func (a *app) collections() []*model.Collection {
	if a.ws == nil {
		return nil
	}
	return a.ws.Collections
}

// newTargetLabel says where a new request or folder will land.
func (a *app) newTargetLabel() string {
	t := a.theme
	n := a.selected()
	if n == nil {
		cs := a.collections()
		if len(cs) == 0 {
			return "a new collection"
		}
		return cs[len(cs)-1].Name
	}
	if n.kind == rowRequest {
		return strings.Join(a.pathOf(n.parent, n.coll), " "+t.Glyphs.Arrow+" ")
	}
	return strings.Join(n.path, " "+t.Glyphs.Arrow+" ")
}

// pathOf returns the display path of a container.
func (a *app) pathOf(c model.Container, coll *model.Collection) []string {
	if n, ok := a.tree.nodes[c.ContainerID()]; ok {
		return n.path
	}
	return []string{coll.Name}
}

// create adds a request, folder or collection and queues the save. The
// returned string is a validation message for the prompt.
func (a *app) create(kind itemKind, name string) (string, tea.Cmd) {
	if a.ws == nil {
		// First run in a directory without a workspace: set it up, then create.
		return "", a.ensureWorkspace(func(a *app) tea.Cmd {
			_, cmd := a.create(kind, name)
			return cmd
		})
	}
	if kind == kindCollection {
		c := model.NewCollection(name)
		a.ws.Collections = append(a.ws.Collections, c)
		return "", a.afterEdit(c, c.ID, false)
	}

	coll, parent, index := a.insertionPoint()
	if coll == nil {
		coll = model.NewCollection("My collection")
		a.ws.Collections = append(a.ws.Collections, coll)
		parent, index = coll, -1
	}
	var item model.Item
	switch kind {
	case kindFolder:
		item = model.NewFolder(name)
	case kindWebSocket:
		item = model.NewWebSocketRequest(name, "")
	default:
		item = model.NewRequest(name, "GET", "")
	}
	if err := coll.Insert(parent.ContainerID(), index, item); err != nil {
		return "Could not add it here: " + errText(err), nil
	}
	if parent.ContainerID() != coll.ID {
		a.tree.expanded[parent.ContainerID()] = true
	}
	return "", a.afterEdit(coll, item.ItemID(), kind == kindRequest || kind == kindWebSocket)
}

// insertionPoint picks where a new request or folder goes: inside a selected
// folder or collection, or right after a selected request.
func (a *app) insertionPoint() (*model.Collection, model.Container, int) {
	n := a.selected()
	if n == nil {
		cs := a.collections()
		if len(cs) == 0 {
			return nil, nil, 0
		}
		c := cs[len(cs)-1]
		return c, c, -1
	}
	switch n.kind {
	case rowCollection:
		return n.coll, n.coll, -1
	case rowFolder:
		return n.coll, n.item.(*model.Folder), -1
	default:
		_, parent, idx, _ := n.coll.Find(n.id)
		return n.coll, parent, idx + 1
	}
}

// afterEdit re-indexes the tree, selects id, and queues the collection save.
// open moves focus into the request pane.
func (a *app) afterEdit(c *model.Collection, id model.ID, open bool) tea.Cmd {
	a.tree.clearFilter()
	a.tree.setWorkspace(a.ws, a.problems, a.be.Root())
	a.tree.reveal(id)
	a.layoutPanes()
	return tea.Batch(a.queueSave(c), a.syncSelection(open))
}

// ensureWorkspace initializes an empty directory as a workspace, loads it and
// then runs next. Nothing is created until the user asks for something.
func (a *app) ensureWorkspace(next func(a *app) tea.Cmd) tea.Cmd {
	be := a.be
	name := a.workspaceName()
	a.loadSeq++
	seq := a.loadSeq
	return func() tea.Msg {
		ctx := context.Background()
		if err := be.Init(ctx, name); err != nil {
			return loadedMsg{seq: seq, err: fmt.Errorf("could not set up a workspace in %s: %w", be.Root(), err)}
		}
		ws, problems, err := be.Load(ctx)
		return loadedMsg{seq: seq, ws: ws, problems: problems, err: err, then: next}
	}
}

// ---- rename / duplicate ------------------------------------------------------------

func (a *app) openRename() tea.Cmd {
	n := a.selected()
	if n == nil {
		return a.toastInfo("Pick a request, folder or collection to rename.")
	}
	label := "request"
	switch n.kind {
	case rowFolder:
		label = "folder"
	case rowCollection:
		label = "collection"
	}
	p, cmd := newPrompt(a, "Rename "+label, strings.Join(n.path, " "+a.theme.Glyphs.Arrow+" "), n.item0Name(), nil,
		func(a *app, name string, _ int) (string, tea.Cmd) { return "", a.rename(n.id, name) })
	a.ov = p
	return cmd
}

func (n *node) item0Name() string {
	if n.item != nil {
		return n.item.ItemName()
	}
	return n.coll.Name
}

func (a *app) rename(id model.ID, name string) tea.Cmd {
	n, ok := a.tree.nodes[id]
	if !ok {
		return nil
	}
	switch v := n.item.(type) {
	case *model.Request:
		v.Name = name
	case *model.Folder:
		v.Name = name
	default:
		n.coll.Name = name
	}
	return a.afterEdit(n.coll, id, false)
}

func (a *app) duplicateSelected() tea.Cmd {
	n := a.selected()
	if n == nil {
		return a.toastInfo("Pick a request, folder or collection to duplicate.")
	}
	if n.kind == rowCollection {
		c := n.coll.Clone()
		freshCollectionIDs(c)
		c.Name += " copy"
		at := 0
		for i, x := range a.ws.Collections {
			if x == n.coll {
				at = i + 1
			}
		}
		cs := append([]*model.Collection(nil), a.ws.Collections[:at]...)
		a.ws.Collections = append(append(cs, c), a.ws.Collections[at:]...)
		return tea.Batch(a.afterEdit(c, c.ID, false), a.toastOK("Duplicated "+n.coll.Name))
	}
	item := model.CloneItem(n.item)
	freshIDs(item)
	switch v := item.(type) {
	case *model.Request:
		v.Name += " copy"
	case *model.Folder:
		v.Name += " copy"
	}
	_, parent, idx, _ := n.coll.Find(n.id)
	if err := n.coll.Insert(parent.ContainerID(), idx+1, item); err != nil {
		return a.toastErr("Could not duplicate: " + errText(err))
	}
	return tea.Batch(a.afterEdit(n.coll, item.ItemID(), false), a.toastOK("Duplicated "+n.item.ItemName()))
}

// freshIDs gives a copied subtree new IDs and detaches it from the original's
// secrets (model rule 6): copies never share a Keychain entry.
func freshIDs(it model.Item) {
	switch v := it.(type) {
	case *model.Request:
		v.ID = model.NewID()
		clearAuthSecrets(v.Auth)
	case *model.Folder:
		v.ID = model.NewID()
		clearVarSecrets(v.Variables)
		clearAuthSecrets(v.Auth)
		for _, k := range v.Items {
			freshIDs(k)
		}
	}
}

func freshCollectionIDs(c *model.Collection) {
	c.ID = model.NewID()
	clearVarSecrets(c.Variables)
	clearAuthSecrets(c.Auth)
	for _, k := range c.Items {
		freshIDs(k)
	}
}

func clearVarSecrets(vs []model.Variable) {
	for i := range vs {
		vs[i].SecretRef = ""
	}
}

func clearAuthSecrets(a *model.Auth) {
	if a != nil && a.OAuth2 != nil {
		a.OAuth2.Token = nil
	}
}

// ---- move / reorder --------------------------------------------------------------------

func (a *app) openMove() tea.Cmd {
	n := a.selected()
	if n == nil || n.kind == rowCollection {
		return a.toastInfo("Pick a request or folder to move. Collections keep their place.")
	}
	arrow := " " + a.theme.Glyphs.Arrow + " "
	var items []pickItem
	for _, c := range a.ws.Collections {
		items = append(items, pickItem{label: c.Name, detail: "collection", payload: a.tree.nodes[c.ID]})
		_ = c.Walk(func(it model.Item, _ model.Container, _ int) error {
			f, ok := it.(*model.Folder)
			if !ok {
				return nil
			}
			dest := a.tree.nodes[f.ID]
			if n.kind == rowFolder && (f.ID == n.id || containsID(dest.anc, n.id)) {
				return model.ErrSkipChildren // cannot move a folder into itself
			}
			items = append(items, pickItem{label: strings.Join(dest.path, arrow), detail: "folder", payload: dest})
			return nil
		})
	}
	// The current parent is not a destination.
	kept := items[:0]
	for _, it := range items {
		if it.payload.(*node).id != n.parent.ContainerID() {
			kept = append(kept, it)
		}
	}
	p, cmd := newPicker(a, "Move "+n.item.ItemName()+" to", "type to find a folder or collection", "There is nowhere else to move it.", kept,
		func(a *app, it pickItem) tea.Cmd { return a.move(n.id, it.payload.(*node)) })
	a.ov = p
	return cmd
}

func containsID(ids []model.ID, id model.ID) bool {
	for _, x := range ids {
		if x == id {
			return true
		}
	}
	return false
}

// move relocates an item to the end of dest, across collections if needed.
func (a *app) move(id model.ID, dest *node) tea.Cmd {
	src, ok := a.tree.nodes[id]
	if !ok {
		return nil
	}
	destID := dest.id
	if src.coll == dest.coll {
		if err := src.coll.Move(id, destID, -1); err != nil {
			return a.toastErr("Could not move: " + errText(err))
		}
		a.tree.expanded[destID] = true
		return tea.Batch(a.afterEdit(src.coll, id, false), a.toastOK("Moved to "+strings.Join(dest.path, " "+a.theme.Glyphs.Arrow+" ")))
	}
	_, _, idx, _ := src.coll.Find(id)
	item, err := src.coll.Remove(id)
	if err != nil {
		return a.toastErr("Could not move: " + errText(err))
	}
	if err := dest.coll.Insert(destID, -1, item); err != nil {
		_ = src.coll.Insert(src.parent.ContainerID(), idx, item) // put it back
		return a.toastErr("Could not move: " + errText(err))
	}
	a.tree.expanded[destID] = true
	// Write the destination first: a crash in between then duplicates the
	// item instead of losing it.
	cmds := []tea.Cmd{a.queueSave(dest.coll)}
	a.tree.clearFilter()
	a.tree.setWorkspace(a.ws, a.problems, a.be.Root())
	a.tree.reveal(id)
	a.layoutPanes()
	cmds = append(cmds, a.queueSave(src.coll), a.syncSelection(false),
		a.toastOK("Moved to "+strings.Join(dest.path, " "+a.theme.Glyphs.Arrow+" ")))
	return tea.Batch(cmds...)
}

// reorder moves the selected item one place up or down among its siblings.
func (a *app) reorder(delta int) tea.Cmd {
	n := a.selected()
	if n == nil || n.kind == rowCollection || a.tree.query != "" {
		return nil
	}
	_, parent, idx, ok := n.coll.Find(n.id)
	if !ok {
		return nil
	}
	to := idx + delta
	if to < 0 || to >= len(parent.Children()) {
		return nil
	}
	if err := n.coll.Move(n.id, parent.ContainerID(), to); err != nil {
		return a.toastErr("Could not reorder: " + errText(err))
	}
	return a.afterEdit(n.coll, n.id, false)
}

// ---- delete ----------------------------------------------------------------------------

func (a *app) openDelete() tea.Cmd {
	r, ok := a.tree.current()
	if !ok {
		return nil
	}
	if r.kind == rowProblem {
		return a.toastInfo("postctl will not delete a file it cannot read. Fix or remove it in your editor.")
	}
	n := r.node
	if n == nil {
		return nil
	}
	kind, extra := "request", ""
	switch n.kind {
	case rowFolder:
		kind = "folder"
		extra = " and its " + plural(a.tree.counts[n.id], "request", "requests")
	case rowCollection:
		kind = "collection"
		extra = " and its " + plural(a.tree.counts[n.id], "request", "requests")
	}
	arrow := " " + a.theme.Glyphs.Arrow + " "
	body := fmt.Sprintf("Delete %s %q%s?\n%s\n\nThis removes the files from disk. If the workspace is in git you can restore them.",
		kind, n.item0Name(), extra, strings.Join(n.path, arrow))
	a.ov = &confirmOverlay{
		c:     ui.NewConfirm("Delete "+kind, body, "Delete", true),
		onYes: func(a *app) tea.Cmd { return a.delete(n.id) },
	}
	return nil
}

// delete removes a node and selects a neighbour.
func (a *app) delete(id model.ID) tea.Cmd {
	n, ok := a.tree.nodes[id]
	if !ok {
		return nil
	}
	name := n.item0Name()
	if n.kind == rowCollection {
		var next model.ID
		out := make([]*model.Collection, 0, len(a.ws.Collections))
		for i, c := range a.ws.Collections {
			if c == n.coll {
				switch {
				case i+1 < len(a.ws.Collections):
					next = a.ws.Collections[i+1].ID
				case i > 0:
					next = a.ws.Collections[i-1].ID
				}
				continue
			}
			out = append(out, c)
		}
		a.ws.Collections = out
		a.tree.clearFilter()
		a.tree.sel = next
		a.tree.setWorkspace(a.ws, a.problems, a.be.Root())
		a.layoutPanes()
		return tea.Batch(a.queueDelete(id), a.syncSelection(false), a.toastOK("Deleted "+name))
	}
	_, parent, idx, _ := n.coll.Find(id)
	next := parent.ContainerID()
	if kids := parent.Children(); len(kids) > 1 {
		if idx+1 < len(kids) {
			next = kids[idx+1].ItemID()
		} else {
			next = kids[idx-1].ItemID()
		}
	}
	if _, err := n.coll.Remove(id); err != nil {
		return a.toastErr("Could not delete: " + errText(err))
	}
	a.tree.clearFilter()
	a.tree.sel = next
	a.tree.setWorkspace(a.ws, a.problems, a.be.Root())
	a.layoutPanes()
	return tea.Batch(a.queueSave(n.coll), a.syncSelection(false), a.toastOK("Deleted "+name))
}
