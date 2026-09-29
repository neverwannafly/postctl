package engine

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/shubham/postctl/internal/model"
	"github.com/shubham/postctl/internal/vars"
)

// RunInput configures a collection run. It follows the Postman runner.
type RunInput struct {
	// Collection is the collection to run. Required.
	Collection *model.Collection
	// FolderID limits the run to one folder (and its subfolders). Empty
	// runs the whole collection. Scripts and auth of the folder's
	// ancestors still apply.
	FolderID model.ID
	// Environment is the active environment, or nil.
	Environment *model.Environment
	// Globals are the workspace globals; nil means read globals.yaml.
	Globals *model.Globals
	// Iterations is how many times to run the scope. When zero it is the
	// number of Data rows, or one without data. When it exceeds the number
	// of rows the rows are reused in order.
	Iterations int
	// Data are the iteration-data rows (see [LoadCSV], [LoadJSON]). Row n
	// feeds the Data scope of iteration n.
	Data []map[string]any
	// Locals seeds pm.variables at the start of every iteration.
	Locals map[string]any
	// Delay is the pause between requests.
	Delay time.Duration
	// Bail stops the run after the first request that fails: a failing
	// test, a script error or a transport error.
	Bail bool
	// StopOnScriptError stops the run after the first script error.
	StopOnScriptError bool
	// NoPersist skips writing environment, collection and global variable
	// changes to disk for this run. Scripts still mutate the live variable
	// store, so later requests in the same run see the values (R3 in memory
	// only). The zero value keeps normal persistence.
	NoPersist bool
	// OnlyRequests limits the run to these request IDs, in their usual
	// depth-first order within the scope. Empty runs every request in the
	// scope. pm.execution.setNextRequest to a request outside this set
	// ends the run with [StopNextMissing].
	OnlyRequests []model.ID
}

// StopReason says why a run ended early.
type StopReason string

// Stop reasons. An empty reason means the run completed every iteration.
const (
	// StopBail is a failed request with Bail set.
	StopBail StopReason = "bail"
	// StopScriptError is a script error with StopOnScriptError set.
	StopScriptError StopReason = "script-error"
	// StopCancelled is context cancellation.
	StopCancelled StopReason = "cancelled"
	// StopNextNull is pm.execution.setNextRequest(null).
	StopNextNull StopReason = "set-next-request-null"
	// StopNextMissing is setNextRequest naming a request that is not in
	// the run's scope.
	StopNextMissing StopReason = "set-next-request-not-found"
)

// FailureKind classifies a [Failure].
type FailureKind string

// Failure kinds.
const (
	// FailureTest is a failing pm.test.
	FailureTest FailureKind = "test"
	// FailureScript is a script that threw or timed out.
	FailureScript FailureKind = "script"
	// FailureTransport is a request that could not be completed.
	FailureTransport FailureKind = "transport"
)

// Failure is one thing that went wrong in a run.
type Failure struct {
	// Iteration is the zero-based iteration.
	Iteration int
	// RequestID and RequestName identify the request.
	RequestID   model.ID
	RequestName string
	// Path is collection > folders > request.
	Path []string
	// Kind classifies the failure.
	Kind FailureKind
	// Name is the failing test's name; empty for other kinds.
	Name string
	// Message describes the failure without secret values.
	Message string
}

// RunSummary is the result of a run.
type RunSummary struct {
	// Started is when the run began.
	Started time.Time
	// TotalTime is the wall time of the run, delays included.
	TotalTime time.Duration
	// AvgResponseTime is the mean response time of the requests that got a
	// response.
	AvgResponseTime time.Duration
	// Iterations is how many iterations were started.
	Iterations int
	// Requests is how many requests were attempted (skipped ones excluded).
	Requests int
	// Skipped counts requests a pre-request script skipped.
	Skipped int
	// Tests tallies every test of every request.
	Tests TestCounts
	// ScriptErrors counts scripts that failed.
	ScriptErrors int
	// TransportErrors counts requests that got no response.
	TransportErrors int
	// Failures lists everything that failed, in run order.
	Failures []Failure
	// StopReason is empty when every iteration completed.
	StopReason StopReason
}

// ExitCode maps the summary to the CLI exit codes of plan §9: 0 when
// everything passed, 1 for failing tests or script errors, 2 for request or
// transport errors (which take precedence).
func (s *RunSummary) ExitCode() int {
	switch {
	case s.TransportErrors > 0:
		return 2
	case s.Tests.Failed > 0 || s.ScriptErrors > 0:
		return 1
	}
	return 0
}

