package request

import (
	tea "charm.land/bubbletea/v2"
	"github.com/shubham/postctl/internal/model"
	"github.com/shubham/postctl/internal/tui"
)

// onSelection binds the pane to the selected request. The shell sends the
// message again after a workspace reload (pointers change), which rebinds the
// same request without losing unsaved edits.
func (p *Pane) onSelection(m tui.SelectionMsg) tea.Cmd {
	ref := m.Ref
	old := p.req
	if old != nil && ref.Request != nil && old.ID == ref.Request.ID {
		p.rebind(ref)
		return nil
	}
	var cmd tea.Cmd
	if old != nil {
		cmd = p.flush(old.ID)
	}
	p.stopAll()
	p.ref = ref
	p.req = ref.Request
	p.coll = ref.Collection
	p.zone = zTabs
	p.msel = nil
	p.confirm = nil
	p.notice = ""
	p.oauthErr = ""
	p.refreshChain()
	p.reloadAll()
	if m.Open && p.req != nil && p.req.URL.Raw == "" {
		// A brand-new request: start at the URL, typing.
		p.setZone(zBar)
		p.focusURL("i")
	}
	return cmd
}

// rebind swaps in a fresh pointer for the same request after a reload. Unsaved
// edits win over what the reload found on disk.
func (p *Pane) rebind(ref tui.RequestRef) {
	if ref.Request != p.req && p.dirtyFor(p.req.ID) {
		*ref.Request = *p.req.Clone()
	}
	changed := ref.Request != p.req
	p.ref = ref
	p.req = ref.Request
	p.coll = ref.Collection
	p.refreshChain()
	if changed && !p.Editing() {
		p.reloadAll()
	}
}

// refreshChain recomputes the container chain and variable scope.
func (p *Pane) refreshChain() {
	p.chain = nil
	if p.coll != nil && p.req != nil {
		p.chain = chainOf(p.coll, p.req.ID)
	}
	p.rebuildScope()
}

// rebuildScope rebuilds the masked variable scope after the environment,
// workspace or a variable changed.
func (p *Pane) rebuildScope() {
	p.sc = newScope(p.ws, p.chain, p.activeEnv, p.th.Glyphs.Mask, false)
}

// onReloaded keeps the workspace for globals; the request is rebound by the
// SelectionMsg that follows.
func (p *Pane) onReloaded(ws *model.Workspace) tea.Cmd {
	p.ws = ws
	p.rebuildScope()
	return nil
}

// stopAll leaves every edit mode.
func (p *Pane) stopAll() {
	p.urlE = false
	p.url.Blur()
	p.msel = nil
	p.endField()
	for _, v := range p.views {
		v.stop()
	}
}

// reloadAll refreshes every widget from the request.
func (p *Pane) reloadAll() {
	if p.req == nil {
		p.url.Set("")
		return
	}
	p.url.Set(p.req.URL.Raw)
	p.fitURL()
	for _, v := range p.views {
		v.reload(p)
	}
}

// syncURL refreshes the URL field after a Params edit changed the URL.
func (p *Pane) syncURL() {
	if p.req != nil && !p.urlE {
		p.url.Set(p.req.URL.Raw)
	}
}
