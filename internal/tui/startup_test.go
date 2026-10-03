package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/shubham/postctl/internal/ui"
)

func TestStartupCmdShowsNoticeOnce(t *testing.T) {
	root := copyDir(t, demoDir)
	calls := 0
	o := Options{
		Backend: NewStoreBackend(root),
		NoWatch: true,
		StartupCmd: func() tea.Msg {
			calls++
			return ToastMsg{Kind: ui.ToastWarning, Text: "tmux: set extended-keys on (see docs/terminal.md)"}
		},
	}
	a := newApp(o)
	h := &harness{t: t, a: a}
	h.msg(sizeMsg(80, 24))
	h.run(a.Init())
	if calls != 1 {
		t.Fatalf("StartupCmd runs once in Init, got %d calls", calls)
	}
	if a.toast == nil || a.toast.text == "" {
		t.Fatal("expected a status-line toast from StartupCmd")
	}
}
