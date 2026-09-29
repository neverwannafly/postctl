// Package fakerun is a scripted [tui.CollectionRunner] for tests and demos.
package fakerun

import (
	"context"
	"time"

	"github.com/shubham/postctl/internal/engine"
	"github.com/shubham/postctl/internal/httpx"
	"github.com/shubham/postctl/internal/model"
)

// Script is one request step in a fake run.
type Script struct {
	// Name is the request name.
	Name string
	// Method is the HTTP method shown in the live view.
	Method string
	// Steps are pipeline progress texts, in order.
	Steps []string
	// Status is the HTTP status when the request finishes.
	Status int
	// Tests passed and failed counts.
	TestsPass, TestsFail int
	// Err, when set, makes the request a transport failure.
	Err error
	// ScriptErr is recorded as a script error on the outcome.
	ScriptErr string
	// Skipped marks the request skipped.
	Skipped bool
	// Delay pauses before this request starts.
	Delay time.Duration
}

// Runner replays [Script] entries as engine run events.
type Runner struct {
	// Scripts is the sequence per iteration.
	Scripts []Script
	// Iterations defaults to 1 when zero.
	Iterations int
	// StepDelay is the pause between progress steps.
	StepDelay time.Duration
}

// RunAsync implements [tui.CollectionRunner].
func (r *Runner) RunAsync(ctx context.Context, in engine.RunInput) (<-chan engine.RunEvent, error) {
	ch := make(chan engine.RunEvent, 64)
	iters := in.Iterations
	if iters <= 0 {
		iters = 1
	}
	if r.Iterations > 0 {
		iters = r.Iterations
	}
	go func() {
		defer close(ch)
		started := time.Now()
		sum := &engine.RunSummary{Started: started, Iterations: iters}
		reqCount := len(r.Scripts)
		if reqCount == 0 {
			reqCount = 1
		}
		ch <- engine.RunStarted{
			CollectionName: in.Collection.Name,
			Requests:       reqCount,
			Iterations:     iters,
			Started:        started,
		}
		for it := 0; it < iters; it++ {
			select {
			case <-ctx.Done():
				sum.StopReason = engine.StopCancelled
				ch <- engine.RunFinished{Summary: sum}
				return
			default:
			}
			ch <- engine.IterationStarted{Iteration: it, Iterations: iters}
			failed := 0
			for i, sc := range r.Scripts {
				if sc.Delay > 0 {
					timer := time.NewTimer(sc.Delay)
					select {
					case <-ctx.Done():
						timer.Stop()
						sum.StopReason = engine.StopCancelled
						ch <- engine.RunFinished{Summary: sum}
						return
					case <-timer.C:
					}
				}
				id := model.ID("fake-" + sc.Name)
				if id == "" {
					id = model.NewID()
				}
				ch <- engine.RequestStarted{
					Iteration: it, Index: i, RequestID: id, RequestName: sc.Name,
					Path: []string{in.Collection.Name, sc.Name},
				}
				for _, step := range sc.Steps {
					select {
					case <-ctx.Done():
						sum.StopReason = engine.StopCancelled
						ch <- engine.RunFinished{Summary: sum}
						return
					default:
					}
					ch <- engine.RequestProgress{
						Iteration: it, RequestID: id,
						Step: engine.Step{Kind: engine.StepPreRequest, Text: step},
					}
					if r.StepDelay > 0 {
						timer := time.NewTimer(r.StepDelay)
						select {
						case <-ctx.Done():
							timer.Stop()
							sum.StopReason = engine.StopCancelled
							ch <- engine.RunFinished{Summary: sum}
							return
						case <-timer.C:
						}
					}
				}
				out := &engine.Outcome{
					RequestID: id,
					Started:   time.Now(),
					Path:      []string{in.Collection.Name, sc.Name},
				}
				if sc.Skipped {
					out.Skipped = true
					sum.Skipped++
					ch <- engine.RequestFinished{Iteration: it, Index: i, Outcome: out}
					continue
				}
				if sc.Err != nil {
					out.Err = sc.Err
					sum.TransportErrors++
					failed++
					sum.Failures = append(sum.Failures, engine.Failure{
						Iteration: it, RequestID: id, RequestName: sc.Name,
						Kind: engine.FailureTransport, Message: sc.Err.Error(),
					})
				} else {
					out.Response = &httpx.Response{
						StatusCode: sc.Status,
						Status:     httpStatusText(sc.Status),
						Body:       []byte(`{"ok":true}`),
					}
					for t := 0; t < sc.TestsPass; t++ {
						out.Tests = append(out.Tests, engine.TestResult{Name: "pass", Passed: true})
					}
					for t := 0; t < sc.TestsFail; t++ {
						out.Tests = append(out.Tests, engine.TestResult{Name: "fail", Passed: false, Message: "assertion failed"})
						sum.Tests.Failed++
						failed++
						sum.Failures = append(sum.Failures, engine.Failure{
							Iteration: it, RequestID: id, RequestName: sc.Name,
							Kind: engine.FailureTest, Name: "fail", Message: "assertion failed",
						})
					}
					sum.Tests.Passed += sc.TestsPass
				}
				if sc.ScriptErr != "" {
					out.ScriptError = &engine.ScriptFailure{Text: sc.ScriptErr, Event: "test"}
					sum.ScriptErrors++
					failed++
				}
				sum.Requests++
				ch <- engine.RequestFinished{Iteration: it, Index: i, Outcome: out}
				if in.Bail && failed > 0 {
					sum.StopReason = engine.StopBail
					ch <- engine.IterationFinished{Iteration: it, Requests: i + 1, Failed: failed}
					sum.TotalTime = time.Since(started)
					ch <- engine.RunFinished{Summary: sum}
					return
				}
			}
			ch <- engine.IterationFinished{Iteration: it, Requests: len(r.Scripts), Failed: failed}
		}
		sum.TotalTime = time.Since(started)
		ch <- engine.RunFinished{Summary: sum}
	}()
	return ch, nil
}

func httpStatusText(code int) string {
	if code == 0 {
		return ""
	}
	return "OK"
}
