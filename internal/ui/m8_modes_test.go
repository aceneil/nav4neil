package ui

// M8 mode-split tests: standalone (SectionBoth, default `nav4neil`) opens
// quit the TUI and hand main() an argv to exec in the current pane (ssh /
// local shell / editor); sidebar instances (--section servers|files) open
// brand-new Zellij tabs and keep the TUI alive. These tests exercise the
// model-level plumbing (Update → prepareExec → ExecArgv) with fake PATH
// tools, plus the editor detection and the exec-prep cleanup function.

import (
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/aceneil/nav4neil/internal/ws"
)

// newBothModel builds a SectionBoth model pinned to temp data files and a
// temp file-browser root, so standalone-mode tests never touch real config.
func newBothModel(t *testing.T, extraContent, browseDir string) *Model {
	t.Helper()
	dir := t.TempDir()
	sshFile := filepath.Join(dir, "sshconfig")
	extraFile := filepath.Join(dir, "wezterm4neil", "servers.txt")
	if extraContent != "" {
		if err := os.MkdirAll(filepath.Dir(extraFile), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(extraFile, []byte(extraContent), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	m := NewModel(browseDir, nil, SectionBoth)
	m.sshPathOverride = sshFile
	m.extraPathOverride = extraFile
	m.reloadServers()
	m.width = 80
	m.height = 24
	return m
}

// fakeToolAbs installs an executable named name in a temp dir and returns
// its absolute path with PATH set to that dir (mirrors action fakeTool).
func fakeToolAbs(t *testing.T, name string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	return p
}

// TestStandalone_OpenSshEntryRequestsQuitAndExec: standalone Enter on an ssh
// entry returns tea.Quit and records the resolved ssh argv (absolute binary,
// port and user@host preserved) for main() to exec.
func TestStandalone_OpenSshEntryRequestsQuitAndExec(t *testing.T) {
	t.Setenv("ZELLIJ", "") // a plain terminal is fine for standalone
	sshAbs := fakeToolAbs(t, "ssh")
	m := newBothModel(t, "db1|root@10.0.0.5:2222|prod||\n", "/tmp")
	idx := rowIndexOfAlias(m, "db1")
	if idx < 0 {
		t.Fatalf("db1 row missing: %+v", m.srvRows)
	}
	m.srvCursor = idx
	_, cmd := m.Update(teaKeyMsg("enter"))
	if cmd == nil {
		t.Fatalf("standalone server open must return a quit command (got nil); status=%q", m.status)
	}
	want := []string{sshAbs, "-p", "2222", "root@10.0.0.5"}
	if !reflect.DeepEqual(m.ExecArgv(), want) {
		t.Fatalf("ExecArgv = %#v, want %#v", m.ExecArgv(), want)
	}
}

// TestStandalone_OpenLocalhostRequestsQuitAndExec: localhost execs the local
// shell resolved to an absolute path (fish here).
func TestStandalone_OpenLocalhostRequestsQuitAndExec(t *testing.T) {
	t.Setenv("ZELLIJ", "")
	fishAbs := fakeToolAbs(t, "fish")
	t.Setenv("SHELL", "/bin/bash") // fish on PATH must win
	m := newBothModel(t, "", "/tmp")
	m.serverCursor = 0 // localhost
	m.syncSelectionToRow()
	m.srvCursor = rowIndexOfAlias(m, "localhost")
	_, cmd := m.Update(teaKeyMsg("enter"))
	if cmd == nil {
		t.Fatalf("standalone localhost open must return a quit command; status=%q", m.status)
	}
	if got := m.ExecArgv(); !reflect.DeepEqual(got, []string{fishAbs}) {
		t.Fatalf("ExecArgv = %#v, want [%q]", got, fishAbs)
	}
}

// TestStandalone_SshPasswordMissingSshpassHints: without sshpass a
// password-protected entry stays in the TUI with an install hint.
func TestStandalone_SshPasswordMissingSshpassHints(t *testing.T) {
	t.Setenv("ZELLIJ", "")
	t.Setenv("PATH", t.TempDir()) // no sshpass, no ssh
	m := newBothModel(t, "db1|root@10.0.0.5:22|pw||s3cr3t\n", "/tmp")
	m.srvCursor = rowIndexOfAlias(m, "db1")
	_, cmd := m.Update(teaKeyMsg("enter"))
	if cmd != nil {
		t.Fatalf("sshpass-missing open must not quit")
	}
	if len(m.ExecArgv()) != 0 {
		t.Fatalf("no exec argv expected: %#v", m.ExecArgv())
	}
	if !strings.Contains(m.status, "sshpass missing") {
		t.Fatalf("status must hint at sshpass, got %q", m.status)
	}
}

// TestStandalone_LocalhostNoShellHints: no fish and no $SHELL leaves the TUI
// up with an explanatory hint instead of exec'ing nothing.
func TestStandalone_LocalhostNoShellHints(t *testing.T) {
	t.Setenv("ZELLIJ", "")
	t.Setenv("PATH", t.TempDir())
	t.Setenv("SHELL", "")
	m := newBothModel(t, "", "/tmp")
	m.serverCursor = 0 // localhost
	m.syncSelectionToRow()
	m.srvCursor = rowIndexOfAlias(m, "localhost")
	_, cmd := m.Update(teaKeyMsg("enter"))
	if cmd != nil {
		t.Fatalf("no-shell localhost must not quit")
	}
	if !strings.Contains(m.status, "no local shell") {
		t.Fatalf("status must explain the missing shell, got %q", m.status)
	}
}

// TestStandalone_FileOpenRequestsQuitAndExec drives the files pane of a
// SectionBoth model: Enter on a file resolves nvim (wz-open detection) and
// records [nvim, path] for the exec hand-off.
func TestStandalone_FileOpenRequestsQuitAndExec(t *testing.T) {
	t.Setenv("ZELLIJ", "")
	nvimAbs := fakeToolAbs(t, "nvim")
	browse := t.TempDir()
	if err := os.WriteFile(filepath.Join(browse, "a.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := newBothModel(t, "", browse)
	m.focus = paneFiles
	idx := -1
	for i, it := range m.fileView {
		if it.Name == "a.txt" {
			idx = i
			break
		}
	}
	if idx < 0 {
		t.Fatalf("a.txt not listed: %+v", m.fileView)
	}
	m.fileCur = idx
	_, cmd := m.Update(teaKeyMsg("enter"))
	if cmd == nil {
		t.Fatalf("standalone file open must return a quit command; status=%q", m.status)
	}
	want := []string{nvimAbs, filepath.Join(browse, "a.txt")}
	if !reflect.DeepEqual(m.ExecArgv(), want) {
		t.Fatalf("ExecArgv = %#v, want %#v", m.ExecArgv(), want)
	}
}

// TestStandalone_FileNoEditorHints: neither nvim nor vim installed → the TUI
// stays up with a status hint.
func TestStandalone_FileNoEditorHints(t *testing.T) {
	t.Setenv("ZELLIJ", "")
	t.Setenv("PATH", t.TempDir())
	browse := t.TempDir()
	if err := os.WriteFile(filepath.Join(browse, "a.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := newBothModel(t, "", browse)
	m.focus = paneFiles
	for i, it := range m.fileView {
		if it.Name == "a.txt" {
			m.fileCur = i
			break
		}
	}
	_, cmd := m.Update(teaKeyMsg("enter"))
	if cmd != nil {
		t.Fatalf("no-editor file open must not quit")
	}
	if !strings.Contains(m.status, "no editor") {
		t.Fatalf("status must explain the missing editor, got %q", m.status)
	}
}

// TestEditorArgv asserts the wz-open-compatible editor detection used for the
// standalone file exec: nvim preferred, vim fallback, absolute argv[0].
func TestEditorArgv(t *testing.T) {
	// Neither editor installed.
	t.Setenv("PATH", t.TempDir())
	if argv := editorArgv("/tmp/x.txt"); argv != nil {
		t.Fatalf("no editors: expected nil, got %#v", argv)
	}

	// Only vim → fallback.
	vimAbs := fakeToolAbs(t, "vim")
	argv := editorArgv("/tmp/x.txt")
	if !reflect.DeepEqual(argv, []string{vimAbs, "/tmp/x.txt"}) {
		t.Fatalf("vim fallback argv = %#v", argv)
	}

	// Both installed → nvim wins.
	nvimAbs := fakeToolAbs(t, "nvim")
	argv = editorArgv("/tmp/x.txt")
	if !reflect.DeepEqual(argv, []string{nvimAbs, "/tmp/x.txt"}) {
		t.Fatalf("nvim preference argv = %#v, want %#v", argv, []string{nvimAbs, "/tmp/x.txt"})
	}
}

// TestPrepareExec_RecordsArgvAndQuits is the unit test for the exec-prep
// cleanup function: it stops the ws server, snapshots argv and requests a
// bubbletea quit. wss is a never-started server (Stop is a cheap no-op) so
// the test stays hermetic — the "mock" simplification of the ws dependency.
func TestPrepareExec_RecordsArgvAndQuits(t *testing.T) {
	m := newBothModel(t, "", "/tmp")
	m.wss = ws.New("local") // constructed but never started

	src := []string{"/usr/bin/ssh", "web01"}
	cmd := m.prepareExec(src)
	if cmd == nil {
		t.Fatalf("prepareExec must return tea.Quit (non-nil cmd)")
	}
	if !reflect.DeepEqual(m.ExecArgv(), src) {
		t.Fatalf("ExecArgv = %#v, want %#v", m.ExecArgv(), src)
	}
	// The recorded argv is a snapshot, not a view over the caller's slice.
	src[0] = "/bin/sh"
	if got := m.ExecArgv(); got[0] != "/usr/bin/ssh" {
		t.Fatalf("ExecArgv must not alias the caller's slice: %#v", got)
	}
	// prepareExec must be idempotent on repeated use (later argv wins).
	m.prepareExec([]string{"/bin/fish"})
	if !reflect.DeepEqual(m.ExecArgv(), []string{"/bin/fish"}) {
		t.Fatalf("second prepareExec must replace argv: %#v", m.ExecArgv())
	}
}

// TestPrepareExec_StopsWsServer proves the ws shutdown really happens inside
// prepareExec (critical because syscall.Exec never unwinds main()'s defers):
// after prepareExec the previously healthy /health endpoint is gone.
func TestPrepareExec_StopsWsServer(t *testing.T) {
	t.Setenv("NEILWZ_NAV_TUI_WS", "0") // ephemeral port
	wss := ws.New("local")
	if err := wss.Start(0); err != nil {
		t.Fatalf("ws.Start: %v", err)
	}
	defer wss.Stop()
	m := newBothModel(t, "", "/tmp")
	m.wss = wss

	health := "http://" + strings.TrimPrefix(wss.URL(), "ws://") + "/health"
	if resp, err := http.Get(health); err != nil || resp.StatusCode != 200 {
		t.Fatalf("server should be healthy before prepareExec: %v %v", resp, err)
	}
	m.prepareExec([]string{"/bin/true"})
	// The listener must be closed by now; a fresh request fails.
	client := http.Client{Timeout: time.Second}
	if resp, err := client.Get(health); err == nil {
		resp.Body.Close()
		t.Fatalf("ws server must be stopped after prepareExec (still reachable: %v)", resp.StatusCode)
	}
}

// TestSidebarEnterKeepsTuiRunning pins the mirror-image contract: a sidebar
// (SectionServers) open returns no quit command — the TUI must stay alive
// after asking Zellij for a new tab.
func TestSidebarEnterKeepsTuiRunning(t *testing.T) {
	t.Setenv("ZELLIJ", "1")
	uiFakeZellijRecorder(t, writeSideFile(t, ""), filepath.Join(t.TempDir(), "zellij.log"))
	m, _ := newServersModel(t, "db1|root@10.0.0.5:22|||\n")
	m.srvCursor = rowIndexOfAlias(m, "db1")
	_, cmd := m.Update(teaKeyMsg("enter"))
	if cmd != nil {
		t.Fatalf("sidebar open must keep the TUI running (cmd non-nil)")
	}
	if m.svFailed["db1"] || !m.svOK["db1"] {
		t.Fatalf("sidebar open must be recorded OK, got failed=%v ok=%v", m.svFailed["db1"], m.svOK["db1"])
	}
}

// TestStandaloneDoubleClickServerQuits mirrors the mouse path: a double-click
// on a server row in standalone mode also yields the exec cmd.
func TestStandaloneDoubleClickServerQuits(t *testing.T) {
	t.Setenv("ZELLIJ", "")
	fakeToolAbs(t, "ssh")
	m := newBothModel(t, "db1|root@10.0.0.5:22|||\n", "/tmp")
	db1 := rowIndexOfAlias(m, "db1")
	// SectionBoth layout: row 0 = server header; server rows start at y=1.
	m.lastClickAt = timeZero()
	m.Update(tea.MouseMsg{Type: tea.MouseLeft, X: 3, Y: 1 + db1})
	_, cmd := m.Update(tea.MouseMsg{Type: tea.MouseLeft, X: 3, Y: 1 + db1}) // double-click
	if cmd == nil {
		t.Fatalf("standalone double-click must return a quit command; status=%q", m.status)
	}
	if len(m.ExecArgv()) == 0 || m.ExecArgv()[1] != "root@10.0.0.5" {
		t.Fatalf("unexpected ExecArgv: %#v", m.ExecArgv())
	}
}
