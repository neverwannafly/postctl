package request

import (
	"github.com/shubham/postctl/internal/tui/textin"
	"regexp"
	"strings"

	"github.com/shubham/postctl/internal/model"
	"github.com/shubham/postctl/internal/ui"
	"github.com/shubham/postctl/internal/vars"
)

// varRE matches {{name}} references (also with inner spaces).
var varRE = regexp.MustCompile(`\{\{\s*([^{}]*?)\s*\}\}`)

// scope resolves variables for previews and copy-as-curl. Secret variable
// values are replaced by mask unless the scope was built with reveal, so a
// preview can never show one.
type scope struct {
	store *vars.MemStore
}

// chainOf returns the containers from the collection down to the parent of
// the request with the given ID (outermost first), or nil when not found.
func chainOf(c *model.Collection, id model.ID) []model.Container {
	if c == nil {
		return nil
	}
	var walk func(parent model.Container, path []model.Container) []model.Container
	walk = func(parent model.Container, path []model.Container) []model.Container {
		path = append(path, parent)
		for _, it := range parent.Children() {
			switch v := it.(type) {
			case *model.Request:
				if v.ID == id {
					return path
				}
			case *model.Folder:
				if got := walk(v, append([]model.Container(nil), path...)); got != nil {
					return got
				}
			}
		}
		return nil
	}
	return walk(c, nil)
}

// containerVars returns the variables a container defines.
func containerVars(c model.Container) []model.Variable {
	switch v := c.(type) {
	case *model.Collection:
		return v.Variables
	case *model.Folder:
		return v.Variables
	}
	return nil
}

// containerAuth returns the auth a container sets, or nil.
func containerAuth(c model.Container) *model.Auth {
	switch v := c.(type) {
	case *model.Collection:
		return v.Auth
	case *model.Folder:
		return v.Auth
	}
	return nil
}

// containerName is the display name of a container.
func containerName(c model.Container) string {
	switch v := c.(type) {
	case *model.Collection:
		return v.Name
	case *model.Folder:
		return v.Name
	}
	return ""
}

// maskVars returns vars with secret values replaced by mask.
func maskVars(in []model.Variable, mask string) []model.Variable {
	out := make([]model.Variable, len(in))
	for i, v := range in {
		if v.IsSecret() {
			v.Value = mask
		}
		out[i] = v
	}
	return out
}

// newScope builds the variable scope for a request: globals, then collection
// and folder variables (inner folders win), then the active environment.
func newScope(ws *model.Workspace, chain []model.Container, env *model.Environment, mask string, reveal bool) *scope {
	prep := func(in []model.Variable) []model.Variable {
		if reveal {
			return in
		}
		return maskVars(in, mask)
	}
	var globals, coll, envVars []model.Variable
	if ws != nil && ws.Globals != nil {
		globals = prep(ws.Globals.Variables)
	}
	for _, c := range chain {
		coll = append(coll, prep(containerVars(c))...)
	}
	if env != nil {
		envVars = prep(env.Variables)
	}
	return &scope{store: vars.New(globals, coll, envVars, nil)}
}

// resolve substitutes known variables and returns the names it could not
// resolve. Dynamic variables ({{$guid}}) are left as written so a preview does
// not flicker with random values.
func (s *scope) resolve(raw string) (string, []string) {
	if s == nil || !strings.Contains(raw, "{{") {
		return raw, nil
	}
	guarded := strings.ReplaceAll(raw, "{{$", "{\x01{$")
	out := strings.ReplaceAll(s.store.Replace(guarded), "{\x01{$", "{{$")
	var missing []string
	seen := map[string]bool{}
	for _, m := range varRE.FindAllStringSubmatch(raw, -1) {
		name := m[1]
		if name == "" || strings.HasPrefix(name, "$") || seen[name] {
			continue
		}
		seen[name] = true
		if _, _, ok := s.store.Get(name); !ok {
			missing = append(missing, name)
		}
	}
	return out, missing
}

// highlight returns spans for every {{variable}} in s: info for resolved
// names, warning for unresolved ones. Indices are runes.
func (s *scope) highlight(str string) []textin.Span {
	if !strings.Contains(str, "{{") {
		return nil
	}
	var spans []textin.Span
	for _, loc := range varRE.FindAllStringSubmatchIndex(str, -1) {
		name := str[loc[2]:loc[3]]
		tok := ui.SyntaxKey
		if s != nil && !strings.HasPrefix(name, "$") {
			if _, _, ok := s.store.Get(name); !ok {
				tok = ui.StatusWarning
			}
		} else if s == nil {
			tok = ui.TextPrimary
		}
		from := len([]rune(str[:loc[0]]))
		spans = append(spans, textin.Span{From: from, To: from + len([]rune(str[loc[0]:loc[1]])), Tok: tok})
	}
	return spans
}
