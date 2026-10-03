package response

import (
	"strings"
	"testing"
)

func TestJSONPathFilter(t *testing.T) {
	body := []byte(`{"data":{"items":[{"id":"a"}]}}`)
	doc, err := buildJSONDoc(body, ".data.items[0].id")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(doc.lines, "\n"), `"a"`) {
		t.Fatalf("expected filtered id, got %q", doc.lines)
	}
}

func TestSearchInBody(t *testing.T) {
	body := []byte(`{"token":"findme"}`)
	if DetectBodyKind("application/json", body) != BodyJSON {
		t.Fatal("expected json")
	}
}
