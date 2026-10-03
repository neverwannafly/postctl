package wspane

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/shubham/postctl/internal/tui/vimtext"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/shubham/postctl/internal/model"
	"github.com/shubham/postctl/internal/ui"
	"github.com/shubham/postctl/internal/wsx"
)

// confirmState is an open confirmation dialog and what accepting it does.
type confirmState struct {
	c   ui.Confirm
	run func() tea.Cmd
}

// detailState is the open message detail view.
type detailState struct {
	id     uint64
	lines  []string
	kind   detailKind
	w      int
	scroll int
}

// ---- composer -------------------------------------------------------------------

// composerMessage is the composer's content as a message to send.
func (p *Pane) composerMessage() model.WSMessage {
	f := p.format()
	if f == model.WSText {
		f = ""
	}
	return model.WSMessage{Body: p.comp.Text(), Format: f}
}

// jsonProblem says why body is not valid JSON once variables are resolved, or
// returns "". Variables that cannot be resolved (dynamic ones such as
// {{$guid}}) count as valid values.
func (p *Pane) jsonProblem(body string) string {
	s, _ := p.scNum.resolve(body)
	s = varRE.ReplaceAllString(s, "0")
	if err := wsx.CheckJSON(s); err != nil {
		return strings.TrimPrefix(err.Error(), wsx.ErrInvalidJSON.Error()+": ")
	}
	return ""
}

// revalidate recomputes whether the composer's message can be sent as its
// format says.
func (p *Pane) revalidate() {
	p.compErr = ""
	if p.req == nil || p.scNum == nil {
		return
	}
	body := p.comp.Text()
	switch p.format() {
	case model.WSJSON:
		p.compErr = p.jsonProblem(body)
	case model.WSBinaryBase64:
		s, _ := p.scNum.resolve(body)
		s = varRE.ReplaceAllString(s, "AAAA")
		if _, err := wsx.BuildFrame(model.WSBinaryBase64, s); err != nil {
			p.compErr = "not valid base64"
		}
	}
}

// composerEdited stores the composer text in the request's draft.
func (p *Pane) composerEdited() tea.Cmd {
	d := p.draft()
	d.Body = p.comp.Text()
	p.setDraft(d)
	p.revalidate()
	return p.touched()
}

// cycleFormat moves the composer through Text, JSON and Binary.
func (p *Pane) cycleFormat() tea.Cmd {
	next := map[model.WSFormat]model.WSFormat{
		model.WSText: model.WSJSON, model.WSJSON: model.WSBinaryBase64, model.WSBinaryBase64: model.WSText,
	}[p.format()]
	d := p.draft()
	d.Body = p.comp.Text()
	d.Format = next
	p.setDraft(d)
	p.syncComposer()
	p.revalidate()
	return tea.Batch(p.touched(), p.setNotice("Format: "+formatName(next), false))
}

func formatName(f model.WSFormat) string {
	switch f {
	case model.WSJSON:
		return "JSON"
	case model.WSBinaryBase64:
		return "Binary (base64)"
	}
	return "Text"
}

func formatTag(f model.WSFormat) string {
	switch f {
	case model.WSJSON:
		return "JSON"
	case model.WSBinaryBase64:
		return "bin"
	}
	return "text"
}

// formatJSON pretty-prints the composer's JSON, keeping {{variables}} intact.
func (p *Pane) formatJSON() tea.Cmd {
	if p.req == nil {
		return nil
	}
	body := p.comp.Text()
	out, err := formatJSON(body)
	if err != nil {
		return p.setNotice("Cannot format: "+strings.TrimPrefix(err.Error(), wsx.ErrInvalidJSON.Error()+": "), true)
	}
	if out == body {
		return p.setNotice("Already formatted", false)
	}
	p.comp.Set(out)
	return tea.Batch(p.composerEdited(), p.setNotice("Formatted JSON", false))
}

