package history

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTemplateSensitiveFieldRule(t *testing.T) {
	const secret = "literal-secret-value-abc"
	r := NewRedactor([]string{secret})
	cases := []struct {
		name string
		key  string
		in   string
		want string
	}{
		{"pure ref", "Authorization", "{{accessToken}}", "{{accessToken}}"},
		{"bearer ref", "Authorization", "Bearer {{accessToken}}", "Bearer {{accessToken}}"},
		{"literal token", "Authorization", "Bearer abc123", Mask},
		{"mixed", "X-Api-Key", "prefix-{{key}}-suffix", Mask},
		{"known secret", "X-Public", secret, Mask},
		{"harmless", "X-Trace", "abc", "abc"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := templateFieldValue(r, tc.key, tc.in)
			if got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
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
