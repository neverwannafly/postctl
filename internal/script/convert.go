package script

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/shubham/postctl/internal/httpx"
	"github.com/shubham/postctl/internal/model"
)

// The sandbox speaks Postman collection v2.1 definitions, the same shape the
// importer reads, so pm.request and pm.response look to a script exactly as
// they do in Postman. These types are that wire format.

type kvDef struct {
	Key         string `json:"key"`
	Value       any    `json:"value,omitempty"`
	Description string `json:"description,omitempty"`
	Disabled    bool   `json:"disabled,omitempty"`
	Type        string `json:"type,omitempty"`
}

func (k kvDef) str() string { return toString(k.Value) }

type formDef struct {
	Key         string `json:"key"`
	Value       any    `json:"value,omitempty"`
	Type        string `json:"type,omitempty"`
	Src         any    `json:"src,omitempty"`
	ContentType string `json:"contentType,omitempty"`
	Description string `json:"description,omitempty"`
	Disabled    bool   `json:"disabled,omitempty"`
}

type urlDef struct {
	Raw      string  `json:"raw,omitempty"`
	Query    []kvDef `json:"query,omitempty"`
	Variable []kvDef `json:"variable,omitempty"`
}

type rawOptions struct {
	Language string `json:"language,omitempty"`
}

type bodyOptions struct {
	Raw *rawOptions `json:"raw,omitempty"`
}

type graphQLDef struct {
	Query     string `json:"query,omitempty"`
	Variables string `json:"variables,omitempty"`
}

type fileDef struct {
	Src string `json:"src,omitempty"`
}

type bodyDef struct {
	Mode       string       `json:"mode,omitempty"`
	Raw        string       `json:"raw,omitempty"`
	URLEncoded []kvDef      `json:"urlencoded,omitempty"`
	FormData   []formDef    `json:"formdata,omitempty"`
	File       *fileDef     `json:"file,omitempty"`
	GraphQL    *graphQLDef  `json:"graphql,omitempty"`
	Options    *bodyOptions `json:"options,omitempty"`
	Disabled   bool         `json:"disabled,omitempty"`
}

type authDef struct {
	Type   string             `json:"type,omitempty"`
	Params map[string][]kvDef `json:"-"`
}

// MarshalJSON writes Postman's {"type":"bearer","bearer":[{key,value}]} shape.
func (a authDef) MarshalJSON() ([]byte, error) {
	out := map[string]any{}
	if a.Type != "" {
		out["type"] = a.Type
	}
	for k, v := range a.Params {
		out[k] = v
	}
	return json.Marshal(out)
}

// UnmarshalJSON reads back whatever parameter list matches "type".
func (a *authDef) UnmarshalJSON(b []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	a.Params = map[string][]kvDef{}
	for k, v := range raw {
		if k == "type" {
			_ = json.Unmarshal(v, &a.Type)
			continue
		}
		var list []kvDef
		if err := json.Unmarshal(v, &list); err == nil {
			a.Params[k] = list
		}
	}
	return nil
}

func (a *authDef) get(key string) string {
	if a == nil {
		return ""
	}
	for _, p := range a.Params[a.Type] {
		if p.Key == key {
			return p.str()
		}
	}
	return ""
}

type requestDef struct {
	ID          string   `json:"id,omitempty"`
	Name        string   `json:"name,omitempty"`
	Method      string   `json:"method,omitempty"`
	URL         *urlDef  `json:"url,omitempty"`
	Header      []kvDef  `json:"header,omitempty"`
	Body        *bodyDef `json:"body,omitempty"`
	Auth        *authDef `json:"auth,omitempty"`
	Description string   `json:"description,omitempty"`
}

type responseDef struct {
	Code         int     `json:"code"`
	Status       string  `json:"status,omitempty"`
	Header       []kvDef `json:"header,omitempty"`
	Cookie       []any   `json:"cookie,omitempty"`
	Body         string  `json:"body"`
	ResponseTime float64 `json:"responseTime"`
	ResponseSize int64   `json:"responseSize"`
	HeaderSize   int64   `json:"headerSize"`
}

func toString(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case bool:
		return strconv.FormatBool(t)
	case float64:
		if t == float64(int64(t)) {
			return strconv.FormatInt(int64(t), 10)
		}
		return strconv.FormatFloat(t, 'f', -1, 64)
	case json.Number:
		return t.String()
	default:
		b, err := json.Marshal(t)
		if err != nil {
			return ""
		}
		return string(b)
	}
}

