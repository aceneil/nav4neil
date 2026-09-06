package ui

// M6 tests: servers open in brand-new panes in the right main area (nothing
// is typed into an existing pane), and the servers list gains an edit mode
// whose affordances are the angle-bracket ops line (<NEW> <EDIT>), a green
// cursor and Enter/→ editing semantics for server and folder rows.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// opsLineOf returns the rendered ops row (index 1 under the title) for a
// SectionServers view.
func opsLineOf(t *testing.T, m *Model) string {
	t.Helper()
	lines := strings.Split(m.View(), "\n")
	if len(lines) < 2 {
		t.Fatalf("view too short:\n%s", m.View())
	}
	return lines[1]
}

// TestEditMode_EnterExitBracketsAndCursorJumps: pressing e toggles edit mode
// (ops line shows <NEW> <EDIT>, cursor jumps to the first server); Esc (or e
// again) restores the plain brackets and normal semantics.
func TestEditMode_EnterExitBracketsAndCursorJumps(t *testing.T) {
	m, _ := newServersModel(t, "db1|root@10.0.0.5:22|||\n")
	// Normal mode: square brackets on the ops line.
	op := opsLineOf(t, m)
	if !strings.Contains(op, "[NEW]") || !strings.Contains(op, "[EDIT]") {
		t.Fatalf("normal ops line must use square brackets: %q", op)
	}
	if strings.Contains(op, "<NEW>") || strings.Contains(op, "<EDIT>") {
		t.Fatalf("normal ops line must not use angle brackets: %q", op)
	}
	if m.editMode {
		t.Fatalf("edit mode must start disabled")
	}

	// e → edit mode, cursor lands on the first server (localhost, row 1).
	m.Update(teaKeyMsg("e"))
	if !m.editMode {
		t.Fatalf("e must enter edit mode, status=%q", m.status)
	}
	if m.srvCursor != 1 || m.srvRows[m.srvCursor].entry.Alias != "localhost" {
		t.Fatalf("edit-mode entry must jump the cursor to the first server: row=%d %+v", m.srvCursor, m.srvRows[m.srvCursor])
	}
	op = opsLineOf(t, m)
	if !strings.Contains(op, "<NEW>") || !strings.Contains(op, "<EDIT>") {
		t.Fatalf("edit-mode ops line must use angle brackets: %q", op)
	}
	if strings.Contains(op, "[NEW]") || strings.Contains(op, "[EDIT]") {
		t.Fatalf("edit-mode ops line must not use square brackets: %q", op)
	}

	// Esc leaves edit mode and restores square brackets without moving rows.
	row := m.srvCursor
	m.Update(teaKeyMsg("esc"))
	if m.editMode {
		t.Fatalf("esc must exit edit mode")
	}
	if m.srvCursor != row {
		t.Fatalf("exiting edit mode must not move the cursor: %d → %d", row, m.srvCursor)
	}
	op = opsLineOf(t, m)
	if !strings.Contains(op, "[NEW]") || !strings.Contains(op, "[EDIT]") {
		t.Fatalf("after esc the ops line must be square again: %q", op)
	}

	// e again re-enters (toggle); Esc in normal mode is a no-op.
	m.Update(teaKeyMsg("e"))
	if !m.editMode {
		t.Fatalf("e must toggle back into edit mode")
	}
	m.Update(teaKeyMsg("esc"))
	m.Update(teaKeyMsg("esc")) // second Esc while normal → still normal
	if m.editMode {
		t.Fatalf("esc outside edit mode must be a no-op")
	}
}

// TestEditMode_GreenCursorAndColorAbstraction: while edit mode is active the
// focused pointer is painted with the editPtr256 colour (asserted both on the
// raw view and through the pointerCell abstraction); normal mode keeps the
// plain glyph.
func TestEditMode_GreenCursorAndColorAbstraction(t *testing.T) {
	forceColor(t)
	// Unit-level abstraction: the colour lives behind pointerCell so views
	// can be asserted without parsing every escape.
	if got := pointerCell("▶", false); got != "▶" {
		t.Fatalf("pointerCell without edit mode must be the plain glyph, got %q", got)
	}
	if got := pointerCell("▶", true); !strings.Contains(got, "\x1b[38;5;46m▶") || !strings.Contains(got, "\x1b[0m") {
		t.Fatalf("pointerCell in edit mode must wrap the glyph in green 256 colour: %q", got)
	}

	m, _ := newServersModel(t, "db1|root@10.0.0.5:22|||\n")
	// Normal mode: focused server row has an uncoloured ▶.
	if !strings.Contains(m.View(), "▶") {
		t.Fatalf("normal mode must show a plain pointer:\n%s", m.View())
	}
	if strings.Contains(m.View(), "\x1b[38;5;46m") {
		t.Fatalf("normal mode must not paint the pointer green:\n%s", m.View())
	}

	m.Update(teaKeyMsg("e")) // cursor now on localhost (row 1)
	v := m.View()
	if !strings.Contains(v, "\x1b[38;5;46m▶") {
		t.Fatalf("edit mode must paint the focused pointer green:\n%s", v)
	}
	// The green escape sits in the pointer cell only; the visible text keeps
	// the pointer → square → name column order.
	row := ""
	for _, l := range strings.Split(v, "\n") {
		if strings.Contains(l, "localhost") {
			row = l
			break
		}
	}
	if !strings.HasPrefix(stripANSI(row), "▶ ▮localhost") {
		t.Fatalf("coloured pointer must not disturb the row layout: %q", stripANSI(row))
	}
}

