package ui

import (
	"strings"
)

// KVRow is one row of a key-value table (headers, query parameters, form
// fields, variables).
type KVRow struct {
	// Key is the name column.
	Key string
	// Value is the value column.
	Value string
	// Description is optional documentation, shown only when there is room.
	Description string
	// Disabled rows show "[ ]" and secondary text.
	Disabled bool
	// Secret values render masked (••••, ASCII ****) unless revealed.
	Secret bool
}

// KVTableOptions configures [Theme.KVTable].
type KVTableOptions struct {
	// Width is the table width in cells.
	Width int
	// Focus is the focused row index, or -1 for none.
	Focus int
	// Reveal shows secret values (only while the reveal key is held).
	Reveal bool
	// Header adds a Key / Value / Description header row.
	Header bool
}

// KVTable renders rows with an enable marker, key, value and description.
// The focused row carries the accent marker; the enabled marker is "[x]" and a
// disabled row shows "[ ]" in secondary text. Value and key get width before
// the description does.
func (t Theme) KVTable(rows []KVRow, o KVTableOptions) string {
	const markers = 2 + 3 + 1 // focus marker + "[x]" + space
	w := max(o.Width, markers+8)

	keyW := 0
	for _, r := range rows {
		keyW = max(keyW, Width(r.Key))
	}
	keyW = min(max(keyW, 3), max(8, (w-markers)*35/100))
	rest := w - markers - keyW - SpaceSM
	descW := 0
	if rest >= 40 {
		descW = rest * 30 / 100
	}
	valW := rest
	if descW > 0 {
		valW = rest - descW - SpaceSM
	}
	valW = max(valW, 1)

	var lines []string
	if o.Header {
		h := strings.Repeat(" ", markers) + t.Fit("Key", keyW) + strings.Repeat(" ", SpaceSM) + t.Fit("Value", valW)
		if descW > 0 {
			h += strings.Repeat(" ", SpaceSM) + "Description"
		}
		lines = append(lines, t.Render(TextSecondary, t.Truncate(h, w)))
	}
	for i, r := range rows {
		focused := i == o.Focus
		marker := "  "
		if focused {
			marker = t.Render(AccentPrimary, t.Glyphs.Select) + " "
		}
		box := "[x]"
		if r.Disabled {
			box = "[ ]"
		}
		value := r.Value
		if r.Secret && !o.Reveal {
			value = t.Glyphs.Mask
		}
		keyTok, valTok := TextPrimary, TextPrimary
		if r.Disabled {
			keyTok, valTok = TextSecondary, TextSecondary
		}
		keyStyle := t.Style(keyTok)
		if focused {
			keyStyle = keyStyle.Bold(true)
		}
		line := marker + t.Render(TextSecondary, box) + " " +
			keyStyle.Render(t.Fit(r.Key, keyW)) + strings.Repeat(" ", SpaceSM) +
			t.Render(valTok, t.Fit(value, valW))
		if descW > 0 {
			line += strings.Repeat(" ", SpaceSM) + t.Render(TextQuiet, t.Truncate(r.Description, descW))
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}
