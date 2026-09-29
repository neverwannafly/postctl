package oauth2

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/shubham/postctl/internal/model"
)

const maxTokenBody = 1 << 20

var defaultClient = &http.Client{
	Timeout: 30 * time.Second,
	CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	},
}

func httpClient(opts FetchOptions) *http.Client {
	if opts.HTTPClient != nil {
		return opts.HTTPClient
	}
	return defaultClient
}

func clock(opts FetchOptions) time.Time {
	if opts.Now != nil {
		return opts.Now()
	}
	return time.Now()
}

type wireToken struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	IDToken      string `json:"id_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    any    `json:"expires_in"`
	Scope        string `json:"scope"`
	Error        string `json:"error"`
	ErrorDesc    string `json:"error_description"`
}

func postToken(ctx context.Context, endpoint string, form url.Values, id, secret string, bodyAuth bool, opts FetchOptions) (*model.OAuth2Token, error) {
	header := applyClient(form, id, secret, bodyAuth)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, errors.New("oauth2: invalid token URL")
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	if header != "" {
		req.Header.Set("Authorization", header)
	}
	resp, err := httpClient(opts).Do(req)
	if err != nil {
		return nil, transportErr(err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxTokenBody))
	if err != nil {
		return nil, errors.New("oauth2: reading the token response failed")
	}
	tok, err := parseToken(resp.StatusCode, resp.Header.Get("Content-Type"), body, clock(opts))
	if err != nil {
		return nil, err
	}
	return tok, nil
}

func transportErr(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) && ue.Err != nil {
		return errors.Join(errors.New("oauth2: token request failed"), ue.Err)
	}
	return errors.Join(errors.New("oauth2: token request failed"), err)
}

func parseToken(status int, contentType string, body []byte, now time.Time) (*model.OAuth2Token, error) {
	body = bytes.TrimSpace(body)
	wire, parsed := decodeWire(contentType, body)
	if wire.Error != "" {
		return nil, &Error{Code: wire.Error, Description: wire.ErrorDesc, Status: status}
	}
	if !parsed || status < http.StatusOK || status >= 300 {
		return nil, &Error{Status: status}
	}
	if wire.AccessToken == "" {
		return nil, &Error{Status: status, Description: "token response did not include an access token"}
	}
	if len(wire.AccessToken) > 16<<10 || len(wire.RefreshToken) > 16<<10 || len(wire.IDToken) > 24<<10 {
		return nil, &Error{Status: status, Description: "token response was rejected"}
	}
	return &model.OAuth2Token{
		AccessToken:  wire.AccessToken,
		RefreshToken: wire.RefreshToken,
		IDToken:      wire.IDToken,
		TokenType:    wire.TokenType,
		Scope:        wire.Scope,
		ExpiresAt:    expiry(now, wire.ExpiresIn),
	}, nil
}

func decodeWire(contentType string, body []byte) (wireToken, bool) {
	if len(body) == 0 {
		return wireToken{}, false
	}
	ct := strings.ToLower(contentType)
	if strings.Contains(ct, "json") || (body[0] == '{' && !strings.Contains(ct, "x-www-form-urlencoded")) {
		var wire wireToken
		if err := json.Unmarshal(body, &wire); err != nil {
			return wireToken{}, false
		}
		return wire, true
	}
	if strings.Contains(ct, "x-www-form-urlencoded") || bytes.Contains(body, []byte("=")) && body[0] != '<' {
		v, err := url.ParseQuery(string(body))
		if err != nil {
			return wireToken{}, false
		}
		return wireToken{
			AccessToken:  v.Get("access_token"),
			RefreshToken: v.Get("refresh_token"),
			IDToken:      v.Get("id_token"),
			TokenType:    v.Get("token_type"),
			ExpiresIn:    v.Get("expires_in"),
			Scope:        v.Get("scope"),
			Error:        v.Get("error"),
			ErrorDesc:    v.Get("error_description"),
		}, true
	}
	return wireToken{}, false
}

func expiry(now time.Time, raw any) time.Time {
	if raw == nil {
		return time.Time{}
	}
	if s, ok := raw.(string); ok && s == "" {
		return time.Time{}
	}
	sec, ok := asSeconds(raw)
	if !ok {
		return time.Time{}
	}
	if sec <= 0 {
		return now
	}
	d := time.Duration(sec) * time.Second
	const maxExpires = 10 * 365 * 24 * time.Hour
	if d > maxExpires {
		d = maxExpires
	}
	return now.Add(d)
}

func asSeconds(v any) (int64, bool) {
	switch n := v.(type) {
	case float64:
		if math.IsNaN(n) || math.IsInf(n, 0) {
			return 0, false
		}
		return int64(n), true
	case int:
		return int64(n), true
	case int64:
		return n, true
	case json.Number:
		i, err := n.Int64()
		if err == nil {
			return i, true
		}
		f, ferr := n.Float64()
		if ferr != nil || math.IsNaN(f) || math.IsInf(f, 0) {
			return 0, false
		}
		return int64(f), true
	case string:
		if n == "" {
			return 0, false
		}
		i, err := strconv.ParseInt(n, 10, 64)
		if err == nil {
			return i, true
		}
		f, ferr := strconv.ParseFloat(n, 64)
		if ferr != nil || math.IsNaN(f) || math.IsInf(f, 0) {
			return 0, false
		}
		return int64(f), true
	default:
		return 0, false
	}
}

func decorate(cfg *model.OAuth2Config, tok *model.OAuth2Token, refresh bool) *model.OAuth2Token {
	if tok.TokenType == "" {
		tok.TokenType = "Bearer"
	}
	if tok.Scope == "" {
		tok.Scope = cfg.Scope
	}
	if cfg.Token == nil {
		return tok
	}
	tok.Ref = cfg.Token.Ref
	if refresh && tok.RefreshToken == "" {
		tok.RefreshToken = cfg.Token.RefreshToken
	}
	return tok
}
