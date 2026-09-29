package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGeneratedKeysDocMatchesFile(t *testing.T) {
	docPath := filepath.Join("..", "..", "docs", "keys.md")
	data, err := os.ReadFile(docPath)
	if err != nil {
		t.Fatalf("could not read %s: %v", docPath, err)
	}
	expected := GenerateMarkdown()
	if string(data) != expected {
		t.Fatalf("docs/keys.md does not match generator output. Run 'go run ./cmd/genkeys -out docs/keys.md' to update.")
	}
}

func TestCatalogKeysAreNonEmpty(t *testing.T) {
	for _, sec := range KeyCatalog {
		if sec.Title == "" {
			t.Errorf("section with empty title found")
		}
		if len(sec.Entries) == 0 {
			t.Errorf("section %q has no key entries", sec.Title)
		}
		for _, e := range sec.Entries {
			if e.Key == "" {
				t.Errorf("section %q has entry with empty key", sec.Title)
			}
			if e.Action == "" {
				t.Errorf("section %q has entry %q with empty action", sec.Title, e.Key)
			}
			if e.Description == "" {
				t.Errorf("section %q has entry %q with empty description", sec.Title, e.Key)
			}
		}
	}
}

func TestDocsContainsWebSocketTODO(t *testing.T) {
	doc := GenerateMarkdown()
	if !strings.Contains(doc, "WebSocket Pane") {
		t.Errorf("expected WebSocket Pane section")
	}
	if !strings.Contains(doc, "TODO") {
		t.Errorf("expected TODO note in WebSocket Pane section")
	}
}
