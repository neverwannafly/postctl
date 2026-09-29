package cli

import (
	"encoding/json"
	"io"
	"strings"

	"github.com/shubham/postctl/internal/engine"
)

// jsonReporter streams one JSON object per line (NDJSON).
type jsonReporter struct {
	a   *app
	enc *json.Encoder
}

func newJSONReporter(a *app, w io.Writer) *jsonReporter {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	return &jsonReporter{a: a, enc: enc}
}

func (r *jsonReporter) close() error { return nil }

func (r *jsonReporter) event(ev engine.RunEvent) error {
	switch e := ev.(type) {
	case engine.RunStarted:
		return r.enc.Encode(map[string]any{
			"type": "run-started", "collectionId": e.CollectionID, "collection": e.CollectionName,
			"folderId": e.FolderID, "folder": e.FolderName,
			"requests": e.Requests, "iterations": e.Iterations,
		})
	case engine.IterationStarted:
		return r.enc.Encode(map[string]any{
			"type": "iteration-started", "iteration": e.Iteration, "iterations": e.Iterations,
		})
	case engine.RequestStarted:
		return r.enc.Encode(map[string]any{
			"type": "request-started", "iteration": e.Iteration, "index": e.Index,
			"id": e.RequestID, "name": e.RequestName, "path": e.Path,
		})
	case engine.RequestProgress:
		return r.enc.Encode(map[string]any{
			"type": "request-progress", "iteration": e.Iteration, "id": e.RequestID,
			"kind": e.Step.Kind, "step": e.Step.Text,
		})
	case engine.RequestFinished:
		return r.enc.Encode(r.finished(e))
	case engine.IterationFinished:
		return r.enc.Encode(map[string]any{
			"type": "iteration-finished", "iteration": e.Iteration,
			"requests": e.Requests, "failed": e.Failed,
		})
	case engine.RunFinished:
		return r.enc.Encode(map[string]any{
			"type": "run-finished", "summary": r.summary(e.Summary),
		})
	}
	return nil
}

func (r *jsonReporter) finished(e engine.RequestFinished) map[string]any {
	o := e.Outcome
	doc := map[string]any{
		"type": "request-finished", "iteration": e.Iteration, "index": e.Index,
	}
	if o == nil {
		return doc
	}
	doc["id"] = o.RequestID
	doc["name"] = o.RequestName
	doc["path"] = o.Path
	doc["durationMs"] = durationMS(o.Duration)
	doc["skipped"] = o.Skipped
	if o.Response != nil {
		doc["status"] = o.Response.StatusCode
		doc["statusText"] = o.Response.Status
		doc["durationMs"] = durationMS(o.Response.Timings.Total)
		doc["sizeBytes"] = o.Response.Size.Headers + o.Response.Size.Body
	}
	if o.Err != nil {
		doc["error"] = r.a.mask(o.Err.Error())
	}
	if o.ScriptError != nil {
		doc["scriptError"] = r.a.mask(o.ScriptError.Text)
		doc["scriptEvent"] = o.ScriptError.Event
	}
	if len(o.Warnings) > 0 {
		warn := make([]string, len(o.Warnings))
		for i, w := range o.Warnings {
			warn[i] = r.a.mask(w)
		}
		doc["warnings"] = warn
	}
	tests := make([]testDoc, 0, len(o.Tests))
	for _, t := range o.Tests {
		tests = append(tests, testDoc{
			Name: t.Name, Passed: t.Passed, Skipped: t.Skipped,
			Message: r.a.mask(t.Message), Origin: t.Origin,
		})
	}
	doc["tests"] = tests
	return doc
}

func (r *jsonReporter) summary(sum *engine.RunSummary) map[string]any {
	if sum == nil {
		return nil
	}
	fails := make([]map[string]any, 0, len(sum.Failures))
	for _, f := range sum.Failures {
		fails = append(fails, map[string]any{
			"iteration": f.Iteration,
			"id":        f.RequestID,
			"name":      f.RequestName,
			"path":      strings.Join(f.Path, "/"),
			"kind":      f.Kind,
			"test":      f.Name,
			"message":   r.a.mask(f.Message),
		})
	}
	return map[string]any{
		"iterations":        sum.Iterations,
		"requests":          sum.Requests,
		"skipped":           sum.Skipped,
		"assertions":        map[string]int{"passed": sum.Tests.Passed, "failed": sum.Tests.Failed, "skipped": sum.Tests.Skipped},
		"scriptErrors":      sum.ScriptErrors,
		"transportErrors":   sum.TransportErrors,
		"totalTimeMs":       durationMS(sum.TotalTime),
		"avgResponseTimeMs": durationMS(sum.AvgResponseTime),
		"stopReason":        sum.StopReason,
		"exitCode":          runExit(sum),
		"failures":          fails,
	}
}
