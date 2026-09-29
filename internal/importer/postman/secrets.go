package postman

import (
	"fmt"
	"regexp"
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
		if auth.OAuth2 != nil {
			move("oauth2", "client_secret", &auth.OAuth2.ClientSecret)
			move("oauth2", "password", &auth.OAuth2.Password)
			if auth.OAuth2.Token != nil {
				for _, token := range []*string{
					&auth.OAuth2.Token.AccessToken,
					&auth.OAuth2.Token.RefreshToken,
					&auth.OAuth2.Token.IDToken,
				} {
					if isLiteralSecret(*token) {
						report.Counts.SecretsMoved++
					}
				}
			}
		}
	case model.AuthAWSV4:
		if auth.AWSV4 != nil {
			move("awsv4", "secret_key", &auth.AWSV4.SecretKey)
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
				fmt.Sprintf("require(%q) is not supported", match[1]))
		}
	}
}
