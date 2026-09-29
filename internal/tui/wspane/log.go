package wspane

import (
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/shubham/postctl/internal/ui"
	"github.com/shubham/postctl/internal/wsx"
)

// Log limits: the log is bounded so a chatty server cannot exhaust memory.
const (
	maxEntries    = 5000
	maxLogBytes   = 64 << 20
	maxEntryBytes = 1 << 20
)

// entryKind classifies a log entry.
type entryKind int

const (
	ekMessage entryKind = iota
	ekPing
	ekPong
	ekOpen
	ekClose
	ekError
	ekNote
)

// entry is one line of the live log.
type entry struct {
	// id is unique within the log; seq is the connection's event number.
	id     uint64
	seq    uint64
	at     time.Time
	kind   entryKind
	dir    wsx.Direction
	binary bool
	// b64 marks data as the base64 text of a sent binary message.
	b64 bool
	// data is the payload (cut at maxEntryBytes; truncated says so).
	data      []byte
	truncated bool
	// size is the payload size on the wire.
	size int
	// text is the description of a system entry.
	text    string
	code    int
	latency time.Duration
}

func (e *entry) bytes() int { return len(e.data) + len(e.text) + 96 }

// dirFilter narrows the log by direction.
type dirFilter int

const (
	filterAll dirFilter = iota
	filterIn
	filterOut
)

func (f dirFilter) label() string {
	switch f {
	case filterIn:
		return "Received"
	case filterOut:
		return "Sent"
	}
	return "All"
}

// msgLog is one connection's log with its filter and cursor. Entries are
// appended in order; the visible list is indexes into entries.
type msgLog struct {
	entries []entry
	size    int
	trimmed int
	next    uint64

	dir   dirFilter
	query string
	idx   []int // visible entries when filtered; unused otherwise

	// sel indexes the visible list. follow keeps it on the newest entry.
	sel    int
	follow bool
	top    int
}

func newLog() *msgLog { return &msgLog{follow: true} }

func (l *msgLog) filtered() bool { return l.dir != filterAll || l.query != "" }

// count is the number of visible entries.
func (l *msgLog) count() int {
	if l.filtered() {
		return len(l.idx)
	}
	return len(l.entries)
}

// at returns the i-th visible entry.
func (l *msgLog) at(i int) *entry {
	if l.filtered() {
		return &l.entries[l.idx[i]]
	}
	return &l.entries[i]
}

// current returns the selected entry, or nil.
func (l *msgLog) current() *entry {
	if l.count() == 0 {
		return nil
	}
	l.clamp()
	return l.at(l.sel)
}

func (l *msgLog) clamp() {
	n := l.count()
	if n == 0 {
		l.sel, l.top = 0, 0
		return
	}
	if l.follow {
		l.sel = n - 1
	}
	l.sel = min(max(l.sel, 0), n-1)
}

func (e *entry) matches(dir dirFilter, q string) bool {
	if e.dir != wsx.DirSystem {
		if dir == filterIn && e.dir != wsx.DirIn {
			return false
		}
		if dir == filterOut && e.dir != wsx.DirOut {
			return false
		}
	}
	if q == "" {
		return true
	}
	if strings.Contains(strings.ToLower(e.text), q) {
		return true
	}
	data := e.data
	if len(data) > 64<<10 {
		data = data[:64<<10]
	}
	if e.binary && !e.b64 {
		return false
	}
	return strings.Contains(strings.ToLower(string(data)), q)
}

// refilter rebuilds the visible list, keeping the selected entry when it is
// still visible.
func (l *msgLog) refilter() {
	var keep uint64
	if c := l.current(); c != nil {
		keep = c.id
	}
	l.idx = l.idx[:0]
	if l.filtered() {
		q := strings.ToLower(l.query)
		for i := range l.entries {
			if l.entries[i].matches(l.dir, q) {
				l.idx = append(l.idx, i)
			}
		}
	}
	l.follow = true
	if keep != 0 {
		for i := 0; i < l.count(); i++ {
			if l.at(i).id == keep {
				l.sel = i
				l.follow = i == l.count()-1
				break
			}
		}
	}
	l.clamp()
}

// add appends an entry, dropping the oldest ones beyond the limits.
func (l *msgLog) add(e entry) {
	l.next++
	e.id = l.next
	if len(e.data) > maxEntryBytes {
		e.data = e.data[:maxEntryBytes]
		e.truncated = true
	}
	l.entries = append(l.entries, e)
	l.size += e.bytes()
	if l.filtered() && e.matches(l.dir, strings.ToLower(l.query)) {
		l.idx = append(l.idx, len(l.entries)-1)
	}
	over := 0
	for len(l.entries)-over > maxEntries || (l.size > maxLogBytes && len(l.entries)-over > 1) {
		l.size -= l.entries[over].bytes()
		over++
	}
	if over > 0 {
		l.drop(over)
	}
	l.clamp()
}

