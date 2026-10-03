package ui

import (
	"strconv"
	"strings"
)

// EnvInfo describes the active environment for the header. The zero value
// means "no environment".
type EnvInfo struct {
	// Name is the environment name; empty means none is active.
	Name string
	// Production flags a production environment (plan §0, Q10): the header
	// shows a warning badge. It never blocks sending.
	Production bool
}

// EnvBadge renders the environment marker. Production environments get the
// warning glyph and bold warning color; others a plain dot; no environment is
// quiet text. withLabel prefixes the word "env". The environment is never
// conveyed by color alone.
func (t Theme) EnvBadge(e EnvInfo, withLabel bool) string {
	label := ""
	if withLabel {
		label = t.Render(TextSecondary, "env") + "  "
	}
	switch {
	case e.Name == "":
		// "none" is meaningless alone, so the label always stays.
		return t.Render(TextQuiet, "env  none")
	case e.Production:
		name := e.Name
		if !strings.Contains(strings.ToLower(name), "prod") {
			name += " (prod)"
		}
		return label + t.Render(EnvProduction, t.Glyphs.Warn+" "+name)
	default:
		return label + t.Render(TextSecondary, t.Glyphs.OK+" "+e.Name)
	}
}

// HeaderInfo is the content of the header.
type HeaderInfo struct {
	// Workspace is the workspace name.
	Workspace string
	// Env is the active environment.
	Env EnvInfo
}

// Header renders the product mark, workspace name and the persistent
// environment badge on one line followed by a subtle rule. When space runs out
// the workspace name is shortened first, then the "env" label is dropped, and
// the environment name is only truncated as a last resort.
func (t Theme) Header(width int, h HeaderInfo) string {
	mark := t.Bold().Render("postctl")
	sep := strings.Repeat(" ", SpaceSM)

	withLabel := width >= BreakpointMedium
	env := t.EnvBadge(h.Env, withLabel)
	if Width(mark)+SpaceSM+Width(env) > width {
		env = t.EnvBadge(h.Env, false)
	}
	if Width(mark)+SpaceSM+Width(env) > width && h.Env.Name != "" {
		room := max(4, width-Width(mark)-SpaceSM)
		short := h.Env
		short.Name = t.Truncate(h.Env.Name, max(1, room-2))
		env = t.EnvBadge(short, false)
	}

	line := mark
	room := width - Width(mark) - Width(env) - 2*SpaceSM
	if h.Workspace != "" && room >= 4 {
		line += sep + t.Render(TextSecondary, t.Truncate(h.Workspace, room))
	}
	gap := max(1, width-Width(line)-Width(env))
	return line + strings.Repeat(" ", gap) + env + "\n" + t.Rule(width)
}

// Hint is one footer key hint. Lower Priority values survive longer when the
// terminal is narrow.
type Hint struct {
	// Key is the key label, for example "Ctrl-P".
	Key string
	// Label is the action, for example "Palette".
	Label string
	// Priority orders survival when space runs out; 0 is kept longest.
	Priority int
}

// KeyHints renders hints on one line, dropping the lowest-priority hints until
// the line fits. The key is bold, the label secondary.
func (t Theme) KeyHints(width int, hints []Hint) string {
	sep := strings.Repeat(" ", SpaceMD)
	render := func(hs []Hint) string {
		parts := make([]string, 0, len(hs))
		for _, h := range hs {
			if h.Label == "" {
				parts = append(parts, t.Bold().Render(h.Key)) // a status such as the vim mode
				continue
			}
			parts = append(parts, t.Bold().Render(h.Key)+" "+t.Render(TextSecondary, h.Label))
		}
		return strings.Join(parts, sep)
	}
	hs := append([]Hint(nil), hints...)
	for len(hs) > 1 && Width(render(hs)) > width {
		worst := 0
		for i, h := range hs {
			if h.Priority >= hs[worst].Priority {
				worst = i
			}
		}
		hs = append(hs[:worst], hs[worst+1:]...)
	}
	return t.Truncate(render(hs), width)
}

// Tab is one entry of a tab bar.
type Tab struct {
	// Label is the tab name.
	Label string
	// Count, when positive, is shown as Label(Count).
	Count int
	// Dot marks a tab with content that has no meaningful count, for example
	// a script ("Pre ●").
	Dot bool
}

func (t Theme) tabLabel(tb Tab) string {
	switch {
	case tb.Count > 0:
		return tb.Label + "(" + strconv.Itoa(tb.Count) + ")"
	case tb.Dot:
		return tb.Label + " " + t.Glyphs.OK
	}
	return tb.Label
}

// Tabs renders a tab bar as two lines: the labels, then a rule in which the
// active tab is underlined with a heavier accent segment. The active tab is
// bold (and accent when focused); the others are secondary.
func (t Theme) Tabs(width int, tabs []Tab, active int, focused bool) string {
	var labels, rule strings.Builder
	used := 0
	for i, tb := range tabs {
		text := t.tabLabel(tb)
		w := Width(text)
		gap := 0
		if i > 0 {
			gap = SpaceSM
		}
		if used+gap+w > width {
			break
		}
		if gap > 0 {
			labels.WriteString(strings.Repeat(" ", gap))
			rule.WriteString(t.Rule(gap))
		}
		if i == active {
			tok := TextPrimary
			if focused {
				tok = AccentPrimary
			}
			labels.WriteString(t.Style(tok).Bold(true).Render(text))
			rule.WriteString(t.Style(AccentPrimary).Render(strings.Repeat(t.Glyphs.RuleActive, w)))
		} else {
			labels.WriteString(t.Render(TextSecondary, text))
			rule.WriteString(t.Rule(w))
		}
		used += gap + w
	}
	if used < width {
		rule.WriteString(t.Rule(width - used))
	}
	return labels.String() + "\n" + rule.String()
}
