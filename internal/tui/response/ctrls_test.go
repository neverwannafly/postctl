package response

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shubham/postctl/internal/tui"
)

// countSender counts the sends the shell asks for.
type countSender struct{ n atomic.Int32 }

func (c *countSender) Send(_ context.Context, req tui.SendRequest, _ func(tui.SendStep)) tui.SendResult {
	c.n.Add(1)
	return tui.SendResult{RequestID: req.Request.ID}
}

// Ctrl-S sends from every pane and mode (AGENTS.md pitfall 13). With the
// keyboard in the response body buffer it used to be handed to the shell as
// an unhandled key, which dropped it.
func TestShellCtrlSSendsFromTheResponseBody(t *testing.T) {
	for _, key := range []string{"ctrl+s", "ctrl+enter"} {
		t.Run(key, func(t *testing.T) {
			sender := &countSender{}
			r := newShellRigOptions(t, 140, 40, func(o *tui.Options) { o.Sender = sender })
			r.open()
			if !r.pane.vimLive() || !r.pane.focused {
				t.Fatal("the body buffer should have the keyboard")
			}
			r.keys(key)
			deadline := time.Now().Add(2 * time.Second)
			for sender.n.Load() == 0 && time.Now().Before(deadline) {
				r.pump(100 * time.Millisecond)
			}
			if got := sender.n.Load(); got != 1 {
				t.Fatalf("%s from the response body sent %d times, want 1", key, got)
			}
		})
	}
}
