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

// ----- ssh argv construction ------------------------------------------------

func TestSshArgv_AliasNoPort(t *testing.T) {
	argv, need := SshArgv(servers.Entry{Alias: "web01", Source: "ssh", SshAlias: "web01"})
	if need {
		t.Fatalf("no password: NeedSshpass must be false")
	}
	want := []string{"ssh", "web01"}
	if !reflect.DeepEqual(argv, want) {
		t.Fatalf("argv = %#v, want %#v", argv, want)
	}
}

func TestSshArgv_LegacyAliasIsSshTarget(t *testing.T) {
	argv, _ := SshArgv(servers.Entry{Alias: "root@db1", Source: "extra", SshAlias: "root@db1"})
	if !reflect.DeepEqual(argv, []string{"ssh", "root@db1"}) {
		t.Fatalf("argv = %#v, want ssh root@db1", argv)
	}
}

func TestSshArgv_StructuredPortAndUser(t *testing.T) {
	e := servers.Entry{Alias: "db1", Source: "extra", User: "root", Host: "10.0.0.5", Port: 2222}
	argv, _ := SshArgv(e)
	if !reflect.DeepEqual(argv, []string{"ssh", "-p", "2222", "root@10.0.0.5"}) {
		t.Fatalf("argv = %#v", argv)
	}
}

func TestSshArgv_NoUserOmitsAt(t *testing.T) {
	e := servers.Entry{Alias: "gw", Source: "extra", Host: "10.0.0.7", Port: 2200}
	argv, _ := SshArgv(e)
	if !reflect.DeepEqual(argv, []string{"ssh", "-p", "2200", "10.0.0.7"}) {
		t.Fatalf("argv = %#v, want ssh -p 2200 10.0.0.7", argv)
	}
}

func TestSshArgv_DefaultPortOmitsDashP(t *testing.T) {
	e := servers.Entry{Alias: "web", Source: "extra", User: "deploy", Host: "web1.example.com", Port: 22}
	argv, _ := SshArgv(e)
	if !reflect.DeepEqual(argv, []string{"ssh", "deploy@web1.example.com"}) {
		t.Fatalf("argv = %#v", argv)
	}
}

// TestSshArgv_PasswordWithSshpass verifies the password reaches sshpass as a
// raw argv element (zellij new-pane execs argv directly — no shell quoting).
func TestSshArgv_PasswordWithSshpass(t *testing.T) {
	t.Setenv("PATH", fakeTool(t, "sshpass"))
	e := servers.Entry{Alias: "db1", Source: "extra", User: "root", Host: "10.0.0.5", Port: 2222, Password: "s3 cr3t'x"}
	argv, need := SshArgv(e)
	if need {
		t.Fatalf("sshpass present: NeedSshpass must be false")
	}
	want := []string{"sshpass", "-p", "s3 cr3t'x", "ssh", "-p", "2222", "root@10.0.0.5"}
	if !reflect.DeepEqual(argv, want) {
		t.Fatalf("argv = %#v, want %#v", argv, want)
	}
}

func TestSshArgv_PasswordMissingSshpass_Hint(t *testing.T) {
	t.Setenv("PATH", emptyPathDir(t))
	e := servers.Entry{Alias: "db1", Source: "extra", User: "root", Host: "10.0.0.5", Port: 22, Password: "s3cr3t"}
	argv, need := SshArgv(e)
	if !need {
		t.Fatalf("sshpass missing with password: NeedSshpass must be true")
	}
	if len(argv) != 0 {
		t.Fatalf("hint argv must be empty, got %#v", argv)
	}
}

// ----- new-pane plans -------------------------------------------------------

// TestSshNewPanePlan_Steps pins the exact M6 sequence: move focus right so
// the split lands in the right main area, then new-pane running the ssh
// argv. Nothing is ever typed into an existing pane.
func TestSshNewPanePlan_Steps(t *testing.T) {
	e := servers.Entry{Alias: "db1", Source: "extra", User: "root", Host: "10.0.0.5", Port: 2222}
	p := SshNewPanePlan(e)
	if !p.UseZellij || p.Detected != "zellij" {
		t.Fatalf("metadata wrong: %+v", p)
	}
	if p.TabName != "db1" || p.SshTarget != "root@10.0.0.5" {
		t.Fatalf("metadata wrong: %+v", p)
	}
	want := [][]string{
		{"zellij", "action", "move-focus", "right"},
		{"zellij", "action", "new-pane", "--", "ssh", "-p", "2222", "root@10.0.0.5"},
	}
	if !reflect.DeepEqual(p.Steps, want) {
		t.Fatalf("Steps = %#v\nwant %#v", p.Steps, want)
	}
	for _, s := range p.Steps {
		joined := strings.Join(s, " ")
		if strings.Contains(joined, "new-tab") || strings.Contains(joined, "write") {
			t.Fatalf("M6 must never open a tab or type into a pane: %v", s)
		}
	}
}

