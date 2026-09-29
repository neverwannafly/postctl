package vars

import (
	"errors"
	"maps"
	"net/url"
	"sort"
	"strings"

	"github.com/shubham/postctl/internal/model"
)

// ResolveRequest returns a deep copy with inherited auth flattened and all
// request variable references resolved. The input request is not modified.
func ResolveRequest(
	req *model.Request,
	chain []model.Container,
	store Store,
) (*model.Request, error) {
	if req == nil {
		return nil, errors.New("resolve request: nil request")
	}
	if store == nil {
		return nil, errors.New("resolve request: nil variable store")
	}
	resolved := cloneRequest(req)
	resolved.Name = store.Replace(resolved.Name)
	resolved.Method = store.Replace(resolved.Method)
	resolved.URL.Raw = store.Replace(resolved.URL.Raw)
	for i := range resolved.URL.Query {
		resolved.URL.Query[i].Key = store.Replace(resolved.URL.Query[i].Key)
		resolved.URL.Query[i].Value = store.Replace(resolved.URL.Query[i].Value)
	}
	pathVars := append([]model.Variable(nil), resolved.URL.PathVars...)
	sort.SliceStable(pathVars, func(i, j int) bool {
		return len(pathVars[i].Key) > len(pathVars[j].Key)
	})
	for i := range resolved.URL.PathVars {
		resolved.URL.PathVars[i].Key = store.Replace(resolved.URL.PathVars[i].Key)
		resolved.URL.PathVars[i].Value = store.Replace(resolved.URL.PathVars[i].Value)
	}
	for _, variable := range pathVars {
		if variable.Disabled {
			continue
		}
		key := store.Replace(variable.Key)
		value := store.Replace(variable.Value)
		resolved.URL.Raw = strings.ReplaceAll(resolved.URL.Raw, ":"+key, url.PathEscape(value))
	}
	for i := range resolved.Headers {
		resolved.Headers[i].Key = store.Replace(resolved.Headers[i].Key)
		resolved.Headers[i].Value = store.Replace(resolved.Headers[i].Value)
	}
	resolveBody(&resolved.Body, store)

	auth := resolved.Auth
	if auth.IsInherit() {
		auth = inheritedAuth(chain)
	}
	if auth == nil || auth.IsInherit() {
		auth = &model.Auth{Type: model.AuthNone}
	}
	resolved.Auth = cloneAuth(auth)
	resolveAuth(resolved.Auth, store)
	return resolved, nil
}

func inheritedAuth(chain []model.Container) *model.Auth {
	for i := len(chain) - 1; i >= 0; i-- {
		var auth *model.Auth
		switch container := chain[i].(type) {
		case *model.Collection:
			auth = container.Auth
		case *model.Folder:
			auth = container.Auth
		}
		if !auth.IsInherit() {
			return auth
		}
	}
	return nil
}

func resolveBody(body *model.Body, store Store) {
	body.Raw = store.Replace(body.Raw)
	body.Language = store.Replace(body.Language)
	for i := range body.URLEncoded {
		body.URLEncoded[i].Key = store.Replace(body.URLEncoded[i].Key)
		body.URLEncoded[i].Value = store.Replace(body.URLEncoded[i].Value)
	}
	for i := range body.FormData {
		body.FormData[i].Key = store.Replace(body.FormData[i].Key)
		body.FormData[i].Value = store.Replace(body.FormData[i].Value)
		body.FormData[i].Src = store.Replace(body.FormData[i].Src)
		body.FormData[i].ContentType = store.Replace(body.FormData[i].ContentType)
	}
	body.File = store.Replace(body.File)
	if body.GraphQL != nil {
		body.GraphQL.Query = store.Replace(body.GraphQL.Query)
		body.GraphQL.Variables = store.Replace(body.GraphQL.Variables)
	}
}

