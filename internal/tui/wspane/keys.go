package wspane

import (
	tea "charm.land/bubbletea/v2"
	"github.com/shubham/postctl/internal/model"
	"github.com/shubham/postctl/internal/tui/vimtext"
	"github.com/shubham/postctl/internal/ui"
)

// setZone moves the cursor between the URL bar, the tab bar and the content.
// The URL bar and the composer are fields: arriving there puts the cursor in
// them in normal mode.
func (p *Pane) setZone(z zone) {
	if p.zone == zBar && z != zBar {
		p.url.Blur()
	}
	p.zone = z
	p.urlE = z == zBar
	if p.urlE {
		p.fitURL()
		p.url.Begin("")
	}
	if z == zBody && p.tab == tabMessages && p.sect == sComposer {
		p.fitComposer()
		p.comp.Begin("")
	}
	p.syncFocus()
}

// enterBody moves the cursor from the tab bar into the tab's content.
func (p *Pane) enterBody() {
	p.setZone(zBody)
	switch p.tab {
	case tabAuth:
		p.auth.enter()
	case tabSettings:
		p.settings.enter()
	}
}

func (p *Pane) onKey(m tea.KeyPressMsg) tea.Cmd {
	if !p.editable() {
		return nil
	}
	if p.confirm != nil {
		return p.confirmKey(m)
	}
	cmd := p.dispatch(m)
	p.syncFocus()
	return cmd
}

func (p *Pane) dispatch(m tea.KeyPressMsg) tea.Cmd {
	switch m.String() {
	case "ctrl+s", "ctrl+enter":
		return p.primary()
	case "esc":
		// The bottom of the Esc ladder: a field gets it first (insert to
		// normal, normal to leaving the field); only with no field open does
		// it cancel a connection attempt.
		if l := p.peek(); l != nil && l.state == stConnecting && !p.searching && p.rename == nil && p.activeField() == nil {
			return p.cancelConnect()
		}
	}
	switch p.zone {
	case zBar:
		return p.barKey(m)
	case zTabs:
		return p.tabsKey(m)
	}
	return p.bodyKey(m)
}

// primary is the send key: connect when there is no connection, otherwise
// send the composer's message.
func (p *Pane) primary() tea.Cmd {
	l := p.cur()
	switch l.state {
	case stIdle:
		return p.withFlush(p.connect())
	case stOpen:
		return p.withFlush(p.sendComposer())
	}
	return nil
}

// withFlush writes the pending autosave together with cmd, so the file on disk
// is the request that was used.
func (p *Pane) withFlush(cmd tea.Cmd) tea.Cmd {
	if p.req == nil {
		return cmd
	}
	return tea.Batch(p.flush(p.req.ID), cmd)
}

// ---- URL bar ---------------------------------------------------------------------

func (p *Pane) barKey(m tea.KeyPressMsg) tea.Cmd {
	if m.String() == "down" && !p.url.Completing() {
		p.setZone(zTabs)
		return nil
	}
	p.fitURL()
	res := p.url.Update(m)
	cmds := []tea.Cmd{res.Cmd}
	if res.Changed {
		cmds = append(cmds, p.urlEdited())
	}
	switch res.Event.Kind {
	case vimtext.Submit: // Enter connects or disconnects in normal and insert mode
		cmds = append(cmds, p.toggleConnect())
	case vimtext.Leave:
		p.setZone(zTabs)
	case vimtext.Unhandled:
		if res.Event.Key.String() == "tab" {
			p.setZone(zTabs)
		} else {
			cmds = append(cmds, forward(p.url, res.Event))
		}
	default:
		cmds = append(cmds, forward(p.url, res.Event))
	}
	return tea.Batch(cmds...)
}

// urlEdited copies the field into the request, keeping disabled parameters
// and path variable values that the raw text cannot carry.
func (p *Pane) urlEdited() tea.Cmd {
	applyRawURL(&p.req.URL, p.url.Text())
	p.params.reload(p)
	return p.touched()
}

