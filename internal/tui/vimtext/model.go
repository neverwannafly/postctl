// Package vimtext provides a reusable modal Vim-style text editor for postctl.
package vimtext

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/rivo/uniseg"
	"github.com/shubham/postctl/internal/ui"
)

// Mode is the editor's current input mode.
type Mode int

// Editor modes.
const (
	// Normal interprets keys as motions and commands.
	Normal Mode = iota
	// Insert inserts typed text.
	Insert
	// Visual selects characters.
	Visual
	// VisualLine selects complete lines.
	VisualLine
)

// String returns the display name of a mode.
func (m Mode) String() string {
	switch m {
	case Insert:
		return "INSERT"
	case Visual:
		return "VISUAL"
	case VisualLine:
		return "VISUAL LINE"
	default:
		return "NORMAL"
	}
}

// Options configures a Model.
type Options struct {
	SingleLine  bool
	ReadOnly    bool
	StartInsert bool
	LineNumbers bool
	TabText     string
	Placeholder string
	Highlighter Highlighter
	Completer   Completer
	Clipboard   func(text string) tea.Cmd
	Redact      func(text string) string
}

// EventKind identifies the result of an Update.
type EventKind int

// Update event kinds.
const (
	// None means the message was handled without a parent-facing action.
	None EventKind = iota
	// Changed reports a buffer edit.
	Changed
	// Submit reports Enter in a single-line editor.
	Submit
	// Leave reports Escape from idle normal mode.
	Leave
	// Unhandled asks the parent to route the original key.
	Unhandled
	// Seq asks the parent to route an unsupported two-key sequence.
	Seq
)

// Event reports an editor action to its parent.
type Event struct {
	Kind EventKind
	Key  tea.KeyPressMsg
	Seq  string
}

// Span is a styled byte range within one line.
type Span struct {
	Start, End int
	Style      lipgloss.Style
}

// Highlighter returns styled byte ranges for each supplied line.
type Highlighter interface {
	Highlight(lines []string) [][]Span
}

// Item is one completion candidate.
type Item struct {
	Label, Detail, Insert string
}

// Completer supplies completion candidates for text before the cursor.
type Completer interface {
	Complete(lineBeforeCursor string) (items []Item, replace int)
}

type position struct{ row, col int }

type snapshot struct {
	lines      []string
	pos        position
	redoPos    position
	hasRedoPos bool
}

// Model is a modal text editor.
type Model struct {
	th   ui.Theme
	opts Options

	lines       []string
	row, col    int
	preferred   int
	mode        Mode
	focused     bool
	mask        bool
	width       int
	height      int
	top         int
	left        int
	pending     string
	count       int
	operator    byte
	operatorCnt int
	operatorAt  position
	operatorG   bool
	gPending    bool
	textObject  byte
	replace     bool
	findKind    byte
	lastFind    byte
	lastFindKey string
	lastFindPos int

	anchor             position
	visualEOL          bool
	visualLineStart    bool
	visualObjectFailed bool
	reg                string
	regLine            bool

	undo []snapshot
	redo []snapshot

	changeStart   *snapshot
	changeKeys    []tea.Msg
	lastChange    []tea.Msg
	replaying     bool
	insertCount   int
	insertKind    string
	insertSession string
	insertStart   position
	insertBreak   bool

	searching  bool
	searchDir  int
	searchText string
	lastSearch string
	lastDir    int

	completions []Item
	completeAt  int
	completeSel int

	highlights [][]Span
}

// New creates an empty editor.
func New(th ui.Theme, opts Options) *Model {
	if opts.TabText == "" {
		opts.TabText = "  "
	}
	if opts.Clipboard == nil {
		opts.Clipboard = tea.SetClipboard
	}
	if opts.Redact == nil {
		opts.Redact = func(s string) string { return s }
	}
	m := &Model{th: th, opts: opts, lines: []string{""}, width: 1, height: 1}
	if opts.StartInsert && !opts.ReadOnly {
		m.mode = Insert
	}
	m.rehighlight()
	return m
}

