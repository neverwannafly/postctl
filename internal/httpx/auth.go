package httpx

import (
	"context"
	"crypto/hmac"
	"crypto/md5" //nolint:gosec // HTTP Digest and AWS compatibility require MD5 support.
	"crypto/rand"
	"crypto/sha1" //nolint:gosec // Hawk permits SHA-1 for imported legacy configurations.
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/shubham/postctl/internal/model"
)

// ErrUnsupportedAuth identifies an authentication scheme the HTTP sender
// cannot implement.
var ErrUnsupportedAuth = errors.New("unsupported authentication")

func applyAuth(
	ctx context.Context,
	request *http.Request,
	auth *model.Auth,
	body []byte,
	source TokenSource,
) error {
	if auth == nil {
		return nil
	}
	switch auth.Type {
	case "", model.AuthNone, model.AuthInherit:
		return nil
	case model.AuthBearer:
		if auth.Bearer == nil {
			return errors.New("bearer auth configuration is missing")
		}
		request.Header.Set("Authorization", "Bearer "+auth.Bearer.Token)
	case model.AuthBasic:
		if auth.Basic == nil {
			return errors.New("basic auth configuration is missing")
		}
		request.SetBasicAuth(auth.Basic.Username, auth.Basic.Password)
	case model.AuthAPIKey:
		if auth.APIKey == nil {
			return errors.New("API key auth configuration is missing")
		}
		if auth.APIKey.In == model.APIKeyQuery {
			query := request.URL.Query()
			query.Add(auth.APIKey.Key, auth.APIKey.Value)
			request.URL.RawQuery = query.Encode()
		} else {
			request.Header.Set(auth.APIKey.Key, auth.APIKey.Value)
		}
	case model.AuthDigest:
		// Digest is applied after the first 401 challenge.
		if auth.Digest == nil {
			return errors.New("digest auth configuration is missing")
		}
		return nil
	case model.AuthOAuth2:
		return applyOAuth2(ctx, request, auth.OAuth2, source)
	case model.AuthAWSV4:
		if auth.AWSV4 == nil {
			return errors.New("AWS v4 auth configuration is missing")
		}
		return signAWSV4(request, body, auth.AWSV4)
	case model.AuthHawk:
		if auth.Hawk == nil {
			return errors.New("hawk auth configuration is missing")
		}
		return signHawk(request, body, auth.Hawk)
	case model.AuthNTLM:
		return fmt.Errorf("%w: NTLM is not available in this build", ErrUnsupportedAuth)
	default:
		name := string(auth.Type)
		if auth.OriginalType != "" {
			name = auth.OriginalType
		}
		return fmt.Errorf("%w: %s", ErrUnsupportedAuth, name)
	}
	return nil
}

func applyOAuth2(
	ctx context.Context,
	request *http.Request,
	config *model.OAuth2Config,
	source TokenSource,
) error {
	if config == nil {
		return errors.New("OAuth2 configuration is missing")
	}
	token := config.Token
	if token == nil || token.AccessToken == "" || token.Expired(time.Now()) {
		if source == nil {
			return errors.New("OAuth2 access token is missing or expired")
		}
		var err error
		token, err = source.Token(ctx, config)
		if err != nil {
			return fmt.Errorf("obtain OAuth2 token: %w", err)
		}
		if token == nil || token.AccessToken == "" {
			return errors.New("OAuth2 token source returned an empty token")
		}
		config.Token = token
	}
	if config.AddTokenTo == model.TokenInQuery {
		query := request.URL.Query()
		query.Set("access_token", token.AccessToken)
		request.URL.RawQuery = query.Encode()
		return nil
	}
	prefix := config.HeaderPrefix
	if prefix == "" {
		prefix = token.TokenType
	}
	if prefix == "" {
		prefix = "Bearer"
	}
	request.Header.Set("Authorization", prefix+" "+token.AccessToken)
	return nil
}

