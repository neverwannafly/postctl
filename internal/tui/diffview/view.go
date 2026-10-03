package diffview

import (
	"fmt"
	"strings"

	"github.com/shubham/postctl/internal/ui"
)

// View renders the unified diff at the model size.
func (m *Model) View() string {
	if m == nil {
		return ""
	}
	t := m.th
	var b strings.Builder
	b.WriteString(t.Bold().Render("Response diff"))
	b.WriteString("\n")
	b.WriteString(m.summaryLine(t))
	b.WriteString("\n")
	if m.truncated && m.notice != "" {
		b.WriteString(t.Render(ui.StatusWarning, m.notice))
		b.WriteString("\n")
	}
	if len(m.Lines) == 0 || allSame(m.Lines) {
		b.WriteString(t.EmptyState("No differences", "The two responses match.", "", m.w))
		return b.String()
	}
	rows := max(1, m.h-4)
	start := min(m.scroll, max(0, len(m.Lines)-rows))
	end := min(len(m.Lines), start+rows)
	for i := start; i < end; i++ {
		b.WriteString(renderLine(t, m.Lines[i]))
		b.WriteByte('\n')
	}
	if len(m.Hunks) > 0 {
		b.WriteString(t.Render(ui.TextQuiet, fmt.Sprintf("Hunk %d/%d  n/N hunk  h toggle headers", m.hunk+1, len(m.Hunks))))
	}
	return b.String()
}

func allSame(lines []Line) bool {
	for _, ln := range lines {
		if ln.Kind != LineSame {
			return false
		}
	}
	return true
}

func (m *Model) summaryLine(t ui.Theme) string {
	left := fmt.Sprintf("%s  %s  %d ms  %d B", m.Left.Label, m.Left.Status, m.Left.DurationMS, m.Left.SizeBytes)
	right := fmt.Sprintf("%s  %s  %d ms  %d B", m.Right.Label, m.Right.Status, m.Right.DurationMS, m.Right.SizeBytes)
	return t.Render(ui.TextSecondary, left) + "  ↔  " + t.Render(ui.TextSecondary, right)
}

func renderLine(t ui.Theme, ln Line) string {
	switch ln.Kind {
	case LineAdd:
		return t.Render(ui.StatusSuccess, "+ "+ln.Text)
	case LineRemove:
		return t.Render(ui.StatusDanger, "- "+ln.Text)
	default:
		return t.Render(ui.TextPrimary, "  "+ln.Text)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
