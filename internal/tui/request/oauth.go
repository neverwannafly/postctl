package request

import (
	"context"
	"errors"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/shubham/postctl/internal/model"
	"github.com/shubham/postctl/internal/oauth2"
	"github.com/shubham/postctl/internal/tui"
	"github.com/shubham/postctl/internal/ui"
	"github.com/shubham/postctl/internal/vars"
)

var grants = []struct {
	g     model.OAuth2Grant
	label string
}{
	{model.GrantAuthorizationCodePKCE, "Authorization Code (PKCE)"},
	{model.GrantAuthorizationCode, "Authorization Code"},
	{model.GrantClientCredentials, "Client Credentials"},
	{model.GrantPassword, "Password Credentials"},
	{model.GrantImplicit, "Implicit"},
}

func grantLabel(g model.OAuth2Grant) string {
	for _, x := range grants {
		if x.g == g {
			return x.label
		}
	}
	return string(g)
}

// oauthRun is one "Get new token" attempt. The fetch runs in a command; its
// callbacks post to ch, which a listening command turns into messages.
type oauthRun struct {
	id      model.ID
	cancel  context.CancelFunc
	ch      chan any
	url     string
	note    string
	started time.Time
}

type (
	oauthEventsMsg struct {
		run *oauthRun
		ev  any
	}
	oauthURLEv  struct{ url string }
	oauthNoteEv struct{ note string }
	oauthDoneEv struct {
		tok *model.OAuth2Token
		err error
	}
	oauthSavedMsg struct {
		run *oauthRun
		tok *model.OAuth2Token
		err error
	}
)

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
	case model.AuthDigest:
		if a.Digest == nil {
			a.Digest = &model.DigestAuth{}
		}
	case model.AuthOAuth2:
		if a.OAuth2 == nil {
			a.OAuth2 = &model.OAuth2Config{GrantType: model.GrantAuthorizationCodePKCE}
		}
	case model.AuthAWSV4:
		if a.AWSV4 == nil {
			a.AWSV4 = &model.AWSV4Auth{}
		}
	case model.AuthHawk:
		if a.Hawk == nil {
			a.Hawk = &model.HawkAuth{}
		}
	case model.AuthNTLM:
		if a.NTLM == nil {
			a.NTLM = &model.NTLMAuth{}
		}
	}
}

func (a *authView) oauthRows(p *Pane) []formRow {
	c := p.req.Auth.OAuth2
	g := c.GrantType
	if g == "" {
		g = model.GrantAuthorizationCodePKCE
	}
	rows := []formRow{
		{label: "Grant type", kind: rowChoice, value: grantLabel(g), cycle: func(dir int) {
			cur := 0
			for i, x := range grants {
				if x.g == g {
					cur = i
				}
			}
			c.GrantType = grants[(cur+dir+len(grants))%len(grants)].g
		}},
		{label: "Token", kind: rowInfo, value: p.tokenStatus(c)},
		{label: "", kind: rowAction, value: "Get new token", action: p.getToken},
	}
	code := g == model.GrantAuthorizationCode || g == model.GrantAuthorizationCodePKCE || g == model.GrantImplicit
	rows = append(rows, text("Token name", &c.TokenName, false, "optional"))
	if code {
		rows = append(rows, text("Auth URL", &c.AuthURL, false, "https://…/authorize"))
	}
	if g != model.GrantImplicit {
		rows = append(rows, text("Access token URL", &c.AccessTokenURL, false, "https://…/token"))
	}
	rows = append(rows, text("Client ID", &c.ClientID, false, ""))
	if g != model.GrantImplicit {
		rows = append(rows, text("Client secret", &c.ClientSecret, true, "Secret or {{variable}}"))
	}
	if g == model.GrantPassword {
		rows = append(rows, text("Username", &c.Username, false, ""), text("Password", &c.Password, true, ""))
	}
	rows = append(rows, text("Scope", &c.Scope, false, "space separated"))
	if code {
		rows = append(rows, text("Redirect URI", &c.RedirectURI, false, "local callback (automatic)"))
	}
	if g != model.GrantImplicit {
		rows = append(rows, formRow{label: "Client auth", kind: rowChoice,
			value: map[bool]string{true: "In request body", false: "Basic auth header"}[c.ClientAuth == model.ClientAuthBody],
			cycle: func(int) {
				if c.ClientAuth == model.ClientAuthBody {
					c.ClientAuth = model.ClientAuthHeader
				} else {
					c.ClientAuth = model.ClientAuthBody
				}
			}})
	}
	rows = append(rows, formRow{label: "Send token in", kind: rowChoice,
		value: map[bool]string{true: "Query parameter", false: "Header"}[c.AddTokenTo == model.TokenInQuery],
		cycle: func(int) {
			if c.AddTokenTo == model.TokenInQuery {
				c.AddTokenTo = model.TokenInHeader
			} else {
				c.AddTokenTo = model.TokenInQuery
			}
		}})
	if c.AddTokenTo != model.TokenInQuery {
		rows = append(rows, text("Header prefix", &c.HeaderPrefix, false, "Bearer"))
	}
	return rows
}

