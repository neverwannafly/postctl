package tui

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"charm.land/bubbles/v2/key"
)

func TestAllCodeKeyBindingsDocumentedInKeysDoc(t *testing.T) {
	// Find docs/keys.md relative to repo root
	root := "../.."
	docPath := filepath.Join(root, "docs", "keys.md")
	data, err := os.ReadFile(docPath)
	if err != nil {
		t.Fatalf("could not read %s: %v", docPath, err)
	}
	doc := string(data)

	km := newKeyMap(false)
	val := reflect.ValueOf(km)
	typ := val.Type()

	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if field.Type != reflect.TypeOf(key.Binding{}) {
			continue
		}
		binding := val.Field(i).Interface().(key.Binding)
		help := binding.Help()
		if help.Key == "" {
			continue
		}

		// Ensure the help key or description is mentioned in docs/keys.md
		keyName := help.Key
		if !strings.Contains(doc, keyName) && !strings.Contains(doc, help.Desc) {
			t.Errorf("key binding %s (%s: %q) exists in code's keyMap but is missing from docs/keys.md",
				field.Name, keyName, help.Desc)
		}

		// Also check each actual raw key trigger (e.g. "ctrl+p", "ctrl+s")
		for _, k := range binding.Keys() {
			normalized := strings.ReplaceAll(strings.ToLower(k), " ", "")
			variants := []string{normalized, strings.ReplaceAll(normalized, "+", "-")}
			switch normalized {
			case "pgdown":
				variants = append(variants, "pgdn", "pagedown")
			case "pgup":
				variants = append(variants, "pageup")
			case "up":
				variants = append(variants, "↑")
			case "down":
				variants = append(variants, "↓")
			case "left":
				variants = append(variants, "←")
			case "right":
				variants = append(variants, "→")
			case "alt+up":
				variants = append(variants, "alt-↑", "alt+↑")
			case "alt+down":
				variants = append(variants, "alt-↓", "alt+↓")
			}

			var found bool
			docLower := strings.ToLower(doc)
			for _, v := range variants {
				if strings.Contains(docLower, v) {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("raw key %q for binding %s (%s) is not documented in docs/keys.md",
					k, field.Name, help.Desc)
			}
		}
	}
}

func TestKeysDocCoversAllContexts(t *testing.T) {
	docPath := filepath.Join("../..", "docs", "keys.md")
	data, err := os.ReadFile(docPath)
	if err != nil {
		t.Fatalf("could not read %s: %v", docPath, err)
	}
	doc := string(data)

	requiredSections := []string{
		"Global Shortcuts",
		"Collections Sidebar",
		"Request Pane",
		"Response Pane",
		"Environment and Variable Editor",
		"Cookie Manager",
		"Collection Runner",
		"Request Send History",
		"Command Palette",
		"WebSocket Pane",
	}

	for _, sec := range requiredSections {
		if !strings.Contains(doc, sec) {
			t.Errorf("docs/keys.md missing required section %q", sec)
		}
	}
}

func TestKeysDocCoversWebSocketPane(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("../..", "docs", "keys.md"))
	if err != nil {
		t.Fatal(err)
	}
	doc := string(data)
	required := []string{
		"Ctrl-S, Ctrl-Enter",
		"Ctrl-L",
		"Enter (URL bar)",
		"c (tab bar)",
		"Ctrl-T (composer)",
		"Ctrl-R (composer)",
		"Ctrl-X (composer)",
		"Tab, Shift-Tab",
		"Enter (saved)",
		"e, r, d, a (saved)",
		"Alt-↑, Alt-↓ (saved)",
		"PgUp, PgDn",
		"f, / (log)",
		"c, x, p (log)",
		"←, → (detail)",
	}
	for _, key := range required {
		if !strings.Contains(doc, key) {
			t.Errorf("docs/keys.md missing WebSocket key coverage for %q", key)
		}
	}
}