// drop removes the oldest n entries and keeps the cursor on its entry.
func (l *msgLog) drop(n int) {
	l.entries = append([]entry(nil), l.entries[n:]...)
	l.trimmed += n
	if !l.filtered() {
		l.sel = max(0, l.sel-n)
		l.top = max(0, l.top-n)
		return
	}
	var idx []int
	removed := 0
	for i, j := range l.idx {
		if j < n {
			if i < l.sel {
				removed++
			}
			continue
		}
		idx = append(idx, j-n)
	}
	l.idx = idx
	l.sel = max(0, l.sel-removed)
	l.top = max(0, l.top-removed)
}

// clear empties the log.
func (l *msgLog) clear() {
	l.entries, l.idx = nil, nil
	l.size, l.sel, l.top = 0, 0, 0
	l.follow = true
}

// move moves the cursor by delta visible entries.
func (l *msgLog) move(delta int) {
	n := l.count()
	if n == 0 {
		return
	}
	l.clamp()
	l.sel = min(max(l.sel+delta, 0), n-1)
	l.follow = l.sel == n-1
}

func (l *msgLog) toEnd() {
	l.follow = true
	l.clamp()
}

func (l *msgLog) toStart() {
	if l.count() == 0 {
		return
	}
	l.follow = false
	l.sel = 0
}

// window returns the first visible row for a viewport of rows lines, keeping
// the cursor inside it.
func (l *msgLog) window(rows int) int {
	n := l.count()
	if rows < 1 || n == 0 {
		return 0
	}
	l.clamp()
	if l.sel < l.top {
		l.top = l.sel
	}
	if l.sel >= l.top+rows {
		l.top = l.sel - rows + 1
	}
	l.top = max(0, min(l.top, max(0, n-rows)))
	return l.top
}

// ---- events to entries ----------------------------------------------------

// entryFromEvent converts a connection event into a log entry.
func entryFromEvent(ev wsx.Event) entry {
	e := entry{seq: ev.Seq, at: ev.Time, dir: ev.Dir, size: ev.Size, latency: ev.Latency}
	switch ev.Type {
	case wsx.EventOpen:
		e.kind = ekOpen
		e.text = "Connected"
		if ev.Info != nil {
			if ev.Info.Status > 0 {
				e.text += " (" + strconv.Itoa(ev.Info.Status) + ")"
			}
			if ev.Info.Subprotocol != "" {
				e.text += ", subprotocol " + ev.Info.Subprotocol
			}
		}
	case wsx.EventMessage:
		e.kind = ekMessage
		e.binary = ev.Msg == wsx.Binary
		e.b64 = e.binary && ev.Dir == wsx.DirOut
		e.data = ev.Data
	case wsx.EventPing:
		e.kind = ekPing
		e.data = ev.Data
	case wsx.EventPong:
		e.kind = ekPong
		e.data = ev.Data
	case wsx.EventError:
		e.kind = ekError
		e.text = "Error"
		if ev.Err != nil {
			e.text = ev.Err.Error()
		}
	case wsx.EventClose:
		e.kind = ekClose
		e.code = ev.Code
		e.text = closeText(ev.Code, ev.Reason, ev.Err)
	}
	return e
}

// closeText describes a close in plain words.
func closeText(code int, reason string, err error) string {
	name := ""
	switch code {
	case 1000:
		name = "normal closure"
	case 1001:
		name = "server going away"
	case 1002:
		name = "protocol error"
	case 1003:
		name = "unsupported data"
	case 1005:
		name = "closed without a status"
	case 1006:
		name = "connection lost"
	case 1007:
		name = "invalid data"
	case 1008:
		name = "policy violation"
	case 1009:
		name = "message too big"
	case 1011:
		name = "server error"
	case 1012:
		name = "service restart"
	case 1013:
		name = "try again later"
	}
	text := "Closed"
	if code != 0 {
		text += " " + strconv.Itoa(code)
	}
	if name != "" {
		text += " " + name
	}
	switch {
	case reason != "":
		text += ": " + reason
	case err != nil && code != 1000:
		text += ": " + err.Error()
	}
	return text
}

// ---- rows -------------------------------------------------------------------

// payloadText is the single-line form of a payload for the list: whitespace
// runs collapse to one space and control characters vanish.
func payloadText(b []byte, limit int) string {
	if len(b) > limit {
		b = b[:limit]
	}
	s := strings.ToValidUTF8(string(b), "\uFFFD")
	var sb strings.Builder
	space := false
	for _, r := range s {
		switch {
		case r == '\n' || r == '\r' || r == '\t' || r == ' ':
			if !space && sb.Len() > 0 {
				sb.WriteByte(' ')
			}
			space = true
			continue
		case r < 0x20 || r == 0x7f:
			continue
		}
		space = false
		sb.WriteRune(r)
	}
	return strings.TrimRight(sb.String(), " ")
}

// hexPreview is the first bytes of a binary payload as hex.
func hexPreview(b []byte, n int) string {
	if len(b) > n {
		b = b[:n]
	}
	parts := make([]string, len(b))
	for i, c := range b {
		parts[i] = fmt.Sprintf("%02x", c)
	}
	return strings.Join(parts, " ")
}

