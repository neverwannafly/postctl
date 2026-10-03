package response

import (
	"errors"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/shubham/postctl/internal/history"
	"github.com/shubham/postctl/internal/tui"
	"github.com/shubham/postctl/internal/tui/diffview"
	"github.com/shubham/postctl/internal/ui"
)

// diffState is the open response diff.
type diffState struct {
	open     bool
	loading  bool
	body     *diffview.Model
	headers  *diffview.Model
	showBody bool
}

// diffReadyMsg carries one finished diff. Diffs are numbered; a result whose
// number is not the latest is dropped.
type diffReadyMsg struct {
	gen     int
	body    *diffview.Model
	headers *diffview.Model
	// problem is a one-line reason when there is nothing to show; it never
	// contains body data.
	problem string
}

// diffUnavailable explains why a diff cannot start now, or returns "".
func (p *Pane) diffUnavailable() string {
	switch {
	case !p.historyEnabled():
		return "History is not available in this build, so there is nothing to diff against."
	case p.ref.Request == nil:
		return "Select a request first."
	}
	st := p.current()
	if st == nil || st.result.Response == nil {
		return "Nothing to diff: send the request first (Ctrl-S)."
	}
	return ""
}

// cmdDiffPrevious is Space x: diff the response on screen with the previous
// send of the same request.
func (p *Pane) cmdDiffPrevious() tea.Cmd {
	if why := p.diffUnavailable(); why != "" {
		return toast(ui.ToastInfo, why)
	}
	// With the History tab loaded the answer is already known.
	if p.hist.loaded && !p.hist.err {
		if _, ok := previousEntry(p.hist.items, p.currentHistoryID()); !ok {
			return toast(ui.ToastInfo, "No earlier send of this request to compare with.")
		}
	}
	return tea.Batch(p.startDiff(""), focusResponse())
}

func focusResponse() tea.Cmd {
	return func() tea.Msg { return tui.FocusPaneMsg{Slot: tui.SlotResponse} }
}

func toast(kind ui.ToastKind, text string) tea.Cmd {
	return func() tea.Msg { return tui.ToastMsg{Kind: kind, Text: text} }
}

// previousEntry picks the send before the one that produced the response on
// screen. items is newest first. An empty curID means the response was not
// recorded, so the newest entry is the previous one.
func previousEntry(items []history.Entry, curID string) (history.Entry, bool) {
	if curID == "" {
		if len(items) == 0 {
			return history.Entry{}, false
		}
		return items[0], true
	}
	for i, e := range items {
		if e.ID == curID {
			if i+1 < len(items) {
				return items[i+1], true
			}
			return history.Entry{}, false
		}
	}
	return history.Entry{}, false
}

// startDiff opens the diff panel and computes it in a command. entryID names
// the past send to compare with; empty means the previous send.
func (p *Pane) startDiff(entryID string) tea.Cmd {
	if why := p.diffUnavailable(); why != "" {
		return toast(ui.ToastInfo, why)
	}
	st := p.current()
	p.diffGen++
	p.diff = diffState{open: true, loading: true, showBody: true}
	gen, open := p.diffGen, p.deps.Log
	reqID, curID, res := p.requestID(), st.result.HistoryID, st.result
	th, w, h := p.th, p.w, p.h
	return func() tea.Msg {
		log, err := open()
		if err != nil {
			return diffReadyMsg{gen: gen, problem: "Could not read the history."}
		}
		var other *history.Entry
		label := "Previous"
		if entryID == "" {
			items, err := log.List(history.Filter{RequestID: reqID, Limit: historyLimit})
			if err != nil {
				return diffReadyMsg{gen: gen, problem: "Could not read the history."}
			}
			prev, ok := previousEntry(items, curID)
			if !ok {
				return diffReadyMsg{gen: gen, problem: "No earlier send of this request to compare with."}
			}
			entryID = prev.ID
		} else {
			label = "Past send"
		}
		other, err = log.Get(entryID)
		if err != nil {
			if errors.Is(err, history.ErrNotFound) {
				return diffReadyMsg{gen: gen, problem: "That history entry no longer exists."}
			}
			return diffReadyMsg{gen: gen, problem: "Could not read that history entry."}
		}
		return buildDiff(gen, th, w, h, label, other, res)
	}
}

