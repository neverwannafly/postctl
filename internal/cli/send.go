package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/shubham/postctl/internal/engine"
	"github.com/shubham/postctl/internal/history"
	"github.com/shubham/postctl/internal/httpx"
	"github.com/shubham/postctl/internal/model"
	"github.com/shubham/postctl/internal/ui"
)

func runSend(ctx context.Context, a *app, args []string) int {
	fs := a.flagSet()
	envName := fs.String("e", "", "environment name or id (default: the active environment)")
	fs.StringVar(envName, "env", "", "environment name or id")
	asJSON := fs.Bool("json", false, "print JSON")
	include := fs.Bool("include", false, "print response headers")
	verbose := fs.Bool("verbose", false, "print the sent request, timeline and console output")
	noScripts := fs.Bool("no-scripts", false, "do not run pre-request or test scripts")
	noHistory := fs.Bool("no-history", false, "do not record this send in history")
	var overrides []string
	fs.Func("var", "set a variable for this send, as k=v (repeatable)", func(s string) error {
		overrides = append(overrides, s)
		return nil
	})
	positional, code := a.parse(fs, args)
	if code >= 0 {
		return code
	}
	path, code := a.onePath(positional)
	if code >= 0 {
		return code
	}
	locals, err := parseVars(overrides)
	if err != nil {
		fmt.Fprintln(a.err, err.Error())
		return ExitUsage
	}
	if code := a.open(); code >= 0 {
		return code
	}
	n, err := resolvePath(a.ws, path)
	if err != nil {
		fmt.Fprintln(a.err, err.Error())
		return ExitUsage
	}
	if n.request() == nil {
		fmt.Fprintf(a.err, "%q is a %s. postctl send takes a request.\n", path, n.kind())
		return ExitUsage
	}
	if n.request().Kind == model.KindWebSocket {
		fmt.Fprintln(a.err, "Open WebSocket requests in the TUI; `postctl send` supports HTTP requests.")
		return ExitUsage
	}
	env, code := a.lookupEnv(*envName)
	if code >= 0 {
		return code
	}
	col, req := n.col, n.request()
	if *noScripts {
		col, req = withoutScripts(col, req)
	} else {
		col, req = col.Clone(), req.Clone()
	}
	eng, code := a.newEngine(*noHistory)
	if code >= 0 {
		return code
	}
	out, err := eng.Send(ctx, engine.SendInput{
		Collection:  col,
		Request:     req,
		Environment: env,
		Globals:     a.ws.Globals,
		Locals:      locals,
	}, nil)
	if err != nil {
		fmt.Fprintf(a.err, "Cannot send: %v\n", a.mask(err.Error()))
		return ExitUsage
	}
	if *asJSON {
		if err := writeSendJSON(a, out, req.Method, *include, *verbose); err != nil {
			fmt.Fprintf(a.err, "Cannot write JSON: %v\n", err)
			return ExitRequest
		}
		return outcomeExit(out)
	}
	writeSendText(a, out, *include, *verbose)
	return outcomeExit(out)
}

func parseVars(pairs []string) (map[string]any, error) {
	if len(pairs) == 0 {
		return nil, nil
	}
	out := make(map[string]any, len(pairs))
	for _, p := range pairs {
		key, val, ok := strings.Cut(p, "=")
		if !ok || strings.TrimSpace(key) == "" {
			return nil, fmt.Errorf("--var expects k=v, got %q", p)
		}
		out[key] = val
	}
	return out, nil
}

func withoutScripts(col *model.Collection, req *model.Request) (*model.Collection, *model.Request) {
	c := col.Clone()
	c.Scripts = model.Scripts{}
	_ = c.Walk(func(it model.Item, _ model.Container, _ int) error {
		switch n := it.(type) {
		case *model.Folder:
			n.Scripts = model.Scripts{}
		case *model.Request:
			n.Scripts = model.Scripts{}
		}
		return nil
	})
	if found, _, _, ok := c.Find(req.ID); ok {
		if r, ok := found.(*model.Request); ok {
			return c, r
		}
	}
	r := req.Clone()
	r.Scripts = model.Scripts{}
	return c, r
}

func outcomeExit(o *engine.Outcome) int {
	if o == nil || o.Err != nil {
		return ExitRequest
	}
	if o.ScriptError != nil || o.TestCounts().Failed > 0 {
		return ExitTests
	}
	return ExitOK
}

