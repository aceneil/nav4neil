// Package ui wires the nav4neil TUI on top of bubbletea. Layout (SectionBoth):
//
//	┌─ Servers ─────────────┐
//	│ a1                    │
//	│ a2   (highlighted)    │   ← arrow keys / j/k move; click selects;
//	│ a3                    │     double-click or Enter triggers action
//	├───────────────────────┤
//	│ Files (cwd: ~/)       │
//	│ ..                    │
//	│ dir1/                 │   ← same keys; Enter on file → wz-open.sh;
//	│ dir2/                 │     Enter on dir  → navigate in
//	│ ······                │   ← section divider
//	│ ▶ Files (cwd: ~/)     │
//	│ file.txt              │
//	├───────────────────────┤
//	│ ctx: local  | ? help  │
//	└───────────────────────┘
//
// Panes are tab-cycled with Tab / 1 / 2. '/' filters the active pane.
// On startup the program also boots the local websocket server from
// internal/ws (best-effort, never blocks the TUI).
//
// Single-section mode (SectionServers / SectionFiles): the model renders
// exactly one pane and consumes the full height minus the title + status
// row. Tab / 1 / 2 are silently ignored (no other pane exists), 'r'
// refreshes only the active section, and the focus marker is always the
// focused-arrow form. This is what lets two nav4neil instances live in two
// stacked Zellij panes and have focus moved between them via Zellij's
// native Alt+h/j/k/l.
package ui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/aceneil/nav4neil/internal/action"
	"github.com/aceneil/nav4neil/internal/fs"
	"github.com/aceneil/nav4neil/internal/icon"
	"github.com/aceneil/nav4neil/internal/servers"
	"github.com/aceneil/nav4neil/internal/ws"
)

// pane identifies which pane currently owns focus.
type pane int

const (
	paneServers pane = iota
	paneFiles
)

// Section selects which top-level area(s) the model renders.
//
// SectionBoth keeps the historical two-pane layout (default;
// Tab / 1 / 2 cycles focus). SectionServers and SectionFiles render
// only that one area, fill the available height, lock the focus to
// the visible area, and ignore Tab / 1 / 2 — useful when each nav4neil
// lives in its own Zellij pane and the user moves focus between the
// two panes via Zellij's native Alt+h/j/k/l.
type Section int

const (
	SectionBoth Section = iota
	SectionServers
	SectionFiles
)

// ParseSection converts a CLI string into a Section. Empty, "both"
// (case-insensitive) yield SectionBoth. Unknown values return an error
// so the CLI can exit cleanly instead of silently falling back.
func ParseSection(s string) (Section, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "both":
		return SectionBoth, nil
	case "servers", "server":
		return SectionServers, nil
	case "files", "file":
		return SectionFiles, nil
	}
	return SectionBoth, fmt.Errorf("invalid section %q (want both|servers|files)", s)
}

// String renders Section in a stable form suitable for the status bar.
func (s Section) String() string {
	switch s {
	case SectionServers:
		return "servers"
	case SectionFiles:
		return "files"
	default:
		return "both"
	}
}

// srvRowKind identifies one focusable row of the servers pane list.
type srvRowKind int

const (
	srvRowOps   srvRowKind = iota // combined [NEW]/[EDIT] ops row (SectionServers only)
	srvRowEntry                   // a real server entry (flat or inside a group)
	srvRowGroup                   // a group folder, ▾ expanded / ▸ collapsed
)

// srvOp selects the armed operation inside the single ops row.
type srvOp int

const (
	opNew  srvOp = iota // [NEW]
	opEdit              // [EDIT]
)

// srvRow is one focusable row of the servers pane. entry is valid for
// srvRowEntry; group labels the folder for srvRowGroup (children reuse the
// same group value so rendering can indent them).
type srvRow struct {
	kind  srvRowKind
	entry servers.Entry // srvRowEntry only
	group string        // srvRowGroup label / srvRowEntry parent group
}

// rowIdentity is a stable handle for one row across rebuilds, so collapsing
// a folder, filtering, or refreshing keeps the display cursor on the same
// item (or its nearest surviving equivalent) instead of jumping to row 0.
type rowIdentity struct {
	kind srvRowKind
	key  string // entry alias or group label
}

func (m *Model) identityOf(row srvRow) rowIdentity {
	switch row.kind {
	case srvRowEntry:
		return rowIdentity{kind: srvRowEntry, key: row.entry.Alias}
	case srvRowGroup:
		return rowIdentity{kind: srvRowGroup, key: row.group}
	default:
		return rowIdentity{kind: row.kind}
	}
}

// findRowIdentity returns the first row index matching id, or -1.
func (m *Model) findRowIdentity(id rowIdentity) int {
	for i, r := range m.srvRows {
		if m.identityOf(r) == id {
			return i
		}
	}
	return -1
}

// Model is the bubbletea model. All state lives here; bubbletea guarantees
// single-threaded Update access.
type Model struct {
	width, height int

	section Section
	focus   pane

	serversAll   []servers.Entry
	serversView  []servers.Entry
	serverCursor int
	serverFilter string

	// M4/M5 row model: the servers pane is rendered as an ordered list of
	// focusable rows — a combined [NEW]/[EDIT] ops row (single-section mode
	// only), ungrouped "flat" entries, then ▾/▸ group folders with their
	// children. opsSel arms which op Enter triggers while the cursor rests on
	// the ops row. srvCursor indexes srvRows and drives highlighting;
	// serverCursor keeps the selected ENTRY inside serversView so open/edit
	// actions keep working while the display cursor rests on an ops or
	// folder row. srvTop scrolls the window when a grouped list grows
	// taller than the pane.
	srvRows   []srvRow
	srvCursor int
	srvTop    int
	opsSel    srvOp
	collapsed map[string]bool // group name → folded (children hidden)

	// M6 edit mode: while editMode is true the servers pane re-interprets
	// Enter/→ (edit the row under the cursor instead of opening it), the ops
	// row renders as <NEW> <EDIT>, and the focused pointer turns green. It is
	// entered with e/E or the [EDIT] click and left with Esc (or e/E again).
	editMode bool

	// M3 connection-status squares: in-Zellij we poll dump-layout for open
	// tab names; the ok/failed ledger below also drives the fallback mode
	// (outside Zellij or when a poll fails).
	inZellij bool            // set at startup: $ZELLIJ present → polling allowed
	liveTabs bool            // last dump-layout poll succeeded (only meaningful inZellij)
	tabsOpen map[string]bool // Zellij tab names seen by the last successful poll
	svOK     map[string]bool // per alias: last open attempt succeeded (fallback green)
	svFailed map[string]bool // per alias: last open attempt failed (red until cleared)

	files      *fs.Browser
	fileView   []fs.Item
	fileCur    int
	fileFilter string

	editingFilter bool

	// form is non-nil while the NEW/EDIT server overlay is open, gform while
	// the RENAME GROUP overlay is open. At most one overlay exists at a time;
	// while it is open all normal keys and mouse events route to it until it
	// is closed.
	form  *serverForm
	gform *groupForm

	// Path overrides (tests): when empty the real $HOME/.ssh/config and
	// $XDG_CONFIG_HOME/wezterm4neil/servers.txt are used.
	sshPathOverride   string
	extraPathOverride string

	status string // transient bottom-line message (right side)

	ctx string // "local" or last-selected ssh alias
	wss *ws.Server

	// M8 standalone exec: in SectionBoth mode picking a server/file records
	// the target argv here and returns tea.Quit. bubbletea shuts down
	// (restores the terminal), then main() replaces this process with
	// execArgv via syscall.Exec — the current pane keeps running ssh / the
	// local shell / the file editor and the nav never comes back. nil means
	// a normal quit.
	execArgv []string

	// Mouse double-click detection.
	lastClickAt time.Time
	lastClickX  int
	lastClickY  int
}

