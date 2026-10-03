package wspane

import "testing"

// Same rule as the request pane: an editor that changes nothing, or fails,
// hands the keyboard back to the URL field.
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
			h.editor()
			if got := h.p.VimMode(); got != "NORMAL" {
				t.Fatalf("after the editor the URL field mode = %q, want NORMAL", got)
			}
		})
	}
}
