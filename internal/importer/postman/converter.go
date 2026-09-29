package postman

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/shubham/postctl/internal/model"
)

const (
	schemaV21 = "https://schema.getpostman.com/json/collection/v2.1.0/collection.json"
	schemaV20 = "https://schema.getpostman.com/json/collection/v2.0.0/collection.json"
)

// Options controls conversion.
type Options struct {
	KeepLiteralSecrets bool
}

// DocumentKind identifies a supported Postman export.
type DocumentKind string

// Supported document kinds.
const (
	KindCollection  DocumentKind = "collection"
	KindEnvironment DocumentKind = "environment"
	KindGlobals     DocumentKind = "globals"
)

// Document is one converted Postman export.
type Document struct {
	Kind        DocumentKind
	Collection  *model.Collection
	Environment *model.Environment
	Globals     *model.Globals
}

// Convert parses and converts one supported Postman JSON document.
func Convert(data []byte, options Options) (*Document, Report, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var raw map[string]any
	if err := dec.Decode(&raw); err != nil {
		return nil, Report{}, errors.New("invalid JSON document")
	}
	var trailing any
	if err := dec.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, Report{}, errors.New("JSON document contains trailing data")
	}
	return ConvertObject(raw, options)
}

// ConvertObject converts an already-decoded Postman document.
func ConvertObject(raw map[string]any, options Options) (*Document, Report, error) {
	if raw == nil {
		return nil, Report{}, errors.New("document must be a JSON object")
	}
	if info := object(raw["info"]); info != nil {
		schema := text(info["schema"])
		if schema != schemaV21 && schema != schemaV20 {
			return nil, Report{}, errors.New("unsupported Postman collection schema (want v2.0 or v2.1)")
		}
		report := Report{}
		collection := convertCollection(raw, &report)
		if !options.KeepLiteralSecrets {
			moveCollectionSecrets(collection, &report)
		}
		report.Counts.Collections = 1
		report.sort()
		return &Document{Kind: KindCollection, Collection: collection}, report, nil
	}
	switch text(raw["_postman_variable_scope"]) {
	case "environment":
		env := convertEnvironment(raw)
		return &Document{Kind: KindEnvironment, Environment: env},
			Report{Counts: Counts{Environments: 1}}, nil
	case "globals", "global":
		globals := convertGlobals(raw)
		return &Document{Kind: KindGlobals, Globals: globals},
			Report{Counts: Counts{Globals: 1}}, nil
	default:
		return nil, Report{}, errors.New("unknown Postman document format")
	}
}

func convertCollection(raw map[string]any, report *Report) *model.Collection {
	info := object(raw["info"])
	c := &model.Collection{
		ID:          idOrNew(info["_postman_id"]),
		Name:        text(info["name"]),
		Description: description(info["description"]),
		Version:     versionString(info["version"]),
		Auth:        convertAuth(raw["auth"], text(info["name"]), report),
		Variables:   convertVariables(raw["variable"], false),
		Extra:       extras(raw, "info", "item", "auth", "variable", "event"),
	}
	infoExtra := extras(info, "_postman_id", "name", "description", "version", "schema")
	if len(infoExtra) > 0 {
		putExtra(&c.Extra, "info", infoExtra)
	}
	c.Scripts = convertEvents(raw["event"], c.Name, &c.Extra, report)
	c.Items = convertItems(raw["item"], c.Name, report)
	return c
}

