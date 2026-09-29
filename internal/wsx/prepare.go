package wsx

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/shubham/postctl/internal/httpx"
	"github.com/shubham/postctl/internal/model"
	"github.com/shubham/postctl/internal/vars"
)

// Hydrator fills in secret values from the secret store, in place. The TUI
// backend implements it.
type Hydrator interface {
	// HydrateVariables reads the values of secret variables.
	HydrateVariables(ctx context.Context, vs []model.Variable) error
	// HydrateAuth reads OAuth 2.0 token strings for an auth block.
	HydrateAuth(ctx context.Context, a *model.Auth) error
}

// Mask replaces secret values in text that is shown to the user.
const Mask = "****"

// minMaskedSecret is the shortest secret value that is masked inside text;
// shorter values would wreck ordinary words.
const minMaskedSecret = 4

// PrepareInput is what [Prepare] needs to resolve a request.
type PrepareInput struct {
	// Collection owns the request; it supplies variables and inherited auth.
	Collection *model.Collection
	// Request is the WebSocket request (a copy is resolved; it is not
	// modified).
	Request *model.Request
	// Environment is the active environment, or nil.
	Environment *model.Environment
	// Globals are the workspace globals, or nil.
	Globals *model.Globals
	// Hydrator reads secret values. Nil leaves secret variables empty.
	Hydrator Hydrator
	// Tokens obtains OAuth 2.0 tokens; nil uses only tokens already stored.
	Tokens httpx.TokenSource
	// Defaults are the workspace request settings for VerifyTLS and Timeout.
	Defaults model.RequestSettings
	// Jar supplies cookies to the handshake; nil sends none.
	Jar http.CookieJar
	// Now is the clock for the connection; time.Now when nil.
	Now func() time.Time
}

// Prepared is a resolved request, ready to dial and to turn saved messages
// into frames. It holds secret values in memory; do not log it.
type Prepared struct {
	// Target is the handshake. Its URL and headers can hold credentials.
	Target *Target
	// Options are the connection options derived from the request.
	Options Options
	// Warnings are non-fatal problems, such as unresolved variables in the
	// URL. They never contain secret values.
	Warnings []string

	store   *vars.MemStore
	secrets []string
}

// Prepare resolves the request's variables and inherited auth and builds the
// handshake. Pre-request and test scripts are not run for WebSocket requests.
func Prepare(ctx context.Context, in PrepareInput) (*Prepared, error) {
	if in.Request == nil {
		return nil, errors.New("no request")
	}
	if in.Request.Kind != model.KindWebSocket {
		return nil, fmt.Errorf("%q is not a WebSocket request", in.Request.Name)
	}
	req := in.Request.Clone()
	chain := chainOf(in.Collection, req.ID)

	var globals, coll, env []model.Variable
	if in.Globals != nil {
		globals = append(globals, in.Globals.Variables...)
	}
	for _, c := range chain {
		coll = append(coll, containerVars(c)...)
	}
	if in.Environment != nil {
		env = append(env, in.Environment.Variables...)
	}
	var secrets []string
	for _, vs := range [][]model.Variable{globals, coll, env} {
		if in.Hydrator != nil {
			if err := in.Hydrator.HydrateVariables(ctx, vs); err != nil {
				return nil, fmt.Errorf("could not read secret variables: %w", err)
			}
		}
		for _, v := range vs {
			if v.IsSecret() && !v.Disabled && len(v.Value) >= minMaskedSecret {
				secrets = append(secrets, v.Value)
			}
		}
	}
	store := vars.New(globals, coll, env, nil)

	resolved, err := vars.ResolveRequest(req, chain, store)
	if err != nil {
		return nil, err
	}
	if in.Hydrator != nil && resolved.Auth != nil && resolved.Auth.Type == model.AuthOAuth2 {
		if err := in.Hydrator.HydrateAuth(ctx, resolved.Auth); err != nil {
			return nil, fmt.Errorf("could not read the OAuth 2.0 token: %w", err)
		}
	}

	p := &Prepared{store: store, secrets: secrets}
	target, err := buildTarget(ctx, resolved, in.Tokens)
	if err != nil {
		return nil, err
	}
	p.Target = target
	if left := unresolved(resolved.URL.Raw); len(left) > 0 {
		p.Warnings = append(p.Warnings, "Unresolved variable "+strings.Join(left, ", ")+" in the URL")
	}

	ws := resolved.WebSocket
	if ws == nil {
		ws = &model.WebSocketSettings{}
	}
	for _, sp := range ws.Subprotocols {
		if sp = strings.TrimSpace(store.Replace(sp)); sp != "" {
			target.Subprotocols = append(target.Subprotocols, sp)
		}
	}
	verify := true
	switch {
	case resolved.Settings.VerifyTLS != nil:
		verify = *resolved.Settings.VerifyTLS
	case in.Defaults.VerifyTLS != nil:
		verify = *in.Defaults.VerifyTLS
	}
	timeout := DefaultHandshakeTimeout
	switch {
	case resolved.Settings.Timeout != nil && *resolved.Settings.Timeout > 0:
		timeout = *resolved.Settings.Timeout
	case in.Defaults.Timeout != nil && *in.Defaults.Timeout > 0:
		timeout = *in.Defaults.Timeout
	}
	p.Options = Options{Jar: in.Jar, HandshakeTimeout: timeout, InsecureSkipVerify: !verify, Now: in.Now}
	if resolved.Settings.DisableCookies != nil && *resolved.Settings.DisableCookies {
		p.Options.Jar = nil
	}
	if ws.PingInterval != nil && *ws.PingInterval > 0 {
		p.Options.PingInterval = *ws.PingInterval
	}
	if ws.MaxMessageBytes != nil && *ws.MaxMessageBytes > 0 {
		p.Options.MaxMessageBytes = *ws.MaxMessageBytes
	}
	return p, nil
}