// tokenStatus describes the stored token with text, never color alone.
func (p *Pane) tokenStatus(c *model.OAuth2Config) string {
	g := p.th.Glyphs
	if p.oauthErr != "" {
		return g.Fail + " " + p.oauthErr
	}
	t := c.Token
	if t == nil || (t.Ref == "" && t.AccessToken == "" && t.ExpiresAt.IsZero()) {
		return "No token yet"
	}
	now := p.deps.now()
	switch {
	case t.ExpiresAt.IsZero():
		return g.OK + " Saved, no expiry given"
	case t.Expired(now):
		return g.Expired + " Expired " + humanDur(now.Sub(t.ExpiresAt)) + " ago"
	}
	return g.OK + " Valid, expires in " + humanDur(t.ExpiresAt.Sub(now))
}

func humanDur(d time.Duration) string {
	d = d.Round(time.Second)
	switch {
	case d < time.Minute:
		return itoa(int(d.Seconds())) + "s"
	case d < time.Hour:
		return itoa(int(d.Minutes())) + "m"
	case d < 48*time.Hour:
		h := int(d.Hours())
		if m := int(d.Minutes()) % 60; m > 0 && h < 10 {
			return itoa(h) + "h " + itoa(m) + "m"
		}
		return itoa(h) + "h"
	}
	return itoa(int(d.Hours()/24)) + " days"
}

// resolveOAuth substitutes variables in every string field, as Fetch requires.
func resolveOAuth(c *model.OAuth2Config, st *vars.MemStore) {
	for _, s := range []*string{&c.TokenName, &c.AuthURL, &c.AccessTokenURL, &c.RefreshTokenURL, &c.ClientID,
		&c.ClientSecret, &c.Scope, &c.RedirectURI, &c.State, &c.Audience, &c.Resource, &c.Username,
		&c.Password, &c.CodeVerifier, &c.HeaderPrefix} {
		*s = st.Replace(*s)
	}
}

// getToken starts the OAuth 2.0 "Get new token" flow for the open request.
func (p *Pane) getToken() tea.Cmd {
	if !p.editable() || p.req.Auth == nil || p.req.Auth.Type != model.AuthOAuth2 || p.req.Auth.OAuth2 == nil {
		return nil
	}
	if p.oauth != nil {
		return nil
	}
	cfg := p.req.Auth.OAuth2
	if cfg.GrantType == "" {
		cfg.GrantType = model.GrantAuthorizationCodePKCE
	}
	clone := p.req.Auth.Clone().OAuth2
	var globals, coll, envVars []model.Variable
	if p.ws != nil && p.ws.Globals != nil {
		globals = append(globals, p.ws.Globals.Variables...)
	}
	for _, c := range p.chain {
		coll = append(coll, containerVars(c)...)
	}
	if p.activeEnv != nil {
		envVars = append(envVars, p.activeEnv.Variables...)
	}
	hyd, _ := p.env.Backend.(tui.Hydrator)
	fetch := p.deps.Fetch
	if fetch == nil {
		fetch = oauth2.Fetch
	}
	browser := p.deps.Browser

	ctx, cancel := context.WithCancel(context.Background())
	run := &oauthRun{id: p.req.ID, cancel: cancel, ch: make(chan any, 8), started: p.deps.now()}
	p.oauth = run
	p.oauthErr = ""

	work := func() tea.Msg {
		done := func(tok *model.OAuth2Token, err error) { run.ch <- oauthDoneEv{tok: tok, err: err} }
		if hyd != nil {
			for _, vs := range [][]model.Variable{globals, coll, envVars} {
				if err := hyd.HydrateVariables(ctx, vs); err != nil {
					done(nil, errors.New("could not read secret variables"))
					return nil
				}
			}
			_ = hyd.HydrateAuth(ctx, &model.Auth{Type: model.AuthOAuth2, OAuth2: clone})
		}
		resolveOAuth(clone, vars.New(globals, coll, envVars, nil))
		opts := oauth2.FetchOptions{
			Browser:   browser,
			OnAuthURL: func(u string) { run.ch <- oauthURLEv{url: u} },
			OnNote:    func(n string) { run.ch <- oauthNoteEv{note: n} },
		}
		tok, err := fetch(ctx, clone, opts)
		done(tok, err)
		return nil
	}
	return tea.Batch(work, listenOAuth(run), p.ensureTick())
}

