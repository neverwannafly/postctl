package ui

import (
	"strings"

	"charm.land/bubbles/v2/spinner"
	"charm.land/lipgloss/v2"
)

// EmptyState explains why there is nothing to show and what to do next. An
// empty list is not a failure: title says what is empty, body says whether it
// is new or not yet loaded, and action (optional) names the next step, for
// example "Press n to create a request."
func (t Theme) EmptyState(title, body, action string, width int) string {
	lines := []string{t.Bold().Render(title)}
	if body != "" {
		lines = append(lines, t.Render(TextSecondary, Wrap(body, width)))
	}
	if action != "" {
		lines = append(lines, "", t.Render(AccentPrimary, Wrap(action, width)))
	}
	return strings.Join(lines, "\n")
}

// ErrorState orders content as: what happened, what remains intact, next
// action. Empty intact or next are omitted. Technical detail belongs in a
// secondary view; callers must redact secrets before passing text in.
func (t Theme) ErrorState(what, intact, next string, width int) string {
	inner := max(1, width-2)
	lines := []string{t.Render(StatusDanger, t.Glyphs.Fail) + " " + t.Bold().Render(Wrap(what, inner))}
	if intact != "" {
		lines = append(lines, Indent(t.Render(TextSecondary, Wrap(intact, inner)), 2))
	}
	if next != "" {
		lines = append(lines, "", Indent(t.Render(TextPrimary, Wrap(next, inner)), 2))
	}
	return strings.Join(lines, "\n")
}

// ToastKind classifies a transient message.
type ToastKind int

// Toast kinds.
const (
	// ToastInfo is a neutral confirmation, for example "Environment: staging".
	ToastInfo ToastKind = iota
	// ToastSuccess confirms a completed action.
	ToastSuccess
	// ToastWarning reports a recoverable problem.
	ToastWarning
	// ToastError reports a failure; errors stay until dismissed.
	ToastError
)

// Toast renders the transient status line: a symbol and text, truncated to
// width. Text should be plain and redacted.
func (t Theme) Toast(kind ToastKind, text string, width int) string {
	var glyph string
	var tok Token
	switch kind {
	case ToastSuccess:
		glyph, tok = t.Glyphs.OK, StatusSuccess
	case ToastWarning:
		glyph, tok = t.Glyphs.Warn, StatusWarning
	case ToastError:
		glyph, tok = t.Glyphs.Fail, StatusDanger
	default:
		glyph, tok = t.Glyphs.OK, StatusInfo
	}
	return t.Render(tok, glyph) + " " + t.Render(TextPrimary, t.Truncate(text, max(1, width-2)))
}

// NewSpinner returns the shared spinner: a quiet braille dot, or a plain line
// in ASCII. The spinner is styled with the info token.
func (t Theme) NewSpinner() spinner.Model {
	sp := spinner.MiniDot
	if t.ASCII {
		sp = spinner.Line
	}
	return spinner.New(spinner.WithSpinner(sp), spinner.WithStyle(t.Style(StatusInfo)))
}

// Busy pairs an activity indicator with what is happening, for example
// "… Running pre-request script". frame is the current spinner frame, or ""
// for a static ellipsis (accessible mode, no animation). A spinner is never
// shown without text.
func (t Theme) Busy(frame, text string) string {
	if frame == "" {
		return t.Render(StatusInfo, t.Glyphs.Ellipsis) + " " + t.Render(TextPrimary, text)
	}
	return frame + " " + t.Render(TextPrimary, text)
}

// Section renders a section title (bold, accent when focused) with an
// optional quiet detail to its right, for example "Collections  12".
func (t Theme) Section(title, detail string, focused bool) string {
	st := t.Bold()
	prefix := "  "
	if focused {
		st = t.Style(AccentPrimary).Bold(true)
		prefix = t.Render(AccentPrimary, t.Glyphs.Select) + " "
	}
	out := prefix + st.Render(title)
	if detail != "" {
		out += "  " + t.Render(TextQuiet, detail)
	}
	return out
}

// Label is a small helper for "Label  value" metadata lines. label is padded
// to labelWidth cells.
func (t Theme) Label(labelWidth int, label, value string) string {
	return t.Render(TextSecondary, t.Fit(label, labelWidth)) + strings.Repeat(" ", SpaceSM) + value
}

// StyleOn is [Theme.Style] with the selected-surface background when sel is
// true and color is available. Backgrounds must be applied per segment: a
// nested style would reset an outer background mid-row. Without color the
// selection is carried by the marker and bold text alone.
func (t Theme) StyleOn(tok Token, sel bool) lipgloss.Style {
	s := t.Style(tok)
	if sel {
		if bg := t.SelectedBG(); bg != nil {
			s = s.Background(bg)
		}
	}
	return s
}
