// Package request is the Request pane: the URL bar, the seven tabs, autosave,
// undo, the OAuth 2.0 flow, curl copy and import, and the send wiring.
package request

import (
	"context"
	"net/url"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/shubham/postctl/internal/httpx"
	"github.com/shubham/postctl/internal/model"
	"github.com/shubham/postctl/internal/tui"
	"github.com/shubham/postctl/internal/ui"
	"github.com/shubham/postctl/internal/vars"
)

// curlMask replaces secrets in copied commands. It is ASCII so a pasted
// command never carries a look-alike glyph.
const curlMask = "****"

// curlDoneMsg carries a finished "copy as curl" back to Update.
type curlDoneMsg struct {
	text   string
	reveal bool
	err    error
}

// copyCurl builds the curl command in a command (revealing secrets needs the
// keychain) and hands it to the clipboard when done. By default every secret
// variable and every auth credential is masked.
func (p *Pane) copyCurl(reveal bool) tea.Cmd {
	if p.req == nil {
		return nil
	}
	req := p.req.Clone()
	chain := p.chain
	var globals, coll, envVars []model.Variable
	if p.ws != nil && p.ws.Globals != nil {
		globals = append(globals, p.ws.Globals.Variables...)
	}
	for _, c := range chain {
		coll = append(coll, containerVars(c)...)
	}
	if p.activeEnv != nil {
		envVars = append(envVars, p.activeEnv.Variables...)
	}
	hyd, _ := p.env.Backend.(tui.Hydrator)
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if reveal && hyd != nil {
			for _, vs := range [][]model.Variable{globals, coll, envVars} {
				if err := hyd.HydrateVariables(ctx, vs); err != nil {
					return curlDoneMsg{err: err, reveal: reveal}
				}
			}
		} else if !reveal {
			globals, coll, envVars = maskVars(globals, curlMask), maskVars(coll, curlMask), maskVars(envVars, curlMask)
		}
		store := vars.New(globals, coll, envVars, nil)
		resolved, err := vars.ResolveRequest(req, chain, store)
		if err != nil {
			return curlDoneMsg{err: err, reveal: reveal}
		}
		if reveal && hyd != nil && resolved.Auth != nil && resolved.Auth.Type == model.AuthOAuth2 {
			_ = hyd.HydrateAuth(ctx, resolved.Auth)
		}
		return curlDoneMsg{text: buildCurl(resolved, reveal), reveal: reveal}
	}
}

func (p *Pane) onCurlDone(m curlDoneMsg) tea.Cmd {
	if m.err != nil {
		return p.setNotice("Could not build the curl command")
	}
	note := "Copied as curl, secrets masked"
	if m.reveal {
		note = "Copied as curl, with secrets"
	}
	return tea.Batch(p.deps.clip(m.text), p.setNotice(note))
}

// buildCurl renders a resolved request as curl, adding the authentication
// httpx.Curl leaves out.
func buildCurl(r *model.Request, reveal bool) string {
	args, query := authArgs(r.Auth, reveal)
	if query != nil {
		sep := "&"
		if !strings.Contains(r.URL.Raw, "?") {
			sep = "?"
		}
		r.URL.Raw += sep + url.QueryEscape(query[0]) + "=" + url.QueryEscape(query[1])
	}
	out := httpx.Curl(r)
	for _, a := range args {
		out += " " + a
	}
	return out
}