// requestToDef renders a request the way pm.request should see it.
func requestToDef(r *model.Request) requestDef {
	if r == nil {
		return requestDef{Method: "GET", URL: &urlDef{}}
	}
	d := requestDef{
		ID:          string(r.ID),
		Name:        r.Name,
		Method:      r.Method,
		Description: r.Description,
		URL:         &urlDef{Raw: r.URL.Raw},
	}
	for _, q := range r.URL.Query {
		d.URL.Query = append(d.URL.Query, kvDef{Key: q.Key, Value: q.Value, Description: q.Description, Disabled: q.Disabled})
	}
	for _, p := range r.URL.PathVars {
		d.URL.Variable = append(d.URL.Variable, kvDef{Key: p.Key, Value: p.Value, Description: p.Description, Disabled: p.Disabled})
	}
	for _, h := range r.Headers {
		d.Header = append(d.Header, kvDef{Key: h.Key, Value: h.Value, Description: h.Description, Disabled: h.Disabled})
	}
	if b := bodyToDef(r.Body); b != nil {
		d.Body = b
	}
	if a := authToDef(r.Auth); a != nil {
		d.Auth = a
	}
	return d
}

func bodyToDef(b model.Body) *bodyDef {
	if b.Mode == "" || b.Mode == model.BodyNone {
		return nil
	}
	d := &bodyDef{Disabled: b.Disabled}
	switch b.Mode {
	case model.BodyRaw:
		d.Mode, d.Raw = "raw", b.Raw
		if b.Language != "" {
			d.Options = &bodyOptions{Raw: &rawOptions{Language: b.Language}}
		}
	case model.BodyURLEncoded:
		d.Mode = "urlencoded"
		for _, kv := range b.URLEncoded {
			d.URLEncoded = append(d.URLEncoded, kvDef{Key: kv.Key, Value: kv.Value, Description: kv.Description, Disabled: kv.Disabled})
		}
	case model.BodyFormData:
		d.Mode = "formdata"
		for _, p := range b.FormData {
			f := formDef{Key: p.Key, Type: string(p.Type), ContentType: p.ContentType, Description: p.Description, Disabled: p.Disabled}
			if p.Type == model.FormFile {
				f.Src = p.Src
			} else {
				f.Value = p.Value
			}
			d.FormData = append(d.FormData, f)
		}
	case model.BodyBinary:
		d.Mode, d.File = "file", &fileDef{Src: b.File}
	case model.BodyGraphQL:
		d.Mode = "graphql"
		if b.GraphQL != nil {
			d.GraphQL = &graphQLDef{Query: b.GraphQL.Query, Variables: b.GraphQL.Variables}
		}
	default:
		return nil
	}
	return d
}

func authToDef(a *model.Auth) *authDef {
	if a == nil || a.Type == model.AuthInherit || a.Type == "" {
		return nil
	}
	d := &authDef{Type: string(a.Type), Params: map[string][]kvDef{}}
	add := func(pairs ...[2]string) {
		var list []kvDef
		for _, p := range pairs {
			if p[1] != "" {
				list = append(list, kvDef{Key: p[0], Value: p[1], Type: "string"})
			}
		}
		d.Params[d.Type] = list
	}
	switch a.Type {
	case model.AuthNone:
		d.Type = "noauth"
	case model.AuthBearer:
		if a.Bearer != nil {
			add([2]string{"token", a.Bearer.Token})
		}
	case model.AuthBasic:
		if a.Basic != nil {
			add([2]string{"username", a.Basic.Username}, [2]string{"password", a.Basic.Password})
		}
	case model.AuthAPIKey:
		if a.APIKey != nil {
			in := string(a.APIKey.In)
			if in == "" {
				in = string(model.APIKeyHeader)
			}
			add([2]string{"key", a.APIKey.Key}, [2]string{"value", a.APIKey.Value}, [2]string{"in", in})
		}
	case model.AuthDigest:
		if a.Digest != nil {
			g := a.Digest
			add([2]string{"username", g.Username}, [2]string{"password", g.Password}, [2]string{"realm", g.Realm},
				[2]string{"nonce", g.Nonce}, [2]string{"algorithm", g.Algorithm}, [2]string{"qop", g.QOP},
				[2]string{"nonceCount", g.NonceCount}, [2]string{"clientNonce", g.ClientNonce}, [2]string{"opaque", g.Opaque})
		}
	case model.AuthAWSV4:
		if a.AWSV4 != nil {
			w := a.AWSV4
			add([2]string{"accessKey", w.AccessKey}, [2]string{"secretKey", w.SecretKey},
				[2]string{"region", w.Region}, [2]string{"service", w.Service}, [2]string{"sessionToken", w.SessionToken})
		}
	case model.AuthHawk:
		if a.Hawk != nil {
			h := a.Hawk
			add([2]string{"authId", h.ID}, [2]string{"authKey", h.Key}, [2]string{"algorithm", h.Algorithm},
				[2]string{"user", h.User}, [2]string{"nonce", h.Nonce}, [2]string{"extraData", h.ExtraData},
				[2]string{"app", h.AppID}, [2]string{"delegation", h.Delegation}, [2]string{"timestamp", h.Timestamp})
		}
	case model.AuthNTLM:
		if a.NTLM != nil {
			n := a.NTLM
			add([2]string{"username", n.Username}, [2]string{"password", n.Password},
				[2]string{"domain", n.Domain}, [2]string{"workstation", n.Workstation})
		}
	case model.AuthUnsupported:
		if a.OriginalType != "" {
			d.Type = a.OriginalType
		}
	}
	// Anything the typed fields do not model (and every parameter of an
	// OAuth2 or unsupported scheme) comes from Raw.
	if len(a.Raw) > 0 {
		list := d.Params[d.Type]
		for k, v := range a.Raw {
			list = append(list, kvDef{Key: k, Value: v})
		}
		d.Params[d.Type] = list
	}
	if _, ok := d.Params[d.Type]; !ok {
		d.Params[d.Type] = []kvDef{}
	}
	return d
}

