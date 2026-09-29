// Package script runs Postman-compatible pre-request and test scripts on
// goja, a pure-Go ECMAScript engine.
//
// The goal is that collections exported from Postman keep working unchanged:
// the pm.* surface, the chai assertions behind pm.expect and pm.response.to,
// the bundled npm libraries reachable through require(), and the legacy
// postman.* globals all behave as they do in Postman's own sandbox. See
// docs/scripting.md for the compatibility table.
//
// Nothing outside the APIs described there is reachable from a script: there
// is no file system, no process environment and no network except
// pm.sendRequest, which goes through the [httpx.Sender] the caller supplies.
package script

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/shubham/postctl/internal/httpx"
	"github.com/shubham/postctl/internal/model"
	"github.com/shubham/postctl/internal/vars"
)

// Event is which of a request's two script slots is running.
type Event string

// Script events.
const (
	// EventPreRequest runs before the request is sent. pm.response is
	// undefined and pm.request may be mutated.
	EventPreRequest Event = "prerequest"
	// EventTest runs after the response arrives. pm.request is read-only.
	EventTest Event = "test"
)

// DefaultTimeout bounds one [Run], including pending timers, promises and
// pm.sendRequest calls. It matches Postman's default script timeout.
const DefaultTimeout = 5 * time.Second

// Info is what pm.info reports to the script.
type Info struct {
	// RequestName is the saved name of the request being run.
	RequestName string
	// RequestID is the request's stable identifier.
	RequestID string
	// CollectionName is the name of the enclosing collection. It is not
	// part of pm.info in Postman; postctl exposes it as
	// pm.info.collectionName as a documented extension.
	CollectionName string
	// Iteration is the zero-based collection-runner iteration.
	Iteration int
	// IterationCount is how many iterations are scheduled.
	IterationCount int
}

// CookieAccess is the cookie jar as scripts see it: pm.cookies lists the
// cookies that apply to the current request, and pm.cookies.jar() reaches the
// whole jar. WS2's jar satisfies this interface.
//
// Every method is called on the script runtime's goroutine, never
// concurrently. A nil CookieAccess makes pm.cookies an empty list and
// pm.cookies.jar() operations fail with an error passed to their callback.
type CookieAccess interface {
	// Cookies returns the cookies the jar would send to rawURL.
	Cookies(rawURL string) ([]model.Cookie, error)
	// SetCookie stores one cookie for rawURL.
	SetCookie(rawURL string, c model.Cookie) error
	// UnsetCookie removes the named cookie for rawURL.
	UnsetCookie(rawURL, name string) error
	// ClearCookies removes every cookie that applies to rawURL.
	ClearCookies(rawURL string) error
}

// Input is everything one [Run] needs.
type Input struct {
	// Event selects the script slot; it becomes pm.info.eventName.
	Event Event
	// Request is the request as pm.request sees it. A pre-request script
	// may mutate it; the result is in [Result.Request]. Run never writes
	// through this pointer.
	Request *model.Request
	// Response backs pm.response. It must be nil for [EventPreRequest].
	Response *httpx.Response
	// Vars is the live variable state. Scripts read and write it directly,
	// so Store.Changes reports what to persist afterwards (plan §0 R3).
	Vars vars.Store
	// Info backs pm.info.
	Info Info
	// Sender performs pm.sendRequest. When nil, pm.sendRequest fails with
	// an error rather than panicking.
	Sender httpx.Sender
	// SendOptions are passed to Sender for pm.sendRequest calls.
	SendOptions httpx.Options
	// Cookies backs pm.cookies and pm.cookies.jar(). May be nil.
	Cookies CookieAccess
	// Timeout bounds the whole run. Zero means [DefaultTimeout].
	Timeout time.Duration
}

// TestResult is one pm.test (or one legacy tests["name"] entry).
type TestResult struct {
	// Name is the test's name.
	Name string
	// Passed reports whether every assertion in the test held. A skipped
	// test is reported with Passed true and Skipped true, as in Postman.
	Passed bool
	// Skipped reports a pm.test.skip call.
	Skipped bool
	// Async reports that the test used a done callback or returned a
	// promise.
	Async bool
	// Error is the assertion message when Passed is false, for example
	// "AssertionError: expected response to have status code 200 but got
	// 404".
	Error string
	// Index is the test's position in the run, counting from zero.
	Index int
}

