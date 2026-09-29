package store

import (
	"time"

	"github.com/shubham/postctl/internal/model"
)

func ptr[T any](v T) *T { return &v }

const (
	secretPassword = "hunter2-SENTINEL"
	secretToken    = "tok-SENTINEL-access"
	secretRefresh  = "tok-SENTINEL-refresh"
	secretEnvKey   = "env-SENTINEL-key"
)

// richWorkspace builds a model that exercises every part of the schema: nested
// folders, every body mode, every auth type, disabled rows, secret variables,
// scripts, examples, settings, and explicit ordering. It uses only values that
// survive a YAML round trip unchanged.
func richWorkspace() *model.Workspace {
	tok := &model.OAuth2Token{
		AccessToken: secretToken, RefreshToken: secretRefresh,
		TokenType: "Bearer", Scope: "read write",
		ExpiresAt: time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC),
		Ref:       SecretRefFor(ScopeOAuth2, "req-oauth", ""),
	}

	get := &model.Request{
		ID: "req-list", Name: "List charges", Kind: model.KindHTTP, Method: "GET",
		Description: "Lists charges.\n\nSupports paging.",
		URL: model.URL{
			Raw: "{{baseUrl}}/v1/charges/:id?limit=10&expand=customer",
			Query: []model.KV{
				{Key: "limit", Value: "10", Description: "page size"},
				{Key: "expand", Value: "customer"},
				{Key: "debug", Value: "1", Disabled: true, Description: "off by default"},
			},
			PathVars: []model.Variable{{Key: "id", Value: "ch_1", Description: "charge id"}},
		},
		Headers: []model.KV{
			{Key: "Accept", Value: "application/json"},
			{Key: "X-Trace", Value: "abc", Disabled: true, Description: "tracing"},
		},
		Auth: &model.Auth{Type: model.AuthInherit},
		Scripts: model.Scripts{
			PreRequest: &model.Script{Source: "pm.variables.set('t', Date.now());\nconsole.log('pre');"},
			Test: &model.Script{
				Source: "pm.test('ok', () => {\n  pm.response.to.have.status(200);\n});\n",
				ID:     "script-1", Name: "status check",
			},
		},
		Settings: model.RequestSettings{
			FollowRedirects: ptr(false), MaxRedirects: ptr(3), VerifyTLS: ptr(true),
			Timeout: ptr(5 * time.Second), HTTPVersion: "http2",
			Extra: map[string]any{"tlsCipherSelection": "default"},
		},
		Examples: []model.Example{{
			ID: "ex-1", Name: "200 OK",
			OriginalRequest: &model.Request{
				Name: "List charges", Kind: model.KindHTTP, Method: "GET",
				URL: model.URL{Raw: "{{baseUrl}}/v1/charges"},
			},
			Status: "OK", Code: 200,
			Headers: []model.KV{{Key: "Content-Type", Value: "application/json"}},
			Cookies: []model.Cookie{{Name: "sid", Value: "1", Domain: "x.test", Path: "/", HTTPOnly: true, Secure: true}},
			Body:    "{\n  \"data\": []\n}", PreviewLanguage: "json",
			Extra: map[string]any{"_postman_previewlanguage": "json"},
		}},
	}

	rawJSON := &model.Request{
		ID: "req-create", Name: "Create charge", Kind: model.KindHTTP, Method: "POST",
		URL:     model.URL{Raw: "{{baseUrl}}/v1/charges"},
		Headers: []model.KV{{Key: "Content-Type", Value: "application/json"}},
		Body: model.Body{
			Mode: model.BodyRaw, Language: "json",
			Raw: "{\n  \"amount\": {{amount}},\n  \"currency\": \"usd\"\n}",
			// Content of inactive modes is retained.
			URLEncoded: []model.KV{{Key: "kept", Value: "yes"}},
		},
		Auth: &model.Auth{Type: model.AuthBearer, Bearer: &model.BearerAuth{Token: "{{token}}"}},
	}

	urlenc := &model.Request{
		ID: "req-urlenc", Name: "Form login", Kind: model.KindHTTP, Method: "POST",
		URL: model.URL{Raw: "{{baseUrl}}/login"},
		Body: model.Body{Mode: model.BodyURLEncoded, URLEncoded: []model.KV{
			{Key: "user", Value: "alex"},
			{Key: "otp", Value: "000000", Disabled: true, Description: "optional"},
		}},
		Auth: &model.Auth{Type: model.AuthBasic, Basic: &model.BasicAuth{Username: "{{user}}", Password: "{{pass}}"}},
	}

	formdata := &model.Request{
		ID: "req-form", Name: "Upload", Kind: model.KindHTTP, Method: "POST",
		URL: model.URL{Raw: "{{baseUrl}}/upload"},
		Body: model.Body{Mode: model.BodyFormData, FormData: []model.FormPart{
			{Key: "title", Type: model.FormText, Value: "hello", Description: "text part"},
			{Key: "file", Type: model.FormFile, Src: "fixtures/a.png", ContentType: "image/png"},
			{Key: "skip", Type: model.FormText, Value: "x", Disabled: true},
		}},
		Auth: &model.Auth{Type: model.AuthAPIKey, APIKey: &model.APIKeyAuth{Key: "X-Key", Value: "{{key}}", In: model.APIKeyHeader}},
	}

	binary := &model.Request{
		ID: "req-bin", Name: "Send binary", Kind: model.KindHTTP, Method: "PUT",
		URL:  model.URL{Raw: "{{baseUrl}}/blob"},
		Body: model.Body{Mode: model.BodyBinary, File: "fixtures/blob.bin"},
		Auth: &model.Auth{Type: model.AuthDigest, Digest: &model.DigestAuth{
			Username: "u", Password: "{{p}}", Realm: "r", Algorithm: "SHA-256", QOP: "auth", NonceCount: "00000001",
		}},
	}

	graphql := &model.Request{
		ID: "req-gql", Name: "GraphQL query", Kind: model.KindHTTP, Method: "POST",
		URL: model.URL{Raw: "{{baseUrl}}/graphql"},
		Body: model.Body{Mode: model.BodyGraphQL, GraphQL: &model.GraphQLBody{
			Query:     "query($id: ID!) {\n  charge(id: $id) { id }\n}",
			Variables: "{\"id\": \"ch_1\"}",
		}},
		Auth: &model.Auth{Type: model.AuthAWSV4, AWSV4: &model.AWSV4Auth{
			AccessKey: "{{ak}}", SecretKey: "{{sk}}", Region: "us-east-1", Service: "execute-api", SessionToken: "{{st}}",
		}},
	}

	noBody := &model.Request{
		ID: "req-nobody", Name: "Ping", Kind: model.KindHTTP, Method: "HEAD",
		URL:  model.URL{Raw: "{{baseUrl}}/ping"},
		Body: model.Body{Mode: model.BodyNone},
		Auth: &model.Auth{Type: model.AuthNone},
	}

	hawk := &model.Request{
		ID: "req-hawk", Name: "Hawk signed", Kind: model.KindHTTP, Method: "GET",
		URL: model.URL{Raw: "{{baseUrl}}/hawk"},
		Auth: &model.Auth{Type: model.AuthHawk, Hawk: &model.HawkAuth{
			ID: "{{hid}}", Key: "{{hkey}}", Algorithm: "sha256", IncludePayloadHash: true, ExtraData: "ext",
		}},
	}

	ntlm := &model.Request{
		ID: "req-ntlm", Name: "NTLM", Kind: model.KindHTTP, Method: "GET",
		URL: model.URL{Raw: "{{baseUrl}}/ntlm"},
		Auth: &model.Auth{Type: model.AuthNTLM, NTLM: &model.NTLMAuth{
			Username: "u", Password: "{{p}}", Domain: "corp", Workstation: "ws1",
		}},
	}

	unsupported := &model.Request{
		ID: "req-oauth1", Name: "OAuth1 legacy", Kind: model.KindHTTP, Method: "GET",
		URL: model.URL{Raw: "{{baseUrl}}/oauth1"},
		Auth: &model.Auth{
			Type: model.AuthUnsupported, OriginalType: "oauth1",
			Raw: map[string]any{"consumerKey": "ck", "signatureMethod": "HMAC-SHA1", "addParamsToHeader": true, "timestamp": 12},
		},
	}

	oauth := &model.Request{
		ID: "req-oauth", Name: "OAuth2 PKCE", Kind: model.KindHTTP, Method: "GET",
		URL: model.URL{Raw: "{{baseUrl}}/me"},
		Auth: &model.Auth{Type: model.AuthOAuth2, OAuth2: &model.OAuth2Config{
			GrantType: model.GrantAuthorizationCodePKCE, TokenName: "main",
			AuthURL: "https://idp.test/authorize", AccessTokenURL: "https://idp.test/token",
			RefreshTokenURL: "https://idp.test/refresh",
			ClientID:        "cid", ClientSecret: "{{csecret}}", Scope: "read write",
			RedirectURI: "http://127.0.0.1:8123/cb", Audience: "api",
			ClientAuth: model.ClientAuthBody, CodeChallengeMethod: "S256",
			AddTokenTo: model.TokenInHeader, HeaderPrefix: "Bearer",
			Token: tok, Extra: map[string]any{"grant_extra": "x"},
		}},
	}

	oauthCC := &model.Request{
		ID: "req-oauth-cc", Name: "OAuth2 client credentials", Kind: model.KindHTTP, Method: "GET",
		URL: model.URL{Raw: "{{baseUrl}}/svc"},
		Auth: &model.Auth{Type: model.AuthOAuth2, OAuth2: &model.OAuth2Config{
			GrantType: model.GrantClientCredentials, AccessTokenURL: "https://idp.test/token",
			ClientID: "svc", ClientSecret: "{{svcsecret}}", Scope: "svc.read",
		}},
	}
	oauthPw := &model.Request{
		ID: "req-oauth-pw", Name: "OAuth2 password", Kind: model.KindHTTP, Method: "GET",
		URL: model.URL{Raw: "{{baseUrl}}/pw"},
		Auth: &model.Auth{Type: model.AuthOAuth2, OAuth2: &model.OAuth2Config{
			GrantType: model.GrantPassword, AccessTokenURL: "https://idp.test/token",
			ClientID: "c", Username: "{{u}}", Password: "{{p}}",
		}},
	}
	oauthImp := &model.Request{
		ID: "req-oauth-imp", Name: "OAuth2 implicit", Kind: model.KindHTTP, Method: "GET",
		URL: model.URL{Raw: "{{baseUrl}}/imp"},
		Auth: &model.Auth{Type: model.AuthOAuth2, OAuth2: &model.OAuth2Config{
			GrantType: model.GrantImplicit, AuthURL: "https://idp.test/authorize", ClientID: "c", AddTokenTo: model.TokenInQuery,
		}},
	}
	oauthCode := &model.Request{
		ID: "req-oauth-code", Name: "OAuth2 auth code", Kind: model.KindHTTP, Method: "GET",
		URL: model.URL{Raw: "{{baseUrl}}/code"},
		Auth: &model.Auth{Type: model.AuthOAuth2, OAuth2: &model.OAuth2Config{
			GrantType: model.GrantAuthorizationCode, AuthURL: "https://idp.test/authorize",
			AccessTokenURL: "https://idp.test/token", ClientID: "c", ClientSecret: "{{cs}}",
		}},
	}

	socket := &model.Request{
		ID: "req-ws", Name: "Live prices", Kind: model.KindWebSocket,
		Description: "Streams price ticks.",
		URL:         model.URL{Raw: "wss://{{wsHost}}/prices?feed=eur", Query: []model.KV{{Key: "feed", Value: "eur"}}},
		Headers:     []model.KV{{Key: "Origin", Value: "https://app.test"}},
		Auth:        &model.Auth{Type: model.AuthBearer, Bearer: &model.BearerAuth{Token: "{{token}}"}},
		Settings:    model.RequestSettings{VerifyTLS: ptr(false), Timeout: ptr(10 * time.Second)},
		WebSocket: &model.WebSocketSettings{
			Messages: []model.WSMessage{
				{ID: "msg-sub", Name: "Subscribe", Format: model.WSJSON, Body: "{\n  \"op\": \"subscribe\",\n  \"symbol\": \"{{symbol}}\"\n}"},
				{ID: "msg-ping", Name: "Hello", Format: model.WSText, Body: "hello"},
				{ID: "msg-bin", Name: "Binary blob", Format: model.WSBinaryBase64, Body: "AQID"},
			},
			Draft:           &model.WSMessage{Format: model.WSJSON, Body: "{\"op\": \"unsubscribe\"}"},
			Subprotocols:    []string{"v1.prices", "v2.prices"},
			PingInterval:    ptr(30 * time.Second),
			MaxMessageBytes: ptr(int64(1 << 20)),
			Extra:           map[string]any{"note": "kept"},
		},
	}

	formsFolder := &model.Folder{
		ID: "f-forms", Name: "Forms & uploads",
		Description: "Body modes.",
		Items:       []model.Item{urlenc, formdata, binary},
	}
	chargesFolder := &model.Folder{
		ID: "f-charges", Name: "Charges",
		Auth: &model.Auth{Type: model.AuthBasic, Basic: &model.BasicAuth{Username: "svc", Password: "{{svcpass}}"}},
		Variables: []model.Variable{
			{Key: "page", Value: "1"},
			{Key: "folderSecret", Type: model.VarSecret, Value: secretPassword, SecretRef: SecretRefFor(ScopeFolder, "f-charges", "folderSecret")},
		},
		Scripts: model.Scripts{
			PreRequest: &model.Script{Source: "// folder pre\n"},
			Test:       &model.Script{Source: "// folder test"},
		},
		Items: []model.Item{get, rawJSON, formsFolder, graphql},
	}
	authFolder := &model.Folder{
		ID: "f-auth", Name: "Auth schemes",
		Items: []model.Item{hawk, ntlm, unsupported, oauth, oauthCC, oauthPw, oauthImp, oauthCode},
	}
	emptyFolder := &model.Folder{ID: "f-empty", Name: "Empty"}

	pay := &model.Collection{
		ID: "coll-pay", Name: "Payments API", Version: "1.2.0",
		Description: "Payments.\n\nSee docs.",
		Auth:        &model.Auth{Type: model.AuthBearer, Bearer: &model.BearerAuth{Token: "{{token}}"}},
		Variables: []model.Variable{
			{Key: "baseUrl", Value: "https://api.test", Description: "root"},
			{Key: "apiSecret", Type: model.VarSecret, Value: secretPassword, SecretRef: SecretRefFor(ScopeCollection, "coll-pay", "apiSecret")},
			{Key: "off", Value: "x", Disabled: true, DataType: "string"},
		},
		Scripts: model.Scripts{
			PreRequest: &model.Script{Source: "pm.environment.set('a', 1);"},
			Test:       &model.Script{Source: "pm.test('t', () => {});", Disabled: true},
		},
		Extra: map[string]any{"postman": map[string]any{"schema": "v2.1.0", "tags": []any{"a", "b"}}},
		Items: []model.Item{chargesFolder, noBody, authFolder, emptyFolder, socket},
	}
	dup := &model.Collection{
		ID: "coll-dup", Name: "payments api",
		Items: []model.Item{
			&model.Request{ID: "d1", Name: "Same name", Kind: model.KindHTTP, Method: "GET", URL: model.URL{Raw: "https://x.test/1"}},
			&model.Request{ID: "d2", Name: "Same name", Kind: model.KindHTTP, Method: "GET", URL: model.URL{Raw: "https://x.test/2"}},
			&model.Folder{ID: "d3", Name: "Same", Items: []model.Item{&model.Request{ID: "d5", Name: "in", Kind: model.KindHTTP, Method: "GET"}}},
			&model.Folder{ID: "d4", Name: "same!", Items: []model.Item{&model.Request{ID: "d6", Name: "in", Kind: model.KindHTTP, Method: "GET"}}},
		},
	}

	return &model.Workspace{
		ID: "ws-1", Name: "payments-api", Description: "Demo workspace.",
		Defaults:    model.RequestSettings{Timeout: ptr(30 * time.Second), FollowRedirects: ptr(true)},
		Extra:       map[string]any{"owner": "team-x"},
		Collections: []*model.Collection{pay, dup},
		Environments: []*model.Environment{
			{
				ID: "env-dev", Name: "Dev", Description: "Local dev",
				Variables: []model.Variable{
					{Key: "baseUrl", Value: "http://localhost:8080"},
					{Key: "apiKey", Type: model.VarSecret, Value: secretEnvKey, SecretRef: SecretRefFor(ScopeEnvironment, "env-dev", "apiKey")},
					{Key: "unused", Value: "1", Disabled: true},
				},
			},
			{
				ID: "env-prod", Name: "Prod", Production: true,
				Variables: []model.Variable{
					{Key: "baseUrl", Value: "https://api.example.com"},
					{Key: "empty", Type: model.VarSecret, SecretRef: SecretRefFor(ScopeEnvironment, "env-prod", "empty")},
				},
				Extra: map[string]any{"_postman_variable_scope": "environment"},
			},
		},
		Globals: &model.Globals{
			ID: "globals-1", Name: "Globals",
			Variables: []model.Variable{
				{Key: "g1", Value: "v1"},
				{Key: "gsecret", Type: model.VarSecret, Value: secretPassword, SecretRef: SecretRefFor(ScopeGlobals, "globals-1", "gsecret")},
			},
		},
	}
}
