package postman

import (
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/shubham/postctl/internal/model"
)

// Postman does not store current variable values locally, so an import can
// leave a variable the requests need with no value at all. CheckEmptyInputs
// warns about those, by key name only, so the user knows what to fill in
// before the first send.
//
// A variable is warned about when all of these hold:
//   - it is declared, without a value, in an imported environment, globals or
//     collection (a name declared nowhere is not an "empty input");
//   - it is read: `pm.environment.get("X")`, `pm.variables.get("X")`,
//     `pm.collectionVariables.get("X")`, `pm.globals.get("X")` (or the legacy
//     `postman.getEnvironmentVariable` / `getGlobalVariable`) in a script, or
//     `{{X}}` in an enabled part of a request (URL, query, headers, body) or
//     in any auth block;
//   - no imported environment, globals, collection or folder gives it a value;
//   - no script sets it (`pm.*.set("X", ...)`, `postman.setEnvironmentVariable`,
//     `postman.setGlobalVariable`), because then a run fills it in.
//
// Variables read or set through a computed name (`pm.environment.get(key)`)
// cannot be seen and are never reported. Values are never inspected beyond
// "is empty".

var (
	scriptReadPattern = regexp.MustCompile(
		`\b(?:pm\.(?:environment|variables|collectionVariables|globals)\.get|` +
			`postman\.(?:getEnvironmentVariable|getGlobalVariable))\(\s*(["'` + "`" + `])([^"'` + "`" + `\n]+?)["'` + "`" + `]`)
	scriptSetPattern = regexp.MustCompile(
		`\b(?:pm\.(?:environment|variables|collectionVariables|globals)\.set|` +
			`postman\.(?:setEnvironmentVariable|setGlobalVariable))\(\s*(["'` + "`" + `])([^"'` + "`" + `\n]+?)["'` + "`" + `]`)
	templatePattern = regexp.MustCompile(`\{\{\s*([^{}\s][^{}]*?)\s*\}\}`)
)

// CheckEmptyInputs adds one warning (code "empty-input") for each imported
// environment in which a variable that the imported data reads, and that
// nothing gives a value or sets, is declared empty. A variable declared empty
// only in a collection or in globals gets one warning naming that scope.
func CheckEmptyInputs(documents []*Document, report *Report) {
	scan := newInputScan()
	for _, document := range documents {
		switch document.Kind {
		case KindEnvironment:
			scan.environment(document.Environment)
		case KindGlobals:
			scan.variables("globals", "", document.Globals.Variables)
		case KindCollection:
			scan.collection(document.Collection)
		}
	}
	keys := make([]string, 0, len(scan.declared))
	for key := range scan.declared {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if !scan.read[key] || scan.set[key] || scan.valued[key] {
			continue
		}
		emptyIn := scan.declared[key]
		for _, place := range emptyIn {
			report.AddWarning("", "empty-input", place.message(key, scan.secret[key] || LooksSecretName(key)))
		}
	}
	report.sort()
}

// inputScan collects what the imported documents declare, read and set.
type inputScan struct {
	declared map[string][]emptyPlace // key → where it is declared without a value
	valued   map[string]bool
	secret   map[string]bool
	read     map[string]bool
	set      map[string]bool
}

// emptyPlace is one scope that declares a variable without a value.
type emptyPlace struct {
	kind string // "environment", "collection" or "globals"
	name string
}

func newInputScan() *inputScan {
	return &inputScan{
		declared: map[string][]emptyPlace{},
		valued:   map[string]bool{},
		secret:   map[string]bool{},
		read:     map[string]bool{},
		set:      map[string]bool{},
	}
}

func (p emptyPlace) message(key string, secret bool) string {
	switch p.kind {
	case "environment":
		hint := "postctl env set " + shellWord(p.name) + " " + shellWord(key)
		if secret {
			hint += " --secret"
		} else {
			hint += " <value>"
		}
		return fmt.Sprintf("%s is empty in %s; set it with `%s`", key, p.name, hint)
	case "globals":
		return fmt.Sprintf("%s is empty in Globals; give it a value in the globals editor", key)
	default:
		return fmt.Sprintf("%s is empty in collection %s; give it a value in the collection's variables", key, p.name)
	}
}

