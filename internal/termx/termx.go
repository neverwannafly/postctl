// Package termx detects tmux and terminal settings postctl relies on, and
// builds argv for tmux pane handoff. It has no UI dependencies.
package termx

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

const (
	checkTimeout = 300 * time.Millisecond
	docLink      = "docs/terminal.md"
)

// ErrTmuxNotFound means the tmux executable is not on PATH.
var ErrTmuxNotFound = errors.New("termx: tmux not found")

// Runner runs a subprocess with a context deadline. Tests inject a fake.
type Runner interface {
	Run(ctx context.Context, name string, args ...string) (stdout string, err error)
}

// ExecRunner runs real commands with the given per-call timeout.
type ExecRunner struct {
	Timeout time.Duration
}

// Run implements [Runner].
func (e ExecRunner) Run(ctx context.Context, name string, args ...string) (string, error) {
	timeout := e.Timeout
	if timeout == 0 {
		timeout = checkTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.Output()
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return strings.TrimSpace(string(out)), err
		}
		if errors.Is(err, exec.ErrNotFound) {
			return "", fmt.Errorf("%w: %s", ErrTmuxNotFound, name)
		}
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// DefaultRunner is the production subprocess runner.
func DefaultRunner() Runner { return ExecRunner{} }

// InTmux reports whether the process appears to run inside tmux.
func InTmux(env []string) bool {
	return envLookup(env, "TMUX") != ""
}

// IsGhostty reports whether the outer terminal is Ghostty.
func IsGhostty(env []string) bool {
	prog := strings.ToLower(envLookup(env, "TERM_PROGRAM"))
	if prog == "ghostty" {
		return true
	}
	term := strings.ToLower(envLookup(env, "TERM"))
	return strings.Contains(term, "ghostty") || strings.HasPrefix(term, "xterm-ghostty")
}

func envLookup(env []string, key string) string {
	if env == nil {
		return os.Getenv(key)
	}
	prefix := key + "="
	for _, e := range env {
		if strings.HasPrefix(e, prefix) {
			return e[len(prefix):]
		}
	}
	return ""
}

// CheckSettings inspects tmux when [InTmux] is true and returns at most one
// short notice for the status line, or "" when everything looks fine.
func CheckSettings(ctx context.Context, r Runner, env []string) string {
	if !InTmux(env) {
		return ""
	}
	ctx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()

	type setting struct {
		name string
		ok   func(string) bool
		fix  string
	}
	checks := []setting{
		{"extended-keys", extendedKeysOK, "extended-keys on"},
		{"escape-time", escapeTimeOK, "escape-time 0"},
		{"mouse", boolOn, "mouse on"},
		{"set-clipboard", boolOn, "set-clipboard on"},
	}
	var fixes []string
	for _, c := range checks {
		val, err := tmuxGlobal(ctx, r, c.name)
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
				return "" // overall budget exhausted
			}
			continue // unknown on this tmux version: keep checking others
		}
		if !c.ok(val) {
			fixes = append(fixes, c.fix)
		}
	}
	if IsGhostty(env) {
		feat, err := tmuxDisplay(ctx, r, "#{client_termfeatures}")
		if err == nil && feat != "" && !strings.Contains(feat, "extkeys") {
			fixes = append(fixes, "terminal-features 'xterm-ghostty:extkeys'")
		}
	}
	if len(fixes) == 0 {
		return ""
	}
	// One line: lead with keys users feel first, then the rest.
	head := "tmux: set " + strings.Join(fixes, ", ")
	if strings.Contains(head, "extended-keys") || strings.Contains(head, "escape-time") {
		head += " for Ctrl-Enter and Esc"
	}
	return head + " (see " + docLink + ")"
}

func tmuxGlobal(ctx context.Context, r Runner, opt string) (string, error) {
	return r.Run(ctx, "tmux", "show", "-gv", opt)
}

func tmuxDisplay(ctx context.Context, r Runner, format string) (string, error) {
	return r.Run(ctx, "tmux", "display", "-p", format)
}

func extendedKeysOK(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "on", "always":
		return true
	default:
		return false
	}
}

func boolOn(v string) bool {
	return strings.EqualFold(strings.TrimSpace(v), "on")
}

// EscapeTimeOK reports whether escape-time is at most 10 ms.
func EscapeTimeOK(v string) bool {
	return escapeTimeOK(v)
}

func escapeTimeOK(v string) bool {
	v = strings.TrimSpace(v)
	if v == "" {
		return false
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return false
	}
	return n <= 10
}

// PaneZoomed asks tmux whether the current pane's window is zoomed.
func PaneZoomed(ctx context.Context, r Runner) (bool, error) {
	out, err := tmuxDisplay(ctx, r, "#{window_zoomed_flag}")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) == "1", nil
}

// SelectPaneArgv returns the full argv for handing focus to the neighbouring
// tmux pane (no shell). dir is left, down, up or right.
func SelectPaneArgv(dir string) []string {
	return []string{"tmux", "select-pane", paneFlag(dir)}
}

func paneFlag(dir string) string {
	switch strings.ToLower(dir) {
	case "left":
		return "-L"
	case "down":
		return "-D"
	case "up":
		return "-U"
	default:
		return "-R"
	}
}

// HandoffPane moves tmux focus in dir unless the window is zoomed or tmux is
// unavailable. Errors are ignored except [ErrTmuxNotFound].
func HandoffPane(ctx context.Context, r Runner, env []string, dir string) error {
	if !InTmux(env) {
		return nil
	}
	zoomed, err := PaneZoomed(ctx, r)
	if err != nil {
		return nil
	}
	if zoomed {
		return nil
	}
	argv := SelectPaneArgv(dir)
	_, err = r.Run(ctx, argv[0], argv[1:]...)
	if errors.Is(err, ErrTmuxNotFound) {
		return ErrTmuxNotFound
	}
	return nil // other errors: silent
}
