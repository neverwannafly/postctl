package qa_test

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestSecretDoesNotLeakThroughUXFlows presses the keys a person presses in the
// real shell (switcher, which-key, Ctrl-S, g jumps, the response tabs and its
// History, diff and jq views, yanks, the history, environment and runner
// screens, the auth rows of the request and WebSocket panes, $EDITOR) over a
// workspace with a secret in every scope and literal credentials in auth
// rows. Every frame, every clipboard write and every file the fake editor
// saw is searched for each secret: the whole value and its first 12
// characters, raw and with escape sequences stripped. The server answers
// with secrets in a header and a cookie but not in the body, so the Body tab
// (exempt by design) does not need an exemption here.
func TestSecretDoesNotLeakThroughUXFlows(t *testing.T) {
	for _, size := range []struct{ w, h int }{{140, 40}, {80, 24}} {
		t.Run(fmt.Sprintf("%dx%d", size.w, size.h), func(t *testing.T) {
			t.Parallel()
			sec := newUXSecrets(t)
			fx := newUXFixture(t, sec)
			r := newUXRig(t, fx, size.w, size.h)
			runUXFlow(t, r)
			checkUXRig(t, r, sec)
		})
	}
}

// must fails the test, naming the step, when the screen lacks a marker that
// proves the step ran (a sweep that silently skipped a view proves nothing).
func (r *uxRig) must(marker, what string) {
	r.t.Helper()
	if !strings.Contains(r.screen(), marker) {
		r.t.Fatalf("step %q did not reach its view (no %q on a frame of %s)", what, marker, frameShape(r.screen()))
	}
}

// mustClip fails when the step wrote nothing to the clipboard since n writes
// were seen. A toast on screen could be left over from an earlier step, so the
// write itself is the proof.
func (r *uxRig) mustClip(n int, what string) {
	r.t.Helper()
	if len(r.clips) <= n {
		r.t.Fatalf("step %q wrote nothing to the clipboard (frame: %s)", what, frameShape(r.screen()))
	}
}

