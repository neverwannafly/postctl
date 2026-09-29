package postman

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"github.com/shubham/postctl/internal/model"
)

func moveCollectionSecrets(collection *model.Collection, report *Report) {
	used := map[string]bool{}
	for _, variable := range collection.Variables {
		used[variable.Key] = true
	}
	moveAuthSecrets(collection.Auth, collection, used, report)
	_ = collection.Walk(func(item model.Item, _ model.Container, _ int) error {
		switch typed := item.(type) {
		case *model.Folder:
			moveAuthSecrets(typed.Auth, collection, used, report)
		case *model.Request:
			moveAuthSecrets(typed.Auth, collection, used, report)
		}
		return nil
	})
}

func moveAuthSecrets(auth *model.Auth, collection *model.Collection, used map[string]bool, report *Report) {
	if auth == nil {
		return
	}
	move := func(authType, field string, target *string) {
		if target == nil || !isLiteralSecret(*target) {
			return
		}
		base := normalizeSecretName(authType + "_" + field)
		if base == "" {
			base = "secret"
		}
		name := base
		for suffix := 2; used[name]; suffix++ {
			name = fmt.Sprintf("%s_%d", base, suffix)
		}
		used[name] = true
		collection.Variables = append(collection.Variables, model.Variable{
			Key: name, Value: *target, Type: model.VarSecret,
		})
		*target = "{{" + name + "}}"
		report.Counts.SecretsMoved++
	}
	authType := string(auth.Type)
	switch auth.Type {
	case model.AuthBearer:
		if auth.Bearer != nil {
			move("bearer", "token", &auth.Bearer.Token)
		}
	case model.AuthBasic:
		if auth.Basic != nil {
			move("basic", "password", &auth.Basic.Password)
		}
	case model.AuthAPIKey:
		if auth.APIKey != nil {
			move("apikey", "value", &auth.APIKey.Value)
		}
	case model.AuthDigest:
		if auth.Digest != nil {
			move("digest", "password", &auth.Digest.Password)
		}
	case model.AuthOAuth2:
		if cfg := auth.OAuth2; cfg != nil {
			move("oauth2", "client_secret", &cfg.ClientSecret)
			move("oauth2", "password", &cfg.Password)
			move("oauth2", "code_verifier", &cfg.CodeVerifier)
			// Access, refresh and ID tokens stay on OAuth2Config.Token: the
			// engine reads them through the OAuth 2.0 source, and the store
			// moves them to the Keychain on save (model rule 6). They are
			// counted separately by countOAuthTokens, not as secrets moved
			// to variables.
			moveRawSecrets(authType, cfg.Extra, move)
		}
	case model.AuthAWSV4:
		if auth.AWSV4 != nil {
			move("awsv4", "secret_key", &auth.AWSV4.SecretKey)
			move("awsv4", "session_token", &auth.AWSV4.SessionToken)
		}
	case model.AuthHawk:
		if auth.Hawk != nil {
			move("hawk", "key", &auth.Hawk.Key)
		}
	case model.AuthNTLM:
		if auth.NTLM != nil {
			move("ntlm", "password", &auth.NTLM.Password)
		}
	}
	// Whatever the typed fields do not model (unsupported schemes such as
	// OAuth 1.0, EdgeGrid, JWT and ASAP, or extra parameters on a supported
	// one) is kept in Raw and is written to the YAML verbatim.
	if auth.Type == model.AuthUnsupported {
		authType = auth.OriginalType
	}
	moveRawSecrets(authType, auth.Raw, move)
}