func signAWSV4(request *http.Request, body []byte, config *model.AWSV4Auth) error {
	if config.AccessKey == "" || config.SecretKey == "" || config.Region == "" || config.Service == "" {
		return errors.New("AWS v4 requires access key, secret key, region, and service")
	}
	payloadHash := sha256Hex(body)
	if config.Service == "s3" && request.Header.Get("X-Amz-Content-Sha256") == "" {
		request.Header.Set("X-Amz-Content-Sha256", payloadHash)
	}
	date := request.Header.Get("X-Amz-Date")
	if date == "" {
		date = time.Now().UTC().Format("20060102T150405Z")
		request.Header.Set("X-Amz-Date", date)
	}
	if len(date) < 8 {
		return errors.New("invalid X-Amz-Date")
	}
	if config.SessionToken != "" {
		request.Header.Set("X-Amz-Security-Token", config.SessionToken)
	}

	canonicalHeaders, signedHeaders := awsCanonicalHeaders(request)
	canonicalURI := request.URL.EscapedPath()
	if canonicalURI == "" {
		canonicalURI = "/"
	}
	canonicalQuery := strings.ReplaceAll(request.URL.Query().Encode(), "+", "%20")
	canonicalRequest := strings.Join([]string{
		request.Method,
		canonicalURI,
		canonicalQuery,
		canonicalHeaders,
		signedHeaders,
		payloadHash,
	}, "\n")
	scope := date[:8] + "/" + config.Region + "/" + config.Service + "/aws4_request"
	stringToSign := "AWS4-HMAC-SHA256\n" + date + "\n" + scope + "\n" + sha256Hex([]byte(canonicalRequest))
	dateKey := hmacBytes(sha256.New, []byte("AWS4"+config.SecretKey), date[:8])
	regionKey := hmacBytes(sha256.New, dateKey, config.Region)
	serviceKey := hmacBytes(sha256.New, regionKey, config.Service)
	signingKey := hmacBytes(sha256.New, serviceKey, "aws4_request")
	signature := hex.EncodeToString(hmacBytes(sha256.New, signingKey, stringToSign))
	request.Header.Set("Authorization", "AWS4-HMAC-SHA256 "+
		"Credential="+config.AccessKey+"/"+scope+", "+
		"SignedHeaders="+signedHeaders+", "+
		"Signature="+signature)
	return nil
}

func awsCanonicalHeaders(request *http.Request) (string, string) {
	names := []string{"host"}
	for name := range request.Header {
		lower := strings.ToLower(name)
		if awsSignableHeader(lower) && !contains(names, lower) {
			names = append(names, lower)
		}
	}
	sort.Strings(names)
	var canonical strings.Builder
	for _, name := range names {
		value := request.Header.Get(name)
		if name == "host" {
			value = request.Host
			if value == "" {
				value = request.URL.Host
			}
		}
		canonical.WriteString(name)
		canonical.WriteByte(':')
		canonical.WriteString(strings.Join(strings.Fields(value), " "))
		canonical.WriteByte('\n')
	}
	return canonical.String(), strings.Join(names, ";")
}