// formatJSON indents body. {{variables}} are swapped for numeric placeholders
// while parsing, so a message such as {"id": {{userId}}} formats too.
func formatJSON(body string) (string, error) {
	var vars []string
	out := varRE.ReplaceAllStringFunc(body, func(m string) string {
		vars = append(vars, m)
		return placeholder(len(vars) - 1)
	})
	for i := range vars {
		if strings.Count(body, placeholder(i)) > 0 {
			return "", errors.New("the message contains text that looks like a variable placeholder")
		}
	}
	if err := wsx.CheckJSON(out); err != nil {
		return "", err
	}
	pretty, ok := prettyJSON([]byte(out))
	if !ok {
		return "", errors.New("the message is not valid JSON")
	}
	for i, v := range vars {
		pretty = strings.ReplaceAll(pretty, placeholder(i), v)
	}
	return pretty, nil
}

func placeholder(i int) string { return fmt.Sprintf("7707%06d7", i) }

// sendComposer sends the composer's message.
func (p *Pane) sendComposer() tea.Cmd {
	m := p.composerMessage()
	if strings.TrimSpace(m.Body) == "" {
		return p.setNotice("The message is empty. Type something to send", true)
	}
	if p.compErr != "" {
		return p.setNotice("Not sent: "+p.compErr+". Ctrl-T changes the format", true)
	}
	return p.sendMessage(m)
}

// suggestName names a new template after the message: the value of its
// "type", "event" or "action" field when it has one, else "Message N".
func (p *Pane) suggestName(body string) string {
	var obj map[string]any
	if json.Unmarshal([]byte(body), &obj) == nil {
		for _, k := range []string{"type", "event", "action", "op", "method", "command", "cmd"} {
			if s, ok := obj[k].(string); ok && s != "" && len(s) <= 32 {
				return s
			}
		}
	}
	return fmt.Sprintf("Message %d", len(p.messages())+1)
}

// saveTemplate adds the composer's message to the saved list and asks for its
// name.
func (p *Pane) saveTemplate() tea.Cmd {
	m := p.composerMessage()
	if strings.TrimSpace(m.Body) == "" {
		return p.setNotice("Nothing to save. The message is empty", true)
	}
	m.ID = model.NewID()
	m.Name = p.suggestName(m.Body)
	ws := p.ensureWS()
	ws.Messages = append(ws.Messages, m)
	p.savedCur = len(ws.Messages) - 1
	p.setSection(sSaved)
	p.beginRename()
	return p.touched()
}

// ---- saved list -------------------------------------------------------------------

func (p *Pane) clampSaved() {
	n := len(p.messages())
	p.savedCur = min(max(p.savedCur, 0), max(0, n-1))
}

func (p *Pane) savedMessage() (model.WSMessage, bool) {
	ms := p.messages()
	if p.savedCur < 0 || p.savedCur >= len(ms) {
		return model.WSMessage{}, false
	}
	return ms[p.savedCur], true
}

// sendSaved sends the selected template.
func (p *Pane) sendSaved() tea.Cmd {
	m, ok := p.savedMessage()
	if !ok {
		return nil
	}
	if l := p.peek(); l == nil || l.state != stOpen {
		return p.setNotice("Not connected. Press Ctrl-S to connect", true)
	}
	if m.Format == model.WSJSON {
		if prob := p.jsonProblem(m.Body); prob != "" {
			return p.setNotice("Not sent: "+prob, true)
		}
	}
	send := p.sendMessage(m)
	return tea.Batch(send, p.setNotice("Sent \""+m.Name+"\"", false))
}

// loadSaved copies the selected template into the composer.
func (p *Pane) loadSaved() tea.Cmd {
	m, ok := p.savedMessage()
	if !ok {
		return nil
	}
	p.comp.Set(m.Body)
	f := m.Format
	if f == model.WSText {
		f = ""
	}
	p.setDraft(model.WSMessage{Body: m.Body, Format: f})
	p.syncComposer()
	p.revalidate()
	p.setSection(sComposer)
	return p.touched()
}

