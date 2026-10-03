package request

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/shubham/postctl/internal/model"
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
	ed   *textEditor
	th   ui.Theme
	on   bool // the editor has input focus
	pick bool // the snippet list is open
	sel  int
}

func newScriptView(pre bool, th ui.Theme) *scriptView {
	return &scriptView{pre: pre, th: th, ed: newTextEditor(th, true)}
}

func (s *scriptView) setTheme(th ui.Theme) {
	s.th = th
	s.ed.setTheme(th)
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
	s.ed.set(s.source(p))
	if !p.Editing() {
		s.on = false
		s.ed.blur()
	}
	s.pick = false
}

func (s *scriptView) stop() {
	s.on = false
	s.pick = false
	s.ed.blur()
}

func (s *scriptView) editing() bool { return s.on || s.pick }
func (s *scriptView) atTop() bool   { return !s.on && !s.pick }

func (s *scriptView) enter(p *Pane) {
	s.ed.set(s.source(p))
	s.on = true
	s.ed.focus()
}

func (s *scriptView) paste(p *Pane, m tea.PasteMsg) bool {
	if !s.on {
		return false
	}
	if s.ed.update(m) {
		s.setSource(p, s.ed.text())
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
			cur := s.ed.text()
			if cur != "" && !strings.HasSuffix(cur, "\n") {
				code = "\n" + code
			}
			s.ed.ta.MoveToEnd()
			s.ed.insertText(code + "\n")
			s.setSource(p, s.ed.text())
			s.on = true
			s.ed.focus()
			return true, true, nil
		}
		return true, false, nil
	}
	if s.on {
		switch k {
		case "esc":
			s.on = false
			s.ed.blur()
			return true, false, nil
		case "tab":
			// Insert two spaces rather than moving focus.
			s.ed.insertText("  ")
			s.setSource(p, s.ed.text())
			return true, true, nil
		}
		if s.ed.update(m) {
			s.setSource(p, s.ed.text())
			return true, true, nil
		}
		return true, false, nil
	}
	switch k {
	case "enter", "i":
		s.on = true
		s.ed.focus()
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
		return []ui.Hint{{Key: "Esc", Label: "Done", Priority: 1}}
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
		l, c := s.ed.pos()
		status = "Line " + itoa(l) + ", column " + itoa(c) + " " + th.Glyphs.Dot + " " + itoa(s.ed.lineCount()) + " lines"
	case strings.TrimSpace(s.source(p)) == "":
		status = "JavaScript runs " + when + ". Press Enter to write one, Space i for snippets."
	default:
		status = "JavaScript runs " + when
	}
	return s.ed.render(w, max(1, h-1)) + "\n" + th.Render(ui.TextQuiet, th.Truncate(status, w))
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