// NewModel constructs the initial model. The websocket server is built
// but not started here; main() calls ws.Start so we can hand back the
// URL for logging even if startup fails.
//
// section picks which top-level area(s) the model is responsible for.
// SectionBoth reproduces the historical two-pane layout (Tab / 1 / 2
// cycle focus). SectionServers loads only the server list and locks
// focus to it; SectionFiles loads only the file browser and locks
// focus there. In single-section mode the model skips the irrelevant
// data load entirely (no ~/.ssh/config parsing for a files-only
// nav4neil, no ReadDir for a servers-only one).
func NewModel(startDir string, wss *ws.Server, section Section) *Model {
	m := &Model{
		section:  section,
		ctx:      "local",
		wss:      wss,
		inZellij: action.InZellij(),
		svOK:     map[string]bool{},
		svFailed: map[string]bool{},
	}
	switch section {
	case SectionServers:
		m.focus = paneServers
		m.reloadServers()
	case SectionFiles:
		m.focus = paneFiles
		m.files = fs.New(startDir)
		m.refreshFiles()
	default:
		m.focus = paneServers
		m.files = fs.New(startDir)
		m.reloadServers()
		m.refreshFiles()
	}
	return m
}

// tickMsg drives the status-line auto-expiry.
type tickMsg time.Time

// tabsPollMsg carries the outcome of one `zellij action dump-layout` poll.
// ok=false means the dump failed (fall back to the local open ledger);
// tabs holds the open tab names when ok.
type tabsPollMsg struct {
	ok   bool
	tabs []string
}

// Poll cadence for Zellij tab presence. The first poll fires quickly after
// startup so status squares settle within a blink; later polls every 2.5s.
const (
	firstTabsPollDelay = 300 * time.Millisecond
	tabsPollEvery      = 2500 * time.Millisecond
)

// Init is required by bubbletea. We tick every 750ms so the transient
// status message can self-clear after a few seconds, and — only when the
// process runs inside Zellij — we also start the dump-layout poll loop.
func (m *Model) Init() tea.Cmd {
	cmds := []tea.Cmd{tea.Tick(750*time.Millisecond, func(t time.Time) tea.Msg { return tickMsg(t) })}
	if poll := m.tabsPollCmd(firstTabsPollDelay); poll != nil {
		cmds = append(cmds, poll)
	}
	return tea.Batch(cmds...)
}

// tabsPollCmd returns a command that sleeps for delay, runs
// `zellij action dump-layout` (2s timeout) and reports the parsed tab names
// as a tabsPollMsg. Outside Zellij it returns nil so the poll loop never
// spins: the UI simply keeps showing the local ledger state. Failures are
// silent — they flip ok=false and the caller falls back to the ledger.
func (m *Model) tabsPollCmd(delay time.Duration) tea.Cmd {
	if !m.inZellij {
		return nil
	}
	return func() tea.Msg {
		time.Sleep(delay)
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		out, err := exec.CommandContext(ctx, "zellij", "action", "dump-layout").Output()
		if err != nil {
			return tabsPollMsg{ok: false}
		}
		return tabsPollMsg{ok: true, tabs: parseTabNames(string(out))}
	}
}

// noteOpenResult updates the per-server ledger after one open attempt
// (err == nil → success) or after a poll proves a tab is present again.
func (m *Model) noteOpenResult(alias string, err error) {
	if m.svOK == nil {
		m.svOK = map[string]bool{}
	}
	if m.svFailed == nil {
		m.svFailed = map[string]bool{}
	}
	if err != nil {
		m.svFailed[alias] = true
		delete(m.svOK, alias)
		return
	}
	delete(m.svFailed, alias)
	m.svOK[alias] = true
}

// ----- Update --------------------------------------------------------------

// Update is the bubbletea message dispatcher.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil
	case tea.KeyMsg:
		return m.handleKey(msg)
	case tea.MouseMsg:
		return m.handleMouse(msg)
	case tickMsg:
		// Every tick is a chance to clear stale status. We re-arm the timer.
		return m, tea.Tick(750*time.Millisecond, func(t time.Time) tea.Msg { return tickMsg(t) })
	case tabsPollMsg:
		m.liveTabs = m.inZellij && msg.ok
		m.tabsOpen = nil
		if msg.ok {
			m.tabsOpen = make(map[string]bool, len(msg.tabs))
			for _, t := range msg.tabs {
				m.tabsOpen[t] = true
			}
			// A tab being present proves the connection is up again: clear
			// any pending red for that server (until the next failure).
			for _, e := range m.serversAll {
				if m.svFailed[e.Alias] && m.tabsOpen[action.Tab(e)] {
					m.noteOpenResult(e.Alias, nil)
				}
			}
		}
		return m, m.tabsPollCmd(tabsPollEvery)
	default:
		return m, nil
	}
}

func (m *Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// If we're in filter-edit mode, every printable key edits the buffer.
	if m.editingFilter {
		return m.handleFilterKey(msg)
	}
	// While an overlay is open it owns every key until closed.
	if m.form != nil {
		return m.handleFormKey(msg)
	}
	if m.gform != nil {
		return m.handleGroupFormKey(msg)
	}
	k := msg.String()
	switch k {
	case "ctrl+c":
		return m, tea.Quit
	case "q":
		return m, tea.Quit
	case "?":
		m.status = helpText(m.section)
		return m, nil
	case "/":
		m.editingFilter = true
		m.status = ""
		return m, nil
	}
	// Tab / 1 / 2 only make sense when both sections are visible.
	// In single-section mode they're silently ignored — the user
	// moves focus between sibling Zellij panes with Alt+h/j/k/l.
	if m.section == SectionBoth {
		switch k {
		case "tab":
			m.focus = (m.focus + 1) % 2
			m.clearFilter()
			return m, nil
		case "1":
			m.focus = paneServers
			m.clearFilter()
			return m, nil
		case "2":
			m.focus = paneFiles
			m.clearFilter()
			return m, nil
		}
	}
	switch k {
	case "r":
		switch m.section {
		case SectionServers:
			m.reloadServers()
			m.status = "refreshed servers"
		case SectionFiles:
			m.refreshFiles()
			m.status = "refreshed files"
		default:
			m.reloadServers()
			m.refreshFiles()
			m.status = "refreshed"
		}
		return m, nil
	}
	switch m.focus {
	case paneServers:
		return m.handleServersKey(msg)
	case paneFiles:
		return m.handleFilesKey(msg)
	}
	return m, nil
}

