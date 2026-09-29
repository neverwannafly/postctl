package tuiengine

import (
	"context"

	"github.com/shubham/postctl/internal/engine"
	"github.com/shubham/postctl/internal/tui"
)

// Runner implements [tui.CollectionRunner] on an [engine.Engine].
type Runner struct {
	engine *engine.Engine
}

var _ tui.CollectionRunner = (*Runner)(nil)

// NewRunner returns a Runner that uses e.
func NewRunner(e *engine.Engine) *Runner { return &Runner{engine: e} }

// RunAsync implements [tui.CollectionRunner].
func (r *Runner) RunAsync(ctx context.Context, in engine.RunInput) (<-chan engine.RunEvent, error) {
	return r.engine.RunAsync(ctx, in)
}
