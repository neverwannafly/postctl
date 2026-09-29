// Package ui holds postctl's Clear Signal theme: semantic color tokens with
// dark and light values, glyph sets, and the Theme that resolves them into
// Lip Gloss styles. It contains no components; those are added by the TUI
// workstream on top of these tokens.
//
// Everything here is pure: functions take state and return values. Components
// must ask for semantic tokens and never introduce one-off colors (see
// docs/design-language.md).
package ui

import (
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
)

// Spacing tokens, in terminal cells.
const (
	// SpaceXS separates an icon from its label.
	SpaceXS = 1
	// SpaceSM is the gap between related controls or rows.
	SpaceSM = 2
	// SpaceMD is the gap between sections.
	SpaceMD = 3
	// SpaceLG is the screen edge inset or major grouping gap.
	SpaceLG = 4
)

// Layout breakpoints in terminal columns (design-language.md, "Responsive
// layout").
const (
	// BreakpointWide is the minimum width for three panes.
	BreakpointWide = 140
	// BreakpointMedium is the minimum width for two panes; below it the UI
	// is a single pane with overlays.
	BreakpointMedium = 100
)

// Layout is the pane arrangement for a terminal width.
type Layout int

// Layouts.
const (
	// LayoutSingle shows one pane at a time; the sidebar is an overlay.
	LayoutSingle Layout = iota
	// LayoutTwoPane shows the sidebar beside stacked or tabbed request and
	// response panes.
	LayoutTwoPane
	// LayoutThreePane shows sidebar, request and response side by side.
	LayoutThreePane
)

// LayoutFor returns the layout for a terminal width in columns.
func LayoutFor(width int) Layout {
	switch {
	case width >= BreakpointWide:
		return LayoutThreePane
	case width >= BreakpointMedium:
		return LayoutTwoPane
	default:
		return LayoutSingle
	}
}

// Token is a semantic color role. Components use tokens, never raw colors.
type Token string

// Core Clear Signal tokens.
const (
	// TextPrimary is main labels, headings and values.
	TextPrimary Token = "text.primary"
	// TextSecondary is supporting descriptions and metadata.
	TextSecondary Token = "text.secondary"
	// TextQuiet is timestamps and low-priority hints; never essential.
	TextQuiet Token = "text.quiet"
	// AccentPrimary is current selection, primary action and active navigation.
	AccentPrimary Token = "accent.primary"
	// StatusSuccess is ready, healthy, passed.
	StatusSuccess Token = "status.success"
	// StatusWarning is attention needed or recoverable trouble.
	StatusWarning Token = "status.warning"
	// StatusDanger is failed or destructive.
	StatusDanger Token = "status.danger"
	// StatusInfo is neutral informational state.
	StatusInfo Token = "status.info"
	// BorderSubtle is section separators and card edges.
	BorderSubtle Token = "border.subtle"
	// SurfaceSelected is the selected row background.
	SurfaceSelected Token = "surface.selected"
)

// HTTP method tokens. Always render the method text next to the color.
const (
	// MethodGet is GET (info).
	MethodGet Token = "method.get"
	// MethodPost is POST (success).
	MethodPost Token = "method.post"
	// MethodPut is PUT (warning).
	MethodPut Token = "method.put"
	// MethodPatch is PATCH (warning).
	MethodPatch Token = "method.patch"
	// MethodDelete is DELETE (danger).
	MethodDelete Token = "method.delete"
	// MethodOther is every other method (secondary).
	MethodOther Token = "method.other"
)

// Syntax tokens for JSON and other highlighted bodies, derived from the base
// palette so no new colors are introduced.
const (
	// SyntaxKey is object keys.
	SyntaxKey Token = "syntax.key"
	// SyntaxString is string values.
	SyntaxString Token = "syntax.string"
	// SyntaxNumber is numbers.
	SyntaxNumber Token = "syntax.number"
	// SyntaxBool is true and false.
	SyntaxBool Token = "syntax.bool"
	// SyntaxNull is null.
	SyntaxNull Token = "syntax.null"
)