// helpText returns the keybinding summary appropriate for the
// currently rendered section. In single-section mode Tab / 1 / 2 are
// omitted (they would be no-ops), and the 'r' description is tightened
// to the area that actually refreshes. M8 wording reflects the two launch
// modes: sidebar (--section servers|files) opens new Zellij tabs, while
// standalone (SectionBoth) quits the nav and execs the target in the
// current pane.
func helpText(s Section) string {
	switch s {
	case SectionServers:
		return "sidebar: enter = new tab · j/k move · ←/→ or h/l arm [NEW]/[EDIT] · group fold · n new · e edit-mode · / filter · r refresh · ? help · q quit"
	case SectionFiles:
		return "sidebar: enter = wz-open (floating) · j/k move · h/l parent/into · / filter · r refresh dir · ? help · q quit"
	default:
		return "standalone: enter = run in this pane & nav exits · j/k move · / filter · 1/2 panes · tab cycle · r refresh · ? help · q quit"
	}
}

// standalone reports whether this instance runs in standalone mode — the
// default `nav4neil` with SectionBoth. Standalone opens quit the TUI and
// replace the current process (ssh / local shell / editor). Sidebar
// instances (--section servers|files) instead ask Zellij to open new tabs
// and keep the nav alive.
func (m *Model) standalone() bool { return m.section == SectionBoth }

func (m *Model) handleServersKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		// Leave edit mode; outside edit mode Esc has no servers-pane action.
		if m.editMode {
			m.editMode = false
			m.status = ""
		}
	case "j", "down":
		m.moveSrvCursor(1)
	case "k", "up":
		m.moveSrvCursor(-1)
	case "g":
		m.jumpSrvFirstEntry()
	case "G":
		if n := len(m.srvRows); n > 0 {
			m.srvCursor = n - 1
			m.syncSelectionToRow()
		}
	case "h", "left":
		// On the ops row, ←/h arm [NEW]; elsewhere the key is unused in the
		// servers pane (left/right do not exist for entry rows).
		if m.onOpsRow() {
			m.opsSel = opNew
		}
	case "l", "right":
		// In edit mode →/l edits whatever is under the cursor (server form or
		// group rename); on the ops row it arms [EDIT]. Outside edit mode →
		// only arms [EDIT] while the cursor rests on the ops row.
		if m.editMode {
			m.editCurrentRow()
		} else if m.onOpsRow() {
			m.opsSel = opEdit
		}
	case "n", "N":
		m.openServerForm(servers.Entry{Source: "extra"}, false)
	case "e", "E":
		m.toggleEditMode()
	case "enter":
		var cmd tea.Cmd
		if m.editMode {
			cmd = m.activateEditRow()
		} else {
			cmd = m.activateServerRow()
		}
		return m, cmd
	}
	return m, nil
}

// toggleEditMode flips the servers-pane edit mode. Entering it moves the
// cursor to the top-most real row (a server, or the first folder when the
// list starts with one) and shows a hint; leaving it clears the mode without
// touching the cursor.
func (m *Model) toggleEditMode() {
	if m.editMode {
		m.editMode = false
		m.status = ""
		return
	}
	m.editMode = true
	m.jumpSrvFirstItem()
	m.status = "edit mode: Enter/→ 编辑当前项 · Esc 退出"
}

// jumpSrvFirstItem sends the display cursor to the top-most actionable row
// (first server or, when the visible list starts with folders, the first
// folder), skipping the ops row.
func (m *Model) jumpSrvFirstItem() {
	for i, r := range m.srvRows {
		if r.kind == srvRowEntry || r.kind == srvRowGroup {
			m.srvCursor = i
			m.syncSelectionToRow()
			return
		}
	}
	m.srvCursor = 0
}

// activateEditRow runs the M6 edit-mode semantics of Enter: an entry row
// opens its EDIT form, a folder row folds/unfolds (Enter never renames), and
// the ops row triggers its armed op. Edit-mode actions never open a
// connection, so it always returns nil.
func (m *Model) activateEditRow() tea.Cmd {
	if len(m.srvRows) == 0 {
		return nil
	}
	r := m.srvRows[m.srvCursor]
	switch r.kind {
	case srvRowOps:
		if m.opsSel == opEdit {
			m.editSelectedServer()
		} else {
			m.openServerForm(servers.Entry{Source: "extra"}, false)
		}
	case srvRowGroup:
		m.toggleGroup(r.group)
	case srvRowEntry:
		m.editEntry(r.entry)
	}
	return nil
}

// editCurrentRow implements the M6 edit-mode semantics of →/l: the row under
// the cursor opens its editing overlay — server rows the EDIT server form,
// folder rows the RENAME GROUP form; on the ops row → merely arms [EDIT].
func (m *Model) editCurrentRow() {
	if len(m.srvRows) == 0 {
		return
	}
	r := m.srvRows[m.srvCursor]
	switch r.kind {
	case srvRowOps:
		m.opsSel = opEdit
	case srvRowGroup:
		m.openGroupForm(r.group)
	case srvRowEntry:
		m.editEntry(r.entry)
	}
}

// editEntry opens the EDIT overlay for one real entry, guarding the
// read-only rows: the built-in localhost/herdr entries and ssh-config hosts
// get a status hint instead of a form, so built-ins can never be changed or
// removed through the UI.
func (m *Model) editEntry(e servers.Entry) {
	switch {
	case servers.IsBuiltin(e):
		m.status = "内置项不可编辑：localhost/herdr 为内置项，用 [NEW] 添加新服务器"
		return
	case e.Source == "ssh":
		m.status = "ssh config hosts are not in servers.txt; use [NEW] to add a copy"
		return
	}
	m.openServerForm(e, true)
}

// onOpsRow reports whether the display cursor rests on the combined ops row.
func (m *Model) onOpsRow() bool {
	return m.srvCursor >= 0 && m.srvCursor < len(m.srvRows) && m.srvRows[m.srvCursor].kind == srvRowOps
}

// moveSrvCursor steps the display cursor by d (±1), wrapping around the row
// list so ↑/↓ (j/k) cycle ops row → first server → … → last → ops row.
// Arriving on the ops row arms the op matching the travel direction: coming
// up from the servers (d<0) arms [EDIT] (the row that used to sit directly
// above the first server), wrapping down from the bottom (d>0) arms [NEW].
func (m *Model) moveSrvCursor(d int) {
	n := len(m.srvRows)
	if n == 0 {
		return
	}
	old := m.srvCursor
	m.srvCursor = (m.srvCursor + d + n) % n
	if m.srvRows[m.srvCursor].kind == srvRowOps {
		if d > 0 {
			m.opsSel = opNew
		} else if old != m.srvCursor {
			m.opsSel = opEdit
		}
	}
	m.syncSelectionToRow()
}

// jumpSrvFirstEntry sends the cursor to the first real server row, skipping
// the ops row (localhost is the very first entry, herdr right behind it).
func (m *Model) jumpSrvFirstEntry() {
	for i, r := range m.srvRows {
		if r.kind == srvRowEntry {
			m.srvCursor = i
			m.syncSelectionToRow()
			return
		}
	}
	m.srvCursor = 0
}

