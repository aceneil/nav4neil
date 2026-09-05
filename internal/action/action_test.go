package action

import (
	"testing"

	"github.com/wezterm4neil/wznav/internal/servers"
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
