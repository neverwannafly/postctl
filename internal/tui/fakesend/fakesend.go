// Package fakesend is a canned [tui.Sender] for tests and for the demo
// binary until the real engine adapter (WS4) replaces it. It resolves
// {{variables}} for display, reports the same steps the real pipeline does,
// and answers with a small JSON response. It never touches the network.
package fakesend

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/shubham/postctl/internal/httpx"
	"github.com/shubham/postctl/internal/model"
	"github.com/shubham/postctl/internal/tui"
	"github.com/shubham/postctl/internal/vars"
)

// DefaultSteps are the steps reported for every send, in order.
var DefaultSteps = []string{
	"Resolving variables",
	"Running pre-request script",
	"Waiting for response",
	"Running tests",
}

// Sender implements [tui.Sender] with canned results.
type Sender struct {
	// Steps are reported in order; DefaultSteps when nil.
	Steps []string
	// StepDelay is the pause after each step; zero means none.
	StepDelay time.Duration
	// Status is the response status code; 200 when zero.
	Status int
	// Err makes every send fail with this transport error.
	Err error
	// Gate, when set, blocks each step until a value is received or the
	// context ends (deterministic tests).
	Gate chan struct{}

	mu    sync.Mutex
	calls []tui.SendRequest
}

// New returns a sender with the default steps and a short delay so the demo
// shows real progress.
func New() *Sender { return &Sender{StepDelay: 350 * time.Millisecond} }

// Calls returns the requests received so far (copies of the pointers).
func (s *Sender) Calls() []tui.SendRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]tui.SendRequest(nil), s.calls...)
}

// Send implements [tui.Sender].
func (s *Sender) Send(ctx context.Context, req tui.SendRequest, progress func(tui.SendStep)) tui.SendResult {
	s.mu.Lock()
	s.calls = append(s.calls, req)
	s.mu.Unlock()

	res := tui.SendResult{RequestID: req.Request.ID, Started: time.Now()}
	steps := s.Steps
	if steps == nil {
		steps = DefaultSteps
	}
	for _, text := range steps {
		progress(tui.SendStep{Text: text})
		if s.Gate != nil {
			select {
			case <-s.Gate:
			case <-ctx.Done():
				res.Err = ctx.Err()
				return res
			}
		}
		if s.StepDelay > 0 {
			select {
			case <-time.After(s.StepDelay):
			case <-ctx.Done():
				res.Err = ctx.Err()
				return res
			}
		}
		if err := ctx.Err(); err != nil {
			res.Err = err
			return res
		}
	}
	if s.Err != nil {
		res.Err = s.Err
		return res
	}

	url := resolveURL(req)
	status := s.Status
	if status == 0 {
		status = http.StatusOK
	}
	body, _ := json.MarshalIndent(map[string]any{
		"ok": status < 400, "method": req.Request.Method, "url": url, "note": "canned response from the fake sender",
	}, "", "  ")
	res.Response = &httpx.Response{
		StatusCode: status,
		Status:     http.StatusText(status),
		Proto:      "HTTP/1.1",
		Headers:    []model.KV{{Key: "Content-Type", Value: "application/json"}, {Key: "X-Fake-Sender", Value: "1"}},
		Body:       body,
		Size:       httpx.Size{Headers: 96, Body: int64(len(body))},
		Timings:    httpx.Timings{TTFB: 42 * time.Millisecond, Total: 58 * time.Millisecond},
		Sent:       httpx.SentRequest{Method: req.Request.Method, URL: url},
	}
	if req.Request.Scripts.Test != nil && !req.Request.Scripts.Test.Empty() {
		res.Tests = []tui.TestOutcome{{Name: "Status code is 2xx", Passed: status/100 == 2, Message: "expected a 2xx status"}}
	}
	return res
}

// resolveURL substitutes variables for display; unknown ones stay as written.
func resolveURL(req tui.SendRequest) string {
	var env, coll []model.Variable
	if req.Environment != nil {
		env = req.Environment.Variables
	}
	if req.Collection != nil {
		coll = req.Collection.Variables
	}
	return vars.New(nil, coll, env, nil).Replace(strings.TrimSpace(req.Request.URL.Raw))
}

// ErrOffline is a ready-made transport error for tests.
var ErrOffline = errors.New("dial tcp: no route to host")

var _ tui.Sender = (*Sender)(nil)
