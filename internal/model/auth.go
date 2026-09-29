package model

// AuthType selects the authentication scheme.
type AuthType string

// Authentication types.
const (
	// AuthInherit uses the nearest ancestor's auth. A nil *Auth is
	// equivalent.
	AuthInherit AuthType = "inherit"
	// AuthNone sends no authentication and stops inheritance.
	AuthNone AuthType = "none"
	// AuthBearer sends "Authorization: Bearer <token>".
	AuthBearer AuthType = "bearer"
	// AuthBasic sends HTTP Basic credentials.
	AuthBasic AuthType = "basic"
	// AuthAPIKey sends a key in a header or query parameter.
	AuthAPIKey AuthType = "apikey"
	// AuthDigest performs HTTP Digest authentication.
	AuthDigest AuthType = "digest"
	// AuthOAuth2 obtains and sends an OAuth 2.0 access token.
	AuthOAuth2 AuthType = "oauth2"
	// AuthAWSV4 signs the request with AWS Signature Version 4.
	AuthAWSV4 AuthType = "awsv4"
	// AuthHawk signs the request with Hawk.
	AuthHawk AuthType = "hawk"
	// AuthNTLM performs NTLM authentication.
	AuthNTLM AuthType = "ntlm"
	// AuthUnsupported marks a scheme postctl cannot run (for example OAuth
	// 1.0, Akamai EdgeGrid, ASAP, JWT). The original type name is in
	// Auth.OriginalType and its parameters in Auth.Raw so nothing is lost.
	AuthUnsupported AuthType = "unsupported"
)

// Auth configures authentication at collection, folder or request level. Set
// exactly the typed field matching Type; the others stay nil.
//
// Credentials are stored as written, usually "{{variable}}" references. A
// literal secret typed into a field is written to the YAML file verbatim, so
// prefer references to secret variables.
type Auth struct {
	// Type selects the scheme and which typed field is used.
	Type AuthType `yaml:"type"`
	// Bearer holds the token for [AuthBearer].
	Bearer *BearerAuth `yaml:"bearer,omitempty"`
	// Basic holds the credentials for [AuthBasic].
	Basic *BasicAuth `yaml:"basic,omitempty"`
	// APIKey holds the key for [AuthAPIKey].
	APIKey *APIKeyAuth `yaml:"apikey,omitempty"`
	// Digest holds the parameters for [AuthDigest].
	Digest *DigestAuth `yaml:"digest,omitempty"`
	// OAuth2 holds the configuration for [AuthOAuth2].
	OAuth2 *OAuth2Config `yaml:"oauth2,omitempty"`
	// AWSV4 holds the parameters for [AuthAWSV4].
	AWSV4 *AWSV4Auth `yaml:"awsv4,omitempty"`
	// Hawk holds the parameters for [AuthHawk].
	Hawk *HawkAuth `yaml:"hawk,omitempty"`
	// NTLM holds the parameters for [AuthNTLM].
	NTLM *NTLMAuth `yaml:"ntlm,omitempty"`
	// OriginalType is the source scheme name for [AuthUnsupported]
	// (for example "oauth1").
	OriginalType string `yaml:"originalType,omitempty"`
	// Raw preserves parameters the typed fields do not model: for
	// [AuthUnsupported] the full parameter set, otherwise any extra keys.
	Raw map[string]any `yaml:"raw,omitempty"`
}

// IsInherit reports whether the auth defers to its parent. It is safe to call
// on a nil receiver.
func (a *Auth) IsInherit() bool { return a == nil || a.Type == AuthInherit || a.Type == "" }

// BearerAuth is a bearer token.
type BearerAuth struct {
	// Token is the token value or a {{variable}} reference.
	Token string `yaml:"token,omitempty"`
}

// BasicAuth is an HTTP Basic username and password.
type BasicAuth struct {
	// Username is the user name.
	Username string `yaml:"username,omitempty"`
	// Password is the password or a {{variable}} reference.
	Password string `yaml:"password,omitempty"`
}

