package request

import (
	"strings"
	"testing"

	"github.com/shubham/postctl/internal/model"
)

// A secret row that is not being edited shows its text only when the whole
// value is one {{reference}}, the same rule the open editor uses. A literal
// that merely contains "{{" stays masked.
func TestClosedSecretRowMasksLiteralsThatContainBraces(t *testing.T) {
	for _, token := range []string{"lit-sentinel-{{x", "{{a}}-tail-sentinel", "head-sentinel-{{a}}"} {
		h := newHarness(t)
		h.req.Auth = &model.Auth{Type: model.AuthBearer, Bearer: &model.BearerAuth{Token: token}}
		h.p.reloadAll()
		h.gotoTab(tabAuth)
		if strings.Contains(h.view(), "sentinel") {
			t.Fatalf("a closed secret row shows a %d-character literal", len(token))
		}
		if !strings.Contains(h.view(), h.theme.Glyphs.Mask) {
			t.Fatal("the secret row shows no mask, so the row was not exercised")
		}
	}
	// A pure reference stays readable.
	h := newHarness(t)
	h.req.Auth = &model.Auth{Type: model.AuthBearer, Bearer: &model.BearerAuth{Token: "{{token}}"}}
	h.p.reloadAll()
	h.gotoTab(tabAuth)
	h.mustHave("{{token}}")
}
