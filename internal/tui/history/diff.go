package history

import (
	"fmt"

	tea "charm.land/bubbletea/v2"
	"github.com/shubham/postctl/internal/history"
	"github.com/shubham/postctl/internal/tui"
	"github.com/shubham/postctl/internal/tui/diffview"
)

func (m *Model) beginDiff() (tui.Screen, tea.Cmd) {
	if m.markID == "" {
		m.notice, m.fail = "Mark an entry with m, then press d to diff.", false
		return m, nil
	}
	cur := m.current()
	if cur == nil || cur.ID == m.markID {
		m.notice, m.fail = "Select a different entry to compare with the mark.", false
		return m, nil
	}
	return m, m.loadDiffPair(m.markID, cur.ID)
}

type diffPairMsg struct {
	left, right *history.Entry
	err         error
}

func (m *Model) loadDiffPair(markID, otherID string) tea.Cmd {
	return func() tea.Msg {
		log, err := m.deps.Log()
		if err != nil {
			return diffPairMsg{err: err}
		}
		left, err := log.Get(markID)
		if err != nil {
			return diffPairMsg{err: err}
		}
		right, err := log.Get(otherID)
		if err != nil {
			return diffPairMsg{err: err}
		}
		return diffPairMsg{left: left, right: right}
	}
}

func (m *Model) onDiffPair(msg diffPairMsg) (tui.Screen, tea.Cmd) {
	if msg.err != nil {
		m.notice, m.fail = msg.err.Error(), true
		return m, nil
	}
	leftText, leftHdrs := responseTexts(msg.left)
	rightText, rightHdrs := responseTexts(msg.right)
	m.diff = diffview.New(m.th,
		sideSummary("Marked", msg.left), sideSummary("Current", msg.right),
		leftText, rightText,
	)
	m.diffHeaders = diffview.NewHeaders(m.th,
		sideSummary("Marked", msg.left), sideSummary("Current", msg.right),
		leftHdrs, rightHdrs,
	)
	m.diffBodyMode = true
	m.phase = phDiff
	m.diff.SetSize(m.w, m.h)
	m.diffHeaders.SetSize(m.w, m.h)
	return m, nil
}

func sideSummary(label string, e *history.Entry) diffview.SideSummary {
	st := fmt.Sprintf("%d", e.Status)
	if e.Error != "" {
		st = "Failed"
	}
	return diffview.SideSummary{Label: label, Status: st, DurationMS: e.DurationMS, SizeBytes: e.SizeBytes}
}

func responseTexts(e *history.Entry) (body string, hdrs []string) {
	if e == nil || e.Response == nil {
		return "", nil
	}
	body = responseBodyString(e.Response)
	for _, h := range e.Response.Headers {
		hdrs = append(hdrs, h.Key+": "+h.Value)
	}
	return body, hdrs
}

func responseBodyString(r *history.ResponseSnapshot) string {
	if r == nil {
		return ""
	}
	if r.BodyEncoding == "base64" {
		return "(binary body)"
	}
	return r.Body
}

func (m *Model) diffKey(k tea.KeyPressMsg) (tui.Screen, tea.Cmd) {
	switch k.String() {
	case "esc", "q":
		m.phase = phList
		return m, nil
	case "h", "t":
		m.diffBodyMode = !m.diffBodyMode
		return m, nil
	}
	var cmd tea.Cmd
	if m.diffBodyMode {
		m.diff, cmd = m.diff.Update(k)
	} else {
		m.diffHeaders, cmd = m.diffHeaders.Update(k)
	}
	return m, cmd
}

func (m *Model) viewDiff() string {
	if m.diffBodyMode {
		return m.screenHeader("History diff") + "\n" + m.diff.View()
	}
	return m.screenHeader("History diff (headers)") + "\n" + m.diffHeaders.View()
}

func (m *Model) markLabel(e history.Entry) string {
	if e.ID != m.markID {
		return "  "
	}
	return "* "
}
