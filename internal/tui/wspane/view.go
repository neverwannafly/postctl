package wspane

import (
	"path/filepath"
	"strings"

	"github.com/shubham/postctl/internal/model"
	"github.com/shubham/postctl/internal/tui/vimfield"
	"github.com/shubham/postctl/internal/ui"
)

const (
	badgeW = 6
	urlX   = 2 + badgeW + 2
	// actionW fits "Disconnect" and the marker before it.
	actionW = 11
	// chrome is the rows above the tab content: bar, status row, tab labels
	// and the tab rule.
	chrome = 4
)

// bodySize is the size of the tab content area.
func (p *Pane) bodySize() (w, h int) {
	return max(1, p.w), max(1, p.h-chrome)
}

// View implements tui.Pane.
func (p *Pane) View() string {
	if p.w <= 0 || p.h <= 0 {
		return ""
	}
	if s := p.emptyView(); s != "" {
		return p.th.Block(s, p.w, p.h)
	}
	w, h := p.bodySize()
	var b strings.Builder
	b.WriteString(p.barView())
	b.WriteByte('\n')
	b.WriteString(p.statusRow())
	b.WriteByte('\n')
	b.WriteString(p.tabsView())
	b.WriteByte('\n')
	var body string
	switch p.tab {
	case tabMessages:
		body = p.messagesView(w, h)
	case tabParams:
		body = p.params.view(p, w, h)
	case tabHeaders:
		body = p.headers.view(p, w, h)
	case tabAuth:
		body = p.auth.view(p, w, h)
	default:
		body = p.settings.view(p, w, h)
	}
	if p.confirm != nil {
		body = p.th.Block(p.th.ConfirmView(p.confirm.c, min(w, 64)), w, h)
	}
	b.WriteString(p.th.Block(body, w, h))
	out := p.th.Block(b.String(), p.w, p.h)
	if pop := p.url.Popup(p.urlWidth()); p.urlE && len(pop) > 0 {
		// The URL field's completion menu draws over the rows under the bar.
		out = strings.Join(vimfield.Overlay(strings.Split(out, "\n"), 0, urlX, pop), "\n")
	}
	return out
}

// emptyView explains why there is nothing to edit, or returns "".
func (p *Pane) emptyView() string {
	th := p.th
	switch {
	case p.ref.Problem != nil:
		name := filepath.Base(p.ref.Problem.Path)
		return th.ErrorState("Could not load "+name,
			"The file on disk is unchanged.",
			"Fix it in your editor; postctl reloads when it changes.", p.w)
	case p.req == nil:
		return th.EmptyState("No WebSocket request selected",
			"Choose a WebSocket request in Collections, or press n there to create one.", "", p.w)
	case p.req.Kind != model.KindWebSocket:
		return th.EmptyState("Not a WebSocket request", "This pane edits WebSocket requests only.", "", p.w)
	}
	return ""
}

// ---- bar ------------------------------------------------------------------------

// barView is the WS badge, URL input and Connect button.
func (p *Pane) barView() string {
	th := p.th
	marker := "  "
	if p.zone == zBar && p.focused {
		marker = th.Render(ui.AccentPrimary, th.Glyphs.Select) + " "
	}
	badge := th.MethodBadge("WS", badgeW)
	right := p.actionLabel()
	field := p.url.Render(p.urlWidth(), 1)
	return marker + badge + "  " + field + " " + right
}

// actionLabel is the right end of the bar.
func (p *Pane) actionLabel() string {
	th := p.th
	state := stIdle
	if l := p.peek(); l != nil {
		state = l.state
	}
	var text string
	var st = th.Style(ui.AccentPrimary)
	switch state {
	case stConnecting:
		text, st = "Connecting", th.Style(ui.StatusInfo)
	case stOpen:
		text, st = "Disconnect", th.Bold()
	case stClosing:
		text, st = "Closing", th.Style(ui.StatusInfo)
	default:
		text = "Connect"
	}
	return th.Fit(st.Render(text), actionW-1)
}

// statusRow is the line under the bar: connection progress, a quiet notice,
// the connection summary or the resolved URL preview, and the autosave state.
func (p *Pane) statusRow() string {
	th := p.th
	w := p.w
	var right string
	if label, failed := p.saveLabel(); label != "" {
		tok := ui.TextQuiet
		if failed {
			tok = ui.StatusDanger
		}
		right = th.Render(tok, label)
	}
	rw := ui.Width(right)
	room := w - 2
	if rw > 0 {
		room -= rw + 2
	}
	left := p.statusLeft(max(4, room))
	lw := ui.Width(left)
	if rw == 0 {
		return "  " + left
	}
	return "  " + left + strings.Repeat(" ", max(1, w-2-lw-rw)) + right
}

