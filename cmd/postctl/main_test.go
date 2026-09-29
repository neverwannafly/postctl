package main

import (
	"bytes"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shubham/postctl/internal/tui"
)

func runCLI(args ...string) (code int, out, errOut string) {
	var o, e bytes.Buffer
	code = run(args, &o, &e)
	return code, o.String(), e.String()
}

func TestVersion(t *testing.T) {
	old := version
	defer func() { version = old }()
	version = "1.2.3"
	code, out, _ := runCLI("--version")
	if code != 0 || out != "postctl 1.2.3\n" {
		t.Fatalf("code=%d out=%q", code, out)
	}
}

func TestHelp(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"-h"}, {"help"}} {
		code, out, _ := runCLI(args...)
		if code != 0 {
			t.Fatalf("%v: code %d", args, code)
		}
		for _, want := range []string{"postctl import", "postctl ls", "postctl send", "postctl run", "postctl env", "postctl curl", "--version"} {
			if !strings.Contains(out, want) {
				t.Errorf("%v: help lacks %q", args, want)
			}
		}
	}
}

func TestCommandHelp(t *testing.T) {
	for _, c := range []string{"ls", "send", "run", "env", "curl"} {
		code, out, _ := runCLI(c, "--help")
		if code != 0 || !strings.Contains(out, "Usage: postctl "+c) {
			t.Errorf("%s --help: code=%d out=%q", c, code, out)
		}
	}
}

func TestImportWorkspaceFlagMayFollowSubcommand(t *testing.T) {
	args, workspace, err := extractImportWorkspace([]string{"--workspace", "/tmp/work", "postman", "collection.json"})
	if err != nil {
		t.Fatal(err)
	}
	if workspace != "/tmp/work" || len(args) != 2 || args[0] != "postman" {
		t.Fatalf("workspace=%q args=%v", workspace, args)
	}
}

func TestNoArgsAndUnknown(t *testing.T) {
	launchUI = func(tui.Options) error { return nil }
	if code, _, errOut := runCLI(); code != 0 || errOut != "" {
		t.Errorf("no args: %d %q", code, errOut)
	}
	code, _, errOut := runCLI("bogus")
	if code != 3 || !strings.Contains(errOut, `Unknown command "bogus"`) {
		t.Errorf("unknown: %d %q", code, errOut)
	}
	if code, _, errOut := runCLI("--nope"); code != 3 || !strings.Contains(errOut, "--help") {
		t.Errorf("bad flag: %d %q", code, errOut)
	}
}

// stubUI replaces the UI launcher and records the options it was given.
func stubUI(t *testing.T, err error) *tui.Options {
	t.Helper()
	var got tui.Options
	old := launchUI
	launchUI = func(o tui.Options) error { got = o; return err }
	t.Cleanup(func() { launchUI = old })
	return &got
}

func TestNoArgsLaunchesUIWithFlags(t *testing.T) {
	dir := t.TempDir()
	got := stubUI(t, nil)
	if code, _, errOut := runCLI("--workspace", dir, "--accessible", "--ascii"); code != 0 || errOut != "" {
		t.Fatalf("code=%d err=%q", code, errOut)
	}
	if got.Backend == nil || got.Backend.Root() != dir || !got.Accessible || !got.ASCII {
		t.Fatalf("options: %+v", got)
	}
}

func TestWorkspaceFromEnvironmentAndDiscovery(t *testing.T) {
	dir := t.TempDir()
	got := stubUI(t, nil)
	t.Setenv("POSTCTL_WORKSPACE", dir)
	if code, _, _ := runCLI(); code != 0 || got.Backend.Root() != dir {
		t.Fatalf("POSTCTL_WORKSPACE ignored: %+v", got)
	}
	// The flag wins over the environment.
	other := t.TempDir()
	if code, _, _ := runCLI("--workspace", other); code != 0 || got.Backend.Root() != other {
		t.Fatalf("flag should win: %v", got.Backend.Root())
	}
	// Without either, discovery falls back to the global directory.
	t.Setenv("POSTCTL_WORKSPACE", "")
	home := filepath.Join(t.TempDir(), "home")
	t.Setenv("POSTCTL_HOME", home)
	t.Chdir(t.TempDir())
	if code, _, _ := runCLI(); code != 0 || got.Backend.Root() != home {
		t.Fatalf("discovery: %v", got.Backend.Root())
	}
}

func TestUIFailureIsExplained(t *testing.T) {
	stubUI(t, errors.New("no tty"))
	code, _, errOut := runCLI("--workspace", t.TempDir())
	if code != 3 || !strings.Contains(errOut, "needs a terminal") || !strings.Contains(errOut, "no tty") {
		t.Fatalf("code=%d err=%q", code, errOut)
	}
}

func TestHelpMentionsGlobalFlags(t *testing.T) {
	_, out, _ := runCLI("--help")
	for _, want := range []string{"--workspace", "--accessible", "--ascii", "NO_COLOR"} {
		if !strings.Contains(out, want) {
			t.Errorf("help lacks %q", want)
		}
	}
}