func signHawk(request *http.Request, body []byte, config *model.HawkAuth) error {
	if config.ID == "" || config.Key == "" {
		return errors.New("hawk requires an id and key")
	}
	algorithm := strings.ToLower(config.Algorithm)
	var hashFunc func() hash.Hash
	switch algorithm {
	case "", "sha256":
		hashFunc = sha256.New
	case "sha1":
		hashFunc = sha1.New
	default:
		return fmt.Errorf("unsupported Hawk algorithm %q", config.Algorithm)
	}
	timestamp := config.Timestamp
	if timestamp == "" {
		timestamp = strconv.FormatInt(time.Now().Unix(), 10)
	}
	nonce := config.Nonce
	if nonce == "" {
		nonce = randomHex(6)
	}
	port := request.URL.Port()
	if port == "" {
		if request.URL.Scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	payloadHash := ""
	if config.IncludePayloadHash {
		normalizedPayload := "hawk.1.payload\n" +
			strings.ToLower(strings.TrimSpace(strings.Split(request.Header.Get("Content-Type"), ";")[0])) + "\n" +
			string(body) + "\n"
		sum := hashFunc()
		_, _ = sum.Write([]byte(normalizedPayload))
		payloadHash = base64.StdEncoding.EncodeToString(sum.Sum(nil))
	}
	normalized := "hawk.1.header\n" +
		timestamp + "\n" +
		nonce + "\n" +
		request.Method + "\n" +
		request.URL.RequestURI() + "\n" +
		strings.ToLower(request.URL.Hostname()) + "\n" +
		port + "\n" +
		payloadHash + "\n" +
		config.ExtraData + "\n" +
		config.AppID + "\n" +
		config.Delegation + "\n"
	mac := hmac.New(hashFunc, []byte(config.Key))
	_, _ = mac.Write([]byte(normalized))
	macValue := base64.StdEncoding.EncodeToString(mac.Sum(nil))
	parts := []string{
		`id="` + quoteAuth(config.ID) + `"`,
		`ts="` + quoteAuth(timestamp) + `"`,
		`nonce="` + quoteAuth(nonce) + `"`,
	}
	if payloadHash != "" {
		parts = append(parts, `hash="`+quoteAuth(payloadHash)+`"`)
	}
	if config.ExtraData != "" {
		parts = append(parts, `ext="`+quoteAuth(config.ExtraData)+`"`)
	}
	if config.AppID != "" {
		parts = append(parts, `app="`+quoteAuth(config.AppID)+`"`)
	}
	if config.Delegation != "" {
		parts = append(parts, `dlg="`+quoteAuth(config.Delegation)+`"`)
	}
	parts = append(parts, `mac="`+quoteAuth(macValue)+`"`)
	request.Header.Set("Authorization", "Hawk "+strings.Join(parts, ", "))
	return nil
}

func digestAuthorization(
	request *http.Request,
	body []byte,
	config *model.DigestAuth,
	challenge string,
) (string, error) {
	parameters, err := parseDigestChallenge(challenge)
	if err != nil {
		return "", err
	}
	realm := firstNonEmpty(config.Realm, parameters["realm"])
	nonce := firstNonEmpty(config.Nonce, parameters["nonce"])
	algorithm := strings.ToUpper(firstNonEmpty(config.Algorithm, parameters["algorithm"], "MD5"))
	opaque := firstNonEmpty(config.Opaque, parameters["opaque"])
	if realm == "" || nonce == "" {
		return "", errors.New("digest challenge is missing realm or nonce")
	}
	hashFunc, sess, err := digestHash(algorithm)
	if err != nil {
		return "", err
	}
	cnonce := config.ClientNonce
	if cnonce == "" {
		cnonce = randomHex(8)
	}
	nc := config.NonceCount
	if nc == "" {
		nc = "00000001"
	}
	qop := chooseQOP(firstNonEmpty(config.QOP, parameters["qop"]))
	uri := request.URL.RequestURI()
	ha1 := hashHex(hashFunc, config.Username+":"+realm+":"+config.Password)
	if sess {
		ha1 = hashHex(hashFunc, ha1+":"+nonce+":"+cnonce)
	}
	a2 := request.Method + ":" + uri
	if qop == "auth-int" {
		a2 += ":" + hashHex(hashFunc, string(body))
	}
	ha2 := hashHex(hashFunc, a2)
	responseInput := ha1 + ":" + nonce + ":"
	if qop != "" {
		responseInput += nc + ":" + cnonce + ":" + qop + ":"
	}
	response := hashHex(hashFunc, responseInput+ha2)
	fields := []string{
		`username="` + quoteAuth(config.Username) + `"`,
		`realm="` + quoteAuth(realm) + `"`,
		`nonce="` + quoteAuth(nonce) + `"`,
		`uri="` + quoteAuth(uri) + `"`,
		"response=\"" + response + "\"",
		"algorithm=" + algorithm,
	}
	if opaque != "" {
		fields = append(fields, `opaque="`+quoteAuth(opaque)+`"`)
	}
	if qop != "" {
		fields = append(fields, "qop="+qop, "nc="+nc, `cnonce="`+quoteAuth(cnonce)+`"`)
	}
	return "Digest " + strings.Join(fields, ", "), nil
}

func parseDigestChallenge(challenge string) (map[string]string, error) {
	if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(challenge)), "digest ") {
		return nil, errors.New("server did not return a Digest challenge")
	}
	input := strings.TrimSpace(strings.TrimSpace(challenge)[len("Digest "):])
	result := make(map[string]string)
	for len(input) > 0 {
		equal := strings.IndexByte(input, '=')
		if equal <= 0 {
			return nil, errors.New("invalid Digest challenge")
		}
		key := strings.ToLower(strings.TrimSpace(input[:equal]))
		input = strings.TrimSpace(input[equal+1:])
		var value string
		if strings.HasPrefix(input, `"`) {
			input = input[1:]
			var builder strings.Builder
			escaped := false
			index := 0
			for ; index < len(input); index++ {
				char := input[index]
				if escaped {
					builder.WriteByte(char)
					escaped = false
				} else if char == '\\' {
					escaped = true
				} else if char == '"' {
					break
				} else {
					builder.WriteByte(char)
				}
			}
			if index >= len(input) {
				return nil, errors.New("unterminated Digest challenge value")
			}
			value = builder.String()
			input = input[index+1:]
		} else {
			comma := strings.IndexByte(input, ',')
			if comma < 0 {
				value, input = strings.TrimSpace(input), ""
			} else {
				value, input = strings.TrimSpace(input[:comma]), input[comma:]
			}
		}
		result[key] = value
		input = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(input), ","))
	}
	return result, nil
}

