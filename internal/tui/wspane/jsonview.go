package wspane

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/shubham/postctl/internal/ui"
)

// maxPretty is the largest payload that is pretty-printed and highlighted;
// bigger ones are shown as plain wrapped text.
const maxPretty = 256 << 10

// maxDump is how many bytes of a binary payload the hex dump shows.
const maxDump = 4 << 10

// seg is a run of text in one token style.
type seg struct {
	text string
	tok  ui.Token
}

func renderSegs(th ui.Theme, segs []seg) string {
	var sb strings.Builder
	for _, s := range segs {
		sb.WriteString(th.Render(s.tok, s.text))
	}
	return sb.String()
}

// lexJSONLine splits one line of indented JSON into highlighted runs.
func lexJSONLine(line string) []seg {
	var out []seg
	n := len(line)
	for i := 0; i < n; {
		c := line[i]
		switch {
		case c == ' ':
			j := i
			for j < n && line[j] == ' ' {
				j++
			}
			out = append(out, seg{line[i:j], ui.TextPrimary})
			i = j
		case c == '"':
			j := i + 1
			for j < n {
				if line[j] == '\\' {
					j += 2
					continue
				}
				if line[j] == '"' {
					j++
					break
				}
				j++
			}
			j = min(j, n)
			tok := ui.SyntaxString
			k := j
			for k < n && line[k] == ' ' {
				k++
			}
			if k < n && line[k] == ':' {
				tok = ui.SyntaxKey
			}
			out = append(out, seg{line[i:j], tok})
			i = j
		case strings.IndexByte("{}[],:", c) >= 0:
			out = append(out, seg{line[i : i+1], ui.TextSecondary})
			i++
		default:
			j := i
			for j < n && strings.IndexByte(" ,}]", line[j]) < 0 {
				j++
			}
			word := line[i:j]
			tok := ui.SyntaxNumber
			switch word {
			case "true", "false":
				tok = ui.SyntaxBool
			case "null":
				tok = ui.SyntaxNull
			}
			out = append(out, seg{word, tok})
			i = j
		}
	}
	return out
}

// wrapSegs breaks runs into lines of at most w cells. Continuation lines are
// indented by hang cells.
func wrapSegs(segs []seg, w, hang int) [][]seg {
	w = max(w, 1)
	hang = min(hang, w/2)
	var lines [][]seg
	var cur []seg
	width := 0
	flush := func() {
		lines = append(lines, cur)
		cur = nil
		width = 0
		if hang > 0 {
			cur = []seg{{strings.Repeat(" ", hang), ui.TextPrimary}}
			width = hang
		}
	}
	for _, s := range segs {
		var b strings.Builder
		for _, r := range s.text {
			rw := ui.Width(string(r))
			if width+rw > w && (width > hang || len(cur) > 1) {
				if b.Len() > 0 {
					cur = append(cur, seg{b.String(), s.tok})
					b.Reset()
				}
				flush()
			}
			b.WriteRune(r)
			width += rw
		}
		if b.Len() > 0 {
			cur = append(cur, seg{b.String(), s.tok})
		}
	}
	if len(cur) > 0 || len(lines) == 0 {
		lines = append(lines, cur)
	}
	return lines
}

// prettyJSON indents data. ok is false when data is not valid JSON.
func prettyJSON(data []byte) (string, bool) {
	var buf bytes.Buffer
	if err := json.Indent(&buf, bytes.TrimSpace(data), "", "  "); err != nil {
		return "", false
	}
	return buf.String(), true
}

// hexDump formats b as offset, hex and ASCII columns, w cells wide at most.
func hexDump(b []byte, w int) []string {
	per := 16
	if w < 8+2+per*3+2+per+2 {
		per = 8
	}
	if len(b) > maxDump {
		b = b[:maxDump]
	}
	var out []string
	for off := 0; off < len(b); off += per {
		end := min(off+per, len(b))
		var hx, as strings.Builder
		for i := off; i < off+per; i++ {
			if i < end {
				fmt.Fprintf(&hx, "%02x ", b[i])
				c := b[i]
				if c < 0x20 || c > 0x7e {
					c = '.'
				}
				as.WriteByte(c)
			} else {
				hx.WriteString("   ")
			}
		}
		out = append(out, fmt.Sprintf("%08x  %s |%s|", off, hx.String(), as.String()))
	}
	return out
}

// detailKind says how a payload was presented.
type detailKind int

const (
	detailText detailKind = iota
	detailJSON
	detailBinary
	detailNone
)

func (k detailKind) label() string {
	switch k {
	case detailJSON:
		return "JSON"
	case detailBinary:
		return "Binary"
	case detailText:
		return "Text"
	}
	return ""
}

// detailLines builds the body of the detail view: pretty highlighted JSON
// when the payload is JSON, a hex dump for binary frames, wrapped text
// otherwise. Every line fits w cells.
func detailLines(th ui.Theme, e *entry, w int) ([]string, detailKind) {
	var lines []string
	kind := detailNone
	plain := func(text string, tok ui.Token) {
		for _, ln := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
			for _, wl := range wrapSegs([]seg{{ln, tok}}, w, 0) {
				lines = append(lines, renderSegs(th, wl))
			}
		}
	}
	if e.text != "" {
		plain(e.text, ui.TextPrimary)
		if len(e.data) > 0 {
			lines = append(lines, "", th.Render(ui.TextSecondary, "Response body"))
		}
	}
	if len(e.data) > 0 {
		switch {
		case e.binary && !e.b64:
			kind = detailBinary
			for _, l := range hexDump(e.data, w) {
				lines = append(lines, th.Render(ui.TextPrimary, th.Truncate(l, w)))
			}
			if len(e.data) > maxDump {
				lines = append(lines, th.Render(ui.TextQuiet, th.Glyphs.Ellipsis+" "+ui.FormatSize(int64(e.size))+" in all, the first "+ui.FormatSize(maxDump)+" shown"))
			}
		default:
			kind = detailText
			text := strings.ToValidUTF8(string(e.data), "\uFFFD")
			pretty, ok := "", false
			if len(e.data) <= maxPretty && !e.truncated && !e.b64 && e.kind == ekMessage {
				pretty, ok = prettyJSON(e.data)
			}
			if ok {
				kind = detailJSON
				for _, ln := range strings.Split(pretty, "\n") {
					hang := len(ln) - len(strings.TrimLeft(ln, " ")) + 2
					for _, wl := range wrapSegs(lexJSONLine(ln), w, hang) {
						lines = append(lines, renderSegs(th, wl))
					}
				}
			} else {
				plain(text, ui.TextPrimary)
			}
			if e.truncated {
				lines = append(lines, th.Render(ui.TextQuiet, th.Glyphs.Ellipsis+" cut at "+ui.FormatSize(maxEntryBytes)+" of "+ui.FormatSize(int64(e.size))))
			}
		}
	} else if e.kind == ekMessage {
		lines = append(lines, th.Render(ui.TextQuiet, "(empty message)"))
	}
	if !utf8.ValidString(string(e.data)) && kind == detailText {
		lines = append(lines, th.Render(ui.TextQuiet, "Some bytes are not valid text and were replaced."))
	}
	return lines, kind
}
