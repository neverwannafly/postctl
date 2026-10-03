// Package vimfield wraps [vimtext.Model] for the request and WebSocket panes.
// It adds what a pane needs around the editor: a result type that reports
// buffer changes and parent-facing events, entering a field in normal or
// insert mode, mode names for the footer, secret masking, and a pure render
// (vimtext sizes itself in Update, so drawing must work on a copy).
package vimfield

import (
	"regexp"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/rivo/uniseg"
	"github.com/shubham/postctl/internal/tui/vimtext"
	"github.com/shubham/postctl/internal/ui"
)

// Field is one modal text field.
type Field struct {
	// single is set for one-line fields, whose completion menu is drawn by
	// the owner (see [Field.Popup]) because the field has no rows to spare.
	single bool
	m      *vimtext.Model
	w, h   int
	// glyph is the single-cell glyph SetMask(true) hides the text behind.
	glyph string
}

// New builds an unfocused field. The theme must be the one the editor draws
// with; later theme changes need a new field (callers rebuild on
// ThemeChangedMsg).
func New(th ui.Theme, o vimtext.Options) *Field {
	return &Field{m: vimtext.New(th, o), w: 1, h: 1, single: o.SingleLine, glyph: maskGlyph(th)}
}

// Set replaces the text unless it already is the text, so a reload does not
// move the cursor or drop the field's undo history.
func (f *Field) Set(s string) {
	if f.m.Value() == s {
		return
	}
	f.m.SetValue(s)
}

// Replace puts s in the buffer and the cursor at its end. Callers use it for
// programmatic edits (formatting, snippets, $EDITOR results); the field's own
// undo history restarts.
func (f *Field) Replace(s string) {
	f.m.SetValue(s)
	f.MoveEnd()
}

// Text is the buffer.
func (f *Field) Text() string { return f.m.Value() }

// Focus gives the field the keyboard.
func (f *Field) Focus() { _ = f.m.Focus() }

// Blur takes the keyboard away and drops any pending command.
func (f *Field) Blur() { f.m.Blur() }

// Focused reports whether the field has the keyboard.
func (f *Field) Focused() bool { return f.m.Focused() }

// Fit sets the drawing area. Call it from Update paths (and pane SetSize),
// never from a View, so scroll offsets follow the cursor with the right size.
func (f *Field) Fit(w, h int) {
	f.w, f.h = max(1, w), max(1, h)
	f.m.SetSize(f.w, f.h)
}

// SetMask hides the text behind one mask glyph per character (on) or reveals
// it (off). It uses vimtext's Options.Mask, so cursor and selection still map
// to the real text.
func (f *Field) SetMask(on bool) {
	if on {
		f.m.SetMask(f.glyph)
		return
	}
	f.m.SetMask("")
}

// maskGlyph is the first cluster of the theme's mask glyph ("*" in ASCII).
func maskGlyph(th ui.Theme) string {
	cluster, _, _, _ := uniseg.FirstGraphemeClusterInString(th.Glyphs.Mask, -1)
	if cluster == "" {
		return "*"
	}
	return cluster
}

// ScrollLines scrolls the view by n lines (positive is down) without editing
// or changing the mode: the wheel uses it. The cursor stays inside the view.
func (f *Field) ScrollLines(n int) { f.m.ScrollLines(n) }

// Top is the zero-based index of the first visible line.
func (f *Field) Top() int { return f.m.Top() }

// Cursor is the zero-based row and column of the cursor.
func (f *Field) Cursor() (row, col int) { return f.m.Cursor() }

// MoveEnd puts the cursor at the end of the last line: after the last
// character in insert mode, on it in normal mode.
func (f *Field) MoveEnd() {
	row := strings.Count(f.m.Value(), "\n")
	f.m.SetCursor(row, 1<<20)
	if _, col := f.m.Cursor(); col > 0 && f.m.Mode() == vimtext.Normal {
		f.m.SetCursor(row, col-1)
	}
}

// LineCount is the number of lines.
func (f *Field) LineCount() int { return strings.Count(f.m.Value(), "\n") + 1 }

// Mode is the vim mode.
func (f *Field) Mode() vimtext.Mode { return f.m.Mode() }

// ModeName is NORMAL, INSERT, VISUAL or VISUAL LINE while the field has the
// keyboard, otherwise "".
func (f *Field) ModeName() string {
	if !f.m.Focused() {
		return ""
	}
	return f.m.Mode().String()
}

// Typing reports whether plain keys are text: insert and visual modes of a
// focused field. Normal mode is not typing; the leader works there.
func (f *Field) Typing() bool {
	return f.m.Focused() && f.m.Mode() != vimtext.Normal
}

// Completing reports whether the completion popup is open. While it is, Up,
// Down, Enter, Tab and Esc belong to the popup.
func (f *Field) Completing() bool { return f.m.Completing() }

// Pending is the count, operator or search text being typed, for the status
// line.
func (f *Field) Pending() string { return f.m.Pending() }

// Result is what one message did to a field.
type Result struct {
	// Changed reports that the buffer text differs from before the message.
	Changed bool
	// Event is the parent-facing event (Submit, Leave, Unhandled, Seq).
	Event vimtext.Event
	// Cmd is a command the editor wants run (the OSC 52 clipboard).
	Cmd tea.Cmd
}

// Update applies a message.
func (f *Field) Update(msg tea.Msg) Result {
	before := f.m.Value()
	cmd, ev := f.m.Update(msg)
	return Result{Changed: f.m.Value() != before, Event: ev, Cmd: cmd}
}

