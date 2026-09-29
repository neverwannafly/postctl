package request

import (
	"bytes"
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/shubham/postctl/internal/model"
	"github.com/shubham/postctl/internal/ui"
)

var bodyModes = []model.BodyMode{model.BodyNone, model.BodyRaw, model.BodyURLEncoded, model.BodyFormData, model.BodyBinary, model.BodyGraphQL}

func modeLabel(m model.BodyMode) string {
	switch m {
	case model.BodyRaw:
		return "raw"
	case model.BodyURLEncoded:
		return "x-www-form-urlencoded"
	case model.BodyFormData:
		return "form-data"
	case model.BodyBinary:
		return "binary"
	case model.BodyGraphQL:
		return "GraphQL"
	}
	return "none"
}

var bodyLangs = []string{"json", "text", "javascript", "xml", "html"}

func langLabel(l string) string {
	switch l {
	case "json":
		return "JSON"
	case "javascript":
		return "JavaScript"
	case "xml":
		return "XML"
	case "html":
		return "HTML"
	}
	return "Text"
}

func langExt(l string) string {
	switch l {
	case "json":
		return ".json"
	case "javascript":
		return ".js"
	case "xml":
		return ".xml"
	case "html":
		return ".html"
	}
	return ".txt"
}

// zoneKind is one cursor stop of the Body tab.
type zoneKind int

const (
	zkMode zoneKind = iota
	zkLang
	zkRaw
	zkQuery
	zkVars
	zkTable
	zkFile
)

// bodyView is the Body tab: mode selector, then the mode's editor.
type bodyView struct {
	cur  int
	on   bool // a text editor has input focus
	raw  *textEditor
	gq   *textEditor
	gv   *textEditor
	kvs  map[model.BodyMode]*kvView
	file form
	hint string // cached validation result for the raw editor
	warn bool
}

func newBodyView(th ui.Theme) *bodyView {
	return &bodyView{
		raw: newTextEditor(th, false),
		gq:  newTextEditor(th, false),
		gv:  newTextEditor(th, false),
		kvs: map[model.BodyMode]*kvView{
			model.BodyURLEncoded: newKVView(kindURLEncoded),
			model.BodyFormData:   newKVView(kindFormData),
		},
	}
}

func (b *bodyView) setTheme(th ui.Theme) {
	b.raw.setTheme(th)
	b.gq.setTheme(th)
	b.gv.setTheme(th)
	for _, k := range b.kvs {
		k.setTheme(th)
	}
}

func (b *bodyView) mode(p *Pane) model.BodyMode {
	if p.req.Body.Mode == "" {
		return model.BodyNone
	}
	return p.req.Body.Mode
}

func (b *bodyView) layout(p *Pane) []zoneKind {
	switch b.mode(p) {
	case model.BodyRaw:
		return []zoneKind{zkMode, zkLang, zkRaw}
	case model.BodyURLEncoded, model.BodyFormData:
		return []zoneKind{zkMode, zkTable}
	case model.BodyBinary:
		return []zoneKind{zkMode, zkFile}
	case model.BodyGraphQL:
		return []zoneKind{zkMode, zkQuery, zkVars}
	}
	return []zoneKind{zkMode}
}

func (b *bodyView) kv(p *Pane) *kvView { return b.kvs[b.mode(p)] }

func (b *bodyView) reload(p *Pane) {
	if p.req == nil {
		return
	}
	body := &p.req.Body
	b.raw.set(body.Raw)
	if body.GraphQL != nil {
		b.gq.set(body.GraphQL.Query)
		b.gv.set(body.GraphQL.Variables)
	} else {
		b.gq.set("")
		b.gv.set("")
	}
	for _, k := range b.kvs {
		k.reload(p)
	}
	b.file.reset()
	b.revalidate(p)
	lay := b.layout(p)
	b.cur = min(b.cur, len(lay)-1)
	if !p.Editing() {
		b.blurAll()
	}
}

func (b *bodyView) blurAll() {
	b.on = false
	b.raw.blur()
	b.gq.blur()
	b.gv.blur()
}

func (b *bodyView) stop() {
	b.blurAll()
	b.file.reset()
	for _, k := range b.kvs {
		k.stop()
	}
}

func (b *bodyView) editing() bool {
	if b.on || b.file.edit {
		return true
	}
	for _, k := range b.kvs {
		if k.editing() {
			return true
		}
	}
	return false
}

func (b *bodyView) atTop() bool { return b.cur == 0 && !b.editing() }

func (b *bodyView) enter(p *Pane) {
	b.cur = 0
	b.blurAll()
}

// focusZone puts input focus on the zone's editor when it is a text editor.
func (b *bodyView) focusZone(p *Pane, z zoneKind) {
	b.blurAll()
	switch z {
	case zkRaw:
		b.on = true
		b.raw.focus()
	case zkQuery:
		b.on = true
		b.gq.focus()
	case zkVars:
		b.on = true
		b.gv.focus()
	}
}

