package tui

import (
	tea "charm.land/bubbletea/v2"
	"github.com/shubham/postctl/internal/model"
)

// jumpDepth is how many opened requests the jump list remembers.
const jumpDepth = 100

// jumpList remembers the requests the user opened so Ctrl-o and Ctrl-i can walk
// back and forward through them, like vim's jump list. It holds request IDs,
// never pointers, because reloads replace the model.
type jumpList struct {
	ids []model.ID
	pos int // index of the current entry; meaningful only when ids is not empty
}

// push records an opened request. Opening the request that is already current
// does nothing; opening another one discards the forward entries.
func (j *jumpList) push(id model.ID) {
	if id == "" {
		return
	}
	if len(j.ids) > 0 && j.ids[j.pos] == id {
		return
	}
	if len(j.ids) > 0 {
		j.ids = j.ids[:j.pos+1]
	}
	j.ids = append(j.ids, id)
	if over := len(j.ids) - jumpDepth; over > 0 {
		j.ids = append([]model.ID(nil), j.ids[over:]...)
	}
	j.pos = len(j.ids) - 1
}

// step finds the nearest entry in direction delta (-1 back, +1 forward) for
// which exists reports true and returns its ID and index. Entries that no
// longer exist are dropped on the way. The position does not move: the caller
// calls commit with the index once the request has really been opened, so a
// failed jump leaves the list where it was. It returns false when there is no
// such entry.
func (j *jumpList) step(delta int, exists func(model.ID) bool) (model.ID, int, bool) {
	if len(j.ids) == 0 || delta == 0 {
		return "", 0, false
	}
	dir := 1
	if delta < 0 {
		dir = -1
	}
	ids := append([]model.ID(nil), j.ids...)
	pos := j.pos
	i := pos + dir
	for i >= 0 && i < len(ids) {
		if exists(ids[i]) {
			j.ids, j.pos = ids, pos
			return ids[i], i, true
		}
		ids = append(ids[:i], ids[i+1:]...)
		if dir < 0 {
			pos--
			i--
		}
	}
	// Nothing to go to; keep the entries that survived the clean-up.
	j.ids, j.pos = ids, min(max(pos, 0), max(len(ids)-1, 0))
	return "", 0, false
}

// commit makes entry i (an index returned by step) the current one.
func (j *jumpList) commit(i int) {
	if i >= 0 && i < len(j.ids) {
		j.pos = i
	}
}

// recordJump pushes an opened request on the jump list.
func (a *app) recordJump(ref RequestRef) {
	if a.jumping || ref.Request == nil {
		return
	}
	a.jumps.push(ref.Request.ID)
}

// jump walks the jump list by delta and opens the request it lands on.
func (a *app) jump(delta int) tea.Cmd {
	if a.ws == nil {
		return nil
	}
	id, idx, ok := a.jumps.step(delta, func(id model.ID) bool {
		_, r := findRequest(a.ws, id)
		return r != nil
	})
	if !ok {
		if delta < 0 {
			return a.toastInfo("No earlier request. Open requests from the tree to build the list.")
		}
		return a.toastInfo("No later request.")
	}
	a.jumping = true
	defer func() { a.jumping = false }()
	if !a.tree.reveal(id) {
		return a.toastWarn("That request is not in the workspace.")
	}
	a.jumps.commit(idx)
	a.layoutPanes()
	return a.syncSelection(true)
}
