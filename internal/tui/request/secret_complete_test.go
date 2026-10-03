package request

import (
	"testing"

	"github.com/shubham/postctl/internal/model"
)

// A secret field (an auth token or password) never offers {{variable}}
// completion: the menu is another place a secret could surface, and the
// WebSocket pane already behaves this way.
func TestSecretFieldsOfferNoVariableCompletion(t *testing.T) {
	cases := []struct {
		name string
		auth *model.Auth
		keys []string // from the Auth tab, to the secret field
	}{
		{"bearer token", &model.Auth{Type: model.AuthBearer, Bearer: &model.BearerAuth{Token: "literal"}}, []string{"down", "down", "enter"}},
		{"basic password", &model.Auth{Type: model.AuthBasic, Basic: &model.BasicAuth{Username: "u", Password: "literal"}}, []string{"down", "down", "down", "enter"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := newHarness(t)
			h.req.Auth = c.auth
			h.gotoTab(tabAuth)
			h.key(c.keys...)
			f := h.p.activeField()
			if f == nil || !f.Typing() && f.ModeName() == "" {
				t.Fatal("the secret field did not take the keyboard")
			}
			h.key("A")
			h.typ("{{to")
			if f.Completing() {
				t.Fatal("a secret field opened the variable completion menu")
			}
		})
	}
}
