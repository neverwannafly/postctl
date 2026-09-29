package tui

import (
	"context"
	"time"

	"github.com/shubham/postctl/internal/httpx"
	"github.com/shubham/postctl/internal/model"
)

// The types in this file are a contract between the request pane (which asks
// for a send), the shell (which runs it), the response pane (which shows the
// result) and the engine adapter that implements [Sender]. Extend additively.

// Sender runs the whole send pipeline for one request: variable resolution,
// pre-request scripts, HTTP, test scripts and variable persistence. The shell
// calls it from a command, never from Update.
type Sender interface {
	// Send blocks until the exchange and scripts finish or ctx is cancelled.
	// progress is called from Send's goroutine with human-readable steps.
	Send(ctx context.Context, req SendRequest, progress func(SendStep)) SendResult
}

// SendRequest is what to send.
type SendRequest struct {
	// Collection owns the request; it supplies inherited auth, scripts and
	// collection variables.
	Collection *model.Collection
	// Request is the editor's current copy, which may have unsaved edits.
	Request *model.Request
	// Environment is the active environment, or nil.
	Environment *model.Environment
}

// SendStep is one visible stage of a send, such as "Running pre-request
// script" or "Waiting for response".
type SendStep struct {
	// Text is plain, redacted text shown next to the spinner.
	Text string
}

// SendResult is the outcome of one send.
type SendResult struct {
	// RequestID is the request that was sent.
	RequestID model.ID
	// Started is when the send began.
	Started time.Time
	// Response is nil when the request was not sent or the transport failed.
	Response *httpx.Response
	// Tests are the test outcomes, in execution order.
	Tests []TestOutcome
	// Console is the combined script console output, in order.
	Console []ConsoleLine
	// Warnings are non-fatal engine problems, shown above the response body.
	// They never contain secret values.
	Warnings []string
	// ScriptError describes a script that threw or timed out, or is empty.
	ScriptError string
	// Skipped reports that a pre-request script called
	// pm.execution.skipRequest().
	Skipped bool
	// Err is a transport, resolution or cancellation error.
	Err error
}

// TestOutcome is one pm.test result.
type TestOutcome struct {
	// Name is the test name.
	Name string
	// Passed reports success; ignored when Skipped.
	Passed bool
	// Skipped reports pm.test.skip.
	Skipped bool
	// Message is the assertion or error message for a failure.
	Message string
}

// ConsoleLine is one console.* call from a script.
type ConsoleLine struct {
	// Level is log, info, warn, error or debug.
	Level string
	// Text is the formatted message.
	Text string
	// Event is "prerequest" or "test".
	Event string
}

// SendRequestMsg asks the shell to send (from a request pane command).
type SendRequestMsg struct {
	// Request is what to send.
	Request SendRequest
}

// CancelSendMsg asks the shell to cancel the in-flight send for a request.
type CancelSendMsg struct {
	// RequestID is the request whose send to cancel.
	RequestID model.ID
}

// SendStartedMsg tells panes a send began.
type SendStartedMsg struct {
	// RequestID is the request being sent.
	RequestID model.ID
}

// SendProgressMsg tells panes about a new step of an in-flight send.
type SendProgressMsg struct {
	// RequestID is the request being sent.
	RequestID model.ID
	// Step is the new step.
	Step SendStep
}

// SendResultMsg delivers a finished send to every pane.
type SendResultMsg struct {
	// Result is the outcome.
	Result SendResult
}

// RetrySendMsg asks the shell to resend the request for RequestID (for example
// from the response pane after a transport error).
type RetrySendMsg struct {
	// RequestID is the request to send again.
	RequestID model.ID
}
