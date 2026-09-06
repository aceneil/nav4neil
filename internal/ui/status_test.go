package ui

import (
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/aceneil/nav4neil/internal/servers"
)

var ansiRe = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// stripANSI removes SGR colour sequences so row-layout assertions can look
// at the visible text regardless of the colour mode the test runs in.
func stripANSI(s string) string { return ansiRe.ReplaceAllString(s, "") }

// forceColor re-enables colour output for the duration of a test regardless
// of the ambient TERM/NO_COLOR the test runner inherited.
func forceColor(t *testing.T) {
	t.Helper()
	t.Setenv("NO_COLOR", "")
	t.Setenv("TERM", "xterm-256color")
}

func TestParseTabNames_KDL_Zellij045(t *testing.T) {
	// Realistic `zellij action dump-layout` output (Zellij 0.45.1): tab
	// nodes declare the tab's own name; pane nodes may carry their own
	// name= attributes that must NOT be reported as tabs.
	in := `layout {
    cwd "/home/neil"
    tab name="Workspace" hide_floating_panes=true {
        pane size=1 borderless=true {
            plugin location="zellij:tab-bar"
        }
        pane split_direction="vertical" {
            pane size="20%" {
                pane name="🚀 快捷服务" size="40%"
                pane command="yazi" name="📁 文件浏览" size="60%" {
                    start_suspended true
                }
            }
            pane name="💻 终端" size="80%"
        }
    }
    tab name="local" focus=true {
        pane command="fish"
    }
    new_tab_template {
        pane size=1 borderless=true {
            plugin location="zellij:tab-bar"
        }
        pane
    }
}
`
	got := parseTabNames(in)
	want := []string{"Workspace", "local"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseTabNames(KDL) = %v, want %v", got, want)
	}
}

func TestParseTabNames_JSON(t *testing.T) {
	object := `{"tabs":[{"name":"local","position":0,"active":true},{"name":"db1"}],"panes":[]}`
	if got := parseTabNames(object); !reflect.DeepEqual(got, []string{"local", "db1"}) {
		t.Fatalf("object JSON = %v", got)
	}
	array := `[{"name":"web01","tab_id":0},{"name":"web02","tab_id":1}]`
	if got := parseTabNames(array); !reflect.DeepEqual(got, []string{"web01", "web02"}) {
		t.Fatalf("array JSON = %v", got)
	}
}

func TestParseTabNames_EmptyOrGarbage(t *testing.T) {
	for _, in := range []string{"", "   \n  ", "not a layout at all", "tab without name {", "{bad json"} {
		if got := parseTabNames(in); len(got) != 0 {
			t.Fatalf("parseTabNames(%q) = %v, want none", in, got)
		}
	}
}

// mkModel builds a Model with the status fields preset so state-machine
// cases read clearly.
func mkStatusModel(live bool, open []string, okAliases, failedAliases []string) *Model {
	m := &Model{
		liveTabs: live,
		svOK:     map[string]bool{},
		svFailed: map[string]bool{},
	}
	for _, a := range okAliases {
		m.svOK[a] = true
	}
	for _, a := range failedAliases {
		m.svFailed[a] = true
	}
	if live {
		m.tabsOpen = map[string]bool{}
		for _, t := range open {
			m.tabsOpen[t] = true
		}
	}
	return m
}