func resolveAuth(auth *model.Auth, store Store) {
	if auth == nil {
		return
	}
	if auth.Bearer != nil {
		auth.Bearer.Token = store.Replace(auth.Bearer.Token)
	}
	if auth.Basic != nil {
		auth.Basic.Username = store.Replace(auth.Basic.Username)
		auth.Basic.Password = store.Replace(auth.Basic.Password)
	}
	if auth.APIKey != nil {
		auth.APIKey.Key = store.Replace(auth.APIKey.Key)
		auth.APIKey.Value = store.Replace(auth.APIKey.Value)
	}
	if auth.Digest != nil {
		auth.Digest.Username = store.Replace(auth.Digest.Username)
		auth.Digest.Password = store.Replace(auth.Digest.Password)
		auth.Digest.Realm = store.Replace(auth.Digest.Realm)
		auth.Digest.Nonce = store.Replace(auth.Digest.Nonce)
		auth.Digest.Algorithm = store.Replace(auth.Digest.Algorithm)
		auth.Digest.QOP = store.Replace(auth.Digest.QOP)
		auth.Digest.NonceCount = store.Replace(auth.Digest.NonceCount)
		auth.Digest.ClientNonce = store.Replace(auth.Digest.ClientNonce)
		auth.Digest.Opaque = store.Replace(auth.Digest.Opaque)
	}
	if auth.OAuth2 != nil {
		resolveOAuth2(auth.OAuth2, store)
	}
	if auth.AWSV4 != nil {
		auth.AWSV4.AccessKey = store.Replace(auth.AWSV4.AccessKey)
		auth.AWSV4.SecretKey = store.Replace(auth.AWSV4.SecretKey)
		auth.AWSV4.Region = store.Replace(auth.AWSV4.Region)
		auth.AWSV4.Service = store.Replace(auth.AWSV4.Service)
		auth.AWSV4.SessionToken = store.Replace(auth.AWSV4.SessionToken)
	}
	if auth.Hawk != nil {
		auth.Hawk.ID = store.Replace(auth.Hawk.ID)
		auth.Hawk.Key = store.Replace(auth.Hawk.Key)
		auth.Hawk.Algorithm = store.Replace(auth.Hawk.Algorithm)
		auth.Hawk.User = store.Replace(auth.Hawk.User)
		auth.Hawk.Nonce = store.Replace(auth.Hawk.Nonce)
		auth.Hawk.ExtraData = store.Replace(auth.Hawk.ExtraData)
		auth.Hawk.AppID = store.Replace(auth.Hawk.AppID)
		auth.Hawk.Delegation = store.Replace(auth.Hawk.Delegation)
		auth.Hawk.Timestamp = store.Replace(auth.Hawk.Timestamp)
	}
	if auth.NTLM != nil {
		auth.NTLM.Username = store.Replace(auth.NTLM.Username)
		auth.NTLM.Password = store.Replace(auth.NTLM.Password)
		auth.NTLM.Domain = store.Replace(auth.NTLM.Domain)
		auth.NTLM.Workstation = store.Replace(auth.NTLM.Workstation)
	}
	for key, value := range auth.Raw {
		auth.Raw[key] = resolveAny(value, store)
	}
}

func resolveOAuth2(config *model.OAuth2Config, store Store) {
	config.TokenName = store.Replace(config.TokenName)
	config.AuthURL = store.Replace(config.AuthURL)
	config.AccessTokenURL = store.Replace(config.AccessTokenURL)
	config.RefreshTokenURL = store.Replace(config.RefreshTokenURL)
	config.ClientID = store.Replace(config.ClientID)
	config.ClientSecret = store.Replace(config.ClientSecret)
	config.Scope = store.Replace(config.Scope)
	config.RedirectURI = store.Replace(config.RedirectURI)
	config.State = store.Replace(config.State)
	config.Audience = store.Replace(config.Audience)
	config.Resource = store.Replace(config.Resource)
	config.Username = store.Replace(config.Username)
	config.Password = store.Replace(config.Password)
	config.CodeChallengeMethod = store.Replace(config.CodeChallengeMethod)
	config.CodeVerifier = store.Replace(config.CodeVerifier)
	config.HeaderPrefix = store.Replace(config.HeaderPrefix)
	if config.Token != nil {
		config.Token.AccessToken = store.Replace(config.Token.AccessToken)
		config.Token.RefreshToken = store.Replace(config.Token.RefreshToken)
		config.Token.IDToken = store.Replace(config.Token.IDToken)
		config.Token.TokenType = store.Replace(config.Token.TokenType)
		config.Token.Scope = store.Replace(config.Token.Scope)
	}
	for key, value := range config.Extra {
		config.Extra[key] = resolveAny(value, store)
	}
}

