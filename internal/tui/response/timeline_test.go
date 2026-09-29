package response

import (
	"strings"
	"testing"
	"time"

	"github.com/shubham/postctl/internal/httpx"
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
	out := RenderTimeline(th, resp, 80, false)
	if !strings.Contains(out, "DNS") || !strings.Contains(out, th.Glyphs.BarFull) {
		t.Fatalf("timeline missing bars: %q", out)
	}
}
