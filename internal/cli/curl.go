package cli

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/shubham/postctl/internal/history"
	"github.com/shubham/postctl/internal/httpx"
	"github.com/shubham/postctl/internal/model"
	"github.com/shubham/postctl/internal/vars"
)

func runCurl(ctx context.Context, a *app, args []string) int {
	fs := a.flagSet()
	envName := fs.String("e", "", "environment name or id (default: the active environment)")
	fs.StringVar(envName, "env", "", "environment name or id")
	withSecrets := fs.Bool("with-secrets", false, "print secret values instead of masking them")
	positional, code := a.parse(fs, args)
	if code >= 0 {
		return code
	}
	path, code := a.onePath(positional)
	if code >= 0 {
		return code
	}
	if code := a.open(); code >= 0 {
		return code
	}
	n, err := resolvePath(a.ws, path)
	if err != nil {
		fmt.Fprintln(a.err, err.Error())
		return ExitUsage
	}
	if n.request() == nil {
		fmt.Fprintf(a.err, "%q is a %s. postctl curl takes a request.\n", path, n.kind())
		return ExitUsage
	}
	env, code := a.lookupEnv(*envName)
	if code >= 0 {
		return code
	}
	resolved, err := a.resolveRequest(ctx, n, env)
	if err != nil {
		fmt.Fprintf(a.err, "Cannot resolve the request: %v\n", a.mask(err.Error()))
		return ExitUsage
	}
	if !*withSecrets {
		maskRequest(a, resolved)
	}
	fmt.Fprintln(a.out, a.view.swapMask(curlCommand(resolved)))
	return ExitOK
}

func (a *app) resolveRequest(ctx context.Context, n node, env *model.Environment) (*model.Request, error) {
	var globals, collection, environment []model.Variable
	if a.ws.Globals != nil {
		globals = hydrateVars(ctx, a.store, a.ws.Globals.Variables)
	}
	collection = hydrateVars(ctx, a.store, n.col.Variables)
	if env != nil {
		environment = hydrateVars(ctx, a.store, env.Variables)
	}
	store := vars.New(globals, collection, environment, nil)
	resolved, err := vars.ResolveRequest(n.request(), n.containers(), store)
	if err != nil {
		return nil, err
	}
	if resolved.Auth != nil {
		_, _ = a.store.HydrateAuth(ctx, resolved.Auth)
	}
	return resolved, nil
}

func hydrateVars(ctx context.Context, st interface {
	HydrateVariables(context.Context, []model.Variable) ([]model.SecretRef, error)
}, vars []model.Variable) []model.Variable {
	if len(vars) == 0 || st == nil {
		return nil
	}
	cp := append([]model.Variable(nil), vars...)
	_, _ = st.HydrateVariables(ctx, cp)
	return cp
}

func maskRequest(a *app, req *model.Request) {
	req.URL.Raw = a.red.URL(req.URL.Raw)
	req.Headers = a.red.Headers(req.Headers)
	req.Body.Raw = a.red.Body(history.ContentType(req.Headers), req.Body.Raw)
	for i := range req.Body.URLEncoded {
		if a.red.Sensitive(req.Body.URLEncoded[i].Key) && req.Body.URLEncoded[i].Value != "" {
			req.Body.URLEncoded[i].Value = history.Mask
		} else {
			req.Body.URLEncoded[i].Value = a.red.Text(req.Body.URLEncoded[i].Value)
		}
	}
	for i := range req.Body.FormData {
		part := &req.Body.FormData[i]
		if a.red.Sensitive(part.Key) && part.Value != "" {
			part.Value = history.Mask
		} else {
			part.Value = a.red.Text(part.Value)
		}
	}
	if req.Body.GraphQL != nil {
		req.Body.GraphQL.Query = a.red.Text(req.Body.GraphQL.Query)
		req.Body.GraphQL.Variables = a.red.Body("application/json", req.Body.GraphQL.Variables)
	}
	maskAuth(req.Auth)
	req.URL.Raw = a.red.Text(req.URL.Raw)
}

