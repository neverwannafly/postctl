package cli

import (
	"fmt"
	"strings"
	"time"

	"github.com/shubham/postctl/internal/history"
	"github.com/shubham/postctl/internal/ui"
)

// view renders linear text. Color reinforces a glyph or a status code; it is
// never the only signal (design-language.md).
type view struct {
	theme   ui.Theme
	noColor bool
	ascii   bool
}

func (v view) check() string { return v.theme.Glyphs.Check }

func (v view) fail() string { return v.theme.Glyphs.Fail }

func (v view) warn() string { return v.theme.Glyphs.Warn }

func (v view) skip() string {
	if v.ascii {
		return "o"
	}
	return v.theme.Glyphs.Expired
}

func (v view) dot() string { return v.theme.Glyphs.Dot }

func (v view) mask() string { return v.theme.Glyphs.Mask }

// paint colors s when color is enabled. The text itself stays in place.
func (v view) paint(tok ui.Token, s string) string {
	if v.noColor || s == "" {
		return s
	}
	return v.theme.Render(tok, s)
}

// statusLine renders "200 OK · 184 ms · 1.2 KB".
func (v view) statusLine(code int, reason string, elapsed time.Duration, size int64) string {
	if strings.TrimSpace(reason) == "" {
		reason = "OK"
	}
	head := strings.TrimSpace(fmt.Sprintf("%d %s", code, reason))
	head = v.paint(ui.StatusToken(code), head)
	meta := ui.FormatDuration(elapsed) + " " + v.dot() + " " + ui.FormatSize(size)
	return head + " " + v.dot() + " " + meta
}

func (v view) method(method string) string {
	label := ui.MethodLabel(method, 7)
	padded := label + strings.Repeat(" ", max(0, 7-len(label)))
	return v.paint(ui.MethodToken(method), padded)
}

// swapMask turns the history redactor's unicode mask into the active glyph.
func (v view) swapMask(s string) string {
	if !v.ascii || s == "" {
		return s
	}
	return strings.ReplaceAll(s, history.Mask, v.mask())
}
