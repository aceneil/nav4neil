#!/usr/bin/env python3
"""M11 e2e smoke: drive bin/nav4neil inside a PTY and assert on-screen text.

Covers: bare launch shows the menu; Enter on each row opens the matching
section (servers / files / all); Esc exits without a TUI; --section / --no-menu
never show the menu; --menu forces it; all mode shows the persistent Tab hint
directly above the status bar and Tab moves the focus.

Note: the harness answers the terminal queries the TUI emits at startup
(OSC 11 background colour + DSR cursor position). Without an answer bubbletea
blocks in its startup handshake and nothing is ever rendered — a real terminal
answers these automatically.
"""
import fcntl
import os
import pty
import re
import select
import signal
import struct
import subprocess
import sys
import termios
import time

BIN = "/home/neil/Documents/Docs/neilwz-nav-tui/bin/nav4neil"

MENU_TITLE = "选择要显示的区域"
HINT = "Tab: 切换 服务器/文件"


def run_pty(args, keys, settle=1.5, gap=0.9, wait=6.0):
    """Run BIN in a pty, send keys while answering terminal queries.

    The capture window always keeps at least 2.5 s of rendering time after the
    last key, otherwise the final frame (the one the checks look at) is cut off.
    """
    pid, fd = pty.fork()
    if pid == 0:  # child
        env = dict(os.environ)
        env["TERM"] = "xterm-256color"
        env["NO_COLOR"] = "1"
        env["NEILWZ_NAV_TUI_WS"] = "0"
        try:
            os.execve(BIN, [BIN] + list(args), env)
        finally:
            os._exit(127)
    # pty.fork() leaves the pty at 0x0 (nothing would render): size it first.
    fcntl.ioctl(fd, termios.TIOCSWINSZ, struct.pack("HHHH", 24, 100, 0, 0))

    out = b""
    pending = list(keys)
    next_key_at = time.time() + settle
    deadline = time.time() + wait
    while time.time() < deadline:
        if pending and time.time() >= next_key_at:
            try:
                os.write(fd, pending.pop(0).encode())
            except OSError:
                break
            next_key_at = time.time() + gap
            # Keep rendering time after the last key so the final frame lands
            # inside the capture.
            deadline = max(deadline, time.time() + 2.5)
        try:
            r, _, _ = select.select([fd], [], [], 0.1)
        except OSError:
            break
        if not r:
            continue
        try:
            chunk = os.read(fd, 65536)
        except OSError:
            break
        if not chunk:
            break
        out += chunk
        # Be a well-behaved terminal: answer the child's startup queries.
        if b"\x1b[6n" in chunk:
            os.write(fd, b"\x1b[1;1R")
        if b"\x1b]11;?" in chunk:
            os.write(fd, b"\x1b]11;rgb:1e1e/1e1e/1e1e\x1b\\")
        if b"\x1b[?1049h" in chunk:
            # A (new) program just took over the alternate screen: give it a
            # moment to finish its startup before typing. Without this the
            # key can land while the tty is still in canonical mode (dropped),
            # which makes the checks flaky.
            next_key_at = max(next_key_at, time.time() + 0.8)

    try:
        os.kill(pid, signal.SIGKILL)
    except ProcessLookupError:
        pass
    try:
        os.waitpid(pid, 0)
    except ChildProcessError:
        pass
    try:
        os.close(fd)
    except OSError:
        pass
    return out.decode("utf-8", errors="replace")


failures = []


def check(name, text, must_include=(), must_exclude=()):
    ok = True
    for item in must_include:
        if item not in text:
            ok = False
            failures.append(f"{name}: missing {item!r}")
    for item in must_exclude:
        if item in text:
            ok = False
            failures.append(f"{name}: unexpectedly contains {item!r}")
    print(f"[{'PASS' if ok else 'FAIL'}] {name}")
    if not ok:
        print("    ---- tail of captured output ----")
        print("    " + text[-1500:].replace("\x1b", "\\e").replace("\r", "\\r").replace("\n", "\n    "))