func writeSendText(a *app, o *engine.Outcome, include, verbose bool) {
	fmt.Fprintln(a.out, a.outcomeStatus(o))
	for _, w := range o.Warnings {
		fmt.Fprintf(a.out, "%s %s\n", a.view.warn(), a.mask(w))
	}
	if include && o.Response != nil {
		fmt.Fprintln(a.out, "Headers")
		for _, line := range a.headerLines(o.Response.Headers) {
			fmt.Fprintf(a.out, "  %s\n", line)
		}
	}
	if verbose && o.Response != nil {
		writeSent(a, o.Response.Sent)
		writeTimeline(a, o.Response.Timings)
	}
	if o.Response != nil && !o.Skipped {
		fmt.Fprint(a.out, a.formatBody(o.Response.Headers, o.Response.Body))
	}
	writeTests(a, o)
	if o.ScriptError != nil {
		label := "Script failed"
		switch o.ScriptError.Event {
		case "prerequest":
			label = "Pre-request script failed"
		case "test":
			label = "Test script failed"
		}
		fmt.Fprintf(a.out, "%s %s\n", a.view.fail(), label)
		if text := a.mask(o.ScriptError.Text); text != "" {
			fmt.Fprintf(a.out, "  %s\n", text)
		}
	}
	if verbose && len(o.Console) > 0 {
		fmt.Fprintln(a.out, "Console")
		for _, line := range o.Console {
			fmt.Fprintf(a.out, "  %s  %s\n", line.Level, a.mask(line.Text))
		}
	}
}

func (a *app) outcomeStatus(o *engine.Outcome) string {
	switch {
	case o.Skipped:
		if o.SkipReason != "" {
			return a.view.skip() + " Skipped — " + o.SkipReason
		}
		return a.view.skip() + " Skipped"
	case o.Err != nil && errors.Is(o.Err, context.Canceled):
		return a.view.fail() + " Canceled"
	case o.Err != nil:
		return a.view.fail() + " " + a.mask(o.Err.Error())
	case o.Response != nil:
		reason := o.Response.Status
		if reason == "" {
			reason = http.StatusText(o.Response.StatusCode)
		}
		return a.view.statusLine(o.Response.StatusCode, reason, o.Response.Timings.Total, o.Response.Size.Headers+o.Response.Size.Body)
	default:
		return a.view.fail() + " No response"
	}
}

func writeTests(a *app, o *engine.Outcome) {
	for _, t := range o.Tests {
		switch {
		case t.Skipped:
			fmt.Fprintf(a.out, "%s %s  skipped\n", a.view.skip(), t.Name)
		case t.Passed:
			fmt.Fprintf(a.out, "%s %s\n", a.view.paint(ui.StatusSuccess, a.view.check()), t.Name)
		default:
			fmt.Fprintf(a.out, "%s %s\n", a.view.paint(ui.StatusDanger, a.view.fail()), t.Name)
			if t.Message != "" {
				fmt.Fprintf(a.out, "  %s\n", a.mask(t.Message))
			}
		}
	}
}

func writeSent(a *app, sent httpx.SentRequest) {
	fmt.Fprintln(a.out, "Sent")
	fmt.Fprintf(a.out, "  %s %s\n", sent.Method, a.maskURL(sent.URL))
	for _, line := range a.headerLines(sent.Headers) {
		fmt.Fprintf(a.out, "  %s\n", line)
	}
	if len(sent.Body) > 0 {
		body, _ := a.maskBody(sent.Headers, sent.Body)
		for _, line := range strings.Split(strings.TrimRight(body, "\n"), "\n") {
			fmt.Fprintf(a.out, "  %s\n", line)
		}
	}
}

func writeTimeline(a *app, t httpx.Timings) {
	fmt.Fprintln(a.out, "Timeline")
	for _, row := range []struct {
		name string
		d    time.Duration
	}{
		{"DNS", t.DNS},
		{"Connect", t.Connect},
		{"TLS", t.TLS},
		{"Waiting", t.TTFB},
		{"Download", t.Download},
		{"Total", t.Total},
	} {
		fmt.Fprintf(a.out, "  %-8s %s\n", row.name, ui.FormatDuration(row.d))
	}
}

func (a *app) headerLines(headers []model.KV) []string {
	masked := a.red.Headers(headers)
	lines := make([]string, 0, len(masked))
	for _, h := range masked {
		lines = append(lines, h.Key+": "+a.view.swapMask(h.Value))
	}
	return lines
}

func (a *app) mask(s string) string {
	return a.view.swapMask(a.red.Text(s))
}

func (a *app) maskURL(raw string) string {
	return a.view.swapMask(a.red.URL(raw))
}

func (a *app) maskBody(headers []model.KV, body []byte) (string, string) {
	ct := history.ContentType(headers)
	if !utf8.Valid(body) {
		text, _, enc := a.red.SnapshotBody(ct, body, len(body)+1)
		return a.view.swapMask(text), enc
	}
	return a.view.swapMask(a.red.Body(ct, string(body))), ""
}

func (a *app) formatBody(headers []model.KV, body []byte) string {
	if len(body) == 0 {
		return ""
	}
	if !utf8.Valid(body) {
		return fmt.Sprintf("(binary body, %s)\n", ui.FormatSize(int64(len(body))))
	}
	text, _ := a.maskBody(headers, body)
	ct := history.ContentType(headers)
	if a.opts.tty(a.out) && (strings.Contains(strings.ToLower(ct), "json") || looksJSON(text)) {
		var buf strings.Builder
		raw := []byte(text)
		if json.Valid(raw) {
			enc := json.NewEncoder(&buf)
			enc.SetEscapeHTML(false)
			enc.SetIndent("", "  ")
			var v any
			if json.Unmarshal(raw, &v) == nil && enc.Encode(v) == nil {
				return buf.String()
			}
		}
	}
	if !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	return text
}

