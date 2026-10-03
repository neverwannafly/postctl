package termx

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

type fakeRunner struct {
	calls []string
	out   map[string]string
	err   map[string]error
	delay time.Duration
}

func (f *fakeRunner) key(name string, args ...string) string {
	return name + "\000" + strings.Join(args, "\000")
}

func (f *fakeRunner) Run(ctx context.Context, name string, args ...string) (string, error) {
	f.calls = append(f.calls, f.key(name, args...))
	if f.delay > 0 {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(f.delay):
		}
	}
	k := f.key(name, args...)
	if e, ok := f.err[k]; ok {
		return "", e
	}
	if v, ok := f.out[k]; ok {
		return v, nil
	}
	return "", errors.New("unexpected command")
}

func tmuxEnv() []string {
	return []string{"TMUX=/tmp/tmux-123,0,0", "PATH=/usr/bin"}
}

func goodTmuxOut() map[string]string {
	return map[string]string{
		"tmux\000show\000-gv\000extended-keys":            "on",
		"tmux\000show\000-gv\000escape-time":              "0",
		"tmux\000show\000-gv\000mouse":                    "on",
		"tmux\000show\000-gv\000set-clipboard":            "on",
		"tmux\000display\000-p\000#{client_termfeatures}": "extkeys",
		"tmux\000display\000-p\000#{window_zoomed_flag}":  "0",
	}
}

func TestCheckSettingsGood(t *testing.T) {
	r := &fakeRunner{out: goodTmuxOut()}
	if got := CheckSettings(context.Background(), r, tmuxEnv()); got != "" {
		t.Fatalf("notice = %q, want empty", got)
	}
}

func TestCheckSettingsEachBad(t *testing.T) {
	cases := []struct {
		opt string
		val string
	}{
		{"extended-keys", "off"},
		{"escape-time", "500"},
		{"mouse", "off"},
		{"set-clipboard", "off"},
	}
	for _, c := range cases {
		out := goodTmuxOut()
		out["tmux\000show\000-gv\000"+c.opt] = c.val
		r := &fakeRunner{out: out}
		got := CheckSettings(context.Background(), r, tmuxEnv())
		if got == "" {
			t.Fatalf("%s=%q: want a notice", c.opt, c.val)
		}
		if !strings.Contains(got, "tmux:") || !strings.Contains(got, docLink) {
			t.Fatalf("%s: notice %q", c.opt, got)
		}
	}
}

func TestCheckSettingsNotInTmux(t *testing.T) {
	r := &fakeRunner{out: goodTmuxOut()}
	if got := CheckSettings(context.Background(), r, []string{"HOME=/tmp"}); got != "" {
		t.Fatalf("got %q", got)
	}
}

func TestCheckSettingsTimeout(t *testing.T) {
	r := &fakeRunner{out: goodTmuxOut(), delay: 400 * time.Millisecond}
	if got := CheckSettings(context.Background(), r, tmuxEnv()); got != "" {
		t.Fatalf("timeout should stay quiet, got %q", got)
	}
}

func TestCheckSettingsTmuxMissing(t *testing.T) {
	r := &fakeRunner{err: map[string]error{
		"tmux\000show\000-gv\000extended-keys": ErrTmuxNotFound,
	}}
	if got := CheckSettings(context.Background(), r, tmuxEnv()); got != "" {
		t.Fatalf("got %q", got)
	}
}

func TestCheckSettingsGhosttyTermfeatures(t *testing.T) {
	out := goodTmuxOut()
	out["tmux\000display\000-p\000#{client_termfeatures}"] = "paste"
	env := append(tmuxEnv(), "TERM_PROGRAM=ghostty")
	r := &fakeRunner{out: out}
	got := CheckSettings(context.Background(), r, env)
	if !strings.Contains(got, "terminal-features") {
		t.Fatalf("got %q", got)
	}
}

func TestIsGhostty(t *testing.T) {
	if !IsGhostty([]string{"TERM_PROGRAM=ghostty"}) {
		t.Fatal("TERM_PROGRAM")
	}
	if !IsGhostty([]string{"TERM=xterm-ghostty"}) {
		t.Fatal("TERM")
	}
	if IsGhostty([]string{"TERM=xterm-256color"}) {
		t.Fatal("plain xterm")
	}
}

func TestInTmux(t *testing.T) {
	if !InTmux(tmuxEnv()) {
		t.Fatal("expected in tmux")
	}
	if InTmux(nil) && os.Getenv("TMUX") == "" {
		t.Fatal("nil env without TMUX")
	}
}

func TestPaneZoomed(t *testing.T) {
	r := &fakeRunner{out: map[string]string{
		"tmux\000display\000-p\000#{window_zoomed_flag}": "1",
	}}
	ok, err := PaneZoomed(context.Background(), r)
	if err != nil || !ok {
		t.Fatalf("zoomed: ok=%v err=%v", ok, err)
	}
}

func TestHandoffPaneZoomedSkips(t *testing.T) {
	t.Setenv("TMUX", "/tmp/tmux-1,0,0")
	r := &fakeRunner{out: map[string]string{
		"tmux\000display\000-p\000#{window_zoomed_flag}": "1",
	}}
	if err := HandoffPane(context.Background(), r, "left"); err != nil {
		t.Fatal(err)
	}
	for _, c := range r.calls {
		if strings.Contains(c, "select-pane") {
			t.Fatalf("select-pane must not run when zoomed: %v", r.calls)
		}
	}
}

func TestHandoffPaneSelects(t *testing.T) {
	t.Setenv("TMUX", "/tmp/tmux-1,0,0")
	r := &fakeRunner{out: map[string]string{
		"tmux\000display\000-p\000#{window_zoomed_flag}": "0",
	}}
	dirs := map[string]string{
		"left": "-L", "down": "-D", "up": "-U", "right": "-R",
	}
	for dir, flag := range dirs {
		r.calls = nil
		if err := HandoffPane(context.Background(), r, dir); err != nil {
			t.Fatal(dir, err)
		}
		want := "tmux\000select-pane\000" + flag
		found := false
		for _, c := range r.calls {
			if c == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("%s: calls %v want %q", dir, r.calls, want)
		}
	}
}

func TestSelectPaneArgv(t *testing.T) {
	cases := map[string]string{
		"left": "-L", "down": "-D", "up": "-U", "right": "-R",
	}
	for dir, flag := range cases {
		got := SelectPaneArgv(dir)
		want := []string{"tmux", "select-pane", flag}
		if len(got) != len(want) {
			t.Fatalf("%s: %v", dir, got)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("%s: got %v want %v", dir, got, want)
			}
		}
	}
}

func TestEscapeTimeOK(t *testing.T) {
	if !EscapeTimeOK("0") || !EscapeTimeOK("10") {
		t.Fatal("0 and 10 ok")
	}
	if EscapeTimeOK("11") || EscapeTimeOK("bad") {
		t.Fatal("11 and bad not ok")
	}
}

func TestCheckSettingsAtMostOneNotice(t *testing.T) {
	out := goodTmuxOut()
	out["tmux\000show\000-gv\000extended-keys"] = "off"
	out["tmux\000show\000-gv\000escape-time"] = "500"
	out["tmux\000show\000-gv\000mouse"] = "off"
	r := &fakeRunner{out: out}
	got := CheckSettings(context.Background(), r, tmuxEnv())
	if strings.Count(got, "tmux:") != 1 {
		t.Fatalf("want one notice, got %q", got)
	}
}