func (p *Pane) statusLeft(room int) string {
	th := p.th
	l := p.peek()
	switch {
	case l != nil && l.state == stConnecting:
		frames := th.NewSpinner().Spinner.Frames
		frame := ""
		if !th.ASCII && len(frames) > 0 {
			frame = th.Render(ui.StatusInfo, frames[l.frame%len(frames)])
		}
		text := "Connecting " + ui.FormatDuration(p.deps.now().Sub(l.started)) + ". Esc cancels"
		return th.Busy(frame, th.Truncate(text, max(4, room-2)))
	case l != nil && l.state == stClosing:
		return th.Busy("", "Closing the connection")
	case l != nil && l.state == stOpen:
		return p.connectedLine(l, room)
	case l != nil && l.failed:
		text := l.failure
		return th.Render(ui.StatusDanger, th.Glyphs.Fail) + " " + th.Render(ui.TextPrimary, th.Truncate(text+". Details are in the log", max(4, room-2)))
	case l != nil && l.closed:
		return th.Render(ui.TextQuiet, th.Glyphs.Expired) + " " + th.Render(ui.TextSecondary, th.Truncate("Disconnected. Ctrl-S connects again", max(4, room-2)))
	case p.urlE:
		return p.previewLine(room)
	}
	if line := p.unresolvedLine(room); line != "" {
		return line
	}
	return th.Render(ui.TextQuiet, th.Glyphs.Expired) + " " + th.Render(ui.TextSecondary, th.Truncate("Not connected", max(4, room-2)))
}

// connectedLine summarises an open connection.
func (p *Pane) connectedLine(l *live, room int) string {
	th := p.th
	up, down := "↑", "↓"
	if th.ASCII {
		up, down = ">", "<"
	}
	parts := []string{
		up + " " + itoa(l.sent) + " sent",
		down + " " + itoa(l.recv) + " received",
	}
	if l.rtt > 0 {
		parts = append(parts, "ping "+ui.FormatDuration(l.rtt))
	}
	sep := " " + th.Glyphs.Dot + " "
	head := th.Render(ui.StatusSuccess, th.Glyphs.OK) + " " + th.Bold().Render("Connected")
	tail := th.Render(ui.TextSecondary, strings.Join(parts, sep))
	full := head + sep + tail
	if ui.Width(full) <= room {
		return full
	}
	return th.Truncate(full, room)
}

// previewLine is the resolved URL with unresolved variables flagged.
func (p *Pane) previewLine(room int) string {
	th := p.th
	raw := p.req.URL.Raw
	if strings.TrimSpace(raw) == "" {
		return th.Render(ui.TextQuiet, th.Truncate("Type a ws:// or wss:// URL", room))
	}
	resolved, missing := p.sc.resolve(raw)
	if len(missing) == 0 && resolved == raw {
		return th.Render(ui.TextQuiet, th.Truncate("No variables to resolve", room))
	}
	prefix := th.Glyphs.Arrow + " "
	tail := ""
	if len(missing) > 0 {
		tail = "  " + th.Render(ui.StatusWarning, th.Glyphs.Warn+" unresolved: "+strings.Join(missing, ", "))
		if ui.Width(tail) > room/2 {
			tail = "  " + th.Render(ui.StatusWarning, th.Glyphs.Warn+" "+itoa(len(missing))+" unresolved")
		}
	}
	avail := max(4, room-ui.Width(prefix)-ui.Width(tail))
	return th.Render(ui.TextQuiet, prefix) + th.Render(ui.TextSecondary, th.Truncate(resolved, avail)) + tail
}

// unresolvedLine warns about variables in the URL that have no value.
func (p *Pane) unresolvedLine(room int) string {
	th := p.th
	_, missing := p.sc.resolve(p.req.URL.Raw)
	if len(missing) == 0 {
		return ""
	}
	text := th.Glyphs.Warn + " unresolved: " + strings.Join(missing, ", ")
	if ui.Width(text) > room {
		text = th.Glyphs.Warn + " " + itoa(len(missing)) + " unresolved"
	}
	return th.Render(ui.StatusWarning, th.Truncate(text, room))
}

// ---- tabs -------------------------------------------------------------------------

// tabList lists the tab bar entries with counts and dots.
func (p *Pane) tabList() []ui.Tab {
	r := p.req
	enabled := func(kvs []model.KV) int {
		n := 0
		for _, k := range kvs {
			if !k.Disabled {
				n++
			}
		}
		return n
	}
	tabs := make([]ui.Tab, numTabs)
	for i := range tabs {
		tabs[i].Label = tabNames[i]
	}
	tabs[tabMessages].Count = len(p.messages())
	tabs[tabParams].Count = enabled(r.URL.Query)
	tabs[tabHeaders].Count = enabled(r.Headers)
	tabs[tabAuth].Dot = !r.Auth.IsInherit()
	ws := r.WebSocket
	tabs[tabSettings].Dot = ws != nil && (len(ws.Subprotocols) > 0 || ws.PingInterval != nil || ws.MaxMessageBytes != nil)
	return tabs
}

func (p *Pane) tabsView() string {
	return p.th.Tabs(p.w, p.tabList(), int(p.tab), p.zone == zTabs && p.focused)
}