// TestEditMode_RightAndEnterOnServerOpenEditForm: in edit mode both → and
// Enter open the EDIT server form for the row under the cursor.
func TestEditMode_RightAndEnterOnServerOpenEditForm(t *testing.T) {
	m, _ := newServersModel(t, "db1|root@10.0.0.5:2222|prod||\n")
	m.Update(teaKeyMsg("e")) // cursor row 1 (localhost)
	m.Update(teaKeyMsg("j")) // row 2 (db1)
	m.Update(teaKeyMsg("right"))
	if m.form == nil || m.form.mode != formEdit || m.form.oldName != "db1" {
		t.Fatalf("right in edit mode must open the EDIT form for db1: %+v", m.form)
	}
	if got := m.form.values[fieldPort]; got != "2222" {
		t.Fatalf("EDIT form must prefill the port: %q", got)
	}
	m.Update(teaKeyMsg("esc"))
	if m.form != nil || !m.editMode {
		t.Fatalf("cancelling the form must return to edit mode: form=%v edit=%v", m.form != nil, m.editMode)
	}

	// Enter on a server row does the same thing.
	m.Update(teaKeyMsg("enter"))
	if m.form == nil || m.form.mode != formEdit || m.form.oldName != "db1" {
		t.Fatalf("enter in edit mode must open the EDIT form for db1: %+v", m.form)
	}
}

// TestEditMode_FolderEnterTogglesRightRenames: in edit mode Enter on a folder
// row folds/unfolds it (never edits), while →/l opens the RENAME GROUP
// overlay prefilled with the folder label.
func TestEditMode_FolderEnterTogglesRightRenames(t *testing.T) {
	seed := "db1|root@10.0.0.5:22||dc|\napp1|root@10.0.0.6:22||dc|\n"
	m, extraFile := newServersModel(t, seed)
	dc := m.findRowIdentity(rowIdentity{kind: srvRowGroup, key: "dc"})
	if dc != 2 { // ops, localhost, ▾dc
		t.Fatalf("unexpected dc folder index %d", dc)
	}
	m.Update(teaKeyMsg("e")) // cursor on localhost row 1
	m.Update(teaKeyMsg("j")) // onto ▾dc (row 2)

	// Enter toggles the folder.
	m.Update(teaKeyMsg("enter"))
	if !m.collapsed["dc"] {
		t.Fatalf("enter in edit mode on a folder must collapse it")
	}
	m.Update(teaKeyMsg("enter"))
	if m.collapsed["dc"] {
		t.Fatalf("second enter must expand the folder")
	}

	// →/l opens the RENAME GROUP overlay.
	m.Update(teaKeyMsg("right"))
	if m.gform == nil || m.gform.oldName != "dc" || m.gform.value != "dc" {
		t.Fatalf("right on a folder must open the RENAME GROUP overlay: %+v", m.gform)
	}
	// Esc cancels without touching the file.
	m.Update(teaKeyMsg("esc"))
	if m.gform != nil {
		t.Fatalf("esc must close the rename overlay")
	}
	if got := strings.TrimSpace(readExtra(t, extraFile)); got != strings.TrimSpace(seed) {
		t.Fatalf("cancelled rename must not rewrite servers.txt:\n%s", got)
	}
	if !m.editMode {
		t.Fatalf("cancelling the rename must return to edit mode")
	}
}

