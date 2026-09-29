// Package tui defines the collection runner contract between the runner screen
// and cmd/postctl wiring (see CollectionRunner).
package tui

import (
	"context"

	"github.com/shubham/postctl/internal/engine"
	"github.com/shubham/postctl/internal/model"
)

// CollectionRunner executes a Postman-style collection run. The runner screen
// calls it from a command; events arrive on the returned channel until it is
// closed after [engine.RunFinished].
type CollectionRunner interface {
	RunAsync(ctx context.Context, in engine.RunInput) (<-chan engine.RunEvent, error)
}

// RunScope preselects collection or folder scope when opening the runner.
type RunScope struct {
	// Collection is required.
	Collection *model.Collection
	// FolderID limits the run to one folder when non-empty.
	FolderID model.ID
}

// SelectRequestMsg asks the shell to reveal a request in the tree and open it.
type SelectRequestMsg struct {
	RequestID model.ID
}

// SendSavedRequestMsg asks the shell to send a saved request by ID using the
// active environment and current workspace variables.
type SendSavedRequestMsg struct {
	RequestID model.ID
}
