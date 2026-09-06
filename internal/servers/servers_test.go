package servers

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseSSHConfig_BasicAndDedup(t *testing.T) {
	in := `# comment
Host a b
Host c
Host a

Host *
Host ?foo

Host github.com
Host user@db1.example.com
`
	got, err := ParseSSHConfig(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"a", "b", "c", "github.com", "user@db1.example.com"}
	if len(got) != len(want) {
		t.Fatalf("len=%d want=%d (%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("[%d] got %q want %q", i, got[i], want[i])
		}
	}
}

func TestParseSSHConfig_InlineCommentAndIndentation(t *testing.T) {
	in := "   Host   prodbox  # main production\n\tHost devbox\n"
	got, err := ParseSSHConfig(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != "prodbox" || got[1] != "devbox" {
		t.Fatalf("unexpected: %v", got)
	}
}

func TestParseExtraList_PipesAndComments(t *testing.T) {
	in := `
# this is a comment
github.com
root@db1.example.com|prod database
user@k8s-master|control plane

plain alias without pipe
`
	got, err := ParseExtraList(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 4 {
		t.Fatalf("expected 4 entries, got %d: %+v", len(got), got)
	}
	if got[0].Alias != "github.com" || got[0].Desc != "" || got[0].SshAlias != "github.com" {
		t.Fatalf("entry 0 wrong: %+v", got[0])
	}
	if got[1].Alias != "root@db1.example.com" || got[1].Desc != "prod database" {
		t.Fatalf("entry 1 wrong: %+v", got[1])
	}
	if got[1].SshAlias != "root@db1.example.com" {
		t.Fatalf("ssh alias wrong: %+v", got[1])
	}
	if got[3].Alias != "plain alias without pipe" {
		t.Fatalf("entry 3 wrong: %+v", got[3])
	}
}

func TestTabName_Rules(t *testing.T) {
	cases := []struct {
		e    Entry
		want string
	}{
		{Entry{Alias: "github.com", Source: "ssh"}, "github.com"},
		{Entry{Alias: "root@db1.example.com", Source: "extra"}, "db1.example.com"},
		{Entry{Alias: "local-k8s", Source: "extra"}, "local-k8s"},
	}
	for i, c := range cases {
		if got := TabName(c.e); got != c.want {
			t.Fatalf("case %d: got %q want %q", i, got, c.want)
		}
	}
}

func TestSSHArg_Fallback(t *testing.T) {
	e := Entry{Alias: "host1", SshAlias: ""}
	if got := SSHArg(e); got != "host1" {
		t.Fatalf("got %q want host1", got)
	}
	e2 := Entry{Alias: "alias", SshAlias: "user@host"}
	if got := SSHArg(e2); got != "user@host" {
		t.Fatalf("got %q want user@host", got)
	}
}

func TestValidate_Empty(t *testing.T) {
	if err := Validate(nil); err == nil {
		t.Fatalf("expected error for empty list")
	}
	if err := Validate([]Entry{{Alias: "x", Source: "ssh"}}); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
}

func TestInjectBuiltin_LocalhostAndDedup(t *testing.T) {
	got := InjectBuiltin([]Entry{{Alias: "prod", Source: "ssh"}})
	if len(got) != 2 || got[0].Alias != "localhost" || got[0].Desc != "本机" || got[0].Source != "builtin" {
		t.Fatalf("unexpected injection: %+v", got)
	}
	kept := []Entry{{Alias: "LOCALHOST", Source: "ssh"}}
	got = InjectBuiltin(kept)
	if len(got) != 1 || got[0].Source != "ssh" {
		t.Fatalf("localhost should be deduplicated: %+v", got)
	}
}

func TestParseExtraList_StructuredNewFormat(t *testing.T) {
	in := "db1|root@10.0.0.5:2222|prod database|datacenter|s3cr3t\n" +
		"web|deploy@web1.example.com:22||www|pw\n" +
		"bare-host|10.1.2.3:2200|no user|ops|\n" +
		"half|admin@host:2222|no group\n"
	got, err := ParseExtraList(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 4 {
		t.Fatalf("expected 4 entries, got %d: %+v", len(got), got)
	}
	db := got[0]
	if db.Alias != "db1" || db.User != "root" || db.Host != "10.0.0.5" || db.Port != 2222 {
		t.Fatalf("db1 parsed wrong: %+v", db)
	}
	if db.Desc != "prod database" || db.Group != "datacenter" || db.Password != "s3cr3t" {
		t.Fatalf("db1 tail fields wrong: %+v", db)
	}
	if db.Source != "extra" || db.SshAlias != "" {
		t.Fatalf("db1 flags wrong: %+v", db)
	}
	if got[1].User != "deploy" || got[1].Host != "web1.example.com" || got[1].Port != 22 ||
		got[1].Desc != "" || got[1].Group != "www" || got[1].Password != "pw" {
		t.Fatalf("web parsed wrong: %+v", got[1])
	}
	if got[2].User != "" || got[2].Host != "10.1.2.3" || got[2].Port != 2200 || got[2].Password != "" {
		t.Fatalf("bare-host parsed wrong: %+v", got[2])
	}
	// Three-field structured rows (no group/password) are tolerated.
	if got[3].Group != "" || got[3].Password != "" || got[3].Host != "host" {
		t.Fatalf("half parsed wrong: %+v", got[3])
	}
}

func TestParseExtraList_LegacyRowsWithPipesInDesc(t *testing.T) {
	in := "legacy|desc with | pipes inside\n" +
		"root@db1.example.com|prod database\n" +
		"plain\n"
	got, err := ParseExtraList(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 entries, got %d: %+v", len(got), got)
	}
	// Old alias|description rows must stay alias/desc even when the
	// description itself contains pipes or the alias looks like a target.
	if got[0].Alias != "legacy" || got[0].Desc != "desc with | pipes inside" {
		t.Fatalf("legacy pipe desc wrong: %+v", got[0])
	}
	if got[0].Host != "" || got[0].SshAlias != "legacy" {
		t.Fatalf("legacy row must keep alias semantics: %+v", got[0])
	}
	if got[1].Alias != "root@db1.example.com" || got[1].Desc != "prod database" {
		t.Fatalf("legacy target-style row wrong: %+v", got[1])
	}
	if got[2].Alias != "plain" || got[2].Desc != "" {
		t.Fatalf("bare alias wrong: %+v", got[2])
	}
}

func TestParseTarget_Defaults(t *testing.T) {
	cases := []struct {
		in   string
		user string
		host string
		port int
	}{
		{"root@10.0.0.5:2222", "root", "10.0.0.5", 2222},
		{"deploy@host", "deploy", "host", 22},
		{"host:2200", "", "host", 2200},
		{"pure-host", "", "pure-host", 22},
	}
	for _, c := range cases {
		u, h, p := parseTarget(c.in)
		if u != c.user || h != c.host || p != c.port {
			t.Fatalf("parseTarget(%q) = (%q,%q,%d) want (%q,%q,%d)", c.in, u, h, p, c.user, c.host, c.port)
		}
	}
}

func TestEntryLine_RoundTrip(t *testing.T) {
	structured := Entry{Alias: "db1", Desc: "prod db", Group: "dc", User: "root", Host: "10.0.0.5", Port: 2222, Password: "s3cr3t", Source: "extra"}
	line := structured.Line()
	want := "db1|root@10.0.0.5:2222|prod db|dc|s3cr3t"
	if line != want {
		t.Fatalf("Line() = %q want %q", line, want)
	}
	got, err := ParseExtraList(strings.NewReader(line))
	if err != nil || len(got) != 1 {
		t.Fatalf("parse back failed: %+v err=%v", got, err)
	}
	if got[0].Alias != structured.Alias || got[0].User != structured.User ||
		got[0].Host != structured.Host || got[0].Port != structured.Port ||
		got[0].Desc != structured.Desc || got[0].Group != structured.Group ||
		got[0].Password != structured.Password {
		t.Fatalf("round trip mismatch: %+v", got[0])
	}

	// Legacy rows stay in short form and round-trip too.
	legacy := Entry{Alias: "oldbox", Desc: "some notes", Source: "extra", SshAlias: "oldbox"}
	if l := legacy.Line(); l != "oldbox|some notes" {
		t.Fatalf("legacy Line() = %q", l)
	}
	back, _ := ParseExtraList(strings.NewReader(legacy.Line()))
	if len(back) != 1 || back[0].Alias != "oldbox" || back[0].Desc != "some notes" || back[0].SshAlias != "oldbox" {
		t.Fatalf("legacy round trip mismatch: %+v", back)
	}

	// Port zero or invalid is normalised to 22 on serialisation.
	noPort := Entry{Alias: "x", Host: "h", Source: "extra"}
	if l := noPort.Line(); l != "x|h:22|||" {
		t.Fatalf("defaulted-port Line() = %q", l)
	}
}

func TestSaveExtrasTo_LoadFromRoundTrip(t *testing.T) {
	dir := t.TempDir()
	sshFile := filepath.Join(dir, "sshconfig")
	extraFile := filepath.Join(dir, "wezterm4neil", "servers.txt")

	// Seed an old-format file the same way pre-M2 users have it.
	legacy := Entry{Alias: "oldbox", Desc: "notes", Source: "extra", SshAlias: "oldbox"}
	if err := SaveExtrasTo(extraFile, []Entry{legacy}); err != nil {
		t.Fatal(err)
	}
	got := LoadFrom("", extraFile)
	if len(got) != 2 || got[0].Alias != "localhost" || got[1].Alias != "oldbox" {
		t.Fatalf("seed load wrong: %+v", got)
	}

	// Simulate a NEW save: replace the old entry with a structured one plus
	// a second server, then reload through the same file.
	next := []Entry{
		{Alias: "oldbox", Host: "oldbox.example.com", User: "root", Port: 22, Desc: "notes", Source: "extra"},
		{Alias: "db1", User: "root", Host: "10.0.0.5", Port: 2222, Desc: "prod", Group: "dc", Password: "pw", Source: "extra"},
	}
	if err := SaveExtrasTo(extraFile, next); err != nil {
		t.Fatal(err)
	}
	loaded := LoadFrom(sshFile, extraFile)
	// builtin stays first; no legacy leftovers.
	if len(loaded) != 3 || loaded[0].Alias != "localhost" || loaded[0].Source != "builtin" {
		t.Fatalf("post-save load wrong: %+v", loaded)
	}
	if loaded[1].Host != "oldbox.example.com" || loaded[2].Port != 2222 || loaded[2].Password != "pw" {
		t.Fatalf("structured rows wrong: %+v", loaded)
	}

	// The on-disk lines use the exact M2 format.
	raw, err := os.ReadFile(extraFile)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	want := []string{
		"oldbox|root@oldbox.example.com:22|notes||",
		"db1|root@10.0.0.5:2222|prod|dc|pw",
	}
	if len(lines) != 2 || lines[0] != want[0] || lines[1] != want[1] {
		t.Fatalf("file content wrong:\n%s", raw)
	}
}

func TestLoadFrom_ReservesLocalhost(t *testing.T) {
	dir := t.TempDir()
	extraFile := filepath.Join(dir, "servers.txt")
	content := "# user tried to take the builtin name\n" +
		"LOCALHOST|root@x:22|dup||\n" +
		"web|root@w:22|ok||\n"
	if err := os.WriteFile(extraFile, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	got := LoadFrom("", extraFile)
	if len(got) != 2 || got[0].Alias != "localhost" || got[0].Source != "builtin" {
		t.Fatalf("builtin missing/first wrong: %+v", got)
	}
	for _, e := range got[1:] {
		if strings.EqualFold(e.Alias, "localhost") {
			t.Fatalf("user localhost row must be dropped: %+v", got)
		}
	}
}

func TestSSHArg_StructuredUsesHost(t *testing.T) {
	e := Entry{Alias: "web1", User: "deploy", Host: "10.0.0.9", Port: 2222, Source: "extra"}
	if got := SSHArg(e); got != "deploy@10.0.0.9" {
		t.Fatalf("SSHArg() = %q want deploy@10.0.0.9", got)
	}
	e2 := Entry{Alias: "box", Host: "10.0.0.9", Source: "extra"}
	if got := SSHArg(e2); got != "10.0.0.9" {
		t.Fatalf("SSHArg() = %q want bare host", got)
	}
}