func listenOAuth(run *oauthRun) tea.Cmd {
	return func() tea.Msg { return oauthEventsMsg{run: run, ev: <-run.ch} }
}

func (p *Pane) onOAuthEvents(m oauthEventsMsg) tea.Cmd {
	if m.run != p.oauth {
		return nil // an older, cancelled attempt
	}
	switch ev := m.ev.(type) {
	case oauthURLEv:
		m.run.url = ev.url
		return listenOAuth(m.run)
	case oauthNoteEv:
		m.run.note = ev.note
		return listenOAuth(m.run)
	case oauthDoneEv:
		if ev.err != nil {
			p.oauth = nil
			m.run.cancel()
			if errors.Is(ev.err, context.Canceled) {
				return p.setNotice("Sign-in cancelled")
			}
			p.oauthErr = oauthMessage(ev.err)
			return nil
		}
		tokens := p.deps.Tokens
		return func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if tokens == nil {
				return oauthSavedMsg{run: m.run, tok: ev.tok, err: errors.New("no secret store is available, so the token was not saved")}
			}
			return oauthSavedMsg{run: m.run, tok: ev.tok, err: tokens.Save(ctx, m.run.id, ev.tok)}
		}
	}
	return nil
}

// oauthMessage turns a fetch error into a short, calm sentence.
func oauthMessage(err error) string {
	switch {
	case errors.Is(err, oauth2.ErrTimeout):
		return "Timed out waiting for the browser. Check the sign-in page, then select Get new token and press Enter to try again."
	case errors.Is(err, context.Canceled):
		return "Sign-in was cancelled. Select Get new token and press Enter to try again."
	}
	msg := err.Error()
	msg = strings.TrimPrefix(msg, "oauth2: ")
	if len(msg) > 140 {
		msg = msg[:140]
	}
	return "Could not get a token: " + msg + ". Check the OAuth settings, then select Get new token and press Enter to try again."
}

func (p *Pane) onOAuthSaved(m oauthSavedMsg) tea.Cmd {
	if m.run != p.oauth {
		return nil
	}
	p.oauth = nil
	m.run.cancel()
	if m.err != nil {
		p.oauthErr = oauthMessage(m.err)
		return nil
	}
	req := p.req
	if req == nil || req.ID != m.run.id {
		if p.coll != nil {
			if it, _, _, ok := p.coll.Find(m.run.id); ok {
				req, _ = it.(*model.Request)
			}
		}
	}
	if req == nil || req.Auth == nil || req.Auth.OAuth2 == nil {
		return p.setNotice("The request changed; the token was saved but not attached")
	}
	req.Auth.OAuth2.Token = m.tok
	cmds := []tea.Cmd{msgCmd(tui.ToastMsg{Kind: ui.ToastSuccess, Text: "OAuth 2.0 token saved"})}
	if p.req != nil && p.req.ID == req.ID {
		cmds = append(cmds, p.touched())
	}
	return tea.Batch(cmds...)
}

func (p *Pane) cancelOAuth() tea.Cmd {
	if p.oauth == nil {
		return nil
	}
	p.oauth.cancel()
	p.oauth = nil
	return p.setNotice("Sign-in cancelled")
}

func (p *Pane) copyAuthURL() tea.Cmd {
	if p.oauth == nil || p.oauth.url == "" {
		return nil
	}
	return tea.Batch(p.deps.clip(p.oauth.url), p.setNotice("Copied the sign-in link"))
}

// oauthWaitView is the calm waiting state: what is happening, the link to open
// by hand, and how to stop.
func (p *Pane) oauthWaitView(w, h int) string {
	th := p.th
	run := p.oauth
	elapsed := p.deps.now().Sub(run.started)
	frames := th.NewSpinner().Spinner.Frames
	frame := ""
	if !th.ASCII && len(frames) > 0 {
		frame = th.Render(ui.StatusInfo, frames[p.send.frame%len(frames)])
	}
	head := th.Busy(frame, "Waiting for you to sign in "+formatElapsed(elapsed.Truncate(time.Second)))
	lines := []string{head, ""}
	if run.url == "" {
		lines = append(lines, th.Render(ui.TextSecondary, "Preparing the sign-in link"+th.Glyphs.Ellipsis))
	} else {
		lines = append(lines,
			th.Render(ui.TextSecondary, ui.Wrap("Your browser should have opened. If it did not, open this link:", w)), "",
			th.Render(ui.TextPrimary, ui.Wrap(run.url, w)))
	}
	if run.note != "" {
		lines = append(lines, "", th.Render(ui.TextQuiet, ui.Wrap(run.note, w)))
	}
	lines = append(lines, "", th.Render(ui.TextQuiet, "The token arrives on a local callback; nothing else is shared."))
	return th.Block(strings.Join(lines, "\n"), w, h)
}
