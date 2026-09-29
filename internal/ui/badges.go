package ui

import (
	"fmt"
	"strings"
	"time"
)

// MethodLabel returns the text shown for an HTTP method in a column width
// cells wide: the method itself when it fits, otherwise a well-known
// abbreviation (DELETE becomes DEL), otherwise a truncation. The text is
// never replaced by color or a glyph.
func MethodLabel(method string, width int) string {
	m := strings.ToUpper(strings.TrimSpace(method))
	if m == "" {
		m = "-"
	}
	if len(m) <= width {
		return m
	}
	switch m {
	case "DELETE":
		return "DEL"
	case "OPTIONS":
		return "OPT"
	case "CONNECT":
		return "CONN"
	}
	if width <= 0 {
		return ""
	}
	return m[:width]
}

// MethodBadge renders the method in its token color, bold, padded on the
// right to exactly width cells so names line up.
func (t Theme) MethodBadge(method string, width int) string {
	label := MethodLabel(method, width)
	text := label + strings.Repeat(" ", max(0, width-len(label)))
	return t.Style(MethodToken(method)).Render(text)
}

// StatusKind classifies a status line.
type StatusKind int

// Status kinds. Each pairs a stable symbol with a token, so meaning never
// relies on color alone.
const (
	// StatusReady is a healthy or idle state: "● Ready".
	StatusReady StatusKind = iota
	// StatusRunning is active work: "… Waiting for response 1.2s".
	StatusRunning
	// StatusAttention needs a look but is recoverable: "! Slow response".
	StatusAttention
	// StatusFailed is a failure: "× Request failed".
	StatusFailed
	// StatusNote is neutral information: "● 3 requests".
	StatusNote
)

// Status renders a symbol plus text for kind, for example "● Ready" or
// "× Request failed".
func (t Theme) Status(kind StatusKind, text string) string {
	var glyph string
	var tok Token
	switch kind {
	case StatusRunning:
		glyph, tok = t.Glyphs.Ellipsis, StatusInfo
	case StatusAttention:
		glyph, tok = t.Glyphs.Warn, StatusWarning
	case StatusFailed:
		glyph, tok = t.Glyphs.Fail, StatusDanger
	case StatusNote:
		glyph, tok = t.Glyphs.OK, TextSecondary
	default:
		glyph, tok = t.Glyphs.OK, StatusSuccess
	}
	return t.Render(tok, glyph) + " " + t.Render(TextPrimary, text)
}

// FormatDuration renders an elapsed time compactly: "184 ms", "1.2 s",
// "2m 5s".
func FormatDuration(d time.Duration) string {
	switch {
	case d < 0:
		return "0 ms"
	case d < time.Second:
		return fmt.Sprintf("%d ms", d.Milliseconds())
	case d < time.Minute:
		return fmt.Sprintf("%.1f s", d.Seconds())
	}
	d = d.Round(time.Second)
	return fmt.Sprintf("%dm %ds", int(d.Minutes()), int(d.Seconds())%60)
}

// FormatSize renders a byte count: "512 B", "1.2 KB", "3.4 MB".
func FormatSize(n int64) string {
	const unit = 1024
	switch {
	case n < unit:
		return fmt.Sprintf("%d B", n)
	case n < unit*unit:
		return fmt.Sprintf("%.1f KB", float64(n)/unit)
	case n < unit*unit*unit:
		return fmt.Sprintf("%.1f MB", float64(n)/(unit*unit))
	}
	return fmt.Sprintf("%.1f GB", float64(n)/(unit*unit*unit))
}

// ResponseChip renders "201 Created  184 ms  1.2 KB": the code and reason in
// their status-class token (bold), then time and size in secondary text. A
// code of zero means the transport failed and no response arrived; reason then
// carries the short failure.
func (t Theme) ResponseChip(code int, reason string, elapsed time.Duration, size int64) string {
	var head string
	if code == 0 {
		text := "No response"
		if reason != "" {
			text += ": " + reason
		}
		head = t.Render(Status5xx, t.Glyphs.Fail+" "+text)
	} else {
		head = t.Render(StatusToken(code), strings.TrimSpace(fmt.Sprintf("%d %s", code, reason)))
	}
	meta := []string{}
	if elapsed > 0 {
		meta = append(meta, FormatDuration(elapsed))
	}
	if size > 0 {
		meta = append(meta, FormatSize(size))
	}
	if len(meta) == 0 {
		return head
	}
	sep := strings.Repeat(" ", SpaceSM)
	return head + sep + t.Render(TextSecondary, strings.Join(meta, sep))
}
