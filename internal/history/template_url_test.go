package history

import (
	"strings"
	"testing"

	"github.com/shubham/postctl/internal/model"
)

func TestRebuildURLRawFragmentAndDisabledQuery(t *testing.T) {
	u := model.ParseURL("https://example.test/path?on=1&off=2#frag")
	u.Query[1].Disabled = true
	raw, _ := templateURL(u, nil)
	if !strings.Contains(raw, "#frag") || !strings.Contains(raw, "on=1") {
		t.Fatalf("raw = %q", raw)
	}
	if strings.Contains(raw, "off=2") {
		t.Fatalf("disabled query leaked into raw %q", raw)
	}
}

func TestTemplateURLKeepsVariableReferencesUnescaped(t *testing.T) {
	u := model.ParseURL("https://example.test/p?y={{b}}#f")
	tmpl := TemplateFromRequest(&model.Request{URL: u}, NewRedactor(nil))
	if tmpl.URL != "https://example.test/p?y={{b}}#f" {
		t.Fatal("template URL did not preserve the raw variable reference")
	}
	if tmpl.URLMasked {
		t.Fatal("clean query URL was marked masked")
	}
	if len(tmpl.Query) != 1 || tmpl.Query[0].Value != "{{b}}" || tmpl.Query[0].Masked {
		t.Fatal("clean query row was not preserved")
	}
}

func TestTemplateURLMarksOnlyRedactorChanges(t *testing.T) {
	const marker = "synthetic-query-marker"
	u := model.ParseURL("https://example.test/p?y=" + marker)
	tmpl := TemplateFromRequest(&model.Request{URL: u}, NewRedactor([]string{marker}))
	if !tmpl.URLMasked || len(tmpl.Query) != 1 || !tmpl.Query[0].Masked {
		t.Fatal("known query secret was not marked consistently")
	}
}