// syncSelectionToRow keeps serverCursor (the selected entry used by open and
// edit actions) aligned with the display cursor whenever it rests on an
// entry row. Ops and folder rows leave the selection untouched so actions
// still target the last real server the user visited.
func (m *Model) syncSelectionToRow() {
	if m.srvCursor < 0 || m.srvCursor >= len(m.srvRows) {
		return
	}
	r := m.srvRows[m.srvCursor]
	if r.kind != srvRowEntry {
		return
	}
	for i, e := range m.serversView {
		if e.Alias == r.entry.Alias {
			m.serverCursor = i
			return
		}
	}
}

// activateServerRow runs the action of the row under the display cursor:
// the ops row opens the armed overlay (NEW/EDIT), a folder row toggles
// collapse, an entry row connects to the server. The returned command is
// tea.Quit when a standalone (SectionBoth) open asked the TUI to exit so
// main() can exec the target; sidebar opens (and every non-entry row) stay
// in the UI and return nil.
func (m *Model) activateServerRow() tea.Cmd {
	if len(m.srvRows) == 0 {
		m.status = "no servers to open (refresh with r)"
		return nil
	}
	r := m.srvRows[m.srvCursor]
	switch r.kind {
	case srvRowOps:
		if m.opsSel == opEdit {
			m.editSelectedServer()
		} else {
			m.openServerForm(servers.Entry{Source: "extra"}, false)
		}
	case srvRowGroup:
		m.toggleGroup(r.group)
	case srvRowEntry:
		return m.openEntry(r.entry)
	}
	return nil
}

// toggleGroup folds/unfolds a group folder and keeps the cursor on it.
func (m *Model) toggleGroup(g string) {
	if m.collapsed == nil {
		m.collapsed = map[string]bool{}
	}
	m.collapsed[g] = !m.collapsed[g]
	m.rebuildServerRows()
	if i := m.findRowIdentity(rowIdentity{kind: srvRowGroup, key: g}); i >= 0 {
		m.srvCursor = i
	}
}

// editSelectedServer opens the EDIT overlay for the currently selected entry
// (used by the ops row's armed [EDIT] action; entry/folder rows under the
// cursor go through editEntry/editCurrentRow). Only servers.txt-managed rows
// (Source "extra") are editable: ssh-config hosts and the built-in localhost
// get an explanatory hint instead.
func (m *Model) editSelectedServer() {
	if len(m.serversView) == 0 {
		m.status = "nothing to edit"
		return
	}
	if m.serverCursor < 0 || m.serverCursor >= len(m.serversView) {
		m.serverCursor = clamp(m.serverCursor, 0, max(0, len(m.serversView)-1))
	}
	m.editEntry(m.serversView[m.serverCursor])
}

func (m *Model) handleFilesKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "j", "down":
		if m.fileCur < len(m.fileView)-1 {
			m.fileCur++
		}
	case "k", "up":
		if m.fileCur > 0 {
			m.fileCur--
		}
	case "h", "left":
		if err := m.files.Parent(); err != nil {
			m.status = err.Error()
		} else {
			m.status = ""
			m.refreshFiles()
		}
	case "l", "right":
		return m, m.enterSelectedFile()
	case "enter":
		return m, m.enterSelectedFile()
	}
	return m, nil
}

func (m *Model) handleFilterKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.editingFilter = false
		m.clearFilter()
	case "enter":
		m.editingFilter = false
	case "backspace":
		switch m.focus {
		case paneServers:
			if len(m.serverFilter) > 0 {
				m.serverFilter = m.serverFilter[:len(m.serverFilter)-1]
				m.rebuildServerView()
			}
		case paneFiles:
			if len(m.fileFilter) > 0 {
				m.fileFilter = m.fileFilter[:len(m.fileFilter)-1]
				m.rebuildFileView()
			}
		}
	default:
		s := msg.String()
		if len(s) == 1 && s[0] >= 32 && s[0] < 127 {
			switch m.focus {
			case paneServers:
				m.serverFilter += s
				m.rebuildServerView()
			case paneFiles:
				m.fileFilter += s
				m.rebuildFileView()
			}
		}
	}
	return m, nil
}

func (m *Model) clearFilter() {
	if m.serverFilter != "" {
		m.serverFilter = ""
		m.rebuildServerView()
	}
	if m.fileFilter != "" {
		m.fileFilter = ""
		m.rebuildFileView()
	}
}

func (m *Model) currentFilter() string {
	if m.focus == paneServers {
		return m.serverFilter
	}
	return m.fileFilter
}

func (m *Model) rebuildServerView() {
	q := strings.ToLower(m.serverFilter)
	if q == "" {
		m.serversView = append([]servers.Entry(nil), m.serversAll...)
	} else {
		var out []servers.Entry
		for _, e := range m.serversAll {
			if strings.Contains(strings.ToLower(e.Alias), q) ||
				strings.Contains(strings.ToLower(e.Desc), q) {
				out = append(out, e)
			}
		}
		m.serversView = out
	}
	m.serverCursor = clamp(m.serverCursor, 0, max(0, len(m.serversView)-1))
	m.rebuildServerRows()
}

// rebuildServerRows builds the focusable row list from serversView:
//
//   - SectionServers mode prepends the single ops row ([NEW] and [EDIT] are
//     rendered side by side on that one line; opsSel arms which one Enter
//     triggers);
//   - an active filter flattens every match (groups are skipped so a partial
//     match list stays navigable);
//   - otherwise ungrouped entries are listed first (the built-ins
//     localhost → herdr sit at the very front), followed by one ▾/▸ folder
//     per group — in first-appearance order — whose children appear only
//     while the folder is expanded.
//
// The display cursor is restored by row identity, falling back to the
// current selection and finally to the first entry row.
func (m *Model) rebuildServerRows() {
	prev := rowIdentity{}
	havePrev := len(m.srvRows) > 0 && m.srvCursor >= 0 && m.srvCursor < len(m.srvRows)
	if havePrev {
		prev = m.identityOf(m.srvRows[m.srvCursor])
	}
	q := strings.ToLower(m.serverFilter)
	var rows []srvRow
	if m.section == SectionServers {
		rows = append(rows, srvRow{kind: srvRowOps})
	}
	if q != "" {
		for _, e := range m.serversView {
			rows = append(rows, srvRow{kind: srvRowEntry, entry: e})
		}
	} else {
		for _, e := range m.serversView {
			if e.Group == "" {
				rows = append(rows, srvRow{kind: srvRowEntry, entry: e})
			}
		}
		var groups []string
		seen := map[string]bool{}
		for _, e := range m.serversView {
			if e.Group == "" || seen[e.Group] {
				continue
			}
			seen[e.Group] = true
			groups = append(groups, e.Group)
		}
		for _, g := range groups {
			rows = append(rows, srvRow{kind: srvRowGroup, group: g})
			if m.collapsed[g] {
				continue
			}
			for _, e := range m.serversView {
				if e.Group == g {
					rows = append(rows, srvRow{kind: srvRowEntry, entry: e, group: g})
				}
			}
		}
	}
	m.srvRows = rows
	m.srvCursor = -1
	if havePrev {
		m.srvCursor = m.findRowIdentity(prev)
	}
	if m.srvCursor < 0 {
		m.srvCursor = m.fallbackCursorRow()
	}
	m.srvCursor = clamp(m.srvCursor, 0, max(0, len(m.srvRows)-1))
}

