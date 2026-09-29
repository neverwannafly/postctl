package main

import (
	"bytes"
	"strings"
	"testing"
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
		for _, want := range []string{"postctl import", "postctl ls", "postctl send", "postctl run", "postctl env", "--version"} {
			if !strings.Contains(out, want) {
				t.Errorf("%v: help lacks %q", args, want)
			}
		}
	}
}

func TestStubsExitThree(t *testing.T) {
	for _, c := range []string{"import", "ls", "send", "run", "env"} {
		code, out, errOut := runCLI(c, "x")
		if code != 3 || out != "" || !strings.Contains(errOut, "not implemented yet") {
			t.Errorf("%s: code=%d out=%q err=%q", c, code, out, errOut)
		}
		code, out, _ = runCLI(c, "--help")
		if code != 0 || !strings.Contains(out, "Usage: postctl "+c) {
			t.Errorf("%s --help: code=%d out=%q", c, code, out)
		}
	}
}

func TestNoArgsAndUnknown(t *testing.T) {
	if code, _, errOut := runCLI(); code != 3 || !strings.Contains(errOut, "not implemented yet") {
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
