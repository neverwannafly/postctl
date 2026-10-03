package history

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/shubham/postctl/internal/tui/vimtext"
	"github.com/shubham/postctl/internal/ui"
)

type filterField struct {
	ed *vimtext.Model
	w  int
}

func newFilterField(th ui.Theme) *filterField {
	ed := vimtext.New(th, vimtext.Options{
		SingleLine:  true,
		StartInsert: true,
		Placeholder: "filter history",
	})
	_ = ed.Focus()
	return &filterField{ed: ed, w: 40}
}

func (f *filterField) setWidth(w int) {
	f.w = max(8, w)
	f.ed.SetSize(f.w, 1)
}

func (f *filterField) setQuery(q string) {
	f.ed.SetValue(q)
}

func (f *filterField) query() string { return strings.TrimSpace(f.ed.Value()) }

func (f *filterField) vimMode() string {
	if !f.ed.Focused() {
		return ""
	}
	return f.ed.Mode().String()
}

func (f *filterField) view() string { return f.ed.View() }

type filterNav int

const (
	filterNavNone filterNav = iota
	filterNavClose
	filterNavApply
)

func (f *filterField) update(msg tea.Msg) (tea.Cmd, filterNav) {
	if p, ok := msg.(tea.PasteMsg); ok {
		msg = tea.PasteMsg{Content: strings.NewReplacer("\r\n", " ", "\n", " ", "\r", " ").Replace(p.Content)}
	}
	if k, ok := msg.(tea.KeyPressMsg); ok && f.ed.Mode() == vimtext.Normal {
		switch k.String() {
		case "esc", "q":
			return nil, filterNavClose
		case "enter":
			return nil, filterNavApply
		}
	}
	cmd, ev := f.ed.Update(msg)
	switch ev.Kind {
	case vimtext.Submit:
		return cmd, filterNavApply
	case vimtext.Leave:
		f.ed.Blur()
		return cmd, filterNavClose
	default:
		return cmd, filterNavNone
	}
}
