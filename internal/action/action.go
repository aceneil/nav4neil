// Package action decides how to launch a remote ssh session in response to
// a user selecting a server in the nav4neil sidebar.
//
// M6 behaviour — every server (including ssh ones) opens inside a brand-new
// pane in the right main area of the current Zellij tab; full-screen new
// tabs and typing into existing panes are both gone:
//
//   - built-in localhost → `zellij action move-focus right` followed by
//     `zellij action new-pane -- <local shell>` (fish preferred), so the
//     right area gains one more local shell pane per click.
//   - other servers → the same two steps with the ssh argv as the new-pane
//     command (`sshpass -p … ssh -p … user@host`), reusing the existing
//     ssh/sshpass logic. Every click opens its own pane — repeating a click
//     on the same server opens another one, never reuses an old pane.
//   - outside Zellij there is no pane layout to steer → the plan has no
//     steps and the TUI turns it into a status-bar hint.
//   - a stored password requires sshpass on PATH; otherwise the plan carries
//     NeedSshpass=true and the TUI explains how to fix it instead of
//     running anything.
//
// The package only *describes* what to run (one argv per step); the TUI owns
// the exec via Run. Keeping the builders separate lets us unit-test them.
package action

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
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
// "local"; ssh-config hosts keep their alias; servers.txt entries prefer the
// part after '@' (root@db1 → db1), sanitized like the historical opener.
func Tab(e servers.Entry) string {
	if e.Source == "builtin" {
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

// Build computes the plan for connecting to e. Inside Zellij ssh entries get
// the new-pane plan (move-focus right + `new-pane -- ssh …`) and the builtin
// localhost gets a fresh local shell pane. Outside Zellij every entry
// degrades to a hint-only plan (no steps). It never opens new tabs and never
// types into an existing pane.
func Build(e servers.Entry) Plan {
	tab := Tab(e)
	if e.Source == "builtin" {
		if InZellij() {
			return LocalhostNewPanePlan(LocalShell())
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
	return SshNewPanePlan(e)
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
