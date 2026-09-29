package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/shubham/postctl/internal/model"
)

func runEnv(ctx context.Context, a *app, args []string) int {
	fs := a.flagSet()
	asJSON := fs.Bool("json", false, "print JSON")
	reveal := fs.Bool("reveal", false, "show secret values (asks when stdin is a terminal)")
	secret := fs.Bool("secret", false, "store the value as a secret")
	positional, code := a.parse(fs, args)
	if code >= 0 {
		return code
	}
	if len(positional) == 0 {
		fmt.Fprintf(a.err, "Usage: postctl %s\n", a.cmd.usage)
		return ExitUsage
	}
	if code := a.open(); code >= 0 {
		return code
	}
	switch positional[0] {
	case "ls":
		if len(positional) != 1 {
			fmt.Fprintln(a.err, "Usage: postctl env ls [--json]")
			return ExitUsage
		}
		return envList(a, *asJSON)
	case "use":
		if len(positional) != 2 {
			fmt.Fprintln(a.err, "Usage: postctl env use <name>")
			return ExitUsage
		}
		return envUse(a, positional[1])
	case "show", "get":
		if len(positional) != 2 {
			fmt.Fprintln(a.err, "Usage: postctl env show <name> [--reveal] [--json]")
			return ExitUsage
		}
		return envShow(ctx, a, positional[1], *reveal, *asJSON)
	case "set":
		if len(positional) != 4 {
			fmt.Fprintln(a.err, "Usage: postctl env set <name> <key> <value> [--secret]")
			return ExitUsage
		}
		return envSet(ctx, a, positional[1], positional[2], positional[3], *secret)
	case "unset":
		if len(positional) != 3 {
			fmt.Fprintln(a.err, "Usage: postctl env unset <name> <key>")
			return ExitUsage
		}
		return envUnset(ctx, a, positional[1], positional[2])
	default:
		fmt.Fprintf(a.err, "Unknown env command %q. Use ls, use, show, set or unset.\n", positional[0])
		return ExitUsage
	}
}

func envList(a *app, asJSON bool) int {
	active := a.ws.Environment(a.state.ActiveEnvironment)
	if asJSON {
		doc := envListDoc{ActiveID: string(a.state.ActiveEnvironment)}
		if active != nil {
			doc.Active = active.Name
		}
		for _, e := range a.ws.Environments {
			doc.Environments = append(doc.Environments, envSummary{
				ID: string(e.ID), Name: e.Name, Production: e.Production,
				Active: active != nil && e.ID == active.ID, Variables: len(e.Variables),
			})
		}
		return writeJSON(a, doc)
	}
	if len(a.ws.Environments) == 0 {
		fmt.Fprintln(a.out, "No environments yet.")
		return ExitOK
	}
	for _, e := range a.ws.Environments {
		mark := " "
		if active != nil && e.ID == active.ID {
			mark = "*"
		}
		line := fmt.Sprintf("%s %s", mark, e.Name)
		if e.Production {
			line += "  " + a.view.warn() + " prod"
		}
		fmt.Fprintln(a.out, line)
	}
	return ExitOK
}

func envUse(a *app, name string) int {
	env, code := a.lookupEnv(name)
	if code >= 0 {
		return code
	}
	if env == nil {
		fmt.Fprintln(a.err, "Pass an environment name.")
		return ExitUsage
	}
	a.state.ActiveEnvironment = env.ID
	if err := a.store.SaveState(a.state); err != nil {
		fmt.Fprintf(a.err, "Cannot save the active environment: %v\n", err)
		return ExitUsage
	}
	line := "Using " + env.Name + "."
	if env.Production {
		line += "  " + a.view.warn() + " prod"
	}
	fmt.Fprintln(a.out, line)
	return ExitOK
}

func envShow(ctx context.Context, a *app, name string, reveal, asJSON bool) int {
	env, code := a.lookupEnv(name)
	if code >= 0 {
		return code
	}
	if env == nil {
		fmt.Fprintln(a.err, "Pass an environment name.")
		return ExitUsage
	}
	vars := append([]model.Variable(nil), env.Variables...)
	if reveal {
		ok, err := confirmReveal(a, env.Name)
		if err != nil {
			fmt.Fprintf(a.err, "Cannot read the confirmation: %v\n", err)
			return ExitUsage
		}
		if !ok {
			fmt.Fprintln(a.err, "Canceled. Secrets stay hidden.")
			return ExitUsage
		}
		if _, err := a.store.HydrateVariables(ctx, vars); err != nil {
			fmt.Fprintf(a.err, "Cannot read secret values: %v\n", err)
			return ExitUsage
		}
	}
	if asJSON {
		doc := envShowDoc{
			ID: string(env.ID), Name: env.Name, Production: env.Production,
			Active: env.ID == a.state.ActiveEnvironment,
		}
		for _, v := range vars {
			doc.Variables = append(doc.Variables, envVarDoc{
				Key: v.Key, Value: displayValue(a, v, reveal), Secret: v.IsSecret(), Disabled: v.Disabled,
			})
		}
		return writeJSON(a, doc)
	}
	fmt.Fprintln(a.out, env.Name)
	if len(vars) == 0 {
		fmt.Fprintln(a.out, "  No variables.")
		return ExitOK
	}
	width := 0
	for _, v := range vars {
		if len(v.Key) > width {
			width = len(v.Key)
		}
	}
	for _, v := range vars {
		val := displayValue(a, v, reveal)
		extra := ""
		if v.IsSecret() {
			extra = "  secret"
		}
		if v.Disabled {
			extra += "  disabled"
		}
		fmt.Fprintf(a.out, "  %-*s  %s%s\n", width, v.Key, val, extra)
	}
	return ExitOK
}