// ConsoleLevel is a console method name.
type ConsoleLevel string

// Console levels, matching the console methods scripts may call.
const (
	// LevelLog is console.log.
	LevelLog ConsoleLevel = "log"
	// LevelInfo is console.info.
	LevelInfo ConsoleLevel = "info"
	// LevelWarn is console.warn, also used for no-op API warnings.
	LevelWarn ConsoleLevel = "warn"
	// LevelError is console.error.
	LevelError ConsoleLevel = "error"
	// LevelDebug is console.debug.
	LevelDebug ConsoleLevel = "debug"
)

// ConsoleEntry is one captured console call. Arguments are formatted the way
// Node's util.inspect does, briefly.
type ConsoleEntry struct {
	// Level is the console method that produced the entry.
	Level ConsoleLevel
	// Message is the formatted arguments joined with spaces.
	Message string
	// Time is when the script made the call.
	Time time.Time
}

// ScriptError is a syntax or runtime error that stopped the chain. The
// remaining scripts do not run.
//
// The name stutters as script.ScriptError, but it is the one the WS3 brief,
// Result.Err and docs/scripting.md all use.
//
//nolint:revive // deliberate, see above.
type ScriptError struct {
	// ScriptIndex is the position in the chain passed to [Run].
	ScriptIndex int
	// File is the failing script's [model.Script] File, or a synthetic
	// name like "prerequest[0]" when the script has no file yet.
	File string
	// Name is the JavaScript error constructor name, for example
	// "TypeError". It is "SyntaxError" for a script that would not compile.
	Name string
	// Message is the error message without the name.
	Message string
	// Line is the 1-based line in File, or 0 when unknown.
	Line int
	// Column is the 1-based column in File, or 0 when unknown.
	Column int
	// Stack is the JavaScript stack trace with frames mapped to File.
	Stack string
}

// Error implements error.
func (e *ScriptError) Error() string {
	if e == nil {
		return "<nil>"
	}
	where := e.File
	if e.Line > 0 {
		where = fmt.Sprintf("%s:%d:%d", e.File, e.Line, e.Column)
	}
	return fmt.Sprintf("%s: %s: %s", where, e.Name, e.Message)
}

// Result is what one [Run] produced.
type Result struct {
	// Request is the request after the scripts ran. For
	// [EventPreRequest] it reflects pm.request mutations; otherwise it is
	// a copy of the input. It is always a distinct value from
	// [Input.Request].
	Request *model.Request
	// Tests are the pm.test results in call order, followed by any legacy
	// tests["name"] entries.
	Tests []TestResult
	// Console is every captured console call, in order.
	Console []ConsoleEntry
	// Err is set when a script failed to compile or threw. Tests and
	// console output recorded before the failure are still present.
	Err *ScriptError
	// NextRequest is the argument of the last pm.execution.setNextRequest
	// or postman.setNextRequest call: nil when never called, a pointer to
	// "" when called with null (stop the run), otherwise the request name.
	NextRequest *string
	// SkipRequest reports a pm.execution.skipRequest call.
	SkipRequest bool
	// TimedOut reports that the run hit [Input.Timeout] or its context was
	// cancelled. Err describes it too.
	TimedOut bool
}

// Passed reports whether every recorded test passed.
func (r *Result) Passed() bool {
	for _, t := range r.Tests {
		if !t.Passed {
			return false
		}
	}
	return true
}

// ErrNoRuntime is returned when the embedded sandbox cannot be prepared. It
// indicates a build problem, not a script problem.
var ErrNoRuntime = errors.New("script: sandbox runtime unavailable")

// Run executes the ordered chain of scripts for one event: collection, then
// folders outermost first, then the request. Every script shares one VM, so
// pm.variables and anything a script puts on the global object carry through,
// but each script body has its own function scope (as in Postman) and errors
// are attributed to the script that raised them.
//
// Disabled and empty scripts are skipped. Run returns an error only when the
// sandbox itself could not start; script failures are reported in
// [Result.Err] so the caller can still show tests and console output.
func Run(ctx context.Context, scripts []*model.Script, in Input) (*Result, error) {
	if in.Timeout <= 0 {
		in.Timeout = DefaultTimeout
	}
	if in.Event == "" {
		in.Event = EventPreRequest
	}
	if in.Event == EventPreRequest {
		in.Response = nil
	}
	return run(ctx, scripts, in)
}
