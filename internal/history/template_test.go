package history

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shubham/postctl/internal/model"
)

func TestTemplateSensitiveFieldRule(t *testing.T) {
	const secret = "literal-secret-value-abc"
	r := NewRedactor([]string{secret})
	cases := []struct {
		name     string
		key      string
		in       string
		want     string
		wantMask bool
	}{
		{"pure ref", "Authorization", "{{accessToken}}", "{{accessToken}}", false},
		{"bearer ref", "Authorization", "Bearer {{accessToken}}", "Bearer {{accessToken}}", false},
		{"literal token", "Authorization", "Bearer abc123", Mask, true},
		{"mixed", "X-Api-Key", "prefix-{{key}}-suffix", Mask, true},
		{"known secret", "X-Public", secret, Mask, true},
		{"harmless", "X-Trace", "abc", "abc", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, masked := templateFieldValue(r, tc.key, tc.in)
			if got != tc.want || masked != tc.wantMask {
				t.Fatalf("got %q masked=%v want %q masked=%v", got, masked, tc.want, tc.wantMask)
			}
		})
	}
}

func TestTemplateQueryPureRefMatchesURL(t *testing.T) {
	r := NewRedactor(nil)
	req := model.NewRequest("Q", "GET", "{{baseUrl}}/x?api_key={{token}}")
	req.URL.Query = []model.KV{{Key: "api_key", Value: "{{token}}"}}
	tmpl := TemplateFromRequest(req, r)
	if strings.Contains(tmpl.URL, Mask) {
		t.Fatalf("url should keep ref, got %q", tmpl.URL)
	}
	if len(tmpl.Query) != 1 || tmpl.Query[0].Masked {
		t.Fatalf("query param should not be masked: %+v", tmpl.Query)
	}
}

func TestEntryWithoutTemplateStillParses(t *testing.T) {
	dir := t.TempDir()
	log, err := Open(dir, Options{})
	if err != nil {
		t.Fatal(err)
	}
	line := `{"id":"legacy1","time":"2026-01-02T15:04:05Z","requestId":"r1","requestName":"Old","request":{"method":"GET","url":"https://example.test/x"},"status":200,"durationMs":1,"tests":{}}`
	if err := os.WriteFile(filepath.Join(dir, "history.jsonl"), []byte(line+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	log2, err := Open(dir, Options{})
	if err != nil {
		t.Fatal(err)
	}
	e, err := log2.Get("legacy1")
	if err != nil {
		t.Fatal(err)
	}
	if e.Template != nil {
		t.Fatal("legacy entry should have no template")
	}
	raw, _ := json.Marshal(e)
	if strings.Contains(string(raw), "template") {
		t.Fatal("marshal should omit empty template")
	}
	_ = log
}

func TestFieldMaskedLegacyGlyph(t *testing.T) {
	if !FieldMasked(false, Mask) {
		t.Fatal("glyph-only legacy templates should be treated as masked")
	}
}
