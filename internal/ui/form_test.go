package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/aceneil/nav4neil/internal/servers"
)

// newServersModel builds a SectionServers model pinned to temp data files so
// tests never touch the real ~/.ssh/config or servers.txt.
func newServersModel(t *testing.T, extraContent string) (*Model, string) {
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
	m := NewModel("/tmp", nil, SectionServers)
	m.sshPathOverride = sshFile
	m.extraPathOverride = extraFile
	m.reloadServers()
	m.width = 60
	m.height = 16
	return m, extraFile
}

func readExtra(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// typeKeys feeds each rune of s as a separate key message (form typing path).
func typeKeys(m *Model, s string) {
	for _, r := range s {
		m.Update(teaKeyMsg(string(r)))
	}
}

// TestModel_SectionServersTitleAndOps asserts the single-section servers view
// starts with the serv4neil title row followed by ONE ops line carrying both
// [NEW] and [EDIT] side by side, then the server rows.
func TestModel_SectionServersTitleAndOps(t *testing.T) {
	m, _ := newServersModel(t, "db1|root@10.0.0.5:2222|prod db||\n")
	lines := strings.Split(m.View(), "\n")
	if strings.TrimSpace(lines[0]) != "serv4neil" {
		t.Fatalf("row 0 must be the serv4neil title, got %q", lines[0])
	}
	if !strings.Contains(lines[1], "[NEW]") || !strings.Contains(lines[1], "[EDIT]") {
		t.Fatalf("row 1 must carry [NEW] and [EDIT] on the SAME line, got %q", lines[1])
	}
	if strings.Contains(lines[2], "[NEW]") || strings.Contains(lines[2], "[EDIT]") {
		t.Fatalf("[NEW]/[EDIT] must not wrap onto row 2, got %q", lines[2])
	}
	// Server rows shift below the single ops line.
	if !strings.Contains(strings.Join(lines[2:], "\n"), "db1") {
		t.Fatalf("server rows must start below the ops row:\n%s", m.View())
	}
}

// TestModel_FormOpenAndEscCancel drives n → form visible → esc → gone.
func TestModel_FormOpenAndEscCancel(t *testing.T) {
	m, _ := newServersModel(t, "")
	m.Update(teaKeyMsg("n"))
	if m.form == nil {
		t.Fatal("pressing n must open the NEW form")
	}
	v := m.View()
	for _, want := range []string{"NEW SERVER", "Name", "Host", "Port", "User", "Pass", "Group", "Enable", "Cancel"} {
		if !strings.Contains(v, want) {
			t.Fatalf("overlay view missing %q:\n%s", want, v)
		}
	}
	// Esc cancels and returns to the plain list.
	m.Update(teaKeyMsg("esc"))
	if m.form != nil {
		t.Fatal("esc must close the form")
	}
	if strings.Contains(m.View(), "NEW SERVER") {
		t.Fatal("closed form must not stay rendered")
	}
}

// TestModel_FormTypingAndTabCycle exercises the text-entry + focus-ring path
// and then Enter-to-save.
func TestModel_FormTypingAndTabCycle(t *testing.T) {
	m, _ := newServersModel(t, "")
	m.Update(teaKeyMsg("N")) // uppercase N also opens NEW
	if m.form == nil {
		t.Fatal("N must open the NEW form")
	}
	typeKeys(m, "web1")
	m.Update(teaKeyMsg("tab"))
	typeKeys(m, "web1.example.com")
	m.Update(teaKeyMsg("tab")) // port already "22"
	if got := m.form.values[fieldPort]; got != "22" {
		t.Fatalf("port default must be 22, got %q", got)
	}
	m.Update(teaKeyMsg("tab"))
	typeKeys(m, "deploy")
	m.Update(teaKeyMsg("tab"))
	typeKeys(m, "pw")
	m.Update(teaKeyMsg("tab"))
	typeKeys(m, "web")
	if m.form.focus != fieldGroup {
		t.Fatalf("expected focus on Group after tabs, got %v", m.form.focus)
	}
	if got := m.form.values[fieldName]; got != "web1" {
		t.Fatalf("name field = %q", got)
	}
	// Backspace removes the last typed rune of the focused field.
	m.Update(teaKeyMsg("backspace"))
	if got := m.form.values[fieldGroup]; got != "we" {
		t.Fatalf("backspace must trim group, got %q", got)
	}
	m.Update(teaKeyMsg("enter"))
	if m.form != nil {
		t.Fatalf("enter must submit and close the form (err=%q)", m.form.err)
	}
}

// TestModel_NewServerSaveRefresh verifies the full NEW → enable path: file
// written in the M2 format, list reloaded with the builtin localhost first.
func TestModel_NewServerSaveRefresh(t *testing.T) {
	m, extraFile := newServersModel(t, "")
	m.Update(teaKeyMsg("n"))
	typeKeys(m, "web1")
	m.Update(teaKeyMsg("tab"))
	typeKeys(m, "web1.example.com")
	m.Update(teaKeyMsg("tab")) // port 22
	m.Update(teaKeyMsg("tab"))
	typeKeys(m, "deploy")
	m.Update(teaKeyMsg("tab"))
	typeKeys(m, "pw")
	m.Update(teaKeyMsg("tab"))
	typeKeys(m, "web")
	m.Update(teaKeyMsg("enter"))

	raw := strings.TrimSpace(readExtra(t, extraFile))
	want := "web1|deploy@web1.example.com:22||web|pw"
	if raw != want {
		t.Fatalf("servers.txt = %q want %q", raw, want)
	}
	if len(m.serversAll) != 2 || m.serversAll[0].Alias != "localhost" || m.serversAll[0].Source != "builtin" {
		t.Fatalf("list must be builtin-localhost first plus new row: %+v", m.serversAll)
	}
	found := false
	for _, e := range m.serversAll {
		if e.Alias == "web1" && e.User == "deploy" && e.Host == "web1.example.com" && e.Password == "pw" && e.Group == "web" {
			found = true
		}
	}
	if !found {
		t.Fatalf("saved entry not present in reloaded list: %+v", m.serversAll)
	}
	// Cursor lands on the freshly saved row.
	if idx := m.serverCursor; idx < 0 || idx >= len(m.serversView) || m.serversView[idx].Alias != "web1" {
		t.Fatalf("cursor must select the saved row: idx=%d view=%+v", idx, m.serversView)
	}
	if !strings.Contains(m.status, "saved web1") {
		t.Fatalf("status must confirm save, got %q", m.status)
	}
}

// TestModel_EditPrefillRenameAndPersist checks the M6 EDIT flow (e enters
// edit mode, cursor lands on the first server, Enter on a server row opens
// the EDIT overlay) plus the rename = save-with-new-name + drop-old-row
// behaviour.
func TestModel_EditPrefillRenameAndPersist(t *testing.T) {
	seed := "db1|root@10.0.0.5:2222|prod db|dc|s3cr3t\n"
	m, extraFile := newServersModel(t, seed)
	// Rows: ops, localhost, db1. Press e → edit mode, cursor jumps to the
	// first server (localhost, row 1); j moves onto db1; Enter opens EDIT.
	m.Update(teaKeyMsg("e"))
	if !m.editMode {
		t.Fatalf("e must enter edit mode, status=%q", m.status)
	}
	if m.srvCursor != 1 || m.srvRows[m.srvCursor].entry.Alias != "localhost" {
		t.Fatalf("entering edit mode must land the cursor on the first server: row=%d %+v", m.srvCursor, m.srvRows[m.srvCursor])
	}
	// Rows: ops, localhost, ▾dc folder, db1 (grouped) → two j presses reach
	// the db1 child; Enter opens its EDIT form.
	m.Update(teaKeyMsg("j"))
	m.Update(teaKeyMsg("j"))
	m.Update(teaKeyMsg("enter"))
	if m.form == nil {
		t.Fatalf("Enter in edit mode on db1 must open the EDIT form, status=%q", m.status)
	}
	if m.form.mode != formEdit || m.form.oldName != "db1" {
		t.Fatalf("edit metadata wrong: %+v", m.form)
	}
	got := m.form.values
	if got[fieldName] != "db1" || got[fieldUser] != "root" || got[fieldHost] != "10.0.0.5" ||
		got[fieldPort] != "2222" || got[fieldPass] != "s3cr3t" || got[fieldGroup] != "dc" {
		t.Fatalf("edit prefill wrong: %+v", got)
	}
	if m.form.desc != "prod db" {
		t.Fatalf("desc must be preserved outside the form fields: %q", m.form.desc)
	}
	// Rename and change the port; keep everything else.
	m.form.values[fieldName] = "db1-prod"
	m.form.values[fieldPort] = "22"
	m.enableForm()
	if m.form != nil {
		t.Fatalf("enable must close the form: %+v", m.form.err)
	}
	raw := strings.TrimSpace(readExtra(t, extraFile))
	want := "db1-prod|root@10.0.0.5:22|prod db|dc|s3cr3t"
	if raw != want {
		t.Fatalf("servers.txt = %q want %q", raw, want)
	}
	if len(m.serversAll) != 2 || m.serversAll[0].Alias != "localhost" {
		t.Fatalf("reload wrong: %+v", m.serversAll)
	}
	if m.serversAll[1].Alias != "db1-prod" {
		t.Fatalf("old row must be gone, new name present: %+v", m.serversAll)
	}
	if m.serversAll[1].Desc != "prod db" || m.serversAll[1].Password != "s3cr3t" {
		t.Fatalf("unchanged fields must survive the rename: %+v", m.serversAll[1])
	}
}

// TestModel_EditRejectsBuiltinAndSSH verifies the guards on what can be
// edited: Enter in edit mode on the built-in localhost row (or on an
// ssh-config host) shows a hint and never opens a form.
func TestModel_EditRejectsBuiltinAndSSH(t *testing.T) {
	m, _ := newServersModel(t, "extra1|root@h:22|||\n")
	// Row 0 = ops, row 1 = builtin localhost. Enter edit mode (cursor jumps
	// to localhost) and press Enter on it.
	m.Update(teaKeyMsg("e"))
	m.Update(teaKeyMsg("enter"))
	if m.form != nil || !strings.Contains(m.status, "built-in") {
		t.Fatalf("builtin edit must be rejected: form=%v status=%q", m.form, m.status)
	}
	// An ssh-config host must also be rejected.
	m.Update(teaKeyMsg("esc")) // leave edit mode
	m.serversAll = []servers.Entry{
		{Alias: "localhost", Source: "builtin"},
		{Alias: "github.com", Source: "ssh", SshAlias: "github.com"},
	}
	m.rebuildServerView()
	m.Update(teaKeyMsg("e"))
	if !m.editMode {
		t.Fatalf("e must re-enter edit mode")
	}
	m.Update(teaKeyMsg("j")) // localhost → github.com
	m.Update(teaKeyMsg("enter"))
	if m.form != nil || !strings.Contains(m.status, "ssh config") {
		t.Fatalf("ssh edit must be rejected: form=%v status=%q", m.form, m.status)
	}
}

// TestModel_FormValidationLocalhostAndDuplicates checks client-side guard rails.
func TestModel_FormValidationLocalhostAndDuplicates(t *testing.T) {
	m, _ := newServersModel(t, "box|root@10.0.0.1:22|first||\n")
	// Reserved name.
	m.Update(teaKeyMsg("n"))
	m.form.values[fieldName] = "LOCALHOST"
	m.form.values[fieldHost] = "x"
	m.enableForm()
	if m.form == nil || !strings.Contains(m.form.err, "reserved") {
		t.Fatalf("localhost must be rejected: %+v", m.form)
	}
	// Duplicate of an existing extra row.
	m.form = nil
	m.serverCursor = 1 // box
	m.openServerForm(m.serversView[m.serverCursor], true)
	m.form.values[fieldName] = "dupe"
	m.form.values[fieldHost] = "10.0.0.9"
	m.enableForm()
	if m.form != nil {
		t.Fatalf("new name dupe should be allowed: %+v", m.form)
	}
	// Now try to reuse "dupe" from a second row.
	m.Update(teaKeyMsg("n"))
	m.form.values[fieldName] = "dupe"
	m.form.values[fieldHost] = "10.0.0.10"
	m.enableForm()
	if m.form == nil || !strings.Contains(m.form.err, "already exists") {
		t.Fatalf("duplicate name must be rejected: %+v", m.form)
	}
	// Missing host/name.
	m.form.values[fieldName] = "ok"
	m.form.values[fieldHost] = ""
	m.enableForm()
	if m.form == nil || !strings.Contains(m.form.err, "Host is required") {
		t.Fatalf("empty host must be rejected: %+v", m.form)
	}
}

// TestModel_MouseOpsRowAndListOffset covers the click zones below the title:
// the ops line (y=1) is split into a [NEW] half (left, opens the NEW form)
// and an [EDIT] half (right, M6: enters edit mode); server entries start at
// y=2.
func TestModel_MouseOpsRowAndListOffset(t *testing.T) {
	m, _ := newServersModel(t, "db1|root@10.0.0.5:22|||\n")
	m.lastClickAt = timeZero()

	// Click the [NEW] half of the ops line.
	m.Update(tea.MouseMsg{Type: tea.MouseLeft, X: 2, Y: 1})
	if m.form == nil || m.form.mode != formNew {
		t.Fatalf("click on the [NEW] half must open the new form: %+v", m.form)
	}
	m.Update(teaKeyMsg("esc"))
	m.lastClickAt = timeZero()

	// Click the [EDIT] half of the ops line: M6 enters edit mode (the cursor
	// jumps to the first server instead of opening a form directly).
	m.Update(tea.MouseMsg{Type: tea.MouseLeft, X: 10, Y: 1})
	if m.form != nil || !m.editMode {
		t.Fatalf("click on the [EDIT] half must enter edit mode: form=%v edit=%v", m.form != nil, m.editMode)
	}
	m.Update(teaKeyMsg("esc"))
	m.lastClickAt = timeZero()

	// Clicking a server row: y=2 is list row 0 (localhost).
	m.Update(tea.MouseMsg{Type: tea.MouseLeft, X: 3, Y: 2})
	if m.srvCursor != 1 || m.serversView[m.serverCursor].Alias != "localhost" {
		t.Fatalf("y=2 must select list row 0: srv=%d sel=%d", m.srvCursor, m.serverCursor)
	}
	// y=3 selects db1.
	m.Update(tea.MouseMsg{Type: tea.MouseLeft, X: 3, Y: 3})
	if m.serversView[m.serverCursor].Alias != "db1" {
		t.Fatalf("y=3 must select list row 1 (db1): cursor=%d", m.serverCursor)
	}
}

// TestModel_FormMouseButtonsClickEnableAndCancel exercises overlay buttons.
func TestModel_FormMouseButtonsClickEnableAndCancel(t *testing.T) {
	m, extraFile := newServersModel(t, "")
	m.Update(teaKeyMsg("n"))
	m.form.values[fieldName] = "clicky"
	m.form.values[fieldHost] = "h"
	// Click Cancel first (content col 17 + border = x0+18).
	x0, y0, _ := m.formGeometry()
	m.Update(tea.MouseMsg{Type: tea.MouseLeft, X: x0 + 18, Y: y0 + 8})
	if m.form != nil {
		t.Fatalf("Cancel click must close the form")
	}
	if _, err := os.Stat(extraFile); !os.IsNotExist(err) {
		t.Fatalf("cancel must not create servers.txt")
	}
	// Open again and click Enable (content col 6 + border = x0+7).
	m.Update(teaKeyMsg("n"))
	m.form.values[fieldName] = "clicky"
	m.form.values[fieldHost] = "h"
	m.Update(tea.MouseMsg{Type: tea.MouseLeft, X: x0 + 7, Y: y0 + 8})
	if m.form != nil {
		t.Fatalf("Enable click must close the form: %+v", m.form.err)
	}
	if got := strings.TrimSpace(readExtra(t, extraFile)); got != "clicky|h:22|||" {
		t.Fatalf("servers.txt = %q", got)
	}
}

// TestModel_OpenSelectedServerWithPasswordMissingSshpass asserts the status
// hint fires (and no run attempt happens) when an entry carries a password
// but sshpass is not installed. PATH is pinned to an empty dir so LookPath
// cannot find sshpass regardless of the host environment.
func TestModel_OpenSelectedServerWithPasswordMissingSshpass(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	t.Setenv("ZELLIJ", "")
	m, _ := newServersModel(t, "db1|root@10.0.0.5:22|pw server||s3cr3t\n")
	if len(m.serversView) < 2 || m.serversView[1].Alias != "db1" {
		t.Fatalf("unexpected view: %+v", m.serversView)
	}
	m.serverCursor = 1
	m.openSelectedServer()
	if !strings.Contains(m.status, "sshpass") {
		t.Fatalf("status must hint at sshpass, got %q", m.status)
	}
}

// timeZero is a tiny helper to neutralise double-click state before a mouse
// test (kept local to this file).
func timeZero() (t time.Time) { return }
