package wspane

import (
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/shubham/postctl/internal/model"
	"github.com/shubham/postctl/internal/ui"
	"github.com/shubham/postctl/internal/wsx"
)

// ---- Auth ------------------------------------------------------------------

type authKind struct {
	t     model.AuthType
	label string
}

// authKinds are the schemes a WebSocket handshake can carry. Digest and NTLM
// need a challenge round trip that an upgrade request cannot do.
var authKinds = []authKind{
	{model.AuthInherit, "Inherit from parent"},
	{model.AuthNone, "No Auth"},
	{model.AuthBearer, "Bearer Token"},
	{model.AuthBasic, "Basic Auth"},
	{model.AuthAPIKey, "API Key"},
}

func authLabel(t model.AuthType) string {
	for _, k := range authKinds {
		if k.t == t {
			return k.label
		}
	}
	switch t {
	case model.AuthOAuth2:
		return "OAuth 2.0"
	case model.AuthDigest:
		return "Digest Auth"
	case model.AuthNTLM:
		return "NTLM"
	case model.AuthAWSV4:
		return "AWS Signature"
	case model.AuthHawk:
		return "Hawk"
	}
	return string(t)
}

// newAuth returns an empty auth of type t.
func newAuth(t model.AuthType) *model.Auth {
	a := &model.Auth{Type: t}
	normalizeAuth(a)
	return a
}

// normalizeAuth makes sure the block for the auth's type exists.
func normalizeAuth(a *model.Auth) {
	if a == nil {
		return
	}
	switch a.Type {
	case model.AuthBearer:
		if a.Bearer == nil {
			a.Bearer = &model.BearerAuth{}
		}
	case model.AuthBasic:
		if a.Basic == nil {
			a.Basic = &model.BasicAuth{}
		}
	case model.AuthAPIKey:
		if a.APIKey == nil {
			a.APIKey = &model.APIKeyAuth{In: model.APIKeyHeader}
		}
	}
}

// inheritedAuth walks the containers from the request's parent outward and
// returns the first explicit auth and a description of where it is set.
func (p *Pane) inheritedAuth() (*model.Auth, string) {
	for i := len(p.chain) - 1; i >= 0; i-- {
		a := containerAuth(p.chain[i])
		if a.IsInherit() {
			continue
		}
		kind := "folder"
		if _, ok := p.chain[i].(*model.Collection); ok {
			kind = "collection"
		}
		return a, kind + ` "` + containerName(p.chain[i]) + `"`
	}
	return nil, ""
}

// authView is the Auth tab.
type authView struct {
	f     form
	stash map[model.ID]map[model.AuthType]*model.Auth
}

func newAuthView() *authView {
	return &authView{stash: map[model.ID]map[model.AuthType]*model.Auth{}}
}

func (a *authView) reload()       { a.f.reset() }
func (a *authView) stop()         { a.f.reset() }
func (a *authView) editing() bool { return a.f.edit }
func (a *authView) enter()        { a.f.reveal = false }

// setType switches the auth type, remembering what was entered for the type
// being left so switching back restores it.
func (a *authView) setType(p *Pane, t model.AuthType) {
	id := p.req.ID
	if a.stash[id] == nil {
		a.stash[id] = map[model.AuthType]*model.Auth{}
	}
	if cur := p.req.Auth; cur != nil && cur.Type != model.AuthInherit {
		a.stash[id][cur.Type] = cur
	}
	switch {
	case t == model.AuthInherit:
		p.req.Auth = nil
	case a.stash[id][t] != nil:
		p.req.Auth = a.stash[id][t]
	default:
		p.req.Auth = newAuth(t)
	}
}

func (a *authView) curType(p *Pane) model.AuthType {
	if p.req.Auth.IsInherit() {
		return model.AuthInherit
	}
	return p.req.Auth.Type
}

func textRow(label string, ptr *string, secret bool, placeholder string) formRow {
	return formRow{
		label: label, kind: rowText, value: *ptr, secret: secret, placeholder: placeholder,
		set: func(t string) bool { *ptr = t; return true },
	}
}

