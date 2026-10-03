package wspane

import (
	"strings"

	"github.com/shubham/postctl/internal/model"
	"github.com/shubham/postctl/internal/ui"
)

// gapWidth is the separator between the two columns of the wide layout.
const gapWidth = 3

// wideAt is the pane width from which the composer and saved list sit beside
// the log instead of above it.
const wideAt = 100

// msgGeo is how the Messages tab divides its area. Heights include each
// section's title row.
type msgGeo struct {
	wide                bool
	compW, savedW, logW int
	compH, savedH, logH int
}

// msgLayout divides a w x h area between composer, saved list and log.
func msgLayout(w, h, nSaved int) msgGeo {
	if w >= wideAt {
		lw := min(max(w*42/100, 38), 64)
		ch := min(max(1+(h-1)*55/100, 5), max(2, h-3))
		return msgGeo{
			wide:  true,
			compW: lw, savedW: lw, logW: w - lw - gapWidth,
			compH: ch, savedH: max(1, h-ch), logH: h,
		}
	}
	rows := min(nSaved, 3)
	if nSaved == 0 {
		rows = 1
	}
	ta := min(max(h/4, 3), 8)
	c, s := 1+ta, 1+rows
	l := h - c - s
	for _, step := range []func(){
		func() { ta = max(2, ta-(6-l)) },
		func() { rows = min(rows, 1) },
		func() { rows = 0 },
		func() { ta = 1 },
	} {
		if l >= 6 {
			break
		}
		step()
		c, s = 1+ta, 1+rows
		l = h - c - s
	}
	l = max(1, l)
	return msgGeo{compW: w, savedW: w, logW: w, compH: c, savedH: s, logH: l}
}

// messagesView draws the Messages tab.
func (p *Pane) messagesView(w, h int) string {
	th := p.th
	g := msgLayout(w, h, len(p.messages()))
	if g.wide {
		left := th.Block(p.composerView(g.compW, g.compH), g.compW, g.compH) + "\n" +
			th.Block(p.savedView(g.savedW, g.savedH), g.savedW, g.savedH)
		right := th.Block(p.logView(g.logW, g.logH), g.logW, g.logH)
		return ui.Columns([]string{left, right}, th.Gap())
	}
	parts := []string{th.Block(p.composerView(g.compW, g.compH), g.compW, g.compH)}
	if g.savedH > 0 {
		parts = append(parts, th.Block(p.savedView(g.savedW, g.savedH), g.savedW, g.savedH))
	}
	parts = append(parts, th.Block(p.logView(g.logW, g.logH), g.logW, g.logH))
	return strings.Join(parts, "\n")
}

// hasCursor reports whether section s is where the cursor is.
func (p *Pane) hasCursor(s section) bool {
	return p.focused && p.zone == zBody && p.tab == tabMessages && p.sect == s
}

// sectionHead is a section title with quiet detail, and text aligned to the
// right edge when it fits.
func (p *Pane) sectionHead(title, detail string, focused bool, right string, w int) string {
	th := p.th
	left := th.Section(title, detail, focused)
	rw := ui.Width(right)
	if rw == 0 {
		return th.Truncate(left, w)
	}
	left = th.Truncate(left, max(1, w-rw-1))
	return left + strings.Repeat(" ", max(1, w-ui.Width(left)-rw)) + right
}

// ---- composer ----------------------------------------------------------------------

func (p *Pane) composerView(w, h int) string {
	th := p.th
	label := formatName(p.format())
	if w < 50 && p.format() == model.WSBinaryBase64 {
		label = "Binary"
	}
	left := th.Section("Message", "", p.hasCursor(sComposer))
	room := w - ui.Width(left) - 2
	right := th.Render(ui.TextSecondary, label)
	switch {
	case p.format() == model.WSText:
		right += "  " + th.Render(ui.TextQuiet, ui.FormatSize(int64(len(p.comp.Text()))))
	case p.compErr != "":
		msg := th.Truncate(th.Glyphs.Warn+" "+p.compErr, max(8, room-ui.Width(label)-2))
		right += "  " + th.Render(ui.StatusWarning, msg)
	default:
		right += "  " + th.Render(ui.StatusSuccess, th.Glyphs.Check+" valid")
	}
	head := p.sectionHead("Message", "", p.hasCursor(sComposer), right, w)
	return head + "\n" + p.comp.Render(w, max(1, h-1))
}

// ---- saved messages -------------------------------------------------------------------

func (p *Pane) savedView(w, h int) string {
	th := p.th
	ms := p.messages()
	detail := ""
	if len(ms) > 0 {
		detail = itoa(len(ms))
	}
	head := p.sectionHead("Saved", detail, p.hasCursor(sSaved), "", w)
	if h <= 1 {
		return head
	}
	if len(ms) == 0 {
		return head + "\n  " + th.Render(ui.TextQuiet, th.Truncate("None yet. Ctrl-X saves the message above as a template", max(1, w-2)))
	}
	rows := h - 1
	cur := min(max(p.savedCur, 0), len(ms)-1)
	top := scrollTo(p.savedTop, cur, rows, len(ms))
	end := min(len(ms), top+rows)
	lines := []string{head}
	for i := top; i < end; i++ {
		lines = append(lines, p.savedRow(ms[i], w, i == cur))
	}
	return strings.Join(lines, "\n")
}

