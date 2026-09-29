package request

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/shubham/postctl/internal/model"
	"github.com/shubham/postctl/internal/ui"
)

const (
	methodW = 6
	sendW   = 9 // "  Send ⏎" / "  Sending"
	// chrome is the rows above the tab content: bar, status row, tab labels
	// and the tab rule.
	chrome = 4
)

var methods = []string{"GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS"}

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
	body := p.views[p.tab].view(p, w, h)
	if p.confirm != nil {
		body = p.confirmView(w, h)
	}
	b.WriteString(p.th.Block(body, w, h))
	return p.th.Block(b.String(), p.w, p.h)
}

// emptyView explains why there is nothing to edit, or returns "".
func (p *Pane) emptyView() string {
	th := p.th
	w := p.w
	switch {
	case p.ref.Problem != nil:
		name := filepath.Base(p.ref.Problem.Path)
		return th.ErrorState("Could not load "+name,
			"The file on disk is unchanged.",
			"Fix it in your editor; postctl reloads when it changes.", w)
	case p.req == nil:
		return th.EmptyState("No request selected",
			"Choose a request in Collections to edit it here.", "", w)
	case p.req.Kind != "" && p.req.Kind != model.KindHTTP:
		return th.EmptyState("Not an HTTP request",
			"This is a "+string(p.req.Kind)+" request, which postctl cannot edit yet. It is kept as it is.", "", w)
	}
	return ""
}

// barView is the method selector, URL input and send label.
func (p *Pane) barView() string {
	th := p.th
	marker := "  "
	if p.zone == zBar && p.focused {
		marker = th.Render(ui.AccentPrimary, th.Glyphs.Select) + " "
	}
	method := p.req.Method
	if method == "" {
		method = "GET"
	}
	badge := th.MethodBadge(method, methodW)
	if p.msel != nil {
		badge = th.MethodBadge(p.msel.methods[p.msel.idx], methodW)
	}
	right := p.sendLabel()
	urlW := max(4, p.w-2-methodW-2-ui.Width(right)-1)
	focused := p.urlE && p.focused
	field := p.url.view(th, urlW, focused)
	return marker + badge + "  " + field + " " + right
}

// sendLabel is the right end of the bar.
func (p *Pane) sendLabel() string {
	th := p.th
	if _, ok := p.sending(); ok {
		return th.Fit(th.Render(ui.StatusInfo, "Sending"), sendW-1)
	}
	return th.Fit(th.Render(ui.AccentPrimary, "Send"), sendW-1)
}

// statusRow is the line under the bar. While the URL has focus (or the method
// list is open) it carries the resolved preview; otherwise the send progress,
// a quiet notice, unresolved variables and the autosave state.
func (p *Pane) statusRow() string {
	th := p.th
	w := p.w
	if p.msel != nil {
		return p.methodList(w)
	}
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
	var left string
	switch {
	case p.isSending():
		left = p.sendProgress(room)
	case p.oauth != nil:
		left = th.Busy("", th.Truncate("Waiting for OAuth 2.0 sign-in. Esc cancels.", max(4, room-2)))
	case p.notice != "":
		left = th.Render(ui.TextSecondary, th.Truncate(p.notice, room))
	case p.urlE:
		left = p.previewLine(room)
	default:
		left = p.unresolvedLine(room)
	}
	lw := ui.Width(left)
	gap := max(1, w-2-lw-rw)
	if rw == 0 {
		return "  " + left
	}
	return "  " + left + strings.Repeat(" ", gap) + right
}

// previewLine is the resolved URL with unresolved variables flagged.
func (p *Pane) previewLine(room int) string {
	th := p.th
	raw := p.req.URL.Raw
	if strings.TrimSpace(raw) == "" {
		return th.Render(ui.TextQuiet, th.Truncate("Type a URL, or paste a curl command", room))
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

func (p *Pane) isSending() bool {
	_, ok := p.sending()
	return ok
}

func (p *Pane) sendProgress(room int) string {
	th := p.th
	si, _ := p.sending()
	elapsed := p.deps.now().Sub(si.started)
	step := si.step
	if step == "" {
		step = "Sending request"
	}
	text := step + " " + formatElapsed(elapsed)
	frames := th.NewSpinner().Spinner.Frames
	frame := ""
	if !th.ASCII && len(frames) > 0 {
		frame = th.Render(ui.StatusInfo, frames[p.send.frame%len(frames)])
	}
	return th.Busy(frame, th.Truncate(text, max(4, room-2)))
}

func formatElapsed(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	return fmt.Sprintf("%.1fs", d.Seconds())
}

// methodList is the open method selector, drawn on the status row.
func (p *Pane) methodList(w int) string {
	th := p.th
	var parts []string
	for i, m := range p.msel.methods {
		if i == p.msel.idx {
			parts = append(parts, th.Style(ui.MethodToken(m)).Bold(true).Render("["+m+"]"))
		} else {
			parts = append(parts, th.Render(ui.TextSecondary, m))
		}
	}
	return "  " + th.Truncate(strings.Join(parts, " "), max(1, w-2))
}

// tabs lists the tab bar entries with counts and dots.
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
	full := p.w >= 70
	names := tabShort
	if full {
		names = tabNames
	}
	tabs := make([]ui.Tab, numTabs)
	for i := range tabs {
		tabs[i].Label = names[i]
	}
	tabs[tabParams].Count = enabled(r.URL.Query)
	tabs[tabHeaders].Count = enabled(r.Headers)
	tabs[tabAuth].Dot = !r.Auth.IsInherit()
	tabs[tabBody].Dot = r.Body.Mode != "" && r.Body.Mode != model.BodyNone
	tabs[tabPre].Dot = !r.Scripts.PreRequest.Empty()
	tabs[tabTests].Dot = !r.Scripts.Test.Empty()
	return tabs
}

// tabsView renders the two-line tab bar, scrolling so the active tab always
// shows when the pane is narrow.
func (p *Pane) tabsView() string {
	th := p.th
	tabs := p.tabList()
	focused := p.zone == zTabs && p.focused
	width := p.w
	active := int(p.tab)
	start := 0
	for start < active {
		used := 2 // "‹ "
		for i := start; i <= active; i++ {
			l := tabWidth(th, tabs[i])
			if i > start {
				l += ui.SpaceSM
			}
			used += l
		}
		if used <= width {
			break
		}
		start++
	}
	if start == 0 {
		return th.Tabs(width, tabs, active, focused)
	}
	out := th.Tabs(width-2, tabs[start:], active-start, focused)
	lines := strings.SplitN(out, "\n", 2)
	lead := th.Render(ui.TextQuiet, "‹") + " "
	if th.ASCII {
		lead = th.Render(ui.TextQuiet, "<") + " "
	}
	if len(lines) < 2 {
		return lead + out
	}
	return lead + lines[0] + "\n" + th.Rule(2) + lines[1]
}

func tabWidth(th ui.Theme, tb ui.Tab) int {
	w := ui.Width(tb.Label)
	switch {
	case tb.Count > 0:
		w += 2 + len(itoa(tb.Count))
	case tb.Dot:
		w += 2
	}
	return w
}
