package tui

import (
	"context"
	"errors"

	"github.com/shubham/postctl/internal/model"
	"github.com/shubham/postctl/internal/store"
)

// Backend is everything the shell needs from persistence. Every method may
// block on the disk, so the shell only calls it from commands, never from
// Update or View. [NewStoreBackend] adapts a workspace directory; tests use a
// fake.
type Backend interface {
	// Root is the workspace directory (used for display, file watching and
	// relative paths in error rows).
	Root() string
	// Load reads the workspace leniently: files that fail to load are
	// returned as problems instead of failing the whole load. It returns
	// [store.ErrNotWorkspace] when Root has no workspace yet.
	Load(ctx context.Context) (*model.Workspace, []*store.FileError, error)
	// Init creates an empty workspace at Root.
	Init(ctx context.Context, name string) error
	// SaveCollection writes one collection (and removes files of items that
	// are gone from it).
	SaveCollection(ctx context.Context, c *model.Collection) error
	// DeleteCollection removes a collection from disk.
	DeleteCollection(ctx context.Context, id model.ID) error
	// SaveRequest writes one request of a collection (WS6 autosave).
	SaveRequest(ctx context.Context, c *model.Collection, id model.ID) error
	// SaveEnvironment writes one environment (WS6 environment editor).
	SaveEnvironment(ctx context.Context, e *model.Environment) error
	// LoadState reads local UI state (active environment).
	LoadState() (store.State, error)
	// SaveState writes local UI state.
	SaveState(st store.State) error
}

// storeBackend adapts *store.Store. The store is opened lazily so a directory
// without postctl.yaml can still be shown as a first-run screen.
type storeBackend struct {
	root string
	opts []store.Option
	s    *store.Store
}

// NewStoreBackend returns a Backend for the workspace directory root. The
// directory does not need to be initialized yet.
func NewStoreBackend(root string, opts ...store.Option) Backend {
	b := &storeBackend{root: root, opts: opts}
	if s, err := store.Open(root, opts...); err == nil {
		b.s = s
	}
	return b
}

// Root implements the interface it is used through.
func (b *storeBackend) Root() string { return b.root }

// open returns the store, opening it if the workspace appeared after start.
func (b *storeBackend) open() (*store.Store, error) {
	if b.s != nil {
		return b.s, nil
	}
	s, err := store.Open(b.root, b.opts...)
	if err != nil {
		return nil, err
	}
	b.s = s
	return s, nil
}

// Load implements the interface it is used through.
func (b *storeBackend) Load(_ context.Context) (*model.Workspace, []*store.FileError, error) {
	s, err := b.open()
	if err != nil {
		return nil, nil, err
	}
	return s.LoadLenient()
}

// Init implements the interface it is used through.
func (b *storeBackend) Init(_ context.Context, name string) error {
	s, err := store.Init(b.root, name, b.opts...)
	if err != nil {
		if errors.Is(err, store.ErrExists) {
			_, oerr := b.open()
			return oerr
		}
		return err
	}
	b.s = s
	return nil
}

// SaveCollection implements the interface it is used through.
func (b *storeBackend) SaveCollection(ctx context.Context, c *model.Collection) error {
	s, err := b.open()
	if err != nil {
		return err
	}
	return s.SaveCollection(ctx, c)
}

// DeleteCollection implements the interface it is used through.
func (b *storeBackend) DeleteCollection(ctx context.Context, id model.ID) error {
	s, err := b.open()
	if err != nil {
		return err
	}
	return s.DeleteCollection(ctx, id)
}

// SaveRequest implements the interface it is used through.
func (b *storeBackend) SaveRequest(ctx context.Context, c *model.Collection, id model.ID) error {
	s, err := b.open()
	if err != nil {
		return err
	}
	return s.SaveRequest(ctx, c, id)
}

// SaveEnvironment implements the interface it is used through.
func (b *storeBackend) SaveEnvironment(ctx context.Context, e *model.Environment) error {
	s, err := b.open()
	if err != nil {
		return err
	}
	return s.SaveEnvironment(ctx, e)
}

// LoadState implements the interface it is used through.
func (b *storeBackend) LoadState() (store.State, error) {
	s, err := b.open()
	if err != nil {
		return store.State{}, err
	}
	return s.LoadState()
}

// SaveState implements the interface it is used through.
func (b *storeBackend) SaveState(st store.State) error {
	s, err := b.open()
	if err != nil {
		return err
	}
	return s.SaveState(st)
}

// DeleteEnvironment implements [EnvironmentDeleter].
func (b *storeBackend) DeleteEnvironment(ctx context.Context, id model.ID) error {
	s, err := b.open()
	if err != nil {
		return err
	}
	return s.DeleteEnvironment(ctx, id)
}

// SaveGlobals implements [GlobalsSaver].
func (b *storeBackend) SaveGlobals(ctx context.Context, g *model.Globals) error {
	s, err := b.open()
	if err != nil {
		return err
	}
	return s.SaveGlobals(ctx, g)
}

// HydrateVariables implements [Hydrator]: it reads secret values from the
// secret store into vars. Refs with no stored value stay empty.
func (b *storeBackend) HydrateVariables(ctx context.Context, vars []model.Variable) error {
	s, err := b.open()
	if err != nil {
		return err
	}
	_, err = s.HydrateVariables(ctx, vars)
	return err
}

// HydrateAuth implements [Hydrator] for OAuth 2.0 token secrets.
func (b *storeBackend) HydrateAuth(ctx context.Context, a *model.Auth) error {
	s, err := b.open()
	if err != nil {
		return err
	}
	_, err = s.HydrateAuth(ctx, a)
	return err
}

// PathOf implements [PathResolver].
func (b *storeBackend) PathOf(c *model.Collection, id model.ID) (string, error) {
	s, err := b.open()
	if err != nil {
		return "", err
	}
	return s.PathOf(c, id)
}