func (p *Pane) savedRow(m model.WSMessage, w int, cur bool) string {
	th := p.th
	focused := p.hasCursor(sSaved)
	bg := cur && focused
	var sb strings.Builder
	used := 0
	add := func(tok ui.Token, s string) {
		if s == "" {
			return
		}
		sb.WriteString(th.StyleOn(tok, bg).Render(s))
		used += ui.Width(s)
	}
	switch {
	case cur && focused:
		add(ui.AccentPrimary, th.Glyphs.Select+" ")
	case cur:
		add(ui.TextSecondary, th.Glyphs.Select+" ")
	default:
		add(ui.TextPrimary, "  ")
	}
	tag := th.Fit(formatTag(m.Format), 4)
	nameW := renameWidth(w)
	name := m.Name
	if name == "" {
		name = "Untitled"
	}
	if p.rename != nil && p.renameID == m.ID {
		sb.WriteString(p.rename.Render(nameW, 1))
		used += nameW
	} else {
		tok := ui.TextPrimary
		st := th.StyleOn(tok, bg).Bold(cur)
		txt := th.Fit(name, nameW)
		sb.WriteString(st.Render(txt))
		used += nameW
	}
	add(ui.TextPrimary, "  ")
	room := max(1, w-used-ui.Width(tag)-1)
	add(ui.TextSecondary, th.Fit(payloadText([]byte(m.Body), 256), room))
	add(ui.TextPrimary, " ")
	add(ui.TextQuiet, tag)
	return sb.String()
}

// ---- log ----------------------------------------------------------------------------------

func (p *Pane) logView(w, h int) string {
	th := p.th
	l := p.peek()
	focused := p.hasCursor(sLog)
	detail := ""
	if l != nil && len(l.log.entries) > 0 {
		lg := l.log
		if lg.filtered() {
			detail = itoa(lg.count()) + " of " + itoa(len(lg.entries))
		} else {
			detail = itoa(len(lg.entries))
		}
		if lg.dir != filterAll {
			detail += " " + th.Glyphs.Dot + " " + lg.dir.label()
		}
		if !lg.follow && l.state == stOpen && p.detail == nil {
			detail += " " + th.Glyphs.Dot + " paused, End resumes"
		}
	}
	head := p.sectionHead("Log", detail, focused, "", w)
	if h <= 1 {
		return head
	}
	if p.detail != nil && l != nil {
		return head + "\n" + p.detailView(l, w, h-1)
	}
	lines := []string{head}
	rows := h - 1
	if p.searching || (l != nil && l.log.query != "") {
		lines = append(lines, p.searchLine(l, w))
		rows--
	}
	if l == nil || len(l.log.entries) == 0 {
		return strings.Join(lines, "\n") + "\n" + p.emptyLog(w, max(1, rows))
	}
	lg := l.log
	if lg.count() == 0 {
		body := th.EmptyState("Nothing matches", "The filter or search hides all "+plural(len(lg.entries), "entry", "entries")+".",
			"f changes the filter. Esc clears the search.", max(1, w-2))
		return strings.Join(lines, "\n") + "\n" + ui.Indent(body, 2)
	}
	top := lg.window(rows)
	end := min(lg.count(), top+rows)
	for i := top; i < end; i++ {
		lines = append(lines, renderRow(th, lg.at(i), w, i == lg.sel, focused))
	}
	return strings.Join(lines, "\n")
}

func (p *Pane) searchLine(l *live, w int) string {
	th := p.th
	if p.searching {
		return th.Render(ui.AccentPrimary, "/") + " " + p.search.Render(max(4, w-2), 1)
	}
	q := ""
	if l != nil {
		q = l.log.query
	}
	return th.Render(ui.TextSecondary, th.Truncate("/ "+q, max(4, w-14))) + "  " + th.Render(ui.TextQuiet, "Esc clears")
}

func (p *Pane) emptyLog(w, h int) string {
	th := p.th
	body := "Connect with Ctrl-S, then send a message. Everything sent and received is listed here."
	l := p.peek()
	if l != nil && l.state == stOpen {
		body = "Connected. Send a message with Ctrl-S, or wait for the server to speak."
	}
	return ui.Indent(th.EmptyState("No messages yet", body, "", max(1, w-2)), 2)
}

// detailView draws the selected entry: a summary line, a rule and the body
// (pretty JSON, wrapped text or a hex dump), scrolled.
func (p *Pane) detailView(l *live, w, h int) string {
	th := p.th
	d := p.detail
	e := l.log.current()
	if d == nil || e == nil {
		return ""
	}
	meta := []string{dirGlyph(th, e.dir) + " " + dirWord(e.dir)}
	if e.kind != ekMessage {
		meta = append(meta, e.kindLabel())
	} else if lbl := d.kind.label(); lbl != "" {
		meta = append(meta, lbl)
	}
	if e.kind == ekMessage || e.size > 0 {
		meta = append(meta, ui.FormatSize(int64(e.size)))
	}
	meta = append(meta, timeLabel(e.at, true))
	if e.seq > 0 {
		meta = append(meta, "#"+itoa(int(e.seq)))
	}
	line := th.Render(e.token(), th.Truncate(strings.Join(meta, " "+th.Glyphs.Dot+" "), w))
	rows := max(1, h-2)
	end := min(len(d.lines), d.scroll+rows)
	if len(d.lines) > rows {
		pos := th.Render(ui.TextQuiet, itoa(d.scroll+1)+"-"+itoa(end)+" of "+itoa(len(d.lines)))
		line = th.Truncate(line, max(1, w-ui.Width(pos)-2)) + "  " + pos
	}
	out := []string{line, th.Rule(w)}
	out = append(out, d.lines[min(d.scroll, len(d.lines)):end]...)
	return strings.Join(out, "\n")
}
