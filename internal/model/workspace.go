package model

// Workspace is a loaded postctl workspace: collections, environments and
// globals plus workspace-level defaults. It corresponds to one directory on
// disk (see package store) and has no knowledge of where that directory is.
type Workspace struct {
	// ID identifies the workspace. The store generates one at init time.
	ID ID `yaml:"id"`
	// Name is the human-readable workspace name shown in the TUI header.
	Name string `yaml:"name"`
	// Description is free-form text (Markdown by convention).
	Description string `yaml:"description,omitempty"`
	// Defaults are workspace-wide request settings; per-request settings
	// override them field by field.
	Defaults RequestSettings `yaml:"defaults,omitempty"`
	// Extra preserves unknown workspace-level fields.
	Extra map[string]any `yaml:"extra,omitempty"`

	// Collections are in display order. Not serialized in postctl.yaml
	// itself; each collection lives in its own directory.
	Collections []*Collection `yaml:"-"`
	// Environments are in display order (sorted by name on disk).
	Environments []*Environment `yaml:"-"`
	// Globals holds the workspace-wide global variables. Nil means none.
	Globals *Globals `yaml:"-"`
}

// Collection returns the collection with the given ID, or nil.
func (w *Workspace) Collection(id ID) *Collection {
	for _, c := range w.Collections {
		if c.ID == id {
			return c
		}
	}
	return nil
}

// Environment returns the environment with the given ID, or nil.
func (w *Workspace) Environment(id ID) *Environment {
	for _, e := range w.Environments {
		if e.ID == id {
			return e
		}
	}
	return nil
}

// Collection is a top-level group of requests, equivalent to a Postman
// collection. Its Auth, Variables and Scripts apply to every descendant.
type Collection struct {
	// ID is the stable identifier (Postman: info._postman_id).
	ID ID `yaml:"id"`
	// Name is the display name (Postman: info.name).
	Name string `yaml:"name"`
	// Description is free-form text (Postman: info.description). The
	// Postman description "type" (text/markdown/html) is not preserved.
	Description string `yaml:"description,omitempty"`
	// Version is the collection version string (Postman: info.version).
	Version string `yaml:"version,omitempty"`
	// Auth is the default authentication for descendants that inherit.
	// Nil means no collection-level auth.
	Auth *Auth `yaml:"auth,omitempty"`
	// Variables are collection-scoped variables (Postman: variable[]).
	Variables []Variable `yaml:"variables,omitempty"`
	// Scripts run before/after every request in the collection.
	Scripts Scripts `yaml:"scripts,omitempty"`
	// Extra preserves unknown collection-level fields.
	Extra map[string]any `yaml:"extra,omitempty"`

	// Items are the direct children in display order: *Folder or *Request.
	Items []Item `yaml:"-"`
}

// Folder groups requests and nested folders. Its Auth, Variables and Scripts
// apply to every descendant.
type Folder struct {
	// ID is the stable identifier (Postman: item.id).
	ID ID `yaml:"id"`
	// Name is the display name.
	Name string `yaml:"name"`
	// Description is free-form text.
	Description string `yaml:"description,omitempty"`
	// Auth overrides the parent's auth for descendants that inherit. Nil
	// means inherit from the parent.
	Auth *Auth `yaml:"auth,omitempty"`
	// Variables are folder-scoped variables.
	Variables []Variable `yaml:"variables,omitempty"`
	// Scripts run before/after every request in the folder.
	Scripts Scripts `yaml:"scripts,omitempty"`
	// Extra preserves unknown folder-level fields.
	Extra map[string]any `yaml:"extra,omitempty"`

	// Items are the direct children in display order: *Folder or *Request.
	Items []Item `yaml:"-"`
}

// Item is a node in a collection tree: either *Folder or *Request. The
// interface is sealed; only this package implements it. The position of an
// item in its parent's Items slice is its order.
type Item interface {
	// ItemID returns the node's stable ID.
	ItemID() ID
	// ItemName returns the node's display name.
	ItemName() string
	isItem()
}

// Container is a node that holds Items: *Collection or *Folder.
type Container interface {
	// ContainerID returns the collection's or folder's ID.
	ContainerID() ID
	// Children returns the direct children in order. The slice is the live
	// backing slice; use the Collection tree methods to mutate structure.
	Children() []Item
	setChildren([]Item)
}

// ItemID implements [Item].
func (f *Folder) ItemID() ID { return f.ID }

// ItemName implements [Item].
func (f *Folder) ItemName() string { return f.Name }

// ItemID implements [Item].
func (r *Request) ItemID() ID { return r.ID }

// ItemName implements [Item].
func (r *Request) ItemName() string { return r.Name }

func (*Folder) isItem()  {}
func (*Request) isItem() {}

// ContainerID implements [Container].
func (c *Collection) ContainerID() ID { return c.ID }

// Children implements [Container].
func (c *Collection) Children() []Item { return c.Items }
func (c *Collection) setChildren(it []Item) { c.Items = it }

// ContainerID implements [Container].
func (f *Folder) ContainerID() ID { return f.ID }

// Children implements [Container].
func (f *Folder) Children() []Item { return f.Items }
func (f *Folder) setChildren(it []Item) { f.Items = it }
