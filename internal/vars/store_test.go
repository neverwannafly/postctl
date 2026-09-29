package vars

import (
	"math/rand"
	"reflect"
	"regexp"
	"testing"

	"github.com/shubham/postctl/internal/model"
)

func TestStorePrecedenceOrderAndChanges(t *testing.T) {
	t.Parallel()
	store := New(
		[]model.Variable{{Key: "same", Value: "global"}, {Key: "g", Value: "1"}, {Key: "off", Value: "x", Disabled: true}},
		[]model.Variable{{Key: "same", Value: "collection"}, {Key: "c", Value: "2"}},
		[]model.Variable{{Key: "same", Value: "environment"}, {Key: "e", Value: "3"}},
		map[string]any{"same": "data"},
	)
	store.Set(Local, "same", "local")

	value, scope, ok := store.Get("same")
	if !ok || value != "local" || scope != Local {
		t.Fatalf("Get(same) = (%v, %v, %v)", value, scope, ok)
	}
	if _, _, ok := store.Get("off"); ok {
		t.Fatal("disabled variable was loaded")
	}
	if got := store.Keys(Global); !reflect.DeepEqual(got, []string{"same", "g"}) {
		t.Fatalf("global order = %v", got)
	}
	store.Set(Environment, "new", 42)
	store.Unset(Environment, "e")
	changes := store.Changes()[Environment]
	if len(changes) != 2 || changes[0].Key != "new" || changes[1].Key != "e" || !changes[1].Unset {
		t.Fatalf("changes = %#v", changes)
	}
}

func TestReplaceNestingCyclesWhitespaceAndStringification(t *testing.T) {
	t.Parallel()
	store := New(
		[]model.Variable{
			{Key: "nested", Value: "{{ value }}"},
			{Key: "value", Value: "done"},
			{Key: "a", Value: "{{b}}"},
			{Key: "b", Value: "{{a}}"},
		},
		nil,
		nil,
		nil,
	)
	store.Set(Local, "object", map[string]any{"ok": true})
	store.Set(Local, "array", []any{1.0, "two"})
	store.Set(Local, "nothing", nil)
	store.Set(Local, "number", 1.25)

	got := store.Replace("{{ nested }} {{unknown}} {{object}} {{array}} x{{nothing}}y {{number}}")
	want := `done {{unknown}} {"ok":true} [1,"two"] xy 1.25`
	if got != want {
		t.Fatalf("Replace() = %q, want %q", got, want)
	}
	cycle := store.Replace("{{a}}")
	if matched, _ := regexp.MatchString(`^\{\{[ab]\}\}$`, cycle); !matched {
		t.Fatalf("cycle resolved unexpectedly: %q", cycle)
	}
}

func TestDynamicVariablesUseFreshInjectedRandomness(t *testing.T) {
	t.Parallel()
	source := rand.New(rand.NewSource(7)) //nolint:gosec // deterministic test source
	store := NewWithRandom(nil, nil, nil, nil, source)
	got := store.Replace("{{$randomInt}} {{$randomInt}} {{$randomUUID}}")
	parts := regexp.MustCompile(`\s+`).Split(got, -1)
	if len(parts) != 3 || parts[0] == parts[1] {
		t.Fatalf("dynamic values were not fresh: %q", got)
	}
	if !regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`).MatchString(parts[2]) {
		t.Fatalf("UUID = %q", parts[2])
	}
}

func TestClearRecordsAndRemovesInOrder(t *testing.T) {
	t.Parallel()
	store := New(nil, []model.Variable{{Key: "one", Value: "1"}, {Key: "two", Value: "2"}}, nil, nil)
	store.Clear(Collection)
	if keys := store.Keys(Collection); len(keys) != 0 {
		t.Fatalf("keys after clear = %v", keys)
	}
	changes := store.Changes()[Collection]
	if len(changes) != 2 || changes[0].Key != "one" || changes[1].Key != "two" {
		t.Fatalf("clear changes = %#v", changes)
	}
}