func (p *Pane) beginRename() {
	m, ok := p.savedMessage()
	if !ok {
		return
	}
	p.rename = p.newField(fieldSpec{single: true, plain: true, placeholder: "name"})
	p.rename.Set(m.Name)
	p.renameID = m.ID
	p.fitPrompts()
	p.rename.Begin("A")
}

// renameKey edits the template name. Enter saves it, Esc in normal mode
// cancels (Esc in insert mode only returns to normal mode).
func (p *Pane) renameKey(m tea.KeyPressMsg) tea.Cmd {
	p.fitPrompts()
	res := p.rename.Update(m)
	switch res.Event.Kind {
	case vimtext.Leave:
		p.rename = nil
		return res.Cmd
	case vimtext.Submit:
		name := strings.TrimSpace(p.rename.Text())
		p.rename = nil
		if name != "" {
			ms := p.messages()
			for i := range ms {
				if ms[i].ID == p.renameID && ms[i].Name != name {
					ms[i].Name = name
					return tea.Batch(res.Cmd, p.touched())
				}
			}
		}
		return res.Cmd
	}
	return tea.Batch(res.Cmd, forward(p.rename, res.Event))
}

func (p *Pane) askDeleteSaved() tea.Cmd {
	m, ok := p.savedMessage()
	if !ok {
		return nil
	}
	c := ui.NewConfirm("Delete saved message", "Delete \""+m.Name+"\" from this request? The message log is not affected.", "Delete", true)
	p.confirm = &confirmState{c: c, run: func() tea.Cmd { return p.deleteSaved(m.ID) }}
	return nil
}

func (p *Pane) deleteSaved(id model.ID) tea.Cmd {
	ws := p.req.WebSocket
	if ws == nil {
		return nil
	}
	for i := range ws.Messages {
		if ws.Messages[i].ID == id {
			ws.Messages = append(ws.Messages[:i], ws.Messages[i+1:]...)
			p.clampSaved()
			return tea.Batch(p.touched(), p.setNotice("Deleted the saved message", false))
		}
	}
	return nil
}

func (p *Pane) moveSaved(dir int) tea.Cmd {
	ms := p.messages()
	j := p.savedCur + dir
	if p.savedCur < 0 || p.savedCur >= len(ms) || j < 0 || j >= len(ms) {
		return nil
	}
	ms[p.savedCur], ms[j] = ms[j], ms[p.savedCur]
	p.savedCur = j
	return p.touched()
}

func (p *Pane) confirmKey(m tea.KeyPressMsg) tea.Cmd {
	c := p.confirm
	switch m.String() {
	case "left", "shift+tab", "up":
		c.c.Move(-1)
	case "right", "tab", "down":
		c.c.Move(1)
	case "esc":
		p.confirm = nil
	case "enter":
		p.confirm = nil
		if c.c.Accepted() {
			return c.run()
		}
	}
	return nil
}

// ---- log -----------------------------------------------------------------------------

// logRows is how many entries the log shows at once.
func (p *Pane) logRows() int {
	w, h := p.bodySize()
	g := msgLayout(w, h, len(p.messages()))
	rows := g.logH - 1
	if p.searching || (p.peek() != nil && p.peek().log.query != "") {
		rows--
	}
	return max(1, rows)
}

func (p *Pane) cycleFilter() tea.Cmd {
	l := p.cur()
	l.log.dir = (l.log.dir + 1) % 3
	l.log.refilter()
	return p.setNotice("Showing: "+l.log.dir.label(), false)
}

func (p *Pane) beginSearch() {
	l := p.cur()
	p.searching = true
	p.search.Set(l.log.query)
	p.fitPrompts()
	p.search.Begin("A")
}

// searchKey edits the log search. The results follow the text; Enter keeps
// them and closes the prompt, Esc in normal mode clears the search (Esc in
// insert mode only returns to normal mode).
func (p *Pane) searchKey(m tea.KeyPressMsg) tea.Cmd {
	l := p.cur()
	p.fitPrompts()
	res := p.search.Update(m)
	if q := p.search.Text(); res.Changed && q != l.log.query {
		l.log.query = q
		l.log.refilter()
	}
	switch res.Event.Kind {
	case vimtext.Submit:
		p.searching = false
		p.search.Blur()
		return res.Cmd
	case vimtext.Leave:
		p.searching = false
		p.search.Blur()
		l.log.query = ""
		l.log.refilter()
		return res.Cmd
	}
	return tea.Batch(res.Cmd, forward(p.search, res.Event))
}

