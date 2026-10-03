package request

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/shubham/postctl/internal/model"
	"github.com/shubham/postctl/internal/tui/vimtext"
	"github.com/shubham/postctl/internal/ui"
)

// methodSel is the open method selector.
type methodSel struct {
	methods []string
	idx     int
	orig    string
}

func newMethodSel(current string) *methodSel {
	ms := append([]string(nil), methods...)
	cur := strings.ToUpper(current)
	if cur == "" {
		cur = "GET"
	}
	idx := -1
	for i, m := range ms {
		if m == cur {
			idx = i
		}
	}
	if idx < 0 {
		ms = append(ms, cur) // a custom method stays selectable
		idx = len(ms) - 1
	}
	return &methodSel{methods: ms, idx: idx, orig: cur}
}

// editable reports whether there is an HTTP request to edit.
func (p *Pane) editable() bool {
	return p.req != nil && p.ref.Problem == nil && (p.req.Kind == "" || p.req.Kind == model.KindHTTP)
}

// setZone moves the cursor between the URL bar, the tab bar and the content.
// The URL bar is a field: arriving there puts the cursor in it in normal mode.
func (p *Pane) setZone(z zone) {
	if p.zone == zBar && z != zBar {
		p.url.Blur()
		p.endField()
	}
	p.zone = z
	p.urlE = z == zBar
	if p.urlE {
		p.focusURL("")
	}
}

// focusURL gives the URL field the keyboard: start is "" for normal mode or
// "i", "a", "A", "I" to begin that insert command.
func (p *Pane) focusURL(start string) {
	p.fitURL()
	p.url.Begin(start)
	if !p.focused {
		p.url.Blur() // the cursor shows once the pane has focus
	}
}