// Paste delivers pasted text. A bracketed paste while the field is in normal
// mode starts an insert first, as typing it would; in visual mode it is ignored.
func (f *Field) Paste(m tea.PasteMsg) Result {
	if f.m.Mode() == vimtext.Normal {
		f.Update(tea.KeyPressMsg(tea.Key{Code: 'i', Text: "i"}))
	}
	return f.Update(m)
}

// Begin focuses the field and starts in normal mode (start == "") or, for
// "i", "a", "A" and "I", performs that vim command so typing continues in
// insert mode.
func (f *Field) Begin(start string) Result {
	f.Focus()
	if start == "" {
		if f.m.Mode() != vimtext.Normal {
			return f.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEscape}))
		}
		return Result{}
	}
	r := []rune(start)
	return f.Update(tea.KeyPressMsg(tea.Key{Code: r[0], Text: start}))
}

// popupRows is the most rows a completion menu takes.
const popupRows = 4

// Render draws the field in w x h without touching it. A one-line field draws
// only its line; its completion menu comes from [Field.Popup].
func (f *Field) Render(w, h int) string {
	c := *f.m
	if f.single && h <= 1 {
		if !c.Completing() {
			c.SetSize(max(1, w), 1)
			return c.View()
		}
		c.SetSize(max(1, w), 1+popupRows)
		return strings.SplitN(c.View(), "\n", 2)[0]
	}
	c.SetSize(max(1, w), max(1, h))
	return c.View()
}

// Popup returns the completion menu of a one-line field as drawn rows (each
// padded to start under the cursor), or nil. The owner overlays them on the
// rows next to the field with [Overlay].
func (f *Field) Popup(w int) []string {
	if !f.single || !f.m.Completing() {
		return nil
	}
	c := *f.m
	c.SetSize(max(1, w), 1+popupRows)
	rows := strings.Split(c.View(), "\n")
	var out []string
	for _, r := range rows[1:] {
		if strings.TrimSpace(ansi.Strip(r)) == "" {
			break
		}
		out = append(out, r)
	}
	return out
}

// Overlay draws popup rows over lines: below the field's row when they fit,
// otherwise above it. indent is the field's first column; the text left of
// it stays. The result is a copy.
func Overlay(lines []string, row, indent int, popup []string) []string {
	if len(popup) == 0 || row < 0 || row >= len(lines) {
		return lines
	}
	out := append([]string(nil), lines...)
	first := row + 1
	if first+len(popup) > len(out) {
		first = row - len(popup)
	}
	if first < 0 {
		popup = popup[-first:]
		first = 0
	}
	for i, p := range popup {
		at := first + i
		if at >= len(out) || at == row {
			continue
		}
		left := ansi.Truncate(out[at], indent, "")
		left += strings.Repeat(" ", max(0, indent-ansi.StringWidth(left)))
		out[at] = left + p
	}
	return out
}

// View draws the field at the size last given to [Field.Fit].
func (f *Field) View() string { return f.Render(f.w, f.h) }

// varRE matches {{name}} references, also with inner spaces.
var varRE = regexp.MustCompile(`\{\{\s*([^{}]*?)\s*\}\}`)

type varHighlighter struct {
	th       ui.Theme
	base     vimtext.Highlighter
	resolved func(name string) bool
}

// VarHighlighter overlays {{variable}} references on base (which may be nil):
// names resolved reports true are drawn as syntax keys, the others as
// warnings, {{$dynamic}} names as keys. With a nil resolved function every
// reference is drawn as plain text.
func VarHighlighter(th ui.Theme, base vimtext.Highlighter, resolved func(name string) bool) vimtext.Highlighter {
	return &varHighlighter{th: th, base: base, resolved: resolved}
}

// Highlight implements [vimtext.Highlighter]. Variable spans come first so
// they win over the base spans underneath them.
func (h *varHighlighter) Highlight(lines []string) [][]vimtext.Span {
	var out [][]vimtext.Span
	if h.base != nil {
		out = h.base.Highlight(lines)
	}
	if len(out) != len(lines) {
		out = make([][]vimtext.Span, len(lines))
	}
	for i, line := range lines {
		if !strings.Contains(line, "{{") {
			continue
		}
		var spans []vimtext.Span
		for _, loc := range varRE.FindAllStringSubmatchIndex(line, -1) {
			name := line[loc[2]:loc[3]]
			tok := ui.SyntaxKey
			switch {
			case h.resolved == nil:
				tok = ui.TextPrimary
			case !strings.HasPrefix(name, "$") && !h.resolved(name):
				tok = ui.StatusWarning
			}
			spans = append(spans, vimtext.Span{Start: loc[0], End: loc[1], Style: h.th.Style(tok)})
		}
		out[i] = append(spans, out[i]...)
	}
	return out
}

// PureRef reports whether text is exactly one {{reference}}, optionally
// surrounded by whitespace. A secret field shows such a value as it is (it
// names a variable, not a secret); any literal text next to or around it keeps
// the field masked.
func PureRef(text string) bool {
	t := strings.TrimSpace(text)
	if !strings.HasPrefix(t, "{{") || !strings.HasSuffix(t, "}}") || len(t) <= 4 {
		return false
	}
	inner := t[2 : len(t)-2]
	return strings.TrimSpace(inner) != "" && !strings.Contains(inner, "{{") && !strings.Contains(inner, "}}")
}

// SetCursor moves the cursor to a clamped zero-based row and column.
func (f *Field) SetCursor(row, col int) { f.m.SetCursor(row, col) }
