// Package screenfield wraps vimtext for single-line fields on full screens.
package screenfield

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/rivo/uniseg"
	"github.com/shubham/postctl/internal/tui/vimtext"
	"github.com/shubham/postctl/internal/ui"
)

// Field is a single-line vimtext editor for screen forms and tables.
type Field struct {
	ed *vimtext.Model
}

// New builds a field and focuses it in insert mode.
func New(th ui.Theme, value, placeholder string, masked bool) (*Field, tea.Cmd) {
	opts := vimtext.Options{SingleLine: true, StartInsert: true, Placeholder: placeholder}
	if masked {
		opts.Redact = maskFn(th)
	}
	ed := vimtext.New(th, opts)
	ed.SetValue(value)
	cursorEnd(ed)
	_ = ed.Focus() // screen fields are active as soon as they are created
	f := &Field{ed: ed}
	return f, nil
}

func cursorEnd(ed *vimtext.Model) {
	line := ed.Value()
	g := uniseg.NewGraphemes(line)
	n := 0
	for g.Next() {
		n++
	}
	ed.SetCursor(0, n)
}

func maskFn(th ui.Theme) func(string) string {
	return func(s string) string {
		if s == "" {
			return ""
		}
		if th.ASCII {
			return "****"
		}
		return "••••"
	}
}

// SetWidth sets the render width in cells.
func (f *Field) SetWidth(w int) { f.ed.SetSize(max(4, w), 1) }

// Value returns the edited text.
func (f *Field) Value() string { return f.ed.Value() }

// View renders the field.
func (f *Field) View() string { return f.ed.View() }

// VimMode returns INSERT, NORMAL, or "" when unfocused.
func (f *Field) VimMode() string {
	if !f.ed.Focused() {
		return ""
	}
	return f.ed.Mode().String()
}

// Inserting reports whether insert or visual mode is active.
func (f *Field) Inserting() bool {
	if !f.ed.Focused() {
		return false
	}
	switch f.ed.Mode() {
	case vimtext.Insert, vimtext.Visual, vimtext.VisualLine:
		return true
	default:
		return false
	}
}

// Blur leaves the field.
func (f *Field) Blur() { f.ed.Blur() }

// Update applies msg and reports whether the buffer changed.
func (f *Field) Update(msg tea.Msg) (changed bool, cmd tea.Cmd) {
	_, changed, cmd, _, _ = f.UpdateEx(msg)
	return changed, cmd
}

// UpdateEx applies msg and reports submit/leave in normal mode.
func (f *Field) UpdateEx(msg tea.Msg) (submit, changed bool, cmd tea.Cmd, leave bool, normal bool) {
	if p, ok := msg.(tea.PasteMsg); ok {
		msg = tea.PasteMsg{Content: strings.NewReplacer("\r\n", " ", "\n", " ", "\r", " ").Replace(p.Content)}
	}
	before := f.ed.Value()
	cmd, ev := f.ed.Update(msg)
	after := f.ed.Value()
	normal = f.ed.Mode() == vimtext.Normal
	switch ev.Kind {
	case vimtext.Submit:
		submit = true
	case vimtext.Leave:
		leave = true
		f.ed.Blur()
	}
	return submit, after != before, cmd, leave, normal
}

// Mode returns the underlying vim mode.
func (f *Field) Mode() vimtext.Mode { return f.ed.Mode() }