# A. bare launch → menu, Enter on the default row = servers
out = run_pty([], ["\r", "q"])
check("A bare launch shows the menu, Enter opens servers", out,
      [MENU_TITLE, "▶ servers", "files", "all", "serv4neil", "mode=servers"],
      ["mode=files", "pane="])

# B. bare launch → j moves the highlight to files, Enter opens the files pane
out = run_pty([], ["j", "\r", "q"])
check("B bare launch + j + Enter opens files", out,
      [MENU_TITLE, "mode=files"], ["serv4neil", MENU_TITLE + "\n\n▶ servers"])

# C. bare launch → j j Enter = all (two panes + Tab hint), Tab moves focus
# NB: in the two-pane layout the very top line ("neilwz-servers") is dropped
# by the renderer because the view is one line taller than the terminal
# (pre-existing, also present in the M10 binary), so the pane markers used
# here are the server rows, the divider, the files header and the hint.
out = run_pty([], ["j", "j", "\r", "\t", "q"])
check("C bare launch + all shows the hint and Tab switches focus", out,
      [MENU_TITLE, "herdr  (Agent", "Tab: 切换", "pane=1", "pane=2"], [])
check("C hint row sits directly above the status bar",
      "".join(re.findall(r"Tab: 切换 服务器/文件[^\n]*\n\s*ctx=local pane=", out)) or "NO MATCH",
      ["ctx=local pane="])

# D. bare launch → Esc quits the menu without ever starting the TUI
out = run_pty([], ["\x1b"])
check("D Esc quits the menu without a TUI", out,
      [MENU_TITLE], ["ctx=local", "serv4neil", "herdr  (Agent"])

# E. --section servers (the layout caller) must never see the menu
out = run_pty(["--section", "servers"], ["q"])
check("E --section servers skips the menu", out,
      ["serv4neil", "mode=servers"], [MENU_TITLE])

# F. explicit --section both must not see the menu either, but keeps both panes
out = run_pty(["--section", "both"], ["q"])
check("F --section both skips the menu", out,
      ["herdr  (Agent", "Tab: 切换", "pane=1"], [MENU_TITLE])

# G. --no-menu: no menu, straight into the two-pane layout
out = run_pty(["--no-menu"], ["q"])
check("G --no-menu skips the menu and keeps both panes", out,
      ["herdr  (Agent", "Tab: 切换", "pane=1"], [MENU_TITLE])

# H. --menu forces the menu even when --section is given: the menu's own
# choice (j j → all) must win over --section files, so the two-pane layout
# with the hint appears instead of the files-only sidebar.
out = run_pty(["--menu", "--section", "files"], ["j", "j", "\r", "q"])
check("H --menu forces the menu over --section", out,
      [MENU_TITLE, "Tab: 切换", "pane=1"], ["serv4neil", "mode=files"])

# I. Tab cycles servers → files → servers in all mode
out = run_pty(["--no-menu"], ["\t", "\t", "q"])
check("I Tab cycles servers -> files -> servers", out,
      [HINT, "pane=1", "pane=2"], [])

# J. --list stays headless (no menu, no TUI)
out = run_pty(["--list"], [])
check("J --list prints the list and no menu", out,
      ["count      :", "ssh config:"], [MENU_TITLE, "ctx=local"])

# K. non-TTY stdin keeps the historical behaviour (no menu, no TUI)
p = subprocess.run([BIN], stdin=subprocess.DEVNULL, capture_output=True, text=True, timeout=30)
check("K non-TTY stdin prints the terminal hint", p.stdout + p.stderr,
      ["interactive TUI requires a terminal"], [MENU_TITLE])

# L. --help prints the flag list plus the footer documenting the menu
p = subprocess.run([BIN, "--help"], stdin=subprocess.DEVNULL, capture_output=True, text=True, timeout=30)
check("L help documents --menu/--no-menu", p.stdout + p.stderr,
      ["-menu", "-no-menu", "手动呼出"], [])

print()
if failures:
    print(f"{len(failures)} CHECK(S) FAILED:")
    for f in failures:
        print("  -", f)
    sys.exit(1)
print("all e2e checks passed")