// Dial connects with the prepared target and options.
func (p *Prepared) Dial(ctx context.Context) (*Conn, error) { return Dial(ctx, p.Target, p.Options) }

// Replace resolves {{variables}} in text with their real values. Dynamic
// variables such as {{$guid}} produce a new value on every call.
func (p *Prepared) Replace(s string) string { return p.store.Replace(s) }

// Mask replaces every secret variable value in s with [Mask]. Use it on text
// that resolved through [Prepared.Replace] before showing it.
func (p *Prepared) Mask(s string) string { return maskSecrets(s, p.secrets) }

// Frame turns a saved message into what goes on the wire and what the log
// shows. Variables are resolved first. A JSON message must parse
// ([ErrInvalidJSON]); a binary-base64 message is decoded ([ErrInvalidBase64]).
func (p *Prepared) Frame(m model.WSMessage) (Frame, error) {
	body := p.Replace(m.Body)
	f, err := BuildFrame(m.Format, body)
	if err != nil {
		return Frame{}, err
	}
	f.Display = []byte(p.Mask(string(f.Display)))
	return f, nil
}

// Frame is a message ready to send.
type Frame struct {
	// Type is the frame type.
	Type MessageType
	// Data is the payload for the wire.
	Data []byte
	// Display is what the log shows: the text, or the base64 text of a
	// binary message. [Prepared.Frame] masks secret values in it.
	Display []byte
}

// BuildFrame turns an already-resolved body into a frame according to format.
func BuildFrame(format model.WSFormat, body string) (Frame, error) {
	switch format {
	case model.WSBinaryBase64:
		raw, err := decodeBase64(body)
		if err != nil {
			return Frame{}, fmt.Errorf("%w: %w", ErrInvalidBase64, err)
		}
		return Frame{Type: Binary, Data: raw, Display: []byte(strings.Join(strings.Fields(body), ""))}, nil
	case model.WSJSON:
		if err := CheckJSON(body); err != nil {
			return Frame{}, err
		}
	}
	return Frame{Type: Text, Data: []byte(body), Display: []byte(body)}, nil
}

// CheckJSON reports whether s is valid JSON. The error wraps [ErrInvalidJSON]
// and, for syntax errors, says where the problem is.
func CheckJSON(s string) error {
	if strings.TrimSpace(s) == "" {
		return fmt.Errorf("%w: the message is empty", ErrInvalidJSON)
	}
	var v json.RawMessage
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		var se *json.SyntaxError
		if errors.As(err, &se) {
			line, col := lineCol(s, int(se.Offset))
			return fmt.Errorf("%w: %s at line %d, column %d", ErrInvalidJSON, se.Error(), line, col)
		}
		return fmt.Errorf("%w: %w", ErrInvalidJSON, err)
	}
	return nil
}