func (b *bodyView) editor(z zoneKind) *textEditor {
	switch z {
	case zkRaw:
		return b.raw
	case zkQuery:
		return b.gq
	case zkVars:
		return b.gv
	}
	return nil
}

func (b *bodyView) fileRows(p *Pane) []formRow {
	return []formRow{{
		label: "File", kind: rowText, value: p.req.Body.File, placeholder: "Path to the file to send",
		note: "",
		set: func(t string) bool {
			p.req.Body.File = t
			return true
		},
	}}
}

// write copies an editor's text into the request.
func (b *bodyView) write(p *Pane, z zoneKind) {
	body := &p.req.Body
	switch z {
	case zkRaw:
		body.Raw = b.raw.text()
		b.revalidate(p)
	case zkQuery, zkVars:
		if body.GraphQL == nil {
			body.GraphQL = &model.GraphQLBody{}
		}
		body.GraphQL.Query = b.gq.text()
		body.GraphQL.Variables = b.gv.text()
		if body.GraphQL.Query == "" && body.GraphQL.Variables == "" {
			body.GraphQL = nil
		}
	}
}

func (b *bodyView) cycleMode(p *Pane, dir int) {
	cur := 0
	for i, m := range bodyModes {
		if m == b.mode(p) {
			cur = i
		}
	}
	next := bodyModes[(cur+dir+len(bodyModes))%len(bodyModes)]
	p.req.Body.Mode = next
	if next == model.BodyNone {
		p.req.Body.Mode = model.BodyNone
	}
	if next == model.BodyRaw && p.req.Body.Language == "" {
		p.req.Body.Language = "json"
	}
	b.revalidate(p)
	b.blurAll()
}

func (b *bodyView) cycleLang(p *Pane, dir int) {
	cur := 0
	for i, l := range bodyLangs {
		if l == p.req.Body.Language {
			cur = i
		}
	}
	p.req.Body.Language = bodyLangs[(cur+dir+len(bodyLangs))%len(bodyLangs)]
	b.revalidate(p)
}

func (b *bodyView) paste(p *Pane, m tea.PasteMsg) bool {
	lay := b.layout(p)
	z := lay[min(b.cur, len(lay)-1)]
	if e := b.editor(z); e != nil && b.on {
		if e.update(m) {
			b.write(p, z)
			return true
		}
		return false
	}
	switch z {
	case zkFile:
		return b.file.paste(p, func() []formRow { return b.fileRows(p) }, m)
	case zkTable:
		return b.kv(p).paste(p, m)
	}
	return false
}

func (b *bodyView) key(p *Pane, m tea.KeyPressMsg) (bool, bool, tea.Cmd) {
	lay := b.layout(p)
	b.cur = min(b.cur, len(lay)-1)
	z := lay[b.cur]
	s := m.String()

	// Editing a text editor.
	if e := b.editor(z); e != nil && b.on {
		switch s {
		case "esc":
			b.blurAll()
			return true, false, nil
		case "tab":
			if z == zkQuery {
				b.cur++
				b.focusZone(p, zkVars)
				return true, false, nil
			}
			if z == zkVars {
				b.cur--
				b.focusZone(p, zkQuery)
				return true, false, nil
			}
		}
		if e.update(m) {
			b.write(p, z)
			return true, true, nil
		}
		return true, false, nil
	}

	switch z {
	case zkTable:
		k := b.kv(p)
		handled, changed, cmd := k.key(p, m)
		if handled || k.editing() {
			return handled, changed, cmd
		}
	case zkFile:
		handled, changed, cmd := b.file.key(p, func() []formRow { return b.fileRows(p) }, m)
		if handled || b.file.edit {
			return handled, changed, cmd
		}
	}

	switch s {
	case "up":
		if b.cur > 0 {
			b.cur--
			b.blurAll()
			return true, false, nil
		}
		return false, false, nil
	case "down":
		if b.cur+1 < len(lay) {
			b.cur++
			b.focusZone(p, lay[b.cur])
			return true, false, nil
		}
		return true, false, nil
	}
	switch z {
	case zkMode:
		switch s {
		case "left":
			b.cycleMode(p, -1)
			return true, true, nil
		case "right", "enter", "space", " ":
			b.cycleMode(p, 1)
			return true, true, nil
		}
	case zkLang:
		switch s {
		case "left":
			b.cycleLang(p, -1)
			return true, true, nil
		case "right", "enter", "space", " ":
			b.cycleLang(p, 1)
			return true, true, nil
		case "f":
			return true, b.format(p), nil
		}
	case zkRaw, zkQuery, zkVars:
		switch s {
		case "enter", "i":
			b.focusZone(p, z)
			return true, false, nil
		case "f":
			if z == zkRaw {
				return true, b.format(p), nil
			}
		}
	}
	return false, false, nil
}