// fallbackCursorRow picks where the display cursor should sit when the old
// row vanished (filter change, refresh, group collapse): the row of the
// currently selected entry, else the first entry row, else row 0.
func (m *Model) fallbackCursorRow() int {
	if m.serverCursor >= 0 && m.serverCursor < len(m.serversView) {
		alias := m.serversView[m.serverCursor].Alias
		for i, r := range m.srvRows {
			if r.kind == srvRowEntry && r.entry.Alias == alias {
				return i
			}
		}
	}
	for i, r := range m.srvRows {
		if r.kind == srvRowEntry {
			return i
		}
	}
	return 0
}

// snapCursorToSelection moves the display cursor onto the row of the entry
// that serverCursor selects (used after save/reload when the selection
// index changed but rows were already rebuilt).
func (m *Model) snapCursorToSelection() {
	if m.serverCursor >= 0 && m.serverCursor < len(m.serversView) {
		alias := m.serversView[m.serverCursor].Alias
		for i, r := range m.srvRows {
			if r.kind == srvRowEntry && r.entry.Alias == alias {
				m.srvCursor = i
				return
			}
		}
	}
	m.srvCursor = m.fallbackCursorRow()
}

func (m *Model) rebuildFileView() {
	m.refreshFiles()
	q := strings.ToLower(m.fileFilter)
	if q == "" {
		return
	}
	var out []fs.Item
	for _, it := range m.fileView {
		if strings.Contains(strings.ToLower(it.Name), q) {
			out = append(out, it)
		}
	}
	m.fileView = out
	m.fileCur = clamp(m.fileCur, 0, max(0, len(m.fileView)-1))
}

// ----- Mouse handling -------------------------------------------------------

func (m *Model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	_, y := msg.X, msg.Y
	// While an overlay is open mouse events only target the overlay.
	if m.form != nil {
		return m.handleFormMouse(msg)
	}
	if m.gform != nil {
		return m.handleGroupFormMouse(msg)
	}
	switch msg.Type {
	case tea.MouseLeft:
		// Detect double-click via timestamp+position heuristic.
		double := !m.lastClickAt.IsZero() &&
			time.Since(m.lastClickAt) < 500*time.Millisecond &&
			m.lastClickX == msg.X && m.lastClickY == msg.Y
		m.lastClickAt = time.Now()
		m.lastClickX = msg.X
		m.lastClickY = msg.Y
		switch m.section {
		case SectionServers:
			// Row 0 is the product title; the whole remaining area above the
			// status bar is the focusable row list (starting with the single
			// ops row that carries [NEW] and [EDIT] side by side). A single
			// click on either op activates it; entry rows select on click and
			// open on double click; folder rows select on click and fold on
			// double click.
			if y >= 1 && y < m.height-1 {
				return m, m.clickServerListRow(msg.X, y-1, double)
			}
			return m, nil
		case SectionFiles:
			if y >= 1 && y < m.height-1 {
				m.fileCur = clamp(y-1, 0, max(0, len(m.fileView)-1))
				if double {
					return m, m.enterSelectedFile()
				}
			}
			return m, nil
		}
		// SectionBoth: server rows start immediately after the server header.
		if y >= 1 && y < m.serverListEnd() {
			m.focus = paneServers
			return m, m.clickServerListRow(msg.X, y-1, double)
		}
		if y >= m.serverListEnd()+2 && y < m.height-1 {
			m.focus = paneFiles
			m.fileCur = clamp(y-(m.serverListEnd()+2), 0, max(0, len(m.fileView)-1))
			if double {
				return m, m.enterSelectedFile()
			}
			return m, nil
		}
	case tea.MouseWheelUp, tea.MouseWheelDown:
		k := tea.KeyMsg{Type: tea.KeyDown}
		if msg.Type == tea.MouseWheelUp {
			k = tea.KeyMsg{Type: tea.KeyUp}
		}
		if m.focus == paneServers {
			return m.handleServersKey(k)
		}
		return m.handleFilesKey(k)
	}
	return m, nil
}

// clickServerListRow maps a click at visual row vis (0 = first row of the
// servers list area, taking the scroll offset into account) and column x
// onto the row model and applies the row's mouse behaviour. x matters only
// for the ops row, where it decides between the [NEW] (left) and [EDIT]
// (right) half. double distinguishes the second click of a double-click pair.
// A standalone double-click on a server returns tea.Quit (exec hand-off);
// everything else keeps the TUI running and returns nil.
func (m *Model) clickServerListRow(x, vis int, double bool) tea.Cmd {
	if len(m.srvRows) == 0 {
		return nil
	}
	abs := m.srvTop + vis
	if abs >= len(m.srvRows) {
		abs = len(m.srvRows) - 1
	}
	if abs < 0 {
		abs = 0
	}
	m.srvCursor = abs
	r := m.srvRows[abs]
	switch r.kind {
	case srvRowOps:
		// Fixed hit zones match the static ops render: segment 1 (marker +
		// "[NEW]", 7 cells) + one space = columns 0..7, then segment 2.
		// The ▶ marker of the armed op sits inside each segment.
		if x < 8 {
			m.openServerForm(servers.Entry{Source: "extra"}, false)
		} else if !m.editMode {
			// M6: clicking [EDIT] enters edit mode (already editing → no-op).
			m.toggleEditMode()
		}
	case srvRowGroup:
		m.syncSelectionToRow()
		if double {
			m.toggleGroup(r.group)
		}
	case srvRowEntry:
		m.syncSelectionToRow()
		if double {
			if m.editMode {
				// While editing, double-clicking a server edits it instead of
				// opening a connection (avoids accidental ssh launches).
				m.editEntry(r.entry)
			} else {
				return m.openEntry(r.entry)
			}
		}
	}
	return nil
}

// clampSrvTop keeps the scroll window anchored on the display cursor when
// the row list is taller than the visible area (vis rows fit).
func (m *Model) clampSrvTop(vis int) {
	n := len(m.srvRows)
	if n <= vis || vis <= 0 {
		m.srvTop = 0
		return
	}
	if m.srvCursor < m.srvTop {
		m.srvTop = m.srvCursor
	}
	if m.srvCursor >= m.srvTop+vis {
		m.srvTop = m.srvCursor - vis + 1
	}
}

// serverListEnd returns the row index of the section divider (between
// servers and the file header). Layout:
// server header (0), servers (1..sEnd-1), divider (sEnd),
// file header (sEnd+1), files (sEnd+2..height-2), status (last).
func (m *Model) serverListEnd() int {
	usable := m.height - 1 // leave 1 for status
	if usable < 6 {
		usable = 6
	}
	serverH := usable / 2
	return 1 + serverH
}

// ----- Operations ----------------------------------------------------------

