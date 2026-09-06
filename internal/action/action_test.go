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

func TestBuild_InZellij(t *testing.T) {
	t.Setenv("ZELLIJ", "1")
	p := Build(servers.Entry{Alias: "github.com", Source: "ssh", SshAlias: "github.com"})
	if !p.UseZellij {
		t.Fatalf("expected UseZellij=true, got %+v", p)
	}
	if p.Detected != "zellij" {
		t.Fatalf("Detected=%q", p.Detected)
	}
	if len(p.Argv) < 5 || p.Argv[0] != "zellij" {
		t.Fatalf("argv wrong: %v", p.Argv)
	}
	// last two must be "--" and "ssh <target>"
	last := p.Argv[len(p.Argv)-3:]
	if last[0] != "--" || last[1] != "ssh" || last[2] != "github.com" {
		t.Fatalf("argv tail wrong: %v", p.Argv)
	}
}

func TestBuild_NoZellij_SshAliasWithAt(t *testing.T) {
	t.Setenv("ZELLIJ", "")
	p := Build(servers.Entry{Alias: "root@db1", Source: "extra", SshAlias: "root@db1"})
	// We can't guarantee the host has the zellij binary, but the plan
	// shape should be consistent.
	if p.Detected != "no-zellij-or-zellij-bin" && p.Detected != "no-zellij" {
		t.Fatalf("unexpected Detected=%q", p.Detected)
	}
	if p.SshTarget != "root@db1" {
		t.Fatalf("SshTarget=%q", p.SshTarget)
	}
	if p.TabName != "db1" {
		t.Fatalf("TabName=%q want db1 (after @)", p.TabName)
	}
}

func TestSanitizeTab(t *testing.T) {
	cases := map[string]string{
		"clean":     "clean",
		"a/b":       "a_b",
		"a b c":     "a_b_c",
		"x:y":       "x_y",
		"\"weird\"": "weird",
	}
	for in, want := range cases {
		if got := sanitizeTab(in); got != want {
			t.Fatalf("sanitize(%q)=%q want %q", in, got, want)
		}
	}
}

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

func TestBuild_PasswordWithSshpass(t *testing.T) {
	t.Setenv("ZELLIJ", "")
	t.Setenv("PATH", fakeTool(t, "sshpass"))
	e := servers.Entry{
		Alias: "db1", Source: "extra", User: "root", Host: "10.0.0.5",
		Port: 2222, Password: "s3cr3t", Group: "dc",
	}
	p := Build(e)
	if p.NeedSshpass {
		t.Fatalf("sshpass is present; NeedSshpass must be false: %+v", p)
	}
	want := []string{"sshpass", "-p", "s3cr3t", "ssh", "-p", "2222", "root@10.0.0.5"}
	if !reflect.DeepEqual(p.Argv, want) {
		t.Fatalf("Argv = %v want %v", p.Argv, want)
	}
	if p.SshTarget != "root@10.0.0.5" || p.TabName != "db1" {
		t.Fatalf("metadata wrong: %+v", p)
	}
}

func TestBuild_PasswordMissingSshpass_NeedHint(t *testing.T) {
	t.Setenv("ZELLIJ", "")
	t.Setenv("PATH", emptyPathDir(t))
	e := servers.Entry{
		Alias: "db1", Source: "extra", User: "root", Host: "10.0.0.5",
		Port: 22, Password: "s3cr3t",
	}
	p := Build(e)
	if !p.NeedSshpass {
		t.Fatalf("sshpass missing with a password set: NeedSshpass must be true: %+v", p)
	}
	// Never try to run sshpass when it is not installed.
	if len(p.Argv) == 0 || p.Argv[0] == "sshpass" {
		t.Fatalf("argv must not start with sshpass when it is missing: %v", p.Argv)
	}
	if p.UseZellij {
		t.Fatalf("a hint-only plan must not claim it runs: %+v", p)
	}
}

func TestBuild_NoPassword_DefaultPort22OmitsDashP(t *testing.T) {
	t.Setenv("ZELLIJ", "")
	t.Setenv("PATH", emptyPathDir(t))
	e := servers.Entry{Alias: "web", Source: "extra", User: "deploy", Host: "web1.example.com", Port: 22}
	p := Build(e)
	if p.NeedSshpass {
		t.Fatalf("no password: NeedSshpass must be false: %+v", p)
	}
	want := []string{"ssh", "deploy@web1.example.com"}
	if !reflect.DeepEqual(p.Argv, want) {
		t.Fatalf("Argv = %v want %v", p.Argv, want)
	}
}

