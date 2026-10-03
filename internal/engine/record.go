package engine

import (
	"context"
	"errors"
	"fmt"

	"github.com/shubham/postctl/internal/history"
	"github.com/shubham/postctl/internal/model"
)

// record appends a redacted history entry for a request that was attempted:
// one that reached the HTTP layer, whether or not a response came back.
// Skipped requests, failed pre-request scripts and cancelled sends are not
// recorded because nothing (complete) went on the wire.
func (s *session) record(_ context.Context, out *Outcome) {
	rec := s.e.cfg.History
	if rec == nil || out.Skipped || out.Resolved == nil || errors.Is(out.Err, context.Canceled) {
		return
	}
	entry := s.entry(out)
	saved, err := rec.Append(entry)
	if err != nil {
		out.Warnings = append(out.Warnings, "Could not record history: "+err.Error())
		return
	}
	out.HistoryID = saved.ID
}

func (s *session) entry(out *Outcome) history.Entry {
	resolved := out.Resolved
	redactor := out.Redactor
	if redactor == nil {
		redactor = s.redactor(authNames(resolved.Auth)...)
	}
	counts := out.TestCounts()
	e := history.Entry{
		Time:         out.Started,
		RequestID:    out.RequestID,
		RequestName:  out.RequestName,
		Path:         out.Path,
		CollectionID: s.collection.ID,
		Tests: history.TestCounts{
			Passed: counts.Passed, Failed: counts.Failed, Skipped: counts.Skipped,
		},
	}
	if s.env != nil {
		e.Environment = s.env.Name
	}

	e.Request = history.RequestSnapshot{Method: resolved.Method}
	if resp := out.Response; resp != nil {
		sent := resp.Sent
		e.Request.Method = sent.Method
		e.Request.URL = redactor.URL(sent.URL)
		e.Request.Headers = redactor.Headers(sent.Headers)
		e.Request.Body, e.Request.BodyTruncated = snapshotText(redactor, history.ContentType(sent.Headers), sent.Body)
	} else {
		e.Request.URL = redactor.URL(resolved.URL.Raw)
		e.Request.Headers = redactor.Headers(enabledHeaders(resolved.Headers))
		e.Request.Body, e.Request.BodyTruncated = snapshotText(
			redactor, history.ContentType(resolved.Headers), []byte(previewBody(&resolved.Body)))
	}

	if resp := out.Response; resp != nil {
		e.Status = resp.StatusCode
		e.StatusText = resp.Status
		e.DurationMS = resp.Timings.Total.Milliseconds()
		e.SizeBytes = resp.Size.Headers + resp.Size.Body
		body, truncated, encoding := redactor.SnapshotBody(
			history.ContentType(resp.Headers), resp.Body, history.DefaultMaxBodyBytes)
		e.Response = &history.ResponseSnapshot{
			Headers:      redactor.Headers(resp.Headers),
			Body:         body,
			BodyEncoding: encoding,
			Truncated:    truncated || resp.BodyTruncated,
		}
	} else {
		e.DurationMS = out.Duration.Milliseconds()
	}
	if out.Err != nil {
		e.Error = redactor.Text(out.Err.Error())
	}
	if out.ScriptError != nil {
		e.ScriptError = redactor.Text(out.ScriptError.Text)
	}
	if src := out.Unresolved; src != nil {
		e.Template = history.TemplateFromRequest(src, redactor)
	}
	return e
}

func snapshotText(r *history.Redactor, contentType string, body []byte) (string, bool) {
	text, truncated, encoding := r.SnapshotBody(contentType, body, history.DefaultMaxBodyBytes)
	if encoding != "" {
		return fmt.Sprintf("(binary body, %d bytes)", len(body)), truncated
	}
	return text, truncated
}

func enabledHeaders(headers []model.KV) []model.KV {
	out := make([]model.KV, 0, len(headers))
	for _, h := range headers {
		if h.Enabled() {
			out = append(out, h)
		}
	}
	return out
}

// previewBody is the text of a body that was never encoded because the send
// failed early. Only text modes are shown.
func previewBody(b *model.Body) string {
	switch b.Mode {
	case model.BodyRaw:
		return b.Raw
	case model.BodyGraphQL:
		if b.GraphQL != nil {
			return b.GraphQL.Query
		}
	case model.BodyURLEncoded:
		var out string
		for _, kv := range b.URLEncoded {
			if kv.Enabled() {
				if out != "" {
					out += "&"
				}
				out += kv.Key + "=" + kv.Value
			}
		}
		return out
	}
	return ""
}