// applyRawURL sets u from raw text. Disabled query rows stay where they are;
// enabled rows take their values from the text in order; path variables keep
// the values already entered.
func applyRawURL(u *model.URL, raw string) {
	parsed := model.ParseURL(raw)
	var query []model.KV
	j := 0
	for _, q := range u.Query {
		if q.Disabled {
			query = append(query, q)
			continue
		}
		if j < len(parsed.Query) {
			nq := parsed.Query[j]
			nq.Description = q.Description
			query = append(query, nq)
			j++
		}
	}
	query = append(query, parsed.Query[j:]...)
	old := map[string]model.Variable{}
	for _, v := range u.PathVars {
		old[v.Key] = v
	}
	for i, v := range parsed.PathVars {
		if o, ok := old[v.Key]; ok {
			parsed.PathVars[i] = o
		}
	}
	u.Raw = raw
	u.Query = query
	u.PathVars = parsed.PathVars
}

// ---- tab bar ---------------------------------------------------------------------

func (p *Pane) tabsKey(m tea.KeyPressMsg) tea.Cmd {
	switch m.String() {
	case "left":
		p.tab = (p.tab + numTabs - 1) % numTabs
	case "right":
		p.tab = (p.tab + 1) % numTabs
	case "home":
		p.tab = 0
	case "end":
		p.tab = numTabs - 1
	case "up":
		p.setZone(zBar)
	case "down", "enter":
		p.enterBody()
	case "c":
		return p.toggleConnect()
	}
	return nil
}

// ---- tab content -----------------------------------------------------------------

func (p *Pane) bodyKey(m tea.KeyPressMsg) tea.Cmd {
	var handled, changed, editing bool
	var fcmd tea.Cmd
	switch p.tab {
	case tabMessages:
		return p.messagesKey(m)
	case tabParams:
		handled, changed, fcmd = p.params.key(p, m)
		editing = p.params.editing()
	case tabHeaders:
		handled, changed, fcmd = p.headers.key(p, m)
		editing = p.headers.editing()
	case tabAuth:
		handled, changed, fcmd = p.auth.key(p, m)
		editing = p.auth.editing()
	default:
		handled, changed, fcmd = p.settings.key(p, m)
		editing = p.settings.editing()
	}
	cmd := fcmd
	if changed {
		cmd = batch(fcmd, p.touched())
		p.syncURL()
	}
	if handled || editing {
		return cmd
	}
	switch m.String() {
	case "up", "esc":
		p.setZone(zTabs)
	case "left":
		p.tab = (p.tab + numTabs - 1) % numTabs
		p.enterBody()
	case "right":
		p.tab = (p.tab + 1) % numTabs
		p.enterBody()
	}
	return cmd
}

func (p *Pane) onPaste(m tea.PasteMsg) tea.Cmd {
	if !p.editable() || p.confirm != nil {
		return nil
	}
	switch {
	case p.zone == zBar:
		p.fitURL()
		if p.url.Paste(m).Changed {
			return p.urlEdited()
		}
		return nil
	case p.searching:
		p.fitPrompts()
		if l := p.cur(); p.search.Paste(m).Changed && p.search.Text() != l.log.query {
			l.log.query = p.search.Text()
			l.log.refilter()
		}
		return nil
	case p.rename != nil:
		p.fitPrompts()
		p.rename.Paste(m)
		return nil
	case p.zone != zBody:
		return nil
	}
	var changed bool
	switch p.tab {
	case tabMessages:
		if p.sect == sComposer {
			p.fitComposer()
			if p.comp.Paste(m).Changed {
				return p.composerEdited()
			}
		}
		return nil
	case tabParams:
		changed = p.params.paste(p, m)
	case tabHeaders:
		changed = p.headers.paste(p, m)
	case tabAuth:
		changed = p.auth.f.paste(p, func() []formRow { return p.auth.rows(p) }, m)
	default:
		changed = p.settings.f.paste(p, func() []formRow { return p.settings.rows(p) }, m)
	}
	if changed {
		p.syncURL()
		return p.touched()
	}
	return nil
}

// ---- hints -------------------------------------------------------------------------

