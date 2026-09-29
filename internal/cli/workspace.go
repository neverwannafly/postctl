package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/shubham/postctl/internal/engine"
	"github.com/shubham/postctl/internal/history"
	"github.com/shubham/postctl/internal/httpx"
	"github.com/shubham/postctl/internal/model"
	"github.com/shubham/postctl/internal/oauth2"
	"github.com/shubham/postctl/internal/store"
)

// open loads the workspace strictly (plan §0 D3: the CLI stays strict).
func (a *app) open() int {
	if a.ws != nil {
		return -1
	}
	st := a.opts.Store
	if st == nil {
		cwd, err := os.Getwd()
		if err != nil {
			fmt.Fprintf(a.err, "Cannot read the current directory: %v. Pass --workspace DIR.\n", err)
			return ExitUsage
		}
		loc, err := store.Discover(cwd, store.WithWorkspace(a.opts.Workspace))
		if err != nil {
			fmt.Fprintf(a.err, "Cannot find a workspace: %v. Pass --workspace DIR.\n", err)
			return ExitUsage
		}
		if !loc.Initialized {
			fmt.Fprintf(a.err, "No workspace at %s. Import a collection with: postctl import postman <file>\n", loc.Root)
			return ExitUsage
		}
		root, err := filepath.Abs(loc.Root)
		if err != nil {
			fmt.Fprintf(a.err, "Invalid workspace path: %v\n", err)
			return ExitUsage
		}
		st, err = store.Open(root, store.WithSecretStore(store.NewKeychain("")))
		if err != nil {
			fmt.Fprintf(a.err, "Cannot open the workspace: %v\n", err)
			return ExitUsage
		}
	}
	ws, err := st.Load()
	if err != nil {
		fmt.Fprintf(a.err, "Cannot load the workspace: %v\n", err)
		return ExitUsage
	}
	state, err := st.LoadState()
	if err != nil {
		fmt.Fprintf(a.err, "Cannot read workspace state: %v\n", err)
		return ExitUsage
	}
	a.store = st
	a.ws = ws
	a.state = state
	a.red = history.NewRedactor(secretValues(context.Background(), st, ws))
	return -1
}

// secretValues hydrates copies of secret variables so output can be masked.
// The loaded workspace is left unchanged.
func secretValues(ctx context.Context, st *store.Store, ws *model.Workspace) []string {
	if st == nil || ws == nil {
		return nil
	}
	var vals []string
	add := func(vars []model.Variable) {
		if len(vars) == 0 {
			return
		}
		cp := make([]model.Variable, len(vars))
		copy(cp, vars)
		_, _ = st.HydrateVariables(ctx, cp)
		for _, v := range cp {
			if v.IsSecret() && v.Value != "" {
				vals = append(vals, v.Value)
			}
		}
	}
	if ws.Globals != nil {
		add(ws.Globals.Variables)
	}
	for _, e := range ws.Environments {
		add(e.Variables)
	}
	addAuth := func(auth *model.Auth) {
		if auth == nil {
			return
		}
		cp := auth.Clone()
		_, _ = st.HydrateAuth(ctx, cp)
		if cp == nil || cp.OAuth2 == nil || cp.OAuth2.Token == nil {
			return
		}
		tok := cp.OAuth2.Token
		for _, s := range []string{tok.AccessToken, tok.RefreshToken, tok.IDToken} {
			if s != "" {
				vals = append(vals, s)
			}
		}
	}
	for _, c := range ws.Collections {
		add(c.Variables)
		addAuth(c.Auth)
		_ = c.Walk(func(it model.Item, _ model.Container, _ int) error {
			switch n := it.(type) {
			case *model.Folder:
				add(n.Variables)
				addAuth(n.Auth)
			case *model.Request:
				add(n.URL.PathVars)
				addAuth(n.Auth)
			}
			return nil
		})
	}
	return vals
}

// newEngine builds an engine the same way cmd/postctl/wire.go does.
func (a *app) newEngine(disableHistory bool) (*engine.Engine, int) {
	secrets := a.store.SecretStore()
	eng, err := engine.New(engine.Config{
		Store:          a.store,
		Secrets:        secrets,
		HTTP:           httpx.NewClient(a.opts.Version),
		OAuth2:         oauth2.NewSource(secrets, oauth2.Fetch),
		Version:        a.opts.Version,
		DisableHistory: disableHistory,
	})
	if err != nil {
		fmt.Fprintf(a.err, "Cannot prepare the HTTP engine: %v\n", err)
		return nil, ExitUsage
	}
	return eng, -1
}

// lookupEnv resolves an environment by name, slug or ID. An empty name uses
// the active environment. A stale active id warns and returns nil.
func (a *app) lookupEnv(name string) (*model.Environment, int) {
	if name == "" {
		if a.state.ActiveEnvironment == "" {
			return nil, -1
		}
		env := a.ws.Environment(a.state.ActiveEnvironment)
		if env == nil {
			a.warn("Active environment %s was not found. Continuing with no environment.", a.state.ActiveEnvironment)
		}
		return env, -1
	}
	var hits []*model.Environment
	for _, e := range a.ws.Environments {
		if nameMatch(e.Name, string(e.ID), name) {
			hits = append(hits, e)
		}
	}
	switch len(hits) {
	case 0:
		fmt.Fprintf(a.err, "No environment matches %q.\n", name)
		return nil, ExitUsage
	case 1:
		return hits[0], -1
	default:
		fmt.Fprintf(a.err, "%q matches more than one environment:\n", name)
		for _, e := range hits {
			fmt.Fprintf(a.err, "  %s\n", e.Name)
		}
		fmt.Fprintln(a.err, "Pass the environment ID.")
		return nil, ExitUsage
	}
}
