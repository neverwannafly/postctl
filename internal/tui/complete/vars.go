// Package complete provides the completion sources of the request editors:
// {{variable}} names from the resolved scope chain and pm.* members in
// scripts. Both implement [vimtext.Completer] and are pure: they read the
// layers or table they were built with and never touch the disk, the
// Keychain or the network.
package complete

import (
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/shubham/postctl/internal/model"
	"github.com/shubham/postctl/internal/tui/vimtext"
	"github.com/shubham/postctl/internal/vars"
)

// Layer is one variable scope. Callers list layers from the lowest precedence
// to the highest, in the same order the engine resolves them: global,
// collection, folders (outermost first), environment. A name defined in
// several layers is reported once, from the highest layer.
type Layer struct {
	// Scope is the label shown beside a candidate, for example "global",
	// "collection", "folder Charges" or "environment".
	Scope string
	// Variables are the layer's variables; disabled ones are skipped.
	Variables []model.Variable
}

// maxVarItems caps the candidates returned for one keystroke.
const maxVarItems = 60

// detailWidth is the most characters of a plain value shown in Detail.
const detailWidth = 24

// Glyphs are the theme glyphs the completers draw with, so ASCII mode stays
// ASCII.
type Glyphs struct {
	// Mask replaces a secret variable's value.
	Mask string
	// Dot separates the scope from the value.
	Dot string
	// Ellipsis ends a shortened value.
	Ellipsis string
}

type varCompleter struct {
	layers func() []Layer
	g      Glyphs
}

// Vars returns the {{variable}} completer. layers is called on every
// keystroke, so the list follows the active environment. The Detail of a
// secret variable shows g.Mask instead of the value; secret values are never
// read.
//
// Typing "{{" lists every variable and every {{$dynamic}} generator; more
// characters filter by prefix, then by substring. Accepting a candidate
// inserts "name}}".
func Vars(layers func() []Layer, g Glyphs) vimtext.Completer {
	if g.Dot == "" {
		g.Dot = "-"
	}
	if g.Ellipsis == "" {
		g.Ellipsis = "..."
	}
	return &varCompleter{layers: layers, g: g}
}

// Complete implements [vimtext.Completer].
func (c *varCompleter) Complete(before string) ([]vimtext.Item, int) {
	open := strings.LastIndex(before, "{{")
	if open < 0 {
		return nil, 0
	}
	typed := before[open+2:]
	prefix := strings.TrimLeft(typed, " ")
	if strings.ContainsAny(prefix, " {}\"'") {
		return nil, 0
	}
	type entry struct {
		name, detail string
		dynamic      bool
	}
	seen := map[string]int{}
	var entries []entry
	if c.layers != nil {
		for _, layer := range c.layers() {
			for _, v := range layer.Variables {
				if v.Key == "" || !v.Enabled() {
					continue
				}
				value := c.g.Mask
				if !v.IsSecret() {
					value = shorten(v.Value, c.g.Ellipsis)
				}
				e := entry{name: v.Key, detail: layer.Scope}
				if value != "" {
					e.detail += " " + c.g.Dot + " " + value
				}
				if i, ok := seen[v.Key]; ok {
					entries[i] = e // a higher layer replaces the lower one
					continue
				}
				seen[v.Key] = len(entries)
				entries = append(entries, e)
			}
		}
	}
	for _, name := range vars.DynamicNames() {
		if _, ok := seen[name]; !ok {
			entries = append(entries, entry{name: name, detail: "dynamic", dynamic: true})
		}
	}
	lower := strings.ToLower(prefix)
	var starts, contains []vimtext.Item
	var dynStarts []vimtext.Item
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].dynamic != entries[j].dynamic {
			return !entries[i].dynamic
		}
		return strings.ToLower(entries[i].name) < strings.ToLower(entries[j].name)
	})
	for _, e := range entries {
		name := strings.ToLower(e.name)
		if e.dynamic && !strings.HasPrefix(lower, "$") {
			name = strings.TrimPrefix(name, "$") // "gu" finds $guid, and nothing deeper
		}
		item := vimtext.Item{Label: e.name, Detail: e.detail, Insert: e.name + "}}"}
		switch {
		case strings.HasPrefix(name, lower):
			if e.dynamic {
				dynStarts = append(dynStarts, item)
			} else {
				starts = append(starts, item)
			}
		case lower != "" && !e.dynamic && strings.Contains(name, lower):
			contains = append(contains, item)
		}
	}
	items := append(append(starts, contains...), dynStarts...)
	if len(items) > maxVarItems {
		items = items[:maxVarItems]
	}
	return items, len(typed)
}

// shorten flattens a plain value to one short line for Detail.
func shorten(s, ellipsis string) string {
	s = strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(s) <= detailWidth {
		return s
	}
	r := []rune(s)
	return string(r[:detailWidth-1]) + ellipsis
}

// chain tries each completer in order.
type chain []vimtext.Completer

// Chain returns a completer that asks each of cs in turn and returns the first
// non-empty answer. Nil entries are skipped; with none it returns nil.
func Chain(cs ...vimtext.Completer) vimtext.Completer {
	var out chain
	for _, c := range cs {
		if c != nil {
			out = append(out, c)
		}
	}
	switch len(out) {
	case 0:
		return nil
	case 1:
		return out[0]
	}
	return out
}

// Complete implements [vimtext.Completer].
func (c chain) Complete(before string) ([]vimtext.Item, int) {
	for _, one := range c {
		if items, replace := one.Complete(before); len(items) > 0 {
			return items, replace
		}
	}
	return nil, 0
}