func convertItems(value any, parentPath string, report *Report) []model.Item {
	values := array(value)
	items := make([]model.Item, 0, len(values))
	for _, itemValue := range values {
		raw := object(itemValue)
		if raw == nil {
			continue
		}
		name := text(raw["name"])
		path := joinPath(parentPath, name)
		if _, isFolder := raw["item"]; isFolder {
			folder := &model.Folder{
				ID:          idOrNew(raw["id"]),
				Name:        name,
				Description: description(raw["description"]),
				Auth:        convertAuth(raw["auth"], path, report),
				Variables:   convertVariables(raw["variable"], false),
				Extra:       extras(raw, "id", "name", "description", "item", "auth", "variable", "event"),
			}
			folder.Scripts = convertEvents(raw["event"], path, &folder.Extra, report)
			folder.Items = convertItems(raw["item"], path, report)
			items = append(items, folder)
			report.Counts.Folders++
			continue
		}
		requestRaw := raw["request"]
		if requestRaw == nil {
			report.AddWarning(path, "unsupported-item", "Item has neither request nor child items and was not imported")
			continue
		}
		request := convertRequest(requestRaw, path, report)
		request.ID = idOrNew(raw["id"])
		request.Name = name
		if request.Description == "" {
			request.Description = description(raw["description"])
		}
		request.Scripts = convertEvents(raw["event"], path, &request.Extra, report)
		request.Examples = convertExamples(raw["response"], path, report)
		itemExtra := extras(raw, "id", "name", "description", "request", "response", "event")
		if len(itemExtra) > 0 {
			putExtra(&request.Extra, "item", itemExtra)
		}
		items = append(items, request)
		report.Counts.Requests++
		report.Counts.Examples += len(request.Examples)
	}
	return items
}

func convertRequest(value any, path string, report *Report) *model.Request {
	if rawURL, ok := value.(string); ok {
		return &model.Request{Kind: model.KindHTTP, URL: model.ParseURL(rawURL)}
	}
	raw := object(value)
	request := &model.Request{
		Kind:        model.KindHTTP,
		Method:      strings.ToUpper(text(raw["method"])),
		Description: description(raw["description"]),
		URL:         convertURL(raw["url"]),
		Headers:     convertKV(raw["header"]),
		Body:        convertBody(raw["body"]),
		Auth:        convertAuth(raw["auth"], path, report),
		Settings:    convertSettings(raw["protocolProfileBehavior"]),
		Extra: extras(raw, "method", "description", "url", "header", "body", "auth",
			"protocolProfileBehavior"),
	}
	if urlRaw := object(raw["url"]); urlRaw != nil {
		urlExtra := extras(urlRaw, "raw", "protocol", "host", "port", "path", "query", "variable", "hash")
		if len(urlExtra) > 0 {
			putExtra(&request.Extra, "url", urlExtra)
		}
	}
	return request
}

func convertURL(value any) model.URL {
	if raw, ok := value.(string); ok {
		return model.ParseURL(raw)
	}
	obj := object(value)
	raw := text(obj["raw"])
	if raw == "" {
		raw = buildRawURL(obj)
	}
	result := model.ParseURL(raw)
	if _, ok := obj["query"]; ok {
		result.Query = convertKV(obj["query"])
	}
	if _, ok := obj["variable"]; ok {
		result.PathVars = convertVariables(obj["variable"], false)
	}
	return result
}

func buildRawURL(raw map[string]any) string {
	var b strings.Builder
	protocol := text(raw["protocol"])
	if protocol != "" {
		b.WriteString(protocol)
		b.WriteString("://")
	}
	b.WriteString(joinStringParts(raw["host"], "."))
	if port := text(raw["port"]); port != "" {
		b.WriteByte(':')
		b.WriteString(port)
	}
	path := joinStringParts(raw["path"], "/")
	if path != "" {
		if !strings.HasPrefix(path, "/") {
			b.WriteByte('/')
		}
		b.WriteString(path)
	}
	sep := "?"
	for _, q := range convertKV(raw["query"]) {
		if q.Disabled {
			continue
		}
		b.WriteString(sep)
		sep = "&"
		b.WriteString(q.Key)
		if q.Value != "" {
			b.WriteByte('=')
			b.WriteString(q.Value)
		}
	}
	if hash := text(raw["hash"]); hash != "" {
		b.WriteByte('#')
		b.WriteString(hash)
	}
	return b.String()
}