// Response status class tokens. Always render the numeric code and reason.
const (
	// Status2xx is success responses.
	Status2xx Token = "status.2xx"
	// Status3xx is redirects.
	Status3xx Token = "status.3xx"
	// Status4xx is client errors.
	Status4xx Token = "status.4xx"
	// Status5xx is server errors and transport failures.
	Status5xx Token = "status.5xx"
)

// EnvProduction is the header badge for an environment flagged production. It
// is a warning only; it never blocks sending (plan §0, Q10).
const EnvProduction Token = "env.production"

type palette struct{ light, dark string }

// base holds the only hex values in the theme.
var base = map[Token]palette{
	TextPrimary:     {"#1F2933", "#E6EDF3"},
	TextSecondary:   {"#526170", "#9EABB8"},
	TextQuiet:       {"#667482", "#7F8C99"},
	AccentPrimary:   {"#006A70", "#53D6C3"},
	StatusSuccess:   {"#16794F", "#47D18C"},
	StatusWarning:   {"#8B5E00", "#FFD166"},
	StatusDanger:    {"#B42332", "#FF7583"},
	StatusInfo:      {"#175CD3", "#7CC4FF"},
	BorderSubtle:    {"#D7DEE5", "#394652"},
	SurfaceSelected: {"#E5F3F2", "#1E343A"},
}

// alias maps derived tokens onto base tokens.
var alias = map[Token]Token{
	MethodGet:     StatusInfo,
	MethodPost:    StatusSuccess,
	MethodPut:     StatusWarning,
	MethodPatch:   StatusWarning,
	MethodDelete:  StatusDanger,
	MethodOther:   TextSecondary,
	SyntaxKey:     StatusInfo,
	SyntaxString:  StatusSuccess,
	SyntaxNumber:  StatusWarning,
	SyntaxBool:    AccentPrimary,
	SyntaxNull:    TextQuiet,
	Status2xx:     StatusSuccess,
	Status3xx:     StatusInfo,
	Status4xx:     StatusWarning,
	Status5xx:     StatusDanger,
	EnvProduction: StatusWarning,
}

// resolve follows aliases to a base token.
func resolve(t Token) Token {
	for i := 0; i < 4; i++ {
		next, ok := alias[t]
		if !ok {
			return t
		}
		t = next
	}
	return t
}

// Tokens returns every defined token, base and derived, for documentation and
// tests.
func Tokens() []Token {
	out := make([]Token, 0, len(base)+len(alias))
	for t := range base {
		out = append(out, t)
	}
	for t := range alias {
		out = append(out, t)
	}
	return out
}

// Hex returns the source palette value of a token for a dark or light
// terminal, or "" for an unknown token.
func Hex(t Token, dark bool) string {
	p, ok := base[resolve(t)]
	if !ok {
		return ""
	}
	if dark {
		return p.dark
	}
	return p.light
}

// MethodToken returns the token for an HTTP method (case-insensitive).
func MethodToken(method string) Token {
	switch strings.ToUpper(method) {
	case "GET":
		return MethodGet
	case "POST":
		return MethodPost
	case "PUT":
		return MethodPut
	case "PATCH":
		return MethodPatch
	case "DELETE":
		return MethodDelete
	default:
		return MethodOther
	}
}

// StatusToken returns the response status class token for an HTTP status
// code. Codes outside 200-599 (including 1xx and 0 for transport failures)
// map to the danger class when they are failures and to info otherwise.
func StatusToken(code int) Token {
	switch {
	case code >= 200 && code < 300:
		return Status2xx
	case code >= 300 && code < 400:
		return Status3xx
	case code >= 400 && code < 500:
		return Status4xx
	case code >= 500 && code < 600:
		return Status5xx
	case code >= 100 && code < 200:
		return StatusInfo
	default:
		return Status5xx
	}
}

// Theme resolves tokens for the detected terminal background. In NoColor mode
// hierarchy comes from bold and prefixes only.
type Theme struct {
	// Dark reports a dark terminal background (from tea.BackgroundColorMsg).
	Dark bool
	// NoColor disables all color (NO_COLOR or --accessible).
	NoColor bool
	// ASCII selects the ASCII glyph set.
	ASCII bool
	// Glyphs is the symbol set for this theme.
	Glyphs Glyphs
}