func looksJSON(s string) bool {
	s = strings.TrimSpace(s)
	return strings.HasPrefix(s, "{") || strings.HasPrefix(s, "[")
}

type sendDoc struct {
	Name         string       `json:"name"`
	ID           string       `json:"id"`
	Method       string       `json:"method,omitempty"`
	Path         []string     `json:"path"`
	Status       int          `json:"status,omitempty"`
	StatusText   string       `json:"statusText,omitempty"`
	DurationMS   int64        `json:"durationMs"`
	SizeBytes    int64        `json:"sizeBytes,omitempty"`
	Skipped      bool         `json:"skipped,omitempty"`
	Error        string       `json:"error,omitempty"`
	ScriptError  string       `json:"scriptError,omitempty"`
	Warnings     []string     `json:"warnings,omitempty"`
	Headers      []kvDoc      `json:"headers,omitempty"`
	Body         string       `json:"body,omitempty"`
	BodyEncoding string       `json:"bodyEncoding,omitempty"`
	Tests        []testDoc    `json:"tests,omitempty"`
	Sent         *sentDoc     `json:"sent,omitempty"`
	Timeline     []phaseDoc   `json:"timeline,omitempty"`
	Console      []consoleDoc `json:"console,omitempty"`
}

type kvDoc struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type testDoc struct {
	Name    string `json:"name"`
	Passed  bool   `json:"passed"`
	Skipped bool   `json:"skipped,omitempty"`
	Message string `json:"message,omitempty"`
	Origin  string `json:"origin,omitempty"`
}

type sentDoc struct {
	Method  string  `json:"method"`
	URL     string  `json:"url"`
	Headers []kvDoc `json:"headers,omitempty"`
	Body    string  `json:"body,omitempty"`
}

type phaseDoc struct {
	Name string `json:"name"`
	MS   int64  `json:"ms"`
}

type consoleDoc struct {
	Level string `json:"level"`
	Event string `json:"event,omitempty"`
	Text  string `json:"text"`
}

func writeSendJSON(a *app, o *engine.Outcome, method string, include, verbose bool) error {
	doc := sendDoc{
		Name:   o.RequestName,
		ID:     string(o.RequestID),
		Method: method,
		Path:   o.Path,
	}
	if o.Skipped {
		doc.Skipped = true
	}
	if o.Err != nil {
		doc.Error = a.mask(o.Err.Error())
	}
	if o.ScriptError != nil {
		doc.ScriptError = a.mask(o.ScriptError.Text)
	}
	for _, w := range o.Warnings {
		doc.Warnings = append(doc.Warnings, a.mask(w))
	}
	for _, t := range o.Tests {
		doc.Tests = append(doc.Tests, testDoc{
			Name: t.Name, Passed: t.Passed, Skipped: t.Skipped,
			Message: a.mask(t.Message), Origin: t.Origin,
		})
	}
	dur := o.Duration
	if o.Response != nil {
		doc.Status = o.Response.StatusCode
		doc.StatusText = o.Response.Status
		dur = o.Response.Timings.Total
		doc.SizeBytes = o.Response.Size.Headers + o.Response.Size.Body
		if include {
			doc.Headers = a.kvDocs(o.Response.Headers)
		}
		body, enc := a.maskBody(o.Response.Headers, o.Response.Body)
		doc.Body, doc.BodyEncoding = body, enc
		if verbose {
			sentBody, _ := a.maskBody(o.Response.Sent.Headers, o.Response.Sent.Body)
			doc.Sent = &sentDoc{
				Method:  o.Response.Sent.Method,
				URL:     a.maskURL(o.Response.Sent.URL),
				Headers: a.kvDocs(o.Response.Sent.Headers),
				Body:    sentBody,
			}
			t := o.Response.Timings
			doc.Timeline = []phaseDoc{
				{"DNS", t.DNS.Milliseconds()},
				{"Connect", t.Connect.Milliseconds()},
				{"TLS", t.TLS.Milliseconds()},
				{"Waiting", t.TTFB.Milliseconds()},
				{"Download", t.Download.Milliseconds()},
				{"Total", t.Total.Milliseconds()},
			}
		}
	}
	doc.DurationMS = dur.Milliseconds()
	if verbose {
		for _, line := range o.Console {
			doc.Console = append(doc.Console, consoleDoc{Level: line.Level, Event: line.Event, Text: a.mask(line.Text)})
		}
	}
	enc := json.NewEncoder(a.out)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	return enc.Encode(doc)
}

func (a *app) kvDocs(headers []model.KV) []kvDoc {
	masked := a.red.Headers(headers)
	out := make([]kvDoc, 0, len(masked))
	for _, h := range masked {
		out = append(out, kvDoc{Key: h.Key, Value: a.view.swapMask(h.Value)})
	}
	return out
}
