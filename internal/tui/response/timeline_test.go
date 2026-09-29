package response

import (
	"strings"
	"testing"
	"time"

	"github.com/shubham/postctl/internal/history"
	"github.com/shubham/postctl/internal/httpx"
	"github.com/shubham/postctl/internal/model"
	"github.com/shubham/postctl/internal/ui"
)

func TestTimelineBars(t *testing.T) {
	th := ui.NewTheme(true, false, false)
	resp := &httpx.Response{
		Timings: httpx.Timings{
			DNS:      10 * time.Millisecond,
			Connect:  20 * time.Millisecond,
			TTFB:     30 * time.Millisecond,
			Download: 40 * time.Millisecond,
			Total:    100 * time.Millisecond,
		},
		RemoteAddr: "127.0.0.1:443",
	}
	out := RenderTimeline(th, resp, 80, false, nil)
	if !strings.Contains(out, "DNS") || !strings.Contains(out, th.Glyphs.BarFull) {
		t.Fatalf("timeline missing bars: %q", out)
	}
}

func TestTimelineMasksCredentialURLsAndHeaders(t *testing.T) {
	const secret = "synthetic-timeline-secret"
	th := ui.NewTheme(true, true, false)
	resp := &httpx.Response{
		Redirects: []httpx.Redirect{{StatusCode: 302, Location: "https://example.test/next?access_token=" + secret}},
		Sent: httpx.SentRequest{
			Method: "GET",
			URL:    "https://example.test/path/" + secret + "?access_token=" + secret,
			Headers: []model.KV{
				{Key: "Authorization", Value: "Bearer " + secret},
				{Key: "Set-Cookie", Value: "session=" + secret},
			},
		},
	}
	out := RenderTimeline(th, resp, 100, false, history.NewRedactor([]string{secret}))
	if strings.Contains(out, secret) {
		t.Fatalf("timeline exposed a credential: %q", out)
	}
}

func TestTimelineEmptyStateHasNextAction(t *testing.T) {
	out := RenderTimeline(ui.NewTheme(true, true, false), nil, 80, false, nil)
	if !strings.Contains(out, "Send") {
		t.Fatalf("empty timeline has no next action: %q", out)
	}
}
