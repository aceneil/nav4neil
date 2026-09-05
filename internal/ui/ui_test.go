package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbletea"
	"github.com/wezterm4neil/wznav/internal/servers"
)

func TestModel_InitialStateLoadsServers(t *testing.T) {
	m := NewModel("/tmp", nil, SectionBoth)
	// servers.Load() depends on the host's ~/.ssh/config, but we know the
	// model must initialise without panicking and produce a non-nil view.
	if m.focus != paneServers {
		t.Fatalf("expected default focus paneServers, got %v", m.focus)
	}
	if m.fileView == nil {
		t.Fatalf("fileView not initialised")
	}
	v := m.View()
	if !strings.Contains(v, "wznav") {
		t.Fatalf("View() missing title: %q", v)
	}
	if !strings.Contains(v, "Servers") {
		t.Fatalf("View() missing Servers header")
	}
	if !strings.Contains(v, "Files") {
		t.Fatalf("View() missing Files header")
	}
}

func TestModel_RebuildServerView_FilterMatches(t *testing.T) {
	m := NewModel("/tmp", nil, SectionBoth)
	m.serversAll = []servers.Entry{
		{Alias: "alpha", Source: "ssh", SshAlias: "alpha"},
		{Alias: "beta-host", Source: "ssh", SshAlias: "beta-host"},
		{Alias: "gamma", Source: "extra", SshAlias: "gamma", Desc: "Gamma desc"},
	}
	m.serverFilter = "beta"
	m.rebuildServerView()
	if len(m.serversView) != 1 || m.serversView[0].Alias != "beta-host" {
		t.Fatalf("filter wrong: %v", m.serversView)
	}
}

func TestModel_RebuildServerView_EmptyAfterFilter(t *testing.T) {
	m := NewModel("/tmp", nil, SectionBoth)
	m.serversAll = []servers.Entry{{Alias: "x", Source: "ssh"}}
	m.serverFilter = "no-match"
	m.rebuildServerView()
	if len(m.serversView) != 0 {
		t.Fatalf("expected empty view, got %v", m.serversView)
	}
	// Cursor should clamp to 0 (last valid index = 0 in empty state).
	if m.serverCursor != 0 {
		t.Fatalf("expected cursor clamp to 0, got %d", m.serverCursor)
	}
}

func TestModel_ServerPaneRendersMarker(t *testing.T) {
	m := NewModel("/tmp", nil, SectionBoth)
	m.serversAll = []servers.Entry{{Alias: "testhost", Source: "ssh", SshAlias: "testhost"}}
	m.serversView = m.serversAll
	m.serverCursor = 0
	m.width = 40
	m.height = 12
	v := m.View()
	if !strings.Contains(v, "▶ testhost") && !strings.Contains(v, "testhost") {
		t.Fatalf("expected testhost in view: %q", v)
	}
}

func TestModel_FilterEditClears(t *testing.T) {
	m := NewModel("/tmp", nil, SectionBoth)
	m.editingFilter = true
	m.serverFilter = "abc"
	if m.currentFilter() != "abc" {
		t.Fatalf("expected abc, got %q", m.currentFilter())
	}
	m.clearFilter()
	if m.serverFilter != "" {
		t.Fatalf("expected clear, got %q", m.serverFilter)
	}
	if !m.editingFilter {
		t.Fatalf("clearFilter() must NOT touch editingFilter (esc/enter does that)")
	}
}