// lineCol converts a byte offset into a 1-based line and column.
func lineCol(s string, off int) (line, col int) {
	off = max(1, min(off, len(s)))
	line, col = 1, 1
	for _, r := range s[:off] {
		if r == '\n' {
			line++
			col = 1
			continue
		}
		col++
	}
	// The offset points just past the offending byte.
	return line, max(1, col-1)
}

func decodeBase64(s string) ([]byte, error) {
	s = strings.Join(strings.Fields(s), "")
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		if b, err := enc.DecodeString(s); err == nil {
			return b, nil
		}
	}
	return nil, errors.New("the text is not base64")
}

func maskSecrets(s string, secrets []string) string {
	if len(secrets) == 0 {
		return s
	}
	ordered := append([]string(nil), secrets...)
	sort.Slice(ordered, func(i, j int) bool { return len(ordered[i]) > len(ordered[j]) })
	for _, sec := range ordered {
		s = strings.ReplaceAll(s, sec, Mask)
	}
	return s
}

// ---- handshake ------------------------------------------------------------------

var varRE = regexp.MustCompile(`\{\{\s*([^{}$][^{}]*?)\s*\}\}`)

func unresolved(raw string) []string {
	var out []string
	seen := map[string]bool{}
	for _, m := range varRE.FindAllStringSubmatch(raw, -1) {
		if !seen[m[1]] {
			seen[m[1]] = true
			out = append(out, "{{"+m[1]+"}}")
		}
	}
	return out
}

// normalizeScheme returns raw with an http(s) scheme changed to ws(s), or
// [ErrBadScheme].
func normalizeScheme(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", &DialError{Err: errors.New("the URL is not valid")}
	}
	switch strings.ToLower(u.Scheme) {
	case "ws", "wss":
	case "http":
		u.Scheme = "ws"
	case "https":
		u.Scheme = "wss"
	default:
		return "", ErrBadScheme
	}
	if u.Host == "" {
		return "", &DialError{Err: errors.New("the URL has no host")}
	}
	return u.String(), nil
}

// buildTarget makes the handshake from a resolved request: enabled headers,
// then auth.
func buildTarget(ctx context.Context, r *model.Request, tokens httpx.TokenSource) (*Target, error) {
	raw := strings.TrimSpace(r.URL.Raw)
	if raw == "" {
		return nil, ErrNoURL
	}
	wsURL, err := normalizeScheme(raw)
	if err != nil {
		return nil, err
	}
	// Auth helpers work on *http.Request, so give them the http form of the URL.
	httpURL := "http" + strings.TrimPrefix(wsURL, "ws")
	scratch, err := http.NewRequestWithContext(ctx, http.MethodGet, httpURL, nil)
	if err != nil {
		return nil, &DialError{Err: errors.New("the URL is not valid")}
	}
	t := &Target{}
	for _, h := range r.Headers {
		if h.Disabled || strings.TrimSpace(h.Key) == "" {
			continue
		}
		if strings.EqualFold(h.Key, "Host") {
			t.Host = h.Value
			continue
		}
		scratch.Header.Add(strings.TrimSpace(h.Key), h.Value)
	}
	if a := r.Auth; a != nil {
		switch a.Type {
		case model.AuthDigest, model.AuthNTLM:
			return nil, fmt.Errorf("%w: %s", ErrUnsupportedAuth, a.Type)
		}
		if err := httpx.ApplyAuth(ctx, scratch, a, tokens); err != nil {
			return nil, err
		}
	}
	t.Header = scratch.Header
	t.URL = "ws" + strings.TrimPrefix(scratch.URL.String(), "http")
	return t, nil
}

// chainOf returns the containers above the request with the given ID,
// outermost first (the collection, then folders).
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
	if got := walk(c, nil); got != nil {
		return got
	}
	return []model.Container{c}
}

func containerVars(c model.Container) []model.Variable {
	switch v := c.(type) {
	case *model.Collection:
		return v.Variables
	case *model.Folder:
		return v.Variables
	}
	return nil
}
