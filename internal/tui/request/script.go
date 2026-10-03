package request

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/shubham/postctl/internal/model"
	"github.com/shubham/postctl/internal/tui/vimfield"
	"github.com/shubham/postctl/internal/tui/vimtext"
	"github.com/shubham/postctl/internal/ui"
)

// snippet is a common script idiom.
type snippet struct {
	title string
	code  string
}

var preSnippets = []snippet{
	{"Set an environment variable", `pm.environment.set("name", "value");`},
	{"Set a request header", `pm.request.headers.upsert({ key: "X-Request-Id", value: pm.variables.replaceIn("{{$guid}}") });`},
	{"Set a timestamp variable", `pm.variables.set("timestamp", new Date().toISOString());`},
	{"Log a variable", `console.log("name =", pm.variables.get("name"));`},
	{"Skip this request", `pm.execution.skipRequest();`},
}

var testSnippets = []snippet{
	{"Status code is 200", "pm.test(\"Status code is 200\", function () {\n    pm.response.to.have.status(200);\n});"},
	{"Response time is under 500 ms", "pm.test(\"Response time is under 500 ms\", function () {\n    pm.expect(pm.response.responseTime).to.be.below(500);\n});"},
	{"Set an environment variable from the JSON body", "const body = pm.response.json();\npm.environment.set(\"token\", body.token);"},
	{"Body has a JSON field", "pm.test(\"Body has an id\", function () {\n    pm.expect(pm.response.json()).to.have.property(\"id\");\n});"},
	{"Header is present", "pm.test(\"Content-Type is JSON\", function () {\n    pm.response.to.have.header(\"Content-Type\");\n});"},
}

// scriptView is the Pre-request and Tests tabs.
type scriptView struct {
	pre  bool
	ed   *vimfield.Field
	on   bool // the editor has input focus
	pick bool // the snippet list is open
	sel  int
}

func newScriptView(pre bool, p *Pane) *scriptView {
	s := &scriptView{pre: pre}
	s.build(p, "")
	return s
}

// build creates the editor: JavaScript highlighting, line numbers, and pm.*
// plus {{variable}} completion.
func (s *scriptView) build(p *Pane, text string) {
	s.ed = p.newField(fieldSpec{numbers: true, lang: "javascript", script: true})
	s.ed.Set(text)
}

func (s *scriptView) setTheme(p *Pane) {
	s.build(p, s.ed.Text())
	s.on = false
}

// field is the focused editor, or nil.
func (s *scriptView) field() *vimfield.Field {
	if s.on {
		return s.ed
	}
	return nil
}

// size gives the editor the size view draws it at.
func (s *scriptView) size(p *Pane) {
	w, h := p.bodySize()
	s.ed.Fit(w, max(1, h-1))
}

func (s *scriptView) script(p *Pane) *model.Script {
	if s.pre {
		return p.req.Scripts.PreRequest
	}
	return p.req.Scripts.Test
}

func (s *scriptView) source(p *Pane) string {
	if sc := s.script(p); sc != nil {
		return sc.Source
	}
	return ""
}

// setSource writes text into the request, dropping the script when empty so
// the file stays free of noise.
func (s *scriptView) setSource(p *Pane, text string) {
	sc := s.script(p)
	if sc == nil {
		if strings.TrimSpace(text) == "" && text == "" {
			return
		}
		sc = &model.Script{}
		if s.pre {
			p.req.Scripts.PreRequest = sc
		} else {
			p.req.Scripts.Test = sc
		}
	}
	sc.Source = text
	if text == "" && sc.Src == "" && sc.Name == "" && sc.ID == "" {
		if s.pre {
			p.req.Scripts.PreRequest = nil
		} else {
			p.req.Scripts.Test = nil
		}
	}
}

func (s *scriptView) reload(p *Pane) {
	if p.req == nil {
		return
	}
	s.ed.Set(s.source(p))
	if !p.Editing() {
		s.on = false
		s.ed.Blur()
	}
	s.pick = false
}

func (s *scriptView) stop() {
	s.on = false
	s.pick = false
	s.ed.Blur()
}

func (s *scriptView) editing() bool { return s.on || s.pick }
func (s *scriptView) atTop() bool   { return !s.on && !s.pick }

// enter puts the cursor in the script, in normal mode.
func (s *scriptView) enter(p *Pane) {
	s.ed.Set(s.source(p))
	s.on = true
	s.size(p)
	s.ed.Begin("")
}