// TestGroupForm_RenameUpdatesEveryServerInFolder persists a rename across all
// members of the folder and leaves other groups untouched.
func TestGroupForm_RenameUpdatesEveryServerInFolder(t *testing.T) {
	seed := "db1|root@10.0.0.5:22|prod|dc|\napp1|root@10.0.0.6:22||dc|\nweb1|root@10.0.0.7:22||web|\n"
	m, extraFile := newServersModel(t, seed)
	m.Update(teaKeyMsg("e"))
	m.Update(teaKeyMsg("j")) // onto ▾dc
	m.Update(teaKeyMsg("right"))
	if m.gform == nil || m.gform.value != "dc" {
		t.Fatalf("expected rename overlay prefilled dc: %+v", m.gform)
	}
	m.gform.value = "eu-east"
	m.Update(teaKeyMsg("enter"))
	if m.gform != nil {
		t.Fatalf("enter must apply and close the rename: %+v", m.gform.err)
	}
	want := "db1|root@10.0.0.5:22|prod|eu-east|\napp1|root@10.0.0.6:22||eu-east|\nweb1|root@10.0.0.7:22||web|\n"
	if got := readExtra(t, extraFile); got != want {
		t.Fatalf("servers.txt after rename:\n%q\nwant:\n%q", got, want)
	}
	v := m.View()
	if !strings.Contains(v, "▾ eu-east/") || strings.Contains(v, "▾ dc/") {
		t.Fatalf("view must show the renamed folder only:\n%s", v)
	}
	for _, e := range m.serversAll {
		if e.Alias == "db1" && e.Group != "eu-east" {
			t.Fatalf("db1 must follow the rename: %+v", e)
		}
		if e.Alias == "app1" && e.Group != "eu-east" {
			t.Fatalf("app1 must follow the rename: %+v", e)
		}
		if e.Alias == "web1" && e.Group != "web" {
			t.Fatalf("web1 must keep its group: %+v", e)
		}
	}
	if !strings.Contains(m.status, "renamed") {
		t.Fatalf("status must confirm the rename, got %q", m.status)
	}
}

// TestGroupForm_EmptyLabelUngroupsMembers: renaming a group to an empty
// label flattens its servers (Group → "") without losing the rows.
func TestGroupForm_EmptyLabelUngroupsMembers(t *testing.T) {
	seed := "db1|root@10.0.0.5:22|prod|dc|\napp1|root@10.0.0.6:22||dc|\n"
	m, extraFile := newServersModel(t, seed)
	m.Update(teaKeyMsg("e"))
	m.Update(teaKeyMsg("j")) // onto ▾dc
	m.Update(teaKeyMsg("l"))
	if m.gform == nil {
		t.Fatalf("l on a folder in edit mode must open the rename overlay")
	}
	m.gform.value = ""
	m.Update(teaKeyMsg("enter"))
	want := "db1|root@10.0.0.5:22|prod||\napp1|root@10.0.0.6:22|||\n"
	if got := readExtra(t, extraFile); got != want {
		t.Fatalf("servers.txt after ungroup:\n%q\nwant:\n%q", got, want)
	}
	v := m.View()
	if strings.Contains(v, "dc/") || strings.Contains(v, "▾") || strings.Contains(v, "▸") {
		t.Fatalf("ungrouped view must be flat and contain no dc folder:\n%s", v)
	}
	if !strings.Contains(m.status, "flat") {
		t.Fatalf("status must explain the ungroup, got %q", m.status)
	}
}

// TestGroupForm_CollapseStateTransfersOnRename: a folder that was folded
// stays folded under its new name after the rename.
func TestGroupForm_CollapseStateTransfersOnRename(t *testing.T) {
	seed := "db1|root@10.0.0.5:22||dc|\napp1|root@10.0.0.6:22||dc|\n"
	m, _ := newServersModel(t, seed)
	// Collapse dc through edit-mode Enter.
	m.Update(teaKeyMsg("e"))
	m.Update(teaKeyMsg("j"))
	m.Update(teaKeyMsg("enter"))
	if !m.collapsed["dc"] {
		t.Fatalf("folder must be collapsed")
	}
	// Rename via the overlay (cursor still on the ▸dc row).
	m.Update(teaKeyMsg("right"))
	m.gform.value = "eu"
	m.Update(teaKeyMsg("enter"))
	if m.collapsed["eu"] != true {
		t.Fatalf("collapsed state must transfer to the new name: %+v", m.collapsed)
	}
	if m.collapsed["dc"] {
		t.Fatalf("old group key must be dropped: %+v", m.collapsed)
	}
	if !strings.Contains(m.View(), "▸ eu/") {
		t.Fatalf("renamed folder must stay collapsed:\n%s", m.View())
	}
}