// ---- JSON ---------------------------------------------------------------------

// jsonSentinel replaces {{variables}} while formatting or validating: a number
// is valid JSON both bare and inside a string.
func jsonSentinel(text string) (string, []string) {
	var vars []string
	out := varRE.ReplaceAllStringFunc(text, func(m string) string {
		vars = append(vars, m)
		return "9000000000" + strconv.Itoa(100000+len(vars)-1)
	})
	return out, vars
}

func restoreSentinels(text string, vars []string) string {
	for i, v := range vars {
		text = strings.ReplaceAll(text, "9000000000"+strconv.Itoa(100000+i), v)
	}
	return text
}

// validateJSON reports "" for valid JSON, otherwise "line L, column C: why".
func validateJSON(text string) string {
	if strings.TrimSpace(text) == "" {
		return ""
	}
	t, _ := jsonSentinel(text)
	if json.Valid([]byte(t)) {
		return ""
	}
	var v any
	err := json.Unmarshal([]byte(t), &v)
	var se *json.SyntaxError
	if errors.As(err, &se) {
		line, col := 1, 1
		for i := 0; i < int(se.Offset) && i < len(t); i++ {
			if t[i] == '\n' {
				line++
				col = 1
			} else {
				col++
			}
		}
		return "line " + itoa(line) + ", column " + itoa(max(1, col-1)) + ": " + se.Error()
	}
	return "not valid JSON"
}

// formatJSON pretty-prints text, keeping {{variables}} where they were.
func formatJSON(text string) (string, bool) {
	t, vars := jsonSentinel(text)
	var buf bytes.Buffer
	if err := json.Indent(&buf, []byte(t), "", "  "); err != nil {
		return text, false
	}
	return restoreSentinels(buf.String(), vars), true
}

func (b *bodyView) revalidate(p *Pane) {
	b.hint, b.warn = "", false
	if p.req.Body.Mode != model.BodyRaw {
		return
	}
	if p.req.Body.Language == "json" {
		if msg := validateJSON(p.req.Body.Raw); msg != "" {
			b.hint, b.warn = msg, true
		} else if strings.TrimSpace(p.req.Body.Raw) != "" {
			b.hint = "Valid JSON"
		}
	}
}

// format pretty-prints the raw JSON body; it reports whether the body changed.
func (b *bodyView) format(p *Pane) bool {
	if p.req.Body.Mode != model.BodyRaw || p.req.Body.Language != "json" {
		return false
	}
	out, ok := formatJSON(p.req.Body.Raw)
	if !ok || out == p.req.Body.Raw {
		return false
	}
	p.req.Body.Raw = out
	b.raw.ta.SetValue(out)
	b.raw.ta.MoveToBegin()
	b.revalidate(p)
	return true
}

// formatBody is the palette action.
func (p *Pane) formatBody() tea.Cmd {
	bv := p.views[tabBody].(*bodyView)
	pre := p.req.Clone()
	if bv.format(p) {
		return tea.Batch(p.record(pre, true), p.setNotice("Formatted JSON"))
	}
	if msg := validateJSON(p.req.Body.Raw); msg != "" {
		return p.setNotice("Cannot format: " + msg)
	}
	return p.setNotice("Already formatted")
}

// ---- view ---------------------------------------------------------------------

func (b *bodyView) hints(p *Pane) []ui.Hint {
	lay := b.layout(p)
	z := lay[min(b.cur, len(lay)-1)]
	switch {
	case b.on:
		hs := []ui.Hint{{Key: "Esc", Label: "Done", Priority: 1}, {Key: "Ctrl-O", Label: "$EDITOR", Priority: 4}}
		if z == zkQuery || z == zkVars {
			hs = append(hs, ui.Hint{Key: "Tab", Label: "Query/Variables", Priority: 3})
		}
		return hs
	case z == zkTable:
		return b.kv(p).hints(p)
	case z == zkMode:
		return []ui.Hint{{Key: "←→", Label: "Body type", Priority: 1}, {Key: "↓", Label: "Content", Priority: 2}}
	case z == zkLang:
		return []ui.Hint{{Key: "←→", Label: "Language", Priority: 1}, {Key: "f", Label: "Format JSON", Priority: 2}}
	}
	return []ui.Hint{{Key: "Enter", Label: "Edit", Priority: 1}, {Key: "f", Label: "Format JSON", Priority: 3}, {Key: "Ctrl-O", Label: "$EDITOR", Priority: 4}}
}

