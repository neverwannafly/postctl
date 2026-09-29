package request

import (
	tea "charm.land/bubbletea/v2"
	"github.com/shubham/postctl/internal/model"
	"github.com/shubham/postctl/internal/ui"
)

type authKind struct {
	t     model.AuthType
	label string
}

var authKinds = []authKind{
	{model.AuthInherit, "Inherit from parent"},
	{model.AuthNone, "No Auth"},
	{model.AuthBearer, "Bearer Token"},
	{model.AuthBasic, "Basic Auth"},
	{model.AuthAPIKey, "API Key"},
	{model.AuthDigest, "Digest Auth"},
	{model.AuthOAuth2, "OAuth 2.0"},
	{model.AuthAWSV4, "AWS Signature"},
	{model.AuthHawk, "Hawk"},
	{model.AuthNTLM, "NTLM"},
}

func authLabel(t model.AuthType) string {
	for _, k := range authKinds {
		if k.t == t {
			return k.label
		}
	}
	return string(t)
}

// newAuth returns an empty auth of type t with sensible defaults.
func newAuth(t model.AuthType) *model.Auth {
	a := &model.Auth{Type: t}
	switch t {
	case model.AuthBearer:
		a.Bearer = &model.BearerAuth{}
	case model.AuthBasic:
		a.Basic = &model.BasicAuth{}
	case model.AuthAPIKey:
		a.APIKey = &model.APIKeyAuth{In: model.APIKeyHeader}
	case model.AuthDigest:
		a.Digest = &model.DigestAuth{}
	case model.AuthOAuth2:
		a.OAuth2 = &model.OAuth2Config{GrantType: model.GrantAuthorizationCodePKCE}
	case model.AuthAWSV4:
		a.AWSV4 = &model.AWSV4Auth{}
	case model.AuthHawk:
		a.Hawk = &model.HawkAuth{Algorithm: "sha256"}
	case model.AuthNTLM:
		a.NTLM = &model.NTLMAuth{}
	}
	return a
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

func (a *authView) reload(p *Pane) {
	a.f.reset()
}
func (a *authView) stop()         { a.f.reset() }
func (a *authView) editing() bool { return a.f.edit }
func (a *authView) enter(p *Pane) { a.f.reveal = false }
func (a *authView) atTop() bool   { return a.f.cur == 0 && !a.f.edit }

func (a *authView) paste(p *Pane, m tea.PasteMsg) bool {
	return a.f.paste(p, func() []formRow { return a.rows(p) }, m)
}

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

func text(label string, ptr *string, secret bool, placeholder string) formRow {
	return formRow{
		label: label, kind: rowText, value: *ptr, secret: secret, placeholder: placeholder,
		set: func(t string) bool { *ptr = t; return true },
	}
}

func (a *authView) rows(p *Pane) []formRow {
	t := a.curType(p)
	typeLabel := authLabel(t)
	if t == model.AuthUnsupported {
		typeLabel = "Unsupported (" + p.req.Auth.OriginalType + ")"
	}
	rows := []formRow{{
		label: "Type", kind: rowChoice, value: typeLabel,
		cycle: func(dir int) {
			cur := 0
			for i, k := range authKinds {
				if k.t == t {
					cur = i
				}
			}
			n := len(authKinds)
			a.setType(p, authKinds[(cur+dir+n)%n].t)
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
				formRow{label: "", kind: rowInfo, value: "Nothing above sets authentication, so none is sent."})
			break
		}
		label := authLabel(eff.Type)
		if eff.Type == model.AuthUnsupported {
			label = "Unsupported (" + eff.OriginalType + ")"
		}
		rows = append(rows,
			formRow{label: "Effective", kind: rowInfo, value: label},
			formRow{label: "From", kind: rowInfo, value: src})
	case model.AuthNone:
		rows = append(rows, formRow{label: "", kind: rowInfo, value: "No authentication is sent, and nothing is inherited."})
	case model.AuthBearer:
		rows = append(rows, text("Token", &au.Bearer.Token, true, "Token or {{variable}}"))
	case model.AuthBasic:
		rows = append(rows,
			text("Username", &au.Basic.Username, false, "Username or {{variable}}"),
			text("Password", &au.Basic.Password, true, "Password or {{variable}}"))
	case model.AuthAPIKey:
		k := au.APIKey
		rows = append(rows,
			text("Key", &k.Key, false, "Header or parameter name"),
			text("Value", &k.Value, true, "Value or {{variable}}"),
			formRow{label: "Add to", kind: rowChoice, value: map[bool]string{true: "Query parameter", false: "Header"}[k.In == model.APIKeyQuery],
				cycle: func(int) {
					if k.In == model.APIKeyQuery {
						k.In = model.APIKeyHeader
					} else {
						k.In = model.APIKeyQuery
					}
				}})
	case model.AuthDigest:
		d := au.Digest
		rows = append(rows,
			text("Username", &d.Username, false, ""),
			text("Password", &d.Password, true, ""),
			text("Realm", &d.Realm, false, "from the server"),
			text("Algorithm", &d.Algorithm, false, "from the server"),
			text("QOP", &d.QOP, false, "from the server"))
	case model.AuthOAuth2:
		rows = append(rows, a.oauthRows(p)...)
	case model.AuthAWSV4:
		v := au.AWSV4
		rows = append(rows,
			text("Access key", &v.AccessKey, false, ""),
			text("Secret key", &v.SecretKey, true, ""),
			text("Region", &v.Region, false, "us-east-1"),
			text("Service", &v.Service, false, "execute-api"),
			text("Session token", &v.SessionToken, true, "optional"))
	case model.AuthHawk:
		v := au.Hawk
		rows = append(rows,
			text("Auth ID", &v.ID, false, ""),
			text("Auth key", &v.Key, true, ""),
			text("Algorithm", &v.Algorithm, false, "sha256"),
			text("User", &v.User, false, "optional"),
			text("Nonce", &v.Nonce, false, "generated"),
			text("Ext", &v.ExtraData, false, "optional"),
			text("App ID", &v.AppID, false, "optional"),
			text("Delegation", &v.Delegation, false, "optional"))
	case model.AuthNTLM:
		v := au.NTLM
		rows = append(rows,
			text("Username", &v.Username, false, ""),
			text("Password", &v.Password, true, ""),
			text("Domain", &v.Domain, false, "optional"),
			text("Workstation", &v.Workstation, false, "optional"))
	case model.AuthUnsupported:
		rows = append(rows, formRow{kind: rowInfo, value: "postctl cannot run this scheme. Its settings are kept as they are."})
	}
	return rows
}

func (a *authView) key(p *Pane, m tea.KeyPressMsg) (bool, bool, tea.Cmd) {
	if p.oauth != nil {
		// While waiting for the browser only cancel and copy apply.
		switch m.String() {
		case "c":
			return true, false, p.copyAuthURL()
		case "esc":
			return true, false, p.cancelOAuth()
		}
		return true, false, nil
	}
	if m.String() == "g" && !a.f.edit && a.curType(p) == model.AuthOAuth2 {
		return true, false, p.getToken()
	}
	return a.f.key(p, func() []formRow { return a.rows(p) }, m)
}

func (a *authView) view(p *Pane, w, h int) string {
	if p.oauth != nil {
		return p.oauthWaitView(w, h)
	}
	return a.f.view(p, a.rows(p), w, h)
}

func (a *authView) hints(p *Pane) []ui.Hint {
	switch {
	case p.oauth != nil:
		return []ui.Hint{{Key: "Esc", Label: "Cancel", Priority: 1}, {Key: "c", Label: "Copy link", Priority: 2}}
	case a.f.edit:
		hs := []ui.Hint{{Key: "Esc", Label: "Done", Priority: 1}, {Key: "Tab", Label: "Next field", Priority: 3}}
		return append(hs, ui.Hint{Key: "Ctrl-R", Label: "Reveal", Priority: 4})
	}
	hs := []ui.Hint{{Key: "←→", Label: "Change", Priority: 2}, {Key: "Enter", Label: "Edit", Priority: 2}, {Key: "r", Label: "Reveal", Priority: 4}}
	if a.curType(p) == model.AuthOAuth2 {
		hs = append(hs, ui.Hint{Key: "g", Label: "Get token", Priority: 2})
	}
	return hs
}
