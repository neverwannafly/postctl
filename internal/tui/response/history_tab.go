package response

import (
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/shubham/postctl/internal/history"
	"github.com/shubham/postctl/internal/model"
	"github.com/shubham/postctl/internal/tui"
	"github.com/shubham/postctl/internal/ui"
)

// historyLimit bounds how many past sends the History tab lists.
const historyLimit = 200

// histState is the History tab's data for one request.
type histState struct {
	loaded  bool
	loading bool
	err     bool
	items   []history.Entry
	cur     int
	top     int
	// now is the clock the relative times are drawn against. Update stamps it
	// (View never reads the clock).
	now time.Time
}

// histLoadedMsg carries one History tab load. Loads are numbered and tied to a
// request so a slow load never overwrites a newer one or another request.
type histLoadedMsg struct {
	gen   int
	reqID model.ID
	items []history.Entry
	err   error
}

func (p *Pane) historyEnabled() bool { return p.deps.Log != nil }

func (p *Pane) requestID() model.ID {
	if p.ref.Request == nil {
		return ""
	}
	return p.ref.Request.ID
}

// loadHistory starts a load of the current request's past sends.
func (p *Pane) loadHistory() tea.Cmd {
	id := p.requestID()
	if !p.historyEnabled() || id == "" {
		return nil
	}
	p.histGen++
	p.hist.loading = true
	gen, open := p.histGen, p.deps.Log
	return func() tea.Msg {
		log, err := open()
		if err != nil {
			return histLoadedMsg{gen: gen, reqID: id, err: err}
		}
		items, err := log.List(history.Filter{RequestID: id, Limit: historyLimit})
		return histLoadedMsg{gen: gen, reqID: id, items: items, err: err}
	}
}

// refreshHistory reloads after a send when the tab has data or is showing.
func (p *Pane) refreshHistory() tea.Cmd {
	if !p.historyEnabled() {
		return nil
	}
	if p.hist.loaded || p.tab == tabHistory {
		return p.loadHistory()
	}
	return nil
}

// ensureHistory loads the tab's data on first use for a request.
func (p *Pane) ensureHistory() tea.Cmd {
	if !p.historyEnabled() || p.hist.loading || (p.hist.loaded && !p.hist.err) {
		return nil
	}
	return p.loadHistory()
}

func (p *Pane) onHistoryLoaded(msg histLoadedMsg) {
	if msg.gen != p.histGen || msg.reqID != p.requestID() {
		return // a newer load or another request owns the tab now
	}
	keep := ""
	if e := p.histCurrent(); e != nil {
		keep = e.ID
	}
	p.hist.loading = false
	p.hist.loaded = true
	p.hist.err = msg.err != nil
	if msg.err != nil {
		p.hist.items = nil
		p.hist.cur, p.hist.top = 0, 0
		return
	}
	p.hist.items = msg.items
	p.hist.cur = 0
	for i, e := range msg.items {
		if keep != "" && e.ID == keep {
			p.hist.cur = i
		}
	}
	p.clampHist()
}

func (p *Pane) histCurrent() *history.Entry {
	if p.hist.cur < 0 || p.hist.cur >= len(p.hist.items) {
		return nil
	}
	return &p.hist.items[p.hist.cur]
}

// histRows is how many entries fit under the tab bar.
func (p *Pane) histRows() int { return p.contentHeight() }

func (p *Pane) clampHist() {
	n := len(p.hist.items)
	p.hist.cur = min(max(0, p.hist.cur), max(0, n-1))
	rows := p.histRows()
	if p.hist.cur < p.hist.top {
		p.hist.top = p.hist.cur
	}
	if p.hist.cur >= p.hist.top+rows {
		p.hist.top = p.hist.cur - rows + 1
	}
	p.hist.top = min(max(0, p.hist.top), max(0, n-rows))
}

func (p *Pane) histMove(d int) {
	p.hist.cur += d
	p.clampHist()
}