// TestGroupForm_InvalidLabelRejectedEscNoWrite: labels containing pipes or
// starting with # are rejected with an inline error; Esc then aborts without
// touching servers.txt.
func TestGroupForm_InvalidLabelRejectedEscNoWrite(t *testing.T) {
	seed := "db1|root@10.0.0.5:22||dc|\n"
	m, extraFile := newServersModel(t, seed)
	m.Update(teaKeyMsg("e"))
	m.Update(teaKeyMsg("j"))
	m.Update(teaKeyMsg("right"))
	m.gform.value = "a|b"
	m.Update(teaKeyMsg("enter"))
	if m.gform == nil || m.gform.err == "" {
		t.Fatalf("invalid group label must be rejected: %+v", m.gform)
	}
	if got := readExtra(t, extraFile); got != seed {
		t.Fatalf("rejected rename must not write servers.txt:\n%q", got)
	}
	m.Update(teaKeyMsg("esc"))
	if m.gform != nil {
		t.Fatalf("esc must close the failed rename overlay")
	}
}

// TestGroupForm_SameNameClosesWithoutWrite: submitting the current label is a
// cheap no-op that closes the overlay and leaves the file untouched.
func TestGroupForm_SameNameClosesWithoutWrite(t *testing.T) {
	seed := "db1|root@10.0.0.5:22||dc|\n"
	m, extraFile := newServersModel(t, seed)
	m.Update(teaKeyMsg("e"))
	m.Update(teaKeyMsg("j"))
	m.Update(teaKeyMsg("right"))
	m.Update(teaKeyMsg("enter")) // value still "dc"
	if m.gform != nil {
		t.Fatalf("same-name submit must close the overlay: %+v", m.gform.err)
	}
	if got := readExtra(t, extraFile); got != seed {
		t.Fatalf("same-name submit must not rewrite servers.txt:\n%q", got)
	}
}

// TestGroupForm_KeyboardTypingAndFocusRing drives the rename overlay with the
// keyboard: printable keys edit the field, Tab moves to Enable, Enter saves.
func TestGroupForm_KeyboardTypingAndFocusRing(t *testing.T) {
	seed := "db1|root@10.0.0.5:22||dc|\n"
	m, extraFile := newServersModel(t, seed)
	m.Update(teaKeyMsg("e"))
	m.Update(teaKeyMsg("j"))
	m.Update(teaKeyMsg("right"))
	// Prefill is "dc": backspace twice, then type the new name.
	m.Update(teaKeyMsg("backspace"))
	m.Update(teaKeyMsg("backspace"))
	typeKeys(m, "eu")
	if m.gform.value != "eu" {
		t.Fatalf("typing must replace the group value, got %q", m.gform.value)
	}
	m.Update(teaKeyMsg("tab"))
	if m.gform.focus != gFieldEnable {
		t.Fatalf("tab must focus Enable, got %v", m.gform.focus)
	}
	m.Update(teaKeyMsg("enter"))
	if m.gform != nil {
		t.Fatalf("Enter on Enable must apply the rename: %+v", m.gform.err)
	}
	if got := readExtra(t, extraFile); got != "db1|root@10.0.0.5:22||eu|\n" {
		t.Fatalf("servers.txt after keyboard rename:\n%q", got)
	}
}

// TestEditMode_DoubleClickServerEditsInsteadOfConnecting: inside edit mode a
// double-click on a server opens its EDIT form (no connection attempt);
// outside edit mode the same double click keeps its open-connection meaning.
func TestEditMode_DoubleClickServerEditsInsteadOfConnecting(t *testing.T) {
	t.Setenv("ZELLIJ", "")
	m, _ := newServersModel(t, "db1|root@10.0.0.5:22|||\n")
	// Screen rows: title 0, ops 1, localhost 2, db1 3.
	m.lastClickAt = timeZero()
	m.Update(tea.MouseMsg{Type: tea.MouseLeft, X: 3, Y: 3})
	m.Update(tea.MouseMsg{Type: tea.MouseLeft, X: 3, Y: 3}) // double-click on db1
	if m.form != nil {
		t.Fatalf("normal-mode double-click must open a connection, not a form")
	}
	if !strings.Contains(m.status, "not inside Zellij") {
		t.Fatalf("normal-mode double-click must keep the open-connection path: %q", m.status)
	}

	m.lastClickAt = timeZero()
	m.Update(teaKeyMsg("e")) // enter edit mode (cursor row 1 = localhost)
	m.Update(tea.MouseMsg{Type: tea.MouseLeft, X: 3, Y: 3})
	m.Update(tea.MouseMsg{Type: tea.MouseLeft, X: 3, Y: 3}) // double-click db1 again
	if m.form == nil || m.form.mode != formEdit || m.form.oldName != "db1" {
		t.Fatalf("edit-mode double-click must open the EDIT form for db1: %+v", m.form)
	}
}