// buildDiff computes the body and header diffs. Both sides are redacted text:
// the stored entry was redacted when it was recorded, and the current response
// goes through its own redactor here whether or not the pane is revealed, so a
// diff never shows a secret.
func buildDiff(gen int, th ui.Theme, w, h int, label string, other *history.Entry, cur tui.SendResult) diffReadyMsg {
	oldBody, oldHdrs := entryTexts(other)
	newBody, newHdrs := resultTexts(cur)
	glyph := th.Glyphs.Mask
	fix := func(s string) string { return strings.ReplaceAll(s, history.Mask, glyph) }
	fixAll := func(in []string) []string {
		out := make([]string, len(in))
		for i, s := range in {
			out[i] = fix(s)
		}
		return out
	}
	left := diffSide(label, other.Status, other.Error != "", other.DurationMS, other.SizeBytes)
	right := diffview.SideSummary{Label: "Current"}
	if cur.Response != nil {
		right = diffSide("Current", cur.Response.StatusCode, false,
			cur.Response.Timings.Total.Milliseconds(), cur.Response.Size.Body+cur.Response.Size.Headers)
	}
	body := diffview.New(th, left, right, fix(oldBody), fix(newBody))
	headers := diffview.NewHeaders(th, left, right, fixAll(oldHdrs), fixAll(newHdrs))
	body.SetSize(w, h)
	headers.SetSize(w, h)
	return diffReadyMsg{gen: gen, body: body, headers: headers}
}

func diffSide(label string, status int, failed bool, ms, size int64) diffview.SideSummary {
	st := fmt.Sprintf("%d", status)
	if failed || status == 0 {
		st = "Failed"
	}
	return diffview.SideSummary{Label: label, Status: st, DurationMS: ms, SizeBytes: size}
}

// entryTexts is the stored (already redacted) body and header lines.
func entryTexts(e *history.Entry) (body string, hdrs []string) {
	if e == nil || e.Response == nil {
		return "", nil
	}
	body = e.Response.Body
	if e.Response.BodyEncoding == "base64" {
		body = "(binary body)"
	}
	for _, h := range e.Response.Headers {
		hdrs = append(hdrs, h.Key+": "+h.Value)
	}
	return body, hdrs
}

// resultTexts is the live response body and headers, redacted.
func resultTexts(r tui.SendResult) (body string, hdrs []string) {
	if r.Response == nil {
		return "", nil
	}
	if DetectBodyKind(history.ContentType(r.Response.Headers), r.Response.Body) == BodyBinary {
		body = "(binary body)"
	} else {
		body = string(MaskBodyForExternalOutput(r))
	}
	for _, h := range resultRedactor(r).Headers(r.Response.Headers) {
		hdrs = append(hdrs, h.Key+": "+h.Value)
	}
	return body, hdrs
}

func (p *Pane) onDiffReady(msg diffReadyMsg) tea.Cmd {
	if msg.gen != p.diffGen || !p.diff.open {
		return nil
	}
	if msg.problem != "" {
		p.diff = diffState{}
		return toast(ui.ToastInfo, msg.problem)
	}
	p.diff.loading = false
	p.diff.body, p.diff.headers = msg.body, msg.headers
	p.sizeDiff()
	return nil
}

func (p *Pane) sizeDiff() {
	if p.diff.body != nil {
		p.diff.body.SetSize(p.w, p.h)
	}
	if p.diff.headers != nil {
		p.diff.headers.SetSize(p.w, p.h)
	}
}

// closeDiff leaves the diff and drops any result still being computed.
func (p *Pane) closeDiff() {
	p.diffGen++
	p.diff = diffState{}
}

// diffKey handles keys while the diff is open.
func (p *Pane) diffKey(s string, msg tea.KeyPressMsg) tea.Cmd {
	switch s {
	case "esc", "q":
		p.closeDiff()
		return nil
	case "h", "t":
		p.diff.showBody = !p.diff.showBody
		return nil
	case "ctrl+d":
		msg = tea.KeyPressMsg(tea.Key{Code: tea.KeyPgDown})
	case "ctrl+u":
		msg = tea.KeyPressMsg(tea.Key{Code: tea.KeyPgUp})
	}
	if p.diff.loading {
		return nil
	}
	m := p.diff.headers
	if p.diff.showBody {
		m = p.diff.body
	}
	if m != nil {
		_, cmd := m.Update(msg)
		return cmd
	}
	return nil
}

func (p *Pane) viewDiff(w int) string {
	t := p.th
	if p.diff.loading || p.diff.body == nil {
		return t.EmptyState("Comparing…", "Building the response diff.", "Esc cancels.", w)
	}
	if p.diff.showBody {
		return p.diff.body.View()
	}
	return strings.Replace(p.diff.headers.View(), "Response diff", "Response diff (headers)", 1)
}