// openSelectedServer opens the currently selected server entry (kept as the
// entry-level entry point used by tests and legacy callers; keyboard Enter
// goes through activateServerRow so ops/folder rows get their own actions).
// The returned command is tea.Quit when a standalone open replaced the TUI.
func (m *Model) openSelectedServer() tea.Cmd {
	if len(m.serversView) == 0 {
		m.status = "no servers to open (refresh with r)"
		return nil
	}
	if m.serverCursor < 0 || m.serverCursor >= len(m.serversView) {
		m.serverCursor = clamp(m.serverCursor, 0, max(0, len(m.serversView)-1))
	}
	return m.openEntry(m.serversView[m.serverCursor])
}

// openEntry connects to one real server row. The behaviour depends on the
// launch mode (M8):
//
//   - standalone (SectionBoth): prepare the ssh argv for exec(2) and return
//     tea.Quit — the TUI exits and main() replaces this process with the ssh
//     client, so the current pane keeps the session and the nav never
//     returns.
//   - sidebar (--section servers|files): open a brand-new full Zellij tab
//     whose initial command is the ssh argv (`zellij action new-tab --name
//     <tab> -- ssh …`); the nav stays alive and its status squares keep
//     polling the tab list.
//
// The built-in localhost never opens an ssh session — openLocalhost handles
// it, and the built-in herdr row opens its own floating pane via openHerdr.
// A stored password without sshpass on PATH turns into a status hint in
// both modes instead of a launch.
func (m *Model) openEntry(e servers.Entry) tea.Cmd {
	if e.Source == "builtin" {
		if servers.IsHerdr(e) {
			return m.openHerdr()
		}
		return m.openLocalhost()
	}
	if m.standalone() {
		return m.openStandaloneSsh(e)
	}
	// Sidebar mode: ask Zellij for a brand-new tab.
	plan := action.Build(e)
	if plan.NeedSshpass {
		// A password is stored for this server but sshpass is not on PATH:
		// do not launch anything — tell the user how to fix it.
		m.status = "password set but sshpass missing — sudo apt install sshpass (or use keys)"
		return nil
	}
	if len(plan.Steps) == 0 {
		// Outside Zellij there is no session to add a tab to.
		m.status = fmt.Sprintf("→ %s: not inside Zellij — start zellij to open it in a new tab", plan.TabName)
		return nil
	}
	m.ctx = servers.TabName(e)
	if m.wss != nil {
		m.wss.SetContext(m.ctx)
	}
	m.status = fmt.Sprintf("→ %s  (new tab)", plan.TabName)
	if err := action.Run(context.Background(), plan); err != nil {
		m.noteOpenResult(e.Alias, err)
		m.status = "exec failed: " + err.Error()
		return nil
	}
	// Record the successful open: clears any red and — in fallback mode
	// (outside Zellij / poll failing) — turns the square green.
	m.noteOpenResult(e.Alias, nil)
	return nil
}

// openStandaloneSsh prepares the exec argv for an ssh entry in standalone
// mode. argv[0] is resolved to an absolute path (syscall.Exec does no PATH
// lookup); when nothing runnable exists the user gets a status hint and the
// TUI stays up.
func (m *Model) openStandaloneSsh(e servers.Entry) tea.Cmd {
	argv, need := action.SshExecArgv(e)
	if need {
		m.status = "password set but sshpass missing — sudo apt install sshpass (or use keys)"
		return nil
	}
	if len(argv) == 0 {
		m.status = "ssh client not found on PATH (install openssh-client)"
		return nil
	}
	m.ctx = servers.TabName(e)
	return m.prepareExec(argv)
}

// openLocalhost handles the built-in localhost row. In standalone mode it
// asks the TUI to exit and exec the local shell (fish preferred — see
// action.LocalShellPath) in the current pane; in sidebar mode it opens a
// new Zellij tab named "local" running that shell. When no shell can be
// resolved (no fish, no $SHELL) it degrades to a status hint.
func (m *Model) openLocalhost() tea.Cmd {
	if m.standalone() {
		shell := action.LocalShellPath()
		if shell == "" {
			m.status = "localhost: no local shell (install fish or set $SHELL)"
			return nil
		}
		m.ctx = "local"
		return m.prepareExec([]string{shell})
	}
	if !action.InZellij() {
		m.status = "localhost: not inside Zellij — start zellij to open a local shell in a new tab"
		return nil
	}
	plan := action.LocalhostNewTabPlan(action.LocalShell())
	m.ctx = "local"
	if m.wss != nil {
		m.wss.SetContext(m.ctx)
	}
	m.status = "→ local shell (new tab)"
	if err := action.Run(context.Background(), plan); err != nil {
		m.noteOpenResult("localhost", err)
		m.status = "exec failed: " + err.Error()
		return nil
	}
	m.noteOpenResult("localhost", nil)
	return nil
}

// openHerdr handles the built-in herdr row (M9). In every launch mode —
// sidebar (--section servers|files) and standalone (SectionBoth) — the open
// is the same: pop a floating pane named "herdr" running the bundle's
// ~/.local/bin/wz-herdr.sh over the current Zellij session, then keep the
// nav pane alive. herdr is never exec'd into the current pane, so unlike
// every other standalone row this never requests tea.Quit. Outside Zellij,
// or when the helper script is missing, it degrades to a status hint.
func (m *Model) openHerdr() tea.Cmd {
	if !action.InZellij() {
		m.status = "herdr: not inside Zellij — start zellij to open the agent workbench"
		return nil
	}
	script := action.HerdrScriptPath()
	if _, err := os.Stat(script); err != nil {
		m.status = "herdr: missing " + script + " (reinstall the nav bundle)"
		return nil
	}
	m.ctx = "herdr"
	if m.wss != nil {
		m.wss.SetContext(m.ctx)
	}
	m.status = "→ herdr (floating pane)"
	if err := action.Run(context.Background(), action.HerdrFloatingPlan()); err != nil {
		m.noteOpenResult("herdr", err)
		m.status = "exec failed: " + err.Error()
		return nil
	}
	// herdr's square is always green regardless of the ledger; the record
	// only exists so a failed open attempt stays visible in the maps.
	m.noteOpenResult("herdr", nil)
	return nil
}

// enterSelectedFile runs the action of the currently highlighted file row.
// Folders/".." navigate; files open according to the launch mode: sidebar
// keeps the historical wz-open.sh / xdg-open spawn (floating nvim/vim in
// Zellij), while standalone exits the TUI and execs the editor in the
// current pane. The returned tea.Cmd is non-nil (tea.Quit) exactly for that
// standalone exec path.
func (m *Model) enterSelectedFile() tea.Cmd {
	if len(m.fileView) == 0 {
		return nil
	}
	name := m.fileView[m.fileCur].Name
	if name == ".." {
		_ = m.files.Parent()
		m.refreshFiles()
		return nil
	}
	full := m.files.PathOf(name)
	if m.files.IsDir(name) {
		if _, err := m.files.Into(name); err != nil {
			m.status = err.Error()
			return nil
		}
		m.refreshFiles()
		return nil
	}
	// File.
	if m.standalone() {
		argv := editorArgv(full)
		if len(argv) == 0 {
			m.status = "no editor found (install nvim or vim)"
			return nil
		}
		return m.prepareExec(argv)
	}
	// Sidebar: prefer wz-open.sh (floating nvim/vim), then xdg-open.
	if path, err := exec.LookPath("wz-open.sh"); err == nil {
		cmd := exec.Command(path, full) //nolint:gosec
		_ = cmd.Start()
		go func() { _ = cmd.Wait() }()
		m.status = "opened: " + full
		return nil
	}
	if path, err := exec.LookPath("xdg-open"); err == nil {
		cmd := exec.Command(path, full) //nolint:gosec
		_ = cmd.Start()
		go func() { _ = cmd.Wait() }()
		m.status = "xdg-open: " + full
		return nil
	}
	m.status = "no opener (install ~/.local/bin/wz-open.sh)"
	return nil
}