// TestEditMode_EnterEditOnOpsRowArmedOpsStillWorks: while edit mode is active
// the ops row keeps its NEW/EDIT behaviour (Enter on armed NEW opens the NEW
// form; armed EDIT opens the last-selected server's form, guarded for the
// read-only rows).
func TestEditMode_EnterEditOnOpsRowArmedOpsStillWorks(t *testing.T) {
	m, _ := newServersModel(t, "db1|root@10.0.0.5:22|||\n")
	m.Update(teaKeyMsg("e")) // edit mode on; cursor row 1 (localhost)

	// Armed NEW on the ops row still opens the NEW overlay.
	m.srvCursor = 0
	m.opsSel = opNew
	m.Update(teaKeyMsg("enter"))
	if m.form == nil || m.form.mode != formNew {
		t.Fatalf("enter on armed NEW in edit mode must open the NEW form: %+v", m.form)
	}
	m.Update(teaKeyMsg("esc"))

	// Armed EDIT opens the last-selected extra server (selection survives the
	// trip over the ops row like in normal mode).
	m.serverCursor = 1 // db1
	m.srvCursor = 0
	m.opsSel = opEdit
	m.Update(teaKeyMsg("enter"))
	if m.form == nil || m.form.mode != formEdit || m.form.oldName != "db1" {
		t.Fatalf("enter on armed EDIT in edit mode must open db1's form: %+v", m.form)
	}
	m.Update(teaKeyMsg("esc"))

	// A builtin selection is guarded with a hint, never a form.
	m.serverCursor = 0 // localhost
	m.srvCursor = 0
	m.opsSel = opEdit
	m.Update(teaKeyMsg("enter"))
	if m.form != nil || !strings.Contains(m.status, "built-in") {
		t.Fatalf("armed EDIT on builtin localhost must hint, got form=%v status=%q", m.form != nil, m.status)
	}
}

// TestCellText_ColourEscapesAreZeroWidth guards renderOpsRow's padding: a
// green pointer (SGR escapes) must not disturb the visible-cell width of the
// ops line, and truncation must never cut an escape in half.
func TestCellText_ColourEscapesAreZeroWidth(t *testing.T) {
	forceColor(t)
	colored := pointerCell("▶", true) // one visible cell + SGR wrappers
	if colored == "▶" {
		t.Fatalf("test needs a coloured pointer (colorEnabled must be true)")
	}
	// Pad to 10 visible cells: the colour must not inflate the count.
	got := cellText(colored+"abc", 10)
	if s := stripANSI(got); s != "▶abc      " {
		t.Fatalf("cellText must pad to 10 visible cells, got %q", s)
	}
	// Truncate to 3 visible cells: ▶ plus two letters, escapes intact.
	got = cellText(colored+"123456789", 3)
	if s := stripANSI(got); s != "▶12" {
		t.Fatalf("cellText must truncate on visible cells, got %q", s)
	}
}

// TestSshEntry_OpenInsideZellijUsesNewPaneWithPassword verifies the full UI
// path for a password-protected server: sshpass argv reaches new-pane (fake
// sshpass + fake zellij in the same temp PATH) and nothing is typed into
// existing panes.
func TestSshEntry_OpenInsideZellijUsesNewPaneWithPassword(t *testing.T) {
	t.Setenv("ZELLIJ", "1")
	dir := t.TempDir()
	log := filepath.Join(dir, "zellij.log")
	// Fake `zellij`: records every invocation, then replays an empty answer.
	if err := os.WriteFile(filepath.Join(dir, "zellij"), []byte("#!/bin/sh\necho \"$@\" >> "+log+"\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Fake `sshpass`: present on PATH so SshArgv stops demanding an install.
	if err := os.WriteFile(filepath.Join(dir, "sshpass"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	m, _ := newServersModel(t, "db1|root@10.0.0.5:22|pw||s3cr3t\n")
	idx := rowIndexOfAlias(m, "db1")
	m.srvCursor = idx
	m.Update(teaKeyMsg("enter"))
	if m.svFailed["db1"] {
		t.Fatalf("open must not fail: %+v", m.svFailed)
	}
	raw, _ := os.ReadFile(log)
	s := string(raw)
	if !strings.Contains(s, "new-pane -- sshpass -p s3cr3t ssh root@10.0.0.5") {
		t.Fatalf("password must ride inside the new-pane argv: %s", s)
	}
	if strings.Contains(s, "write") {
		t.Fatalf("M6 must never type into an existing pane: %s", s)
	}
}
