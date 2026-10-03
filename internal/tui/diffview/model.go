package diffview

import (
	"encoding/json"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/shubham/postctl/internal/ui"
)

// SideSummary describes one compared response for the header row.
type SideSummary struct {
	// Label names the side, for example "Marked" or "Current".
	Label string
	// Status is the HTTP status code text.
	Status string
	// DurationMS is the response time in milliseconds.
	DurationMS int64
	// SizeBytes is the on-wire response size.
	SizeBytes int64
}

// Model is a scrollable unified diff viewer.
type Model struct {
	th ui.Theme
	w  int
	h  int

	Left, Right SideSummary
	Lines       []Line
	Hunks       []Hunk
	hunk        int
	scroll      int
	headersMode bool
	truncated   bool
	notice      string
}

// New builds a diff view from two redacted body texts and optional header
// texts (when headersMode is toggled on).
func New(th ui.Theme, left, right SideSummary, leftText, rightText string) *Model {
	leftText, trL := Cap(prettyJSON(leftText), DefaultMaxLines)
	rightText, trR := Cap(prettyJSON(rightText), DefaultMaxLines)
	lines := Diff(leftText, rightText)
	m := &Model{
		th:        th,
		Left:      left,
		Right:     right,
		Lines:     lines,
		Hunks:     Hunks(lines),
		truncated: trL || trR,
	}
	if trL || trR {
		m.notice = "Diff input truncated at 20,000 lines per side."
	}
	return m
}

// NewHeaders builds a diff of formatted header blocks.
func NewHeaders(th ui.Theme, left, right SideSummary, leftHdrs, rightHdrs []string) *Model {
	return New(th, left, right, strings.Join(leftHdrs, "\n"), strings.Join(rightHdrs, "\n"))
}

func prettyJSON(s string) string {
	trim := strings.TrimSpace(s)
	if trim == "" {
		return s
	}
	if !strings.HasPrefix(trim, "{") && !strings.HasPrefix(trim, "[") {
		return s
	}
	var v any
	if json.Unmarshal([]byte(trim), &v) != nil {
		return s
	}
	out, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return s
	}
	return string(out)
}

// SetSize implements the size contract for embedded screens.
func (m *Model) SetSize(w, h int) {
	m.w, m.h = w, h
}

// Scroll returns the current scroll offset for purity tests.
func (m *Model) Scroll() int { return m.scroll }

// HunkIndex returns the active hunk for purity tests.
func (m *Model) HunkIndex() int { return m.hunk }

// HeadersMode reports whether headers are shown instead of bodies.
func (m *Model) HeadersMode() bool { return m.headersMode }

// Update handles keys for scroll and hunk navigation.
func (m *Model) Update(msg tea.Msg) (*Model, tea.Cmd) {
	if k, ok := msg.(tea.KeyPressMsg); ok {
		switch k.String() {
		case "up", "k":
			m.scroll = max(0, m.scroll-1)
		case "down", "j":
			m.scroll++
		case "pgup":
			m.scroll = max(0, m.scroll-max(1, m.h-4))
		case "pgdown":
			m.scroll += max(1, m.h-4)
		case "n":
			m.nextHunk(1)
		case "N":
			m.nextHunk(-1)
		case "h", "t":
			m.headersMode = !m.headersMode
		}
	}
	return m, nil
}

func (m *Model) nextHunk(d int) {
	if len(m.Hunks) == 0 {
		return
	}
	m.hunk = (m.hunk + d + len(m.Hunks)) % len(m.Hunks)
	m.scroll = m.Hunks[m.hunk].Start
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
