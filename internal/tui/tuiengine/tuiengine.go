// Package tuiengine adapts the engine to the TUI's [tui.Sender] contract. The
// shell (cmd/postctl) builds one with [New] and hands it to the TUI. The send
// path (tui.Sender) never imports the engine; only the runner types
// (internal/tui/runner.go, internal/tui/runner) use engine run events.
package tuiengine

import (
	"context"
	"fmt"

	"github.com/shubham/postctl/internal/engine"
	"github.com/shubham/postctl/internal/tui"
)

// Sender implements [tui.Sender] on top of an [engine.Engine].
type Sender struct {
	engine *engine.Engine
}

var _ tui.Sender = (*Sender)(nil)

// New returns a Sender that sends through e.
func New(e *engine.Engine) *Sender { return &Sender{engine: e} }

// Send implements [tui.Sender]. It maps the request to an [engine.SendInput],
// forwards each pipeline step to progress, and maps the [engine.Outcome] to a
// [tui.SendResult]:
//
//   - tests, console lines and the skipped flag map one to one;
//   - a script failure becomes ScriptError text naming its event;
//   - engine warnings map to Warnings;
//   - a transport, OAuth 2.0, resolution or cancellation error is Err.
func (s *Sender) Send(ctx context.Context, req tui.SendRequest, progress func(tui.SendStep)) tui.SendResult {
	var steps func(engine.Step)
	if progress != nil {
		steps = func(st engine.Step) { progress(tui.SendStep{Text: st.Text}) }
	}
	out, err := s.engine.Send(ctx, engine.SendInput{
		Collection:  req.Collection,
		Request:     req.Request,
		Environment: req.Environment,
	}, steps)
	if err != nil {
		res := tui.SendResult{Err: err}
		if req.Request != nil {
			res.RequestID = req.Request.ID
		}
		return res
	}
	return result(out)
}

func result(out *engine.Outcome) tui.SendResult {
	mask := func(s string) string {
		if out.Redactor == nil {
			return s
		}
		return out.Redactor.Text(s)
	}
	res := tui.SendResult{
		RequestID: out.RequestID,
		Started:   out.Started,
		Response:  out.Response,
		Skipped:   out.Skipped,
		Redactor:  out.Redactor,
		Err:       out.Err,
		HistoryID: out.HistoryID,
	}
	for _, warning := range out.Warnings {
		res.Warnings = append(res.Warnings, mask(warning))
	}
	for _, t := range out.Tests {
		res.Tests = append(res.Tests, tui.TestOutcome{
			Name: mask(t.Name), Passed: t.Passed, Skipped: t.Skipped, Message: mask(t.Message),
		})
	}
	for _, c := range out.Console {
		res.Console = append(res.Console, tui.ConsoleLine{Level: c.Level, Text: mask(c.Text), Event: c.Event})
	}
	if f := out.ScriptError; f != nil {
		res.ScriptError = mask(scriptErrorText(f))
	}
	return res
}

func scriptErrorText(f *engine.ScriptFailure) string {
	what := "Pre-request script"
	if f.Event == "test" {
		what = "Test script"
	}
	if f.Origin != "" && f.Origin != "request" {
		what = fmt.Sprintf("%s (%s)", what, f.Origin)
	}
	if f.TimedOut {
		return what + " timed out: " + f.Text
	}
	return what + " failed: " + f.Text
}
