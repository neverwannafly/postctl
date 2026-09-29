package httpx

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shubham/postctl/internal/model"
)

func TestClientBodyModes(t *testing.T) {
	t.Parallel()
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "upload.txt"), []byte("file-data"), 0o600); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name        string
		body        model.Body
		contentType string
		check       func(*testing.T, *http.Request, []byte)
	}{
		{
			name:        "raw",
			body:        model.Body{Mode: model.BodyRaw, Language: "json", Raw: `{"ok":true}`},
			contentType: "application/json",
			check: func(t *testing.T, _ *http.Request, data []byte) {
				t.Helper()
				if string(data) != `{"ok":true}` {
					t.Fatalf("raw body = %q", data)
				}
			},
		},
		{
			name:        "urlencoded",
			body:        model.Body{Mode: model.BodyURLEncoded, URLEncoded: []model.KV{{Key: "a", Value: "one two"}, {Key: "off", Value: "x", Disabled: true}}},
			contentType: "application/x-www-form-urlencoded",
			check: func(t *testing.T, _ *http.Request, data []byte) {
				t.Helper()
				values, err := url.ParseQuery(string(data))
				if err != nil || values.Get("a") != "one two" || values.Has("off") {
					t.Fatalf("urlencoded body was not encoded as expected")
				}
			},
		},
		{
			name: "formdata",
			body: model.Body{Mode: model.BodyFormData, FormData: []model.FormPart{
				{Key: "text", Value: "value"},
				{Key: "file", Type: model.FormFile, Src: "upload.txt"},
			}},
			contentType: "multipart/form-data",
			check: func(t *testing.T, request *http.Request, _ []byte) {
				t.Helper()
				if err := request.ParseMultipartForm(1 << 20); err != nil {
					t.Fatal(err)
				}
				if request.FormValue("text") != "value" {
					t.Fatal("multipart text field is missing")
				}
				file, _, err := request.FormFile("file")
				if err != nil {
					t.Fatal(err)
				}
				defer file.Close()
				data, err := io.ReadAll(file)
				if err != nil || string(data) != "file-data" {
					t.Fatal("multipart file field is missing")
				}
			},
		},
		{
			name:        "binary",
			body:        model.Body{Mode: model.BodyBinary, File: "upload.txt"},
			contentType: "text/plain",
			check: func(t *testing.T, _ *http.Request, data []byte) {
				t.Helper()
				if string(data) != "file-data" {
					t.Fatal("binary body is missing")
				}
			},
		},
		{
			name:        "graphql",
			body:        model.Body{Mode: model.BodyGraphQL, GraphQL: &model.GraphQLBody{Query: "query Q { ok }", Variables: `{"id":7}`}},
			contentType: "application/json",
			check: func(t *testing.T, _ *http.Request, data []byte) {
				t.Helper()
				var value struct {
					Query     string         `json:"query"`
					Variables map[string]any `json:"variables"`
				}
				if err := json.Unmarshal(data, &value); err != nil || value.Query == "" || value.Variables["id"] != float64(7) {
					t.Fatal("GraphQL body is invalid")
				}
			},
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				data, err := io.ReadAll(request.Body)
				if err != nil {
					t.Error(err)
					writer.WriteHeader(http.StatusInternalServerError)
					return
				}
				request.Body = io.NopCloser(bytes.NewReader(data))
				if !strings.HasPrefix(request.Header.Get("Content-Type"), test.contentType) {
					t.Errorf("unexpected content type")
				}
				test.check(t, request, data)
				writer.WriteHeader(http.StatusNoContent)
			}))
			defer server.Close()

			request := model.NewRequest("body", http.MethodPost, server.URL)
			request.Body = test.body
			response, err := NewClient("test").Send(t.Context(), request, Options{WorkspaceDir: workspace})
			if err != nil {
				t.Fatal(err)
			}
			if response.StatusCode != http.StatusNoContent {
				t.Fatalf("status = %d", response.StatusCode)
			}
		})
	}
}

func TestDisabledContentTypeStaysDisabled(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Content-Type") != "" {
			t.Error("disabled Content-Type was added")
		}
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	request := model.NewRequest("disabled", http.MethodPost, server.URL)
	request.Headers = []model.KV{{Key: "Content-Type", Disabled: true}}
	request.Body = model.Body{Mode: model.BodyRaw, Language: "json", Raw: "{}"}
	if _, err := NewClient("test").Send(t.Context(), request, Options{}); err != nil {
		t.Fatal(err)
	}
}

func TestDisabledDefaultHeadersStayDisabled(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		for _, name := range []string{"User-Agent", "Accept", "Accept-Encoding", "Connection"} {
			if request.Header.Get(name) != "" {
				t.Errorf("disabled %s was added", name)
			}
		}
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	request := model.NewRequest("disabled", http.MethodGet, server.URL)
	for _, name := range []string{"User-Agent", "Accept", "Accept-Encoding", "Connection"} {
		request.Headers = append(request.Headers, model.KV{Key: name, Disabled: true})
	}
	if _, err := NewClient("test").Send(t.Context(), request, Options{}); err != nil {
		t.Fatal(err)
	}
}

func TestMultipartWriterProducesReadableBody(t *testing.T) {
	t.Parallel()
	body, err := buildMultipart([]model.FormPart{{Key: "a", Value: "b"}}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	_, params, err := mimeParse(body.contentType)
	if err != nil {
		t.Fatal(err)
	}
	reader := multipart.NewReader(strings.NewReader(string(body.data)), params)
	part, err := reader.NextPart()
	if err != nil || part.FormName() != "a" {
		t.Fatal("multipart body cannot be parsed")
	}
}

func mimeParse(contentType string) (string, string, error) {
	const prefix = "multipart/form-data; boundary="
	if !strings.HasPrefix(contentType, prefix) {
		return "", "", http.ErrNotSupported
	}
	return "multipart/form-data", strings.TrimPrefix(contentType, prefix), nil
}