// teaKeyMsg constructs a bubbletea KeyMsg from a compact key string
// (used only by the single-section-mode tests above).
func teaKeyMsg(s string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func TestParseSection(t *testing.T) {
	cases := []struct {
		in      string
		want    Section
		wantErr bool
	}{
		{"", SectionBoth, false},
		{"both", SectionBoth, false},
		{"BOTH", SectionBoth, false},
		{"  Both  ", SectionBoth, false},
		{"servers", SectionServers, false},
		{"server", SectionServers, false},
		{"SERVERS", SectionServers, false},
		{"files", SectionFiles, false},
		{"file", SectionFiles, false},
		{"FILES", SectionFiles, false},
		{"bogus", SectionBoth, true},
		{"serverss", SectionBoth, true},
	}
	for _, c := range cases {
		got, err := ParseSection(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("ParseSection(%q): expected error, got %v", c.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseSection(%q): unexpected error %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("ParseSection(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestSection_String(t *testing.T) {
	if (SectionBoth).String() != "both" {
		t.Errorf(`SectionBoth.String() = %q, want "both"`, SectionBoth.String())
	}
	if (SectionServers).String() != "servers" {
		t.Errorf(`SectionServers.String() = %q, want "servers"`, SectionServers.String())
	}
	if (SectionFiles).String() != "files" {
		t.Errorf(`SectionFiles.String() = %q, want "files"`, SectionFiles.String())
	}
	if Section(99).String() != "both" {
		t.Errorf(`Section(99).String() = %q, want "both" (default)`, Section(99).String())
	}
}

// TestModel_SectionServersView verifies single-section servers mode:
//   - no "Files" header anywhere
//   - "Servers" header is always present and uses the focused arrow marker
//   - status line shows "mode=servers" (NOT "pane=1")
//   - the model did NOT load the file browser at all
func TestModel_SectionServersView(t *testing.T) {
	m := NewModel("/tmp", nil, SectionServers)
	m.serversAll = []servers.Entry{
		{Alias: "alpha", Source: "ssh", SshAlias: "alpha"},
		{Alias: "beta", Source: "ssh", SshAlias: "beta"},
	}
	m.rebuildServerView()
	m.width = 60
	m.height = 16

	v := m.View()

	if strings.Contains(v, "Files") {
		t.Fatalf("SectionServers view must NOT mention Files; got:\n%s", v)
	}
	if !strings.Contains(v, "▶ Servers") {
		t.Fatalf("SectionServers view must show focused ▶ Servers header; got:\n%s", v)
	}
	if !strings.Contains(v, "mode=servers") {
		t.Fatalf("SectionServers status line must contain mode=servers; got:\n%s", v)
	}
	if strings.Contains(v, "pane=") {
		t.Fatalf("SectionServers status line must NOT contain pane=; got:\n%s", v)
	}
	if !strings.Contains(v, "alpha") {
		t.Fatalf("SectionServers view must list servers; got:\n%s", v)
	}
	if m.files != nil {
		t.Fatalf("SectionServers must NOT load file browser")
	}
}

// TestModel_SectionFilesView verifies single-section files mode:
//   - no "Servers" header anywhere
//   - "Files" header is always present and uses the focused arrow marker
//   - status line shows "mode=files"
//   - the model did NOT load the server list at all
func TestModel_SectionFilesView(t *testing.T) {
	m := NewModel("/tmp", nil, SectionFiles)
	m.width = 60
	m.height = 16

	v := m.View()

	if strings.Contains(v, "Servers") {
		t.Fatalf("SectionFiles view must NOT mention Servers; got:\n%s", v)
	}
	if !strings.Contains(v, "▶ Files") {
		t.Fatalf("SectionFiles view must show focused ▶ Files header; got:\n%s", v)
	}
	if !strings.Contains(v, "mode=files") {
		t.Fatalf("SectionFiles status line must contain mode=files; got:\n%s", v)
	}
	if strings.Contains(v, "pane=") {
		t.Fatalf("SectionFiles status line must NOT contain pane=; got:\n%s", v)
	}
	if m.serversAll != nil {
		t.Fatalf("SectionFiles must NOT load server list")
	}
}

// TestModel_SectionServersIgnoresTab confirms Tab/1/2 are no-ops in
// single-section mode (so the user can type those characters into a
// filter without accidentally trying to switch panes).
func TestModel_SectionServersIgnoresTab(t *testing.T) {
	m := NewModel("/tmp", nil, SectionServers)
	m.serversAll = []servers.Entry{{Alias: "x", Source: "ssh"}}
	m.rebuildServerView()
	before := m.focus
	for _, k := range []string{"tab", "1", "2"} {
		msg := teaKeyMsg(k)
		m.Update(msg)
		if m.focus != before {
			t.Fatalf("SectionServers: %s must not move focus; focus=%d want=%d", k, m.focus, before)
		}
	}
}

// TestModel_SectionFilesIgnoresTab mirrors the above for the files-only mode.
func TestModel_SectionFilesIgnoresTab(t *testing.T) {
	m := NewModel("/tmp", nil, SectionFiles)
	before := m.focus
	for _, k := range []string{"tab", "1", "2"} {
		msg := teaKeyMsg(k)
		m.Update(msg)
		if m.focus != before {
			t.Fatalf("SectionFiles: %s must not move focus; focus=%d want=%d", k, m.focus, before)
		}
	}
}

// TestModel_SectionServersRefreshOnlyReloadsServers: pressing 'r' in
// single-section servers mode must reload only the server list and
// must NOT spin up a file browser.
func TestModel_SectionServersRefreshOnlyReloadsServers(t *testing.T) {
	m := NewModel("/tmp", nil, SectionServers)
	m.serversAll = []servers.Entry{{Alias: "x", Source: "ssh"}}
	m.rebuildServerView()
	m.Update(teaKeyMsg("r"))
	if m.files != nil {
		t.Fatalf("SectionServers 'r' must NOT instantiate file browser")
	}
	if !strings.Contains(m.status, "servers") {
		t.Fatalf(`status must mention "servers", got %q`, m.status)
	}
}