// editorArgv mirrors wz-open.sh's editor detection for the standalone file
// open: nvim preferred, vim as the fallback. argv[0] is an absolute path
// (syscall.Exec performs no $PATH lookup). nil when neither editor exists.
func editorArgv(path string) []string {
	for _, ed := range []string{"nvim", "vim"} {
		if p, err := exec.LookPath(ed); err == nil {
			return []string{p, path}
		}
	}
	return nil
}

// prepareExec is the standalone-mode cleanup + exec hand-off. It stops the
// ws server (the deferred Stop in main() will never run — syscall.Exec
// replaces the process without unwinding defers), records argv for main(),
// and returns tea.Quit so bubbletea performs its normal shutdown (restores
// the terminal, leaves the alt screen) before main() execs the target.
func (m *Model) prepareExec(argv []string) tea.Cmd {
	if m.wss != nil {
		m.wss.Stop()
	}
	m.execArgv = append(m.execArgv[:0], argv...)
	return tea.Quit
}

// ExecArgv returns the argv that should replace this process after the TUI
// exits (standalone open), or nil for a normal quit. main() calls
// syscall.Exec with it once prog.Run has returned.
func (m *Model) ExecArgv() []string {
	return append([]string(nil), m.execArgv...)
}

func (m *Model) reloadServers() {
	sshPath, extraPath := servers.SSHConfigPath(), servers.ExtraListPath()
	if m.sshPathOverride != "" {
		sshPath = m.sshPathOverride
	}
	if m.extraPathOverride != "" {
		extraPath = m.extraPathOverride
	}
	m.serversAll = servers.LoadFrom(sshPath, extraPath)
	m.rebuildServerView()
}

func (m *Model) refreshFiles() {
	items, err := m.files.ListDir()
	if err != nil {
		m.status = "fs: " + err.Error()
		return
	}
	if m.files.Current != "/" {
		items = append([]fs.Item{{Name: "..", IsDir: true}}, items...)
	}
	m.fileView = items
	if m.fileFilter != "" {
		m.rebuildFileView()
		return
	}
	m.fileCur = clamp(m.fileCur, 0, max(0, len(m.fileView)-1))
}

// ----- View -----------------------------------------------------------------

func (m *Model) View() string {
	var b strings.Builder
	if m.width == 0 {
		m.width = 80
	}
	if m.height == 0 {
		m.height = 24
	}

	// There is no separate product title row: the pane header is the title.
	// This keeps stacked Zellij panes compact and avoids decorative borders.
	switch m.section {
	case SectionServers:
		// Single servers mode: product title row, then the focusable row
		// list (starting with the combined [NEW]/[EDIT] ops row) filling the
		// remaining height above the status.
		b.WriteString(truncRunes(serversTitle(), m.width))
		b.WriteByte('\n')
		rows := m.height - 2
		if rows < 1 {
			rows = 1
		}
		m.clampSrvTop(rows)
		for i := 0; i < rows; i++ {
			b.WriteString(m.renderServerRow(i))
			if i < rows-1 {
				b.WriteByte('\n')
			}
		}
	case SectionFiles:
		b.WriteString(truncRunes(m.sectionHeader(paneFiles), m.width))
		b.WriteByte('\n')
		rows := m.height - 2
		if rows < 1 {
			rows = 1
		}
		for i := 0; i < rows; i++ {
			b.WriteString(m.renderFileRow(i))
			if i < rows-1 {
				b.WriteByte('\n')
			}
		}
	default:
		// The two panes are stacked; the server pane ends at the divider.
		sEnd := m.serverListEnd()
		b.WriteString(truncRunes(m.sectionHeader(paneServers), m.width))
		b.WriteByte('\n')
		serverH := sEnd - 1
		m.clampSrvTop(serverH)
		for i := 0; i < serverH; i++ {
			b.WriteString(m.renderServerRow(i))
			b.WriteByte('\n')
		}
		b.WriteString(pad("", m.width, '·'))
		b.WriteByte('\n')
		b.WriteString(truncRunes(m.sectionHeader(paneFiles), m.width))
		b.WriteByte('\n')
		fileRows := m.height - sEnd - 2
		if fileRows < 1 {
			fileRows = 1
		}
		for i := 0; i < fileRows; i++ {
			b.WriteString(m.renderFileRow(i))
			if i < fileRows-1 {
				b.WriteByte('\n')
			}
		}
	}

	// Status bar.
	b.WriteByte('\n')
	b.WriteString(m.statusLine())
	view := b.String()
	if m.form != nil {
		view = m.withFormOverlay(view)
	}
	if m.gform != nil {
		view = m.withGroupOverlay(view)
	}
	return view
}

// sectionHeader returns the compact, stable label for a pane. The files
// header is the current path itself, left aligned with the pane border.
func (m *Model) sectionHeader(p pane) string {
	if p == paneServers {
		return "neilwz-servers"
	}
	if m.files == nil {
		return "neilwz-files"
	}
	return m.displayPath()
}

func (m *Model) displayPath() string {
	if m.files == nil {
		return "neilwz-files"
	}
	path := m.files.Current
	if home, err := os.UserHomeDir(); err == nil {
		home, err = filepath.Abs(home)
		if err == nil {
			if path == home {
				return "~"
			}
			if strings.HasPrefix(path, home+string(filepath.Separator)) {
				return "~" + string(filepath.Separator) + strings.TrimPrefix(path, home+string(filepath.Separator))
			}
		}
	}
	return path
}

// renderServerRow draws the row at visual index i of the servers pane
// (absolute row srvTop+i once the row model is built). When srvRows is
// empty — raw models built directly in tests, or legacy callers — it falls
// back to the plain entry list so rendering stays deterministic.
func (m *Model) renderServerRow(i int) string {
	if len(m.srvRows) > 0 {
		abs := m.srvTop + i
		if abs < 0 || abs >= len(m.srvRows) {
			return blank(m.width)
		}
		r := m.srvRows[abs]
		switch r.kind {
		case srvRowOps:
			return m.renderOpsRow(abs == m.srvCursor)
		case srvRowGroup:
			return m.renderFolderRow(r.group, abs == m.srvCursor)
		default:
			return m.renderEntryRow(r.entry, r.group != "", abs == m.srvCursor)
		}
	}
	if i >= len(m.serversView) {
		return blank(m.width)
	}
	return m.renderEntryRow(m.serversView[i], false, i == m.serverCursor)
}

