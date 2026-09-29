package request

import (
	"reflect"
	"strings"
	"testing"

	"github.com/shubham/postctl/internal/ui"
)

func TestTextareaViewIsPure(t *testing.T) {
	e := newTextEditor(ui.NewTheme(true, false, false), true)
	e.set(strings.Repeat("line\n", 40))
	e.resize(24, 4)
	e.ta.MoveToEnd()
	before := e.ta
	first, second := e.view(), e.view()
	if first != second || !reflect.DeepEqual(before, e.ta) {
		t.Fatal("textarea View mutated its model")
	}
}
