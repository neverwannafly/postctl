package tui

import (
	"context"
	"errors"
	"reflect"

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

// lenientLoader is what WS0.1 adds to the store. Both plausible signatures are
// probed with type assertions so this file compiles before and after it
// lands; until then Load is used and a broken file fails the whole load.
type (
	lenientLoader interface {
		LoadLenient() (*model.Workspace, []*store.FileError, error)
	}
	lenientLoaderCtx interface {
		LoadLenient(ctx context.Context) (*model.Workspace, []*store.FileError, error)
	}
)

func (b *storeBackend) Load(ctx context.Context) (*model.Workspace, []*store.FileError, error) {
	s, err := b.open()
	if err != nil {
		return nil, nil, err
	}
	switch l := any(s).(type) {
	case lenientLoaderCtx:
		return l.LoadLenient(ctx)
	case lenientLoader:
		return l.LoadLenient()
	}
	ws, err := s.Load()
	return ws, nil, err
}

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

func (b *storeBackend) SaveCollection(ctx context.Context, c *model.Collection) error {
	s, err := b.open()
	if err != nil {
		return err
	}
	return s.SaveCollection(ctx, c)
}

func (b *storeBackend) DeleteCollection(ctx context.Context, id model.ID) error {
	s, err := b.open()
	if err != nil {
		return err
	}
	return s.DeleteCollection(ctx, id)
}

func (b *storeBackend) SaveRequest(ctx context.Context, c *model.Collection, id model.ID) error {
	s, err := b.open()
	if err != nil {
		return err
	}
	return s.SaveRequest(ctx, c, id)
}

func (b *storeBackend) SaveEnvironment(ctx context.Context, e *model.Environment) error {
	s, err := b.open()
	if err != nil {
		return err
	}
	return s.SaveEnvironment(ctx, e)
}

func (b *storeBackend) LoadState() (store.State, error) {
	s, err := b.open()
	if err != nil {
		return store.State{}, err
	}
	return s.LoadState()
}

func (b *storeBackend) SaveState(st store.State) error {
	s, err := b.open()
	if err != nil {
		return err
	}
	return s.SaveState(st)
}

// deepCopy returns an independent copy of v. Saves run on another goroutine,
// so they always work on a snapshot taken on the Update goroutine; that also
// keeps a later in-place edit from racing with a write in flight.
func deepCopy[T any](v T) T {
	out := copyValue(reflect.ValueOf(&v).Elem())
	return out.Interface().(T)
}

func copyValue(src reflect.Value) reflect.Value {
	switch src.Kind() {
	case reflect.Pointer:
		if src.IsNil() {
			return src
		}
		dst := reflect.New(src.Type().Elem())
		dst.Elem().Set(copyValue(src.Elem()))
		return dst
	case reflect.Interface:
		if src.IsNil() {
			return src
		}
		dst := reflect.New(src.Type()).Elem()
		dst.Set(copyValue(src.Elem()))
		return dst
	case reflect.Slice:
		if src.IsNil() {
			return src
		}
		dst := reflect.MakeSlice(src.Type(), src.Len(), src.Len())
		for i := 0; i < src.Len(); i++ {
			dst.Index(i).Set(copyValue(src.Index(i)))
		}
		return dst
	case reflect.Map:
		if src.IsNil() {
			return src
		}
		dst := reflect.MakeMapWithSize(src.Type(), src.Len())
		for _, k := range src.MapKeys() {
			dst.SetMapIndex(k, copyValue(src.MapIndex(k)))
		}
		return dst
	case reflect.Struct:
		dst := reflect.New(src.Type()).Elem()
		dst.Set(src) // unexported fields are copied shallowly
		for i := 0; i < src.NumField(); i++ {
			if dst.Field(i).CanSet() {
				dst.Field(i).Set(copyValue(src.Field(i)))
			}
		}
		return dst
	default:
		return src
	}
}