// rowPointer renders the pointer cell of one focused servers-pane row: a
// blank cell when the row is not focused, ▶/▷ otherwise. In M6 edit mode the
// glyph is painted green (see pointerCell) so the active mode is obvious
// even in a long list. The glyph always occupies exactly one terminal cell —
// colour escapes live inside it, away from the padded row body.
func (m *Model) rowPointer(focused bool) string {
	if !focused {
		return " "
	}
	g := "▶"
	if m.focus != paneServers {
		g = "▷"
	}
	return pointerCell(g, m.editMode)
}

// renderEntryRow renders one server entry in the M5 column order —
// pointer (▶/▷ or a blank placeholder) + one space + status square + name —
// so the coloured square hugs the left edge of the name instead of sitting
// in front of the pointer. Group children are indented two cells inside the
// name area so they align under the group folder label. Colour escapes live
// only in the glyph cell; the padded body is plain text, so column
// alignment and truncation never see ANSI.
func (m *Model) renderEntryRow(e servers.Entry, child, focused bool) string {
	ptr := m.rowPointer(focused)
	desc := ""
	if e.Desc != "" {
		desc = "  (" + e.Desc + ")"
	}
	name := e.Alias
	if child {
		name = "  " + name
	}
	avail := m.width - 3
	if avail < 1 {
		avail = 1
	}
	body := truncRunes(padRunes(name+desc, avail), avail)
	return ptr + " " + svGlyph(m.serverState(e)) + body
}

// renderOpsRow draws the single ops line directly below the title: both
// [NEW] and [EDIT] sit side by side on the same row (M5: no more wrapping
// onto two list rows). While the ops row is focused, the armed op
// (opsSel, toggled with ←/→ or h/l) carries the ▶/▷ pointer; Enter triggers
// the armed op. In M6 edit mode the labels render with angle brackets —
// <NEW> <EDIT> — so the current mode is visible at a glance. Segments keep
// fixed widths so click hit zones never move.
func (m *Model) renderOpsRow(focused bool) string {
	mark := func(armed bool) string {
		if !focused || !armed {
			return "  "
		}
		return m.rowPointer(true) + " "
	}
	open, close := "[", "]"
	if m.editMode {
		open, close = "<", ">"
	}
	line := mark(m.opsSel == opNew) + open + "NEW" + close + " " + mark(m.opsSel == opEdit) + open + "EDIT" + close
	// The line may carry SGR colour inside the armed pointer, so pad/truncate
	// on visible cells (escapes count as zero width) instead of raw runes.
	return cellText(line, m.width)
}

// renderFolderRow renders a group folder: "▾ group/" while expanded,
// "▸ group/" while collapsed. Folder rows omit the status square (a single
// blank cell keeps the square column aligned with server rows); the ▾/▸
// arrow occupies the same two cells a child entry reserves for its indent,
// so the group label lines up exactly under the child names.
func (m *Model) renderFolderRow(group string, focused bool) string {
	ptr := m.rowPointer(focused)
	arrow := "▾ "
	if m.collapsed[group] {
		arrow = "▸ "
	}
	avail := m.width - 3
	if avail < 1 {
		avail = 1
	}
	body := truncRunes(padRunes(arrow+group+"/", avail), avail)
	// pointer + space + (blank square column) + arrow/group.
	return ptr + "  " + body
}

func (m *Model) renderFileRow(i int) string {
	if i >= len(m.fileView) {
		return blank(m.width)
	}
	it := m.fileView[i]
	marker := "  "
	if i == m.fileCur {
		if m.focus == paneFiles {
			marker = "▶ "
		} else {
			marker = "▷ "
		}
	}
	name := it.Name
	if it.IsDir {
		name += "/"
	}
	// Icons occupy one terminal cell (two columns) and are followed by a
	// fixed space, keeping names aligned across a long file list.
	label := " " + marker + icon.Aligned(it.Name, it.IsDir, it.IsLink) + name
	return truncRunes(pad(label, m.width, ' '), m.width)
}

func (m *Model) statusLine() string {
	filter := ""
	if m.editingFilter {
		filter = " filter:" + m.currentFilter() + "_"
	} else if m.currentFilter() != "" {
		filter = " filter:" + m.currentFilter()
	}
	// In single-section mode there is only one pane, so "pane=N" is
	// noise; we surface "mode=servers|files" instead to make it easy
	// to tell two stacked nav4neil instances apart from the status line.
	var left string
	switch m.section {
	case SectionServers, SectionFiles:
		left = fmt.Sprintf(" ctx=%s mode=%s ws=%s%s", m.ctx, m.section, m.wssURL(), filter)
	default:
		left = fmt.Sprintf(" ctx=%s pane=%d ws=%s%s", m.ctx, m.focus+1, m.wssURL(), filter)
	}
	right := m.status
	if right != "" {
		right = "  |  " + right
	}
	row := left + right
	return trunc(pad(row, m.width, ' '), m.width)
}

func (m *Model) wssURL() string {
	if m.wss == nil {
		return "off"
	}
	return m.wss.URL()
}

// ----- helpers -------------------------------------------------------------

// serversTitle is the single-section servers product title (row 0).
func serversTitle() string { return "serv4neil" }

func pad(s string, w int, fill byte) string {
	if len(s) >= w {
		return s
	}
	return s + strings.Repeat(string(fill), w-len(s))
}

// padRunes pads s with trailing spaces until it occupies w runes (terminal
// cells). Unlike pad it counts multibyte glyphs — e.g. the ▶ pointer — as a
// single cell, so rows keep their exact intended width.
func padRunes(s string, w int) string {
	n := len([]rune(s))
	if n >= w {
		return s
	}
	return s + strings.Repeat(" ", w-n)
}

// skipSGR returns the index just past the SGR sequence starting at s[i]
// (s[i] == '\x1b', "...m"). If no terminating 'm' exists it consumes the
// rest of the string.
func skipSGR(s string, i int) int {
	j := i + 1
	for j < len(s) && s[j] != 'm' {
		j++
	}
	if j < len(s) {
		j++ // past the 'm'
	}
	return j
}

// cellText pads/truncates s to w visible terminal cells, treating SGR colour
// escapes (\x1b[…m) as zero width so coloured rows keep their exact layout.
// Escapes are consumed whole (they never count toward the cell budget), so
// truncation can never split one open — no stray colour can bleed into the
// padded remainder.
func cellText(s string, w int) string {
	if w <= 0 {
		return ""
	}
	var b strings.Builder
	cells := 0
	for i := 0; i < len(s) && cells < w; {
		if s[i] == '\x1b' {
			j := skipSGR(s, i)
			b.WriteString(s[i:j])
			i = j
			continue
		}
		_, sz := utf8.DecodeRuneInString(s[i:])
		if sz <= 0 {
			sz = 1
		}
		b.WriteString(s[i : i+sz])
		cells++
		i += sz
	}
	for cells < w {
		b.WriteByte(' ')
		cells++
	}
	return b.String()
}

func blank(w int) string {
	if w <= 0 {
		return ""
	}
	return strings.Repeat(" ", w)
}

func trunc(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if len(s) <= w {
		return s
	}
	return s[:w]
}

func truncRunes(s string, w int) string {
	if w <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= w {
		return s
	}
	return string(runes[:w])
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
