package model

import (
	"errors"
	"fmt"
)

// Errors returned by the tree operations.
var (
	// ErrNotFound means no node with the requested ID exists.
	ErrNotFound = errors.New("model: item not found")
	// ErrDuplicateID means an inserted subtree contains an ID already in
	// the collection.
	ErrDuplicateID = errors.New("model: duplicate id")
	// ErrCycle means a folder was moved into itself or one of its
	// descendants.
	ErrCycle = errors.New("model: cannot move a folder into itself")
)

// Walk visits every item below c in display order, depth first. depth is 0
// for direct children of the collection. parent is the containing collection
// or folder. Returning a non-nil error stops the walk and is returned; return
// [ErrSkipChildren] to skip a folder's children.
func (c *Collection) Walk(fn func(it Item, parent Container, depth int) error) error {
	return walk(c, 0, fn)
}

// ErrSkipChildren can be returned from a Walk callback to skip the children of
// the folder just visited.
var ErrSkipChildren = errors.New("model: skip children")

func walk(parent Container, depth int, fn func(Item, Container, int) error) error {
	for _, it := range parent.Children() {
		err := fn(it, parent, depth)
		if errors.Is(err, ErrSkipChildren) {
			continue
		}
		if err != nil {
			return err
		}
		if f, ok := it.(*Folder); ok {
			if err := walk(f, depth+1, fn); err != nil {
				return err
			}
		}
	}
	return nil
}

// Requests returns every request in the collection in display order,
// flattening folders.
func (c *Collection) Requests() []*Request {
	var out []*Request
	_ = c.Walk(func(it Item, _ Container, _ int) error {
		if r, ok := it.(*Request); ok {
			out = append(out, r)
		}
		return nil
	})
	return out
}

// Find returns the item with the given ID, its parent container and its index
// in the parent. ok is false if the ID is not in the collection.
func (c *Collection) Find(id ID) (it Item, parent Container, index int, ok bool) {
	var (
		found Item
		par   Container
		idx   int
	)
	_ = c.Walk(func(i Item, p Container, _ int) error {
		if i.ItemID() == id {
			found, par = i, p
			for n, sib := range p.Children() {
				if sib == i {
					idx = n
				}
			}
			return errStop
		}
		return nil
	})
	if found == nil {
		return nil, nil, 0, false
	}
	return found, par, idx, true
}

var errStop = errors.New("stop")

// Container returns the collection itself when id equals the collection's ID,
// otherwise the folder with that ID. ok is false if there is none.
func (c *Collection) Container(id ID) (Container, bool) {
	if id == c.ID || id == "" {
		return c, true
	}
	it, _, _, ok := c.Find(id)
	if !ok {
		return nil, false
	}
	f, isFolder := it.(*Folder)
	if !isFolder {
		return nil, false
	}
	return f, true
}

// Insert adds it under the container parentID (the collection ID, or "" for
// the collection root, or a folder ID) at index. An index outside
// [0, len(children)] appends. It fails with [ErrNotFound] if the parent is
// missing and [ErrDuplicateID] if it or any descendant reuses an ID that is
// already present.
func (c *Collection) Insert(parentID ID, index int, it Item) error {
	parent, ok := c.Container(parentID)
	if !ok {
		return fmt.Errorf("%w: parent %q", ErrNotFound, parentID)
	}
	seen := map[ID]bool{c.ID: true}
	_ = c.Walk(func(i Item, _ Container, _ int) error { seen[i.ItemID()] = true; return nil })
	var dup ID
	visit(it, func(i Item) {
		if seen[i.ItemID()] && dup == "" {
			dup = i.ItemID()
		}
		seen[i.ItemID()] = true
	})
	if dup != "" {
		return fmt.Errorf("%w: %q", ErrDuplicateID, dup)
	}
	insertAt(parent, index, it)
	return nil
}

// Remove detaches and returns the item with the given ID (with its subtree).
func (c *Collection) Remove(id ID) (Item, error) {
	it, parent, idx, ok := c.Find(id)
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrNotFound, id)
	}
	kids := parent.Children()
	parent.setChildren(append(kids[:idx:idx], kids[idx+1:]...))
	return it, nil
}

// Move relocates the item id under container newParentID (collection ID, "",
// or a folder ID) at index, interpreted after the item has been detached. An
// out-of-range index appends. Moving a folder into itself or a descendant
// returns [ErrCycle].
func (c *Collection) Move(id, newParentID ID, index int) error {
	it, _, _, ok := c.Find(id)
	if !ok {
		return fmt.Errorf("%w: %q", ErrNotFound, id)
	}
	dest, ok := c.Container(newParentID)
	if !ok {
		return fmt.Errorf("%w: parent %q", ErrNotFound, newParentID)
	}
	if f, isFolder := it.(*Folder); isFolder {
		cyc := dest.ContainerID() == f.ID
		visit(f, func(i Item) {
			if i.ItemID() == dest.ContainerID() {
				cyc = true
			}
		})
		if cyc {
			return ErrCycle
		}
	}
	if _, err := c.Remove(id); err != nil {
		return err
	}
	insertAt(dest, index, it)
	return nil
}

func insertAt(parent Container, index int, it Item) {
	kids := parent.Children()
	if index < 0 || index > len(kids) {
		index = len(kids)
	}
	out := make([]Item, 0, len(kids)+1)
	out = append(out, kids[:index]...)
	out = append(out, it)
	out = append(out, kids[index:]...)
	parent.setChildren(out)
}

// visit calls fn for it and, for folders, every descendant.
func visit(it Item, fn func(Item)) {
	fn(it)
	if f, ok := it.(*Folder); ok {
		for _, k := range f.Items {
			visit(k, fn)
		}
	}
}