func (a *authView) rows(p *Pane) []formRow {
	t := a.curType(p)
	kinds := authKinds
	if !supportedAuth(t) {
		kinds = append(append([]authKind(nil), authKinds...), authKind{t, authLabel(t)})
	}
	label := authLabel(t)
	rows := []formRow{{
		label: "Type", kind: rowChoice, value: label,
		cycle: func(dir int) {
			cur := 0
			for i, k := range kinds {
				if k.t == t {
					cur = i
				}
			}
			n := len(kinds)
			a.setType(p, kinds[(cur+dir+n)%n].t)
		},
	}}
	au := p.req.Auth
	normalizeAuth(au)
	switch t {
	case model.AuthInherit:
		eff, src := p.inheritedAuth()
		if eff == nil {
			rows = append(rows,
				formRow{label: "Effective", kind: rowInfo, value: "No Auth"},
				formRow{kind: rowInfo, value: "Nothing above sets authentication, so none is sent."})
			break
		}
		rows = append(rows,
			formRow{label: "Effective", kind: rowInfo, value: authLabel(eff.Type)},
			formRow{label: "From", kind: rowInfo, value: src})
		if !supportedAuth(eff.Type) {
			rows = append(rows, formRow{kind: rowInfo, value: p.th.Glyphs.Warn + " A WebSocket handshake cannot use this scheme; connecting will fail."})
		}
	case model.AuthNone:
		rows = append(rows, formRow{kind: rowInfo, value: "No authentication is sent, and nothing is inherited."})
	case model.AuthBearer:
		rows = append(rows, textRow("Token", &au.Bearer.Token, true, "Token or {{variable}}"))
	case model.AuthBasic:
		rows = append(rows,
			textRow("Username", &au.Basic.Username, false, "Username or {{variable}}"),
			textRow("Password", &au.Basic.Password, true, "Password or {{variable}}"))
	case model.AuthAPIKey:
		k := au.APIKey
		rows = append(rows,
			textRow("Key", &k.Key, false, "Header or parameter name"),
			textRow("Value", &k.Value, true, "Value or {{variable}}"),
			formRow{label: "Add to", kind: rowChoice, value: map[bool]string{true: "Query parameter", false: "Header"}[k.In == model.APIKeyQuery],
				cycle: func(int) {
					if k.In == model.APIKeyQuery {
						k.In = model.APIKeyHeader
					} else {
						k.In = model.APIKeyQuery
					}
				}})
	case model.AuthOAuth2:
		rows = append(rows, formRow{kind: rowInfo, value: "Its settings are kept as they are. Edit them on an HTTP request; the token is sent as a header."})
	default:
		rows = append(rows, formRow{kind: rowInfo, value: p.th.Glyphs.Warn + " A WebSocket handshake cannot use this scheme; connecting will fail."})
	}
	return rows
}

func supportedAuth(t model.AuthType) bool {
	switch t {
	case model.AuthInherit, model.AuthNone, model.AuthBearer, model.AuthBasic, model.AuthAPIKey, model.AuthOAuth2:
		return true
	}
	return false
}

func (a *authView) key(p *Pane, m tea.KeyPressMsg) (handled, changed bool) {
	return a.f.key(p, func() []formRow { return a.rows(p) }, m)
}

func (a *authView) view(p *Pane, w, h int) string { return a.f.view(p, a.rows(p), w, h) }

func (a *authView) hints() []ui.Hint {
	if a.f.edit {
		return []ui.Hint{{Key: "Esc", Label: "Done", Priority: 1}, {Key: "Tab", Label: "Next field", Priority: 3}, {Key: "Ctrl-R", Label: "Reveal", Priority: 4}}
	}
	return []ui.Hint{{Key: "←→", Label: "Change", Priority: 2}, {Key: "Enter", Label: "Edit", Priority: 2}, {Key: "r", Label: "Reveal", Priority: 4}}
}

// ---- Settings --------------------------------------------------------------

// settingsView is the Settings tab: subprotocols, keep-alive ping, message
// size limit, handshake timeout, TLS verification and cookies.
type settingsView struct {
	f                            form
	pingErr, sizeErr, timeoutErr bool
}

func newSettingsView() *settingsView { return &settingsView{} }

func (s *settingsView) reload()       { s.f.reset() }
func (s *settingsView) stop()         { s.f.reset() }
func (s *settingsView) editing() bool { return s.f.edit }
func (s *settingsView) enter()        { s.f.reveal = false }

func onOff(b bool) string {
	if b {
		return "On"
	}
	return "Off"
}

// parseTimeout accepts "30s", "2m", "1h30m", "500ms" or a bare number of
// seconds.
func parseTimeout(s string) (time.Duration, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	if n, err := strconv.ParseFloat(s, 64); err == nil {
		if n < 0 {
			return 0, false
		}
		return time.Duration(n * float64(time.Second)), true
	}
	d, err := time.ParseDuration(s)
	if err != nil || d < 0 {
		return 0, false
	}
	return d, true
}

// parseSize accepts a byte count with an optional KB, MB, GB, KiB, MiB or GiB
// suffix (all powers of 1024).
func parseSize(s string) (int64, bool) {
	s = strings.ToLower(strings.ReplaceAll(strings.TrimSpace(s), " ", ""))
	if s == "" {
		return 0, false
	}
	mult := int64(1)
	for _, u := range []struct {
		suffix string
		mult   int64
	}{{"gib", 1 << 30}, {"mib", 1 << 20}, {"kib", 1 << 10}, {"gb", 1 << 30}, {"mb", 1 << 20}, {"kb", 1 << 10}, {"b", 1}} {
		if strings.HasSuffix(s, u.suffix) {
			s = strings.TrimSuffix(s, u.suffix)
			mult = u.mult
			break
		}
	}
	n, err := strconv.ParseFloat(s, 64)
	if err != nil || n <= 0 {
		return 0, false
	}
	return int64(n * float64(mult)), true
}

