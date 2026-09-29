package httpx

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/shubham/postctl/internal/model"
)

func TestClientStaticAuthSchemes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		auth  *model.Auth
		check func(*http.Request) bool
	}{
		{
			name: "bearer",
			auth: &model.Auth{Type: model.AuthBearer, Bearer: &model.BearerAuth{Token: "sample-token"}},
			check: func(request *http.Request) bool {
				return request.Header.Get("Authorization") == "Bearer sample-token"
			},
		},
		{
			name: "basic",
			auth: &model.Auth{Type: model.AuthBasic, Basic: &model.BasicAuth{Username: "user", Password: "pass"}},
			check: func(request *http.Request) bool {
				username, password, ok := request.BasicAuth()
				return ok && username == "user" && password == "pass"
			},
		},
		{
			name: "api-key-header",
			auth: &model.Auth{Type: model.AuthAPIKey, APIKey: &model.APIKeyAuth{Key: "X-API-Key", Value: "sample", In: model.APIKeyHeader}},
			check: func(request *http.Request) bool {
				return request.Header.Get("X-API-Key") == "sample"
			},
		},
		{
			name: "api-key-query",
			auth: &model.Auth{Type: model.AuthAPIKey, APIKey: &model.APIKeyAuth{Key: "key", Value: "sample", In: model.APIKeyQuery}},
			check: func(request *http.Request) bool {
				return request.URL.Query().Get("key") == "sample"
			},
		},
		{
			name: "oauth2-stored",
			auth: &model.Auth{Type: model.AuthOAuth2, OAuth2: &model.OAuth2Config{
				HeaderPrefix: "Token",
				Token:        &model.OAuth2Token{AccessToken: "sample"},
			}},
			check: func(request *http.Request) bool {
				return request.Header.Get("Authorization") == "Token sample"
			},
		},
		{
			name: "hawk",
			auth: &model.Auth{Type: model.AuthHawk, Hawk: &model.HawkAuth{
				ID:        "id",
				Key:       "key",
				Algorithm: "sha256",
				Timestamp: "1700000000",
				Nonce:     "nonce",
			}},
			check: func(request *http.Request) bool {
				return strings.HasPrefix(request.Header.Get("Authorization"), `Hawk id="id"`)
			},
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				if !test.check(request) {
					t.Error("authentication was not applied")
					writer.WriteHeader(http.StatusUnauthorized)
					return
				}
				writer.WriteHeader(http.StatusNoContent)
			}))
			defer server.Close()
			request := model.NewRequest("auth", http.MethodGet, server.URL)
			request.Auth = test.auth
			response, err := NewClient("test").Send(t.Context(), request, Options{})
			if err != nil {
				t.Fatal(err)
			}
			if response.StatusCode != http.StatusNoContent {
				t.Fatalf("status = %d", response.StatusCode)
			}
		})
	}
}

func TestOAuth2TokenSourceAndQueryPlacement(t *testing.T) {
	t.Parallel()
	source := &fakeTokenSource{token: &model.OAuth2Token{AccessToken: "from-source", TokenType: "Bearer"}}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Query().Get("access_token") != "from-source" {
			t.Error("OAuth2 query token is missing")
		}
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	request := model.NewRequest("oauth", http.MethodGet, server.URL)
	request.Auth = &model.Auth{Type: model.AuthOAuth2, OAuth2: &model.OAuth2Config{
		GrantType:  model.GrantClientCredentials,
		AddTokenTo: model.TokenInQuery,
	}}
	if _, err := NewClient("test").Send(t.Context(), request, Options{OAuth2: source}); err != nil {
		t.Fatal(err)
	}
	if source.calls != 1 {
		t.Fatalf("token source calls = %d", source.calls)
	}
}

func TestDigestRFC7616VectorAndRetry(t *testing.T) {
	t.Parallel()
	const challenge = `Digest realm="testrealm@host.com", qop="auth,auth-int", algorithm=MD5, nonce="dcd98b7102dd2f0e8b11d0f600bfb0c093", opaque="5ccc069c403ebaf9f0171e9517f40e41"`
	const expectedResponse = `response="6629fae49393a05397450978507c4ef1"`
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls++
		authorization := request.Header.Get("Authorization")
		if authorization == "" {
			writer.Header().Set("WWW-Authenticate", challenge)
			writer.WriteHeader(http.StatusUnauthorized)
			return
		}
		if !strings.Contains(authorization, expectedResponse) ||
			!strings.Contains(authorization, `cnonce="0a4f113b"`) {
			t.Error("Digest response did not match the RFC 7616 vector")
			writer.WriteHeader(http.StatusUnauthorized)
			return
		}
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	request := model.NewRequest("digest", http.MethodGet, server.URL+"/dir/index.html")
	request.Auth = &model.Auth{Type: model.AuthDigest, Digest: &model.DigestAuth{
		Username:    "Mufasa",
		Password:    "Circle Of Life",
		NonceCount:  "00000001",
		ClientNonce: "0a4f113b",
	}}
	response, err := NewClient("test").Send(t.Context(), request, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusNoContent || calls != 2 {
		t.Fatalf("Digest retry result: status=%d calls=%d", response.StatusCode, calls)
	}
}

func TestAWSV4OfficialIAMVector(t *testing.T) {
	t.Parallel()
	request, err := http.NewRequest(
		http.MethodGet,
		"https://iam.amazonaws.com/?Action=ListUsers&Version=2010-05-08",
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=utf-8")
	request.Header.Set("X-Amz-Date", "20150830T123600Z")
	err = signAWSV4(request, nil, &model.AWSV4Auth{
		AccessKey: "AKIDEXAMPLE",
		SecretKey: "wJalrXUtnFEMI/K7MDENG+bPxRfiCYEXAMPLEKEY",
		Region:    "us-east-1",
		Service:   "iam",
	})
	if err != nil {
		t.Fatal(err)
	}
	const expected = "AWS4-HMAC-SHA256 " +
		"Credential=AKIDEXAMPLE/20150830/us-east-1/iam/aws4_request, " +
		"SignedHeaders=content-type;host;x-amz-date, " +
		"Signature=5d672d79c15b13162d9279b0855cfba6789a8edb4c82c400e06b5924a6f2b5d7"
	if request.Header.Get("Authorization") != expected {
		t.Fatal("AWS Signature v4 did not match the official IAM ListUsers vector")
	}
}

func TestUnsupportedNTLMIsClear(t *testing.T) {
	t.Parallel()
	request := model.NewRequest("ntlm", http.MethodGet, "https://example.test")
	request.Auth = &model.Auth{Type: model.AuthNTLM, NTLM: &model.NTLMAuth{}}
	response, err := NewClient("test").Send(t.Context(), request, Options{})
	if response != nil || !errors.Is(err, ErrUnsupportedAuth) || !strings.Contains(err.Error(), "NTLM") {
		t.Fatalf("NTLM error = %v", err)
	}
}

type fakeTokenSource struct {
	token *model.OAuth2Token
	err   error
	calls int
}

func (f *fakeTokenSource) Token(
	_ context.Context,
	_ *model.OAuth2Config,
) (*model.OAuth2Token, error) {
	f.calls++
	return f.token, f.err
}