// RunEvent is one event of a run. The concrete types are [RunStarted],
// [IterationStarted], [RequestStarted], [RequestProgress], [RequestFinished],
// [IterationFinished] and [RunFinished], always in that nesting order.
type RunEvent interface {
	runEvent()
}

// RunStarted is the first event.
type RunStarted struct {
	// CollectionID and CollectionName identify the collection.
	CollectionID   model.ID
	CollectionName string
	// FolderID and FolderName identify the folder scope, if any.
	FolderID   model.ID
	FolderName string
	// Requests is how many requests one iteration contains.
	Requests int
	// Iterations is how many iterations are planned.
	Iterations int
	// Started is the start time.
	Started time.Time
}

// IterationStarted begins an iteration.
type IterationStarted struct {
	// Iteration is zero-based.
	Iteration int
	// Iterations is the planned count.
	Iterations int
}

// RequestStarted begins one request.
type RequestStarted struct {
	// Iteration is zero-based.
	Iteration int
	// Index counts requests executed so far in this iteration, from zero.
	// setNextRequest jumps can make it exceed Requests.
	Index int
	// RequestID, RequestName and Path identify the request.
	RequestID   model.ID
	RequestName string
	Path        []string
}

// RequestProgress reports a pipeline step of the running request.
type RequestProgress struct {
	// Iteration is zero-based.
	Iteration int
	// RequestID is the running request.
	RequestID model.ID
	// Step is the stage that just began.
	Step Step
}

// RequestFinished ends one request.
type RequestFinished struct {
	// Iteration is zero-based.
	Iteration int
	// Index matches the RequestStarted event.
	Index int
	// Outcome is the full result.
	Outcome *Outcome
}

// IterationFinished ends an iteration.
type IterationFinished struct {
	// Iteration is zero-based.
	Iteration int
	// Requests is how many requests ran in it.
	Requests int
	// Failed is how many of them failed.
	Failed int
}

// RunFinished is the last event. It is always delivered, also after
// cancellation.
type RunFinished struct {
	// Summary is the final summary.
	Summary *RunSummary
}

func (RunStarted) runEvent()        {}
func (IterationStarted) runEvent()  {}
func (RequestStarted) runEvent()    {}
func (RequestProgress) runEvent()   {}
func (RequestFinished) runEvent()   {}
func (IterationFinished) runEvent() {}
func (RunFinished) runEvent()       {}

// runItem is one request of the scope, with the containers above it.
type runItem struct {
	req   *model.Request
	chain []model.Container
}

// scopeItems lists the requests of the run scope in Postman order: depth
// first, in display order. root is the whole collection; scope is the
// collection or the folder to run.
func scopeItems(root *model.Collection, scope model.Container) []runItem {
	var items []runItem
	var walk func(c model.Container, chain []model.Container, inScope bool)
	walk = func(c model.Container, chain []model.Container, inScope bool) {
		chain = append(chain[:len(chain):len(chain)], c)
		if c == scope {
			inScope = true
		}
		for _, child := range c.Children() {
			switch n := child.(type) {
			case *model.Request:
				if inScope {
					items = append(items, runItem{req: n, chain: chain})
				}
			case *model.Folder:
				walk(n, chain, inScope)
			}
		}
	}
	walk(root, nil, scope == model.Container(root))
	return items
}

// emitter delivers events without ever blocking forever on a consumer that
// has gone away, except for the final event.
type emitter struct {
	ctx context.Context
	ch  chan<- RunEvent
}

func (m emitter) emit(ev RunEvent) {
	if m.ch == nil {
		return
	}
	select {
	case m.ch <- ev:
		return
	default:
	}
	select {
	case m.ch <- ev:
	case <-m.ctx.Done():
	}
}

// final delivers the last event, waiting for the consumer.
func (m emitter) final(ev RunEvent) {
	if m.ch != nil {
		m.ch <- ev
	}
}

