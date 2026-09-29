package engine

import (
	"errors"
	"time"

	"github.com/shubham/postctl/internal/httpx"
	"github.com/shubham/postctl/internal/model"
	"github.com/shubham/postctl/internal/script"
)

// Pipeline step names, as shown next to the spinner. They are plain text.
const (
	stepCollectionPre   = "Running collection pre-request script"
	stepFolderPre       = "Running folder pre-request script"
	stepRequestPre      = "Running pre-request script"
	stepResolve         = "Resolving variables"
	stepSend            = "Waiting for response"
	stepCollectionTests = "Running collection tests"
	stepFolderTests     = "Running folder tests"
	stepRequestTests    = "Running tests"
	stepSave            = "Saving variables"
)

// StepKind classifies a [Step] so clients can style or count them without
// parsing text.
type StepKind string

// Step kinds, in pipeline order.
const (
	// StepPreRequest is a pre-request script running.
	StepPreRequest StepKind = "prerequest"
	// StepResolve is variable resolution.
	StepResolve StepKind = "resolve"
	// StepSend is the HTTP exchange.
	StepSend StepKind = "send"
	// StepTest is a test script running.
	StepTest StepKind = "test"
	// StepSave is variable persistence.
	StepSave StepKind = "save"
)

// Step is one visible stage of a send.
type Step struct {
	// Kind classifies the step.
	Kind StepKind
	// Text is plain text such as "Running collection pre-request script".
	Text string
}

// SendInput is everything one send needs. The engine never mutates any model
// reachable from it: it works on deep copies taken when Send starts, so the
// caller may keep editing while a send is in flight.
type SendInput struct {
	// Collection owns the request and supplies inherited auth, scripts and
	// collection variables. Required.
	Collection *model.Collection
	// Request is what to send; it may be an unsaved copy of a request in
	// Collection (matched by ID). Required.
	Request *model.Request
	// Environment is the active environment, or nil.
	Environment *model.Environment
	// Globals are the workspace globals. When nil the engine reads
	// globals.yaml itself.
	Globals *model.Globals
	// Data is the iteration-data row (pm.iterationData). May be nil.
	Data map[string]any
	// Locals seeds pm.variables for this send, for example CLI --var
	// overrides. Local variables are never persisted.
	Locals map[string]any
	// Iteration is the zero-based iteration number reported to scripts.
	Iteration int
	// IterationCount is how many iterations are scheduled; zero means one.
	IterationCount int
}

// TestResult is one pm.test outcome.
type TestResult struct {
	// Name is the test name.
	Name string
	// Passed reports success. A skipped test is reported as passed and
	// Skipped, as Postman does.
	Passed bool
	// Skipped reports pm.test.skip.
	Skipped bool
	// Message is the assertion or error text for a failure.
	Message string
	// Origin is where the script lives: "collection", "folder" or
	// "request".
	Origin string
}

// ConsoleLine is one console.* call from a script.
type ConsoleLine struct {
	// Level is log, info, warn, error or debug.
	Level string
	// Text is the formatted message.
	Text string
	// Event is "prerequest" or "test".
	Event string
	// Time is when the script made the call.
	Time time.Time
}

// ScriptFailure describes a script that threw, failed to compile or timed
// out.
type ScriptFailure struct {
	// Event is "prerequest" or "test".
	Event string
	// Origin is "collection", "folder" or "request".
	Origin string
	// Text is the message with file, line and column when known.
	Text string
	// TimedOut reports that the script hit its timeout or was cancelled.
	TimedOut bool
	// Err is the underlying script error, if any.
	Err *script.ScriptError
}

// Error implements error.
func (f *ScriptFailure) Error() string {
	if f == nil {
		return ""
	}
	return f.Text
}

// TestCounts is a tally of test results.
type TestCounts struct {
	// Passed counts passing tests, skipped ones excluded.
	Passed int
	// Failed counts failing tests.
	Failed int
	// Skipped counts skipped tests.
	Skipped int
}

// Total is the number of tests counted.
func (c TestCounts) Total() int { return c.Passed + c.Failed + c.Skipped }

// Outcome is the result of one send. Exactly one of Response, Err and Skipped
// describes how the exchange ended; Tests, Console and ScriptError record what
// the scripts did along the way even when the request never went out.
type Outcome struct {
	// RequestID is the request that was sent.
	RequestID model.ID
	// RequestName is its name.
	RequestName string
	// Path lists the collection, folder and request names leading to it.
	Path []string
	// Iteration is the iteration this send belonged to.
	Iteration int
	// Started is when the send began.
	Started time.Time
	// Duration is the wall time of the whole pipeline, scripts included.
	Duration time.Duration
	// Resolved is the request that was handed to the HTTP layer: variables
	// substituted, inherited auth flattened. It can contain secrets. Never
	// log it.
	Resolved *model.Request
	// Response is nil when the request was skipped or the transport failed.
	Response *httpx.Response
	// Tests are the test outcomes in execution order.
	Tests []TestResult
	// Console is the combined console output in execution order.
	Console []ConsoleLine
	// ScriptError is the script failure that ended the pipeline early
	// (pre-request) or was recorded along the way (test), or nil.
	ScriptError *ScriptFailure
	// Skipped reports that a pre-request script called
	// pm.execution.skipRequest().
	Skipped bool
	// NextRequest is the last setNextRequest argument: nil when no script
	// called it, a pointer to "" for null (stop the run), else a request
	// name or ID.
	NextRequest *string
	// Warnings are non-fatal problems: unresolved {{variables}} in the
	// URL, missing secrets, a save that failed, a history write that
	// failed. They never contain secret values.
	Warnings []string
	// Persisted names the scopes written to disk: "environment",
	// "collection" or "globals".
	Persisted []string
	// HistoryID is the history entry recorded for this send, or empty.
	HistoryID string
	// Err is a resolution, transport, OAuth 2.0 or cancellation error.
	Err error
}

// TestCounts tallies Tests.
func (o *Outcome) TestCounts() TestCounts {
	var c TestCounts
	for _, t := range o.Tests {
		switch {
		case t.Skipped:
			c.Skipped++
		case t.Passed:
			c.Passed++
		default:
			c.Failed++
		}
	}
	return c
}

// Failed reports whether anything in the outcome should fail a run: a
// transport error, a script error or a failing test.
func (o *Outcome) Failed() bool {
	return o.Err != nil || o.ScriptError != nil || o.TestCounts().Failed > 0
}

// OAuthTokenMissingError is returned in [Outcome.Err] when a request uses
// OAuth 2.0 auth and no usable token is stored. The engine never fetches a
// token on its own (plan §0 R2).
type OAuthTokenMissingError struct {
	// Err is the underlying oauth2.ErrNoToken.
	Err error
}

// Error implements error with the message the UI shows.
func (e *OAuthTokenMissingError) Error() string {
	return "No OAuth 2.0 token. Get a new token in the Auth tab."
}

// Unwrap returns the underlying error.
func (e *OAuthTokenMissingError) Unwrap() error { return e.Err }

// ErrInvalidInput is wrapped by errors from Send and Run for a missing
// collection or request.
var ErrInvalidInput = errors.New("engine: invalid input")
