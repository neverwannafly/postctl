package main

import (
	"context"
	"errors"
	"os"

	tea "charm.land/bubbletea/v2"
	"github.com/shubham/postctl/internal/termx"
	"github.com/shubham/postctl/internal/tui"
	"github.com/shubham/postctl/internal/ui"
)

func wireTerminal(o *tui.Options) {
	if o.OnEdge != nil && o.StartupCmd != nil {
		return
	}
	r := termx.DefaultRunner()
	if o.OnEdge == nil {
		o.OnEdge = onEdge(r)
	}
	if o.StartupCmd == nil {
		o.StartupCmd = startupNotice(r)
	}
}

func startupNotice(r termx.Runner) tea.Cmd {
	return func() tea.Msg {
		text := termx.CheckSettings(context.Background(), r, os.Environ())
		if text == "" {
			return tui.StartupNoopMsg{}
		}
		return tui.ToastMsg{Kind: ui.ToastWarning, Text: text}
	}
}

func onEdge(r termx.Runner) func(tui.Direction) tea.Cmd {
	return func(dir tui.Direction) tea.Cmd {
		return func() tea.Msg {
			err := termx.HandoffPane(context.Background(), r, dir.String())
			if errors.Is(err, termx.ErrTmuxNotFound) {
				return tui.ToastMsg{Kind: ui.ToastWarning, Text: "tmux not found; install tmux for pane handoff"}
			}
			return tui.StartupNoopMsg{}
		}
	}
}
