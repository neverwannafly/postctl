package report

import (
	"encoding/json"
	"io"
	"strings"

	"github.com/shubham/postctl/internal/engine"
)

type jsonReporter struct {
	opts Options
	enc  *json.Encoder
}

// NewJSON returns an NDJSON run reporter.
func NewJSON(w io.Writer, opts Options) Reporter {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	return &jsonReporter{opts: opts, enc: enc}
}

// Close finishes the reporter.
func (r *jsonReporter) Close() error { return nil }

// Event encodes one run event.
func (r *jsonReporter) Event(ev engine.RunEvent) error {
	switch e := ev.(type) {
	case engine.RunStarted:
		return r.enc.Encode(map[string]any{"type": "run-started", "collectionId": e.CollectionID, "collection": e.CollectionName,
			"folderId": e.FolderID, "folder": e.FolderName, "requests": e.Requests, "iterations": e.Iterations})
	case engine.IterationStarted:
		return r.enc.Encode(map[string]any{"type": "iteration-started", "iteration": e.Iteration, "iterations": e.Iterations})
	case engine.RequestStarted:
		return r.enc.Encode(map[string]any{"type": "request-started", "iteration": e.Iteration, "index": e.Index,
			"id": e.RequestID, "name": e.RequestName, "path": e.Path})
	case engine.RequestProgress:
		return r.enc.Encode(map[string]any{"type": "request-progress", "iteration": e.Iteration, "id": e.RequestID,
			"kind": e.Step.Kind, "step": e.Step.Text})
	case engine.RequestFinished:
		return r.enc.Encode(r.finished(e))
	case engine.IterationFinished:
		return r.enc.Encode(map[string]any{"type": "iteration-finished", "iteration": e.Iteration,
			"requests": e.Requests, "failed": e.Failed})
	case engine.RunFinished:
		return r.enc.Encode(map[string]any{"type": "run-finished", "summary": r.summary(e.Summary)})
	}
	return nil
}

func (r *jsonReporter) finished(e engine.RequestFinished) map[string]any {
	o := e.Outcome
	doc := map[string]any{"type": "request-finished", "iteration": e.Iteration, "index": e.Index}
	if o == nil {
		return doc
	}
	doc["id"], doc["name"], doc["path"] = o.RequestID, o.RequestName, o.Path
	doc["durationMs"], doc["skipped"] = o.Duration.Milliseconds(), o.Skipped
	if o.SkipReason != "" {
		doc["skipReason"] = o.SkipReason
	}
	if o.Response != nil {
		doc["status"], doc["statusText"] = o.Response.StatusCode, o.Response.Status
		doc["durationMs"] = o.Response.Timings.Total.Milliseconds()
		doc["sizeBytes"] = o.Response.Size.Headers + o.Response.Size.Body
	}
	if o.Err != nil {
		doc["error"] = r.opts.mask(o.Err.Error())
	}
	if o.ScriptError != nil {
		doc["scriptError"], doc["scriptEvent"] = r.opts.mask(o.ScriptError.Text), o.ScriptError.Event
	}
	if len(o.Warnings) > 0 {
		warnings := make([]string, len(o.Warnings))
		for i, warning := range o.Warnings {
			warnings[i] = r.opts.mask(warning)
		}
		doc["warnings"] = warnings
	}
	type testDoc struct {
		Name    string `json:"name"`
		Passed  bool   `json:"passed"`
		Skipped bool   `json:"skipped,omitempty"`
		Message string `json:"message,omitempty"`
		Origin  string `json:"origin,omitempty"`
	}
	tests := make([]testDoc, 0, len(o.Tests))
	for _, test := range o.Tests {
		tests = append(tests, testDoc{Name: test.Name, Passed: test.Passed, Skipped: test.Skipped,
			Message: r.opts.mask(test.Message), Origin: test.Origin})
	}
	doc["tests"] = tests
	return doc
}

func (r *jsonReporter) summary(sum *engine.RunSummary) map[string]any {
	if sum == nil {
		return nil
	}
	failures := make([]map[string]any, 0, len(sum.Failures))
	for _, failure := range sum.Failures {
		failures = append(failures, map[string]any{"iteration": failure.Iteration, "id": failure.RequestID,
			"name": failure.RequestName, "path": strings.Join(failure.Path, "/"), "kind": failure.Kind,
			"test": failure.Name, "message": r.opts.mask(failure.Message)})
	}
	return map[string]any{
		"iterations": sum.Iterations, "requests": sum.Requests, "skipped": sum.Skipped,
		"assertions":   map[string]int{"passed": sum.Tests.Passed, "failed": sum.Tests.Failed, "skipped": sum.Tests.Skipped},
		"scriptErrors": sum.ScriptErrors, "transportErrors": sum.TransportErrors,
		"totalTimeMs": sum.TotalTime.Milliseconds(), "avgResponseTimeMs": sum.AvgResponseTime.Milliseconds(),
		"stopReason": sum.StopReason, "exitCode": exitCode(sum), "failures": failures,
	}
}

func exitCode(sum *engine.RunSummary) int {
	code := sum.ExitCode()
	if sum.StopReason == engine.StopCancelled && code == 0 {
		return 2
	}
	return code
}
