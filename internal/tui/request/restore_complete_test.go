package request

import (
	"testing"

	hist "github.com/shubham/postctl/internal/history"
	"github.com/shubham/postctl/internal/model"
)

func TestApplyTemplateReplacesEmptySlices(t *testing.T) {
	req := model.NewRequest("R", "GET", "https://example.test/old")
	req.Headers = []model.KV{{Key: "X-Old", Value: "1"}}
	req.URL.Query = []model.KV{{Key: "q", Value: "old"}}
	req.Body = model.Body{Mode: model.BodyRaw, Raw: "old-body"}
	tmpl := hist.RequestTemplate{
		Method:   "POST",
		URL:      "https://example.test/new",
		Headers:  []hist.TemplateKV{},
		Query:    []hist.TemplateKV{},
		BodyMode: model.BodyRaw,
		BodyRaw:  "new-body",
	}
	_, _ = applyTemplate(req, &tmpl)
	if req.Method != "POST" || req.URL.Raw != "https://example.test/new" {
		t.Fatalf("method/url not replaced")
	}
	if len(req.Headers) != 0 || len(req.URL.Query) != 0 {
		t.Fatalf("expected empty headers/query, got %d %d", len(req.Headers), len(req.URL.Query))
	}
	if req.Body.Raw != "new-body" {
		t.Fatalf("body = %q", req.Body.Raw)
	}
}

func TestApplyTemplateRefusesMaskGlyphsInValues(t *testing.T) {
	req := model.NewRequest("R", "GET", "https://example.test/keep")
	req.URL.Query = []model.KV{{Key: "api_key", Value: "secret-value"}}
	tmpl := hist.RequestTemplate{
		Query: []hist.TemplateKV{{Key: "api_key", Value: "api_key=" + hist.Mask, Masked: true}},
	}
	kept, _ := applyTemplate(req, &tmpl)
	if req.URL.Query[0].Value != "secret-value" {
		t.Fatalf("kept current query value")
	}
	if len(kept) == 0 {
		t.Fatal("expected kept field names")
	}
}
