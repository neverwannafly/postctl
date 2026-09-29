package engine

import (
	"context"
	"fmt"
	"sort"

	"github.com/shubham/postctl/internal/vars"
)

// Send runs the send pipeline of plan §4.1 for one request:
//
//	pre-request scripts (collection → folders → request), on the request with
//	its {{variables}} still unresolved
//	→ variable resolution and inherited auth
//	→ HTTP
//	→ test scripts (collection → folders → request)
//	→ persistence of variable writes (plan §0 R3)
//	→ history.
//
// progress, when not nil, is called from Send's goroutine with each stage.
//
// Send returns an error only when the input is unusable (no collection or
// request). Everything that goes wrong with the send itself, including
// transport failures, a missing OAuth 2.0 token, a failing pre-request script
// and cancellation, is reported in [Outcome].
func (e *Engine) Send(ctx context.Context, in SendInput, progress func(Step)) (*Outcome, error) {
	if in.Collection == nil {
		return nil, fmt.Errorf("%w: no collection", ErrInvalidInput)
	}
	if in.Request == nil {
		return nil, fmt.Errorf("%w: no request", ErrInvalidInput)
	}
	s, err := e.newSession(ctx, in.Collection, in.Environment, in.Globals, in.Data)
	if err != nil {
		// Secrets could not be read (for example a locked Keychain). The
		// request cannot be sent faithfully, so report it as the outcome.
		return &Outcome{
			RequestID:   in.Request.ID,
			RequestName: in.Request.Name,
			Iteration:   in.Iteration,
			Started:     e.cfg.Clock(),
			Err:         err,
		}, nil
	}
	if in.IterationCount > 0 {
		s.iterationCount = in.IterationCount
	}
	keys := make([]string, 0, len(in.Locals))
	for k := range in.Locals {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		s.store.Set(vars.Local, k, in.Locals[k])
	}
	req := in.Request.Clone()
	return s.execute(ctx, req, s.chainFor(req.ID), in.Iteration, progress), nil
}