func runUXFlow(t *testing.T, r *uxRig) {
	t.Helper()

	r.begin("tree")
	r.snap("start")
	r.keys("space")
	r.wait(450 * time.Millisecond)
	r.must("Switch environment", "which-key")
	r.snap("which-key")
	r.keys("esc")

	r.begin("environment")
	r.keys("space", "e")
	r.snap("environment picker")
	r.keys("down", "enter")
	r.must("Secret environment", "environment picked")

	r.begin("switcher")
	r.keys("esc", "esc", "space", "space")
	r.typ("ech")
	r.snap("switcher typed")
	r.keys("enter")
	r.must("/echo/", "switcher open")

	r.begin("request tabs")
	r.snap("request")
	for _, g := range []string{"p", "a", "h", "b", "s", "t", "S", "u"} {
		r.keys("g", g)
		r.snap("tab g" + g)
	}

	r.begin("send")
	r.keys("ctrl+s")
	if !r.waitFor("200", 8*time.Second) {
		t.Fatalf("the send did not produce a 200 response (frame: %s)", frameShape(r.screen()))
	}
	r.snap("sent")
	r.keys("ctrl+s") // a second send gives the diff something to compare
	r.wait(800 * time.Millisecond)
	r.begin("curl")
	clips := len(r.clips)
	r.keys("space", "y")
	r.wait(200 * time.Millisecond)
	if len(r.clips) == clips {
		t.Fatal("Space y wrote nothing to the clipboard")
	}
	if r.w >= 100 {
		// The note belongs to the request pane, which a narrow layout hides
		// while the response is focused (deferred finding, see the ledger).
		r.must("Copied as curl", "copy curl")
	}
	r.snap("copy curl toast")

	r.begin("response tabs")
	r.keys("g", "r")
	r.snap("body")
	for i := 0; i < 6; i++ {
		r.keys("]")
		r.snap(fmt.Sprintf("tab +%d", i+1))
		r.keys("esc") // ends the pending ] repeat
	}
	r.keys("g", "r")

	r.begin("history tab and diff")
	r.keys("g", "H")
	r.must("just now", "response History tab")
	r.snap("history tab")
	r.keys("esc")
	r.keys("space", "x")
	r.must("Previous", "response diff")
	r.snap("diff body")
	r.keys("h")
	r.snap("diff headers")
	r.keys("esc")

	r.begin("jq")
	r.keys("g", "r", "space", "j")
	r.typ(".nested")
	r.snap("jq typing")
	r.keys("enter")
	r.must("Body (jq)", "jq applied")
	r.snap("jq output")
	r.keys("y")
	r.must("jq output copied", "jq yank")
	r.snap("jq yank toast")
	r.keys("esc") // Esc clears the jq view
	for i, filter := range []string{
		".nope | tonumber", `error("x")`, ".items | fromjson", ".nested.count.x", "$ENV.HOME", "env", "input", "{",
		".nested | keys | .[0] | tonumber",
	} {
		r.keys("g", "r", "space", "j")
		r.typ(filter)
		r.keys("enter")
		r.snap(fmt.Sprintf("jq error %d", i))
		r.keys("esc", "esc")
	}
	r.keys("g", "r") // each round already cleared its jq view with Esc
	r.must("[1/7] Body", "jq views cleared")

	r.begin("body yank")
	clips = len(r.clips)
	r.keys("g", "r", "g", "g", "V", "G", "y")
	r.mustClip(clips, "body yank")
	r.snap("body yank toast")
	r.keys("esc")

	r.begin("oauth")
	r.open("oauth")
	r.keys("ctrl+s")
	if !r.waitFor("200", 8*time.Second) {
		t.Fatalf("the OAuth send did not produce a 200 response (frame: %s)", frameShape(r.screen()))
	}
	r.keys("ctrl+s")
	r.wait(800 * time.Millisecond)
	r.snap("oauth sent")
	r.keys("g", "r")
	for i := 0; i < 6; i++ {
		r.keys("]")
		r.snap(fmt.Sprintf("oauth tab +%d", i+1))
		r.keys("esc")
	}
	r.keys("g", "r", "space", "x")
	r.snap("oauth diff")
	r.keys("h")
	r.snap("oauth diff headers")
	r.keys("esc")

	r.begin("history screen")
	r.keys("esc", "esc", "space", "h")
	r.wait(400 * time.Millisecond)
	r.must("Echo", "history screen lists the sends")
	r.snap("history")
	r.keys("/")
	r.typ("ech")
	r.snap("history filter")
	r.keys("enter")
	r.keys("enter")
	r.wait(300 * time.Millisecond)
	r.snap("history detail")
	r.keys("esc", "esc", "esc")

	r.begin("environment editor")
	r.keys("space", "E")
	r.snap("env editor")
	r.keys("enter", "j", "enter")
	r.must("Value (secret)", "env editor secret cell")
	r.snap("env secret insert")
	r.keys("esc")
	r.snap("env secret normal")
	clips = len(r.clips)
	r.keys("y", "y")
	r.keys("v", "$", "y")
	r.mustClip(clips, "env secret yank")
	r.snap("env secret yank toast")
	r.keys("esc", "esc", "esc", "esc")

	r.begin("runner")
	r.keys("esc", "esc", "alt+1", "g", "g")
	r.keys("space", "R")
	r.must("Run", "runner setup")
	r.snap("runner setup")
	r.keys("ctrl+enter")
	r.wait(2 * time.Second)
	r.must("Run summary", "runner results")
	r.snap("runner results")
	r.keys("esc", "esc")

	r.begin("help")
	r.keys("?")
	r.snap("help")
	r.keys("esc")

	r.begin("auth rows")
	r.open("literal")
	r.keys("g", "a")
	r.must("Password", "auth rows")
	r.snap("closed auth rows")
	r.keys("down") // into the rows, on Type
	for row := 1; row <= 2; row++ {
		r.keys("down", "enter")
		r.snap(fmt.Sprintf("auth row %d normal", row))
		r.keys("y", "y")
		r.keys("V", "y")
		r.keys("0", "v", "$", "y")
		r.keys("0", "v", "e", "y")
		r.snap(fmt.Sprintf("auth row %d yank", row))
		r.keys("i")
		r.snap(fmt.Sprintf("auth row %d insert", row))
		r.keys("esc", "esc")
	}
	r.keys("enter") // the Password row again
	r.keys("space", "o")
	r.wait(200 * time.Millisecond)
	r.snap("editor refused")
	r.keys("esc", "esc")

	r.begin("curl on literal auth")
	r.open("literal")
	clips = len(r.clips)
	r.keys("space", "y")
	r.wait(200 * time.Millisecond)
	r.mustClip(clips, "masked curl of the literal-auth request")
	r.begin("curl revealed")
	clips = len(r.clips)
	r.keys("space", "Y")
	r.wait(200 * time.Millisecond)
	r.mustClip(clips, "revealed curl of the literal-auth request")
	if !strings.Contains(r.clips[len(r.clips)-1].text, r.fx.sec.password) {
		t.Fatal("the revealed curl lacks the credential, so the masked one proves nothing")
	}

	r.begin("websocket auth rows")
	r.open("socket")
	r.keys("g", "a")
	r.must("Token", "websocket auth rows")
	r.snap("closed ws auth rows")
	r.keys("down", "down", "enter")
	r.snap("ws auth normal")
	r.keys("y", "y")
	r.keys("V", "y")
	r.keys("0", "v", "$", "y")
	r.snap("ws auth yank")
	r.keys("space", "o")
	r.snap("ws editor refused")
	r.keys("esc", "esc")

	r.begin("editor files")
	r.open("ech")
	for _, g := range []string{"u", "b", "s", "t", "h"} {
		r.keys("g", g)
		r.keys("space", "o")
		r.wait(300 * time.Millisecond)
		r.snap("editor " + g)
	}
	// $EDITOR on a hidden secret must not open at all; the sweep below also
	// searches every file the fake editor saw, so a regression fails twice.
	for _, e := range r.editors {
		if e.name == "auth rows" || e.name == "websocket auth rows" {
			t.Errorf("$EDITOR opened a file during step %q, on a hidden secret", e.name)
		}
	}
	if len(r.editors) == 0 {
		t.Fatal("the fake $EDITOR never ran, so its files were not swept")
	}
}

