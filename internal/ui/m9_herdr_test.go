package ui

// M9 tests: the servers list gains a second built-in row — herdr (Agent
// 工作台) — injected right after localhost. Both built-ins are protected
// (not editable, never grouped) and always render a green status square
// without touching the dump-layout poll or the open ledger. Opening herdr
// pops a floating pane running ~/.local/bin/wz-herdr.sh over the current
// Zellij session in both sidebar and standalone modes; the nav never quits
// for herdr.

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/aceneil/nav4neil/internal/servers"
)

// newHerdrEnv builds a SectionServers model whose $HOME contains the herdr
// launcher script, whose PATH carries the fake recording `zellij`, and whose
// data files are pinned to temp dirs. It returns the model, the fake zellij
// log path and the absolute launcher-script path.
func newHerdrEnv(t *testing.T, extraContent, log string) (*Model, string) {
	t.Helper()
	home := t.TempDir()
	scriptDir := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(scriptDir, 0o755); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(scriptDir, "wz-herdr.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	empty := writeSideFile(t, "")
	uiFakeZellijRecorder(t, empty, log)
	m, _ := newServersModel(t, extraContent)
	return m, script
}

// TestHerdr_BuiltinListOrderAndDescription verifies the injected built-ins:
// the merged list always starts localhost → herdr, and herdr carries the
// Agent 工作台 description and Source builtin.
func TestHerdr_BuiltinListOrderAndDescription(t *testing.T) {
	m, _ := newServersModel(t, "db1|root@10.0.0.5:22|prod||\n")
	if len(m.serversView) < 3 || m.serversView[0].Alias != "localhost" ||
		m.serversView[1].Alias != "herdr" || m.serversView[1].Desc != "Agent 工作台" ||
		m.serversView[1].Source != "builtin" {
		t.Fatalf("built-ins must lead the view (localhost → herdr): %+v", m.serversView)
	}
	// herdr sits right behind localhost in the row model too (ops row first).
	if m.srvRows[1].entry.Alias != "localhost" || m.srvRows[2].entry.Alias != "herdr" {
		t.Fatalf("unexpected rows: %+v", m.srvRows)
	}
	// Rendered: herdr shows its own status square + description, green.
	forceColor(t)
	v := m.View()
	herdrRow := ""
	for _, l := range strings.Split(v, "\n") {
		if strings.Contains(l, "herdr") {
			herdrRow = stripANSI(l)
			break
		}
	}
	if herdrRow == "" {
		t.Fatalf("herdr row missing from view:\n%s", v)
	}
	if !strings.Contains(herdrRow, "▮herdr") || !strings.Contains(herdrRow, "(Agent 工作台)") {
		t.Fatalf("herdr row must hug a green square and show its description: %q", herdrRow)
	}
	// Ordering on screen: localhost appears before herdr, herdr before db1.
	if strings.Index(v, "localhost") > strings.Index(v, "herdr") || strings.Index(v, "herdr") > strings.Index(v, "db1") {
		t.Fatalf("screen order must be localhost → herdr → db1:\n%s", v)
	}
}

// TestHerdr_BuiltinNeverGrouped: even with group folders present, the two
// built-ins stay flat above every ▾ folder and their Group is empty.
func TestHerdr_BuiltinNeverGrouped(t *testing.T) {
	seed := "db1|root@10.0.0.5:22||dc|\napp1|root@10.0.0.6:22||dc|\n"
	m, _ := newServersModel(t, seed)
	for i, e := range m.serversView {
		if (e.Alias == "localhost" || e.Alias == "herdr") && e.Group != "" {
			t.Fatalf("built-in row %d must never carry a group: %+v", i, e)
		}
	}
	// No folder may appear before both built-ins in the row list.
	for i, r := range m.srvRows {
		if r.kind == srvRowGroup && i < 3 {
			t.Fatalf("group folder must follow the built-ins: row %d = %+v (rows=%v)", i, r, m.srvRows)
		}
	}
}

// TestHerdr_OpenInsideZellijFloatingArgv: sidebar Enter on herdr issues one
// `zellij action new-pane --floating --close-on-exit --name herdr … -- <script>`
// invocation, keeps the TUI alive, and never opens a tab or types anywhere.
func TestHerdr_OpenInsideZellijFloatingArgv(t *testing.T) {
	t.Setenv("ZELLIJ", "1")
	log := filepath.Join(t.TempDir(), "zellij.log")
	m, script := newHerdrEnv(t, "db1|root@10.0.0.5:22|||\n", log)
	idx := rowIndexOfAlias(m, "herdr")
	if idx < 0 {
		t.Fatalf("herdr row missing: %+v", m.srvRows)
	}
	m.srvCursor = idx
	_, cmd := m.Update(teaKeyMsg("enter"))
	if cmd != nil {
		t.Fatalf("herdr open must keep the TUI running (cmd non-nil)")
	}
	if m.svFailed["herdr"] {
		t.Fatalf("herdr open must not be recorded failed: %+v", m.svFailed)
	}
	if !strings.Contains(m.status, "herdr") {
		t.Fatalf("status must mention herdr, got %q", m.status)
	}
	raw, _ := os.ReadFile(log)
	s := string(raw)
	want := "action new-pane --floating --close-on-exit --name herdr --width 96% --height 96% --x 2% --y 2% -- " + script
	if !strings.Contains(s, want) {
		t.Fatalf("fake zellij must see the floating-pane argv:\n got: %s\nwant substring: %s", s, want)
	}
	for _, banned := range []string{"new-tab", "move-focus", "write", "dump-layout"} {
		if strings.Contains(s, banned) {
			t.Fatalf("herdr open must not %s: %s", banned, s)
		}
	}
}

// TestHerdr_StandaloneOpenKeepsTuiAlive: standalone (SectionBoth) Enter on
// herdr inside Zellij does NOT quit/exec — it floats the workbench over the
// session and keeps the nav running.
func TestHerdr_StandaloneOpenKeepsTuiAlive(t *testing.T) {
	t.Setenv("ZELLIJ", "1")
	log := filepath.Join(t.TempDir(), "zellij.log")
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".local", "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(home, ".local", "bin", "wz-herdr.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	uiFakeZellijRecorder(t, writeSideFile(t, ""), log)
	m := newBothModel(t, "db1|root@10.0.0.5:22|||\n", "/tmp")
	idx := rowIndexOfAlias(m, "herdr")
	if idx < 0 {
		t.Fatalf("herdr row missing: %+v", m.srvRows)
	}
	m.srvCursor = idx
	_, cmd := m.Update(teaKeyMsg("enter"))
	if cmd != nil {
		t.Fatalf("standalone herdr open must NOT quit the TUI (cmd non-nil)")
	}
	if len(m.ExecArgv()) != 0 {
		t.Fatalf("standalone herdr must never prepare an exec argv: %#v", m.ExecArgv())
	}
	raw, _ := os.ReadFile(log)
	if !strings.Contains(string(raw), "action new-pane --floating --close-on-exit --name herdr") {
		t.Fatalf("standalone herdr must still float over Zellij, saw: %s", raw)
	}
}

// TestHerdr_OutsideZellijHintsOnly: no Zellij session → no exec attempt, no
// ledger record, just a status hint (both modes share openHerdr).
func TestHerdr_OutsideZellijHintsOnly(t *testing.T) {
	t.Setenv("ZELLIJ", "")
	uiFakeTool(t, "zellij") // binary exists but no session → still no exec
	m, _ := newServersModel(t, "")
	m.serverCursor = 1 // herdr
	m.openSelectedServer()
	if !strings.Contains(m.status, "not inside Zellij") {
		t.Fatalf("status must explain the missing session, got %q", m.status)
	}
	if m.svOK["herdr"] || m.svFailed["herdr"] {
		t.Fatalf("hint-only path must not touch the ledger: %+v %+v", m.svOK, m.svFailed)
	}
}

// TestHerdr_MissingScriptHintsOnly: the launcher script is part of the nav
// bundle; when it is absent the open degrades to a status hint instead of
// running a broken zellij command.
func TestHerdr_MissingScriptHintsOnly(t *testing.T) {
	t.Setenv("ZELLIJ", "1")
	home := t.TempDir() // no .local/bin/wz-herdr.sh anywhere under $HOME
	t.Setenv("HOME", home)
	m, _ := newServersModel(t, "")
	m.serverCursor = 1 // herdr
	if got := m.openSelectedServer(); got != nil {
		t.Fatalf("missing script must keep the TUI (no quit cmd)")
	}
	if !strings.Contains(m.status, "missing") || !strings.Contains(m.status, "wz-herdr.sh") {
		t.Fatalf("status must name the missing script, got %q", m.status)
	}
	if len(m.ExecArgv()) != 0 {
		t.Fatalf("no exec argv expected: %#v", m.ExecArgv())
	}
}

// TestHerdr_AlwaysGreenRendersGreenSquare: even with an empty tab set and a
// stale failure recorded, the built-in herdr row renders the green square.
func TestHerdr_AlwaysGreenRendersGreenSquare(t *testing.T) {
	forceColor(t)
	herdr := servers.Entry{Alias: "herdr", Source: "builtin"}
	m := mkStatusModel(false, nil, []string{"herdr"}, []string{"herdr"}) // ledger contradiction
	m.width = 30
	m.serversView = []servers.Entry{herdr}
	if st := m.serverState(herdr); st != svGreen {
		t.Fatalf("serverState(herdr) = %v, want svGreen", st)
	}
	row := m.renderServerRow(0)
	if !strings.Contains(row, "\x1b[38;5;78m▮") {
		t.Fatalf("built-in row must render the green square, got %q", stripANSI(row))
	}
	// localhost behaves identically.
	local := servers.Entry{Alias: "localhost", Source: "builtin"}
	m.serversView = []servers.Entry{local}
	if st := m.serverState(local); st != svGreen {
		t.Fatalf("serverState(localhost) = %v, want svGreen", st)
	}
}

// TestHerdr_FormCannotCreateOrEditBuiltinAlias: the NEW form rejects herdr
// as a reserved alias and an armed-EDIT selection of herdr only hints.
func TestHerdr_FormCannotCreateOrEditBuiltinAlias(t *testing.T) {
	m, _ := newServersModel(t, "")
	m.Update(teaKeyMsg("n"))
	m.form.values[fieldName] = "Herdr"
	m.form.values[fieldHost] = "x"
	m.enableForm()
	if m.form == nil || !strings.Contains(m.form.err, "reserved") {
		t.Fatalf("herdr name must be rejected by the NEW form: %+v", m.form)
	}
	m.form = nil
	// EDIT on the herdr row (row 2) in edit mode → hint, never a form.
	m.Update(teaKeyMsg("e")) // cursor row 1 = localhost
	m.Update(teaKeyMsg("j")) // row 2 = herdr
	m.Update(teaKeyMsg("enter"))
	if m.form != nil || !strings.Contains(m.status, "内置项不可编辑") {
		t.Fatalf("EDIT on herdr must be rejected with the built-in hint: form=%v status=%q", m.form, m.status)
	}
}

// TestHerdr_OpenSshEntryStillOpensNewTab guards the untouched neighbour
// path: after herdr was injected, opening a normal server still records the
// plain M8 new-tab argv and keeps the sidebar alive.
func TestHerdr_OpenSshEntryStillOpensNewTab(t *testing.T) {
	t.Setenv("ZELLIJ", "1")
	log := filepath.Join(t.TempDir(), "zellij.log")
	empty := writeSideFile(t, "")
	uiFakeZellijRecorder(t, empty, log)
	m, _ := newServersModel(t, "db1|root@10.0.0.5:22|||\n")
	idx := rowIndexOfAlias(m, "db1")
	m.srvCursor = idx
	_, cmd := m.Update(teaKeyMsg("enter"))
	if cmd != nil {
		t.Fatalf("sidebar ssh open must keep the TUI running")
	}
	raw, _ := os.ReadFile(log)
	s := string(raw)
	if !strings.Contains(s, "action new-tab --name db1 -- ssh root@10.0.0.5") {
		t.Fatalf("ssh open must still be a plain new-tab: %s", s)
	}
	if strings.Contains(s, "herdr") || strings.Contains(s, "floating") {
		t.Fatalf("ssh open must not leak herdr/floating args: %s", s)
	}
}

// TestHerdr_OpenLocalhostStillOpensLocalTab mirrors the above for the other
// built-in: localhost keeps its M8 local-shell-tab behaviour.
func TestHerdr_OpenLocalhostStillOpensLocalTab(t *testing.T) {
	t.Setenv("ZELLIJ", "1")
	log := filepath.Join(t.TempDir(), "zellij.log")
	empty := writeSideFile(t, "")
	uiFakeZellijRecorder(t, empty, log)
	m, _ := newServersModel(t, "")
	if m.serverCursor != 0 || m.serversView[m.serverCursor].Alias != "localhost" {
		t.Fatalf("localhost must be the selected first entry: %+v", m.serversView)
	}
	m.openSelectedServer()
	raw, _ := os.ReadFile(log)
	s := string(raw)
	if !strings.Contains(s, "action new-tab --name local") {
		t.Fatalf("localhost must still open a local tab: %s", s)
	}
	if strings.Contains(s, "floating") {
		t.Fatalf("localhost must not float: %s", s)
	}
}

// TestHerdr_SavePersistsOnlyExtras asserts that a NEW-save round trip keeps
// servers.txt free of the built-ins (they must never be written or removed).
func TestHerdr_SavePersistsOnlyExtras(t *testing.T) {
	m, extraFile := newServersModel(t, "db1|root@10.0.0.5:22|||\n")
	// Build the extra subset exactly like enableForm does.
	var extras []servers.Entry
	for _, e := range m.serversAll {
		if e.Source == "extra" {
			extras = append(extras, e)
		}
	}
	if len(extras) != 1 || extras[0].Alias != "db1" {
		t.Fatalf("unexpected extras subset: %+v", extras)
	}
	if got := reflect.DeepEqual(extras, []servers.Entry{{Alias: "db1", User: "root", Host: "10.0.0.5", Port: 22, Source: "extra"}}); !got {
		t.Fatalf("extras subset has wrong fields: %+v", extras)
	}
	if raw := readExtra(t, extraFile); strings.Contains(raw, "herdr") || strings.Contains(raw, "localhost") {
		t.Fatalf("servers.txt must never store built-ins:\n%s", raw)
	}
}
