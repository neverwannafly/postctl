package response

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/shubham/postctl/internal/history"
	"github.com/shubham/postctl/internal/model"
	"github.com/shubham/postctl/internal/tui"
	"github.com/shubham/postctl/internal/tui/vimtext"
	"github.com/shubham/postctl/internal/ui"
)

// jqState is the jq filter on the Body tab.
type jqState struct {
	open  bool
	input *vimtext.Model
	// filter is the applied filter; empty means the plain body shows.
	filter string
	// gen numbers runs; a result for an older run is dropped.
	gen int
	// dirty asks for a new run (new filter, new response).
	dirty bool
	// started reports that a run was requested for filter; reqReveal is the
	// reveal state that run used.
	started   bool
	reqReveal bool
	// have reports a finished run; doneReveal is the reveal state it used.
	have       bool
	doneReveal bool
	lines      []string
	truncated  bool
	err        string
}

// jqDoneMsg carries one finished filter run.
type jqDoneMsg struct {
	gen    int
	reqID  model.ID
	reveal bool
	out    jqOutput
}

func (p *Pane) newJQInput(value string) {
	in := vimtext.New(p.th, vimtext.Options{
		SingleLine:  true,
		StartInsert: true,
		Placeholder: ".items[] | select(.ok)",
	})
	in.SetSize(max(1, p.w-4), 1)
	in.SetValue(value)
	_ = in.Focus()
	// Start at the end of the filter so typing extends it.
	in.SetCursor(0, len([]rune(value)))
	p.jq.input = in
}

// jqNeedsJSON explains why jq cannot open now, or returns "".
func (p *Pane) jqUnavailable() string {
	if p.ref.Request == nil {
		return "Select a request first."
	}
	st := p.current()
	if st == nil || st.result.Response == nil {
		return "Nothing to filter: send the request first (Ctrl-S)."
	}
	if p.bodyKindOf(st.result) != BodyJSON {
		return "jq needs a JSON body."
	}
	return ""
}

func (p *Pane) bodyKindOf(r tui.SendResult) BodyKind {
	if r.Response == nil {
		return BodyBinary
	}
	return DetectBodyKind(history.ContentType(r.Response.Headers), r.Response.Body)
}

// cmdOpenJQ is Space j: open the filter input above the body.
func (p *Pane) cmdOpenJQ() tea.Cmd {
	if why := p.jqUnavailable(); why != "" {
		return toast(ui.ToastInfo, why)
	}
	p.closeDiff()
	p.tab = tabBody
	p.revealSecrets = false
	p.newJQInput(p.jq.filter)
	p.jq.open = true
	return focusResponse()
}

// jqClear removes the filter and closes the input.
func (p *Pane) jqClear() {
	p.jq = jqState{gen: p.jq.gen + 1}
	p.scroll = 0
}

// jqKey routes a message to the open filter input.
func (p *Pane) jqUpdate(msg tea.Msg) tea.Cmd {
	cmd, ev := p.jq.input.Update(msg)
	switch ev.Kind {
	case vimtext.Submit:
		filter := strings.TrimSpace(p.jq.input.Value())
		if filter == "" {
			p.jqClear()
			return cmd
		}
		p.jq.open = false
		p.jq.input.Blur()
		p.jq.filter = filter
		p.jq.dirty, p.jq.started, p.jq.have = true, false, false
		p.jq.err = ""
		p.scroll = 0
		return tea.Batch(cmd, p.jqSync())
	case vimtext.Leave:
		p.jqClear()
	}
	return cmd
}

// jqSync starts a run when the filter, the response or the reveal state
// changed since the last one. It is called after every Update, so the shown
// output can never be a stale unmasked result.
func (p *Pane) jqSync() tea.Cmd {
	j := &p.jq
	if j.filter == "" {
		return nil
	}
	st := p.current()
	if st == nil || st.result.Response == nil {
		return nil
	}
	if !j.dirty && j.started && j.reqReveal == p.revealSecrets {
		return nil
	}
	j.gen++
	j.dirty, j.started, j.reqReveal = false, true, p.revealSecrets
	if p.bodyKindOf(st.result) != BodyJSON {
		j.have, j.doneReveal, j.lines, j.truncated = true, p.revealSecrets, nil, false
		j.err = "The body is not JSON."
		return nil
	}
	gen, reqID, filter, reveal, res := j.gen, p.requestID(), j.filter, p.revealSecrets, st.result
	return func() tea.Msg {
		return jqDoneMsg{gen: gen, reqID: reqID, reveal: reveal,
			out: runJQ(filter, jqSource(res, reveal), jqTimeout, jqMaxOutput)}
	}
}

func (p *Pane) onJQDone(msg jqDoneMsg) {
	j := &p.jq
	if msg.gen != j.gen || msg.reqID != p.requestID() || j.filter == "" {
		return
	}
	glyph := p.th.Glyphs.Mask
	j.have, j.doneReveal = true, msg.reveal
	j.err, j.truncated = msg.out.Err, msg.out.Truncated
	j.lines = nil
	if text := strings.TrimRight(strings.ReplaceAll(msg.out.Text, history.Mask, glyph), "\n"); text != "" {
		j.lines = strings.Split(text, "\n")
	}
	p.clampScroll()
}

// jqShowing reports that the Body tab shows jq output.
func (p *Pane) jqShowing() bool {
	j := p.jq
	return p.tab == tabBody && j.filter != "" && j.have && j.doneReveal == p.revealSecrets && j.err == ""
}

// jqBarVisible reports that the one-line jq bar takes a row under the tabs.
func (p *Pane) jqBarVisible() bool {
	return p.tab == tabBody && (p.jq.open || p.jq.filter != "")
}

func (p *Pane) viewJQBar(w int) string {
	t := p.th
	prefix := t.Render(ui.AccentPrimary, "jq ")
	if p.jq.open && p.jq.input != nil {
		return t.Truncate(prefix+p.jq.input.View(), w)
	}
	line := prefix + t.Render(ui.TextPrimary, p.jq.filter)
	switch {
	case p.jq.err != "":
		line += "  " + t.Render(ui.StatusDanger, t.Glyphs.Fail+" "+p.jq.err)
	case p.jq.truncated:
		line += "  " + t.Render(ui.StatusWarning, t.Glyphs.Warn+" Output truncated at 1 MiB.")
	}
	return t.Truncate(line, w)
}

// viewJQ renders the Body tab while a filter is set.
func (p *Pane) viewJQ(r tui.SendResult, w, h int) string {
	t := p.th
	bar := p.viewJQBar(w)
	if p.jq.filter == "" {
		return bar + "\n" + p.viewBody(r, w, h)
	}
	j := p.jq
	var body string
	switch {
	case !j.have || j.doneReveal != p.revealSecrets:
		body = t.EmptyState("Filtering…", "Running the filter on the masked body.", "", w)
	case j.err != "":
		body = t.EmptyState("No jq output", "The filter did not run.", "Edit it with Space j, or press Esc to clear it.", w)
	case len(j.lines) == 0:
		body = t.EmptyState("The filter produced no output.", "", "Edit it with Space j, or press Esc to clear it.", w)
	default:
		var lines []string
		for i := p.scroll; i < len(j.lines) && len(lines) < h; i++ {
			lines = append(lines, t.Truncate(fmt.Sprintf("%4d %s", i+1, highlightLine(t, "json", j.lines[i])), w))
		}
		body = strings.Join(lines, "\n")
	}
	return bar + "\n" + body
}

// jqText is the filtered output as text (already masked unless revealed).
func (p *Pane) jqText() string { return strings.Join(p.jq.lines, "\n") }
