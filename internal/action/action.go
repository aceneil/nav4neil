// Package action decides how to launch a remote ssh session (or the local
// shell / a file editor) in response to the user picking a server or file in
// nav4neil.
//
// M8 splits the behaviour by launch mode (see cmd/nav4neil and README):
//
//   - Sidebar mode (`--section servers|files`, two instances embedded in the
//     left column of a Zellij layout): picking a server opens a brand-new
//     full Zellij tab — `zellij action new-tab --name <tab> -- <ssh argv>` —
//     and the built-in localhost opens a new tab running the local shell.
//     Build()/SshNewTabPlan()/LocalhostNewTabPlan() describe exactly these
//     actions; no move-focus, no new-pane, no typing into existing panes.
//   - Standalone mode (`nav4neil` with no --section, SectionBoth): the TUI
//     first shuts itself down (bubbletea restores the terminal) and main()
//     then replaces the process with exec(2) — the current pane keeps
//     running `ssh …` (localhost → the local shell) or the file editor.
//     SshExecArgv()/LocalShellPath() build argv whose argv[0] is already an
//     absolute path, because syscall.Exec performs no $PATH lookup.
//
// Outside Zellij a sidebar plan has no steps and the TUI turns it into a
// status-bar hint. A stored password requires sshpass on PATH; otherwise the
// plan carries NeedSshpass=true and the TUI explains how to fix it instead
// of running anything.
//
// The package only *describes* what to run (one argv per step); the TUI owns
// the exec via Run / main() via syscall.Exec. Keeping the builders separate
// lets us unit-test them.
package action

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/aceneil/nav4neil/internal/servers"
)

// Plan describes one concrete execution the TUI should perform: an ordered
// list of argv sequences, each run to completion before the next starts.
// An empty Steps list means "hint only" — the TUI must not exec anything and
// should surface a status-bar message (detected outside Zellij, password
// without sshpass, …).
type Plan struct {
	UseZellij   bool
	NeedSshpass bool // entry has a password but sshpass is missing → hint
	TabName     string
	SshTarget   string
	Detected    string // "zellij" | "no-zellij"
	Steps       [][]string
}

// InZellij reports whether the current process is running inside a
// Zellij session (heuristic: $ZELLIJ env var present).
func InZellij() bool { return os.Getenv("ZELLIJ") != "" }

func envDetected() string {
	if InZellij() {
		return "zellij"
	}
	return "no-zellij"
}

// Tab returns the canonical tab-name token for e (used by dump-layout tab
// polling for the status squares). The built-in localhost resolves to
// "local" and the built-in herdr to "herdr"; ssh-config hosts keep their
// alias; servers.txt entries prefer the part after '@' (root@db1 → db1),
// sanitized like the historical opener.
func Tab(e servers.Entry) string {
	if e.Source == "builtin" {
		if servers.IsHerdr(e) {
			return "herdr"
		}
		return "local"
	}
	tab := sanitizeTab(servers.TabName(e))
	if tab == "" {
		tab = "ssh"
	}
	return tab
}

// targetArg renders the destination argument passed to the ssh client for e:
//
//	ssh-config host / legacy alias → the alias itself
//	structured entry             → [user@]host
//
// Ports are handled separately with -p, never embedded in the target. No
// shell is involved (zellij new-pane execs the command directly), so the
// value is kept raw instead of shell-quoted.
func targetArg(e servers.Entry) string {
	if e.Host != "" {
		if e.User != "" {
			return e.User + "@" + e.Host
		}
		return e.Host
	}
	if e.SshAlias != "" {
		return e.SshAlias
	}
	return e.Alias
}

// SshArgv builds the argv of the process that opens e:
//
//	ssh web01
//	ssh -p 2222 root@10.0.0.5
//	sshpass -p s3cr3t ssh -p 2222 root@10.0.0.5
//
// The second return reports that a stored password exists but sshpass is
// missing on PATH — callers must not run the argv and should surface an
// install hint instead.
func SshArgv(e servers.Entry) ([]string, bool) {
	if e.Password != "" {
		if _, err := exec.LookPath("sshpass"); err != nil {
			return nil, true
		}
	}
	var parts []string
	if e.Password != "" {
		parts = append(parts, "sshpass", "-p", e.Password)
	}
	parts = append(parts, "ssh")
	if e.Port > 0 && e.Port != 22 {
		parts = append(parts, "-p", strconv.Itoa(e.Port))
	}
	parts = append(parts, targetArg(e))
	return parts, false
}

