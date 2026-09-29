package oauth2

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/shubham/postctl/internal/model"
)

const defaultCallbackTimeout = 5 * time.Minute

// ErrTimeout is returned when a browser grant's loopback callback does not
// arrive before [FetchOptions.Timeout] (or five minutes, when that is unset).
var ErrTimeout = errors.New("oauth2: timed out waiting for the browser")

// FetchOptions controls one token fetch. The zero value is safe.
type FetchOptions struct {
	// HTTPClient calls the token endpoint. Nil uses a client that does not
	// follow redirects.
	HTTPClient *http.Client
	// Browser opens the authorization URL. Nil uses [SystemBrowser].
	Browser Browser
	// OnAuthURL is called with the authorization URL before the browser
	// opens, so a TUI can show a copyable link. The URL does not contain
	// the client secret, the PKCE verifier, or a token.
	OnAuthURL func(authURL string)
	// OnNote is called when Fetch chooses a loopback redirect the provider
	// may not have registered. The message names that URI and contains no
	// secrets. [model.OAuth2Token] has nowhere to put the note, so it is
	// delivered here.
	OnNote func(note string)
	// Timeout bounds how long a browser grant waits for the callback.
	// Zero means five minutes. Context cancellation ends the wait sooner.
	Timeout time.Duration
	// Now is the clock used to turn expires_in into ExpiresAt. Nil means
	// time.Now.
	Now func() time.Time
	// Refresh exchanges cfg.Token.RefreshToken instead of running the
	// configured grant. Fetch never sets this on its own.
	Refresh bool
}

// Fetch obtains a token for cfg. cfg values must already be variable-resolved.
// Fetch does not modify cfg.
func Fetch(ctx context.Context, cfg *model.OAuth2Config, opts FetchOptions) (*model.OAuth2Token, error) {
	if err := ctx.Err(); err != nil {
		return nil, canceled(err)
	}
	if cfg == nil {
		return nil, errors.New("oauth2: missing oauth2 config")
	}
	if opts.Refresh {
		return fetchRefresh(ctx, cfg, opts)
	}
	switch cfg.GrantType {
	case model.GrantClientCredentials:
		return fetchClient(ctx, cfg, opts)
	case model.GrantPassword:
		return fetchPassword(ctx, cfg, opts)
	case model.GrantAuthorizationCode:
		return fetchAuthCode(ctx, cfg, opts, false)
	case model.GrantAuthorizationCodePKCE:
		return fetchAuthCode(ctx, cfg, opts, true)
	case model.GrantImplicit:
		return fetchImplicit(ctx, cfg, opts)
	default:
		return nil, &Error{Code: "unsupported_grant", Description: "unsupported grant " + string(cfg.GrantType)}
	}
}

func fetchClient(ctx context.Context, cfg *model.OAuth2Config, opts FetchOptions) (*model.OAuth2Token, error) {
	form := url.Values{}
	form.Set("grant_type", "client_credentials")
	return tokenCall(ctx, cfg, opts, form, false)
}

func fetchPassword(ctx context.Context, cfg *model.OAuth2Config, opts FetchOptions) (*model.OAuth2Token, error) {
	if cfg.Username == "" {
		return nil, errors.New("oauth2: username is required")
	}
	form := url.Values{}
	form.Set("grant_type", "password")
	form.Set("username", cfg.Username)
	form.Set("password", cfg.Password)
	return tokenCall(ctx, cfg, opts, form, false)
}

func fetchRefresh(ctx context.Context, cfg *model.OAuth2Config, opts FetchOptions) (*model.OAuth2Token, error) {
	if cfg.Token == nil || cfg.Token.RefreshToken == "" {
		return nil, errors.New("oauth2: no refresh token is stored")
	}
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", cfg.Token.RefreshToken)
	tok, err := tokenCall(ctx, cfg, opts, form, true)
	if err != nil {
		return nil, err
	}
	return tok, nil
}

func tokenCall(ctx context.Context, cfg *model.OAuth2Config, opts FetchOptions, form url.Values, refresh bool) (*model.OAuth2Token, error) {
	endpoint, err := tokenEndpoint(cfg, refresh)
	if err != nil {
		return nil, err
	}
	bodyAuth, err := clientStyle(cfg.ClientAuth)
	if err != nil {
		return nil, err
	}
	bucket := "tokenRequestParams"
	if refresh {
		bucket = "refreshRequestParams"
	}
	mergeExtra(form, cfg, bucket)
	// Scope is already bound into an authorization code. Sending it again makes
	// some token endpoints reject the exchange.
	if form.Get("grant_type") != "authorization_code" {
		setIf(form, "scope", cfg.Scope)
	}
	setIf(form, "audience", cfg.Audience)
	setIf(form, "resource", cfg.Resource)
	tok, err := postToken(ctx, endpoint, form, cfg.ClientID, cfg.ClientSecret, bodyAuth, opts)
	if err != nil {
		return nil, err
	}
	return decorate(cfg, tok, refresh), nil
}

func tokenEndpoint(cfg *model.OAuth2Config, refresh bool) (string, error) {
	if refresh && cfg.RefreshTokenURL != "" {
		return cfg.RefreshTokenURL, nil
	}
	if cfg.AccessTokenURL == "" {
		return "", errors.New("oauth2: access token URL is required")
	}
	return cfg.AccessTokenURL, nil
}