// historyKey handles the keys of the History tab. ok reports a handled key.
func (p *Pane) historyKey(s string) (cmd tea.Cmd, ok bool) {
	half := max(1, p.histRows()/2)
	switch s {
	case "up", "k":
		p.histMove(-1)
	case "down", "j":
		p.histMove(1)
	case "pgup", "ctrl+u":
		p.histMove(-half)
	case "pgdown", "ctrl+d":
		p.histMove(half)
	case "home":
		p.hist.cur = 0
		p.clampHist()
	case "end", "G":
		p.hist.cur = len(p.hist.items) - 1
		p.clampHist()
	case "enter":
		e := p.histCurrent()
		if e == nil {
			return nil, true
		}
		id, entry := p.requestID(), e.ID
		return func() tea.Msg { return tui.OpenRequestHistoryMsg{RequestID: id, EntryID: entry} }, true
	case "d":
		e := p.histCurrent()
		if e == nil {
			return nil, true
		}
		return p.startDiff(e.ID), true
	default:
		return nil, false
	}
	return nil, true
}

// viewHistory renders the History tab.
func (p *Pane) viewHistory(w, h int) string {
	t := p.th
	switch {
	case p.hist.err:
		return t.ErrorState("Could not read the history of this request.",
			"Your request and response are unchanged.",
			"Switch tabs and come back to retry, or send again with Ctrl-S.", w)
	case !p.hist.loaded:
		return t.EmptyState("Loading history", "Reading the past sends of this request.", "", w)
	case len(p.hist.items) == 0:
		return t.EmptyState("No sends of this request yet.", "", "Ctrl-S sends it.", w)
	}
	now := p.hist.now
	curID := p.currentHistoryID()
	rd := history.NewRedactor(nil)
	if st := p.current(); st != nil {
		rd = resultRedactor(st.result)
	}
	var lines []string
	for i := p.hist.top; i < len(p.hist.items) && len(lines) < h; i++ {
		lines = append(lines, p.historyRow(p.hist.items[i], i == p.hist.cur, curID, now, w, rd))
	}
	return strings.Join(lines, "\n")
}

// historyRow renders one past send: method, status code and reason, duration,
// size and relative time. The selected row also carries a glyph, and the send
// that produced the response on screen is marked in words.
func (p *Pane) historyRow(e history.Entry, selected bool, curID string, now time.Time, w int, rd *history.Redactor) string {
	t := p.th
	mark := "  "
	if selected {
		mark = t.Render(ui.AccentPrimary, t.Glyphs.Select) + " "
	}
	var chip string
	if e.Status == 0 || e.Error != "" {
		chip = t.ResponseChip(0, "", time.Duration(e.DurationMS)*time.Millisecond, e.SizeBytes)
	} else {
		// The reason phrase is server text: mask any known secret in it.
		reason := strings.ReplaceAll(rd.Text(e.StatusText), history.Mask, t.Glyphs.Mask)
		chip = t.ResponseChip(e.Status, reason, time.Duration(e.DurationMS)*time.Millisecond, e.SizeBytes)
	}
	line := mark + t.MethodBadge(e.Request.Method, 5) + " " + chip +
		"  " + t.Render(ui.TextQuiet, relativeTime(now, e.Time))
	if curID != "" && e.ID == curID {
		line += "  " + t.Render(ui.TextSecondary, "this response")
	}
	return t.Truncate(line, w)
}

// currentHistoryID is the history entry of the response on screen, if known.
func (p *Pane) currentHistoryID() string {
	if st := p.current(); st != nil {
		return st.result.HistoryID
	}
	return ""
}

// relativeTime renders t as "just now", "3m ago", "2h ago", "4d ago" or a
// date for anything older than 30 days.
func relativeTime(now, t time.Time) string {
	d := now.Sub(t)
	switch {
	case d < 0, d < 5*time.Second:
		return "just now"
	case d < time.Minute:
		return strconv.Itoa(int(d/time.Second)) + "s ago"
	case d < time.Hour:
		return strconv.Itoa(int(d/time.Minute)) + "m ago"
	case d < 24*time.Hour:
		return strconv.Itoa(int(d/time.Hour)) + "h ago"
	case d < 30*24*time.Hour:
		return strconv.Itoa(int(d/(24*time.Hour))) + "d ago"
	}
	return t.Format("2006-01-02")
}
