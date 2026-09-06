// Package action decides how to launch a remote ssh session in response to
// a user selecting a server in the nav4neil sidebar.
//
// M5 behaviour — every server (including ssh ones) is used inside the right
// main terminal pane of the current Zellij tab; full-screen new tabs are
// gone:
//
//   - built-in localhost → `zellij action move-focus right` so the local
//     shell pane on the right takes focus (no typing needed).
//   - other servers → locate the right main terminal pane and type
//     "ssh …" into it: first Ctrl+C (clears any half-typed line), then the
//     command + Enter. When a concrete pane id could be resolved the writes
//     target that pane with `-p <paneId>` and nav4neil keeps focus;
//     otherwise the TUI first runs `zellij action move-focus right` and the
//     writes go to the now-focused pane.
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
	"encoding/json"
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

// Control characters typed into the target pane through `zellij action write`.
const (
	ctrlC = "\u0003" // interrupt / clear the current input line
	cr    = "\r"     // Enter
)

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

// sshTarget renders the destination token typed into the pane shell for e:
//
//	ssh-config host / legacy alias → the alias itself (sq-quoted when needed)
//	structured entry             → [user@]host
//
// Ports are handled separately with -p, never embedded in the target.
func sshTarget(e servers.Entry) string {
	if e.Host != "" {
		t := sq(e.Host)
		if e.User != "" {
			t = sq(e.User) + "@" + t
		}
		return t
	}
	if e.SshAlias != "" {
		return sq(e.SshAlias)
	}
	return sq(e.Alias)
}

// shellSafe reports whether s may be typed into a shell without quoting.
func shellSafe(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case strings.ContainsRune("@._%/:+=,-", r):
		default:
			return false
		}
	}
	return true
}