func convertBody(value any) model.Body {
	raw := object(value)
	if raw == nil {
		return model.Body{}
	}
	body := model.Body{
		Disabled: boolValue(raw["disabled"]),
		Extra: extras(raw, "mode", "disabled", "raw", "options", "urlencoded", "formdata",
			"file", "graphql"),
	}
	switch text(raw["mode"]) {
	case "", "none":
		body.Mode = model.BodyNone
	case "raw":
		body.Mode = model.BodyRaw
		body.Raw = text(raw["raw"])
		body.Language = text(object(object(raw["options"])["raw"])["language"])
	case "urlencoded":
		body.Mode = model.BodyURLEncoded
		body.URLEncoded = convertKV(raw["urlencoded"])
	case "formdata":
		body.Mode = model.BodyFormData
		for _, partValue := range array(raw["formdata"]) {
			part := object(partValue)
			partType := model.FormText
			if text(part["type"]) == "file" {
				partType = model.FormFile
			}
			body.FormData = append(body.FormData, model.FormPart{
				Key:         text(part["key"]),
				Type:        partType,
				Value:       text(part["value"]),
				Src:         sourceString(part["src"]),
				ContentType: text(part["contentType"]),
				Description: description(part["description"]),
				Disabled:    boolValue(part["disabled"]),
			})
		}
	case "file":
		body.Mode = model.BodyBinary
		body.File = sourceString(object(raw["file"])["src"])
	case "graphql":
		body.Mode = model.BodyGraphQL
		graphql := object(raw["graphql"])
		body.GraphQL = &model.GraphQLBody{
			Query:     text(graphql["query"]),
			Variables: jsonText(graphql["variables"]),
		}
	default:
		body.Mode = model.BodyNone
		putExtra(&body.Extra, "unsupportedMode", text(raw["mode"]))
	}
	return body
}

func convertSettings(value any) model.RequestSettings {
	raw := object(value)
	settings := model.RequestSettings{Extra: map[string]any{}}
	for key, value := range raw {
		switch key {
		case "followRedirects":
			settings.FollowRedirects = boolPtr(value)
		case "maxRedirects":
			settings.MaxRedirects = intPtr(value)
		case "followOriginalHttpMethod":
			settings.FollowOriginalMethod = boolPtr(value)
		case "followAuthorizationHeader":
			settings.FollowAuthHeader = boolPtr(value)
		case "strictSSL":
			settings.VerifyTLS = boolPtr(value)
		case "requestTimeout":
			if milliseconds, ok := intValue(value); ok {
				duration := time.Duration(milliseconds) * time.Millisecond
				settings.Timeout = &duration
			} else {
				settings.Extra[key] = value
			}
		case "disableBodyPruning":
			settings.DisableBodyPruning = boolPtr(value)
		case "disableUrlEncoding":
			settings.DisableURLEncoding = boolPtr(value)
		case "disableCookies":
			settings.DisableCookies = boolPtr(value)
		case "removeRefererHeaderOnRedirect":
			settings.RemoveRefererOnRedirect = boolPtr(value)
		case "httpVersion":
			settings.HTTPVersion = text(value)
		default:
			settings.Extra[key] = value
		}
	}
	if len(settings.Extra) == 0 {
		settings.Extra = nil
	}
	return settings
}

