package tui

import (
	"context"
	"errors"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/shubham/postctl/internal/model"
)

// saveOp is one queued disk write. Snapshots are deep copies taken on the
// Update goroutine, so the write never races with later edits.
type saveOp struct {
	kind saveKind
	id   model.ID // collection, request or environment ID
	coll model.ID // owner of a request
	name string

	collSnap *model.Collection
	envSnap  *model.Environment
	glSnap   *model.Globals
}

// saveKind is the internal write type; it extends [SaveKind] with deletes.
type saveKind int

const (
	opCollection saveKind = iota
	opDeleteCollection
	opRequest
	opEnvironment
	opDeleteEnvironment
	opGlobals
)

// pub maps to the exported kind for [SaveDoneMsg].
func (o saveOp) pub() SaveKind {
	switch o.kind {
	case opRequest:
		return SaveRequest
	case opEnvironment:
		return SaveEnvironment
	case opDeleteEnvironment:
		return DeleteEnvironment
	case opGlobals:
		return SaveGlobals
	}
	return SaveCollection
}

// saveQueue serializes writes. Edits are applied to the in-memory workspace
// immediately (the UI never waits for the disk); one command at a time then
// writes them, and consecutive saves of the same thing coalesce.
type saveQueue struct {
	ops     []saveOp
	running bool
	failed  bool // the last write failed; in-memory state is ahead of disk
}

// busy reports whether writes are pending or in flight.
func (q *saveQueue) busy() bool { return q.running || len(q.ops) > 0 }

// enqueue adds op, replacing a not-yet-started save of the same thing.
func (a *app) enqueue(op saveOp) tea.Cmd {
	q := &a.saver
	start := 0
	if q.running {
		start = 1
	}
	if op.kind != opDeleteCollection && op.kind != opDeleteEnvironment {
		for i := start; i < len(q.ops); i++ {
			if q.ops[i].kind == op.kind && q.ops[i].id == op.id {
				q.ops[i] = op
				return a.pump()
			}
		}
	}
	q.ops = append(q.ops, op)
	return a.pump()
}

// queueSave snapshots c and schedules a write.
func (a *app) queueSave(c *model.Collection) tea.Cmd {
	return a.enqueue(saveOp{kind: opCollection, id: c.ID, name: c.Name, collSnap: c.Clone()})
}

// queueDelete schedules removal of a collection from disk.
func (a *app) queueDelete(id model.ID) tea.Cmd {
	return a.enqueue(saveOp{kind: opDeleteCollection, id: id})
}

// onSaveMsg snapshots what a pane or screen asked to persist.
func (a *app) onSaveMsg(m SaveMsg) tea.Cmd {
	if a.ws == nil {
		return nil
	}
	switch m.Kind {
	case SaveRequest:
		c := a.ws.Collection(m.Collection)
		if c == nil {
			return nil
		}
		name := "the request"
		if it, _, _, ok := c.Find(m.ID); ok {
			name = it.ItemName()
		}
		return a.enqueue(saveOp{kind: opRequest, id: m.ID, coll: c.ID, name: name, collSnap: c.Clone()})
	case SaveCollection:
		if c := a.ws.Collection(m.Collection); c != nil {
			return a.queueSave(c)
		}
	case SaveEnvironment:
		if e := a.ws.Environment(m.ID); e != nil {
			return a.enqueue(saveOp{kind: opEnvironment, id: e.ID, name: e.Name, envSnap: e.Clone()})
		}
	case DeleteEnvironment:
		return a.enqueue(saveOp{kind: opDeleteEnvironment, id: m.ID})
	case SaveGlobals:
		if g := a.ws.Globals; g != nil {
			return a.enqueue(saveOp{kind: opGlobals, id: g.ID, name: "globals", glSnap: g.Clone()})
		}
	}
	return nil
}