// sq single-quotes s unless it only contains shell-safe characters. A single
// quote inside s is escaped the POSIX way ('\”).
func sq(s string) string {
	if shellSafe(s) {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// sqPass always single-quotes a password (sshpass -p '<pw>'), keeping spaces
// and shell metacharacters inside one argument.
func sqPass(p string) string {
	return "'" + strings.ReplaceAll(p, "'", `'\''`) + "'"
}

// SshLine renders the shell command line typed into the target pane to
// connect to e:
//
//	ssh web01
//	ssh -p 2222 root@10.0.0.5
//	sshpass -p 's3cr3t' ssh -p 2222 root@10.0.0.5
//
// The second return reports that a stored password exists but sshpass is
// missing on PATH — callers must not run the line and should surface an
// install hint instead.
func SshLine(e servers.Entry) (string, bool) {
	target := sshTarget(e)
	if e.Password != "" {
		if _, err := exec.LookPath("sshpass"); err != nil {
			return "ssh " + target, true
		}
	}
	var parts []string
	if e.Password != "" {
		parts = append(parts, "sshpass", "-p", sqPass(e.Password))
	}
	parts = append(parts, "ssh")
	if e.Port > 0 && e.Port != 22 {
		parts = append(parts, "-p", strconv.Itoa(e.Port))
	}
	parts = append(parts, target)
	return strings.Join(parts, " "), false
}

// SshWritePlan builds the steps that type `ssh …` into the main terminal
// pane for e. paneID is the id resolved from the layout ("" = unknown — the
// plan first moves focus right, then writes into the focused pane). A
// password without sshpass yields a NeedSshpass hint plan with no steps.
func SshWritePlan(e servers.Entry, paneID string) Plan {
	tab := Tab(e)
	target := servers.SSHArg(e)
	_, need := SshLine(e)
	if need {
		return Plan{
			NeedSshpass: true,
			TabName:     tab,
			SshTarget:   target,
			Detected:    envDetected(),
		}
	}
	line, _ := SshLine(e)
	p := Plan{UseZellij: true, TabName: tab, SshTarget: target, Detected: envDetected()}
	if paneID != "" {
		p.Steps = [][]string{
			{"zellij", "action", "write", "-p", paneID, ctrlC},
			{"zellij", "action", "write", "-p", paneID, line + cr},
		}
	} else {
		p.Steps = [][]string{
			{"zellij", "action", "move-focus", "right"},
			{"zellij", "action", "write", ctrlC},
			{"zellij", "action", "write", line + cr},
		}
	}
	return p
}

// LocalhostPlan returns the plan for the built-in localhost row:
// `zellij action move-focus right`, which focuses the pane to the right —
// in the nav4neil layout the local shell. Callers must gate on InZellij()
// first; outside Zellij there is no pane layout to steer.
func LocalhostPlan() Plan {
	return Plan{
		UseZellij: true,
		TabName:   "local",
		SshTarget: "local",
		Detected:  "zellij",
		Steps: [][]string{
			{"zellij", "action", "move-focus", "right"},
		},
	}
}

// Build computes the plan for connecting to e without knowing a pane id.
// Inside Zellij ssh entries get the fallback plan (move-focus right + writes
// into the focused pane); the TUI may upgrade it with SshWritePlan(e, pane)
// once it resolved a concrete pane id. Outside Zellij every entry — and
// builtin localhost — degrades to a hint-only plan (no steps). It never
// opens new tabs anymore.
func Build(e servers.Entry) Plan {
	tab := Tab(e)
	if e.Source == "builtin" {
		if InZellij() {
			return LocalhostPlan()
		}
		return Plan{TabName: tab, SshTarget: "local", Detected: "no-zellij"}
	}
	target := servers.SSHArg(e)
	if _, need := SshLine(e); need {
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
	return SshWritePlan(e, "")
}

// ----- pane resolution ------------------------------------------------------
//
// The target of the ssh writes is the right-hand main terminal pane of the
// current tab. Zellij 0.45.x `dump-layout` emits KDL text that never
// contains pane ids (verified against live sessions), so the resolver first
// tries a layout dump for future/JSON builds and then asks `list-panes
// --json` (real ids since 0.45).

// paneCandidate is one pane record considered by the right-main picker.
type paneCandidate struct {
	id         string
	isPlugin   bool
	isFloating bool
	focused    bool
	title      string
	command    string
	paneX      int
	paneRows   int
	tab        string
}

func isNavPane(title, command string) bool {
	return strings.Contains(strings.ToLower(title), "nav4neil") ||
		strings.Contains(strings.ToLower(command), "nav4neil")
}

// pickRightMainPane selects the id of the right-hand main terminal pane from
// a list of pane records: terminal (non-plugin), non-floating panes outside
// the nav4neil family, restricted to the tab that hosts our nav4neil pane
// (falling back to the focused pane's tab), choosing the rightmost and, on a
// tie, the tallest one. Returns "" when no plausible target exists.
func pickRightMainPane(ps []paneCandidate) string {
	if len(ps) == 0 {
		return ""
	}
	// The tab of our own nav4neil pane(s), falling back to the tab of the
	// focused pane.
	ourTab, haveTab := "", false
	for _, p := range ps {
		if !p.isPlugin && isNavPane(p.title, p.command) {
			ourTab, haveTab = p.tab, p.tab != ""
			break
		}
	}
	if !haveTab {
		for _, p := range ps {
			if p.focused {
				ourTab, haveTab = p.tab, p.tab != ""
				break
			}
		}
	}
	var best paneCandidate
	have := false
	for _, p := range ps {
		if p.isPlugin || p.isFloating {
			continue
		}
		if isNavPane(p.title, p.command) {
			continue
		}
		if haveTab && p.tab != ourTab {
			continue
		}
		if !have || p.paneX > best.paneX || (p.paneX == best.paneX && p.paneRows > best.paneRows) {
			best, have = p, true
		}
	}
	if !have || best.paneX <= 0 {
		return ""
	}
	return best.id
}

// paneMapsFromAny walks decoded JSON and collects every object that looks
// like a pane record (has an id and pane-ish fields).
func paneMapsFromAny(v any, out *[]map[string]any) {
	switch t := v.(type) {
	case map[string]any:
		if _, hasID := t["id"]; hasID {
			if _, hasGeom := t["pane_x"]; hasGeom {
				*out = append(*out, t)
			} else if _, hasTitle := t["title"]; hasTitle {
				*out = append(*out, t)
			} else if _, hasPlugin := t["is_plugin"]; hasPlugin {
				*out = append(*out, t)
			}
		}
		for _, child := range t {
			paneMapsFromAny(child, out)
		}
	case []any:
		for _, child := range t {
			paneMapsFromAny(child, out)
		}
	}
}

func strOf(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case float64:
		return strconv.Itoa(int(t))
	case json.Number:
		return t.String()
	}
	return ""
}

func intOf(v any) int {
	switch t := v.(type) {
	case float64:
		return int(t)
	case string:
		n, _ := strconv.Atoi(t)
		return n
	case json.Number:
		n, _ := strconv.Atoi(t.String())
		return n
	}
	return 0
}

func boolOf(v any) bool {
	b, _ := v.(bool)
	return b
}

func candidateFromMap(m map[string]any) paneCandidate {
	c := paneCandidate{
		id:         strOf(m["id"]),
		isPlugin:   boolOf(m["is_plugin"]),
		isFloating: boolOf(m["is_floating"]),
		focused:    boolOf(m["is_focused"]),
		title:      strOf(m["title"]),
		command:    strOf(m["pane_command"]),
		paneX:      intOf(m["pane_x"]),
		paneRows:   intOf(m["pane_rows"]),
		tab:        strOf(m["tab_id"]),
	}
	return c
}

// rightPaneIDFromPanes extracts the right main terminal pane id from the
// JSON array of `zellij action list-panes --all --json` (real schema: panes
// with numeric ids, is_plugin, pane_x / pane_rows geometry, tab_id, …).
func rightPaneIDFromPanes(out string) string {
	var root any
	if err := json.Unmarshal([]byte(out), &root); err != nil {
		return ""
	}
	var maps []map[string]any
	paneMapsFromAny(root, &maps)
	var ps []paneCandidate
	for _, m := range maps {
		if m["id"] == nil {
			continue
		}
		ps = append(ps, candidateFromMap(m))
	}
	return pickRightMainPane(ps)
}

// rightPaneIDFromLayout parses a `zellij action dump-layout` dump. KDL text
// (Zellij 0.45.x) carries no pane ids and always yields "". JSON dumps that
// embed pane records with ids (newer builds; same fields as list-panes) go
// through the shared pane picker.
func rightPaneIDFromLayout(out string) string {
	trimmed := strings.TrimSpace(out)
	if !strings.HasPrefix(trimmed, "{") && !strings.HasPrefix(trimmed, "[") {
		return "" // KDL text has no ids
	}
	var root any
	if err := json.Unmarshal([]byte(trimmed), &root); err != nil {
		return ""
	}
	var maps []map[string]any
	paneMapsFromAny(root, &maps)
	var ps []paneCandidate
	for _, m := range maps {
		if m["id"] == nil {
			continue
		}
		ps = append(ps, candidateFromMap(m))
	}
	return pickRightMainPane(ps)
}

// ResolveRightPaneID asks the running Zellij session for the id of the
// right-hand main terminal pane. It first tries a layout dump (future/JSON
// dumps may embed ids), then `zellij action list-panes --all --json`
// (concrete ids since 0.45). Every lookup is best-effort with a short
// timeout; "" means "could not resolve — move focus right instead".
func ResolveRightPaneID(ctx context.Context) string {
	dlCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	if out, err := exec.CommandContext(dlCtx, "zellij", "action", "dump-layout").Output(); err == nil {
		if id := rightPaneIDFromLayout(string(out)); id != "" {
			cancel()
			return id
		}
	}
	cancel()

	lpCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if out, err := exec.CommandContext(lpCtx, "zellij", "action", "list-panes", "-a", "-j").Output(); err == nil {
		if id := rightPaneIDFromPanes(string(out)); id != "" {
			return id
		}
	}
	return ""
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
