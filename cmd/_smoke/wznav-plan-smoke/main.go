package main

import (
	"fmt"
	"os"

	"github.com/wezterm4neil/wznav/internal/action"
	"github.com/wezterm4neil/wznav/internal/servers"
)

func main() {
	_ = os.Setenv("ZELLIJ", "")
	for _, e := range []servers.Entry{
		{Alias: "github.com", Source: "ssh", SshAlias: "github.com"},
		{Alias: "root@db1.example.com", Source: "extra", SshAlias: "root@db1.example.com", Desc: "prod db"},
	} {
		p := action.Build(e)
		fmt.Printf("%-30s → %v (detected=%s, tab=%s)\n",
			e.Alias, p.Argv, p.Detected, p.TabName)
	}
}