func (s *inputScan) variables(kind, name string, variables []model.Variable) {
	for _, variable := range variables {
		if variable.Key == "" || variable.Disabled {
			continue
		}
		if variable.IsSecret() {
			s.secret[variable.Key] = true
		}
		if variable.Value != "" {
			s.valued[variable.Key] = true
			continue
		}
		place := emptyPlace{kind: kind, name: name}
		if !slices.Contains(s.declared[variable.Key], place) {
			s.declared[variable.Key] = append(s.declared[variable.Key], place)
		}
	}
}

func (s *inputScan) environment(env *model.Environment) {
	s.variables("environment", env.Name, env.Variables)
}

func (s *inputScan) collection(collection *model.Collection) {
	s.variables("collection", collection.Name, collection.Variables)
	s.scripts(collection.Scripts)
	s.auth(collection.Auth)
	_ = collection.Walk(func(item model.Item, _ model.Container, _ int) error {
		switch typed := item.(type) {
		case *model.Folder:
			// Folder variables give a value but are not an input the user
			// sets through `env set`, so they only count as valued.
			for _, variable := range typed.Variables {
				if variable.Value != "" && !variable.Disabled {
					s.valued[variable.Key] = true
				}
			}
			s.scripts(typed.Scripts)
			s.auth(typed.Auth)
		case *model.Request:
			s.request(typed)
		}
		return nil
	})
}

func (s *inputScan) scripts(scripts model.Scripts) {
	for _, script := range []*model.Script{scripts.PreRequest, scripts.Test} {
		if script == nil || script.Disabled {
			continue
		}
		for _, match := range scriptReadPattern.FindAllStringSubmatch(script.Source, -1) {
			s.read[match[2]] = true
		}
		for _, match := range scriptSetPattern.FindAllStringSubmatch(script.Source, -1) {
			s.set[match[2]] = true
		}
	}
}

func (s *inputScan) request(request *model.Request) {
	s.scripts(request.Scripts)
	s.auth(request.Auth)
	s.refs(request.URL.Raw)
	for _, query := range request.URL.Query {
		if !query.Disabled {
			s.refs(query.Key)
			s.refs(query.Value)
		}
	}
	for _, header := range request.Headers {
		if !header.Disabled {
			s.refs(header.Key)
			s.refs(header.Value)
		}
	}
	if request.Body.Disabled {
		return
	}
	s.refs(request.Body.Raw)
	for _, pair := range request.Body.URLEncoded {
		if !pair.Disabled {
			s.refs(pair.Key)
			s.refs(pair.Value)
		}
	}
	for _, part := range request.Body.FormData {
		if !part.Disabled {
			s.refs(part.Key)
			s.refs(part.Value)
		}
	}
	if request.Body.GraphQL != nil {
		s.refs(request.Body.GraphQL.Query)
		s.refs(request.Body.GraphQL.Variables)
	}
}

// auth reads the template references of every auth field. The block is
// serialised only to find `{{name}}` text; nothing from it is kept.
func (s *inputScan) auth(auth *model.Auth) {
	if auth == nil {
		return
	}
	if encoded, err := json.Marshal(auth); err == nil {
		s.refs(string(encoded))
	}
}

func (s *inputScan) refs(text string) {
	if !strings.Contains(text, "{{") {
		return
	}
	for _, match := range templatePattern.FindAllStringSubmatch(text, -1) {
		name := strings.TrimSpace(match[1])
		if name != "" && !strings.HasPrefix(name, "$") {
			s.read[name] = true
		}
	}
}

// shellWord quotes s for a shell only when it needs it.
func shellWord(s string) string {
	safe := func(r rune) bool {
		return r == '-' || r == '_' || r == '.' || r == '/' || r == ':' ||
			(r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
	}
	if s != "" && strings.IndexFunc(s, func(r rune) bool { return !safe(r) }) < 0 {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