func (p *Pane) clearLog() tea.Cmd {
	l := p.cur()
	n := len(l.log.entries)
	l.log.clear()
	p.detail = nil
	return p.setNotice("Cleared "+plural(n, "entry", "entries"), false)
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return itoa(n) + " " + many
}

// copySelected puts the selected message on the clipboard.
func (p *Pane) copySelected() tea.Cmd {
	l := p.peek()
	if l == nil {
		return p.setNotice("Nothing to copy", true)
	}
	e := l.log.current()
	if e == nil {
		return p.setNotice("Nothing to copy", true)
	}
	text := e.copyText()
	note := "Copied the message, " + ui.FormatSize(int64(len(text)))
	if e.truncated {
		note = "Copied the first " + ui.FormatSize(int64(len(e.data))) + " of the message"
	}
	return tea.Batch(p.deps.clip(text), p.setNotice(note, false))
}

func (p *Pane) openDetail() {
	l := p.peek()
	if l == nil {
		return
	}
	e := l.log.current()
	if e == nil {
		return
	}
	l.log.follow = false
	p.detail = &detailState{id: e.id}
	p.rebuildDetail()
}

// rebuildDetail recomputes the detail lines for the current width; the view
// closes when its entry has left the log.
func (p *Pane) rebuildDetail() {
	if p.detail == nil {
		return
	}
	l := p.peek()
	if l == nil {
		p.detail = nil
		return
	}
	e := l.log.current()
	if e == nil || e.id != p.detail.id {
		p.detail = nil
		return
	}
	w, h := p.bodySize()
	g := msgLayout(w, h, len(p.messages()))
	p.detail.w = max(10, g.logW)
	p.detail.lines, p.detail.kind = detailLines(p.th, e, p.detail.w)
	p.detail.scroll = min(p.detail.scroll, max(0, len(p.detail.lines)-1))
}

func (p *Pane) detailKey(m tea.KeyPressMsg) tea.Cmd {
	l := p.cur()
	d := p.detail
	_, h := p.bodySize()
	rows := max(1, msgLayout(p.w, h, len(p.messages())).logH-3)
	maxScroll := max(0, len(d.lines)-rows)
	switch m.String() {
	case "esc", "enter", "q":
		p.detail = nil
	case "up":
		d.scroll = max(0, d.scroll-1)
	case "down":
		d.scroll = min(maxScroll, d.scroll+1)
	case "pgup":
		d.scroll = max(0, d.scroll-rows)
	case "pgdown", "space":
		d.scroll = min(maxScroll, d.scroll+rows)
	case "home":
		d.scroll = 0
	case "end":
		d.scroll = maxScroll
	case "left", "right":
		delta := -1
		if m.String() == "right" {
			delta = 1
		}
		l.log.move(delta)
		l.log.follow = false
		if e := l.log.current(); e != nil {
			p.detail = &detailState{id: e.id}
			p.rebuildDetail()
		}
	case "c":
		return p.copySelected()
	}
	return nil
}

func (p *Pane) logKey(m tea.KeyPressMsg) tea.Cmd {
	if p.searching {
		return p.searchKey(m)
	}
	if p.detail != nil {
		return p.detailKey(m)
	}
	l := p.cur()
	rows := p.logRows()
	switch m.String() {
	case "up":
		l.log.move(-1)
	case "down":
		l.log.move(1)
	case "pgup":
		l.log.move(-rows)
	case "pgdown":
		l.log.move(rows)
	case "home":
		l.log.toStart()
	case "end":
		l.log.toEnd()
	case "enter":
		p.openDetail()
	case "f":
		return p.cycleFilter()
	case "/":
		p.beginSearch()
	case "c":
		return p.copySelected()
	case "x":
		return p.clearLog()
	case "p":
		return p.ping()
	case "tab":
		p.setSection(sComposer)
	case "shift+tab":
		if len(p.messages()) > 0 {
			p.setSection(sSaved)
		} else {
			p.setSection(sComposer)
		}
	case "esc":
		if l.log.query != "" {
			l.log.query = ""
			l.log.refilter()
			return nil
		}
		p.setSection(sComposer)
	}
	return nil
}

