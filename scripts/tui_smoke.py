#!/usr/bin/env python3
"""Drive the real postctl TUI through a pseudo-terminal, offline.

It copies a synthetic workspace, starts the binary with the fake sender
(POSTCTL_FAKE_SENDER=1, so no network), opens a request, presses Ctrl-S and
checks that a response status reached the screen, then walks the vim-style
keys a person uses: the Space Space switcher, editing the URL in insert mode
and Esc, Ctrl-S, `gr`, `Space H` (history tab), `Space x` (diff), `Space j`
with `.` (jq), Ctrl-h and Ctrl-l, and `q`. It prints only pass/fail
lines (one per step), never screen content, and finishes in about 20 seconds.

Usage: python3 scripts/tui_smoke.py [BINARY] [WORKSPACE_DIR] [COLSxROWS]
Defaults: bin/postctl, internal/tui/testdata/demo, 140x40. Build first with
`task build`. Exit status is 0 on success and 1 on failure.
"""
import fcntl
import os
import pty
import re
import select
import shutil
import struct
import sys
import tempfile
import termios
import time

DOWN, ENTER, CTRL_S = b"\x1b[B", b"\r", b"\x13"
ANSI = re.compile(rb"\x1b\[[0-9;?]*[A-Za-z]|\x1b[()][0-9A-B]|\x1b[=>]")


def drain(fd, seconds):
    out, end = b"", time.time() + seconds
    while time.time() < end:
        ready, _, _ = select.select([fd], [], [], 0.1)
        if ready:
            try:
                out += os.read(fd, 65536)
            except OSError:
                break
    return ANSI.sub(b"", out).decode("utf8", "replace")


def wait_for(fd, needle, timeout):
    """Read until needle appears or timeout seconds pass (a fresh binary can
    start slowly on a loaded machine). Returns everything read."""
    seen, end = "", time.time() + timeout
    while needle not in seen and time.time() < end:
        seen += drain(fd, 0.3)
    return seen


def press(fd, *keys):
    """Write each key on its own, a beat apart, as a person types (one write
    of several bytes can be read as a paste)."""
    for key in keys:
        os.write(fd, key)
        time.sleep(0.12)


def exited(pid, seconds):
    """Reports whether the child exits within seconds."""
    end = time.time() + seconds
    while time.time() < end:
        done, _ = os.waitpid(pid, os.WNOHANG)
        if done:
            return True
        time.sleep(0.1)
    return False


def main():
    binary = os.path.abspath(sys.argv[1] if len(sys.argv) > 1 else "bin/postctl")
    source = sys.argv[2] if len(sys.argv) > 2 else "internal/tui/testdata/demo"
    cols, rows = map(int, (sys.argv[3] if len(sys.argv) > 3 else "140x40").split("x"))
    if not os.path.exists(binary):
        print("FAIL binary not found; run `task build` first")
        return 1
    scratch = tempfile.mkdtemp(prefix="postctl-smoke-")
    workspace = os.path.join(scratch, "ws")
    shutil.copytree(source, workspace)  # never point the TUI at the original
    pid, fd = pty.fork()
    if pid == 0:
        env = dict(os.environ, POSTCTL_FAKE_SENDER="1", TERM="xterm-256color", NO_COLOR="1")
        os.execve(binary, [binary, "--workspace", workspace], env)
    fcntl.ioctl(fd, termios.TIOCSWINSZ, struct.pack("HHHH", rows, cols, 0, 0))
    results = []

    def check(name, ok):
        results.append(ok)
        print(("PASS" if ok else "FAIL") + " " + name)

    def step(name, keys, needle, timeout=5, alternatives=()):
        """Presses keys, then passes when needle (or an alternative) shows."""
        press(fd, *keys)
        seen = wait_for(fd, needle, timeout)
        if needle not in seen:
            for alt in alternatives:
                if alt in seen:
                    needle = alt
        check(name, needle in seen)

    try:
        first = wait_for(fd, "postctl", 15)
        check("first frame shows the header", "postctl" in first)
        press(fd, DOWN, DOWN, ENTER)  # select a request and open it
        drain(fd, 0.4)
        step("Ctrl-S rendered a response status", [CTRL_S], "200", 8)
        step("Space shows the which-key popup", [b" "], "Switch environment")
        press(fd, b"\x1b")  # Esc cancels the sequence
        drain(fd, 0.5)
        step("Space Space opens the switcher", [b" ", b" "], "Enter Open")
        press(fd, b"h", b"e", b"a", b"l")
        step("the switcher filters by name", [], "Health")
        step("Enter opens the chosen request", [ENTER], "Request \u00b7 Health")
        step("g u then i enters insert mode in the URL", [b"g", b"u", b"i"], "INSERT")
        press(fd, b"x")
        step("Esc returns to normal mode", [b"\x1b"], "NORMAL")
        step("Ctrl-S sends the edited request", [CTRL_S], "200", 8)
        step("g r focuses the response body", [b"g", b"r"], "Search")
        step("Space H shows the response History tab", [b" ", b"H"], "7/7]")
        press(fd, b"\x1b")
        drain(fd, 0.4)
        step("Space x diffs with the previous send", [b" ", b"x"], "Previous",
             alternatives=("No earlier send",))
        press(fd, b"\x1b")
        drain(fd, 0.4)
        step("Space j opens the jq input", [b" ", b"j"], "Enter Apply")
        step("a jq filter shows its output", [b".", ENTER], "jq output")
        press(fd, b"\x1b")
        drain(fd, 0.4)
        step("Ctrl-h moves to the request pane", [b"\x08"], "Ctrl-S Send")
        step("Ctrl-l moves back to the response pane", [b"\x0c"], "Search")
        press(fd, b"q")
        check("q quits", exited(pid, 5))
        return 0 if all(results) else 1
    finally:
        shutil.rmtree(scratch, ignore_errors=True)
        try:
            os.kill(pid, 9)
        except OSError:
            pass


if __name__ == "__main__":
    sys.exit(main())
