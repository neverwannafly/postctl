package qa_test

import (
	"strings"
	"testing"
	"time"
)

// An action's result shows in the shell toast, so it is visible whichever
// pane has focus: at 80x24 only one pane is drawn, and the request pane's own
// status row is not on screen while the response is focused.
func TestActionNoticesShowWhicheverPaneIsFocused(t *testing.T) {
	sec := newUXSecrets(t)
	fx := newUXFixture(t, sec)
	r := newUXRig(t, fx, 80, 24)
	r.keys("space", "e", "down", "enter")
	r.open("ech")
	r.keys("ctrl+s")
	if !r.waitFor("200", 8*time.Second) {
		t.Fatalf("the send did not produce a 200 response (frame: %s)", frameShape(r.screen()))
	}
	r.keys("g", "r") // the response pane is the one on screen
	r.keys("space", "y")
	r.wait(200 * time.Millisecond)
	if !strings.Contains(r.screen(), "Copied as curl, secrets masked") {
		t.Fatalf("the curl result is not on screen with the response pane focused (frame: %s)", frameShape(r.screen()))
	}
}