// summary is the one-line description of an entry.
func (e *entry) summary() string {
	switch e.kind {
	case ekMessage:
		switch {
		case e.b64:
			return payloadText(e.data, 256)
		case e.binary:
			return hexPreview(e.data, 24)
		}
		if len(e.data) == 0 {
			return "(empty)"
		}
		return payloadText(e.data, 512)
	case ekPing, ekPong:
		var parts []string
		if e.kind == ekPong && e.latency > 0 {
			parts = append(parts, ui.FormatDuration(e.latency))
		}
		if len(e.data) > 0 {
			if utf8.Valid(e.data) {
				parts = append(parts, payloadText(e.data, 64))
			} else {
				parts = append(parts, hexPreview(e.data, 8))
			}
		}
		return strings.Join(parts, "  ")
	}
	return e.text
}

// kindLabel is the short word in the type column.
func (e *entry) kindLabel() string {
	switch e.kind {
	case ekMessage:
		if e.binary {
			return "bin"
		}
		return "text"
	case ekPing:
		return "ping"
	case ekPong:
		return "pong"
	case ekOpen:
		return "open"
	case ekClose:
		return "close"
	case ekError:
		return "error"
	}
	return "info"
}

// dirGlyph is the arrow for the entry's direction.
func dirGlyph(th ui.Theme, d wsx.Direction) string {
	switch d {
	case wsx.DirIn:
		if th.ASCII {
			return "<"
		}
		return "↓"
	case wsx.DirOut:
		if th.ASCII {
			return ">"
		}
		return "↑"
	}
	if th.ASCII {
		return "*"
	}
	return "•"
}

// dirWord names the direction for the detail header.
func dirWord(d wsx.Direction) string {
	switch d {
	case wsx.DirIn:
		return "Received"
	case wsx.DirOut:
		return "Sent"
	}
	return "Event"
}

// rowStyle picks the token of an entry's marker and kind label.
func (e *entry) token() ui.Token {
	switch e.kind {
	case ekError:
		return ui.StatusDanger
	case ekClose:
		if e.code == 1000 || e.code == 1001 || e.code == 1005 {
			return ui.TextSecondary
		}
		return ui.StatusWarning
	case ekOpen:
		return ui.StatusSuccess
	case ekPing, ekPong, ekNote:
		return ui.TextQuiet
	}
	if e.dir == wsx.DirOut {
		return ui.AccentPrimary
	}
	return ui.StatusInfo
}

func timeLabel(t time.Time, ms bool) string {
	if ms {
		return t.Format("15:04:05.000")
	}
	return t.Format("15:04:05")
}

// renderRow draws one log row in exactly w cells. sel marks the cursor row;
// focused adds the selection background.
func renderRow(th ui.Theme, e *entry, w int, sel, focused bool) string {
	bg := sel && focused
	on := func(tok ui.Token) func(string) string {
		st := th.StyleOn(tok, bg)
		return func(s string) string { return st.Render(s) }
	}
	var sb strings.Builder
	used := 0
	add := func(tok ui.Token, s string) {
		if s == "" {
			return
		}
		sb.WriteString(on(tok)(s))
		used += ui.Width(s)
	}
	switch {
	case sel && focused:
		add(ui.AccentPrimary, th.Glyphs.Select+" ")
	case sel:
		add(ui.TextSecondary, th.Glyphs.Select+" ")
	default:
		add(ui.TextPrimary, "  ")
	}
	ms := w >= 64
	add(ui.TextQuiet, timeLabel(e.at, ms)+" ")
	add(e.token(), dirGlyph(th, e.dir)+" ")
	kw := 0
	if w >= 50 {
		kw = 6
		add(e.token(), th.Fit(e.kindLabel(), kw-1)+" ")
	}
	size := ""
	if e.kind == ekMessage || ((e.kind == ekPing || e.kind == ekPong) && e.size > 0) {
		size = ui.FormatSize(int64(e.size))
		if e.truncated {
			size += "+"
		}
	}
	sw := 0
	if size != "" {
		sw = ui.Width(size) + 1
	}
	room := max(1, w-used-sw)
	text := th.Truncate(e.summary(), room)
	tok := ui.TextPrimary
	if e.kind != ekMessage {
		tok = ui.TextSecondary
	}
	if e.kind == ekError {
		tok = ui.StatusDanger
	}
	add(tok, text)
	if size != "" {
		pad := max(1, w-used-ui.Width(size))
		add(ui.TextPrimary, strings.Repeat(" ", pad))
		add(ui.TextQuiet, size)
	}
	if pad := w - used; pad > 0 && bg {
		add(ui.TextPrimary, strings.Repeat(" ", pad))
	}
	return sb.String()
}

// copyText is what "copy message" puts on the clipboard: the text of a text
// message, or the base64 text of a binary one.
func (e *entry) copyText() string {
	switch {
	case e.kind == ekMessage && e.binary && !e.b64:
		return base64Std(e.data)
	case e.kind == ekMessage || len(e.data) > 0:
		return string(e.data)
	}
	return e.text
}

func base64Std(b []byte) string { return base64.StdEncoding.EncodeToString(b) }