// SshExecArgv is SshArgv prepared for exec(2) in the current pane (M8
// standalone mode). syscall.Exec performs no $PATH lookup, so argv[0] is
// resolved to an absolute path via LookPath. The second return keeps the
// SshArgv contract: true means a stored password exists but sshpass is
// missing (callers must surface the install hint). A nil argv with need
// false means the client binary itself (ssh, or sshpass for a password row)
// could not be resolved on PATH — there is nothing safe to exec.
func SshExecArgv(e servers.Entry) ([]string, bool) {
	argv, need := SshArgv(e)
	if need {
		return nil, true
	}
	bin, err := exec.LookPath(argv[0])
	if err != nil {
		return nil, false
	}
	argv[0] = bin
	return argv, false
}

// SshNewPanePlan builds the steps that open e in a brand-new pane of the
// current Zellij tab's right area (M6): first `zellij action move-focus
// right` so the split happens on the right main pane instead of inside the
// nav pane, then `zellij action new-pane -- ssh …` with e's argv as the
// command. Each call yields its own pane — nothing is typed into an existing
// pane anymore. A password without sshpass yields a NeedSshpass hint plan
// with no steps.
func SshNewPanePlan(e servers.Entry) Plan {
	tab := Tab(e)
	target := servers.SSHArg(e)
	argv, need := SshArgv(e)
	if need {
		return Plan{
			NeedSshpass: true,
			TabName:     tab,
			SshTarget:   target,
			Detected:    envDetected(),
		}
	}
	np := append([]string{"zellij", "action", "new-pane", "--"}, argv...)
	return Plan{
		UseZellij: true,
		TabName:   tab,
		SshTarget: target,
		Detected:  "zellij",
		Steps: [][]string{
			{"zellij", "action", "move-focus", "right"},
			np,
		},
	}
}

// SshNewTabPlan builds the M8 sidebar open plan for one ssh entry: open a
// brand-new full Zellij tab named after the server and let that tab's
// initial command be e's ssh argv (`zellij action new-tab --name <tab> --
// ssh …`). This restores the pre-M6 "click opens its own tab" UX for the
// embedded sidebar — no move-focus, no new-pane, no typing into an existing
// pane. A password without sshpass yields a NeedSshpass hint plan with no
// steps.
func SshNewTabPlan(e servers.Entry) Plan {
	tab := Tab(e)
	target := servers.SSHArg(e)
	argv, need := SshArgv(e)
	if need {
		return Plan{
			NeedSshpass: true,
			TabName:     tab,
			SshTarget:   target,
			Detected:    envDetected(),
		}
	}
	nt := append([]string{"zellij", "action", "new-tab", "--name", tab, "--"}, argv...)
	return Plan{
		UseZellij: true,
		TabName:   tab,
		SshTarget: target,
		Detected:  "zellij",
		Steps:     [][]string{nt},
	}
}

// LocalhostNewTabPlan is the M8 sidebar counterpart of SshNewTabPlan for the
// built-in localhost row: open a new full Zellij tab named "local" running
// the local shell (shell may be "" — then `zellij action new-tab --name
// local` without a command starts the tab with Zellij's default shell).
// Callers must gate on InZellij() first; outside Zellij there is no session
// to add a tab to.
func LocalhostNewTabPlan(shell string) Plan {
	nt := []string{"zellij", "action", "new-tab", "--name", "local"}
	if shell != "" {
		nt = append(nt, "--", shell)
	}
	return Plan{
		UseZellij: true,
		TabName:   "local",
		SshTarget: "local",
		Detected:  "zellij",
		Steps:     [][]string{nt},
	}
}

// moveFocusSteps returns RightMoves× `zellij action move-focus right`,
// the M7 cursor walk that lands focus on the biggest right pane.
func moveFocusSteps(t Target) [][]string {
	steps := make([][]string, 0, t.RightMoves)
	for i := 0; i < t.RightMoves; i++ {
		steps = append(steps, []string{"zellij", "action", "move-focus", "right"})
	}
	return steps
}

// DumpLayout runs `zellij action dump-layout` and returns its stdout. ok is
// false when the dump failed (outside Zellij, no session, timeout, …).
func DumpLayout(ctx context.Context) (out string, ok bool) {
	cctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	raw, err := exec.CommandContext(cctx, "zellij", "action", "dump-layout").Output()
	if err != nil {
		return "", false
	}
	return string(raw), true
}