func (p *Pane) onKey(m tea.KeyPressMsg) tea.Cmd {
	if !p.editable() {
		return nil
	}
	if p.confirm != nil {
		return p.confirmKey(m)
	}
	s := m.String()
	p.notice = "" // a message lasts until the next key
	switch s {
	case "ctrl+s", "ctrl+enter":
		return p.sendNow()
	case "ctrl+z":
		return p.undoStep()
	case "ctrl+y":
		return p.redoStep()
	case "u":
		if !p.Editing() {
			return p.undoStep()
		}
	case "ctrl+r":
		if !p.Editing() {
			return p.redoStep()
		}
	case "esc":
		// The bottom of the Esc ladder: a field gets it first (insert to
		// normal, normal to leaving the field); only with no field open does
		// it cancel a sign-in or a running send.
		if !p.Editing() {
			if p.oauth != nil {
				return p.cancelOAuth()
			}
			if p.isSending() {
				return p.cancelSend()
			}
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

// ---- URL bar --------------------------------------------------------------

func (p *Pane) barKey(m tea.KeyPressMsg) tea.Cmd {
	if p.msel != nil {
		return p.methodKey(m)
	}
	s := m.String()
	if s == "down" && !p.url.Completing() {
		p.setZone(zTabs)
		return nil
	}
	p.fitURL()
	before := p.req.Clone()
	res := p.url.Update(m)
	var cmds []tea.Cmd
	cmds = append(cmds, res.Cmd)
	if res.Changed {
		cmds = append(cmds, p.urlEdited(before))
	}
	switch res.Event.Kind {
	case vimtext.Submit: // Enter sends from the URL bar in normal and insert mode
		cmds = append(cmds, p.sendNow())
	case vimtext.Leave:
		p.setZone(zTabs)
	case vimtext.Unhandled:
		switch res.Event.Key.String() {
		case "tab":
			p.setZone(zTabs)
		case "shift+tab":
			p.msel = newMethodSel(p.req.Method)
		default:
			cmds = append(cmds, forward(p.url, res.Event))
		}
	default:
		cmds = append(cmds, forward(p.url, res.Event))
	}
	return tea.Batch(cmds...)
}

// urlEdited copies the field into the request, keeping disabled parameters
// and path variable values that the raw text cannot carry.
func (p *Pane) urlEdited(pre *model.Request) tea.Cmd {
	applyRawURL(&p.req.URL, p.url.Text())
	p.views[tabParams].reload(p)
	return p.recordIn(p.url, pre)
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

func (p *Pane) methodKey(m tea.KeyPressMsg) tea.Cmd {
	ms := p.msel
	n := len(ms.methods)
	switch s := m.String(); s {
	case "left", "up":
		ms.idx = (ms.idx + n - 1) % n
	case "right", "down":
		ms.idx = (ms.idx + 1) % n
	case "home":
		ms.idx = 0
	case "end":
		ms.idx = n - 1
	case "esc":
		p.msel = nil
	case "enter", "tab":
		method := ms.methods[ms.idx]
		p.msel = nil
		if method == p.req.Method || (p.req.Method == "" && method == "GET") {
			return nil
		}
		pre := p.req.Clone()
		p.req.Method = method
		return p.record(pre, true)
	default:
		// Typeahead: the next method starting with the letter.
		if len(m.Text) == 1 {
			c := strings.ToUpper(m.Text)
			for k := 1; k <= n; k++ {
				i := (ms.idx + k) % n
				if strings.HasPrefix(ms.methods[i], c) {
					ms.idx = i
					break
				}
			}
		}
	}
	return nil
}

// ---- tab bar ----------------------------------------------------------------

func (p *Pane) tabsKey(m tea.KeyPressMsg) tea.Cmd {
	if p.msel != nil {
		return p.methodKey(m)
	}
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
		p.setZone(zBody)
		p.views[p.tab].enter(p)
	case "m":
		p.msel = newMethodSel(p.req.Method)
	}
	return nil
}

// ---- tab content ------------------------------------------------------------

func (p *Pane) bodyKey(m tea.KeyPressMsg) tea.Cmd {
	v := p.views[p.tab]
	wasField := v.field()
	pre := p.req.Clone()
	handled, changed, cmd := v.key(p, m)
	var cmds []tea.Cmd
	if cmd != nil {
		cmds = append(cmds, cmd)
	}
	if changed {
		if wasField != nil {
			cmds = append(cmds, p.recordIn(wasField, pre))
		} else {
			cmds = append(cmds, p.record(pre, true))
		}
		p.afterEdit()
	}
	if v.field() != wasField {
		p.endField() // the field was left or another one opened
	}
	if handled {
		return tea.Batch(cmds...)
	}
	if !v.editing() {
		switch m.String() {
		case "up", "esc":
			p.setZone(zTabs)
		case "left":
			p.tab = (p.tab + numTabs - 1) % numTabs
			p.views[p.tab].enter(p)
		case "right":
			p.tab = (p.tab + 1) % numTabs
			p.views[p.tab].enter(p)
		}
	}
	return tea.Batch(cmds...)
}

// afterEdit refreshes what depends on the request after any edit.
func (p *Pane) afterEdit() {
	p.syncURL()
}

func (p *Pane) onPaste(m tea.PasteMsg) tea.Cmd {
	if !p.editable() || p.confirm != nil {
		return nil
	}
	text := m.Content
	inBar := p.zone == zBar
	if looksLikeCurl(text) && (inBar || !p.Editing()) {
		if pc, err := parseCurl(text); err == nil {
			p.offerImport(pc, text, inBar)
			return nil
		}
	}
	if inBar && p.msel == nil {
		pre := p.req.Clone()
		p.fitURL()
		if p.url.Paste(m).Changed {
			return p.urlEdited(pre)
		}
		return nil
	}
	v := p.views[p.tab]
	if p.zone == zBody && v.editing() {
		pre := p.req.Clone()
		if v.paste(p, m) {
			c := p.record(pre, false)
			p.afterEdit()
			return c
		}
	}
	return nil
}

// KeyHints implements tui.Pane.
func (p *Pane) KeyHints() []ui.Hint {
	send := ui.Hint{Key: "Ctrl-S", Label: "Send", Priority: 1}
	if p.enhanced {
		send.Key = "Ctrl-Enter"
	}
	if p.isSending() {
		return []ui.Hint{{Key: "Esc", Label: "Cancel", Priority: 1}}
	}
	if !p.editable() {
		return nil
	}
	if p.confirm != nil {
		return []ui.Hint{{Key: "Enter", Label: "Confirm", Priority: 1}, {Key: "Esc", Label: "Cancel", Priority: 1}}
	}
	if p.msel != nil {
		return []ui.Hint{{Key: "←→", Label: "Method", Priority: 2}, {Key: "Enter", Label: "Pick", Priority: 1}, {Key: "Esc", Label: "Cancel", Priority: 1}}
	}
	var hs []ui.Hint
	switch p.zone {
	case zBar:
		hs = fieldHints(p.url, "Tabs")
		hs = append(hs, ui.Hint{Key: "Enter", Label: "Send", Priority: 1})
		if !p.url.Typing() {
			hs = append(hs, ui.Hint{Key: "Shift-Tab", Label: "Method", Priority: 3})
		}
		hs = append(hs, send)
	case zTabs:
		hs = []ui.Hint{
			{Key: "[ ]", Label: "Tab", Priority: 2},
			{Key: "↓", Label: "Edit", Priority: 2},
			{Key: "m", Label: "Method", Priority: 4},
			{Key: "Space y", Label: "Copy curl", Priority: 4},
			send,
		}
	default:
		hs = append(hs, p.views[p.tab].hints(p)...)
		hs = append(hs, send)
	}
	if p.Editing() {
		hs = append(hs, ui.Hint{Key: "Ctrl-Z", Label: "Undo", Priority: 7})
	} else {
		hs = append(hs, ui.Hint{Key: "u", Label: "Undo", Priority: 7})
	}
	return hs
}