func (s *settingsView) rows(p *Pane) []formRow {
	set := &p.req.Settings
	ws := p.req.WebSocket
	if ws == nil {
		ws = &model.WebSocketSettings{}
	}
	th := p.th

	protos := formRow{
		label: "Subprotocols", kind: rowText, value: strings.Join(ws.Subprotocols, ", "), placeholder: "none, e.g. graphql-transport-ws",
		set: func(text string) bool {
			var out []string
			for _, part := range strings.Split(text, ",") {
				if part = strings.TrimSpace(part); part != "" {
					out = append(out, part)
				}
			}
			p.ensureWS().Subprotocols = out
			return true
		},
	}

	pv := ""
	if ws.PingInterval != nil {
		pv = ws.PingInterval.String()
	}
	ping := formRow{
		label: "Keep-alive ping", kind: rowText, value: pv, placeholder: "off",
		set: func(text string) bool {
			if strings.TrimSpace(text) == "" {
				p.ensureWS().PingInterval = nil
				s.pingErr = false
				return true
			}
			d, ok := parseTimeout(text)
			if !ok {
				s.pingErr = true
				return false
			}
			s.pingErr = false
			if d == 0 {
				p.ensureWS().PingInterval = nil
			} else {
				p.ensureWS().PingInterval = &d
			}
			return true
		},
	}
	if s.pingErr {
		ping.note, ping.warn = th.Glyphs.Warn+" use 30s or 1m", true
	}

	sv := ""
	if ws.MaxMessageBytes != nil {
		sv = ui.FormatSize(*ws.MaxMessageBytes)
	}
	size := formRow{
		label: "Max message size", kind: rowText, value: sv, placeholder: ui.FormatSize(wsx.DefaultMaxMessageBytes) + " (default)",
		set: func(text string) bool {
			if strings.TrimSpace(text) == "" {
				p.ensureWS().MaxMessageBytes = nil
				s.sizeErr = false
				return true
			}
			n, ok := parseSize(text)
			if !ok {
				s.sizeErr = true
				return false
			}
			s.sizeErr = false
			p.ensureWS().MaxMessageBytes = &n
			return true
		},
	}
	if s.sizeErr {
		size.note, size.warn = th.Glyphs.Warn+" use 2 MB or 512 KB", true
	}

	tv := ""
	if set.Timeout != nil {
		tv = set.Timeout.String()
	}
	timeout := formRow{
		label: "Connect timeout", kind: rowText, value: tv, placeholder: "30s (default)",
		set: func(text string) bool {
			if strings.TrimSpace(text) == "" {
				set.Timeout = nil
				s.timeoutErr = false
				return true
			}
			d, ok := parseTimeout(text)
			if !ok {
				s.timeoutErr = true
				return false
			}
			s.timeoutErr = false
			set.Timeout = &d
			return true
		},
	}
	if s.timeoutErr {
		timeout.note, timeout.warn = th.Glyphs.Warn+" use 30s or 2m", true
	}

	verify := set.VerifyTLS == nil || *set.VerifyTLS
	tls := formRow{
		label: "Verify TLS", kind: rowChoice, value: onOff(verify),
		cycle: func(int) { v := !verify; set.VerifyTLS = &v },
	}
	if !verify {
		tls.note, tls.warn = th.Glyphs.Warn+" certificates are not checked", true
	}

	cookies := set.DisableCookies == nil || !*set.DisableCookies
	ck := formRow{
		label: "Send cookies", kind: rowChoice, value: onOff(cookies),
		cycle: func(int) { v := cookies; set.DisableCookies = &v },
	}

	return []formRow{
		protos, ping, size, timeout, tls, ck,
		{kind: rowInfo},
		{kind: rowInfo, value: "Variables in the URL, headers and messages resolve when you connect."},
		{kind: rowInfo, value: "Pre-request and test scripts do not run for WebSocket requests."},
	}
}

func (s *settingsView) key(p *Pane, m tea.KeyPressMsg) (handled, changed bool) {
	return s.f.key(p, func() []formRow { return s.rows(p) }, m)
}

func (s *settingsView) view(p *Pane, w, h int) string { return s.f.view(p, s.rows(p), w, h) }

func (s *settingsView) hints() []ui.Hint {
	if s.f.edit {
		return []ui.Hint{{Key: "Esc", Label: "Done", Priority: 1}, {Key: "Tab", Label: "Next field", Priority: 3}}
	}
	return []ui.Hint{
		{Key: "Space", Label: "Toggle", Priority: 2},
		{Key: "Enter", Label: "Edit", Priority: 2},
		{Key: "↑↓", Label: "Field", Priority: 4},
	}
}