// NewTheme builds a theme. dark comes from tea.BackgroundColorMsg.
func NewTheme(dark, noColor, ascii bool) Theme {
	return Theme{Dark: dark, NoColor: noColor, ASCII: ascii, Glyphs: GlyphSet(ascii)}
}

func (t Theme) pick(p palette) color.Color {
	return lipgloss.LightDark(t.Dark)(lipgloss.Color(p.light), lipgloss.Color(p.dark))
}

// Color returns the resolved color for a token, or nil in no-color mode or for
// an unknown token.
func (t Theme) Color(tok Token) color.Color {
	if t.NoColor {
		return nil
	}
	p, ok := base[resolve(tok)]
	if !ok {
		return nil
	}
	return t.pick(p)
}

// Style returns a foreground style for tok. Accent, status, method and
// production tokens are bold so they stay distinct without color.
func (t Theme) Style(tok Token) lipgloss.Style {
	s := lipgloss.NewStyle()
	if isEmphasis(tok) {
		s = s.Bold(true)
	}
	if c := t.Color(tok); c != nil {
		s = s.Foreground(c)
	}
	return s
}

func isEmphasis(tok Token) bool {
	switch tok {
	case AccentPrimary, StatusSuccess, StatusWarning, StatusDanger,
		MethodGet, MethodPost, MethodPut, MethodPatch, MethodDelete,
		Status2xx, Status3xx, Status4xx, Status5xx, EnvProduction:
		return true
	}
	return false
}

// Bold is primary text in bold.
func (t Theme) Bold() lipgloss.Style { return t.Style(TextPrimary).Bold(true) }

// Render is shorthand for t.Style(tok).Render(s).
func (t Theme) Render(tok Token, s string) string { return t.Style(tok).Render(s) }

// SelectedBG is the surface.selected fill; nil in no-color mode.
func (t Theme) SelectedBG() color.Color { return t.Color(SurfaceSelected) }

// BorderColor is border.subtle; nil in no-color mode.
func (t Theme) BorderColor() color.Color { return t.Color(BorderSubtle) }

// Glyphs is the symbol set; ASCII is the fallback for terminals without
// Unicode.
type Glyphs struct {
	// Select marks the selected row.
	Select string
	// OK, Pending, Fail, Warn and Expired prefix status text.
	OK, Pending, Fail, Warn, Expired string
	// Ellipsis is the truncation marker.
	Ellipsis string
	// BarFull and BarEmpty draw progress bars.
	BarFull, BarEmpty string
	// Rule draws separators.
	Rule string
	// Dot separates inline metadata.
	Dot string
	// Arrow points to a nested or next item.
	Arrow string
	// Check marks a passed test.
	Check string
	// Expanded and Collapsed mark tree folders.
	Expanded, Collapsed string
	// RuleActive underlines the active tab.
	RuleActive string
	// VRule separates side-by-side panes.
	VRule string
	// Mask replaces secret values.
	Mask string
	// Dirty marks unsaved changes after a name.
	Dirty string
}

// GlyphSet returns the Unicode glyphs, or the ASCII fallback.
func GlyphSet(ascii bool) Glyphs {
	if ascii {
		return Glyphs{
			Select: ">", OK: "*", Pending: "~", Fail: "x", Warn: "!", Expired: "o", Ellipsis: "...",
			BarFull: "#", BarEmpty: "-", Rule: "-", Dot: "|", Arrow: ">", Check: "+",
			Expanded: "v", Collapsed: ">",
			RuleActive: "=", VRule: "|", Mask: "****", Dirty: "*",
		}
	}
	return Glyphs{
		Select: "›", OK: "●", Pending: "◐", Fail: "×", Warn: "!", Expired: "○", Ellipsis: "…",
		BarFull: "▰", BarEmpty: "▱", Rule: "─", Dot: "·", Arrow: "›", Check: "✓",
		Expanded: "▾", Collapsed: "▸",
		RuleActive: "━", VRule: "│", Mask: "••••", Dirty: "●",
	}
}
