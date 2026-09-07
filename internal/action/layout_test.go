package action

import (
	"reflect"
	"strings"
	"testing"

	"github.com/aceneil/nav4neil/internal/servers"
)

// sidebarDump is a realistic `zellij action dump-layout` output for the
// WezTerm4Neil sidebar tab (Zellij 0.45.1 KDL, as verified by running it):
// nav4neil's two stacked panes live in the left 20% column, plugin bars sit
// at the top/bottom, and the terminal panes occupy the right area.
const sidebarDump = `layout {
    cwd "/home/neil"
    tab name="Workspace" {
        pane size=1 borderless=true {
            plugin location="zellij:tab-bar"
        }
        pane split_direction="vertical" {
            pane size="20%" {
                pane command="nav4neil" name="nav4neil-servers" size="40%"
                pane command="nav4neil" name="nav4neil-files" size="60%" {
                    start_suspended true
                }
            }
            pane size="80%" {
                pane name="💻 终端"
            }
        }
        pane size=1 borderless=true {
            plugin location="zellij:status-bar"
        }
    }
}
`

// twoRightDump grows the sidebar to three right panes of different widths
// (30% / 20% / 30%) so the biggest one sits two columns away from nav.
const twoRightDump = `layout {
    tab name="Workspace" focus=true {
        pane size=1 borderless=true {
            plugin location="zellij:tab-bar"
        }
        pane split_direction="vertical" {
            pane size="20%" {
                pane name="nav-servers" size="50%"
                pane name="nav-files" size="50%"
            }
            pane size="30%" {
                pane command="ssh" name="db1"
            }
            pane size="20%" {
                pane command="ssh" name="web01"
            }
            pane size="30%" {
                pane command="fish" name="local-2"
            }
        }
        pane size=1 borderless=true {
            plugin location="zellij:status-bar"
        }
    }
}
`

func TestAnalyzeLayout_SingleRightPaneIsOneMoveAway(t *testing.T) {
	tgt := AnalyzeLayout(sidebarDump)
	if !tgt.UsableTarget() {
		t.Fatalf("sidebar layout must be usable, got %+v", tgt)
	}
	if tgt.RightMoves != 1 {
		t.Fatalf("RightMoves = %d, want 1 (the right pane is directly right of nav)", tgt.RightMoves)
	}
	if tgt.BestName != "💻 终端" {
		t.Fatalf("BestName = %q, want 💻 终端", tgt.BestName)
	}
}

func TestAnalyzeLayout_PicksBiggestRightPaneAcrossTwoColumns(t *testing.T) {
	tgt := AnalyzeLayout(twoRightDump)
	if !tgt.UsableTarget() {
		t.Fatalf("two-right dump must be usable, got %+v", tgt)
	}
	// Biggest panes: db1 (30%) and local-2 (30%) tie; leftmost wins → db1
	// at column 1 → one right move. The 20% web01 column between nav and
	// local-2 is single-pane, so local-2 would be reachable with 2 moves too.
	if tgt.RightMoves != 1 || tgt.BestName != "db1" {
		t.Fatalf("got RightMoves=%d BestName=%q, want 1/db1", tgt.RightMoves, tgt.BestName)
	}
}

func TestAnalyzeLayout_IgnoresLeftColumnInterference(t *testing.T) {
	// Two stacked nav panes (40/60) inside the left 20% column must never
	// be picked; the single 80% right pane is the only candidate.
	tgt := AnalyzeLayout(sidebarDump)
	if !tgt.UsableTarget() || tgt.RightMoves != 1 {
		t.Fatalf("left column interference must not affect target: %+v", tgt)
	}
	if strings.Contains(tgt.BestName, "nav4neil") {
		t.Fatalf("nav pane must never be the right target: %+v", tgt)
	}
}

func TestAnalyzeLayout_LegacyStackColumnsAreNotWalkable(t *testing.T) {
	// The M6 bug left sessions with stacked=true columns; a stacked column
	// has several panes sharing one rectangle, which makes the pure
	// rightward walk ambiguous → must fall back.
	dump := strings.Replace(sidebarDump,
		`            pane size="80%" {
                pane name="💻 终端"
            }`,
		`            pane size="40%" stacked=true {
                pane name="💻 终端"
                pane name="fish"
            }
            pane size="40%" stacked=true {
                pane name="fish"
                pane expanded=true
            }`, 1)
	tgt := AnalyzeLayout(dump)
	if tgt.UsableTarget() {
		t.Fatalf("stacked legacy columns must be reported unusable, got %+v", tgt)
	}
	if tgt.Reason == "" {
		t.Fatalf("unusable target must carry a reason")
	}
}