// ---- sections -------------------------------------------------------------------------

// setSection moves the cursor within the Messages tab.
func (p *Pane) setSection(s section) {
	p.sect = s
	if s == sComposer {
		p.fitComposer()
		p.comp.Begin("") // the composer always opens in normal mode
	}
	p.syncFocus()
}

// syncFocus gives the composer's text area input focus only while it is the
// section with the cursor.
func (p *Pane) syncFocus() {
	if p.focused && p.urlE && p.editable() {
		p.url.Focus()
	} else {
		p.url.Blur()
	}
	if p.focused && p.zone == zBody && p.tab == tabMessages && p.sect == sComposer && p.editable() {
		p.comp.Focus()
		return
	}
	p.comp.Blur()
}

// composerKey routes a key to the composer. Ctrl-T (format) and Ctrl-X (save
// message) work in every mode; Ctrl-R formats JSON while typing (in normal mode
// it stays vim's redo). In normal mode Tab and Shift-Tab move to the other
// sections, Esc leaves for the tab bar, and the keys the editor does not own
// (Space, ":", "q", g+letter) go to the shell.
func (p *Pane) composerKey(m tea.KeyPressMsg) tea.Cmd {
	switch m.String() {
	case "ctrl+t":
		return p.cycleFormat()
	case "ctrl+x":
		return p.saveTemplate()
	case "ctrl+r":
		if p.comp.Typing() {
			return p.formatJSON()
		}
	}
	p.fitComposer()
	res := p.comp.Update(m)
	cmds := []tea.Cmd{res.Cmd}
	if res.Changed {
		cmds = append(cmds, p.composerEdited())
	}
	switch res.Event.Kind {
	case vimtext.Leave:
		p.setZone(zTabs)
	case vimtext.Unhandled:
		switch res.Event.Key.String() {
		case "tab":
			if len(p.messages()) > 0 {
				p.setSection(sSaved)
			} else {
				p.setSection(sLog)
			}
		case "shift+tab":
			p.setSection(sLog)
		default:
			cmds = append(cmds, forward(p.comp, res.Event))
		}
	default:
		cmds = append(cmds, forward(p.comp, res.Event))
	}
	return tea.Batch(cmds...)
}

func (p *Pane) savedKey(m tea.KeyPressMsg) tea.Cmd {
	if p.rename != nil {
		return p.renameKey(m)
	}
	n := len(p.messages())
	switch m.String() {
	case "up":
		if p.savedCur > 0 {
			p.savedCur--
		} else {
			p.setSection(sComposer)
		}
	case "down":
		if p.savedCur < n-1 {
			p.savedCur++
		} else {
			p.setSection(sLog)
		}
	case "home":
		p.savedCur = 0
	case "end":
		p.savedCur = max(0, n-1)
	case "enter":
		return p.sendSaved()
	case "e":
		return p.loadSaved()
	case "r":
		p.beginRename()
	case "d", "delete":
		return p.askDeleteSaved()
	case "a":
		return p.saveTemplate()
	case "alt+up":
		return p.moveSaved(-1)
	case "alt+down":
		return p.moveSaved(1)
	case "tab":
		p.setSection(sLog)
	case "shift+tab", "esc":
		p.setSection(sComposer)
	}
	return nil
}

func (p *Pane) messagesKey(m tea.KeyPressMsg) tea.Cmd {
	switch p.sect {
	case sComposer:
		return p.composerKey(m)
	case sSaved:
		return p.savedKey(m)
	}
	return p.logKey(m)
}