// Run runs a collection or folder the way the Postman runner does:
//
//   - requests run depth first, in display order, once per iteration;
//   - pm.variables (Local) is reset at the start of every iteration and the
//     iteration's data row becomes the Data scope;
//   - environment, collection and global variable writes persist as they do
//     for a single send, and later requests see them;
//   - pm.execution.setNextRequest(name or id) jumps to the first request of
//     the scope with that name or ID and continues in order from there;
//     setNextRequest(null) stops the run;
//   - Bail and StopOnScriptError end the run early.
//
// Events go to events (which may be nil) in the order documented on
// [RunEvent]; Run never closes the channel. The consumer must keep receiving
// until it sees [RunFinished]. Run returns the same summary and an error only
// for unusable input; a cancelled context ends the run with StopCancelled.
func (e *Engine) Run(ctx context.Context, in RunInput, events chan<- RunEvent) (*RunSummary, error) {
	plan, err := e.planRun(ctx, in)
	if err != nil {
		return nil, err
	}
	return plan.run(ctx, events), nil
}

// RunAsync starts a run in a goroutine and returns its event channel, which
// is buffered and closed after [RunFinished]. It is the convenient form for a
// UI: range over the channel.
func (e *Engine) RunAsync(ctx context.Context, in RunInput) (<-chan RunEvent, error) {
	plan, err := e.planRun(ctx, in)
	if err != nil {
		return nil, err
	}
	ch := make(chan RunEvent, 64)
	go func() {
		defer close(ch)
		plan.run(ctx, ch)
	}()
	return ch, nil
}

type runPlan struct {
	s          *session
	in         RunInput
	items      []runItem
	iterations int
	scopeName  string
}

func (e *Engine) planRun(ctx context.Context, in RunInput) (*runPlan, error) {
	s, err := e.newSession(ctx, in.Collection, in.Environment, in.Globals, nil)
	if err != nil {
		return nil, err
	}
	s.noPersist = in.NoPersist
	var scope model.Container = s.collection
	name := ""
	if in.FolderID != "" {
		c, ok := s.collection.Container(in.FolderID)
		if !ok {
			return nil, fmt.Errorf("%w: folder %q is not in the collection", ErrInvalidInput, in.FolderID)
		}
		scope = c
		if f, isFolder := c.(*model.Folder); isFolder {
			name = f.Name
		}
	}
	iterations := in.Iterations
	if iterations <= 0 {
		iterations = max(1, len(in.Data))
	}
	s.iterationCount = iterations
	items := scopeItems(s.collection, scope)
	if len(in.OnlyRequests) > 0 {
		items = filterRunItems(items, in.OnlyRequests)
	}
	return &runPlan{
		s:          s,
		in:         in,
		items:      items,
		iterations: iterations,
		scopeName:  name,
	}, nil
}

func filterRunItems(items []runItem, only []model.ID) []runItem {
	allowed := make(map[model.ID]struct{}, len(only))
	for _, id := range only {
		allowed[id] = struct{}{}
	}
	out := make([]runItem, 0, len(only))
	for _, it := range items {
		if _, ok := allowed[it.req.ID]; ok {
			out = append(out, it)
		}
	}
	return out
}

