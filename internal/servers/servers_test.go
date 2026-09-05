package servers

import (
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
