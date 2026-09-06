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

	files      *fs.Browser
	fileView   []fs.Item
	fileCur    int
	fileFilter string

	editingFilter bool

	// form is non-nil while the NEW/EDIT server overlay is open. All normal
	// keys and mouse events route to the form until it is closed.
	form *serverForm

	// Path overrides (tests): when empty the real $HOME/.ssh/config and
	// $XDG_CONFIG_HOME/wezterm4neil/servers.txt are used.
	sshPathOverride   string
	extraPathOverride string

	status string // transient bottom-line message (right side)

	ctx string // "local" or last-selected ssh alias
	wss *ws.Server

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
		section: section,
		ctx:     "local",
		wss:     wss,
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

// Init is required by bubbletea. We tick every 750ms so the transient
// status message can self-clear after a few seconds.
func (m *Model) Init() tea.Cmd {
	return tea.Tick(750*time.Millisecond, func(t time.Time) tea.Msg { return tickMsg(t) })
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
	default:
		return m, nil
	}
}

func (m *Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// If we're in filter-edit mode, every printable key edits the buffer.
	if m.editingFilter {
		return m.handleFilterKey(msg)
	}
	// While the server form is open it owns every key until closed.
	if m.form != nil {
		return m.handleFormKey(msg)
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
// to the area that actually refreshes.
func helpText(s Section) string {
	switch s {
	case SectionServers:
		return "j/k move · enter connect · n new · e edit · / filter · r refresh · ? help · q quit"
	case SectionFiles:
		return "j/k move · enter open · h/l parent/into · / filter · r refresh dir · ? help · q quit"
	default:
		return "j/k move · enter open · / filter · 1/2 panes · tab cycle · r refresh · ? help · q quit"
	}
}

func (m *Model) handleServersKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "j", "down":
		if m.serverCursor < len(m.serversView)-1 {
			m.serverCursor++
		}
	case "k", "up":
		if m.serverCursor > 0 {
			m.serverCursor--
		}
	case "g":
		m.serverCursor = 0
	case "G":
		if len(m.serversView) > 0 {
			m.serverCursor = len(m.serversView) - 1
		}
	case "n", "N":
		m.openServerForm(servers.Entry{Source: "extra"}, false)
	case "e", "E":
		m.editSelectedServer()
	case "enter":
		m.openSelectedServer()
	}
	return m, nil
}