func (p *runPlan) run(ctx context.Context, events chan<- RunEvent) *RunSummary {
	s, in := p.s, p.in
	clock := s.e.cfg.Clock
	out := emitter{ctx: ctx, ch: events}
	sum := &RunSummary{Started: clock()}
	var responseTotal time.Duration
	var responses int

	out.emit(RunStarted{
		CollectionID: s.collection.ID, CollectionName: s.collection.Name,
		FolderID: in.FolderID, FolderName: p.scopeName,
		Requests: len(p.items), Iterations: p.iterations, Started: sum.Started,
	})

	first := true
	var stop StopReason
iterations:
	for iter := 0; iter < p.iterations; iter++ {
		if ctx.Err() != nil {
			stop = StopCancelled
			break
		}
		p.seedIteration(iter)
		sum.Iterations++
		out.emit(IterationStarted{Iteration: iter, Iterations: p.iterations})

		executed, failed := 0, 0
		for i := 0; i < len(p.items); {
			if ctx.Err() != nil {
				stop = StopCancelled
				out.emit(IterationFinished{Iteration: iter, Requests: executed, Failed: failed})
				break iterations
			}
			if !first && in.Delay > 0 && !sleep(ctx, in.Delay) {
				stop = StopCancelled
				out.emit(IterationFinished{Iteration: iter, Requests: executed, Failed: failed})
				break iterations
			}
			first = false

			item := p.items[i]
			index := executed
			executed++
			out.emit(RequestStarted{
				Iteration: iter, Index: index, RequestID: item.req.ID, RequestName: item.req.Name,
				Path: pathNames(item.chain, item.req.Name),
			})
			outcome := s.execute(ctx, item.req, item.chain, iter, func(step Step) {
				out.emit(RequestProgress{Iteration: iter, RequestID: item.req.ID, Step: step})
			})
			out.emit(RequestFinished{Iteration: iter, Index: index, Outcome: outcome})

			if outcome.Failed() {
				failed++
			}
			cancelled := ctx.Err() != nil
			tally(sum, outcome, cancelled)
			if outcome.Response != nil {
				responseTotal += outcome.Response.Timings.Total
				responses++
			}

			switch {
			case cancelled:
				stop = StopCancelled
			case in.Bail && outcome.Failed():
				stop = StopBail
			case in.StopOnScriptError && outcome.ScriptError != nil:
				stop = StopScriptError
			}
			next := i + 1
			if stop == "" && outcome.NextRequest != nil {
				target := *outcome.NextRequest
				if target == "" {
					stop = StopNextNull
				} else if idx := findRequest(p.items, target); idx >= 0 {
					next = idx
				} else {
					stop = StopNextMissing
					sum.ScriptErrors++
					sum.Failures = append(sum.Failures, Failure{
						Iteration: iter, RequestID: item.req.ID, RequestName: item.req.Name,
						Path: pathNames(item.chain, item.req.Name), Kind: FailureScript,
						Message: fmt.Sprintf("setNextRequest: no request named or identified %q in this run", target),
					})
				}
			}
			if stop != "" {
				out.emit(IterationFinished{Iteration: iter, Requests: executed, Failed: failed})
				break iterations
			}
			i = next
		}
		out.emit(IterationFinished{Iteration: iter, Requests: executed, Failed: failed})
	}

	sum.StopReason = stop
	sum.TotalTime = clock().Sub(sum.Started)
	if responses > 0 {
		sum.AvgResponseTime = responseTotal / time.Duration(responses)
	}
	out.final(RunFinished{Summary: sum})
	return sum
}

// seedIteration resets pm.variables and installs the iteration's data row.
func (p *runPlan) seedIteration(iter int) {
	store := p.s.store
	store.Clear(vars.Local)
	store.Clear(vars.Data)
	if len(p.in.Data) > 0 {
		row := p.in.Data[iter%len(p.in.Data)]
		for _, k := range sortedKeys(row) {
			store.Set(vars.Data, k, row[k])
		}
	}
	for _, k := range sortedKeys(p.in.Locals) {
		store.Set(vars.Local, k, p.in.Locals[k])
	}
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// findRequest returns the index of the first request whose name or ID is
// target, or -1. Names win over IDs, as in Postman.
func findRequest(items []runItem, target string) int {
	for i, it := range items {
		if it.req.Name == target {
			return i
		}
	}
	for i, it := range items {
		if string(it.req.ID) == target {
			return i
		}
	}
	return -1
}

func sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-ctx.Done():
		return false
	}
}

// tally adds one outcome to the summary.
func tally(sum *RunSummary, o *Outcome, cancelled bool) {
	base := Failure{
		Iteration: o.Iteration, RequestID: o.RequestID, RequestName: o.RequestName, Path: o.Path,
	}
	if o.Skipped {
		sum.Skipped++
		return
	}
	if o.Resolved != nil || o.ScriptError != nil || o.Err != nil {
		sum.Requests++
	}
	counts := o.TestCounts()
	sum.Tests.Passed += counts.Passed
	sum.Tests.Failed += counts.Failed
	sum.Tests.Skipped += counts.Skipped
	for _, t := range o.Tests {
		if !t.Skipped && !t.Passed {
			f := base
			f.Kind, f.Name, f.Message = FailureTest, t.Name, t.Message
			sum.Failures = append(sum.Failures, f)
		}
	}
	if o.ScriptError != nil {
		sum.ScriptErrors++
		f := base
		f.Kind, f.Message = FailureScript, o.ScriptError.Text
		sum.Failures = append(sum.Failures, f)
	}
	if o.Err != nil && !cancelled && o.ScriptError == nil {
		sum.TransportErrors++
		f := base
		f.Kind, f.Message = FailureTransport, o.Err.Error()
		sum.Failures = append(sum.Failures, f)
	}
}
