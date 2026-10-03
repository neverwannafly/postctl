package request

import "testing"

// An editor that changes nothing, or fails, hands the keyboard back: the URL
// bar was blurred for the round trip, and a bar that stays blurred ignores
// every key (g jumps, Space, typing) until something else moves focus.
func TestEditorThatChangesNothingReturnsToTheURLField(t *testing.T) {
	for name, script := range map[string]string{
		"unchanged": `true`,
		"fails":     `exit 1`,
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, editorDeps(script))
			h.key("up")
			if h.p.VimMode() != "NORMAL" {
				t.Fatalf("setup: the URL field should be in normal mode, got %q", h.p.VimMode())
			}
			h.action("request.editor")
			if got := h.p.VimMode(); got != "NORMAL" {
				t.Fatalf("after the editor the URL field mode = %q, want NORMAL", got)
			}
			h.key("g", "b") // a forwarded g jump must still work
		})
	}
}
