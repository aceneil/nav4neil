// Package action decides how to launch a remote ssh session in response to
// a user clicking a server in the nav4neil sidebar.
//
// Rule:
//   - If we are running inside a Zellij pane ($ZELLIJ is set and "zellij"
//     is on PATH) → issue `zellij action new-tab --name <tab> -- ssh <host>`.
//     Each click opens a brand-new tab; the existing layout is preserved.
//   - Otherwise → fall back to `zellij action new-tab` (still tries if
//     the binary is on PATH), and if THAT fails we surface the error so
//     the UI status bar can show it. We deliberately do NOT exec ssh in
//     place: that would replace the nav4neil pane and break the sidebar.
//
// The package only *describes* what to run; main() / the TUI owns the
// exec. Keeping the command builder separate lets us unit-test it.
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

// Plan describes one concrete execution the TUI should perform.
type Plan struct {
	UseZellij   bool     // true → issue "zellij action new-tab ..."
	NeedSshpass bool     // entry has a password but sshpass is missing → hint, don't run
	Argv        []string // exec argv
	TabName     string   // informational only (echoed in status bar)
	SshTarget   string   // informational only
	Detected    string   // "zellij"|"no-zellij"|"no-zellij-or-zellij-bin"
}

// InZellij reports whether the current process is running inside a
// Zellij session (heuristic: $ZELLIJ env var present).
func InZellij() bool { return os.Getenv("ZELLIJ") != "" }

// HaveZellijBin reports whether the `zellij` binary is on PATH.
func HaveZellijBin() bool {
	_, err := exec.LookPath("zellij")
	return err == nil
}

// sshCommand builds the actual ssh argv tail for e:
//
//	sshpass -p <pw> ssh [-p <port>] <target>   (password + sshpass installed)
//	ssh [-p <port>] <target>                   (no password)
//
// The second return reports whether a password exists but sshpass is missing;
// callers must not execute in that case and should surface an install hint.
func sshCommand(e servers.Entry) (argv []string, needSshpass bool) {
	target := servers.SSHArg(e)
	if e.Password != "" {
		if _, err := exec.LookPath("sshpass"); err != nil {
			return []string{"ssh", target}, true
		}
		argv = append(argv, "sshpass", "-p", e.Password)
	}
	argv = append(argv, "ssh")
	if e.Port > 0 && e.Port != 22 {
		argv = append(argv, "-p", strconv.Itoa(e.Port))
	}
	return append(argv, target), false
}

// environmentTag reports how this process will reach the new session.
func environmentTag() string {
	if InZellij() {
		return "zellij"
	}
	if HaveZellijBin() {
		return "no-zellij"
	}
	return "no-zellij-or-zellij-bin"
}

// Tab returns the Zellij tab name that opening e creates (the exact value
// passed to `zellij action new-tab --name`). The built-in localhost entry
// always opens as "local"; ssh-config hosts keep their alias; servers.txt
// entries prefer the part after '@' (via servers.TabName) and go through
// sanitizeTab, matching the historical Build behaviour. Status polling
// compares dump-layout tab names against Tab(e), so a row turns green
// exactly when its own tab exists.
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

// Build computes the Plan for connecting to e. It does not touch the
// filesystem beyond looking at PATH.
func Build(e servers.Entry) Plan {
	if e.Source == "builtin" {
		shell := "bash"
		if _, err := exec.LookPath("fish"); err == nil {
			shell = "fish"
		}
		tab := Tab(e) // built-in localhost opens as tab "local"
		if InZellij() {
			return Plan{UseZellij: true, TabName: tab, Argv: []string{"zellij", "action", "new-tab", "--name", tab, "--", shell}, Detected: "zellij"}
		}
		return Plan{UseZellij: false, TabName: tab, Argv: []string{shell}, Detected: "no-zellij"}
	}
	tab := Tab(e)
	target := servers.SSHArg(e)

	sshTail, needSshpass := sshCommand(e)
	if needSshpass {
		// A password is stored but sshpass is not installed: surface an
		// explicit hint instead of launching a broken ssh (or worse,
		// leaking the password through a prompt-less exec).
		return Plan{
			UseZellij:   false,
			NeedSshpass: true,
			TabName:     tab,
			SshTarget:   target,
			Detected:    environmentTag(),
			Argv:        sshTail,
		}
	}

	if !InZellij() {
		// Outside Zellij: still try `zellij action new-tab` so a future
		// "attach" session can absorb the new tab; if no zellij binary
		// is available, we report "no-zellij-or-zellij-bin" so the UI
		// can show a precise hint. We do NOT exec ssh in place because
		// that would tear down the sidebar pane.
		if !HaveZellijBin() {
			return Plan{
				UseZellij: false,
				TabName:   tab,
				SshTarget: target,
				Detected:  "no-zellij-or-zellij-bin",
				// Provide a useful argv anyway so headless / dry-run
				// callers can see what would have run.
				Argv: sshTail,
			}
		}
		return Plan{
			UseZellij: true,
			TabName:   tab,
			SshTarget: target,
			Argv: append([]string{
				"zellij", "action", "new-tab",
				"--name", tab,
				"--",
			}, sshTail...),
			Detected: "no-zellij",
		}
	}

	return Plan{
		UseZellij: true,
		TabName:   tab,
		SshTarget: target,
		Argv: append([]string{
			"zellij", "action", "new-tab",
			"--name", tab,
			"--",
		}, sshTail...),
		Detected: "zellij",
	}
}

// Run executes the plan. The command is awaited so a non-zero exit is
// observed and reported (e.g. `zellij action new-tab` when no Zellij
// server is reachable). A short overall timeout keeps a stuck child from
// hanging the TUI; a child that is still running when the deadline hits is
// treated as launched OK (CommandContext reaps it in the background), so
// callers never block indefinitely. Failures are returned to the caller
// and surfaced in the status bar / red status square.
func Run(ctx context.Context, p Plan) error {
	if len(p.Argv) == 0 {
		return errors.New("action: empty argv")
	}
	cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(cctx, p.Argv[0], p.Argv[1:]...) //nolint:gosec
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("action: start %v: %w", p.Argv, err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		// The command exited on its own.
		if err == nil {
			return nil
		}
		// If the deadline fired first, the child was killed by our own
		// timeout while still running — treat it as launched successfully.
		if cctx.Err() != nil {
			return nil
		}
		return fmt.Errorf("action: %v failed: %w", p.Argv, err)
	case <-cctx.Done():
		// Still running at the deadline (normally only possible for a
		// long-lived child); treated as launched OK. Wait() in the
		// goroutine reaps the process once CommandContext kills it.
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