func digestHash(algorithm string) (func() hash.Hash, bool, error) {
	switch strings.ToUpper(algorithm) {
	case "MD5":
		return md5.New, false, nil
	case "MD5-SESS":
		return md5.New, true, nil
	case "SHA-256":
		return sha256.New, false, nil
	case "SHA-256-SESS":
		return sha256.New, true, nil
	default:
		return nil, false, fmt.Errorf("unsupported Digest algorithm %q", algorithm)
	}
}

func chooseQOP(value string) string {
	for _, candidate := range strings.Split(value, ",") {
		if strings.EqualFold(strings.TrimSpace(candidate), "auth") {
			return "auth"
		}
	}
	for _, candidate := range strings.Split(value, ",") {
		if strings.EqualFold(strings.TrimSpace(candidate), "auth-int") {
			return "auth-int"
		}
	}
	return ""
}

func hashHex(newHash func() hash.Hash, value string) string {
	digest := newHash()
	_, _ = digest.Write([]byte(value))
	return hex.EncodeToString(digest.Sum(nil))
}

func sha256Hex(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}

func hmacBytes(newHash func() hash.Hash, key []byte, value string) []byte {
	digest := hmac.New(newHash, key)
	_, _ = digest.Write([]byte(value))
	return digest.Sum(nil)
}

func randomHex(size int) string {
	value := make([]byte, size)
	if _, err := rand.Read(value); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 16)
	}
	return hex.EncodeToString(value)
}

func quoteAuth(value string) string {
	return strings.ReplaceAll(strings.ReplaceAll(value, `\`, `\\`), `"`, `\"`)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func awsSignableHeader(name string) bool {
	switch name {
	case "authorization", "connection", "expect", "transfer-encoding", "user-agent", "x-amzn-trace-id":
		return false
	default:
		return true
	}
}
