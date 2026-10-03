package wspane

import "testing"

// While the log's direction filter is not "all", the pane says so in text
// (never color alone, in every look); the indicator goes when the filter is
// back to all. Changing the filter is also announced once by a shell toast.
func TestLogFilterIndicatorShowsWhileFilteredAtEverySize(t *testing.T) {
	steps := []struct {
		name  string
		want  string // "" means the indicator must be absent
		toast string
	}{
		{"all", "", ""},
		{"received", "Showing: received only", "Showing: received only"},
		{"sent", "Showing: sent only", "Showing: sent only"},
		{"all again", "", "Showing: all"},
	}
	for _, l := range looks() {
		for _, sz := range sizes {
			t.Run(l.name+"/"+sz.name, func(t *testing.T) {
				h := newHarness(t, withLook(l, sz.w, sz.h))
				feed(h)
				h.key("tab", "tab")
				for i, st := range steps {
					if i > 0 {
						before := len(h.toasts())
						h.key("f")
						got := h.toasts()[before:]
						if len(got) != 1 || got[0] != st.toast {
							t.Fatalf("%s: toasts after the filter change = %q, want exactly %q", st.name, got, st.toast)
						}
					}
					if st.want == "" {
						h.mustNotHave("Showing:")
						continue
					}
					h.mustHave(st.want)
				}
			})
		}
	}
}
