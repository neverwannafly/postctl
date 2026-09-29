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

func TestJSONFoldReducesVisibleLines(t *testing.T) {
	body := []byte(`{"a":{"b":1,"c":2},"d":3}`)
	doc, err := buildJSONDoc(body, "")
	if err != nil {
		t.Fatal(err)
	}
	before := doc.visibleLineCount()
	doc.folded[0] = true
	if doc.visibleLineCount() >= before {
		t.Fatalf("fold should hide lines: before=%d after=%d", before, doc.visibleLineCount())
	}
}

func TestSearchInBody(t *testing.T) {
	body := []byte(`{"token":"findme"}`)
	if DetectBodyKind("application/json", body) != BodyJSON {
		t.Fatal("expected json")
	}
}