func convertAuth(value any, path string, report *Report) *model.Auth {
	raw := object(value)
	if raw == nil {
		return nil
	}
	authType := text(raw["type"])
	if authType == "" {
		return nil
	}
	if authType == "noauth" {
		return &model.Auth{Type: model.AuthNone}
	}
	if authType == "inherit" {
		return &model.Auth{Type: model.AuthInherit}
	}
	values, rawParams := authValues(raw[authType])
	auth := &model.Auth{}
	consume := func(keys ...string) string {
		for _, key := range keys {
			if value, ok := values[key]; ok {
				delete(rawParams, key)
				return value
			}
		}
		return ""
	}
	switch authType {
	case "bearer":
		auth.Type = model.AuthBearer
		auth.Bearer = &model.BearerAuth{Token: consume("token")}
	case "basic":
		auth.Type = model.AuthBasic
		auth.Basic = &model.BasicAuth{Username: consume("username"), Password: consume("password")}
	case "apikey":
		auth.Type = model.AuthAPIKey
		location := model.APIKeyHeader
		if strings.EqualFold(consume("in"), "query") {
			location = model.APIKeyQuery
		}
		auth.APIKey = &model.APIKeyAuth{Key: consume("key"), Value: consume("value"), In: location}
	case "digest":
		auth.Type = model.AuthDigest
		auth.Digest = &model.DigestAuth{
			Username: consume("username"), Password: consume("password"), Realm: consume("realm"),
			Nonce: consume("nonce"), Algorithm: consume("algorithm"), QOP: consume("qop"),
			NonceCount: consume("nonceCount", "nc"), ClientNonce: consume("clientNonce", "cnonce"),
			Opaque: consume("opaque"),
		}
	case "oauth2":
		auth.Type = model.AuthOAuth2
		auth.OAuth2 = convertOAuth2(consume, rawParams)
	case "awsv4":
		auth.Type = model.AuthAWSV4
		auth.AWSV4 = &model.AWSV4Auth{
			AccessKey: consume("accessKey"), SecretKey: consume("secretKey"), Region: consume("region"),
			Service: consume("service"), SessionToken: consume("sessionToken"),
		}
	case "hawk":
		auth.Type = model.AuthHawk
		auth.Hawk = &model.HawkAuth{
			ID: consume("authId", "id"), Key: consume("authKey", "key"), Algorithm: consume("algorithm"),
			User: consume("user"), Nonce: consume("nonce"), ExtraData: consume("extraData", "ext"),
			AppID: consume("app", "appId"), Delegation: consume("dlg", "delegation"),
			Timestamp: consume("timestamp"), IncludePayloadHash: parseBool(consume("includePayloadHash")),
		}
	case "ntlm":
		auth.Type = model.AuthNTLM
		auth.NTLM = &model.NTLMAuth{
			Username: consume("username"), Password: consume("password"),
			Domain: consume("domain"), Workstation: consume("workstation"),
		}
	default:
		auth.Type = model.AuthUnsupported
		auth.OriginalType = authType
		auth.Raw = map[string]any{"parameters": cloneMapAny(rawParams)}
		for key, entry := range extras(raw, "type", authType) {
			auth.Raw["_auth_"+key] = entry
		}
		report.AddWarning(path, "unsupported-auth",
			fmt.Sprintf("Auth type %s was preserved but cannot be sent", authType))
		return auth
	}
	if len(rawParams) > 0 {
		auth.Raw = cloneMapAny(rawParams)
	}
	for key, entry := range extras(raw, "type", authType) {
		if auth.Raw == nil {
			auth.Raw = map[string]any{}
		}
		auth.Raw["_auth_"+key] = entry
	}
	return auth
}

