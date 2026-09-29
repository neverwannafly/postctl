package request

import (
	"strings"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/shubham/postctl/internal/ui"
)

// textEditor wraps a bubbles textarea with the app's look: theme tokens only,
// a virtual cursor and (for scripts) line numbers.
type textEditor struct {
	ta      textarea.Model
	th      ui.Theme
	numbers bool
}

func newTextEditor(th ui.Theme, numbers bool) *textEditor {
	e := &textEditor{th: th, numbers: numbers}
	e.ta = textarea.New()
	e.ta.Prompt = ""
	e.ta.ShowLineNumbers = numbers
	e.ta.CharLimit = 0
	e.ta.MaxWidth = 0
	if numbers {
		e.ta.MaxHeight = 999 // sets the line-number gutter to three digits
	} else {
		e.ta.MaxHeight = 0
	}
	e.ta.SetVirtualCursor(true)
	e.restyle()
	return e
}

func (e *textEditor) setTheme(th ui.Theme) {
	e.th = th
	e.restyle()
}

func (e *textEditor) restyle() {
	th := e.th
	state := func(text, num, curNum ui.Token) textarea.StyleState {
		return textarea.StyleState{
			Base:             lipgloss.NewStyle(),
			Text:             th.Style(text),
			LineNumber:       th.Style(num),
			CursorLineNumber: th.Style(curNum),
			CursorLine:       lipgloss.NewStyle(),
			EndOfBuffer:      th.Style(ui.TextQuiet),
			Placeholder:      th.Style(ui.TextQuiet),
			Prompt:           th.Style(ui.TextQuiet),
			Selection:        lipgloss.NewStyle().Reverse(true),
		}
	}
	e.ta.SetStyles(textarea.Styles{
		Focused: state(ui.TextPrimary, ui.TextQuiet, ui.AccentPrimary),
		Blurred: state(ui.TextPrimary, ui.TextQuiet, ui.TextQuiet),
		Cursor:  textarea.CursorStyle{Color: th.Color(ui.AccentPrimary), Blink: false},
	})
}

// resize sets the drawing area.
func (e *textEditor) resize(w, h int) {
	e.ta.SetWidth(max(4, w))
	e.ta.SetHeight(max(1, h))
}

// set replaces the text unless it is already the text, so a reload does not
// move the cursor.
func (e *textEditor) set(s string) {
	if e.ta.Value() == s {
		return
	}
	e.ta.SetValue(s)
	e.ta.MoveToBegin()
}

func (e *textEditor) text() string { return e.ta.Value() }

func (e *textEditor) focus() {
	e.ta.Focus()
}

func (e *textEditor) blur() { e.ta.Blur() }

// update applies a message and reports whether the text changed.
func (e *textEditor) update(msg tea.Msg) bool {
	before := e.ta.Value()
	var cmd tea.Cmd
	e.ta, cmd = e.ta.Update(msg)
	_ = cmd // the virtual cursor does not blink, so there is nothing to schedule
	return e.ta.Value() != before
}

func (e *textEditor) view() string { return e.ta.View() }

// pos is the 1-based line and column of the cursor.
func (e *textEditor) pos() (line, col int) { return e.ta.Line() + 1, e.ta.Column() + 1 }

// insertLine puts text on new lines at the cursor (snippets).
func (e *textEditor) insertText(s string) {
	e.ta.InsertString(s)
}

// lineCount is the number of lines.
func (e *textEditor) lineCount() int { return strings.Count(e.ta.Value(), "\n") + 1 }