// SetSize sets the drawing area and updates scroll offsets.
func (m *Model) SetSize(w, h int) {
	m.width, m.height = max(1, w), max(1, h)
	m.ensureVisible()
}

// SetValue replaces the buffer, resets history, and moves the cursor to start.
func (m *Model) SetValue(s string) {
	if m.opts.SingleLine {
		s = collapseNewlines(s)
	}
	m.lines = strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
	if len(m.lines) == 0 {
		m.lines = []string{""}
	}
	m.row, m.col, m.top, m.left = 0, 0, 0, 0
	m.undo, m.redo, m.lastChange = nil, nil, nil
	m.clearPending()
	m.rehighlight()
	m.ensureVisible()
}

// Value returns the buffer text.
func (m *Model) Value() string { return strings.Join(m.lines, "\n") }

// Focus focuses the editor.
func (m *Model) Focus() tea.Cmd {
	m.focused = true
	return nil
}

// Blur removes focus and clears transient state.
func (m *Model) Blur() {
	m.focused = false
	m.searching = false
	m.completions = nil
	m.clearPending()
}

// SetMask hides the buffer behind the theme's mask glyph (one per cell) while
// it is drawn. The text itself is unchanged and still edited normally. Fields
// holding secrets use it; they should also set Options.Redact and leave
// Options.Completer nil.
func (m *Model) SetMask(on bool) { m.mask = on }

// Completing reports whether the completion popup is open, so a parent can
// leave Up, Down, Enter and Tab to it.
func (m *Model) Completing() bool { return len(m.completions) > 0 }

// Focused reports whether the editor has focus.
func (m *Model) Focused() bool { return m.focused }

// Mode returns the current mode.
func (m *Model) Mode() Mode { return m.mode }

// Pending returns the incomplete count, operator, or prefix.
func (m *Model) Pending() string {
	if m.searching {
		if m.searchDir < 0 {
			return "?" + m.searchText
		}
		return "/" + m.searchText
	}
	return m.pending
}

// Cursor returns the zero-based grapheme row and column.
func (m *Model) Cursor() (row, col int) { return m.row, m.col }

// SetCursor moves the cursor to a clamped zero-based grapheme position.
func (m *Model) SetCursor(row, col int) {
	m.row = min(max(0, row), len(m.lines)-1)
	m.col = min(max(0, col), m.lineLen(m.row))
	m.preferred = displayColumn(m.lines[m.row], m.col, m.opts.TabText)
	m.ensureVisible()
}

// Select enters visual mode with the supplied clamped endpoints.
func (m *Model) Select(r0, c0, r1, c1 int) {
	m.SetCursor(r0, c0)
	m.anchor = position{m.row, m.col}
	m.SetCursor(r1, c1)
	m.mode = Visual
}

func clusters(s string) []string {
	g := uniseg.NewGraphemes(s)
	var out []string
	for g.Next() {
		out = append(out, g.Str())
	}
	return out
}

func joinClusters(cs []string) string { return strings.Join(cs, "") }
func (m *Model) lineLen(row int) int  { return len(clusters(m.lines[row])) }

func (m *Model) snap() snapshot {
	return snapshot{lines: append([]string(nil), m.lines...), pos: position{m.row, m.col}}
}

func (m *Model) restore(s snapshot) {
	m.lines = append([]string(nil), s.lines...)
	m.row, m.col = s.pos.row, s.pos.col
	m.rehighlight()
	m.ensureVisible()
}

func collapseNewlines(s string) string {
	return strings.NewReplacer("\r\n", " ", "\n", " ", "\r", " ").Replace(s)
}

func (m *Model) clearPending() {
	m.pending, m.operator, m.textObject = "", 0, 0
	m.count, m.operatorCnt = 0, 0
	m.gPending, m.operatorG, m.replace, m.findKind = false, false, false, 0
}