// checkUXRig searches everything the rig saw for each secret.
func checkUXRig(t *testing.T, r *uxRig, sec uxSecrets) {
	t.Helper()
	if len(r.clips) < 4 {
		t.Fatalf("only %d clipboard writes were captured; the yank paths were not exercised", len(r.clips))
	}
	search := func(kind, step string, text string) {
		for _, secret := range sec.all() {
			assertAbsent(t, secret, kind+" "+step, []byte(text))
			assertAbsent(t, secret[:12], kind+" "+step+" (first 12 characters)", []byte(text))
		}
	}
	for _, f := range r.frames {
		search("frame", f.name, f.text)
	}
	for _, c := range r.clips {
		if c.name == "curl revealed" {
			continue // Space Y copies the credential on purpose
		}
		search("clipboard", c.name, c.text)
	}
	for _, e := range r.editors {
		search("$EDITOR file", e.name, e.text)
	}
	entries, err := os.ReadDir(r.tmpDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("%d $EDITOR temporary files were left behind", len(entries))
	}
	// Literal credentials typed into auth rows are plain YAML by design
	// (docs/model.md), so they may appear in their own request file and
	// nowhere else: not in history, state, cookies or any other request.
	literals := map[string]string{sec.password: "literal-auth.yaml", sec.wsToken: "socket.yaml"}
	for _, secret := range sec.all() {
		if file, ok := literals[secret]; ok {
			assertOnlyInFile(t, r.fx.root, secret, file)
			continue
		}
		assertWorkspaceFilesClean(t, r.fx.root, secret)
	}
	assertMaskGlyphsAbsent(t, r.fx.root)
}

// assertOnlyInFile fails when secret appears in any file under root other than
// the one whose name ends in allowed.
func assertOnlyInFile(t *testing.T, root, secret, allowed string) {
	t.Helper()
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if bytes.Contains(raw, []byte(secret)) && !strings.HasSuffix(path, allowed) {
			rel, _ := filepath.Rel(root, path)
			t.Errorf("literal credential leaked to workspace file %s", rel)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