func (b *bodyView) view(p *Pane, w, h int) string {
	th := p.th
	lay := b.layout(p)
	b.cur = min(b.cur, len(lay)-1)
	focusRow := func(i int) bool { return p.zone == zBody && b.cur == i }
	head := []string{b.headRow(p, "Type", modeLabel(b.mode(p)), focusRow(0), w)}
	if b.mode(p) == model.BodyRaw {
		lang := b.headRow(p, "Language", langLabel(p.req.Body.Language), focusRow(1), w)
		head = append(head, lang)
	}
	used := len(head)
	room := max(1, h-used-1)
	var content string
	switch b.mode(p) {
	case model.BodyNone:
		content = th.Render(ui.TextQuiet, th.Truncate("This request has no body. Choose a type above to add one.", w))
	case model.BodyRaw:
		b.raw.resize(w, room-1)
		content = b.raw.view() + "\n" + b.rawStatus(p, w)
	case model.BodyURLEncoded, model.BodyFormData:
		content = b.kv(p).view(p, w, room)
	case model.BodyBinary:
		content = b.fileView(p, w)
	case model.BodyGraphQL:
		content = b.graphqlView(p, w, room)
	}
	return strings.Join(head, "\n") + "\n" + content
}

func (b *bodyView) headRow(p *Pane, label, value string, focused bool, w int) string {
	th := p.th
	marker := "  "
	v := th.Render(ui.TextPrimary, value)
	if focused {
		marker = th.Render(ui.AccentPrimary, th.Glyphs.Select) + " "
		l, r := "‹", "›"
		if th.ASCII {
			l, r = "<", ">"
		}
		v = th.Render(ui.TextQuiet, l) + " " + th.Render(ui.TextPrimary, value) + " " + th.Render(ui.TextQuiet, r)
	}
	return th.Truncate(marker+th.Render(ui.TextSecondary, th.Fit(label, 10))+" "+v, w)
}

func (b *bodyView) rawStatus(p *Pane, w int) string {
	th := p.th
	tok := ui.TextQuiet
	text := b.hint
	switch {
	case b.warn:
		tok = ui.StatusWarning
		text = th.Glyphs.Warn + " " + text
	case b.hint != "":
		tok = ui.StatusSuccess
		text = th.Glyphs.Check + " " + text
	case b.on:
		l, c := b.raw.pos()
		text = "Line " + itoa(l) + ", column " + itoa(c)
	default:
		text = "Press Enter to edit"
	}
	return th.Render(tok, th.Truncate(text, w))
}

func (b *bodyView) fileView(p *Pane, w int) string {
	th := p.th
	f := &b.file
	row := b.fileRows(p)[0]
	line := f.rowView(p, row, p.zone == zBody && b.cur == 1, 10, max(4, w-13))
	note := th.Render(ui.TextQuiet, th.Truncate("The file is sent as the request body, unchanged.", w))
	return line + "\n\n" + note
}

func (b *bodyView) graphqlView(p *Pane, w, h int) string {
	th := p.th
	qh := max(2, (h-2)*2/3)
	vh := max(1, h-2-qh)
	label := func(text string, on bool) string {
		tok := ui.TextSecondary
		if on {
			tok = ui.AccentPrimary
		}
		return th.Render(tok, text)
	}
	b.gq.resize(w, qh)
	b.gv.resize(w, vh)
	return label("Query", b.cur == 1 && p.zone == zBody) + "\n" + b.gq.view() + "\n" +
		label("Variables (JSON)", b.cur == 2 && p.zone == zBody) + "\n" + b.gv.view()
}

// external implements externalEditable for the raw and GraphQL editors.
func (b *bodyView) external(p *Pane) (string, string, bool) {
	switch b.mode(p) {
	case model.BodyRaw:
		return langExt(p.req.Body.Language), p.req.Body.Raw, true
	case model.BodyGraphQL:
		gq := p.req.Body.GraphQL
		lay := b.layout(p)
		if b.cur < len(lay) && lay[b.cur] == zkVars {
			if gq == nil {
				return ".json", "", true
			}
			return ".json", gq.Variables, true
		}
		if gq == nil {
			return ".graphql", "", true
		}
		return ".graphql", gq.Query, true
	case model.BodyURLEncoded:
		return b.kv(p).external(p)
	}
	return "", "", false
}

func (b *bodyView) applyExternal(p *Pane, text string) {
	switch b.mode(p) {
	case model.BodyRaw:
		p.req.Body.Raw = text
	case model.BodyGraphQL:
		if p.req.Body.GraphQL == nil {
			p.req.Body.GraphQL = &model.GraphQLBody{}
		}
		lay := b.layout(p)
		if b.cur < len(lay) && lay[b.cur] == zkVars {
			p.req.Body.GraphQL.Variables = text
		} else {
			p.req.Body.GraphQL.Query = text
		}
	case model.BodyURLEncoded:
		b.kv(p).applyExternal(p, text)
	}
}