func TestBuild_NoPassword_CustomPortAddsDashP(t *testing.T) {
	t.Setenv("ZELLIJ", "")
	t.Setenv("PATH", emptyPathDir(t))
	e := servers.Entry{Alias: "web", Source: "extra", Host: "10.0.0.7", Port: 2200}
	p := Build(e)
	want := []string{"ssh", "-p", "2200", "10.0.0.7"}
	if !reflect.DeepEqual(p.Argv, want) {
		t.Fatalf("Argv = %v want %v", p.Argv, want)
	}
}

func TestBuild_InZellij_WrapsSshpassTail(t *testing.T) {
	t.Setenv("ZELLIJ", "1")
	t.Setenv("PATH", fakeTool(t, "sshpass"))
	e := servers.Entry{
		Alias: "db1", Source: "extra", User: "root", Host: "10.0.0.5",
		Port: 2222, Password: "pw",
	}
	p := Build(e)
	if !p.UseZellij {
		t.Fatalf("inside zellij: UseZellij must be true: %+v", p)
	}
	wantTail := []string{"--", "sshpass", "-p", "pw", "ssh", "-p", "2222", "root@10.0.0.5"}
	gotTail := p.Argv[len(p.Argv)-len(wantTail):]
	if !reflect.DeepEqual(gotTail, wantTail) {
		t.Fatalf("zellij argv tail = %v want %v (full %v)", gotTail, wantTail, p.Argv)
	}
}

// TestTab_CanonicalNames pins down the tab-name rule used both when opening
// a server (Build → new-tab --name) and when matching dump-layout tabs for
// the M3 status squares. localhost must resolve to "local".
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
		{"slash sanitized to underscore", servers.Entry{Alias: "a/b", Source: "ssh", SshAlias: "a/b"}, "a_b"},
		{"all-punct alias falls back to ssh", servers.Entry{Alias: "///", Source: "ssh", SshAlias: "///"}, "ssh"},
	}
	for _, c := range cases {
		if got := Tab(c.e); got != c.want {
			t.Errorf("%s: Tab() = %q, want %q", c.name, got, c.want)
		}
	}
}

// TestTab_AgreesWithBuild guarantees the poll matcher and the tab opener
// can never disagree about which tab name a server uses.
func TestTab_AgreesWithBuild(t *testing.T) {
	t.Setenv("ZELLIJ", "1")
	for _, e := range []servers.Entry{
		{Alias: "localhost", Source: "builtin"},
		{Alias: "github.com", Source: "ssh", SshAlias: "github.com"},
		{Alias: "root@db1", Source: "extra", User: "root", Host: "10.0.0.5", Port: 2222, Password: "pw"},
		{Alias: "my host", Source: "extra", SshAlias: "my host"},
	} {
		p := Build(e)
		if p.TabName != Tab(e) {
			t.Errorf("Build(%q).TabName = %q, Tab() = %q — must match", e.Alias, p.TabName, Tab(e))
		}
	}
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

func TestRun_ZeroExitIsSuccess(t *testing.T) {
	tool := writeTool(t, "okcmd", "#!/bin/sh\nexit 0\n")
	err := Run(context.Background(), Plan{Argv: []string{tool}})
	if err != nil {
		t.Fatalf("expected nil for exit 0, got %v", err)
	}
}

func TestRun_NonZeroExitIsFailure(t *testing.T) {
	tool := writeTool(t, "badcmd", "#!/bin/sh\nexit 3\n")
	err := Run(context.Background(), Plan{Argv: []string{tool}})
	if err == nil {
		t.Fatalf("expected error for exit 3, got nil")
	}
	if !strings.Contains(err.Error(), "exit status 3") {
		t.Fatalf("error should mention exit status 3: %v", err)
	}
}

func TestRun_StartFailureIsFailure(t *testing.T) {
	t.Setenv("PATH", emptyPathDir(t))
	err := Run(context.Background(), Plan{Argv: []string{"definitely-not-a-real-tool"}})
	if err == nil {
		t.Fatalf("expected error for missing binary, got nil")
	}
}

func TestRun_EmptyArgvIsFailure(t *testing.T) {
	if err := Run(context.Background(), Plan{}); err == nil {
		t.Fatalf("expected error for empty argv, got nil")
	}
}