func convertOAuth2(consume func(...string) string, remaining map[string]string) *model.OAuth2Config {
	grantRaw := consume("grant_type", "grantType")
	grant := model.GrantClientCredentials
	switch grantRaw {
	case "authorization_code":
		grant = model.GrantAuthorizationCode
	case "authorization_code_with_pkce", "authorization_code_pkce":
		grant = model.GrantAuthorizationCodePKCE
	case "password_credentials", "password":
		grant = model.GrantPassword
	case "implicit":
		grant = model.GrantImplicit
	case "client_credentials", "":
		grant = model.GrantClientCredentials
	default:
		remaining["grant_type"] = grantRaw
	}
	cfg := &model.OAuth2Config{
		GrantType: grant, TokenName: consume("tokenName"), AuthURL: consume("authUrl", "authURL"),
		AccessTokenURL:  consume("accessTokenUrl", "accessTokenURL"),
		RefreshTokenURL: consume("refreshTokenUrl", "refreshTokenURL"),
		ClientID:        consume("clientId", "clientID"), ClientSecret: consume("clientSecret"),
		Scope: consume("scope"), RedirectURI: consume("redirect_uri", "redirectUri"),
		State: consume("state"), Audience: consume("audience"), Resource: consume("resource"),
		Username: consume("username"), Password: consume("password"),
		CodeChallengeMethod: consume("codeChallengeMethod"), CodeVerifier: consume("codeVerifier"),
		HeaderPrefix: consume("headerPrefix"),
	}
	switch consume("client_authentication", "clientAuth") {
	case "body", "request_body":
		cfg.ClientAuth = model.ClientAuthBody
	case "header", "basic_auth_header":
		cfg.ClientAuth = model.ClientAuthHeader
	}
	switch consume("addTokenTo") {
	case "queryParams", "query":
		cfg.AddTokenTo = model.TokenInQuery
	case "header":
		cfg.AddTokenTo = model.TokenInHeader
	}
	access := consume("accessToken")
	refresh := consume("refreshToken")
	idToken := consume("idToken")
	tokenType := consume("tokenType")
	tokenScope := consume("tokenScope")
	if access != "" || refresh != "" || idToken != "" || tokenType != "" || tokenScope != "" {
		cfg.Token = &model.OAuth2Token{
			AccessToken: access, RefreshToken: refresh, IDToken: idToken,
			TokenType: tokenType, Scope: tokenScope,
		}
	}
	if len(remaining) > 0 {
		cfg.Extra = cloneMapAny(remaining)
	}
	return cfg
}

func convertVariables(value any, environment bool) []model.Variable {
	values := array(value)
	result := make([]model.Variable, 0, len(values))
	for _, variableValue := range values {
		raw := object(variableValue)
		variable := model.Variable{
			Key:         text(raw["key"]),
			Value:       valueString(raw["value"]),
			DataType:    text(raw["type"]),
			Description: description(raw["description"]),
			Disabled:    boolValue(raw["disabled"]),
		}
		if environment {
			variable.Disabled = raw["enabled"] != nil && !boolValue(raw["enabled"])
			switch variable.DataType {
			case "secret":
				variable.Type = model.VarSecret
				variable.DataType = ""
			case "default", "any":
				variable.DataType = ""
			}
		}
		result = append(result, variable)
	}
	return result
}

func convertEnvironment(raw map[string]any) *model.Environment {
	return &model.Environment{
		ID:          idOrNew(raw["id"]),
		Name:        text(raw["name"]),
		Description: description(raw["description"]),
		Variables:   convertVariables(raw["values"], true),
		Extra:       extras(raw, "id", "name", "description", "values"),
	}
}

func convertGlobals(raw map[string]any) *model.Globals {
	return &model.Globals{
		ID:        idOrNew(raw["id"]),
		Name:      text(raw["name"]),
		Variables: convertVariables(raw["values"], true),
		Extra:     extras(raw, "id", "name", "values"),
	}
}

func convertEvents(value any, path string, extra *map[string]any, report *Report) model.Scripts {
	var scripts model.Scripts
	grouped := map[string][]map[string]any{}
	for _, eventValue := range array(value) {
		event := object(eventValue)
		listen := text(event["listen"])
		if listen != "prerequest" && listen != "test" {
			putExtra(extra, "events", append(array((*extra)["events"]), event))
			continue
		}
		grouped[listen] = append(grouped[listen], event)
	}
	for listen, events := range grouped {
		if len(events) > 1 || eventsHaveExtra(events) {
			putExtra(extra, listen+"Events", mapsToAny(events))
		}
		var enabled, disabled []map[string]any
		for _, event := range events {
			if boolValue(event["disabled"]) {
				disabled = append(disabled, event)
			} else {
				enabled = append(enabled, event)
			}
		}
		selected := enabled
		allDisabled := len(enabled) == 0
		if allDisabled {
			selected = disabled
		} else if len(disabled) > 0 {
			putExtra(extra, "disabledEvents", append(array((*extra)["disabledEvents"]), mapsToAny(disabled)...))
		}
		script := combineScripts(selected)
		script.Disabled = allDisabled
		if listen == "prerequest" {
			scripts.PreRequest = script
			report.Counts.PreScripts++
		} else {
			scripts.Test = script
			report.Counts.TestScripts++
		}
	}
	lintRequestScripts(path, scripts, report)
	return scripts
}

