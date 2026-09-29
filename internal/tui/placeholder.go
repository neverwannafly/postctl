package tui

import (
	"path/filepath"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/shubham/postctl/internal/model"
	"github.com/shubham/postctl/internal/ui"
)

// placeholderPane fills the Request and Response slots until the real
// editor and viewer (WS6) are plugged in through Options. The request
// placeholder shows the selected request's method, URL and a summary so the
// shell is usable for browsing.
type placeholderPane struct {
	th      ui.Theme
	slot    Slot
	w, h    int
	ref     RequestRef
	env     *model.Environment
	focused bool
}

func newRequestPlaceholder(env PaneEnv) Pane {
	return &placeholderPane{th: env.Theme, slot: SlotRequest}
}

func newResponsePlaceholder(env PaneEnv) Pane {
	return &placeholderPane{th: env.Theme, slot: SlotResponse}
}

func (p *placeholderPane) Update(msg tea.Msg) (Pane, tea.Cmd) {
	switch msg := msg.(type) {
	case SelectionMsg:
		p.ref = msg.Ref
	case EnvironmentChangedMsg:
		p.env = msg.Env
	case ThemeChangedMsg:
		p.th = msg.Theme
	}
	return p, nil
}

func (p *placeholderPane) SetSize(w, h int) { p.w, p.h = w, h }
func (p *placeholderPane) Focus() tea.Cmd   { p.focused = true; return nil }
func (p *placeholderPane) Blur()            { p.focused = false }

func (p *placeholderPane) KeyHints() []ui.Hint { return nil }

func (p *placeholderPane) View() string {
	if p.slot == SlotResponse {
		return p.responseView()
	}
	return p.requestView()
}

func (p *placeholderPane) requestView() string {
	t := p.th
	w := max(10, p.w)
	ref := p.ref
	switch {
	case ref.Problem != nil:
		name := filepath.Base(ref.Problem.Path)
		return t.ErrorState("Could not read "+name+".",
			"Reason: "+firstLine(ref.Problem.Err.Error())+"\nThe rest of your workspace is intact.",
			"Fix the file in your editor. postctl reloads it as soon as you save.", w) +
			"\n\n" + t.Render(ui.TextQuiet, ui.Wrap(ref.Problem.Path, w))
	case ref.Request == nil && len(ref.Path) > 0:
		return t.EmptyState(strings.Join(ref.Path, " "+t.Glyphs.Arrow+" "), "A folder or collection. Enter folds or unfolds it.", "", w)
	case ref.Request == nil:
		return t.EmptyState("No request selected", "Choose one in Collections, or press Ctrl-P to search.", "", w)
	}

	r := ref.Request
	url := r.URL.Raw
	if url == "" {
		url = "(no URL yet)"
	}
	lines := []string{
		t.MethodBadge(r.Method, 7) + " " + t.Render(ui.TextPrimary, t.Truncate(url, max(1, w-8))),
		t.Render(ui.TextSecondary, t.Truncate(strings.Join(ref.Path, " "+t.Glyphs.Arrow+" "), w)),
		"",
	}
	var facts []string
	if n := len(r.Headers); n > 0 {
		facts = append(facts, "Headers "+strconv.Itoa(n))
	}
	if n := len(r.URL.Query); n > 0 {
		facts = append(facts, "Query "+strconv.Itoa(n))
	}
	if r.Body.Mode != "" && r.Body.Mode != model.BodyNone {
		facts = append(facts, "Body "+string(r.Body.Mode))
	}
	if r.Auth != nil && r.Auth.Type != "" {
		facts = append(facts, "Auth "+string(r.Auth.Type))
	}
	if r.Scripts.PreRequest != nil {
		facts = append(facts, "Pre-request "+t.Glyphs.OK)
	}
	if r.Scripts.Test != nil {
		facts = append(facts, "Tests "+t.Glyphs.OK)
	}
	if len(facts) > 0 {
		lines = append(lines, t.Render(ui.TextSecondary, ui.Wrap(strings.Join(facts, "  "+t.Glyphs.Dot+"  "), w)))
	} else {
		lines = append(lines, t.Render(ui.TextSecondary, "Nothing else set on this request."))
	}
	if d := strings.TrimSpace(r.Description); d != "" {
		lines = append(lines, "", t.Render(ui.TextPrimary, ui.Wrap(firstParagraph(d), w)))
	}
	return strings.Join(lines, "\n")
}

func (p *placeholderPane) responseView() string {
	t := p.th
	w := max(10, p.w)
	if p.ref.Request == nil {
		return t.EmptyState("No response yet", "Select a request to see its response here.", "", w)
	}
	return t.EmptyState("No response yet", "Responses appear here after a request is sent.", "", w)
}

func firstParagraph(s string) string {
	if i := strings.Index(s, "\n\n"); i >= 0 {
		s = s[:i]
	}
	return s
}
