package history

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/shubham/postctl/internal/model"
)

func TestLiteralSecretNotStoredInTemplateJSON(t *testing.T) {
	const sentinel = "template-literal-sentinel-value"
	r := NewRedactor([]string{sentinel})
	req := model.NewRequest("Lit", "POST", "https://example.test/x?api_key="+sentinel)
	req.Headers = []model.KV{{Key: "Authorization", Value: "Bearer " + sentinel}}
	req.Body = model.Body{Mode: model.BodyRaw, Raw: `{"password":"` + sentinel + `"}`}
	tmpl := TemplateFromRequest(req, r)
	raw, err := json.Marshal(tmpl)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), sentinel) {
		t.Fatal("sentinel present in template JSON")
	}
}