func eventsHaveExtra(events []map[string]any) bool {
	for _, event := range events {
		if len(extras(event, "listen", "disabled", "script")) > 0 {
			return true
		}
		script := object(event["script"])
		if len(extras(script, "id", "name", "src", "exec", "type")) > 0 {
			return true
		}
	}
	return false
}

func combineScripts(events []map[string]any) *model.Script {
	var sources []string
	result := &model.Script{}
	for index, event := range events {
		script := object(event["script"])
		if index == 0 {
			result.ID = model.ID(text(script["id"]))
			result.Name = text(script["name"])
			result.Src = sourceString(script["src"])
		}
		sources = append(sources, strings.Join(stringArray(script["exec"]), "\n"))
	}
	result.Source = strings.Join(sources, "\n")
	return result
}

func convertExamples(value any, path string, report *Report) []model.Example {
	var examples []model.Example
	for _, exampleValue := range array(value) {
		raw := object(exampleValue)
		example := model.Example{
			ID:              model.ID(text(raw["id"])),
			Name:            text(raw["name"]),
			Status:          text(raw["status"]),
			Code:            intDefault(raw["code"]),
			Headers:         convertKV(raw["header"]),
			Cookies:         convertCookies(raw["cookie"]),
			Body:            text(raw["body"]),
			PreviewLanguage: text(raw["_postman_previewlanguage"]),
			Extra: extras(raw, "id", "name", "status", "code", "header", "cookie", "body",
				"_postman_previewlanguage", "originalRequest"),
		}
		if original := raw["originalRequest"]; original != nil {
			example.OriginalRequest = convertRequest(original, path+" › example", report)
		}
		examples = append(examples, example)
	}
	return examples
}

func convertCookies(value any) []model.Cookie {
	var result []model.Cookie
	for _, cookieValue := range array(value) {
		raw := object(cookieValue)
		result = append(result, model.Cookie{
			Name: text(raw["name"]), Value: text(raw["value"]), Domain: text(raw["domain"]),
			Path: text(raw["path"]), Expires: text(raw["expires"]), MaxAge: text(raw["maxAge"]),
			HostOnly: boolValue(raw["hostOnly"]), HTTPOnly: boolValue(raw["httpOnly"]),
			Secure: boolValue(raw["secure"]), Session: boolValue(raw["session"]),
		})
	}
	return result
}

func convertKV(value any) []model.KV {
	values := array(value)
	result := make([]model.KV, 0, len(values))
	for _, entryValue := range values {
		raw := object(entryValue)
		result = append(result, model.KV{
			Key: text(raw["key"]), Value: valueString(raw["value"]),
			Description: description(raw["description"]), Disabled: boolValue(raw["disabled"]),
		})
	}
	return result
}

func object(value any) map[string]any {
	if result, ok := value.(map[string]any); ok {
		return result
	}
	reflected := reflect.ValueOf(value)
	if !reflected.IsValid() || reflected.Kind() != reflect.Map ||
		reflected.Type().Key().Kind() != reflect.String {
		return nil
	}
	result := make(map[string]any, reflected.Len())
	iterator := reflected.MapRange()
	for iterator.Next() {
		result[iterator.Key().String()] = iterator.Value().Interface()
	}
	return result
}

func array(value any) []any {
	result, _ := value.([]any)
	return result
}

func text(value any) string {
	result, _ := value.(string)
	return result
}

