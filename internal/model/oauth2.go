package model

import "time"

// OAuth2Grant is the OAuth 2.0 grant type. Refresh is manual in v1: the user
// asks for a new token.
type OAuth2Grant string

// OAuth 2.0 grant types supported in v1 (plan §0, R2).
const (
	// GrantClientCredentials is the client credentials grant.
	GrantClientCredentials OAuth2Grant = "client_credentials"
	// GrantAuthorizationCode is the authorization code grant without PKCE.
	GrantAuthorizationCode OAuth2Grant = "authorization_code"
	// GrantAuthorizationCodePKCE is the authorization code grant with PKCE,
	// using a browser and a 127.0.0.1 loopback callback.
	GrantAuthorizationCodePKCE OAuth2Grant = "authorization_code_pkce"
	// GrantPassword is the resource owner password credentials grant.
	GrantPassword OAuth2Grant = "password"
	// GrantImplicit is the implicit grant.
	GrantImplicit OAuth2Grant = "implicit"
)

// OAuth2ClientAuth is how client credentials are sent to the token endpoint.
type OAuth2ClientAuth string

// Client authentication methods.
const (
	// ClientAuthHeader sends HTTP Basic credentials (default).
	ClientAuthHeader OAuth2ClientAuth = "header"
	// ClientAuthBody sends client_id/client_secret in the form body.
	ClientAuthBody OAuth2ClientAuth = "body"
)

// OAuth2TokenPlacement is where the access token is added to requests.
type OAuth2TokenPlacement string

// Token placements.
const (
	// TokenInHeader adds "Authorization: <prefix> <token>" (default).
	TokenInHeader OAuth2TokenPlacement = "header"
	// TokenInQuery adds access_token=<token> to the query string.
	TokenInQuery OAuth2TokenPlacement = "query"
)

// OAuth2Config configures an OAuth 2.0 grant and holds the current token.
// Fields that do not apply to the selected grant are ignored.
type OAuth2Config struct {
	// GrantType selects the flow.
	GrantType OAuth2Grant `yaml:"grantType"`
	// TokenName labels the token in the UI.
	TokenName string `yaml:"tokenName,omitempty"`
	// AuthURL is the authorization endpoint (code and implicit grants).
	AuthURL string `yaml:"authUrl,omitempty"`
	// AccessTokenURL is the token endpoint (all grants except implicit).
	AccessTokenURL string `yaml:"accessTokenUrl,omitempty"`
	// RefreshTokenURL is the endpoint used for a manual refresh; empty
	// means AccessTokenURL.
	RefreshTokenURL string `yaml:"refreshTokenUrl,omitempty"`
	// ClientID is the client identifier.
	ClientID string `yaml:"clientId,omitempty"`
	// ClientSecret is the client secret or a {{variable}} reference.
	ClientSecret string `yaml:"clientSecret,omitempty"`
	// Scope is the space-separated scope list.
	Scope string `yaml:"scope,omitempty"`
	// RedirectURI is the callback registered with the provider. For PKCE
	// the engine uses a 127.0.0.1 loopback URL when empty.
	RedirectURI string `yaml:"redirectUri,omitempty"`
	// State is an optional fixed state value; generated when empty.
	State string `yaml:"state,omitempty"`
	// Audience is the optional audience parameter.
	Audience string `yaml:"audience,omitempty"`
	// Resource is the optional resource parameter.
	Resource string `yaml:"resource,omitempty"`
	// Username is the resource owner user name ([GrantPassword]).
	Username string `yaml:"username,omitempty"`
	// Password is the resource owner password ([GrantPassword]).
	Password string `yaml:"password,omitempty"`
	// ClientAuth selects how the client authenticates to the token
	// endpoint; empty means [ClientAuthHeader].
	ClientAuth OAuth2ClientAuth `yaml:"clientAuth,omitempty"`
	// CodeChallengeMethod is "S256" (default) or "plain" for
	// [GrantAuthorizationCodePKCE].
	CodeChallengeMethod string `yaml:"codeChallengeMethod,omitempty"`
	// CodeVerifier is an optional fixed PKCE verifier; generated when empty.
	CodeVerifier string `yaml:"codeVerifier,omitempty"`
	// AddTokenTo selects placement; empty means [TokenInHeader].
	AddTokenTo OAuth2TokenPlacement `yaml:"addTokenTo,omitempty"`
	// HeaderPrefix is the Authorization scheme; empty means "Bearer".
	HeaderPrefix string `yaml:"headerPrefix,omitempty"`
	// Token is the current token, if one has been obtained or imported.
	Token *OAuth2Token `yaml:"token,omitempty"`
	// Extra preserves unknown OAuth 2.0 parameters.
	Extra map[string]any `yaml:"extra,omitempty"`
}

// OAuth2Token is an OAuth 2.0 token set. The secret parts are kept in the
// SecretStore and never written to YAML; only metadata and Ref are.
type OAuth2Token struct {
	// AccessToken is the access token. In memory only.
	AccessToken string `yaml:"-"`
	// RefreshToken is the refresh token, if any. In memory only.
	RefreshToken string `yaml:"-"`
	// IDToken is the OpenID Connect ID token, if any. In memory only.
	IDToken string `yaml:"-"`
	// TokenType is the token_type response value (usually "Bearer").
	TokenType string `yaml:"tokenType,omitempty"`
	// Scope is the granted scope.
	Scope string `yaml:"scope,omitempty"`
	// ExpiresAt is when the access token expires; zero means unknown.
	ExpiresAt time.Time `yaml:"expiresAt,omitempty"`
	// Ref locates the token secrets in the SecretStore. The store assigns
	// it on save when empty.
	Ref SecretRef `yaml:"secretRef,omitempty"`
}

// Expired reports whether the access token is known to be expired at t.
func (t *OAuth2Token) Expired(at time.Time) bool {
	return t != nil && !t.ExpiresAt.IsZero() && !at.Before(t.ExpiresAt)
}
