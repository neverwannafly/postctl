package httpx

import (
	"strings"
	"testing"

	"github.com/shubham/postctl/internal/model"
)

func TestCurlShellEscapingAndDisabledHeaders(t *testing.T) {
	t.Parallel()
	request := model.NewRequest("curl", "POST", "https://example.test/a?x=it's")
	request.Headers = []model.KV{
		{Key: "X-Name", Value: "O'Brien"},
		{Key: "X-Off", Value: "ignored", Disabled: true},
	}
	request.Body = model.Body{Mode: model.BodyRaw, Raw: "it's a body"}
	command := Curl(request)
	if !strings.Contains(command, `'https://example.test/a?x=it'\''s'`) ||
		!strings.Contains(command, `'X-Name: O'\''Brien'`) ||
		!strings.Contains(command, `'it'\''s a body'`) {
		t.Fatalf("curl command was not shell-safe: %s", command)
	}
	if strings.Contains(command, "X-Off") {
		t.Fatal("disabled header was included")
	}
}

func TestCurlFromSent(t *testing.T) {
	t.Parallel()
	command := CurlFromSent(SentRequest{
		Method:  "PUT",
		URL:     "https://example.test/resource",
		Headers: []model.KV{{Key: "Content-Type", Value: "text/plain"}},
		Body:    []byte("payload"),
	})
	for _, expected := range []string{"--request 'PUT'", "--url 'https://example.test/resource'", "--data-binary 'payload'"} {
		if !strings.Contains(command, expected) {
			t.Fatalf("command %q lacks %q", command, expected)
		}
	}
}
