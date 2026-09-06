package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/aceneil/nav4neil/internal/action"
	"github.com/aceneil/nav4neil/internal/servers"
)

func main() {
	_ = os.Setenv("ZELLIJ", "")
	for _, e := range []servers.Entry{
		{Alias: "localhost", Source: "builtin"},
		{Alias: "github.com", Source: "ssh", SshAlias: "github.com"},
		{Alias: "root@db1.example.com", Source: "extra", SshAlias: "root@db1.example.com", Desc: "prod db"},
	} {
		p := action.Build(e)
		var steps []string
		for _, s := range p.Steps {
			steps = append(steps, strings.Join(s, " "))
		}
		if len(steps) == 0 {
			steps = []string{"(hint-only — no Zellij session)"}
		}
		fmt.Printf("%-30s → %s (tab=%s, sshpass-hint=%v)\n", e.Alias, strings.Join(steps, " | "), p.TabName, p.NeedSshpass)
	}
}
