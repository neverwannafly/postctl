package wspane

import (
	"strings"
	"testing"

	"github.com/shubham/postctl/internal/tui/vimfield"
	"github.com/shubham/postctl/internal/tui/vimtext"
	"github.com/shubham/postctl/internal/ui"
)

// TestFieldViewIsPure is the successor of the textarea purity test: drawing
// the composer, even a long one with the cursor past the window, changes
// nothing.
func TestFieldViewIsPure(t *testing.T) {
	f := vimfield.New(ui.NewTheme(true, false, false), vimtext.Options{})
	f.Set(strings.Repeat("message\n", 40))
	f.Fit(24, 4)
	f.Begin("")
	f.MoveEnd()
	before := f.Text()
	r, c := f.Cursor()
	first, second := f.Render(24, 4), f.Render(24, 4)
	r2, c2 := f.Cursor()
	if first != second || before != f.Text() || r != r2 || c != c2 {
		t.Fatal("rendering a WebSocket field changed it")
	}
}