func resolveAny(value any, store Store) any {
	switch typed := value.(type) {
	case string:
		return store.Replace(typed)
	case []any:
		result := make([]any, len(typed))
		for i := range typed {
			result[i] = resolveAny(typed[i], store)
		}
		return result
	case map[string]any:
		result := make(map[string]any, len(typed))
		for key, nested := range typed {
			result[key] = resolveAny(nested, store)
		}
		return result
	default:
		return value
	}
}

func cloneRequest(req *model.Request) *model.Request {
	if req == nil {
		return nil
	}
	cloned := *req
	cloned.URL.Query = append([]model.KV(nil), req.URL.Query...)
	cloned.URL.PathVars = append([]model.Variable(nil), req.URL.PathVars...)
	cloned.Headers = append([]model.KV(nil), req.Headers...)
	cloned.Body.URLEncoded = append([]model.KV(nil), req.Body.URLEncoded...)
	cloned.Body.FormData = append([]model.FormPart(nil), req.Body.FormData...)
	cloned.Body.Extra = cloneMap(req.Body.Extra)
	if req.Body.GraphQL != nil {
		graphql := *req.Body.GraphQL
		cloned.Body.GraphQL = &graphql
	}
	cloned.Auth = cloneAuth(req.Auth)
	cloned.Scripts = cloneScripts(req.Scripts)
	cloned.Settings.Extra = cloneMap(req.Settings.Extra)
	cloned.Extra = cloneMap(req.Extra)
	cloned.Examples = make([]model.Example, len(req.Examples))
	for i := range req.Examples {
		cloned.Examples[i] = req.Examples[i]
		cloned.Examples[i].Headers = append([]model.KV(nil), req.Examples[i].Headers...)
		cloned.Examples[i].Cookies = append([]model.Cookie(nil), req.Examples[i].Cookies...)
		cloned.Examples[i].Extra = cloneMap(req.Examples[i].Extra)
		cloned.Examples[i].OriginalRequest = cloneRequest(req.Examples[i].OriginalRequest)
	}
	return &cloned
}

func cloneScripts(scripts model.Scripts) model.Scripts {
	result := scripts
	if scripts.PreRequest != nil {
		pre := *scripts.PreRequest
		result.PreRequest = &pre
	}
	if scripts.Test != nil {
		test := *scripts.Test
		result.Test = &test
	}
	return result
}

func cloneAuth(auth *model.Auth) *model.Auth {
	if auth == nil {
		return nil
	}
	cloned := *auth
	if auth.Bearer != nil {
		value := *auth.Bearer
		cloned.Bearer = &value
	}
	if auth.Basic != nil {
		value := *auth.Basic
		cloned.Basic = &value
	}
	if auth.APIKey != nil {
		value := *auth.APIKey
		cloned.APIKey = &value
	}
	if auth.Digest != nil {
		value := *auth.Digest
		cloned.Digest = &value
	}
	if auth.OAuth2 != nil {
		value := *auth.OAuth2
		value.Extra = cloneMap(auth.OAuth2.Extra)
		if auth.OAuth2.Token != nil {
			token := *auth.OAuth2.Token
			value.Token = &token
		}
		cloned.OAuth2 = &value
	}
	if auth.AWSV4 != nil {
		value := *auth.AWSV4
		cloned.AWSV4 = &value
	}
	if auth.Hawk != nil {
		value := *auth.Hawk
		cloned.Hawk = &value
	}
	if auth.NTLM != nil {
		value := *auth.NTLM
		cloned.NTLM = &value
	}
	cloned.Raw = cloneMap(auth.Raw)
	return &cloned
}

func cloneMap(source map[string]any) map[string]any {
	if source == nil {
		return nil
	}
	result := maps.Clone(source)
	for key, value := range result {
		switch typed := value.(type) {
		case map[string]any:
			result[key] = cloneMap(typed)
		case []any:
			result[key] = cloneSlice(typed)
		}
	}
	return result
}

func cloneSlice(source []any) []any {
	result := make([]any, len(source))
	for i, value := range source {
		switch typed := value.(type) {
		case map[string]any:
			result[i] = cloneMap(typed)
		case []any:
			result[i] = cloneSlice(typed)
		default:
			result[i] = value
		}
	}
	return result
}