func TestServerState_StateMachine(t *testing.T) {
	web := servers.Entry{Alias: "web01", Source: "ssh", SshAlias: "web01"}
	db := servers.Entry{Alias: "root@db1", Source: "extra", SshAlias: "root@db1"}
	local := servers.Entry{Alias: "localhost", Source: "builtin"}

	cases := []struct {
		name string
		m    *Model
		e    servers.Entry
		want svState
	}{
		{"fallback default → yellow", mkStatusModel(false, nil, nil, nil), web, svYellow},
		{"no dump data (empty tab set) → yellow", mkStatusModel(true, nil, nil, nil), web, svYellow},
		{"live tab open → green", mkStatusModel(true, []string{"web01"}, nil, nil), web, svGreen},
		{"localhost matches tab 'local'", mkStatusModel(true, []string{"local"}, nil, nil), local, svGreen},
		{"extra root@db1 matches tab db1", mkStatusModel(true, []string{"db1"}, nil, nil), db, svGreen},
		{"tab wins over stale failure", mkStatusModel(true, []string{"web01"}, nil, []string{"web01"}), web, svGreen},
		{"failed + tab absent → red", mkStatusModel(true, []string{"other"}, nil, []string{"web01"}), web, svRed},
		{"fallback last open failed → red", mkStatusModel(false, nil, nil, []string{"web01"}), web, svRed},
		{"fallback last open ok → green", mkStatusModel(false, nil, []string{"web01"}, nil), web, svGreen},
		{"live mode ignores stale ledger success", mkStatusModel(true, []string{"other"}, []string{"web01"}, nil), web, svYellow},
		{"multi-server: green + yellow together", mkStatusModel(true, []string{"web01"}, nil, nil), db, svYellow},
	}
	for _, c := range cases {
		if got := c.m.serverState(c.e); got != c.want {
			t.Errorf("%s: serverState() = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestStatusGlyph_Coloured(t *testing.T) {
	forceColor(t)
	cases := map[svState]string{
		svGreen:  "\x1b[38;5;78m▮\x1b[0m",
		svYellow: "\x1b[38;5;214m▮\x1b[0m",
		svRed:    "\x1b[38;5;203m▮\x1b[0m",
	}
	for st, want := range cases {
		if got := svGlyph(st); got != want {
			t.Errorf("svGlyph(%v) = %q, want %q", st, got, want)
		}
	}
}

func TestStatusGlyph_DegradesToDot(t *testing.T) {
	// Narrowest environments must degrade to a plain middle dot.
	t.Setenv("NO_COLOR", "")
	t.Setenv("TERM", "dumb")
	if got := svGlyph(svGreen); got != "·" {
		t.Fatalf("TERM=dumb: svGlyph = %q, want ·", got)
	}
	t.Setenv("TERM", "xterm-256color")
	t.Setenv("NO_COLOR", "1")
	if got := svGlyph(svRed); got != "·" {
		t.Fatalf("NO_COLOR: svGlyph = %q, want ·", got)
	}
	t.Setenv("NO_COLOR", "")
	t.Setenv("TERM", "")
	if got := svGlyph(svYellow); got != "·" {
		t.Fatalf("TERM empty: svGlyph = %q, want ·", got)
	}
}

func TestRenderServerRow_StatusSquareLayout(t *testing.T) {
	forceColor(t)
	m := &Model{
		width:        30,
		height:       12,
		focus:        paneServers,
		serverCursor: 0,
		serversView: []servers.Entry{
			{Alias: "localhost", Source: "builtin"},
			{Alias: "web01", Source: "ssh", SshAlias: "web01"},
		},
	}
	row0 := stripANSI(m.renderServerRow(0))
	if !strings.HasPrefix(row0, "▮ ▶ localhost") {
		t.Fatalf("focused row must be '▮ ▶ alias…', got %q", row0)
	}
	row1 := stripANSI(m.renderServerRow(1))
	if !strings.HasPrefix(row1, "▮   web01") {
		t.Fatalf("unfocused row must keep the alias column aligned, got %q", row1)
	}
	if len([]rune(row0)) != m.width || len([]rune(row1)) != m.width {
		t.Fatalf("rows must stay exactly %d cells wide (got %d / %d)", m.width, len([]rune(row0)), len([]rune(row1)))
	}
	// Colour mode must not change the visible layout.
	t.Setenv("TERM", "dumb")
	dumb := stripANSI(m.renderServerRow(0))
	if !strings.HasPrefix(dumb, "· ▶ localhost") {
		t.Fatalf("degraded row must start with dot + pointer, got %q", dumb)
	}
}

func TestRenderServerRow_GreenForOpenTab(t *testing.T) {
	forceColor(t)
	m := mkStatusModel(true, []string{"web01"}, nil, nil)
	m.width = 30
	m.serversView = []servers.Entry{{Alias: "web01", Source: "ssh", SshAlias: "web01"}}
	row := m.renderServerRow(0)
	if !strings.HasPrefix(stripANSI(row), "▮") {
		t.Fatalf("row should show a square, got %q", row)
	}
}

func TestModel_TabsPollMessageUpdatesStatus(t *testing.T) {
	web := servers.Entry{Alias: "web01", Source: "ssh", SshAlias: "web01"}

	// Tab appears → failure cleared, square green.
	m := &Model{
		inZellij:   true,
		svFailed:   map[string]bool{"web01": true},
		serversAll: []servers.Entry{web},
	}
	m.Update(tabsPollMsg{ok: true, tabs: []string{"web01"}})
	if !m.liveTabs {
		t.Fatalf("successful poll must set liveTabs")
	}
	if m.svFailed["web01"] {
		t.Fatalf("present tab must clear the failure flag")
	}
	if st := m.serverState(web); st != svGreen {
		t.Fatalf("server with open tab: state = %v, want svGreen", st)
	}

	// Poll ok but tab missing → failure stays red.
	m.svFailed["web01"] = true
	m.Update(tabsPollMsg{ok: true, tabs: []string{"other"}})
	if st := m.serverState(web); st != svRed {
		t.Fatalf("failed server without tab: state = %v, want svRed", st)
	}

	// Dump failure → fall back to ledger (last success = green).
	m2 := &Model{
		inZellij:   true,
		svOK:       map[string]bool{"web01": true},
		serversAll: []servers.Entry{web},
	}
	m2.Update(tabsPollMsg{ok: false})
	if m2.liveTabs {
		t.Fatalf("failed poll must clear liveTabs")
	}
	if st := m2.serverState(web); st != svGreen {
		t.Fatalf("fallback ledger success: state = %v, want svGreen", st)
	}
}
