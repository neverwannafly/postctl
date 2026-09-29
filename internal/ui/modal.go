package ui

import (
	"strings"

	"charm.land/lipgloss/v2"
)

// Modal wraps content in a bordered box width cells wide (border included).
// Borders are reserved for modals and explicit selection regions. A danger
// modal uses the danger border color; without color the border is still drawn,
// so the modal reads as a modal.
func (t Theme) Modal(content string, width int, danger bool) string {
	b := lipgloss.RoundedBorder()
	if t.ASCII {
		b = lipgloss.ASCIIBorder()
	}
	inner := max(1, width-2-2*SpaceXS)
	s := lipgloss.NewStyle().Border(b).Padding(0, SpaceXS).Width(inner + 2 + 2*SpaceXS)
	tok := AccentPrimary
	if danger {
		tok = StatusDanger
	}
	c := t.Color(tok)
	if c != nil {
		s = s.BorderForeground(c)
	}
	return s.Render(content)
}

// Confirm is the state of a confirmation dialog. Build one with [NewConfirm].
type Confirm struct {
	// Title names the action, for example "Delete request".
	Title string
	// Body names the affected item and what will happen.
	Body string
	// Choices are the buttons, safe choice first. The last one is the action.
	Choices []string
	// Focus is the focused choice.
	Focus int
	// Danger draws the danger variant.
	Danger bool
}

// NewConfirm returns a two-choice dialog [Cancel, action]. A danger dialog
// starts with Cancel focused, so a stray Enter never destroys anything; an
// ordinary dialog starts on the action.
func NewConfirm(title, body, action string, danger bool) Confirm {
	c := Confirm{Title: title, Body: body, Choices: []string{"Cancel", action}, Danger: danger}
	if !danger {
		c.Focus = 1
	}
	return c
}

// Move shifts the focused choice by delta, clamped to the choices.
func (c *Confirm) Move(delta int) {
	c.Focus = min(max(c.Focus+delta, 0), len(c.Choices)-1)
}

// Accepted reports whether the focused choice is the action.
func (c Confirm) Accepted() bool { return c.Focus == len(c.Choices)-1 && len(c.Choices) > 1 }

// ConfirmView renders the dialog. width is the outer width including border.
func (t Theme) ConfirmView(c Confirm, width int) string {
	inner := max(1, width-2-2*SpaceXS)
	title := t.Bold().Render(c.Title)
	if c.Danger {
		title = t.Render(StatusDanger, t.Glyphs.Warn) + " " + title
	}
	btns := make([]string, len(c.Choices))
	for i, label := range c.Choices {
		text := "[ " + label + " ]"
		switch {
		case i == c.Focus && c.Danger && i == len(c.Choices)-1:
			btns[i] = t.Render(StatusDanger, t.Glyphs.Select+" "+text)
		case i == c.Focus:
			btns[i] = t.Render(AccentPrimary, t.Glyphs.Select+" "+text)
		default:
			btns[i] = t.Render(TextSecondary, "  "+text)
		}
	}
	body := t.Render(TextPrimary, Wrap(c.Body, inner))
	content := title + "\n\n" + body + "\n\n" + strings.Join(btns, strings.Repeat(" ", SpaceSM))
	return t.Modal(content, width, c.Danger)
}