// moveRawSecrets moves string values of untyped auth parameters whose names
// look like credentials into secret variables, walking nested maps and lists.
func moveRawSecrets(authType string, raw map[string]any, move func(authType, field string, target *string)) {
	for _, key := range sortedKeys(raw) {
		switch typed := raw[key].(type) {
		case string:
			if sensitiveParam(key) {
				move(authType, key, &typed)
				raw[key] = typed
			}
		case map[string]any:
			moveRawSecrets(authType, typed, move)
		case []any:
			for i, entry := range typed {
				if text, ok := entry.(string); ok && sensitiveParam(key) {
					move(authType, key, &text)
					typed[i] = text
				} else if nested, ok := entry.(map[string]any); ok {
					moveRawSecrets(authType, nested, move)
				}
			}
		}
	}
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// sensitiveParam reports whether an untyped auth parameter name denotes a
// credential. It errs toward moving: a moved value is still usable through its
// {{variable}}, a missed one sits in a 0644 YAML file.
func sensitiveParam(name string) bool {
	key := strings.ToLower(strings.NewReplacer("_", "", "-", "", " ", "").Replace(name))
	for _, suffix := range []string{"url", "uri", "type", "name", "scope", "prefix", "location", "endpoint", "method", "algorithm", "version"} {
		if strings.HasSuffix(key, suffix) {
			return false
		}
	}
	switch key {
	case "token", "key", "authkey", "secret", "pass":
		return true
	}
	for _, part := range []string{
		"secret", "password", "passwd", "privatekey", "assertion", "apikey", "signature",
		"credential", "accesstoken", "refreshtoken", "idtoken", "sessiontoken", "authtoken", "bearer",
	} {
		if strings.Contains(key, part) {
			return true
		}
	}
	return false
}

// countOAuthTokens counts the literal OAuth 2.0 access, refresh and ID tokens
// in a collection's auth blocks. The store writes them to the Keychain when
// the workspace is saved, whether or not literal secrets are kept in YAML.
func countOAuthTokens(collection *model.Collection) int {
	count := 0
	visit := func(auth *model.Auth) {
		if auth == nil || auth.OAuth2 == nil || auth.OAuth2.Token == nil {
			return
		}
		token := auth.OAuth2.Token
		for _, value := range []string{token.AccessToken, token.RefreshToken, token.IDToken} {
			if strings.TrimSpace(value) != "" {
				count++
			}
		}
	}
	visit(collection.Auth)
	_ = collection.Walk(func(item model.Item, _ model.Container, _ int) error {
		switch typed := item.(type) {
		case *model.Folder:
			visit(typed.Auth)
		case *model.Request:
			visit(typed.Auth)
		}
		return nil
	})
	return count
}

func isLiteralSecret(value string) bool {
	trimmed := strings.TrimSpace(value)
	return trimmed != "" && (!strings.Contains(trimmed, "{{") || !strings.Contains(trimmed, "}}"))
}

func normalizeSecretName(value string) string {
	var result strings.Builder
	lastUnderscore := false
	for _, r := range strings.ToLower(value) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			result.WriteRune(r)
			lastUnderscore = false
		} else if !lastUnderscore {
			result.WriteByte('_')
			lastUnderscore = true
		}
	}
	return strings.Trim(result.String(), "_")
}

var requirePattern = regexp.MustCompile(`\brequire\s*\(\s*['"]([^'"]+)['"]\s*\)`)

func lintRequestScripts(path string, scripts model.Scripts, report *Report) {
	lintScript(path, "pre-request", scripts.PreRequest, report)
	lintScript(path, "test", scripts.Test, report)
}

func lintScript(path, kind string, script *model.Script, report *Report) {
	if script == nil {
		return
	}
	source := script.Source
	checks := []struct {
		needle  string
		code    string
		message string
	}{
		{"pm.visualizer", "script-pm-visualizer", "pm.visualizer is not supported and will do nothing"},
		{"pm.vault", "script-pm-vault", "pm.vault is not supported and will do nothing"},
		{"pm.require", "script-pm-require", "pm.require packages are not supported"},
		{"tests[", "script-legacy-tests", "Legacy tests[] is best-effort only"},
		{"responseBody", "script-legacy-response-body", "Legacy responseBody is best-effort only"},
		{"postman.getEnvironmentVariable", "script-legacy-environment",
			"Legacy postman.getEnvironmentVariable is best-effort only"},
	}
	for _, check := range checks {
		if strings.Contains(source, check.needle) {
			report.AddWarning(path, check.code+"-"+kind, check.message)
		}
	}
	supported := map[string]bool{
		"lodash": true, "moment": true, "crypto-js": true, "uuid": true,
		"chai": true, "ajv": true, "xml2js": true,
	}
	for _, match := range requirePattern.FindAllStringSubmatch(source, -1) {
		if !supported[match[1]] {
			report.AddWarning(path, "script-unknown-require-"+kind,
				"An unsupported require() module is used")
		}
	}
}