func TestAnalyzeLayout_GarbageFallsBack(t *testing.T) {
	for _, in := range []string{"", "not a layout", "{bad json", "layout {\n    tab name=\"x\" {\n"} {
		tgt := AnalyzeLayout(in)
		if tgt.UsableTarget() {
			t.Fatalf("AnalyzeLayout(%q) must be unusable, got %+v", in, tgt)
		}
	}
}

func TestAnalyzeLayout_MultiTabWithoutFocusFallsBack(t *testing.T) {
	dump := `layout {
    tab name="one" {
        pane split_direction="vertical" {
            pane size="20%" { pane }
            pane size="80%" { pane name="r1" }
        }
    }
    tab name="two" {
        pane
    }
}
`
	if tgt := AnalyzeLayout(dump); tgt.UsableTarget() {
		t.Fatalf("multi-tab dump without focus must fall back: %+v", tgt)
	}
}

// ----- M7 plan builders ----------------------------------------------------

func TestSshNewPanePlanTargeted_WalksToBiggestRightPaneAndSplitsDirectionRight(t *testing.T) {
	tgt := AnalyzeLayout(twoRightDump)
	// Force the far column (2 moves) so both the walk and the split show.
	tgt = Target{Usable: true, RightMoves: 2, BestName: "local-2"}
	e := servers.Entry{Alias: "web", Source: "extra", User: "root", Host: "10.0.0.9", Port: 22}
	p := SshNewPanePlanTargeted(e, tgt)
	want := [][]string{
		{"zellij", "action", "move-focus", "right"},
		{"zellij", "action", "move-focus", "right"},
		{"zellij", "action", "new-pane", "--direction", "right", "--", "ssh", "root@10.0.0.9"},
	}
	if !reflect.DeepEqual(p.Steps, want) {
		t.Fatalf("Steps = %#v\nwant %#v", p.Steps, want)
	}
}

func TestSshNewPanePlanTargeted_FallsBackToLegacyWhenUnusable(t *testing.T) {
	e := servers.Entry{Alias: "db1", Source: "extra", User: "root", Host: "10.0.0.5", Port: 2222}
	legacy := SshNewPanePlan(e)
	p := SshNewPanePlanTargeted(e, Target{Reason: "no right-side panes"})
	if !reflect.DeepEqual(p.Steps, legacy.Steps) {
		t.Fatalf("unusable target must keep the legacy M6 steps exactly\n got %#v\nwant %#v", p.Steps, legacy.Steps)
	}
}

func TestLocalhostNewPanePlanTargeted_UsesDirectionRightWithShell(t *testing.T) {
	tgt := Target{Usable: true, RightMoves: 1}
	p := LocalhostNewPanePlanTargeted("fish", tgt)
	want := [][]string{
		{"zellij", "action", "move-focus", "right"},
		{"zellij", "action", "new-pane", "--direction", "right", "--", "fish"},
	}
	if !reflect.DeepEqual(p.Steps, want) {
		t.Fatalf("Steps = %#v, want %#v", p.Steps, want)
	}
}

func TestLocalhostNewPanePlanTargeted_NoShellStillDirectionRight(t *testing.T) {
	tgt := Target{Usable: true, RightMoves: 1}
	p := LocalhostNewPanePlanTargeted("", tgt)
	want := [][]string{
		{"zellij", "action", "move-focus", "right"},
		{"zellij", "action", "new-pane", "--direction", "right"},
	}
	if !reflect.DeepEqual(p.Steps, want) {
		t.Fatalf("Steps = %#v, want %#v", p.Steps, want)
	}
}

func TestSshNewPanePlanTargeted_PasswordStillRidesSshpassArgv(t *testing.T) {
	t.Setenv("PATH", fakeTool(t, "sshpass"))
	tgt := Target{Usable: true, RightMoves: 1}
	e := servers.Entry{Alias: "db1", Source: "extra", User: "root", Host: "10.0.0.5", Port: 22, Password: "pw"}
	p := SshNewPanePlanTargeted(e, tgt)
	last := p.Steps[len(p.Steps)-1]
	want := []string{"zellij", "action", "new-pane", "--direction", "right", "--", "sshpass", "-p", "pw", "ssh", "root@10.0.0.5"}
	if !reflect.DeepEqual(last, want) {
		t.Fatalf("new-pane argv must carry sshpass: %#v want %#v", last, want)
	}
}
