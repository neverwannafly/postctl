package vars

import (
	"testing"

	"github.com/shubham/postctl/internal/model"
)

func TestResolveRequestCopiesResolvesAndInheritsAuth(t *testing.T) {
	t.Parallel()
	collection := &model.Collection{
		Auth: &model.Auth{Type: model.AuthBearer, Bearer: &model.BearerAuth{Token: "{{token}}"}},
	}
	folder := &model.Folder{Auth: &model.Auth{Type: model.AuthInherit}}
	request := &model.Request{
		Name:   "{{name}}",
		Kind:   model.KindHTTP,
		Method: "POST",
		URL: model.URL{
			Raw:      "https://example.test/users/:id?q={{query}}",
			Query:    []model.KV{{Key: "q", Value: "{{query}}"}},
			PathVars: []model.Variable{{Key: "id", Value: "{{id}}"}},
		},
		Headers: []model.KV{{Key: "X-{{header}}", Value: "{{value}}"}},
		Body: model.Body{
			Mode:       model.BodyFormData,
			Raw:        "{{raw}}",
			URLEncoded: []model.KV{{Key: "{{key}}", Value: "{{value}}"}},
			FormData:   []model.FormPart{{Key: "{{key}}", Value: "{{value}}", Src: "{{file}}"}},
			File:       "{{file}}",
			GraphQL:    &model.GraphQLBody{Query: "{{query}}", Variables: `{"id":"{{id}}"}`},
		},
	}
	store := New(
		[]model.Variable{
			{Key: "name", Value: "Resolved"},
			{Key: "query", Value: "search"},
			{Key: "id", Value: "a/b"},
			{Key: "header", Value: "Trace"},
			{Key: "value", Value: "yes"},
			{Key: "key", Value: "field"},
			{Key: "file", Value: "files/a.txt"},
			{Key: "raw", Value: "payload"},
			{Key: "token", Value: "secret"},
		},
		nil,
		nil,
		nil,
	)

	resolved, err := ResolveRequest(request, []model.Container{collection, folder}, store)
	if err != nil {
		t.Fatal(err)
	}
	if resolved == request || resolved.Name != "Resolved" {
		t.Fatalf("resolved request = %#v", resolved)
	}
	if resolved.URL.Raw != "https://example.test/users/a%2Fb?q=search" {
		t.Fatalf("URL = %q", resolved.URL.Raw)
	}
	if resolved.Headers[0].Key != "X-Trace" || resolved.Body.FormData[0].Src != "files/a.txt" {
		t.Fatalf("nested fields were not resolved: %#v", resolved)
	}
	if resolved.Auth == collection.Auth || resolved.Auth.Type != model.AuthBearer || resolved.Auth.Bearer.Token != "secret" {
		t.Fatalf("auth = %#v", resolved.Auth)
	}

	resolved.Headers[0].Value = "changed"
	resolved.Body.GraphQL.Query = "changed"
	resolved.Auth.Bearer.Token = "changed"
	if request.Headers[0].Value != "{{value}}" || request.Body.GraphQL.Query != "{{query}}" ||
		collection.Auth.Bearer.Token != "{{token}}" {
		t.Fatal("resolution mutated input")
	}
}

func TestResolveRequestExplicitNoAuthAndErrors(t *testing.T) {
	t.Parallel()
	request := &model.Request{
		Kind: model.KindHTTP,
		Auth: &model.Auth{Type: model.AuthNone},
	}
	resolved, err := ResolveRequest(request, nil, New(nil, nil, nil, nil))
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Auth.Type != model.AuthNone {
		t.Fatalf("auth = %v", resolved.Auth.Type)
	}
	if _, err := ResolveRequest(nil, nil, New(nil, nil, nil, nil)); err == nil {
		t.Fatal("nil request did not fail")
	}
	if _, err := ResolveRequest(request, nil, nil); err == nil {
		t.Fatal("nil store did not fail")
	}
}

func TestApplyPreservesSecretMetadataAndInsertion(t *testing.T) {
	t.Parallel()
	workspace := &model.Workspace{Globals: &model.Globals{
		Variables: []model.Variable{{
			Key:       "secret",
			Value:     "old",
			Type:      model.VarSecret,
			SecretRef: "globals/secret",
		}},
	}}
	environment := &model.Environment{Variables: []model.Variable{{Key: "remove", Value: "x"}}}
	collection := &model.Collection{}
	Apply(workspace, environment, collection, map[Scope][]Change{
		Global:     {{Key: "secret", Value: "new"}},
		Collection: {{Key: "object", Value: map[string]any{"a": 1}}},
		Environment: {
			{Key: "remove", Unset: true},
			{Key: "new", Value: true},
		},
	})
	secret := workspace.Globals.Variables[0]
	if secret.Value != "new" || secret.Type != model.VarSecret || secret.SecretRef != "globals/secret" {
		t.Fatalf("secret metadata lost: %#v", secret)
	}
	if len(environment.Variables) != 1 || environment.Variables[0].Key != "new" ||
		environment.Variables[0].Value != "true" {
		t.Fatalf("environment variables = %#v", environment.Variables)
	}
	if collection.Variables[0].Value != `{"a":1}` {
		t.Fatalf("collection variable = %#v", collection.Variables[0])
	}
}