func (s *scriptView) paste(p *Pane, m tea.PasteMsg) bool {
	if !s.on {
		return false
	}
	if s.ed.Paste(m).Changed {
		s.setSource(p, s.ed.Text())
		return true
	}
	return false
}

func (s *scriptView) list() []snippet {
	if s.pre {
		return preSnippets
	}
	return testSnippets
}

func (s *scriptView) key(p *Pane, m tea.KeyPressMsg) (bool, bool, tea.Cmd) {
	k := m.String()
	if s.pick {
		items := s.list()
		switch k {
		case "up":
			s.sel = (s.sel + len(items) - 1) % len(items)
		case "down":
			s.sel = (s.sel + 1) % len(items)
		case "esc":
			s.pick = false
		case "enter":
			s.pick = false
			code := items[s.sel].code
			cur := s.ed.Text()
			if cur != "" && !strings.HasSuffix(cur, "\n") {
				code = "\n" + code
			}
			s.ed.Replace(cur + code + "\n")
			s.setSource(p, s.ed.Text())
			s.on = true
			s.size(p)
			s.ed.Begin("")
			return true, true, nil
		}
		return true, false, nil
	}
	if s.on {
		s.size(p)
		res := s.ed.Update(m)
		changed := false
		if res.Changed {
			s.setSource(p, s.ed.Text())
			changed = true
		}
		if res.Event.Kind == vimtext.Leave {
			s.on = false
			s.ed.Blur()
			return true, changed, res.Cmd
		}
		return true, changed, batch(res.Cmd, forward(s.ed, res.Event))
	}
	switch k {
	case "enter", "i", "a", "A", "I":
		start := ""
		if k != "enter" {
			start = k
		}
		s.on = true
		s.size(p)
		s.ed.Begin(start)
		return true, false, nil
	}
	return false, false, nil
}

// openSnippets opens the snippet list (the request.snippet action).
func (s *scriptView) openSnippets() {
	s.pick, s.sel = true, 0
}

func (s *scriptView) hints(p *Pane) []ui.Hint {
	switch {
	case s.pick:
		return []ui.Hint{{Key: "Enter", Label: "Insert", Priority: 1}, {Key: "Esc", Label: "Close", Priority: 1}, {Key: "↑↓", Label: "Choose", Priority: 3}}
	case s.on:
		return fieldHints(s.ed, "Done")
	}
	return []ui.Hint{{Key: "Enter", Label: "Edit", Priority: 1}, {Key: "Space i", Label: "Snippets", Priority: 3}, {Key: "Space o", Label: "$EDITOR", Priority: 4}}
}

func (s *scriptView) view(p *Pane, w, h int) string {
	th := p.th
	if s.pick {
		return s.pickerView(p, w, h)
	}
	when := "before the request is sent"
	if !s.pre {
		when = "after the response arrives"
	}
	var status string
	switch {
	case s.on:
		row, col := s.ed.Cursor()
		status = "Line " + itoa(row+1) + ", column " + itoa(col+1) + " " + th.Glyphs.Dot + " " + itoa(s.ed.LineCount()) + " lines"
	case strings.TrimSpace(s.source(p)) == "":
		status = "JavaScript runs " + when + ". Press Enter to write one, Space i for snippets."
	default:
		status = "JavaScript runs " + when
	}
	return s.ed.Render(w, max(1, h-1)) + "\n" + th.Render(ui.TextQuiet, th.Truncate(status, w))
}

func (s *scriptView) pickerView(p *Pane, w, h int) string {
	th := p.th
	items := s.list()
	lines := []string{th.Render(ui.TextSecondary, "Insert a snippet"), ""}
	for i, it := range items {
		marker := "  "
		tok := ui.TextPrimary
		if i == s.sel {
			marker = th.Render(ui.AccentPrimary, th.Glyphs.Select) + " "
			tok = ui.AccentPrimary
		}
		lines = append(lines, marker+th.Render(tok, th.Truncate(it.title, w-2)))
	}
	code := strings.SplitN(items[s.sel].code, "\n", 2)[0]
	lines = append(lines, "", th.Render(ui.TextQuiet, th.Truncate(code, w)))
	return strings.Join(lines, "\n")
}

// external implements externalEditable.
func (s *scriptView) external(p *Pane) (string, string, bool) {
	return ".js", s.source(p), true
}

func (s *scriptView) applyExternal(p *Pane, text string) {
	s.setSource(p, text)
}
