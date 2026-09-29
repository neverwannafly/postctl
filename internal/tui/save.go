package tui

import (
	"context"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/shubham/postctl/internal/model"
)

// saveOp is one queued disk write: save a snapshot of a collection, or delete
// a collection. Snapshots are deep copies taken on the Update goroutine, so
// the write never races with later edits.
type saveOp struct {
	id     model.ID
	name   string
	snap   *model.Collection // nil for a delete
	delete bool
}

// saveQueue serializes writes. Edits are applied to the in-memory workspace
// immediately (the UI never waits for the disk); one command at a time then
// writes them, and consecutive saves of the same collection coalesce.
type saveQueue struct {
	ops     []saveOp
	running bool
	failed  bool // the last write failed; in-memory state is ahead of disk
}

// busy reports whether writes are pending or in flight.
func (q *saveQueue) busy() bool { return q.running || len(q.ops) > 0 }

// queueSave snapshots c and schedules a write.
func (a *app) queueSave(c *model.Collection) tea.Cmd {
	op := saveOp{id: c.ID, name: c.Name, snap: deepCopy(c)}
	q := &a.saver
	// Coalesce with a not-yet-started save of the same collection.
	start := 0
	if q.running {
		start = 1
	}
	for i := start; i < len(q.ops); i++ {
		if q.ops[i].id == c.ID && !q.ops[i].delete {
			q.ops[i] = op
			return a.pump()
		}
	}
	q.ops = append(q.ops, op)
	return a.pump()
}

// queueDelete schedules removal of a collection from disk.
func (a *app) queueDelete(id model.ID) tea.Cmd {
	a.saver.ops = append(a.saver.ops, saveOp{id: id, delete: true})
	return a.pump()
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
		var err error
		if op.delete {
			err = be.DeleteCollection(ctx, op.id)
		} else {
			err = be.SaveCollection(ctx, op.snap)
		}
		return savedMsg{op: op, err: err}
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
		if msg.op.delete {
			what = "Could not delete the collection"
		}
		cmds = append(cmds, a.toastErr(what+": "+errText(msg.err)+" Your change is still on screen but not on disk. Press Ctrl-R to discard it."))
	} else if len(q.ops) == 0 {
		q.failed = false
	}
	cmds = append(cmds, a.pump())
	if a.quitting && !q.busy() {
		cmds = append(cmds, a.quit(true))
	}
	return tea.Batch(cmds...)
}