// APIKeyLocation is where an API key is sent.
type APIKeyLocation string

// API key locations.
const (
	// APIKeyHeader sends the key as a request header.
	APIKeyHeader APIKeyLocation = "header"
	// APIKeyQuery sends the key as a query parameter.
	APIKeyQuery APIKeyLocation = "query"
)

// APIKeyAuth is a static API key.
type APIKeyAuth struct {
	// Key is the header or query parameter name.
	Key string `yaml:"key,omitempty"`
	// Value is the key value or a {{variable}} reference.
	Value string `yaml:"value,omitempty"`
	// In is [APIKeyHeader] (default) or [APIKeyQuery].
	In APIKeyLocation `yaml:"in,omitempty"`
}

// DigestAuth is HTTP Digest authentication. Empty optional fields are
// negotiated from the server challenge.
type DigestAuth struct {
	// Username is the user name.
	Username string `yaml:"username,omitempty"`
	// Password is the password or a {{variable}} reference.
	Password string `yaml:"password,omitempty"`
	// Realm overrides the challenge realm.
	Realm string `yaml:"realm,omitempty"`
	// Nonce overrides the challenge nonce.
	Nonce string `yaml:"nonce,omitempty"`
	// Algorithm is MD5, MD5-sess, SHA-256, ...
	Algorithm string `yaml:"algorithm,omitempty"`
	// QOP is the quality of protection (auth, auth-int).
	QOP string `yaml:"qop,omitempty"`
	// NonceCount is the nc value.
	NonceCount string `yaml:"nonceCount,omitempty"`
	// ClientNonce is the cnonce value.
	ClientNonce string `yaml:"clientNonce,omitempty"`
	// Opaque overrides the challenge opaque value.
	Opaque string `yaml:"opaque,omitempty"`
}

// AWSV4Auth is AWS Signature Version 4.
type AWSV4Auth struct {
	// AccessKey is the AWS access key ID.
	AccessKey string `yaml:"accessKey,omitempty"`
	// SecretKey is the AWS secret access key or a {{variable}} reference.
	SecretKey string `yaml:"secretKey,omitempty"`
	// Region is the AWS region (us-east-1).
	Region string `yaml:"region,omitempty"`
	// Service is the AWS service name (execute-api).
	Service string `yaml:"service,omitempty"`
	// SessionToken is an optional STS session token.
	SessionToken string `yaml:"sessionToken,omitempty"`
}

// HawkAuth is Hawk request signing.
type HawkAuth struct {
	// ID is the Hawk auth ID.
	ID string `yaml:"id,omitempty"`
	// Key is the Hawk key or a {{variable}} reference.
	Key string `yaml:"key,omitempty"`
	// Algorithm is sha256 or sha1.
	Algorithm string `yaml:"algorithm,omitempty"`
	// User is the optional user.
	User string `yaml:"user,omitempty"`
	// Nonce overrides the generated nonce.
	Nonce string `yaml:"nonce,omitempty"`
	// ExtraData is the optional ext value.
	ExtraData string `yaml:"extraData,omitempty"`
	// AppID is the optional app value.
	AppID string `yaml:"appId,omitempty"`
	// Delegation is the optional dlg value.
	Delegation string `yaml:"delegation,omitempty"`
	// Timestamp overrides the generated timestamp.
	Timestamp string `yaml:"timestamp,omitempty"`
	// IncludePayloadHash includes a payload hash in the MAC.
	IncludePayloadHash bool `yaml:"includePayloadHash,omitempty"`
}

// NTLMAuth is NTLM authentication.
type NTLMAuth struct {
	// Username is the user name.
	Username string `yaml:"username,omitempty"`
	// Password is the password or a {{variable}} reference.
	Password string `yaml:"password,omitempty"`
	// Domain is the Windows domain.
	Domain string `yaml:"domain,omitempty"`
	// Workstation is the workstation name.
	Workstation string `yaml:"workstation,omitempty"`
}