func TestSshNewPanePlan_PasswordSshpassInArgv(t *testing.T) {
	t.Setenv("PATH", fakeTool(t, "sshpass"))
	e := servers.Entry{Alias: "db1", Source: "extra", User: "root", Host: "10.0.0.5", Port: 22, Password: "pw"}
	p := SshNewPanePlan(e)
	last := p.Steps[len(p.Steps)-1]
	want := []string{"zellij", "action", "new-pane", "--", "sshpass", "-p", "pw", "ssh", "root@10.0.0.5"}
	if !reflect.DeepEqual(last, want) {
		t.Fatalf("new-pane argv must carry sshpass: %#v want %#v", last, want)
	}
}

func TestSshNewPanePlan_PasswordMissingSshpass_HintPlan(t *testing.T) {
	t.Setenv("PATH", emptyPathDir(t))
	e := servers.Entry{Alias: "db1", Source: "extra", User: "root", Host: "10.0.0.5", Password: "s3cr3t"}
	p := SshNewPanePlan(e)
	if !p.NeedSshpass || len(p.Steps) != 0 || p.UseZellij {
		t.Fatalf("hint plan shape wrong: %+v", p)
	}
}

func TestBuild_InZellij_SshUsesNewPane(t *testing.T) {
	t.Setenv("ZELLIJ", "1")
	e := servers.Entry{Alias: "github.com", Source: "ssh", SshAlias: "github.com"}
	p := Build(e)
	if !p.UseZellij || p.Detected != "zellij" {
		t.Fatalf("expected in-zellij plan: %+v", p)
	}
	if len(p.Steps) != 2 || p.Steps[0][0] != "zellij" {
		t.Fatalf("steps wrong: %#v", p.Steps)
	}
	if !reflect.DeepEqual(p.Steps[1], []string{"zellij", "action", "new-pane", "--", "ssh", "github.com"}) {
		t.Fatalf("second step must be new-pane with the ssh argv: %#v", p.Steps[1])
	}
	for _, s := range p.Steps {
		joined := strings.Join(s, " ")
		if strings.Contains(joined, "new-tab") || strings.Contains(joined, "write") {
			t.Fatalf("M6 must never open a tab or type into a pane: %v", s)
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

func TestBuild_LocalhostInsideZellijOpensLocalPane(t *testing.T) {
	t.Setenv("ZELLIJ", "1")
	p := Build(servers.Entry{Alias: "localhost", Source: "builtin"})
	if p.TabName != "local" || p.Detected != "zellij" || !p.UseZellij {
		t.Fatalf("metadata wrong: %+v", p)
	}
	if len(p.Steps) != 2 {
		t.Fatalf("localhost must move focus right then new-pane: %#v", p.Steps)
	}
	if !reflect.DeepEqual(p.Steps[0], []string{"zellij", "action", "move-focus", "right"}) {
		t.Fatalf("step 0 must move focus right: %#v", p.Steps[0])
	}
	if p.Steps[1][0] != "zellij" || p.Steps[1][2] != "new-pane" {
		t.Fatalf("step 1 must be a new-pane: %#v", p.Steps[1])
	}
}

func TestLocalhostNewPanePlan_WithShell(t *testing.T) {
	p := LocalhostNewPanePlan("fish")
	want := [][]string{
		{"zellij", "action", "move-focus", "right"},
		{"zellij", "action", "new-pane", "--", "fish"},
	}
	if !reflect.DeepEqual(p.Steps, want) {
		t.Fatalf("LocalhostNewPanePlan Steps = %#v, want %#v", p.Steps, want)
	}
	if p.TabName != "local" || !p.UseZellij {
		t.Fatalf("LocalhostNewPanePlan metadata wrong: %+v", p)
	}
}

func TestLocalhostNewPanePlan_NoShellDefaultsToNewPane(t *testing.T) {
	p := LocalhostNewPanePlan("")
	want := [][]string{
		{"zellij", "action", "move-focus", "right"},
		{"zellij", "action", "new-pane"},
	}
	if !reflect.DeepEqual(p.Steps, want) {
		t.Fatalf("LocalhostNewPanePlan Steps = %#v, want %#v", p.Steps, want)
	}
}

// ----- local shell resolution -----------------------------------------------

func TestLocalShell_PrefersFish(t *testing.T) {
	t.Setenv("PATH", fakeTool(t, "fish"))
	t.Setenv("SHELL", "/bin/bash")
	if got := LocalShell(); got != "fish" {
		t.Fatalf("LocalShell() = %q, want fish (fish wins over $SHELL)", got)
	}
}

func TestLocalShell_FallsBackToShell(t *testing.T) {
	t.Setenv("PATH", emptyPathDir(t))
	t.Setenv("SHELL", "/usr/bin/zsh")
	if got := LocalShell(); got != "/usr/bin/zsh" {
		t.Fatalf("LocalShell() = %q, want $SHELL fallback", got)
	}
}

func TestLocalShell_EmptyWithoutBoth(t *testing.T) {
	t.Setenv("PATH", emptyPathDir(t))
	t.Setenv("SHELL", "")
	if got := LocalShell(); got != "" {
		t.Fatalf("LocalShell() = %q, want empty (zellij default shell)", got)
	}
}

// ----- tab names -------------------------------------------------------------

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

// ----- execution -------------------------------------------------------------

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