// targetedNewPaneArgv extracts the command that follows `--` inside one
// legacy new-pane step ("zellij action new-pane -- ssh …"), so the M7
// direction-right variant can reuse the exact same ssh/shell argv.
func targetedNewPaneArgv(step []string) []string {
	for i, a := range step {
		if a == "--" {
			return step[i+1:]
		}
	}
	return nil
}

// targetedNewPane builds the argv of the M7 split: `zellij action new-pane
// --direction right [-- <argv>]`. The explicit direction forces a visible
// tiled split of the focused pane (Zellij's direction-less new-pane instead
// picks the biggest space and starts silently stacking once panes get too
// small to split — the M6 bug this targets).
func targetedNewPane(argv []string) []string {
	np := []string{"zellij", "action", "new-pane", "--direction", "right"}
	if len(argv) > 0 {
		np = append(np, "--")
		np = append(np, argv...)
	}
	return np
}

// SshNewPanePlanTargeted is the M7 open plan for one ssh entry. When the
// layout analysis (AnalyzeLayout) found a usable right target, the plan
// walks focus onto the biggest right pane (RightMoves× move-focus right)
// and then forces a visible tiled split with `new-pane --direction right`
// running e's ssh argv — every click adds one more pane to the right area
// instead of silently stacking into the same column. When the analysis is
// not usable it falls back to the legacy SshNewPanePlan exactly.
func SshNewPanePlanTargeted(e servers.Entry, t Target) Plan {
	fallback := SshNewPanePlan(e)
	if !t.UsableTarget() {
		return fallback
	}
	last := fallback.Steps[len(fallback.Steps)-1]
	return Plan{
		UseZellij: true,
		TabName:   fallback.TabName,
		SshTarget: fallback.SshTarget,
		Detected:  "zellij",
		Steps:     append(moveFocusSteps(t), targetedNewPane(targetedNewPaneArgv(last))),
	}
}

// LocalhostNewPanePlanTargeted is the M7 localhost counterpart of
// SshNewPanePlanTargeted: same cursor walk, then a direction-right split
// running the local shell. Falls back to the legacy
// LocalhostNewPanePlan when the layout analysis is not usable.
func LocalhostNewPanePlanTargeted(shell string, t Target) Plan {
	fallback := LocalhostNewPanePlan(shell)
	if !t.UsableTarget() {
		return fallback
	}
	last := fallback.Steps[len(fallback.Steps)-1]
	return Plan{
		UseZellij: true,
		TabName:   "local",
		SshTarget: "local",
		Detected:  "zellij",
		Steps:     append(moveFocusSteps(t), targetedNewPane(targetedNewPaneArgv(last))),
	}
}

// LocalShell picks the command that opens a local shell pane for the
// built-in localhost row: fish when it is on PATH (the stack's preferred
// shell), otherwise $SHELL, otherwise "" — meaning "let Zellij open its
// configured default shell".
func LocalShell() string {
	if _, err := exec.LookPath("fish"); err == nil {
		return "fish"
	}
	if s := os.Getenv("SHELL"); s != "" {
		return s
	}
	return ""
}

// LocalShellPath resolves the local shell to an absolute path, ready for
// exec(2) in standalone mode (syscall.Exec does no $PATH lookup): fish when
// it is on PATH, otherwise $SHELL. "" when neither exists — callers should
// surface a hint instead of exec'ing.
func LocalShellPath() string {
	if p, err := exec.LookPath("fish"); err == nil {
		return p
	}
	return os.Getenv("SHELL")
}

// LocalhostNewPanePlan returns the plan for the built-in localhost row:
// move focus to the right main area, then open a new pane there running the
// local shell (shell may be "" — then `zellij action new-pane` without a
// command opens Zellij's default shell). Every click opens its own fresh
// local pane; nothing is typed. Callers must gate on InZellij() first;
// outside Zellij there is no pane layout to steer.
func LocalhostNewPanePlan(shell string) Plan {
	np := []string{"zellij", "action", "new-pane"}
	if shell != "" {
		np = append(np, "--", shell)
	}
	return Plan{
		UseZellij: true,
		TabName:   "local",
		SshTarget: "local",
		Detected:  "zellij",
		Steps: [][]string{
			{"zellij", "action", "move-focus", "right"},
			np,
		},
	}
}

