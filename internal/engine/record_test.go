package engine

import (
	"net/http"
	"strings"
	"testing"

	"github.com/shubham/postctl/internal/history"
	"github.com/shubham/postctl/internal/model"
)

func TestHistoryTemplateFromUnresolvedRequest(t *testing.T) {
	srv := newServer(t, jsonHandler(`{"ok":true}`))
	f := newFixture(t, srv)
	const secret = "template-secret-value-xyz"
	f.env.Variables = append(f.env.Variables,
		model.Variable{Key: "token", Value: secret, Type: model.VarSecret})
	req := model.NewRequest("T", "POST", "{{baseUrl}}/items/{{id}}?trace=1")
	req.Headers = []model.KV{
		{Key: "Authorization", Value: "Bearer {{token}}"},
		{Key: "X-Api-Key", Value: "plain-wrong"},
	}
	req.Body = model.Body{Mode: model.BodyRaw, Raw: `{"note":"{{token}}"}`}
	f.col.Items = append(f.col.Items, req)
	f.save()

	out := f.send(f.findRequest("T"), nil)
	entry, err := f.eng.cfg.History.(*history.Log).Get(out.HistoryID)
	if err != nil {
		t.Fatal(err)
	}
	if entry.Template == nil {
		t.Fatal("missing template")
	}
	if entry.Template.URL != "{{baseUrl}}/items/{{id}}?trace=1" && !strings.Contains(entry.Template.URL, "{{") {
		t.Fatalf("template url = %q", entry.Template.URL)
	}
	if entry.Template.Method != "POST" {
		t.Fatalf("method = %q", entry.Template.Method)
	}
	for _, h := range entry.Template.Headers {
		if strings.EqualFold(h.Key, "Authorization") {
			if h.Value != "Bearer {{token}}" || h.Masked {
				t.Fatalf("authorization template = %+v", h)
			}
		}
		if strings.EqualFold(h.Key, "X-Api-Key") {
			if h.Value != history.Mask || !h.Masked {
				t.Fatalf("api key should be masked, got %+v", h)
			}
		}
	}
	if strings.Contains(entry.Template.BodyRaw, secret) {
		t.Fatal("secret in template body")
	}
}

func TestHistoryTemplateBearerLiteralMasked(t *testing.T) {
	srv := newServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	f := newFixture(t, srv)
	req := model.NewRequest("Auth", "GET", "{{baseUrl}}/x")
	req.Headers = []model.KV{{Key: "Authorization", Value: "Bearer literal-token-value"}}
	f.col.Items = append(f.col.Items, req)
	f.save()
	out := f.send(f.findRequest("Auth"), nil)
	entry, err := f.eng.cfg.History.(*history.Log).Get(out.HistoryID)
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range entry.Template.Headers {
		if strings.EqualFold(h.Key, "Authorization") && (!h.Masked || h.Value != history.Mask) {
			t.Fatalf("got %+v", h)
		}
	}
}