// applyDef folds a request definition that came back from the sandbox onto a
// copy of the original request, keeping fields scripts cannot reach (ID,
// scripts, settings, examples).
func applyDef(orig *model.Request, d requestDef) *model.Request {
	out := &model.Request{}
	if orig != nil {
		*out = *orig
	}
	if out.Kind == "" {
		out.Kind = model.KindHTTP
	}
	out.Method = strings.ToUpper(d.Method)

	out.URL = model.URL{}
	if d.URL != nil {
		out.URL.Raw = d.URL.Raw
		for _, q := range d.URL.Query {
			out.URL.Query = append(out.URL.Query, model.KV{Key: q.Key, Value: q.str(), Description: q.Description, Disabled: q.Disabled})
		}
		for _, p := range d.URL.Variable {
			out.URL.PathVars = append(out.URL.PathVars, model.Variable{Key: p.Key, Value: p.str(), Description: p.Description, Disabled: p.Disabled})
		}
	}

	out.Headers = nil
	for _, h := range d.Header {
		out.Headers = append(out.Headers, model.KV{Key: h.Key, Value: h.str(), Description: h.Description, Disabled: h.Disabled})
	}

	out.Body = defToBody(d.Body, orig)
	out.Auth = defToAuth(d.Auth, orig)
	return out
}

func defToBody(d *bodyDef, orig *model.Request) model.Body {
	var b model.Body
	if orig != nil {
		b = orig.Body
	}
	if d == nil {
		return model.Body{}
	}
	b.Raw, b.URLEncoded, b.FormData, b.File, b.GraphQL = "", nil, nil, "", nil
	b.Disabled = d.Disabled
	switch d.Mode {
	case "raw":
		b.Mode, b.Raw = model.BodyRaw, d.Raw
		if d.Options != nil && d.Options.Raw != nil {
			b.Language = d.Options.Raw.Language
		}
	case "urlencoded":
		b.Mode = model.BodyURLEncoded
		for _, kv := range d.URLEncoded {
			b.URLEncoded = append(b.URLEncoded, model.KV{Key: kv.Key, Value: kv.str(), Description: kv.Description, Disabled: kv.Disabled})
		}
	case "formdata":
		b.Mode = model.BodyFormData
		for _, p := range d.FormData {
			part := model.FormPart{Key: p.Key, Type: model.FormPartType(p.Type), ContentType: p.ContentType, Description: p.Description, Disabled: p.Disabled}
			if part.Type == model.FormFile {
				part.Src = toString(p.Src)
			} else {
				part.Value = toString(p.Value)
			}
			b.FormData = append(b.FormData, part)
		}
	case "file":
		b.Mode = model.BodyBinary
		if d.File != nil {
			b.File = d.File.Src
		}
	case "graphql":
		b.Mode = model.BodyGraphQL
		if d.GraphQL != nil {
			b.GraphQL = &model.GraphQLBody{Query: d.GraphQL.Query, Variables: d.GraphQL.Variables}
		}
	case "", "none":
		b.Mode = model.BodyNone
	default:
		b.Mode = model.BodyMode(d.Mode)
	}
	return b
}

