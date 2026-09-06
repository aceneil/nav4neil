package action

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/aceneil/nav4neil/internal/servers"
)

// fakeTool installs an executable named name in a temp dir and returns that
// dir prepended to PATH, so exec.LookPath sees it.
func fakeTool(t *testing.T, name string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

// emptyPathDir returns a dir with no tools, so both zellij and sshpass lookups fail.
func emptyPathDir(t *testing.T) string {
	t.Helper()
	return t.TempDir()
}

// writeTool installs an executable with the given body in a temp dir and
// returns its absolute path.
func writeTool(t *testing.T, name, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestSshLine_AliasNoPort(t *testing.T) {
	line, need := SshLine(servers.Entry{Alias: "web01", Source: "ssh", SshAlias: "web01"})
	if need {
		t.Fatalf("no password: NeedSshpass must be false")
	}
	if line != "ssh web01" {
		t.Fatalf("line = %q, want %q", line, "ssh web01")
	}
}

func TestSshLine_LegacyAliasIsSshTarget(t *testing.T) {
	line, _ := SshLine(servers.Entry{Alias: "root@db1", Source: "extra", SshAlias: "root@db1"})
	if line != "ssh root@db1" {
		t.Fatalf("line = %q, want %q", line, "ssh root@db1")
	}
}

func TestSshLine_StructuredPortAndUser(t *testing.T) {
	e := servers.Entry{Alias: "db1", Source: "extra", User: "root", Host: "10.0.0.5", Port: 2222}
	line, _ := SshLine(e)
	if line != "ssh -p 2222 root@10.0.0.5" {
		t.Fatalf("line = %q", line)
	}
}

func TestSshLine_NoUserOmitsAt(t *testing.T) {
	e := servers.Entry{Alias: "gw", Source: "extra", Host: "10.0.0.7", Port: 2200}
	line, _ := SshLine(e)
	if line != "ssh -p 2200 10.0.0.7" {
		t.Fatalf("line = %q, want ssh -p 2200 10.0.0.7", line)
	}
}

func TestSshLine_DefaultPortOmitsDashP(t *testing.T) {
	e := servers.Entry{Alias: "web", Source: "extra", User: "deploy", Host: "web1.example.com", Port: 22}
	line, _ := SshLine(e)
	if line != "ssh deploy@web1.example.com" {
		t.Fatalf("line = %q", line)
	}
}

func TestSshLine_PasswordQuotedWithSshpass(t *testing.T) {
	t.Setenv("PATH", fakeTool(t, "sshpass"))
	e := servers.Entry{Alias: "db1", Source: "extra", User: "root", Host: "10.0.0.5", Port: 2222, Password: "s3cr3t"}
	line, need := SshLine(e)
	if need {
		t.Fatalf("sshpass present: NeedSshpass must be false")
	}
	want := "sshpass -p 's3cr3t' ssh -p 2222 root@10.0.0.5"
	if line != want {
		t.Fatalf("line = %q, want %q", line, want)
	}
}

func TestSshLine_PasswordMissingSshpass_Hint(t *testing.T) {
	t.Setenv("PATH", emptyPathDir(t))
	e := servers.Entry{Alias: "db1", Source: "extra", User: "root", Host: "10.0.0.5", Port: 22, Password: "s3cr3t"}
	line, need := SshLine(e)
	if !need {
		t.Fatalf("sshpass missing with password: NeedSshpass must be true")
	}
	if strings.Contains(line, "sshpass") {
		t.Fatalf("hint line must not start sshpass: %q", line)
	}
}

// TestSshWritePlan_WithPaneID pins the exact write sequence typed into the
// right main pane when a concrete pane id was resolved: Ctrl+C first, then
// the ssh line + Enter, both targeted with -p (focus stays in nav4neil).
func TestSshWritePlan_WithPaneID(t *testing.T) {
	e := servers.Entry{Alias: "db1", Source: "extra", User: "root", Host: "10.0.0.5", Port: 2222}
	p := SshWritePlan(e, "1")
	if !p.UseZellij || p.Detected != "no-zellij" {
		t.Fatalf("metadata wrong (no zellij env here): %+v", p)
	}
	if p.TabName != "db1" || p.SshTarget != "root@10.0.0.5" {
		t.Fatalf("metadata wrong: %+v", p)
	}
	want := [][]string{
		{"zellij", "action", "write", "-p", "1", "\u0003"},
		{"zellij", "action", "write", "-p", "1", "ssh -p 2222 root@10.0.0.5\r"},
	}
	if !reflect.DeepEqual(p.Steps, want) {
		t.Fatalf("Steps = %#v\nwant %#v", p.Steps, want)
	}
	for _, s := range p.Steps {
		if strings.Contains(strings.Join(s, " "), "new-tab") {
			t.Fatalf("M5 must never open a new tab: %v", s)
		}
	}
}

func TestSshWritePlan_PasswordSshpassInSequence(t *testing.T) {
	t.Setenv("PATH", fakeTool(t, "sshpass"))
	e := servers.Entry{Alias: "db1", Source: "extra", User: "root", Host: "10.0.0.5", Port: 22, Password: "pw"}
	p := SshWritePlan(e, "terminal_2")
	last := p.Steps[len(p.Steps)-1]
	if last[len(last)-1] != "sshpass -p 'pw' ssh root@10.0.0.5\r" {
		t.Fatalf("write payload must carry sshpass line: %v", last)
	}
}

func TestSshWritePlan_NoPaneIDMovesFocusFirst(t *testing.T) {
	e := servers.Entry{Alias: "web01", Source: "ssh", SshAlias: "web01"}
	p := SshWritePlan(e, "")
	want := [][]string{
		{"zellij", "action", "move-focus", "right"},
		{"zellij", "action", "write", "\u0003"},
		{"zellij", "action", "write", "ssh web01\r"},
	}
	if !reflect.DeepEqual(p.Steps, want) {
		t.Fatalf("Steps = %#v\nwant %#v", p.Steps, want)
	}
}

func TestSshWritePlan_PasswordMissingSshpass_HintPlan(t *testing.T) {
	t.Setenv("PATH", emptyPathDir(t))
	e := servers.Entry{Alias: "db1", Source: "extra", User: "root", Host: "10.0.0.5", Password: "s3cr3t"}
	p := SshWritePlan(e, "1")
	if !p.NeedSshpass || len(p.Steps) != 0 || p.UseZellij {
		t.Fatalf("hint plan shape wrong: %+v", p)
	}
}

func TestBuild_InZellij_SshFallsBackToFocusedPane(t *testing.T) {
	t.Setenv("ZELLIJ", "1")
	e := servers.Entry{Alias: "github.com", Source: "ssh", SshAlias: "github.com"}
	p := Build(e)
	if !p.UseZellij || p.Detected != "zellij" {
		t.Fatalf("expected in-zellij plan: %+v", p)
	}
	if len(p.Steps) != 3 || p.Steps[0][0] != "zellij" {
		t.Fatalf("steps wrong: %#v", p.Steps)
	}
	if !strings.Contains(p.Steps[2][len(p.Steps[2])-1], "ssh github.com") {
		t.Fatalf("ssh payload missing alias: %v", p.Steps[2])
	}
	for _, s := range p.Steps {
		if strings.Contains(strings.Join(s, " "), "new-tab") {
			t.Fatalf("M5 must never open a new tab: %v", s)
		}
	}
}

func TestBuild_OutsideZellij_IsHintOnly(t *testing.T) {
	t.Setenv("ZELLIJ", "")
	t.Setenv("PATH", emptyPathDir(t))
	for _, e := range []servers.Entry{
		{Alias: "localhost", Source: "builtin"},
		{Alias: "github.com", Source: "ssh", SshAlias: "github.com"},
	} {
		p := Build(e)
		if p.UseZellij || len(p.Steps) != 0 {
			t.Fatalf("%s: outside zellij must be hint-only: %+v", e.Alias, p)
		}
		if p.Detected != "no-zellij" {
			t.Fatalf("%s: Detected=%q", e.Alias, p.Detected)
		}
	}
}

func TestBuild_LocalhostInsideZellijMovesFocusRight(t *testing.T) {
	t.Setenv("ZELLIJ", "1")
	p := Build(servers.Entry{Alias: "localhost", Source: "builtin"})
	want := [][]string{{"zellij", "action", "move-focus", "right"}}
	if !reflect.DeepEqual(p.Steps, want) {
		t.Fatalf("Steps = %v, want %v", p.Steps, want)
	}
	if p.TabName != "local" || p.Detected != "zellij" {
		t.Fatalf("metadata wrong: %+v", p)
	}
}

func TestLocalhostPlan_Shape(t *testing.T) {
	p := LocalhostPlan()
	want := [][]string{{"zellij", "action", "move-focus", "right"}}
	if !reflect.DeepEqual(p.Steps, want) {
		t.Fatalf("LocalhostPlan Steps = %v, want %v", p.Steps, want)
	}
	if p.TabName != "local" || !p.UseZellij {
		t.Fatalf("LocalhostPlan metadata wrong: %+v", p)
	}
}

func TestTab_CanonicalNames(t *testing.T) {
	cases := []struct {
		name string
		e    servers.Entry
		want string
	}{
		{"builtin localhost → local", servers.Entry{Alias: "localhost", Source: "builtin"}, "local"},
		{"ssh alias kept", servers.Entry{Alias: "web01", Source: "ssh", SshAlias: "web01"}, "web01"},
		{"extra root@db1 → db1", servers.Entry{Alias: "root@db1", Source: "extra", SshAlias: "root@db1"}, "db1"},
		{"extra plain alias kept", servers.Entry{Alias: "jumpbox", Source: "extra", SshAlias: "jumpbox"}, "jumpbox"},
		{"space sanitized to underscore", servers.Entry{Alias: "my host", Source: "ssh", SshAlias: "my host"}, "my_host"},
		{"all-punct alias falls back to ssh", servers.Entry{Alias: "///", Source: "ssh", SshAlias: "///"}, "ssh"},
	}
	for _, c := range cases {
		if got := Tab(c.e); got != c.want {
			t.Errorf("%s: Tab() = %q, want %q", c.name, got, c.want)
		}
	}
}

// ----- main-pane id parsing -------------------------------------------------

// realSidebarPanes is the terminal-pane subset captured from a live
// sidebar.kdl session (Zellij 0.45.1, `list-panes --all --json`), plus the
// plugin rows for realism. terminal_1 (💻 终端, x=28) is the right main pane.
const realSidebarPanes = `[
  {"id":0,"is_plugin":true,"is_focused":false,"title":"(.) - zellij:link","pane_x":35,"pane_rows":14,"tab_id":0},
  {"id":0,"is_plugin":false,"is_focused":false,"title":"nav4neil-servers","pane_command":"nav4neil --section servers","pane_x":0,"pane_y":1,"pane_rows":14,"tab_id":0},
  {"id":1,"is_plugin":false,"is_focused":true,"title":"💻 终端","pane_command":"fish","pane_x":28,"pane_y":1,"pane_rows":28,"tab_id":0},
  {"id":2,"is_plugin":false,"is_focused":false,"title":"nav4neil-files","pane_command":"nav4neil --section files","pane_x":0,"pane_y":15,"pane_rows":14,"tab_id":0},
  {"id":1,"is_plugin":true,"is_focused":false,"title":"zellij:tab-bar","plugin_url":"zellij:tab-bar","pane_x":0,"pane_rows":1,"tab_id":0}
]`

func TestRightPaneIDFromPanes_RealSidebarFixture(t *testing.T) {
	if got := rightPaneIDFromPanes(realSidebarPanes); got != "1" {
		t.Fatalf("rightPaneIDFromPanes = %q, want 1 (💻 终端)", got)
	}
}

func TestRightPaneIDFromPanes_IgnoresOtherTabs(t *testing.T) {
	// A leftover ssh tab (id 7) sits in tab 1; the nav pane lives in tab 0,
	// so the right main pane of tab 0 (id 1) must still win.
	in := `[
	  {"id":7,"is_plugin":false,"title":"ssh pane in another tab","pane_command":"ssh github.com","pane_x":0,"pane_rows":24,"tab_id":1},
	  {"id":0,"is_plugin":false,"title":"nav4neil-servers","pane_command":"nav4neil --section servers","pane_x":0,"pane_rows":14,"tab_id":0},
	  {"id":1,"is_plugin":false,"is_focused":true,"title":"💻 终端","pane_command":"fish","pane_x":28,"pane_rows":28,"tab_id":0}
	]`
	if got := rightPaneIDFromPanes(in); got != "1" {
		t.Fatalf("rightPaneIDFromPanes = %q, want 1", got)
	}
}

func TestRightPaneIDFromPanes_TiePrefersTaller(t *testing.T) {
	in := `[
	  {"id":0,"is_plugin":false,"title":"nav4neil-servers","pane_command":"nav4neil --section servers","pane_x":0,"pane_rows":14,"tab_id":0},
	  {"id":3,"is_plugin":false,"title":"top right","pane_command":"bash","pane_x":30,"pane_rows":10,"tab_id":0},
	  {"id":4,"is_plugin":false,"title":"main right","pane_command":"fish","pane_x":30,"pane_rows":20,"tab_id":0}
	]`
	if got := rightPaneIDFromPanes(in); got != "4" {
		t.Fatalf("rightPaneIDFromPanes = %q, want 4 (tallest right pane)", got)
	}
}

func TestRightPaneIDFromPanes_NoRightPane(t *testing.T) {
	// Only our own nav panes exist — nothing right of us to write into.
	in := `[
	  {"id":0,"is_plugin":false,"title":"nav4neil-servers","pane_command":"nav4neil --section servers","pane_x":0,"pane_rows":14,"tab_id":0},
	  {"id":2,"is_plugin":false,"title":"nav4neil-files","pane_command":"nav4neil --section files","pane_x":0,"pane_rows":14,"tab_id":0}
	]`
	if got := rightPaneIDFromPanes(in); got != "" {
		t.Fatalf("expected no target pane, got %q", got)
	}
}

func TestRightPaneIDFromLayout_KDLTextHasNoIDs(t *testing.T) {
	// Zellij 0.45.x dump-layout text never carries pane ids (verified against
	// live sessions) — the resolver must return "" and let the fallback steer.
	in := `layout {
    cwd "/home/neil"
    tab name="Workspace" hide_floating_panes=true {
        pane size=1 borderless=true { plugin location="zellij:tab-bar" }
        pane split_direction="vertical" {
            pane size="20%" {
                pane command="nav4neil" name="nav4neil-servers" size="50%" { args "--section" "servers" }
                pane command="nav4neil" name="nav4neil-files" size="50%" { args "--section" "files" }
            }
            pane name="💻 终端" focus=true size="80%"
        }
    }
}`
	if got := rightPaneIDFromLayout(in); got != "" {
		t.Fatalf("KDL text must yield no id, got %q", got)
	}
}

func TestRightPaneIDFromLayout_JSONShape(t *testing.T) {
	// A future JSON layout dump embedding the same pane records as
	// list-panes should resolve through the same picker.
	in := `{"tabs":[{"name":"Workspace","panes":[` +
		`{"id":0,"is_plugin":false,"title":"nav4neil-servers","pane_command":"nav4neil --section servers","pane_x":0,"pane_rows":14,"tab_id":0},` +
		`{"id":1,"is_plugin":false,"title":"💻 终端","pane_command":"fish","pane_x":28,"pane_rows":28,"tab_id":0}` +
		`]}]}`
	if got := rightPaneIDFromLayout(in); got != "1" {
		t.Fatalf("rightPaneIDFromLayout(json) = %q, want 1", got)
	}
}

// ----- execution ------------------------------------------------------------

func TestRun_ZeroExitIsSuccess(t *testing.T) {
	tool := writeTool(t, "okcmd", "#!/bin/sh\nexit 0\n")
	p := Plan{Steps: [][]string{{tool}}}
	if err := Run(context.Background(), p); err != nil {
		t.Fatalf("expected nil for exit 0, got %v", err)
	}
}

func TestRun_RunsStepsInOrder(t *testing.T) {
	dir := t.TempDir()
	log := filepath.Join(dir, "log")
	first := writeTool(t, "step1", "#!/bin/sh\necho 1 >> "+log+"\n")
	second := writeTool(t, "step2", "#!/bin/sh\necho 2 >> "+log+"\n")
	p := Plan{Steps: [][]string{{first}, {second}}}
	if err := Run(context.Background(), p); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	raw, _ := os.ReadFile(log)
	if string(raw) != "1\n2\n" {
		t.Fatalf("steps must run in order, log = %q", raw)
	}
}

func TestRun_NonZeroExitIsFailure(t *testing.T) {
	tool := writeTool(t, "badcmd", "#!/bin/sh\nexit 3\n")
	err := Run(context.Background(), Plan{Steps: [][]string{{tool}}})
	if err == nil {
		t.Fatalf("expected error for exit 3")
	}
	if !strings.Contains(err.Error(), "exit status 3") {
		t.Fatalf("error should mention exit status 3: %v", err)
	}
}

func TestRun_StartFailureIsFailure(t *testing.T) {
	t.Setenv("PATH", emptyPathDir(t))
	err := Run(context.Background(), Plan{Steps: [][]string{{"definitely-not-a-real-tool"}}})
	if err == nil {
		t.Fatalf("expected error for missing binary")
	}
}

func TestRun_EmptyPlanIsFailure(t *testing.T) {
	if err := Run(context.Background(), Plan{}); err == nil {
		t.Fatalf("expected error for empty plan")
	}
}
