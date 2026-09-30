#!/usr/bin/env python3
"""Drive the real postctl TUI through a pseudo-terminal, offline.

It copies a synthetic workspace, starts the binary with the fake sender
(POSTCTL_FAKE_SENDER=1, so no network), opens a request, presses Ctrl-S and
checks that a response status reached the screen. It prints only pass/fail
lines, never screen content.

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
    try:
        first = drain(fd, 1.5)
        for key in (DOWN, DOWN, ENTER):  # select a request and open it
            os.write(fd, key)
            drain(fd, 0.4)
        os.write(fd, CTRL_S)
        after = drain(fd, 2.0)
        ok_start = "postctl" in first
        ok_send = "200" in after
        print(("PASS" if ok_start else "FAIL") + " first frame shows the header")
        print(("PASS" if ok_send else "FAIL") + " Ctrl-S rendered a response status")
        os.write(fd, b"q")
        drain(fd, 0.5)
        return 0 if ok_start and ok_send else 1
    finally:
        shutil.rmtree(scratch, ignore_errors=True)
        try:
            os.kill(pid, 9)
        except OSError:
            pass


if __name__ == "__main__":
    sys.exit(main())