func valueString(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return typed
	case json.Number:
		return typed.String()
	case bool:
		return strconv.FormatBool(typed)
	default:
		encoded, err := json.Marshal(typed)
		if err != nil {
			return ""
		}
		return string(encoded)
	}
}

func description(value any) string {
	if result, ok := value.(string); ok {
		return result
	}
	return text(object(value)["content"])
}

func extras(raw map[string]any, known ...string) map[string]any {
	if raw == nil {
		return nil
	}
	skip := make(map[string]bool, len(known))
	for _, key := range known {
		skip[key] = true
	}
	result := map[string]any{}
	for key, value := range raw {
		if !skip[key] {
			result[key] = value
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func putExtra(extra *map[string]any, key string, value any) {
	if value == nil {
		return
	}
	if *extra == nil {
		*extra = map[string]any{}
	}
	(*extra)[key] = value
}

func idOrNew(value any) model.ID {
	if id := text(value); id != "" {
		return model.ID(id)
	}
	return model.NewID()
}

func joinPath(parent, child string) string {
	if parent == "" {
		return child
	}
	if child == "" {
		return parent
	}
	return parent + " › " + child
}

func joinStringParts(value any, separator string) string {
	if single, ok := value.(string); ok {
		return single
	}
	return strings.Join(stringArray(value), separator)
}

func stringArray(value any) []string {
	raw := array(value)
	result := make([]string, 0, len(raw))
	for _, entry := range raw {
		result = append(result, valueString(entry))
	}
	return result
}

func sourceString(value any) string {
	if values := stringArray(value); len(values) > 0 {
		return values[0]
	}
	return text(value)
}

func jsonText(value any) string {
	if result, ok := value.(string); ok {
		return result
	}
	if value == nil {
		return ""
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	return string(encoded)
}

func boolValue(value any) bool {
	result, _ := value.(bool)
	return result
}

func boolPtr(value any) *bool {
	result, ok := value.(bool)
	if !ok {
		return nil
	}
	return &result
}

func parseBool(value string) bool {
	result, _ := strconv.ParseBool(value)
	return result
}

func intValue(value any) (int, bool) {
	switch typed := value.(type) {
	case json.Number:
		result, err := strconv.Atoi(typed.String())
		return result, err == nil
	case float64:
		return int(typed), typed == float64(int(typed))
	case int:
		return typed, true
	default:
		return 0, false
	}
}

func intPtr(value any) *int {
	result, ok := intValue(value)
	if !ok {
		return nil
	}
	return &result
}

func intDefault(value any) int {
	result, _ := intValue(value)
	return result
}

func versionString(value any) string {
	if stringValue := text(value); stringValue != "" {
		return stringValue
	}
	raw := object(value)
	if raw == nil {
		return ""
	}
	major, majorOK := intValue(raw["major"])
	minor, minorOK := intValue(raw["minor"])
	patch, patchOK := intValue(raw["patch"])
	if !majorOK && !minorOK && !patchOK {
		return ""
	}
	return fmt.Sprintf("%d.%d.%d", major, minor, patch)
}

func authValues(value any) (map[string]string, map[string]string) {
	values := map[string]string{}
	raw := map[string]string{}
	if objectValue := object(value); objectValue != nil {
		for key, entry := range objectValue {
			values[key] = valueString(entry)
			raw[key] = valueString(entry)
		}
		return values, raw
	}
	for _, entryValue := range array(value) {
		entry := object(entryValue)
		key := text(entry["key"])
		if key == "" {
			continue
		}
		stringValue := valueString(entry["value"])
		if _, duplicate := values[key]; duplicate {
			raw[key+"#duplicate"] = stringValue
			continue
		}
		values[key] = stringValue
		raw[key] = stringValue
	}
	return values, raw
}

func cloneMapAny[T ~string](source map[string]T) map[string]any {
	result := make(map[string]any, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}

func mapsToAny(values []map[string]any) []any {
	result := make([]any, len(values))
	for index := range values {
		result[index] = values[index]
	}
	return result
}
