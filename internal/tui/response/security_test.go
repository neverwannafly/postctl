package response

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shubham/postctl/internal/history"
	"github.com/shubham/postctl/internal/httpx"
	"github.com/shubham/postctl/internal/model"
	"github.com/shubham/postctl/internal/tui"
	"github.com/shubham/postctl/internal/ui"
)

func TestResponseHeadersMaskSetCookie(t *testing.T) {
	const secret = "synthetic-cookie-secret"
	p := New(tui.PaneEnv{Theme: ui.NewTheme(true, true, false), Actions: &tui.Registry{}}).(*Pane)
	out := p.viewHeaders(tui.SendResult{
		Response: &httpx.Response{Headers: []model.KV{
			{Key: "Set-Cookie", Value: "session=" + secret},
			{Key: "X-Echo", Value: secret},
		}},
		Redactor: history.NewRedactor([]string{secret}),
	}, 80, 20)
	if strings.Contains(out, secret) {
		t.Fatalf("headers exposed Set-Cookie: %q", out)
	}
}

func TestResponseHeadersEmptyStateHasNextAction(t *testing.T) {
	p := New(tui.PaneEnv{Theme: ui.NewTheme(true, true, false), Actions: &tui.Registry{}}).(*Pane)
	out := p.viewHeaders(tui.SendResult{}, 80, 20)
	if !strings.Contains(out, "Send") {
		t.Fatalf("empty headers state has no next action: %q", out)
	}
}

func TestConsoleAndTestOutputMaskKnownValues(t *testing.T) {
	const secret = "synthetic-script-output-secret"
	p := New(tui.PaneEnv{Theme: ui.NewTheme(true, true, false), Actions: &tui.Registry{}}).(*Pane)
	result := tui.SendResult{
		Tests:    []tui.TestOutcome{{Name: "check " + secret, Message: "got " + secret}},
		Console:  []tui.ConsoleLine{{Level: "log", Text: "value " + secret, Event: "test"}},
		Redactor: history.NewRedactor([]string{secret}),
	}
	if out := p.viewTests(result, 100, 20); strings.Contains(out, secret) {
		t.Fatalf("test output exposed a secret: %q", out)
	}
	if out := p.viewConsole(result, 100, 20); strings.Contains(out, secret) {
		t.Fatalf("console output exposed a secret: %q", out)
	}
}

func TestPagerDoesNotCreateResponseTempFile(t *testing.T) {
	pattern := filepath.Join(os.TempDir(), "postctl-response-*")
	before, err := filepath.Glob(pattern)
	if err != nil {
		t.Fatal(err)
	}
	p := New(tui.PaneEnv{Theme: ui.NewTheme(true, true, false), Actions: &tui.Registry{}}).(*Pane)
	id := model.NewID()
	p.ref = tui.RequestRef{Request: &model.Request{ID: id}}
	p.applyResult(tui.SendResult{
		RequestID: id,
		Response:  &httpx.Response{Body: []byte("synthetic pager body")},
	})
	if cmd := p.cmdOpenPager(); cmd == nil {
		t.Fatal("pager command was nil")
	}
	after, err := filepath.Glob(pattern)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range after {
		if !containsPath(before, path) {
			_ = os.Remove(path)
		}
	}
	if len(after) != len(before) {
		t.Fatalf("pager created a response file in the shared temp directory: before=%d after=%d", len(before), len(after))
	}
}

func TestPagerUsesMaskedStdinAndRejectsInterpreters(t *testing.T) {
	const secret = "synthetic-pager-secret"
	result := tui.SendResult{
		Response: &httpx.Response{
			Headers: []model.KV{{Key: "Content-Type", Value: "application/json"}},
			Body:    []byte(`{"value":"` + secret + `"}`),
		},
		Redactor: history.NewRedactor([]string{secret}),
	}
	masked := pagerBody(result, false)
	if strings.Contains(string(masked), secret) || !strings.Contains(string(masked), history.Mask) {
		t.Fatalf("pager output was not masked: %q", masked)
	}
	cmd, err := pagerCommand("cat", masked)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := io.ReadAll(cmd.Stdin)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(raw); strings.Contains(got, secret) || !strings.Contains(got, history.Mask) {
		t.Fatalf("pager stdin was not masked: %q", got)
	}
	if _, err := pagerCommand("sh", []byte(secret)); err == nil {
		t.Fatal("shell was accepted as a pager")
	}
}

func containsPath(paths []string, target string) bool {
	for _, path := range paths {
		if path == target {
			return true
		}
	}
	return false
}