// editSelectedServer opens the EDIT overlay for the current row. Only
// servers.txt-managed rows (Source "extra") are editable: ssh-config hosts
// and the built-in localhost get an explanatory hint instead.
func (m *Model) editSelectedServer() {
	if len(m.serversView) == 0 {
		m.status = "nothing to edit"
		return
	}
	sel := m.serversView[m.serverCursor]
	switch sel.Source {
	case "builtin":
		m.status = "localhost is built-in and fixed; use [NEW] to add another server"
		return
	case "ssh":
		m.status = "ssh config hosts are not in servers.txt; use [NEW] to add a copy"
		return
	}
	m.openServerForm(sel, true)
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
		m.enterSelectedFile()
	case "enter":
		m.enterSelectedFile()
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
		m.serverCursor = clamp(m.serverCursor, 0, max(0, len(m.serversView)-1))
		return
	}
	var out []servers.Entry
	for _, e := range m.serversAll {
		if strings.Contains(strings.ToLower(e.Alias), q) ||
			strings.Contains(strings.ToLower(e.Desc), q) {
			out = append(out, e)
		}
	}
	m.serversView = out
	m.serverCursor = clamp(m.serverCursor, 0, max(0, len(m.serversView)-1))
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
	// While the server form is open mouse events only target the overlay.
	if m.form != nil {
		return m.handleFormMouse(msg)
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
			// Row 0 is the product title; row 1 is [NEW] [EDIT]; rows >= 2
			// are the server list; the final row is the status bar.
			if y == 1 {
				switch opsRowHit(msg.X) {
				case "new":
					m.openServerForm(servers.Entry{Source: "extra"}, false)
				case "edit":
					m.editSelectedServer()
				}
				return m, nil
			}
			if y >= 2 && y < m.height-1 {
				m.serverCursor = clamp(y-2, 0, max(0, len(m.serversView)-1))
				if double {
					m.openSelectedServer()
				}
			}
			return m, nil
		case SectionFiles:
			if y >= 1 && y < m.height-1 {
				m.fileCur = clamp(y-1, 0, max(0, len(m.fileView)-1))
				if double {
					m.enterSelectedFile()
				}
			}
			return m, nil
		}
		// SectionBoth: server rows start immediately after the server header.
		if y >= 1 && y < m.serverListEnd() {
			m.focus = paneServers
			m.serverCursor = clamp(y-1, 0, max(0, len(m.serversView)-1))
			if double {
				m.openSelectedServer()
			}
			return m, nil
		}
		if y >= m.serverListEnd()+2 && y < m.height-1 {
			m.focus = paneFiles
			m.fileCur = clamp(y-(m.serverListEnd()+2), 0, max(0, len(m.fileView)-1))
			if double {
				m.enterSelectedFile()
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

func (m *Model) openSelectedServer() {
	if len(m.serversView) == 0 {
		m.status = "no servers to open (refresh with r)"
		return
	}
	sel := m.serversView[m.serverCursor]
	plan := action.Build(sel)
	if plan.NeedSshpass {
		// A password is stored for this server but sshpass is not on PATH:
		// do not launch anything — tell the user how to fix it.
		m.status = "password set but sshpass missing — sudo apt install sshpass (or use keys)"
		return
	}
	m.ctx = servers.TabName(sel)
	if m.wss != nil {
		m.wss.SetContext(m.ctx)
	}
	if sel.Source == "builtin" && !plan.UseZellij {
		m.status = "localhost: opened shell in current pane (not Zellij)"
	} else {
		m.status = fmt.Sprintf("→ %s  (zellij=%s)", plan.TabName, plan.Detected)
	}
	if err := action.Run(context.Background(), plan); err != nil {
		m.status = "exec failed: " + err.Error()
	}
}

func (m *Model) enterSelectedFile() {
	if len(m.fileView) == 0 {
		return
	}
	name := m.fileView[m.fileCur].Name
	if name == ".." {
		_ = m.files.Parent()
		m.refreshFiles()
		return
	}
	full := m.files.PathOf(name)
	if m.files.IsDir(name) {
		if _, err := m.files.Into(name); err != nil {
			m.status = err.Error()
			return
		}
		m.refreshFiles()
		return
	}
	// File: prefer wz-open.sh, then xdg-open.
	if path, err := exec.LookPath("wz-open.sh"); err == nil {
		cmd := exec.Command(path, full) //nolint:gosec
		_ = cmd.Start()
		go func() { _ = cmd.Wait() }()
		m.status = "opened: " + full
		return
	}
	if path, err := exec.LookPath("xdg-open"); err == nil {
		cmd := exec.Command(path, full) //nolint:gosec
		_ = cmd.Start()
		go func() { _ = cmd.Wait() }()
		m.status = "xdg-open: " + full
		return
	}
	m.status = "no opener (install ~/.local/bin/wz-open.sh)"
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
		// Single servers mode: product title row, [NEW] [EDIT] ops row,
		// then the server list fills the remaining height above the status.
		b.WriteString(truncRunes(serversTitle(), m.width))
		b.WriteByte('\n')
		b.WriteString(truncRunes(serversOpsBar(), m.width))
		b.WriteByte('\n')
		rows := m.height - 3
		if rows < 1 {
			rows = 1
		}
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

func (m *Model) renderServerRow(i int) string {
	if i >= len(m.serversView) {
		return blank(m.width)
	}
	e := m.serversView[i]
	marker := "  "
	if i == m.serverCursor {
		if m.focus == paneServers {
			marker = "▶ "
		} else {
			marker = "▷ "
		}
	}
	desc := ""
	if e.Desc != "" {
		desc = "  (" + e.Desc + ")"
	}
	// Source labels are redundant in the compact server row; descriptions
	// remain useful as they carry the human-friendly server context.
	return truncRunes(pad(marker+e.Alias+desc, m.width, ' '), m.width)
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

// serversOpsBar renders the [NEW] [EDIT] action row (row 1). The two hit
// zones used by opsRowHit must match these offsets.
func serversOpsBar() string { return " [NEW]  [EDIT]" }

// opsRowHit maps an x coordinate on the servers ops row to a button:
// "new", "edit" or "" (outside both zones).
func opsRowHit(x int) string {
	switch {
	case x >= 1 && x < 6:
		return "new"
	case x >= 8 && x < 14:
		return "edit"
	}
	return ""
}

func pad(s string, w int, fill byte) string {
	if len(s) >= w {
		return s
	}
	return s + strings.Repeat(string(fill), w-len(s))
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