func maskAuth(auth *model.Auth) {
	if auth == nil {
		return
	}
	m := history.Mask
	blank := func(s string) string {
		if s == "" {
			return s
		}
		return m
	}
	if auth.Bearer != nil {
		auth.Bearer.Token = blank(auth.Bearer.Token)
	}
	if auth.Basic != nil {
		auth.Basic.Password = blank(auth.Basic.Password)
	}
	if auth.Digest != nil {
		auth.Digest.Password = blank(auth.Digest.Password)
	}
	if auth.NTLM != nil {
		auth.NTLM.Password = blank(auth.NTLM.Password)
	}
	if auth.APIKey != nil {
		auth.APIKey.Value = blank(auth.APIKey.Value)
	}
	if auth.AWSV4 != nil {
		auth.AWSV4.SecretKey = blank(auth.AWSV4.SecretKey)
		auth.AWSV4.SessionToken = blank(auth.AWSV4.SessionToken)
	}
	if auth.Hawk != nil {
		auth.Hawk.Key = blank(auth.Hawk.Key)
	}
	if auth.OAuth2 != nil {
		auth.OAuth2.ClientSecret = blank(auth.OAuth2.ClientSecret)
		auth.OAuth2.Password = blank(auth.OAuth2.Password)
		if auth.OAuth2.Token != nil {
			auth.OAuth2.Token.AccessToken = blank(auth.OAuth2.Token.AccessToken)
			auth.OAuth2.Token.RefreshToken = blank(auth.OAuth2.Token.RefreshToken)
			auth.OAuth2.Token.IDToken = blank(auth.OAuth2.Token.IDToken)
		}
	}
}

func curlCommand(req *model.Request) string {
	if req == nil {
		return "curl"
	}
	args, query := authArgs(req.Auth)
	if len(query) == 2 && query[0] != "" {
		req.URL.Raw = appendQuery(req.URL.Raw, query[0], query[1])
	}
	out := httpx.Curl(req)
	if len(args) > 0 {
		out += " " + strings.Join(args, " ")
	}
	return out
}

func authArgs(auth *model.Auth) (args []string, query []string) {
	if auth == nil {
		return nil, nil
	}
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }
	user := func(name, password string) string { return quote(name + ":" + password) }
	switch auth.Type {
	case model.AuthBearer:
		if auth.Bearer != nil {
			args = append(args, "--header", quote("Authorization: Bearer "+auth.Bearer.Token))
		}
	case model.AuthBasic:
		if auth.Basic != nil {
			args = append(args, "--user", user(auth.Basic.Username, auth.Basic.Password))
		}
	case model.AuthDigest:
		if auth.Digest != nil {
			args = append(args, "--digest", "--user", user(auth.Digest.Username, auth.Digest.Password))
		}
	case model.AuthNTLM:
		if auth.NTLM != nil {
			args = append(args, "--ntlm", "--user", user(auth.NTLM.Username, auth.NTLM.Password))
		}
	case model.AuthAPIKey:
		if auth.APIKey != nil && auth.APIKey.Key != "" {
			if auth.APIKey.In == model.APIKeyQuery {
				query = []string{auth.APIKey.Key, auth.APIKey.Value}
			} else {
				args = append(args, "--header", quote(auth.APIKey.Key+": "+auth.APIKey.Value))
			}
		}
	case model.AuthOAuth2:
		if auth.OAuth2 != nil && auth.OAuth2.Token != nil && auth.OAuth2.Token.AccessToken != "" {
			prefix := auth.OAuth2.HeaderPrefix
			if prefix == "" {
				prefix = "Bearer"
			}
			tok := auth.OAuth2.Token.AccessToken
			if auth.OAuth2.AddTokenTo == model.TokenInQuery {
				query = []string{"access_token", tok}
			} else {
				args = append(args, "--header", quote("Authorization: "+prefix+" "+tok))
			}
		}
	case model.AuthAWSV4:
		if auth.AWSV4 != nil {
			args = append(args,
				"--aws-sigv4", quote("aws:amz:"+auth.AWSV4.Region+":"+auth.AWSV4.Service),
				"--user", user(auth.AWSV4.AccessKey, auth.AWSV4.SecretKey))
		}
	}
	return args, query
}

func appendQuery(raw, key, value string) string {
	sep := "?"
	if strings.Contains(raw, "?") {
		sep = "&"
	}
	return raw + sep + url.QueryEscape(key) + "=" + url.QueryEscape(value)
}
