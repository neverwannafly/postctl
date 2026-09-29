package model

import (
	"errors"
	"fmt"
	"strings"
)

// ValidationError lists every problem found by a Validate method. Each entry
// is prefixed with a path such as `collection "Payments" > "Create charge"`.
type ValidationError struct {
	// Problems are human-readable descriptions, one per issue.
	Problems []string
}

// Error implements error.
func (e *ValidationError) Error() string {
	return "model: invalid: " + strings.Join(e.Problems, "; ")
}

type validator struct{ problems []string }

func (v *validator) addf(path, format string, args ...any) {
	v.problems = append(v.problems, path+": "+fmt.Sprintf(format, args...))
}

func (v *validator) err() error {
	if len(v.problems) == 0 {
		return nil
	}
	return &ValidationError{Problems: v.problems}
}

// Validate checks structural invariants the store relies on: every node has
// an ID, IDs are unique within a collection, enums hold known values, and each
// item is a *Folder or *Request. It returns a *ValidationError or nil.
func (c *Collection) Validate() error {
	v := &validator{}
	c.validate(v)
	return v.err()
}

func (c *Collection) validate(v *validator) {
	path := fmt.Sprintf("collection %q", c.Name)
	if c.ID == "" {
		v.addf(path, "missing id")
	}
	if strings.TrimSpace(c.Name) == "" {
		v.addf(path, "missing name")
	}
	validateAuth(v, path, c.Auth)
	validateVars(v, path, c.Variables)
	seen := map[ID]bool{c.ID: true}
	validateItems(v, path, c.Items, seen)
}

func validateItems(v *validator, parent string, items []Item, seen map[ID]bool) {
	for i, it := range items {
		switch n := it.(type) {
		case *Folder:
			if n == nil {
				v.addf(parent, "item %d is a nil folder", i)
				continue
			}
			path := parent + fmt.Sprintf(" > folder %q", n.Name)
			checkID(v, path, n.ID, seen)
			if strings.TrimSpace(n.Name) == "" {
				v.addf(path, "missing name")
			}
			validateAuth(v, path, n.Auth)
			validateVars(v, path, n.Variables)
			validateItems(v, path, n.Items, seen)
		case *Request:
			if n == nil {
				v.addf(parent, "item %d is a nil request", i)
				continue
			}
			path := parent + fmt.Sprintf(" > request %q", n.Name)
			checkID(v, path, n.ID, seen)
			n.validate(v, path)
		default:
			v.addf(parent, "item %d has unsupported type %T", i, it)
		}
	}
}

func checkID(v *validator, path string, id ID, seen map[ID]bool) {
	switch {
	case id == "":
		v.addf(path, "missing id")
	case seen[id]:
		v.addf(path, "duplicate id %q", id)
	default:
		seen[id] = true
	}
}

// Validate checks a single request in isolation (Kind, body mode, auth type).
func (r *Request) Validate() error {
	v := &validator{}
	r.validate(v, fmt.Sprintf("request %q", r.Name))
	if r.ID == "" {
		v.addf(fmt.Sprintf("request %q", r.Name), "missing id")
	}
	return v.err()
}

func (r *Request) validate(v *validator, path string) {
	if strings.TrimSpace(r.Name) == "" {
		v.addf(path, "missing name")
	}
	switch r.Kind {
	case KindHTTP, KindWebSocket, KindGRPC:
	case "":
		v.addf(path, "missing kind (use model.KindHTTP)")
	default:
		v.addf(path, "unknown kind %q", r.Kind)
	}
	switch r.Body.Mode {
	case "", BodyNone, BodyRaw, BodyURLEncoded, BodyFormData, BodyBinary, BodyGraphQL:
	default:
		v.addf(path, "unknown body mode %q", r.Body.Mode)
	}
	for _, p := range r.Body.FormData {
		switch p.Type {
		case "", FormText, FormFile:
		default:
			v.addf(path, "form part %q has unknown type %q", p.Key, p.Type)
		}
	}
	validateAuth(v, path, r.Auth)
}

