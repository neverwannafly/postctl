// Package textin is the single-line text field shared by the request pane and
// the environment editor. It exists instead of bubbles/textinput because the
// URL bar highlights {{variables}} while you type, secrets are masked, and
// every field must share one look. The cursor is drawn as a reverse-video cell
// (no terminal cursor is positioned).
package textin

import (
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/shubham/postctl/internal/ui"
)

// Span is a highlighted run of a line: [From, To) in runes.
type Span struct {
	From, To int
	Tok      ui.Token
}

// Input is a small single-line editor.
type Input struct {
	value  []rune
	cursor int
	// Mask hides the value (secret fields).
	Mask bool
	// Placeholder is shown, quietly, while the value is empty.
	Placeholder string
	// Highlight returns highlight spans for the value; nil for none.
	Highlight func(s string) []Span
	// offset is the first visible rune, kept so the cursor stays in view.
	offset int
}

// New returns an input holding s with the cursor at the end.
func New(s string) *Input {
	l := &Input{}
	l.Set(s)
	return l
}

// Set replaces the value and puts the cursor at the end.
func (l *Input) Set(s string) {
	l.value = []rune(s)
	l.cursor = len(l.value)
	l.offset = 0
}

// Text is the current value.
func (l *Input) Text() string { return string(l.value) }

// Insert puts s at the cursor; newlines become spaces.
func (l *Input) Insert(s string) {
	// Single-line: newlines in pasted text become spaces.
	s = strings.NewReplacer("\r\n", " ", "\n", " ", "\r", " ", "\t", " ").Replace(s)
	rs := []rune(s)
	out := make([]rune, 0, len(l.value)+len(rs))
	out = append(out, l.value[:l.cursor]...)
	out = append(out, rs...)
	out = append(out, l.value[l.cursor:]...)
	l.value = out
	l.cursor += len(rs)
}

func (l *Input) deleteRange(from, to int) {
	if from < 0 || to > len(l.value) || from >= to {
		return
	}
	l.value = append(l.value[:from:from], l.value[to:]...)
	l.cursor = from
}

func isWord(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' }

func (l *Input) wordLeft() int {
	i := l.cursor
	for i > 0 && !isWord(l.value[i-1]) {
		i--
	}
	for i > 0 && isWord(l.value[i-1]) {
		i--
	}
	return i
}

func (l *Input) wordRight() int {
	i := l.cursor
	for i < len(l.value) && !isWord(l.value[i]) {
		i++
	}
	for i < len(l.value) && isWord(l.value[i]) {
		i++
	}
	return i
}

// Update applies a key or paste. It reports whether it handled the message.
func (l *Input) Update(msg tea.Msg) bool {
	switch m := msg.(type) {
	case tea.PasteMsg:
		l.Insert(m.Content)
		return true
	case tea.KeyPressMsg:
		switch m.String() {
		case "left", "ctrl+b":
			l.cursor = max(0, l.cursor-1)
		case "right", "ctrl+f":
			l.cursor = min(len(l.value), l.cursor+1)
		case "home", "ctrl+a":
			l.cursor = 0
		case "end", "ctrl+e":
			l.cursor = len(l.value)
		case "alt+left", "ctrl+left", "alt+b":
			l.cursor = l.wordLeft()
		case "alt+right", "ctrl+right", "alt+f":
			l.cursor = l.wordRight()
		case "backspace", "ctrl+h":
			l.deleteRange(l.cursor-1, l.cursor)
		case "delete", "ctrl+d":
			l.deleteRange(l.cursor, l.cursor+1)
		case "ctrl+w", "alt+backspace", "ctrl+backspace":
			l.deleteRange(l.wordLeft(), l.cursor)
		case "alt+d", "alt+delete", "ctrl+delete":
			l.deleteRange(l.cursor, l.wordRight())
		case "ctrl+k":
			l.deleteRange(l.cursor, len(l.value))
		case "ctrl+u":
			l.deleteRange(0, l.cursor)
		default:
			if m.Text != "" && m.Mod&(tea.ModCtrl|tea.ModAlt) == 0 {
				l.Insert(m.Text)
				return true
			}
			return false
		}
		return true
	}
	return false
}

// View renders the value in width cells. focused draws the cursor.
func (l *Input) View(th ui.Theme, width int, focused bool) string {
	width = max(1, width)
	if len(l.value) == 0 {
		if focused {
			return cursorCell(th, " ") + th.Render(ui.TextQuiet, th.Truncate(l.Placeholder, max(0, width-1)))
		}
		return th.Render(ui.TextQuiet, th.Truncate(l.Placeholder, width))
	}
	shown := l.value
	if l.Mask {
		shown = []rune(strings.Repeat(string([]rune(th.Glyphs.Mask)[:1]), len(l.value)))
	}
	// Keep the cursor inside the window (one cell reserved for the cursor at
	// the end of the line).
	room := width
	if focused && l.cursor >= len(shown) {
		room--
	}
	if l.cursor < l.offset {
		l.offset = l.cursor
	}
	if l.cursor > l.offset+room-1 {
		l.offset = l.cursor - room + 1
	}
	if l.offset < 0 {
		l.offset = 0
	}
	end := min(len(shown), l.offset+room)
	var spans []Span
	if l.Highlight != nil && !l.Mask {
		spans = l.Highlight(string(shown))
	}
	tokAt := func(i int) ui.Token {
		for _, s := range spans {
			if i >= s.From && i < s.To {
				return s.Tok
			}
		}
		return ui.TextPrimary
	}
	var sb strings.Builder
	run := func(from, to int) {
		for from < to {
			tok := tokAt(from)
			j := from + 1
			for j < to && tokAt(j) == tok {
				j++
			}
			sb.WriteString(th.Render(tok, string(shown[from:j])))
			from = j
		}
	}
	if !focused {
		run(l.offset, end)
		return sb.String()
	}
	run(l.offset, min(l.cursor, end))
	if l.cursor < len(shown) {
		sb.WriteString(cursorCell(th, string(shown[l.cursor])))
		run(l.cursor+1, end)
	} else {
		sb.WriteString(cursorCell(th, " "))
	}
	return sb.String()
}

// cursorCell draws one reverse-video cell.
func cursorCell(th ui.Theme, s string) string {
	st := lipgloss.NewStyle().Reverse(true)
	if c := th.Color(ui.AccentPrimary); c != nil {
		st = st.Foreground(c).Reverse(true)
	}
	return st.Render(s)
}
