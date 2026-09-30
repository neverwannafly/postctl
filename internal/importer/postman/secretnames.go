package postman

import (
	"sort"
	"strings"
	"unicode"

	"github.com/shubham/postctl/internal/model"
)

// The importer marks a variable secret when its key looks like a credential,
// because Postman's own `type` is often `default` for values that are in fact
// tokens (and a script's pm.environment.set(...) would then be written to a
// plaintext YAML file, plan R3). The rule works on the words of the key:
//
//  1. Split the key into lower-case words at every non-alphanumeric character,
//     at letter/digit changes and at camelCase and acronym boundaries
//     ("krakenD_token" → kraken, d, token; "APIKey" → api, key; "token2" →
//     token, 2; "CF_COOKIE_HEADER" → cf, cookie, header).
//  2. The key is secret when one word (or two adjacent words) names a
//     credential:
//     - a word that is, or ends with, token, secret, password, passwd,
//     passphrase, cookie or credential (plural forms too), so authtoken,
//     clientSecret and csrf_token match;
//     - the whole word session, jwt, bearer, authorization, apikey, pwd or
//     passcode;
//     - the pairs api+key, private+key, secret+key, signing+key,
//     encryption+key, master+key and auth+key.
//  3. It is not secret when a word after the match describes the credential
//     instead of holding it: url, uri, endpoint, host, domain, name, type,
//     scope, prefix, expiry/expires/expiration, ttl, lifetime, timeout,
//     path, file, count, length, method, algorithm, version, format,
//     location, limit, age, status, enabled or flag. So token_url,
//     tokenExpiresIn, session_timeout and cookie_name stay plain, while
//     auth_url and base_url never matched to begin with.
//
// It errs toward marking: a variable wrongly marked secret only needs its
// value set with `postctl env set <env> <key> --secret`, while a token left
// plain sits in a readable YAML file.

// secretSuffixes are the stems a word may end with (after dropping a plural s).
var secretSuffixes = []string{
	"token", "secret", "password", "passwd", "passphrase", "cookie", "credential",
}

// secretWords are whole words that name a credential.
var secretWords = map[string]bool{
	"session": true, "jwt": true, "bearer": true, "authorization": true,
	"apikey": true, "pwd": true, "passcode": true,
	"privatekey": true, "secretkey": true, "signingkey": true, "authkey": true,
}

// secretPairs are two adjacent words that together name a credential.
var secretPairs = map[[2]string]bool{
	{"api", "key"}: true, {"private", "key"}: true, {"secret", "key"}: true,
	{"signing", "key"}: true, {"encryption", "key"}: true, {"master", "key"}: true,
	{"auth", "key"}: true,
}

// descriptorWords say something about a credential (where it lives, how long
// it lasts) without being one.
var descriptorWords = map[string]bool{
	"url": true, "uri": true, "endpoint": true, "host": true, "hostname": true,
	"domain": true, "name": true, "type": true, "scope": true, "prefix": true,
	"suffix": true, "expiry": true, "expires": true, "expiration": true,
	"expired": true, "ttl": true, "lifetime": true, "timeout": true, "path": true,
	"file": true, "count": true, "length": true, "method": true, "algorithm": true,
	"version": true, "format": true, "location": true, "limit": true, "age": true,
	"status": true, "enabled": true, "flag": true,
}

// LooksSecretName reports whether a variable key looks like it holds a
// credential. See the rule at the top of this file.
func LooksSecretName(key string) bool {
	words := splitKeyWords(key)
	for i, word := range words {
		width := 0
		switch {
		case isSecretWord(word):
			width = 1
		case i+1 < len(words) && secretPairs[[2]string{word, words[i+1]}]:
			width = 2
		}
		if width == 0 {
			continue
		}
		described := false
		for _, later := range words[i+width:] {
			if descriptorWords[later] {
				described = true
				break
			}
		}
		if !described {
			return true
		}
	}
	return false
}

func isSecretWord(word string) bool {
	if secretWords[word] {
		return true
	}
	singular := strings.TrimSuffix(word, "s")
	if secretWords[singular] {
		return true
	}
	for _, suffix := range secretSuffixes {
		if strings.HasSuffix(word, suffix) || strings.HasSuffix(singular, suffix) {
			return true
		}
	}
	return false
}

// splitKeyWords lower-cases key and splits it at non-alphanumeric characters,
// lower→upper transitions and acronym ends (APIKey → API, Key).
func splitKeyWords(key string) []string {
	var words []string
	var current []rune
	flush := func() {
		if len(current) > 0 {
			words = append(words, strings.ToLower(string(current)))
			current = current[:0]
		}
	}
	runes := []rune(key)
	for i, r := range runes {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			flush()
			continue
		}
		if len(current) > 0 {
			previous := runes[i-1]
			nextIsLower := i+1 < len(runes) && unicode.IsLower(runes[i+1])
			switch {
			case unicode.IsDigit(r) != unicode.IsDigit(previous):
				flush() // token2 → token, 2
			case unicode.IsUpper(r) && (unicode.IsLower(previous) ||
				(unicode.IsUpper(previous) && nextIsLower)):
				flush() // krakenD → kraken, D; APIKey → API, Key
			}
		}
		current = append(current, r)
	}
	flush()
	return words
}

// MarkedScope lists the variables of one scope that were marked secret by
// name. Only key names are recorded, never values.
type MarkedScope struct {
	Scope string   `json:"scope"`
	Keys  []string `json:"keys"`
}

// markSecretsByName marks variables of doc whose key looks secret as
// model.VarSecret and records them in report. A variable that already has a
// value keeps it in memory: the store moves it to the secret store on save
// (plan D2). With options.KeepLiteralSecrets a variable that has a value is
// left alone, and only empty ones get the type.
func markSecretsByName(doc *Document, options Options, report *Report) {
	if options.NoMarkSecrets {
		return
	}
	mark := func(scope string, variables []model.Variable) {
		var keys []string
		for i := range variables {
			variable := &variables[i]
			if variable.IsSecret() || variable.Key == "" || !LooksSecretName(variable.Key) {
				continue
			}
			if variable.Value != "" && options.KeepLiteralSecrets {
				continue
			}
			variable.Type = model.VarSecret
			variable.DataType = ""
			keys = append(keys, variable.Key)
			report.Counts.SecretsMarked++
			if variable.Value != "" {
				report.Counts.SecretsMarkedWithValue++
			}
		}
		if len(keys) > 0 {
			report.Marked = append(report.Marked, MarkedScope{Scope: scope, Keys: keys})
		}
	}
	switch doc.Kind {
	case KindEnvironment:
		mark("Environment › "+doc.Environment.Name, doc.Environment.Variables)
	case KindGlobals:
		mark("Globals", doc.Globals.Variables)
	case KindCollection:
		collection := doc.Collection
		mark("Collection › "+collection.Name, collection.Variables)
		paths := map[model.ID]string{}
		_ = collection.Walk(func(item model.Item, parent model.Container, _ int) error {
			folder, ok := item.(*model.Folder)
			if !ok {
				return nil
			}
			path := joinPath(collection.Name, folder.Name)
			if parentFolder, ok := parent.(*model.Folder); ok {
				path = joinPath(paths[parentFolder.ID], folder.Name)
			}
			paths[folder.ID] = path
			mark("Folder › "+path, folder.Variables)
			return nil
		})
	}
}

// sortMarked orders the marked scopes by name, keys as they appear.
func sortMarked(marked []MarkedScope) {
	sort.SliceStable(marked, func(i, j int) bool { return marked[i].Scope < marked[j].Scope })
}