func shq(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// authArgs returns the curl arguments for auth and, for an API key sent in the
// query string, the name and value to append to the URL. Credentials are
// masked unless reveal.
func authArgs(a *model.Auth, reveal bool) (args []string, query []string) {
	if a == nil {
		return nil, nil
	}
	secret := func(s string) string {
		if reveal {
			return s
		}
		return curlMask
	}
	user := func(u, pw string) string { return shq(u + ":" + secret(pw)) }
	switch a.Type {
	case model.AuthBearer:
		if a.Bearer != nil {
			args = append(args, "--header", shq("Authorization: Bearer "+secret(a.Bearer.Token)))
		}
	case model.AuthBasic:
		if a.Basic != nil {
			args = append(args, "--user", user(a.Basic.Username, a.Basic.Password))
		}
	case model.AuthDigest:
		if a.Digest != nil {
			args = append(args, "--digest", "--user", user(a.Digest.Username, a.Digest.Password))
		}
	case model.AuthNTLM:
		if a.NTLM != nil {
			args = append(args, "--ntlm", "--user", user(a.NTLM.Username, a.NTLM.Password))
		}
	case model.AuthAPIKey:
		if a.APIKey != nil && a.APIKey.Key != "" {
			if a.APIKey.In == model.APIKeyQuery {
				query = []string{a.APIKey.Key, secret(a.APIKey.Value)}
			} else {
				args = append(args, "--header", shq(a.APIKey.Key+": "+secret(a.APIKey.Value)))
			}
		}
	case model.AuthOAuth2:
		if a.OAuth2 != nil && a.OAuth2.Token != nil && (a.OAuth2.Token.AccessToken != "" || !reveal) {
			prefix := a.OAuth2.HeaderPrefix
			if prefix == "" {
				prefix = "Bearer"
			}
			tok := secret(a.OAuth2.Token.AccessToken)
			if a.OAuth2.AddTokenTo == model.TokenInQuery {
				query = []string{"access_token", tok}
			} else {
				args = append(args, "--header", shq("Authorization: "+prefix+" "+tok))
			}
		}
	case model.AuthAWSV4:
		if a.AWSV4 != nil {
			args = append(args, "--aws-sigv4", shq("aws:amz:"+a.AWSV4.Region+":"+a.AWSV4.Service),
				"--user", user(a.AWSV4.AccessKey, a.AWSV4.SecretKey))
		}
	}
	return args, query
}

// ---- curl import ------------------------------------------------------------

// importOffer is the "Import as request" prompt shown after pasting a curl
// command.
type importOffer struct {
	pc      *parsedCurl
	text    string
	inBar   bool
	confirm ui.Confirm
}

func (p *Pane) offerImport(pc *parsedCurl, text string, inBar bool) {
	c := ui.NewConfirm("Import as request", "Replace this request with the curl command? "+pc.summary()+"\nYou can undo with Ctrl-Z.", "Import as request", false)
	c.Choices = []string{"Paste as text", "Import as request"}
	c.Focus = 1
	p.confirm = &importOffer{pc: pc, text: text, inBar: inBar, confirm: c}
}

func (p *Pane) confirmKey(m tea.KeyPressMsg) tea.Cmd {
	o := p.confirm
	switch m.String() {
	case "left", "shift+tab", "up":
		o.confirm.Move(-1)
	case "right", "tab", "down":
		o.confirm.Move(1)
	case "esc":
		p.confirm = nil
	case "enter":
		p.confirm = nil
		if o.confirm.Accepted() {
			return p.importCurl(o.pc)
		}
		if o.inBar {
			before := p.url.Text()
			pre := p.req.Clone()
			p.url.Insert(o.text)
			if p.url.Text() != before {
				return p.urlEdited(pre)
			}
		}
	}
	return nil
}

func (p *Pane) importCurl(pc *parsedCurl) tea.Cmd {
	pre := p.req.Clone()
	p.stopAll()
	pc.applyTo(p.req)
	p.reloadAll()
	p.setZone(zTabs)
	return tea.Batch(p.record(pre, true), p.setNotice("Imported curl command"))
}

func (p *Pane) confirmView(w, h int) string {
	width := min(w, 64)
	body := p.th.ConfirmView(p.confirm.confirm, width)
	return p.th.Block(body, w, h)
}

// ---- palette ------------------------------------------------------------------

// Actions implements tui.ActionSource.
func (p *Pane) Actions() []tui.Action {
	ok := p.editable
	sendKeys := "Ctrl-S"
	if p.enhanced {
		sendKeys = "Ctrl-Enter"
	}
	acts := []tui.Action{
		{ID: "request.send", Title: "Send request", Group: "Request", Keys: sendKeys, Enabled: ok, Run: p.sendNow},
		{ID: "request.copyCurl", Title: "Copy as curl", Group: "Request", Keys: "y", Enabled: ok, Run: func() tea.Cmd { return p.copyCurl(false) }},
		{ID: "request.copyCurlSecrets", Title: "Copy as curl (with secrets)", Group: "Request", Enabled: ok, Run: func() tea.Cmd { return p.copyCurl(true) }},
		{ID: "request.undo", Title: "Undo edit", Group: "Request", Keys: "Ctrl-Z", Enabled: ok, Run: p.undoStep},
		{ID: "request.redo", Title: "Redo edit", Group: "Request", Keys: "Ctrl-Y", Enabled: ok, Run: p.redoStep},
		{ID: "request.editor", Title: "Edit in $EDITOR", Group: "Request", Keys: "Ctrl-O", Enabled: ok, Run: p.openEditor},
		{ID: "request.focusURL", Title: "Edit URL", Group: "Request", Keys: "Ctrl-L", Enabled: ok, Run: func() tea.Cmd {
			p.stopViews()
			p.setZone(zBar)
			return msgCmd(tui.FocusPaneMsg{Slot: tui.SlotRequest})
		}},
		{ID: "request.getToken", Title: "Get new OAuth 2.0 token", Group: "Request", Run: p.getToken,
			Enabled: func() bool {
				return p.editable() && p.req.Auth != nil && p.req.Auth.Type == model.AuthOAuth2
			}},
		{ID: "request.formatJSON", Title: "Format JSON body", Group: "Request", Run: p.formatBody,
			Enabled: func() bool {
				return p.editable() && p.req.Body.Mode == model.BodyRaw && p.req.Body.Language == "json"
			}},
	}
	for i := range numTabs {
		acts = append(acts, tui.Action{
			ID: "request.tab." + strings.ToLower(tabNames[i]), Title: "Go to " + tabNames[i] + " tab", Group: "Request", Enabled: ok,
			Run: func() tea.Cmd {
				p.stopViews()
				p.tab = i
				p.setZone(zBody)
				p.views[i].enter(p)
				return msgCmd(tui.FocusPaneMsg{Slot: tui.SlotRequest})
			},
		})
	}
	return acts
}