// KeyHints implements tui.Pane.
func (p *Pane) KeyHints() []ui.Hint {
	if !p.editable() {
		return nil
	}
	if p.confirm != nil {
		return []ui.Hint{{Key: "Enter", Label: "Confirm", Priority: 1}, {Key: "Esc", Label: "Cancel", Priority: 1}}
	}
	l := p.peek()
	state := stIdle
	if l != nil {
		state = l.state
	}
	primary := ui.Hint{Key: "Ctrl-S", Label: "Connect", Priority: 1}
	if p.enhanced {
		primary.Key = "Ctrl-Enter"
	}
	switch state {
	case stConnecting:
		return []ui.Hint{{Key: "Esc", Label: "Cancel", Priority: 1}}
	case stOpen:
		primary.Label = "Send"
	case stClosing:
		return nil
	}
	var hs []ui.Hint
	switch p.zone {
	case zBar:
		label := "Connect"
		if state == stOpen {
			label = "Disconnect"
		}
		hs = append(fieldHints(p.url, "Tabs"), ui.Hint{Key: "Enter", Label: label, Priority: 1}, primary)
	case zTabs:
		label := "Connect"
		if state == stOpen {
			label = "Disconnect"
		}
		hs = []ui.Hint{
			{Key: "[ ]", Label: "Tab", Priority: 2},
			{Key: "↓", Label: "Edit", Priority: 2},
			{Key: "c", Label: label, Priority: 3},
			primary,
		}
	default:
		hs = append(p.bodyHints(state), primary)
	}
	return hs
}

func (p *Pane) bodyHints(state connState) []ui.Hint {
	switch p.tab {
	case tabParams:
		return p.params.hints()
	case tabHeaders:
		return p.headers.hints()
	case tabAuth:
		return p.auth.hints()
	case tabSettings:
		return p.settings.hints()
	}
	switch p.sect {
	case sComposer:
		next := "Log"
		if len(p.messages()) > 0 {
			next = "Saved"
		}
		hs := fieldHints(p.comp, "Tabs")
		if !p.comp.Typing() {
			hs = append(hs, ui.Hint{Key: "Tab", Label: next, Priority: 3})
		} else {
			hs = append(hs, ui.Hint{Key: "Ctrl-R", Label: "Format JSON", Priority: 5})
		}
		return append(hs,
			ui.Hint{Key: "Ctrl-T", Label: "Format", Priority: 4},
			ui.Hint{Key: "Ctrl-X", Label: "Save message", Priority: 4})
	case sSaved:
		if p.rename != nil {
			return append([]ui.Hint{{Key: "Enter", Label: "Save name", Priority: 1}}, fieldHints(p.rename, "Cancel")...)
		}
		send := "Send"
		if state != stOpen {
			send = "Send (not connected)"
		}
		return []ui.Hint{
			{Key: "Enter", Label: send, Priority: 1},
			{Key: "e", Label: "Edit in composer", Priority: 3},
			{Key: "r", Label: "Rename", Priority: 4},
			{Key: "d", Label: "Delete", Priority: 4},
			{Key: "Esc", Label: "Composer", Priority: 2},
		}
	}
	switch {
	case p.searching:
		return append([]ui.Hint{{Key: "Enter", Label: "Keep results", Priority: 1}}, fieldHints(p.search, "Clear")...)
	case p.detail != nil:
		return []ui.Hint{
			{Key: "Esc", Label: "Back", Priority: 1},
			{Key: "↑↓", Label: "Scroll", Priority: 2},
			{Key: "←→", Label: "Message", Priority: 2},
			{Key: "c", Label: "Copy", Priority: 3},
		}
	}
	return []ui.Hint{
		{Key: "Enter", Label: "Open", Priority: 2},
		{Key: "↑↓", Label: "Select", Priority: 3},
		{Key: "f", Label: "Filter", Priority: 3},
		{Key: "/", Label: "Search", Priority: 3},
		{Key: "c", Label: "Copy", Priority: 4},
		{Key: "p", Label: "Ping", Priority: 5},
		{Key: "x", Label: "Clear", Priority: 6},
		{Key: "Esc", Label: "Composer", Priority: 2},
	}
}
