package main

import (
	"context"
	"testing"

	"github.com/shubham/postctl/internal/tui"
)

func TestOnEdgeUsesExactArgv(t *testing.T) {
	t.Setenv("TMUX", "/tmp/tmux-1,0,0")
	r := &edgeFakeRunner{zoomed: "0"}
	h := onEdge(r)
	for _, c := range []struct {
		dir  tui.Direction
		flag string
	}{
		{tui.DirLeft, "-L"},
		{tui.DirDown, "-D"},
		{tui.DirUp, "-U"},
		{tui.DirRight, "-R"},
	} {
		r.calls = nil
		cmd := h(c.dir)
		if cmd == nil {
			t.Fatal("expected cmd")
		}
		msg := cmd()
		if _, ok := msg.(tui.StartupNoopMsg); !ok {
			t.Fatalf("dir %v: msg %T", c.dir, msg)
		}
		want := "tmux\000select-pane\000" + c.flag
		if len(r.calls) != 2 || r.calls[1] != want {
			t.Fatalf("dir %v: calls %v want select %q", c.dir, r.calls, want)
		}
	}
}

type edgeFakeRunner struct {
	calls  []string
	zoomed string
}

func (f *edgeFakeRunner) Run(_ context.Context, name string, args ...string) (string, error) {
	key := name
	for _, a := range args {
		key += "\000" + a
	}
	f.calls = append(f.calls, key)
	if name == "tmux" && len(args) >= 2 && args[0] == "display" {
		return f.zoomed, nil
	}
	return "", nil
}