// flushDirty queues a save for every request that still has unsaved edits
// (their debounce timers have not fired yet). Called before quitting.
func (a *app) flushDirty() (tea.Cmd, int) {
	var cmds []tea.Cmd
	for id, dirty := range a.tree.dirty {
		if !dirty {
			continue
		}
		if n, ok := a.tree.nodes[id]; ok && n.kind == rowRequest {
			cmds = append(cmds, a.onSaveMsg(SaveMsg{Kind: SaveRequest, Collection: n.coll.ID, ID: id}))
		}
	}
	return tea.Batch(cmds...), len(cmds)
}

// pump starts the next write if none is running.
func (a *app) pump() tea.Cmd {
	q := &a.saver
	if q.running || len(q.ops) == 0 {
		return nil
	}
	q.running = true
	op, be := q.ops[0], a.be
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		return savedMsg{op: op, err: runSaveOp(ctx, be, op)}
	}
}

var errNoBackendSupport = errors.New("this workspace backend cannot do that")

func runSaveOp(ctx context.Context, be Backend, op saveOp) error {
	switch op.kind {
	case opDeleteCollection:
		return be.DeleteCollection(ctx, op.id)
	case opRequest:
		return be.SaveRequest(ctx, op.collSnap, op.id)
	case opEnvironment:
		return be.SaveEnvironment(ctx, op.envSnap)
	case opDeleteEnvironment:
		if d, ok := be.(EnvironmentDeleter); ok {
			return d.DeleteEnvironment(ctx, op.id)
		}
		return errNoBackendSupport
	case opGlobals:
		if g, ok := be.(GlobalsSaver); ok {
			return g.SaveGlobals(ctx, op.glSnap)
		}
		return errNoBackendSupport
	default:
		return be.SaveCollection(ctx, op.collSnap)
	}
}

// savedMsg reports a finished write.
type savedMsg struct {
	op  saveOp
	err error
}

func (a *app) onSaved(msg savedMsg) tea.Cmd {
	q := &a.saver
	if len(q.ops) > 0 {
		q.ops = q.ops[1:]
	}
	q.running = false
	a.selfWriteUntil = a.now().Add(selfWriteWindow)

	var cmds []tea.Cmd
	if msg.err != nil {
		q.failed = true
		what := "Could not save " + msg.op.name
		switch msg.op.kind {
		case opDeleteCollection:
			what = "Could not delete the collection"
		case opDeleteEnvironment:
			what = "Could not delete the environment"
		}
		cmds = append(cmds, a.toastErr(what+": "+errText(msg.err)+" Your change is still on screen but not on disk. Press Ctrl-R to discard it."))
	} else if len(q.ops) == 0 {
		q.failed = false
	}
	cmds = append(cmds, a.pump(), a.forward(SaveDoneMsg{Kind: msg.op.pub(), ID: msg.op.id, Err: msg.err}))
	if a.quitting && !q.busy() {
		cmds = append(cmds, a.quit(true))
	}
	return tea.Batch(cmds...)
}

// ---- fold state -------------------------------------------------------------

const foldPersistDelay = 400 * time.Millisecond

// foldPersistMsg fires after the fold state has been quiet for a moment.
type foldPersistMsg struct{ seq int }

// syncFold schedules persisting the tree's fold state when it changed. The
// write is debounced so holding an arrow key over a folder never hits the
// disk more than once.
func (a *app) syncFold() tea.Cmd {
	if a.phase != phaseReady || a.ws == nil {
		return nil
	}
	cur := a.tree.foldedIDs()
	if sameIDs(cur, a.state.Folded) {
		return nil
	}
	a.foldSeq++
	seq := a.foldSeq
	return tea.Tick(foldPersistDelay, func(time.Time) tea.Msg { return foldPersistMsg{seq: seq} })
}

func (a *app) onFoldPersist(msg foldPersistMsg) tea.Cmd {
	if msg.seq != a.foldSeq {
		return nil
	}
	a.state.Folded = a.tree.foldedIDs()
	return a.persistState()
}

func sameIDs(x, y []model.ID) bool {
	if len(x) != len(y) {
		return false
	}
	for i := range x {
		if x[i] != y[i] {
			return false
		}
	}
	return true
}
