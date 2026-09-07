package ui

// M4/M5/M6 row-model regression tests: keyboard-reachable [NEW]/[EDIT] ops
// (now one combined row under the title), the localhost right-pane
// behaviour, group-folder rendering / fold / open, the server-row column
// order (pointer → status square → name) and opening servers in brand-new
// panes of the right main area (M6) instead of opening full-screen tabs or
// typing into existing panes.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// uiFakeTool installs an executable named name in a temp dir, prepends that
// dir to PATH and returns it, so exec.LookPath finds the tool. zellij is the
// tool the UI needs when it believes it is inside a Zellij session.
func uiFakeTool(t *testing.T, name string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
}

// uiFakeZellijRecorder installs a `zellij` that appends "$@" to log and then
// prints outFile (may be empty) to stdout using shell builtins only (the
// test PATH contains no external tools) — used to verify the exact actions
// the TUI issues while opening a server and to fake list-panes JSON.
func uiFakeZellijRecorder(t *testing.T, outFile, log string) {
	t.Helper()
	dir := t.TempDir()
	body := "#!/bin/sh\n" +
		"echo \"$@\" >> " + log + "\n" +
		"while IFS= read -r line; do printf '%s\\n' \"$line\"; done < " + outFile + "\n" +
		"exit 0\n"
	if err := os.WriteFile(filepath.Join(dir, "zellij"), []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
}

// writeSideFile writes content into a temp file and returns its path. A
// trailing newline is appended when missing: the fake `zellij` replays the
// file through `read`, and dash drops an unterminated final line.
func writeSideFile(t *testing.T, content string) string {
	t.Helper()
	if !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	p := filepath.Join(t.TempDir(), "out")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// rowIndexOfAlias returns the srvRows index whose entry alias matches want,
// or -1 (test helper).
func rowIndexOfAlias(m *Model, want string) int {
	for i, r := range m.srvRows {
		if r.kind == srvRowEntry && r.entry.Alias == want {
			return i
		}
	}
	return -1
}

// rowIndexOfKind returns the srvRows index of the first row of kind k.
func rowIndexOfKind(m *Model, k srvRowKind) int {
	for i, r := range m.srvRows {
		if r.kind == k {
			return i
		}
	}
	return -1
}

// TestServerList_OpsRowSingleLineAndArmedNavigation verifies feature 2:
// [NEW] and [EDIT] live side by side on ONE line under the title, they are
// still keyboard-reachable (↑/↓ reach the ops row, ←/→ or h/l arm one of
// them, Enter triggers the armed op) and mouse clicks hit each half.
func TestServerList_OpsRowSingleLineAndArmedNavigation(t *testing.T) {
	m, _ := newServersModel(t, "db1|root@10.0.0.5:22|||\n")
	// Rows: [ops, localhost, db1]. The initial cursor sits on the first
	// real server (localhost), matching the historical behaviour.
	if got := m.srvRows[0].kind; got != srvRowOps {
		t.Fatalf("row 0 must be the ops row, got %v", got)
	}
	if len(m.srvRows) != 3 || m.srvRows[1].entry.Alias != "localhost" {
		t.Fatalf("unexpected rows: %+v", m.srvRows)
	}
	if m.srvCursor != 1 || m.serversView[m.serverCursor].Alias != "localhost" {
		t.Fatalf("initial cursor must rest on localhost: srvCursor=%d serverCursor=%d", m.srvCursor, m.serverCursor)
	}

	// Rendered: title row, then ONE ops line carrying both labels.
	lines := strings.Split(m.View(), "\n")
	if strings.TrimSpace(lines[0]) != "serv4neil" {
		t.Fatalf("row 0 must be the title, got %q", lines[0])
	}
	if !strings.Contains(lines[1], "[NEW]") || !strings.Contains(lines[1], "[EDIT]") {
		t.Fatalf("ops line must carry both [NEW] and [EDIT]: %q", lines[1])
	}
	if strings.Contains(lines[2], "[NEW]") || strings.Contains(lines[2], "[EDIT]") {
		t.Fatalf("[NEW]/[EDIT] must not wrap onto row 2: %q", lines[2])
	}
	// Servers start right below the ops line.
	if !strings.Contains(lines[2], "localhost") {
		t.Fatalf("servers must start on row 2: %q", lines[2])
	}

	// k from localhost → ops row with [EDIT] armed (old muscle memory:
	// one step up from the first server used to land on EDIT).
	m.Update(teaKeyMsg("k"))
	if m.srvCursor != 0 || m.opsSel != opEdit {
		t.Fatalf("k must land on the ops row arming EDIT: srv=%d op=%v", m.srvCursor, m.opsSel)
	}
	// ←/h arms [NEW], →/l arms [EDIT] back.
	m.Update(teaKeyMsg("h"))
	if m.opsSel != opNew {
		t.Fatalf("h must arm NEW, got %v", m.opsSel)
	}
	m.Update(teaKeyMsg("right"))
	if m.opsSel != opEdit {
		t.Fatalf("right must arm EDIT, got %v", m.opsSel)
	}
	m.Update(teaKeyMsg("left"))
	if m.opsSel != opNew {
		t.Fatalf("left must arm NEW, got %v", m.opsSel)
	}
	// The focused ops line paints the pointer before the armed op.
	v := m.View()
	ol := strings.Split(v, "\n")[1]
	if !strings.Contains(ol, "▶ [NEW]") || !strings.Contains(ol, "[EDIT]") {
		t.Fatalf("ops line must show ▶ before armed NEW: %q", ol)
	}

	// Enter with NEW armed opens the NEW form.
	m.Update(teaKeyMsg("enter"))
	if m.form == nil || m.form.mode != formNew {
		t.Fatalf("Enter on armed NEW must open the form: %+v", m.form)
	}
	m.Update(teaKeyMsg("esc"))

	// Arm EDIT and Enter edits the last selected server (selection survives
	// the trip over the ops row: db1 was the selected entry).
	m.serverCursor = 1 // serversView index of db1
	m.syncSelectionToRow()
	m.srvCursor = 0
	m.opsSel = opEdit
	m.Update(teaKeyMsg("enter"))
	if m.form == nil || m.form.mode != formEdit || m.form.oldName != "db1" {
		t.Fatalf("Enter on armed EDIT must edit the selected server: %+v", m.form)
	}
	m.Update(teaKeyMsg("esc"))

	// j from the ops row moves back onto localhost.
	m.srvCursor = 0
	m.Update(teaKeyMsg("j"))
	if m.srvCursor != 1 {
		t.Fatalf("j from ops must land on localhost, got %d", m.srvCursor)
	}
}

// TestServerList_OpsRowMouseClickZones: the ops line is one row; clicking
// the [NEW] half (x < 8) opens NEW, clicking the [EDIT] half enters M6 edit
// mode.
func TestServerList_OpsRowMouseClickZones(t *testing.T) {
	m, _ := newServersModel(t, "db1|root@10.0.0.5:22|||\n")
	m.lastClickAt = timeZero()

	// Click [NEW] on the ops line (y=1), left half.
	m.Update(tea.MouseMsg{Type: tea.MouseLeft, X: 2, Y: 1})
	if m.form == nil || m.form.mode != formNew {
		t.Fatalf("click [NEW] half must open the new form: %+v", m.form)
	}
	m.Update(teaKeyMsg("esc"))
	m.lastClickAt = timeZero()

	// Click [EDIT] on the ops line, right half (y=1, x ≥ 8): M6 enters edit
	// mode and the cursor jumps to the first server instead of opening a form.
	m.Update(tea.MouseMsg{Type: tea.MouseLeft, X: 10, Y: 1})
	if m.form != nil || !m.editMode {
		t.Fatalf("click [EDIT] half must enter edit mode: form=%v edit=%v", m.form != nil, m.editMode)
	}
	if m.srvCursor != 1 || m.srvRows[m.srvCursor].entry.Alias != "localhost" {
		t.Fatalf("edit mode entry must land on the first server: srv=%d", m.srvCursor)
	}
	m.Update(teaKeyMsg("esc"))
	m.lastClickAt = timeZero()

	// Clicking a server row: y=2 is now the first list row (localhost).
	m.Update(tea.MouseMsg{Type: tea.MouseLeft, X: 3, Y: 2})
	if m.srvCursor != 1 || m.serversView[m.serverCursor].Alias != "localhost" {
		t.Fatalf("y=2 must select list row 0 (localhost): srv=%d sel=%d", m.srvCursor, m.serverCursor)
	}
}

// TestServerList_EntryRowColumnOrder pins feature 1: each server line reads
// pointer → status square → name (square hugs the name).
func TestServerList_EntryRowColumnOrder(t *testing.T) {
	forceColor(t)
	m, _ := newServersModel(t, "db1|root@10.0.0.5:22|prod db||\n")
	lines := strings.Split(m.View(), "\n")
	// lines[2] is the first server row (localhost), focused initially.
	row := stripANSI(lines[2])
	if !strings.HasPrefix(row, "▶ ▮localhost") {
		t.Fatalf("focused entry row must start with pointer, space, square, name: %q", row)
	}
	// The status square must sit immediately before the name text.
	if !strings.Contains(row, "▮localhost") {
		t.Fatalf("square must hug the name (no gap): %q", row)
	}
	// db1 row (unfocused) keeps its square column aligned.
	dbLine := ""
	for _, l := range lines {
		if strings.Contains(l, "db1") {
			dbLine = stripANSI(l)
			break
		}
	}
	if !strings.HasPrefix(dbLine, "  ▮db1") {
		t.Fatalf("unfocused entry row must align pointer-blank + square + name: %q", dbLine)
	}
	// Description trails the name on the same row.
	if !strings.Contains(dbLine, "db1  (prod db)") {
		t.Fatalf("description must follow the alias: %q", dbLine)
	}
}

// TestServerList_GroupFoldersRenderFlatFirst checks group rendering:
// ungrouped servers (localhost first) sit at the top, then one ▾ folder per
// group holds its servers; Group-empty entries stay flat.
func TestServerList_GroupFoldersRenderFlatFirst(t *testing.T) {
	seed := "db1|root@10.0.0.5:22|prod db|dc|s3\n" +
		"app1|root@10.0.0.6:22||dc|\n" +
		"web1|root@10.0.0.7:22||web|\n" +
		"local2|root@10.0.0.8:22|||\n"
	m, _ := newServersModel(t, seed)
	v := m.View()

	// Expected row shape (SectionServers):
	// ops, localhost, local2 | ▾ dc/, db1, app1 | ▾ web/, web1
	got := m.srvRows
	kinds := make([]string, 0, len(got))
	for _, r := range got {
		switch r.kind {
		case srvRowOps:
			kinds = append(kinds, "OPS")
		case srvRowGroup:
			kinds = append(kinds, "▾"+r.group)
		case srvRowEntry:
			kinds = append(kinds, r.entry.Alias)
		}
	}
	wantKinds := []string{"OPS", "localhost", "local2", "▾dc", "db1", "app1", "▾web", "web1"}
	if strings.Join(kinds, ",") != strings.Join(wantKinds, ",") {
		t.Fatalf("row order wrong:\n got %v\nwant %v", kinds, wantKinds)
	}
	for _, s := range []string{"▾ dc/", "▾ web/", "db1", "app1", "web1", "local2"} {
		if !strings.Contains(v, s) {
			t.Fatalf("rendered view missing %q:\n%s", s, v)
		}
	}
	// Group labels line up under their child names: the folder render keeps
	// the square column blank and draws ▾ where children indent.
	folder := ""
	for _, l := range strings.Split(v, "\n") {
		if strings.Contains(l, "▾ dc/") {
			folder = stripANSI(l)
			break
		}
	}
	if !strings.HasPrefix(folder, "   ▾ dc/") {
		t.Fatalf("folder row must omit the square and align under children: %q", folder)
	}
	// Flat entries appear before any folder; localhost is the first entry.
	if i := strings.Index(v, "localhost"); i < 0 || strings.Index(v, "▾ dc/") < i {
		t.Fatalf("localhost must render before group folders:\n%s", v)
	}
}

// TestServerList_GroupFolderCollapseExpandAndDoubleClick verifies Enter on a
// folder folds/unfolds it (▾→▸ hides children, ▸→▾ restores them) and that
// double-clicking the folder does the same.
func TestServerList_GroupFolderCollapseExpandAndDoubleClick(t *testing.T) {
	seed := "db1|root@10.0.0.5:22||dc|\napp1|root@10.0.0.6:22||dc|\n"
	m, _ := newServersModel(t, seed)
	dcIdx := func() int {
		for i, r := range m.srvRows {
			if r.kind == srvRowGroup && r.group == "dc" {
				return i
			}
		}
		return -1
	}
	if dcIdx() != 2 { // ops, localhost, then the dc folder
		t.Fatalf("unexpected dc folder index %d (rows=%v)", dcIdx(), m.srvRows)
	}

	// Enter on the folder collapses it.
	m.srvCursor = dcIdx()
	m.Update(teaKeyMsg("enter"))
	if !m.collapsed["dc"] {
		t.Fatalf("Enter on folder must collapse it")
	}
	v := m.View()
	if !strings.Contains(v, "▸ dc/") {
		t.Fatalf("collapsed folder must render as ▸ dc/:\n%s", v)
	}
	if strings.Contains(v, "db1") || strings.Contains(v, "app1") {
		t.Fatalf("collapsed group children must be hidden:\n%s", v)
	}
	if m.srvCursor != dcIdx() {
		t.Fatalf("cursor must stay on the folder row after collapse, got %d", m.srvCursor)
	}

	// Enter again expands and children come back.
	m.Update(teaKeyMsg("enter"))
	if m.collapsed["dc"] {
		t.Fatalf("second Enter must expand the folder")
	}
	if !strings.Contains(m.View(), "▾ dc/") || !strings.Contains(m.View(), "db1") {
		t.Fatalf("expanded folder must show ▾ dc/ and children:\n%s", m.View())
	}

	// Double-click on the folder row toggles it closed again.
	folderY := dcIdx() + 1 // SectionServers: row i renders on screen y=i+1
	m.lastClickAt = timeZero()
	m.Update(tea.MouseMsg{Type: tea.MouseLeft, X: 2, Y: folderY})
	m.Update(tea.MouseMsg{Type: tea.MouseLeft, X: 2, Y: folderY}) // double-click
	if !m.collapsed["dc"] {
		t.Fatalf("double-click must fold the group")
	}
}

// TestServerList_OpenServerInsideGroupConnects tests that Enter on a child
// row opens a new pane running ssh (recorded through a fake zellij).
func TestServerList_OpenServerInsideGroupConnects(t *testing.T) {
	t.Setenv("ZELLIJ", "1")
	log := filepath.Join(t.TempDir(), "zellij.log")
	empty := writeSideFile(t, "") // never queried in M6
	uiFakeZellijRecorder(t, empty, log)
	m, _ := newServersModel(t, "db1|root@10.0.0.5:22|prod|dc|\napp1|root@10.0.0.6:22||dc|\n")

	idx := rowIndexOfAlias(m, "db1")
	if idx < 0 {
		t.Fatalf("db1 child row missing: %+v", m.srvRows)
	}
	m.srvCursor = idx
	m.Update(teaKeyMsg("enter"))
	if m.svFailed["db1"] {
		t.Fatalf("open must not be recorded as failed: %+v", m.svFailed)
	}
	if !m.svOK["db1"] {
		t.Fatalf("open must be recorded as OK")
	}
	if !strings.Contains(m.status, "db1") {
		t.Fatalf("status must confirm the opened server, got %q", m.status)
	}
	raw, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	// M7: no full-screen new tab, no typing into an existing pane — the ssh
	// session starts in its own brand-new pane after consulting the live
	// layout (dump-layout) and moving focus right. The fake dump is empty,
	// so the plan legitimately falls back to the legacy M6 steps.
	for _, banned := range []string{"new-tab", "write", "list-panes"} {
		if strings.Contains(s, banned) {
			t.Fatalf("must not %s, fake zellij saw: %s", banned, s)
		}
	}
	if !strings.Contains(s, "action dump-layout") {
		t.Fatalf("M7 must consult the live layout via dump-layout, got: %s", s)
	}
	for _, want := range []string{"action move-focus right", "action new-pane -- ssh root@10.0.0.5"} {
		if !strings.Contains(s, want) {
			t.Fatalf("fake zellij must have seen %q, got: %s", want, s)
		}
	}
}

// TestServerList_OpenServerNewPaneCarriesPortAndUser verifies the argv
// reaching `zellij action new-pane --` reuses the ssh/sshpass logic (port and
// user@host are explicit argv elements, no pane resolution runs).
func TestServerList_OpenServerNewPaneCarriesPortAndUser(t *testing.T) {
	t.Setenv("ZELLIJ", "1")
	log := filepath.Join(t.TempDir(), "zellij.log")
	// The recorder would answer list-panes with the real sidebar fixture if
	// the TUI still asked for a pane id — it must not in M6.
	paneJSON := writeSideFile(t, `[
  {"id":0,"is_plugin":false,"title":"nav4neil-servers","pane_command":"nav4neil --section servers","pane_x":0,"pane_rows":14,"tab_id":0},
  {"id":1,"is_plugin":false,"title":"💻 终端","pane_command":"fish","pane_x":28,"pane_rows":28,"tab_id":0}
]`)
	uiFakeZellijRecorder(t, paneJSON, log)
	m, _ := newServersModel(t, "db1|root@10.0.0.5:2222|prod||\n")
	idx := rowIndexOfAlias(m, "db1")
	m.srvCursor = idx
	m.Update(teaKeyMsg("enter"))
	if m.svFailed["db1"] {
		t.Fatalf("open must not fail: %+v", m.svFailed)
	}
	if !strings.Contains(m.status, "new pane, right area") {
		t.Fatalf("status must describe the new-pane open, got %q", m.status)
	}
	raw, _ := os.ReadFile(log)
	s := string(raw)
	if strings.Contains(s, "move-focus") && !strings.Contains(s, "new-pane") {
		t.Fatalf("focus move without a new pane is the old M5 flow: %s", s)
	}
	if !strings.Contains(s, "new-pane -- ssh -p 2222 root@10.0.0.5") {
		t.Fatalf("new-pane argv must carry port and user@host, got: %s", s)
	}
	if strings.Contains(s, "list-panes") || strings.Contains(s, "write") {
		t.Fatalf("M6 must not resolve a pane id or type into one: %s", s)
	}
}

// TestServerList_NoGroupsFlatLayout verifies Group-empty rows render as a
// plain flat list with no folder rows and no ▾/▸ arrows.
func TestServerList_NoGroupsFlatLayout(t *testing.T) {
	m, _ := newServersModel(t, "box|root@h:22|||\nweb|root@w:22|||\n")
	for _, r := range m.srvRows {
		if r.kind == srvRowGroup {
			t.Fatalf("no group rows expected, got %+v", r)
		}
	}
	if m.srvRows[1].kind != srvRowEntry || m.srvRows[1].entry.Alias != "localhost" {
		t.Fatalf("first entry must be builtin localhost: %+v", m.srvRows)
	}
	v := m.View()
	if strings.Contains(v, "▸") || strings.Contains(v, "▾") {
		t.Fatalf("flat view must not draw folder arrows:\n%s", v)
	}
}

// TestLocalhost_OpenInsideZellijOpensLocalShellPane verifies the M6 localhost
// behaviour inside a Zellij session: opening localhost runs move-focus right
// + `zellij action new-pane` (fake binary records the args), and the ledger
// turns green. SHELL is pinned empty so the shell resolution is deterministic
// (fish is absent from the fake PATH → plan falls back to plain new-pane).
func TestLocalhost_OpenInsideZellijOpensLocalShellPane(t *testing.T) {
	t.Setenv("ZELLIJ", "1")
	t.Setenv("SHELL", "")
	log := filepath.Join(t.TempDir(), "zellij.log")
	empty := writeSideFile(t, "")
	uiFakeZellijRecorder(t, empty, log)
	m, _ := newServersModel(t, "")
	if m.serverCursor != 0 || m.serversView[m.serverCursor].Alias != "localhost" {
		t.Fatalf("localhost must be the selected first entry")
	}
	m.openSelectedServer()
	if !strings.Contains(m.status, "local") {
		t.Fatalf("status must confirm the local shell pane, got %q", m.status)
	}
	if !m.svOK["localhost"] {
		t.Fatalf("successful localhost open must be recorded OK")
	}
	raw, _ := os.ReadFile(log)
	s := string(raw)
	for _, want := range []string{"action move-focus right", "action new-pane"} {
		if !strings.Contains(s, want) {
			t.Fatalf("fake zellij must have seen %q, got: %s", want, s)
		}
	}
	if strings.Contains(s, "new-tab") || strings.Contains(s, "write") {
		t.Fatalf("localhost must never open a tab or type into a pane: %s", s)
	}
}

// TestLocalhost_OpenOutsideZellijHintsOnly verifies localhost outside Zellij:
// no exec is attempted (hint-only plan) and the status bar explains that the
// local shell pane must be opened from inside Zellij.
func TestLocalhost_OpenOutsideZellijHintsOnly(t *testing.T) {
	t.Setenv("ZELLIJ", "")
	uiFakeTool(t, "zellij") // binary exists but no session → still no exec
	m, _ := newServersModel(t, "")
	m.openSelectedServer()
	if !strings.Contains(m.status, "not inside Zellij") {
		t.Fatalf("status must explain the missing Zellij session, got %q", m.status)
	}
	if m.svFailed["localhost"] {
		t.Fatalf("hint-only path must not record a failure")
	}
}

// TestSshEntry_OpenOutsideZellijHintsOnly mirrors localhost for ssh rows: no
// new tab, no writes — just a status hint.
func TestSshEntry_OpenOutsideZellijHintsOnly(t *testing.T) {
	t.Setenv("ZELLIJ", "")
	uiFakeTool(t, "zellij")
	m, _ := newServersModel(t, "db1|root@10.0.0.5:22|||\n")
	m.serverCursor = 1
	m.openSelectedServer()
	if !strings.Contains(m.status, "not inside Zellij") {
		t.Fatalf("status must hint outside-zellij, got %q", m.status)
	}
	if m.svOK["db1"] || m.svFailed["db1"] {
		t.Fatalf("hint-only path must not touch the ledger")
	}
}
