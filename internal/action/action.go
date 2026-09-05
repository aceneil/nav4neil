// Package action decides how to launch a remote ssh session in response to
// a user clicking a server in the wznav sidebar.
//
// Rule:
//   - If we are running inside a Zellij pane ($ZELLIJ is set and "zellij"
//     is on PATH) → issue `zellij action new-tab --name <tab> -- ssh <host>`.
//     Each click opens a brand-new tab; the existing layout is preserved.
//   - Otherwise → fall back to `zellij action new-tab` (still tries if
//     the binary is on PATH), and if THAT fails we surface the error so
//     the UI status bar can show it. We deliberately do NOT exec ssh in
//     place: that would replace the wznav pane and break the sidebar.
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
	"strings"
	"time"

	"github.com/wezterm4neil/wznav/internal/servers"
)

// Plan describes one concrete execution the TUI should perform.
type Plan struct {
	UseZellij bool     // true → issue "zellij action new-tab ..."
	Argv      []string // exec argv
	TabName   string   // informational only (echoed in status bar)
	SshTarget string   // informational only
	Detected  string   // "zellij"|"no-zellij"|"no-zellij-or-zellij-bin"
}

// InZellij reports whether the current process is running inside a
// Zellij session (heuristic: $ZELLIJ env var present).
func InZellij() bool { return os.Getenv("ZELLIJ") != "" }

// HaveZellijBin reports whether the `zellij` binary is on PATH.
func HaveZellijBin() bool {
	_, err := exec.LookPath("zellij")
	return err == nil
}

// Build computes the Plan for connecting to e. It does not touch the
// filesystem beyond looking at PATH.
func Build(e servers.Entry) Plan {
	tab := servers.TabName(e)
	target := servers.SSHArg(e)

	tab = sanitizeTab(tab)
	if tab == "" {
		tab = "ssh"
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
				Argv: []string{"ssh", target},
			}
		}
		return Plan{
			UseZellij: true,
			TabName:   tab,
			SshTarget: target,
			Argv: []string{
				"zellij", "action", "new-tab",
				"--name", tab,
				"--", "ssh", target,
			},
			Detected: "no-zellij",
		}
	}

	return Plan{
		UseZellij: true,
		TabName:   tab,
		SshTarget: target,
		Argv: []string{
			"zellij", "action", "new-tab",
			"--name", tab,
			"--", "ssh", target,
		},
		Detected: "zellij",
	}
}

// Run executes the plan with a short timeout so a stuck ssh doesn't
// hang the TUI forever. The TUI fires-and-forgets — failures are
// reported through the returned error and surfaced in the status bar.
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
	// Detach so the TUI is not blocked by the long-running ssh.
	go func() {
		_ = cmd.Wait()
	}()
	return nil
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
