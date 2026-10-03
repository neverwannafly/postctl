package qa_test

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// An action's result is shown once, in the shell toast, whichever pane has
// focus and at every size. At 80x24 only one pane is drawn, so a pane's own
// status row would be off screen; at wider sizes a pane copy would double the
// text.
func TestActionNoticesShowExactlyOnceAtEverySize(t *testing.T) {
	const notice = "Copied as curl, secrets masked"
	for _, size := range []struct{ w, h int }{{80, 24}, {120, 40}, {200, 60}} {
		for _, focus := range []string{"request", "response"} {
			t.Run(fmt.Sprintf("%dx%d/%s focused", size.w, size.h, focus), func(t *testing.T) {
				t.Parallel()
				sec := newUXSecrets(t)
				fx := newUXFixture(t, sec)
				r := newUXRig(t, fx, size.w, size.h)
				r.keys("space", "e", "down", "enter")
				r.open("ech")
				r.keys("ctrl+s")
				if !r.waitFor("200", 8*time.Second) {
					t.Fatalf("the send did not produce a 200 response (frame: %s)", frameShape(r.screen()))
				}
				if focus == "response" {
					r.keys("g", "r")
				} else {
					r.keys("g", "u")
				}
				r.keys("space", "y")
				r.wait(200 * time.Millisecond)
				if n := strings.Count(r.screen(), notice); n != 1 {
					t.Fatalf("the curl result appears %d times on a frame of %s, want 1", n, frameShape(r.screen()))
				}
			})
		}
	}
}
