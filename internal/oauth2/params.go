package oauth2

import (
	"crypto/sha256"
	"encoding/base64"
	"net/url"
	"strconv"
	"strings"

	"github.com/shubham/postctl/internal/model"
)

// sensitiveParam is never copied from Extra onto a request. Typed config
// fields are the only source for these values, so a client secret or password
// cannot leak onto the authorization URL via Extra.
var sensitiveParam = map[string]bool{
	"access_token":     true,
	"assertion":        true,
	"client_assertion": true,
	"client_secret":    true,
	"code":             true,
	"code_verifier":    true,
	"id_token":         true,
	"password":         true,
	"refresh_token":    true,
}

func challengeFor(verifier, method string) (challenge, wireMethod string, err error) {
	switch strings.ToUpper(strings.TrimSpace(method)) {
	case "", "S256":
		sum := sha256.Sum256([]byte(verifier))
		return base64.RawURLEncoding.EncodeToString(sum[:]), "S256", nil
	case "PLAIN":
		return verifier, "plain", nil
	default:
		return "", "", &Error{Code: "invalid_request", Description: "code challenge method must be S256 or plain"}
	}
}

func clientStyle(v model.OAuth2ClientAuth) (body bool, err error) {
	switch model.OAuth2ClientAuth(strings.ToLower(strings.TrimSpace(string(v)))) {
	case "", model.ClientAuthHeader, "client_secret_basic":
		return false, nil
	case model.ClientAuthBody, "client_secret_post":
		return true, nil
	default:
		return false, &Error{Code: "invalid_request", Description: "unknown client authentication " + strconv.Quote(string(v))}
	}
}

// applyClient sets client_id on form. A non-empty secret is sent either as
// HTTP Basic (client_secret_basic) or as client_secret in the body
// (client_secret_post). An empty secret is a public client: client_id stays
// in the body and no Authorization header is returned.
func applyClient(form url.Values, id, secret string, body bool) (authHeader string) {
	if id != "" {
		form.Set("client_id", id)
	}
	if secret == "" {
		return ""
	}
	if body {
		form.Set("client_secret", secret)
		return ""
	}
	user := url.QueryEscape(id)
	pass := url.QueryEscape(secret)
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(user+":"+pass))
}

// extraValues reads cfg.Extra. Flat string-like entries are included for
// every request. bucket, when non-empty, also reads that Postman parameter
// list (authRequestParams, tokenRequestParams, or refreshRequestParams).
func extraValues(extra map[string]any, bucket string) url.Values {
	out := url.Values{}
	if len(extra) == 0 {
		return out
	}
	for k, v := range extra {
		if isParamBucket(k) || sensitiveParam[k] {
			continue
		}
		if s, ok := asString(v); ok {
			out.Set(k, s)
		}
	}
	if bucket == "" {
		return out
	}
	addParams(out, extra[bucket])
	return out
}

func isParamBucket(k string) bool {
	switch k {
	case "authRequestParams", "tokenRequestParams", "refreshRequestParams",
		"authParams", "tokenParams", "refreshParams":
		return true
	default:
		return false
	}
}

func addParams(dst url.Values, v any) {
	switch t := v.(type) {
	case []any:
		for _, item := range t {
			addParams(dst, item)
		}
	case map[string]any:
		if _, kv := t["key"]; kv {
			k, okK := asString(t["key"])
			val, okV := asString(t["value"])
			if okK && okV && k != "" && !sensitiveParam[k] && !paramDisabled(t) {
				dst.Set(k, val)
			}
			return
		}
		for k, val := range t {
			if sensitiveParam[k] {
				continue
			}
			if s, ok := asString(val); ok {
				dst.Set(k, s)
			}
		}
	case map[string]string:
		for k, val := range t {
			if !sensitiveParam[k] {
				dst.Set(k, val)
			}
		}
	}
}

func paramDisabled(m map[string]any) bool {
	if b, ok := m["disabled"].(bool); ok && b {
		return true
	}
	if s, ok := asString(m["disabled"]); ok && (s == "true" || s == "1") {
		return true
	}
	if b, ok := m["enabled"].(bool); ok && !b {
		return true
	}
	if s, ok := asString(m["enabled"]); ok && (s == "false" || s == "0") {
		return true
	}
	return false
}

func asString(v any) (string, bool) {
	switch t := v.(type) {
	case string:
		return t, true
	case bool:
		if t {
			return "true", true
		}
		return "false", true
	case int:
		return strconv.Itoa(t), true
	case int64:
		return strconv.FormatInt(t, 10), true
	case float64:
		if t == float64(int64(t)) {
			return strconv.FormatInt(int64(t), 10), true
		}
		return strconv.FormatFloat(t, 'f', -1, 64), true
	default:
		return "", false
	}
}

func setIf(form url.Values, key, val string) {
	if val != "" {
		form.Set(key, val)
	}
}
