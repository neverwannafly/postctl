package response

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"strconv"
	"strings"
	"time"

	"github.com/itchyny/gojq"
	"github.com/shubham/postctl/internal/history"
	"github.com/shubham/postctl/internal/tui"
)

// jq evaluation limits. The filter runs on the masked body, off the Update
// goroutine, under a deadline and an output cap.
const (
	jqTimeout   = 250 * time.Millisecond
	jqMaxOutput = 1 << 20  // bytes of output kept before "truncated"
	jqMaxInput  = 16 << 20 // largest body jq accepts
)

// jqOutput is the outcome of one filter run. Err is one line of plain text
// that never contains a value from the body.
type jqOutput struct {
	Text      string
	Truncated bool
	Err       string
}

// runJQ evaluates filter against the JSON document src and returns the
// results, one pretty-printed value per block. It never panics and never
// returns a message that quotes the document.
func runJQ(filter string, src []byte, timeout time.Duration, maxOut int) jqOutput {
	query, err := gojq.Parse(filter)
	if err != nil {
		return jqOutput{Err: "Syntax error: " + shortJQText(err.Error())}
	}
	// No environment loader: env and $ENV see an empty environment, and no
	// module loader is set, so a filter cannot read files or variables.
	code, err := gojq.Compile(query, gojq.WithEnvironLoader(func() []string { return nil }))
	if err != nil {
		return jqOutput{Err: "Filter error: " + shortJQText(err.Error())}
	}
	if len(src) > jqMaxInput {
		return jqOutput{Err: "The body is too large for jq."}
	}
	dec := json.NewDecoder(bytes.NewReader(src))
	dec.UseNumber()
	var doc any
	if err := dec.Decode(&doc); err != nil {
		return jqOutput{Err: "The body is not valid JSON."}
	}
	doc = normalizeJSONNumbers(doc)

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	done := make(chan jqOutput, 1)
	go func() { done <- evalJQ(ctx, code, doc, maxOut) }()
	select {
	case out := <-done:
		return out
	case <-ctx.Done():
		return jqOutput{Err: "Timed out after " + timeout.String() + "."}
	}
}

func evalJQ(ctx context.Context, code *gojq.Code, doc any, maxOut int) jqOutput {
	var out bytes.Buffer
	iter := code.RunWithContext(ctx, doc)
	for {
		v, ok := iter.Next()
		if !ok {
			break
		}
		if err, isErr := v.(error); isErr {
			var halt *gojq.HaltError
			if errors.As(err, &halt) && halt.ExitCode() == 0 {
				break
			}
			if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
				return jqOutput{Err: "Timed out."}
			}
			return jqOutput{Err: runtimeJQError(err)}
		}
		raw, err := gojq.Marshal(v)
		if err != nil {
			return jqOutput{Err: "A result could not be written as JSON."}
		}
		var pretty bytes.Buffer
		if err := json.Indent(&pretty, raw, "", "  "); err != nil {
			pretty.Reset()
			pretty.Write(raw)
		}
		pretty.WriteByte('\n')
		if out.Len()+pretty.Len() > maxOut {
			room := maxOut - out.Len()
			if room > 0 {
				chunk := pretty.Bytes()[:room]
				if i := bytes.LastIndexByte(chunk, '\n'); i >= 0 {
					out.Write(chunk[:i+1])
				}
			}
			return jqOutput{Text: out.String(), Truncated: true}
		}
		out.Write(pretty.Bytes())
	}
	return jqOutput{Text: out.String()}
}

// runtimeJQError turns a gojq runtime error into one line of text without any
// value from the body. gojq quotes the offending value after the type name
// ("string (\"...\")"), so the message is cut at the first opening bracket or
// quote; an error(...) call is reported without its payload.
func runtimeJQError(err error) string {
	var ve gojq.ValueError
	if errors.As(err, &ve) {
		return "The filter called error()."
	}
	return "Runtime error: " + shortJQText(err.Error())
}

// shortJQText cuts s before the first character that starts a quoted value
// and limits it to one short line.
func shortJQText(s string) string {
	if i := strings.IndexAny(s, "(\"'`{["); i >= 0 {
		s = s[:i]
	}
	s = strings.Map(func(r rune) rune {
		if r < ' ' || r == 0x7f {
			return ' '
		}
		return r
	}, s)
	s = strings.TrimRight(strings.TrimSpace(s), ":,; ")
	if len(s) > 80 {
		s = s[:80]
	}
	if s == "" {
		return "invalid filter"
	}
	return s
}

// normalizeJSONNumbers converts json.Number values to the number types gojq
// expects (int, *big.Int, float64) so large integers keep their digits.
func normalizeJSONNumbers(v any) any {
	switch x := v.(type) {
	case json.Number:
		if i, err := strconv.ParseInt(string(x), 10, 0); err == nil {
			return int(i)
		}
		if b, ok := new(big.Int).SetString(string(x), 10); ok {
			return b
		}
		f, _ := strconv.ParseFloat(string(x), 64)
		return f
	case map[string]any:
		for k, e := range x {
			x[k] = normalizeJSONNumbers(e)
		}
		return x
	case []any:
		for i, e := range x {
			x[i] = normalizeJSONNumbers(e)
		}
		return x
	}
	return v
}

// jqSource returns the JSON text jq reads: the body with every credential
// field and known secret value masked, unless reveal is on.
func jqSource(r tui.SendResult, reveal bool) []byte {
	if r.Response == nil {
		return nil
	}
	if reveal {
		return r.Response.Body
	}
	return []byte(resultRedactor(r).Body(
		history.ContentType(r.Response.Headers), string(r.Response.Body)))
}