func displayValue(a *app, v model.Variable, reveal bool) string {
	if v.IsSecret() && !reveal {
		return a.view.mask()
	}
	if v.IsSecret() {
		return v.Value
	}
	return a.mask(v.Value)
}

func confirmReveal(a *app, name string) (bool, error) {
	if !a.opts.tty(a.in) {
		return true, nil
	}
	fmt.Fprintf(a.err, "Reveal secret values in %q? Type y to confirm: ", name)
	line, err := bufio.NewReader(a.in).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return false, err
	}
	line = strings.TrimSpace(line)
	return line == "y" || line == "Y" || strings.EqualFold(line, "yes"), nil
}

func envSet(ctx context.Context, a *app, name, key, value string, secret bool) int {
	env, code := a.lookupEnv(name)
	if code >= 0 {
		return code
	}
	if env == nil {
		fmt.Fprintln(a.err, "Pass an environment name.")
		return ExitUsage
	}
	if strings.TrimSpace(key) == "" {
		fmt.Fprintln(a.err, "The variable key is empty.")
		return ExitUsage
	}
	found := false
	for i := range env.Variables {
		if env.Variables[i].Key == key {
			env.Variables[i].Value = value
			env.Variables[i].Disabled = false
			if secret {
				env.Variables[i].Type = model.VarSecret
			}
			found = true
			break
		}
	}
	if !found {
		v := model.Variable{Key: key, Value: value}
		if secret {
			v.Type = model.VarSecret
		}
		env.Variables = append(env.Variables, v)
	}
	if err := a.store.SaveEnvironment(ctx, env); err != nil {
		fmt.Fprintf(a.err, "Cannot save the environment: %v\n", err)
		return ExitUsage
	}
	if secret {
		fmt.Fprintf(a.out, "Set secret %s on %s.\n", key, env.Name)
	} else {
		fmt.Fprintf(a.out, "Set %s on %s.\n", key, env.Name)
	}
	return ExitOK
}

func envUnset(ctx context.Context, a *app, name, key string) int {
	env, code := a.lookupEnv(name)
	if code >= 0 {
		return code
	}
	if env == nil {
		fmt.Fprintln(a.err, "Pass an environment name.")
		return ExitUsage
	}
	next := env.Variables[:0]
	removed := false
	for _, v := range env.Variables {
		if v.Key == key {
			removed = true
			continue
		}
		next = append(next, v)
	}
	if !removed {
		fmt.Fprintf(a.err, "No variable %q on %s.\n", key, env.Name)
		return ExitUsage
	}
	env.Variables = next
	if err := a.store.SaveEnvironment(ctx, env); err != nil {
		fmt.Fprintf(a.err, "Cannot save the environment: %v\n", err)
		return ExitUsage
	}
	fmt.Fprintf(a.out, "Unset %s on %s.\n", key, env.Name)
	return ExitOK
}

type envListDoc struct {
	ActiveID     string       `json:"activeId,omitempty"`
	Active       string       `json:"active,omitempty"`
	Environments []envSummary `json:"environments"`
}

type envSummary struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Production bool   `json:"production"`
	Active     bool   `json:"active"`
	Variables  int    `json:"variables"`
}

type envShowDoc struct {
	ID         string      `json:"id"`
	Name       string      `json:"name"`
	Production bool        `json:"production"`
	Active     bool        `json:"active"`
	Variables  []envVarDoc `json:"variables"`
}

type envVarDoc struct {
	Key      string `json:"key"`
	Value    string `json:"value"`
	Secret   bool   `json:"secret,omitempty"`
	Disabled bool   `json:"disabled,omitempty"`
}

func writeJSON(a *app, v any) int {
	enc := json.NewEncoder(a.out)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		fmt.Fprintf(a.err, "Cannot write JSON: %v\n", err)
		return ExitRequest
	}
	return ExitOK
}
