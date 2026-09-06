package ui

// M4 regression tests: keyboard-reachable [NEW]/[EDIT] pseudo rows, the
// localhost "focus the right pane" behaviour, and group-folder rendering /
// fold / open.

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

// TestServerList_CursorCyclesThroughNewEditAndServers verifies feature 1:
// ↑/↓ (j/k) can move the cursor into the [NEW] and [EDIT] pseudo rows at the
// top of the server list and wraps around the whole row list.
func TestServerList_CursorCyclesThroughNewEditAndServers(t *testing.T) {
	m, _ := newServersModel(t, "db1|root@10.0.0.5:22|||\n")
	// Rows: [NEW, EDIT, localhost, db1]. The initial cursor sits on the
	// first real server (localhost), matching the historical behaviour.
	if got := m.srvRows[0].kind; got != srvRowNew {
		t.Fatalf("row 0 must be [NEW], got %v", got)
	}
	if got := m.srvRows[1].kind; got != srvRowEdit {
		t.Fatalf("row 1 must be [EDIT], got %v", got)
	}
	if m.srvCursor != 2 || m.serversView[m.serverCursor].Alias != "localhost" {
		t.Fatalf("initial cursor must rest on localhost: srvCursor=%d serverCursor=%d", m.srvCursor, m.serverCursor)
	}

	// k k: localhost → EDIT → NEW.
	m.Update(teaKeyMsg("k"))
	if m.srvCursor != 1 {
		t.Fatalf("k from localhost must land on EDIT, got srvCursor=%d", m.srvCursor)
	}
	m.Update(teaKeyMsg("k"))
	if m.srvCursor != 0 {
		t.Fatalf("k from EDIT must land on NEW, got srvCursor=%d", m.srvCursor)
	}
	// The focused NEW row is highlighted in the rendered view.
	v := m.View()
	lines := strings.Split(v, "\n")
	if !strings.Contains(lines[1], "▶") || !strings.Contains(lines[1], "[NEW]") {
		t.Fatalf("focused NEW row must show the ▶ marker + [NEW]: %q", lines[1])
	}
	if strings.Contains(lines[2], "▶") {
		t.Fatalf("EDIT row must not be focused while NEW is: %q", lines[2])
	}

	// j j: NEW → EDIT → localhost, syncing the selection entry.
	m.Update(teaKeyMsg("j"))
	if m.srvCursor != 1 {
		t.Fatalf("j from NEW must land on EDIT, got %d", m.srvCursor)
	}
	m.Update(teaKeyMsg("j"))
	if m.srvCursor != 2 || m.serverCursor != 0 {
		t.Fatalf("j from EDIT must land on localhost (row 2), got srv=%d sel=%d", m.srvCursor, m.serverCursor)
	}
	// j: onto db1 → selection follows the entry row.
	m.Update(teaKeyMsg("j"))
	if m.srvCursor != 3 || m.serversView[m.serverCursor].Alias != "db1" {
		t.Fatalf("j onto db1 must sync selection: srv=%d sel=%d", m.srvCursor, m.serverCursor)
	}
	// j at the bottom wraps to [NEW] (the cycle closes).
	m.Update(teaKeyMsg("j"))
	if m.srvCursor != 0 {
		t.Fatalf("j from the last row must wrap to NEW, got %d", m.srvCursor)
	}
	// And ↑ from NEW wraps to the bottom row.
	m.Update(teaKeyMsg("up"))
	if m.srvCursor != 3 {
		t.Fatalf("up from NEW must wrap to the last row, got %d", m.srvCursor)
	}
}