func defToAuth(d *authDef, orig *model.Request) *model.Auth {
	if d == nil || d.Type == "" {
		if orig != nil && orig.Auth != nil && orig.Auth.IsInherit() {
			return orig.Auth
		}
		return nil
	}
	switch d.Type {
	case "noauth":
		return &model.Auth{Type: model.AuthNone}
	case "bearer":
		return &model.Auth{Type: model.AuthBearer, Bearer: &model.BearerAuth{Token: d.get("token")}}
	case "basic":
		return &model.Auth{Type: model.AuthBasic, Basic: &model.BasicAuth{Username: d.get("username"), Password: d.get("password")}}
	case "apikey":
		in := model.APIKeyLocation(d.get("in"))
		if in == "" {
			in = model.APIKeyHeader
		}
		return &model.Auth{Type: model.AuthAPIKey, APIKey: &model.APIKeyAuth{Key: d.get("key"), Value: d.get("value"), In: in}}
	case "digest":
		return &model.Auth{Type: model.AuthDigest, Digest: &model.DigestAuth{
			Username: d.get("username"), Password: d.get("password"), Realm: d.get("realm"), Nonce: d.get("nonce"),
			Algorithm: d.get("algorithm"), QOP: d.get("qop"), NonceCount: d.get("nonceCount"),
			ClientNonce: d.get("clientNonce"), Opaque: d.get("opaque"),
		}}
	case "awsv4":
		return &model.Auth{Type: model.AuthAWSV4, AWSV4: &model.AWSV4Auth{
			AccessKey: d.get("accessKey"), SecretKey: d.get("secretKey"), Region: d.get("region"),
			Service: d.get("service"), SessionToken: d.get("sessionToken"),
		}}
	case "hawk":
		return &model.Auth{Type: model.AuthHawk, Hawk: &model.HawkAuth{
			ID: d.get("authId"), Key: d.get("authKey"), Algorithm: d.get("algorithm"), User: d.get("user"),
			Nonce: d.get("nonce"), ExtraData: d.get("extraData"), AppID: d.get("app"), Delegation: d.get("delegation"),
			Timestamp: d.get("timestamp"),
		}}
	case "ntlm":
		return &model.Auth{Type: model.AuthNTLM, NTLM: &model.NTLMAuth{
			Username: d.get("username"), Password: d.get("password"), Domain: d.get("domain"), Workstation: d.get("workstation"),
		}}
	case "oauth2":
		// OAuth2 configuration is WS2b's; preserve the parameters verbatim
		// and keep the original typed config if the script did not change
		// the scheme.
		if orig != nil && orig.Auth != nil && orig.Auth.Type == model.AuthOAuth2 {
			return orig.Auth
		}
		return &model.Auth{Type: model.AuthOAuth2, Raw: d.rawParams()}
	default:
		return &model.Auth{Type: model.AuthUnsupported, OriginalType: d.Type, Raw: d.rawParams()}
	}
}

func (a *authDef) rawParams() map[string]any {
	out := map[string]any{}
	for _, p := range a.Params[a.Type] {
		out[p.Key] = p.Value
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// defToRequest builds a model.Request from a definition a script handed to
// pm.sendRequest.
func defToRequest(d requestDef) *model.Request {
	r := applyDef(&model.Request{Kind: model.KindHTTP}, d)
	if r.Method == "" {
		r.Method = "GET"
	}
	r.Name = d.Name
	r.Description = d.Description
	return r
}

// responseToDef renders an httpx.Response the way pm.response should see it.
func responseToDef(r *httpx.Response) *responseDef {
	if r == nil {
		return nil
	}
	d := &responseDef{
		Code:         r.StatusCode,
		Status:       r.Status,
		Body:         string(r.Body),
		ResponseTime: float64(r.Timings.Total.Nanoseconds()) / 1e6,
		ResponseSize: int64(len(r.Body)),
		HeaderSize:   r.Size.Headers,
	}
	for _, h := range r.Headers {
		d.Header = append(d.Header, kvDef{Key: h.Key, Value: h.Value})
	}
	for _, c := range r.Cookies {
		d.Cookie = append(d.Cookie, cookieToDef(c))
	}
	if d.HeaderSize == 0 {
		d.HeaderSize = int64(len("HTTP/X.X ")+len(strconv.Itoa(d.Code))+1+len(d.Status)+4) + headerBytes(r.Headers)
	}
	return d
}

func headerBytes(hs []model.KV) int64 {
	var n int64
	for _, h := range hs {
		n += int64(len(h.Key) + 2 + len(h.Value) + 2)
	}
	return n
}

func cookieToDef(c model.Cookie) map[string]any {
	return map[string]any{
		"name": c.Name, "value": c.Value, "domain": c.Domain, "path": c.Path,
		"expires": c.Expires, "maxAge": c.MaxAge, "hostOnly": c.HostOnly,
		"httpOnly": c.HTTPOnly, "secure": c.Secure, "session": c.Session,
	}
}

func defToCookie(m map[string]any) model.Cookie {
	get := func(k string) string { return toString(m[k]) }
	flag := func(k string) bool { b, _ := m[k].(bool); return b }
	name := get("name")
	if name == "" {
		name = get("key")
	}
	return model.Cookie{
		Name: name, Value: get("value"), Domain: get("domain"), Path: get("path"),
		Expires: get("expires"), MaxAge: get("maxAge"), HostOnly: flag("hostOnly"),
		HTTPOnly: flag("httpOnly"), Secure: flag("secure"), Session: flag("session"),
	}
}