// HerdrScriptName is the fixed helper name inside ~/.local/bin that pops
// herdr up as a floating pane over the current Zellij session (it locks
// Zellij's keybindings while herdr owns the pane and unlocks + closes the
// floating pane when herdr exits via Ctrl+b q). It ships with the nav
// bundle; when it is absent the UI shows a status hint instead of running.
const HerdrScriptName = "wz-herdr.sh"

// HerdrScriptPath returns the absolute path of the herdr launcher script:
// $HOME/.local/bin/wz-herdr.sh (honouring $HOME so tests can pin it).
func HerdrScriptPath() string {
	if h, err := os.UserHomeDir(); err == nil {
		return filepath.Join(h, ".local", "bin", HerdrScriptName)
	}
	return HerdrScriptName
}

// HerdrFloatingPlan builds the M9 open plan for the built-in herdr row: pop
// a 96%-sized floating pane named "herdr" over the current Zellij session
// and run the bundle's wz-herdr.sh inside it (`zellij action new-pane
// --floating --close-on-exit --name herdr --width 96% --height 96% --x 2%
// --y 2% -- <script>`). Callers must gate on InZellij() and check that the
// script exists first; outside Zellij there is no session to float over.
func HerdrFloatingPlan() Plan {
	return Plan{
		UseZellij: true,
		TabName:   "herdr",
		SshTarget: "herdr",
		Detected:  "zellij",
		Steps: [][]string{{
			"zellij", "action", "new-pane", "--floating", "--close-on-exit",
			"--name", "herdr", "--width", "96%", "--height", "96%",
			"--x", "2%", "--y", "2%", "--", HerdrScriptPath(),
		}},
	}
}

// Build computes the sidebar-mode plan for connecting to e — the behaviour
// of a `--section servers|files` instance embedded in the left column of a
// Zellij layout. Inside Zellij the entry opens in a brand-new full tab
// (`zellij action new-tab --name <tab> -- ssh …`), and the builtin localhost
// opens a new tab running the local shell (see SshNewTabPlan and
// LocalhostNewTabPlan). Outside Zellij every entry degrades to a hint-only
// plan (no steps). Standalone mode (SectionBoth) does not use Build: it
// exits the TUI and execs in the current pane instead.
func Build(e servers.Entry) Plan {
	tab := Tab(e)
	if e.Source == "builtin" {
		if InZellij() {
			return LocalhostNewTabPlan(LocalShell())
		}
		return Plan{TabName: tab, SshTarget: "local", Detected: "no-zellij"}
	}
	target := servers.SSHArg(e)
	if _, need := SshArgv(e); need {
		return Plan{
			NeedSshpass: true,
			TabName:     tab,
			SshTarget:   target,
			Detected:    envDetected(),
		}
	}
	if !InZellij() {
		return Plan{TabName: tab, SshTarget: target, Detected: "no-zellij"}
	}
	return SshNewTabPlan(e)
}

// ----- execution ------------------------------------------------------------

// Run executes every step of the plan in order. A non-zero exit or a start
// failure on any step stops the sequence and returns the error. A short
// overall timeout per step keeps a stuck child from hanging the TUI; a child
// still running when its deadline hits is treated as launched OK.
func Run(ctx context.Context, p Plan) error {
	if len(p.Steps) == 0 {
		return errors.New("action: empty plan")
	}
	for i, argv := range p.Steps {
		if err := runStep(ctx, argv); err != nil {
			return fmt.Errorf("action step %d (%v): %w", i+1, argv, err)
		}
	}
	return nil
}

func runStep(ctx context.Context, argv []string) error {
	if len(argv) == 0 {
		return errors.New("action: empty argv")
	}
	cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(cctx, argv[0], argv[1:]...) //nolint:gosec
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start %v: %w", argv, err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err == nil {
			return nil
		}
		if cctx.Err() != nil {
			return nil // killed by our own deadline while running → launched OK
		}
		return fmt.Errorf("%v failed: %w", argv, err)
	case <-cctx.Done():
		return nil
	}
}

// sanitizeTab strips characters that confuse Zellij / tmux / shells
// inside a tab name. Keeps the user-friendly alias.
func sanitizeTab(in string) string {
	var b strings.Builder
	b.Grow(len(in))
	for _, r := range in {
		switch r {
		case '/', '\\', ':', '\n', '\t', ' ', '\'', '"':
			b.WriteByte('_')
		default:
			b.WriteRune(r)
		}
	}
	return strings.Trim(b.String(), "_")
}
