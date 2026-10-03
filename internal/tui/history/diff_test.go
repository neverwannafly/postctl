package history

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	hist "github.com/shubham/postctl/internal/history"
	"github.com/shubham/postctl/internal/tui"
	"github.com/shubham/postctl/internal/ui"
)

func TestDiffGenerationIgnoresLatePairAndResult(t *testing.T) {
	entries := []hist.Entry{
		diffEntry("a1", "same"),
		diffEntry("a2", "same"),
		diffEntry("b1", "left"),
		diffEntry("b2", "right"),
	}
	log := &memLog{entries: entries}
	m := New(tui.ScreenEnv{PaneEnv: tui.PaneEnv{Theme: ui.NewTheme(true, true, false)}},
		Deps{Log: func() (tui.HistoryReader, error) { return log, nil }})
	m.items = entries

	m.markID, m.cur = "a1", 1
	_, oldPairCmd := m.beginDiff()
	oldGen := m.diffGen

	m.phase = phList
	m.markID, m.cur = "b1", 3
	_, newPairCmd := m.beginDiff()
	newGen := m.diffGen

	_, lateCmd := m.Update(oldPairCmd())
	if lateCmd != nil || m.diff != nil {
		t.Fatal("late pair from the first generation was accepted")
	}
	_, _ = m.Update(diffReadyMsg{gen: oldGen})
	if m.diff != nil {
		t.Fatal("late result from the first generation was accepted")
	}

	_, readyCmd := m.Update(newPairCmd())
	if readyCmd == nil {
		t.Fatal("current pair did not start diff computation")
	}
	_, _ = m.Update(readyCmd())
	if m.diff == nil || len(m.diff.Hunks) == 0 || m.diffGen != newGen {
		t.Fatal("second generation was not shown")
	}

	m.phase = phDiff
	_, _ = m.Update(tea.KeyPressMsg{Text: "esc"})
	_, _ = m.Update(diffReadyMsg{gen: newGen})
	if m.phase != phList {
		t.Fatal("result delivered after Esc reopened the diff")
	}
}

func diffEntry(id, body string) hist.Entry {
	return hist.Entry{
		ID: id,
		Response: &hist.ResponseSnapshot{
			Body: body,
		},
	}
}