func validateAuth(v *validator, path string, a *Auth) {
	if a == nil {
		return
	}
	switch a.Type {
	case "", AuthInherit, AuthNone, AuthBearer, AuthBasic, AuthAPIKey, AuthDigest,
		AuthOAuth2, AuthAWSV4, AuthHawk, AuthNTLM, AuthUnsupported:
	default:
		v.addf(path, "unknown auth type %q", a.Type)
	}
	if a.Type == AuthOAuth2 && a.OAuth2 != nil {
		switch a.OAuth2.GrantType {
		case GrantClientCredentials, GrantAuthorizationCode, GrantAuthorizationCodePKCE,
			GrantPassword, GrantImplicit:
		default:
			v.addf(path, "unknown oauth2 grant %q", a.OAuth2.GrantType)
		}
	}
}

func validateVars(v *validator, path string, vars []Variable) {
	for _, x := range vars {
		switch x.Type {
		case "", VarDefault, VarSecret:
		default:
			v.addf(path, "variable %q has unknown type %q", x.Key, x.Type)
		}
	}
}

// Validate checks an environment's ID, name and variable types.
func (e *Environment) Validate() error {
	v := &validator{}
	path := fmt.Sprintf("environment %q", e.Name)
	if e.ID == "" {
		v.addf(path, "missing id")
	}
	if strings.TrimSpace(e.Name) == "" {
		v.addf(path, "missing name")
	}
	validateVars(v, path, e.Variables)
	return v.err()
}

// Validate checks the globals' ID and variable types.
func (g *Globals) Validate() error {
	v := &validator{}
	if g.ID == "" {
		v.addf("globals", "missing id")
	}
	validateVars(v, "globals", g.Variables)
	return v.err()
}

// Validate checks the whole workspace: its own ID/name, every collection,
// environment and globals, and that collection and environment IDs are unique.
func (w *Workspace) Validate() error {
	v := &validator{}
	if w.ID == "" {
		v.addf("workspace", "missing id")
	}
	seen := map[ID]bool{}
	for _, c := range w.Collections {
		if c == nil {
			v.addf("workspace", "nil collection")
			continue
		}
		if seen[c.ID] {
			v.addf(fmt.Sprintf("collection %q", c.Name), "duplicate id %q", c.ID)
		}
		seen[c.ID] = true
		c.validate(v)
	}
	seenEnv := map[ID]bool{}
	for _, e := range w.Environments {
		if e == nil {
			v.addf("workspace", "nil environment")
			continue
		}
		if seenEnv[e.ID] {
			v.addf(fmt.Sprintf("environment %q", e.Name), "duplicate id %q", e.ID)
		}
		seenEnv[e.ID] = true
		if err := e.Validate(); err != nil {
			var ve *ValidationError
			if errors.As(err, &ve) {
				v.problems = append(v.problems, ve.Problems...)
			}
		}
	}
	if w.Globals != nil {
		if err := w.Globals.Validate(); err != nil {
			var ve *ValidationError
			if errors.As(err, &ve) {
				v.problems = append(v.problems, ve.Problems...)
			}
		}
	}
	return v.err()
}

// EnsureIDs assigns a fresh [NewID] to every node with an empty ID. It is a
// convenience for code that builds models by hand; imported nodes should keep
// their source IDs.
func (w *Workspace) EnsureIDs() {
	if w.ID == "" {
		w.ID = NewID()
	}
	for _, c := range w.Collections {
		c.EnsureIDs()
	}
	for _, e := range w.Environments {
		if e.ID == "" {
			e.ID = NewID()
		}
	}
	if w.Globals != nil && w.Globals.ID == "" {
		w.Globals.ID = NewID()
	}
}

// EnsureIDs assigns a fresh [NewID] to the collection and every descendant
// with an empty ID.
func (c *Collection) EnsureIDs() {
	if c.ID == "" {
		c.ID = NewID()
	}
	_ = c.Walk(func(it Item, _ Container, _ int) error {
		switch n := it.(type) {
		case *Folder:
			if n.ID == "" {
				n.ID = NewID()
			}
		case *Request:
			if n.ID == "" {
				n.ID = NewID()
			}
		}
		return nil
	})
}