// TestServerList_NewEditRowsActivateOnEnter verifies that Enter on the NEW
// pseudo row opens the NEW form and Enter on the EDIT row edits the last
// selected server (the selection is preserved while the cursor crosses ops
// rows, so db1 stays targeted after reaching EDIT via the wrap-around).
func TestServerList_NewEditRowsActivateOnEnter(t *testing.T) {
	m, _ := newServersModel(t, "db1|root@10.0.0.5:22|||\n")

	// Select db1 (row 3) then wrap: j → NEW, j → EDIT. The selection must
	// survive the trip over the pseudo rows.
	m.srvCursor = rowIndexOfAlias(m, "db1")
	m.syncSelectionToRow()
	m.moveSrvCursor(1) // wrap → NEW
	if m.srvCursor != 0 {
		t.Fatalf("expected wrap to NEW, got %d", m.srvCursor)
	}
	m.moveSrvCursor(1) // NEW → EDIT
	if m.serverCursor != 1 || m.serversView[m.serverCursor].Alias != "db1" {
		t.Fatalf("selection must still be db1 on EDIT row: serverCursor=%d", m.serverCursor)
	}

	// Enter on EDIT edits db1.
	m.Update(teaKeyMsg("enter"))
	if m.form == nil || m.form.mode != formEdit || m.form.oldName != "db1" {
		t.Fatalf("Enter on EDIT must edit the selected server: %+v", m.form)
	}
	m.Update(teaKeyMsg("esc"))

	// Move to NEW and Enter opens the blank NEW form.
	m.srvCursor = 0
	m.Update(teaKeyMsg("enter"))
	if m.form == nil || m.form.mode != formNew {
		t.Fatalf("Enter on NEW must open the NEW form: %+v", m.form)
	}
	m.Update(teaKeyMsg("esc"))

	// Mouse: a single click on the NEW/EDIT rows still activates them.
	m.lastClickAt = timeZero()
	m.Update(tea.MouseMsg{Type: tea.MouseLeft, X: 2, Y: 1})
	if m.form == nil || m.form.mode != formNew {
		t.Fatalf("click on NEW row must open the form: %+v", m.form)
	}
	m.Update(teaKeyMsg("esc"))
}

// TestServerList_GroupFoldersRenderFlatFirst checks feature 3 rendering:
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
	// NEW, EDIT, localhost, local2 | ▾ dc/, db1, app1 | ▾ web/, web1
	got := m.srvRows
	kinds := make([]string, 0, len(got))
	for _, r := range got {
		switch r.kind {
		case srvRowNew:
			kinds = append(kinds, "NEW")
		case srvRowEdit:
			kinds = append(kinds, "EDIT")
		case srvRowGroup:
			kinds = append(kinds, "▾"+r.group)
		case srvRowEntry:
			kinds = append(kinds, r.entry.Alias)
		}
	}
	wantKinds := []string{"NEW", "EDIT", "localhost", "local2", "▾dc", "db1", "app1", "▾web", "web1"}
	if strings.Join(kinds, ",") != strings.Join(wantKinds, ",") {
		t.Fatalf("row order wrong:\n got %v\nwant %v", kinds, wantKinds)
	}
	for _, s := range []string{"▾ dc/", "▾ web/", "db1", "app1", "web1", "local2"} {
		if !strings.Contains(v, s) {
			t.Fatalf("rendered view missing %q:\n%s", s, v)
		}
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
	if dcIdx() != 3 { // NEW, EDIT, localhost, then the dc folder
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
// row opens that server exactly like a flat one (new-tab for ssh entries).
func TestServerList_OpenServerInsideGroupConnects(t *testing.T) {
	t.Setenv("ZELLIJ", "1")
	uiFakeTool(t, "zellij")
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
	if m.srvRows[2].kind != srvRowEntry || m.srvRows[2].entry.Alias != "localhost" {
		t.Fatalf("first entry must be builtin localhost: %+v", m.srvRows[2])
	}
	v := m.View()
	if strings.Contains(v, "▸") || strings.Contains(v, "▾") {
		t.Fatalf("flat view must not draw folder arrows:\n%s", v)
	}
}

// TestLocalhost_OpenMovesFocusRightInsideZellij verifies feature 2 inside a
// Zellij session: opening localhost runs `zellij action move-focus right`
// (fake binary records nothing but exits 0) instead of a new tab, and the
// ledger turns green.
func TestLocalhost_OpenMovesFocusRightInsideZellij(t *testing.T) {
	t.Setenv("ZELLIJ", "1")
	uiFakeTool(t, "zellij")
	m, _ := newServersModel(t, "")
	if m.serverCursor != 0 || m.serversView[m.serverCursor].Alias != "localhost" {
		t.Fatalf("localhost must be the selected first entry")
	}
	m.openSelectedServer()
	if !strings.Contains(m.status, "move-focus right") {
		t.Fatalf("status must mention move-focus right, got %q", m.status)
	}
	if !m.svOK["localhost"] {
		t.Fatalf("successful localhost open must be recorded OK")
	}
}

// TestLocalhost_OpenOutsideZellijHintsOnly verifies feature 2 outside Zellij:
// no exec is attempted (empty argv plan) and the status bar explains that the
// local shell pane must be focused manually.
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