func mergeExtra(form url.Values, cfg *model.OAuth2Config, bucket string) {
	extra := extraValues(cfg.Extra, bucket)
	// Also accept the short alias key when the Postman name is absent.
	if len(extra) == 0 || bucket != "" {
		switch bucket {
		case "tokenRequestParams":
			for k, vs := range extraValues(cfg.Extra, "tokenParams") {
				if _, ok := extra[k]; !ok {
					extra[k] = vs
				}
			}
		case "refreshRequestParams":
			for k, vs := range extraValues(cfg.Extra, "refreshParams") {
				if _, ok := extra[k]; !ok {
					extra[k] = vs
				}
			}
		}
	}
	for k, vs := range extra {
		if _, reserved := form[k]; reserved {
			continue
		}
		for _, v := range vs {
			form.Add(k, v)
		}
	}
}

func fetchAuthCode(ctx context.Context, cfg *model.OAuth2Config, opts FetchOptions, pkce bool) (*model.OAuth2Token, error) {
	if cfg.AuthURL == "" {
		return nil, errors.New("oauth2: authorization URL is required")
	}
	if cfg.AccessTokenURL == "" {
		return nil, errors.New("oauth2: access token URL is required")
	}
	lb, err := listen(cfg, false, clock(opts))
	if err != nil {
		return nil, err
	}
	defer lb.close()
	var verifier, challenge, method string
	if pkce {
		verifier = cfg.CodeVerifier
		if verifier == "" {
			verifier, err = randomToken(32)
			if err != nil {
				return nil, errors.New("oauth2: could not prepare sign-in")
			}
		}
		challenge, method, err = challengeFor(verifier, cfg.CodeChallengeMethod)
		if err != nil {
			return nil, err
		}
	}
	authURL, err := authorizeURL(cfg, lb, "code", challenge, method)
	if err != nil {
		return nil, err
	}
	res, err := waitCallback(ctx, lb, authURL, opts)
	if err != nil {
		return nil, err
	}
	if res.err != nil {
		return nil, res.err
	}
	if res.code == "" {
		return nil, &Error{Code: "invalid_request", Description: "the sign-in response did not include an authorization code"}
	}
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", res.code)
	form.Set("redirect_uri", lb.redirect)
	if pkce {
		form.Set("code_verifier", verifier)
	}
	return tokenCall(ctx, cfg, opts, form, false)
}

func fetchImplicit(ctx context.Context, cfg *model.OAuth2Config, opts FetchOptions) (*model.OAuth2Token, error) {
	if cfg.AuthURL == "" {
		return nil, errors.New("oauth2: authorization URL is required")
	}
	lb, err := listen(cfg, true, clock(opts))
	if err != nil {
		return nil, err
	}
	defer lb.close()
	authURL, err := authorizeURL(cfg, lb, "token", "", "")
	if err != nil {
		return nil, err
	}
	res, err := waitCallback(ctx, lb, authURL, opts)
	if err != nil {
		return nil, err
	}
	if res.err != nil {
		return nil, res.err
	}
	if res.token == nil || res.token.AccessToken == "" {
		return nil, &Error{Code: "invalid_request", Description: "the sign-in response did not include an access token"}
	}
	return decorate(cfg, res.token, false), nil
}

func authorizeURL(cfg *model.OAuth2Config, lb *loopback, responseType, challenge, method string) (string, error) {
	u, err := url.Parse(cfg.AuthURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", errors.New("oauth2: authorization URL is not a valid URL")
	}
	q := u.Query()
	for k, vs := range extraValues(cfg.Extra, "authRequestParams") {
		if _, exists := q[k]; !exists {
			for _, v := range vs {
				q.Add(k, v)
			}
		}
	}
	for k, vs := range extraValues(cfg.Extra, "authParams") {
		if _, exists := q[k]; !exists {
			for _, v := range vs {
				q.Add(k, v)
			}
		}
	}
	q.Set("response_type", responseType)
	q.Set("client_id", cfg.ClientID)
	q.Set("redirect_uri", lb.redirect)
	q.Set("state", lb.state)
	setQuery(q, "scope", cfg.Scope)
	setQuery(q, "audience", cfg.Audience)
	setQuery(q, "resource", cfg.Resource)
	if challenge != "" {
		q.Set("code_challenge", challenge)
		q.Set("code_challenge_method", method)
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}

func setQuery(q url.Values, key, val string) {
	if val != "" {
		q.Set(key, val)
	}
}

func waitCallback(ctx context.Context, lb *loopback, authURL string, opts FetchOptions) (callbackResult, error) {
	if lb.note != "" && opts.OnNote != nil {
		opts.OnNote(lb.note)
	}
	if opts.OnAuthURL != nil {
		opts.OnAuthURL(authURL)
	}
	br := opts.Browser
	if br == nil {
		br = SystemBrowser{}
	}
	openErr := br.OpenURL(authURL)
	select {
	case res := <-lb.result:
		return res, nil
	default:
	}
	if openErr != nil {
		return callbackResult{}, fmt.Errorf("oauth2: open browser: %w", openErr)
	}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = defaultCallbackTimeout
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case res := <-lb.result:
		return res, nil
	case <-timer.C:
		return callbackResult{}, ErrTimeout
	case <-ctx.Done():
		return callbackResult{}, canceled(ctx.Err())
	}
}

func canceled(err error) error {
	return fmt.Errorf("oauth2: canceled: %w", err)
}
