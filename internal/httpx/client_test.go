package httpx

import (
	"bytes"
	"compress/gzip"
	"compress/zlib"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/andybalholm/brotli"
	"github.com/shubham/postctl/internal/model"
)

func TestClientCompressionTruncationAndDefaults(t *testing.T) {
	t.Parallel()
	payload := strings.Repeat("decoded-", 64)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("User-Agent") != "postctl/1.2.3" ||
			request.Header.Get("Accept") != "*/*" ||
			request.Header.Get("Accept-Encoding") != "gzip, deflate, br" {
			t.Error("default headers are missing")
		}
		var encoding string
		var encode func(io.Writer) io.WriteCloser
		switch request.URL.Path {
		case "/gzip":
			encoding = "gzip"
			encode = func(output io.Writer) io.WriteCloser { return gzip.NewWriter(output) }
		case "/deflate":
			encoding = "deflate"
			encode = func(output io.Writer) io.WriteCloser { return zlib.NewWriter(output) }
		case "/br":
			encoding = "br"
			encode = func(output io.Writer) io.WriteCloser { return brotli.NewWriter(output) }
		}
		writer.Header().Set("Content-Encoding", encoding)
		compressor := encode(writer)
		_, _ = compressor.Write([]byte(payload))
		_ = compressor.Close()
	}))
	defer server.Close()

	for _, encoding := range []string{"gzip", "deflate", "br"} {
		request := model.NewRequest("compressed", http.MethodGet, server.URL+"/"+encoding)
		response, err := NewClient("1.2.3").Send(t.Context(), request, Options{MaxBodyBytes: 17})
		if err != nil {
			t.Fatal(err)
		}
		if !response.BodyTruncated || string(response.Body) != payload[:17] {
			t.Fatalf("%s response was not decoded and truncated", encoding)
		}
		if response.Size.Body <= 0 || response.Size.Headers <= 0 {
			t.Fatalf("%s wire size was not recorded", encoding)
		}
		if response.Timings.Total <= 0 || response.Sent.URL == "" {
			t.Fatalf("%s timing or sent request is missing", encoding)
		}
	}
}

func TestClientRedirectPolicies(t *testing.T) {
	t.Parallel()
	var crossAuth string
	target := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		crossAuth = request.Header.Get("Authorization")
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/same" {
			http.Redirect(writer, request, "/final", http.StatusFound)
			return
		}
		if request.URL.Path == "/final" {
			if request.Method != http.MethodPost {
				t.Errorf("redirect method = %s", request.Method)
			}
			data, _ := io.ReadAll(request.Body)
			if string(data) != "payload" {
				t.Error("redirect body was not preserved")
			}
			writer.WriteHeader(http.StatusCreated)
			return
		}
		http.Redirect(writer, request, target.URL, http.StatusFound)
	}))
	defer source.Close()

	request := model.NewRequest("same", http.MethodPost, source.URL+"/same")
	request.Body = model.Body{Mode: model.BodyRaw, Raw: "payload"}
	request.Settings.FollowOriginalMethod = boolPtr(true)
	response, err := NewClient("test").Send(t.Context(), request, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusCreated || len(response.Redirects) != 1 {
		t.Fatalf("redirect result = %#v", response.Redirects)
	}

	cross := model.NewRequest("cross", http.MethodGet, source.URL+"/cross")
	cross.Headers = []model.KV{{Key: "Authorization", Value: "synthetic"}}
	if _, err := NewClient("test").Send(t.Context(), cross, Options{}); err != nil {
		t.Fatal(err)
	}
	if crossAuth != "" {
		t.Fatal("authorization crossed hosts by default")
	}

	noFollow := model.NewRequest("no-follow", http.MethodGet, source.URL+"/cross")
	noFollow.Settings.FollowRedirects = boolPtr(false)
	response, err = NewClient("test").Send(t.Context(), noFollow, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusFound || len(response.Redirects) != 0 {
		t.Fatalf("no-follow response = %d, %#v", response.StatusCode, response.Redirects)
	}
}

func TestClientTimeoutReturnsErrorWithoutResponse(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		time.Sleep(100 * time.Millisecond)
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	request := model.NewRequest("timeout", http.MethodGet, server.URL)
	timeout := 10 * time.Millisecond
	request.Settings.Timeout = &timeout
	response, err := NewClient("test").Send(t.Context(), request, Options{})
	if err == nil || response != nil {
		t.Fatal("timeout did not return a transport error")
	}
}

func TestClientHTTP2AndTLSOption(t *testing.T) {
	t.Parallel()
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusNoContent)
	}))
	server.EnableHTTP2 = true
	server.StartTLS()
	defer server.Close()
	request := model.NewRequest("http2", http.MethodGet, server.URL)
	request.Settings.VerifyTLS = boolPtr(false)
	response, err := NewClient("test").Send(t.Context(), request, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if response.Proto != "HTTP/2.0" || response.TLS == nil {
		t.Fatalf("protocol = %q, TLS missing = %v", response.Proto, response.TLS == nil)
	}
}

func TestReadLimited(t *testing.T) {
	t.Parallel()
	data, truncated, err := readLimited(bytes.NewBufferString("12345"), 4)
	if err != nil || !truncated || string(data) != "1234" {
		t.Fatal("readLimited did not enforce cap")
	}
}

func boolPtr(value bool) *bool { return &value }
