package servers

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Entry describes one line in the merged server list.
// Source is "builtin", "ssh" (parsed from ~/.ssh/config), or "extra" (parsed from
// ~/.config/wezterm4neil/servers.txt).
//
// Extra entries that carry an explicit login target set User/Host/Port (and
// optionally Password/Group); legacy extra lines keep only Alias/Desc and
// SshAlias == Alias, exactly as they were stored. Structured fields are the
// M2 servers.txt form:
//
//	<name>|<user>@<host>:<port>|<desc>|<group>|<password>
type Entry struct {
	Alias    string // ssh Host name OR extra name (display alias)
	Desc     string // free-form description; empty for ssh entries
	Group    string // extra rows only: server group label
	User     string // extra rows only: ssh login user (may be empty)
	Host     string // extra rows only: ssh host (empty for legacy rows)
	Port     int    // extra rows only: ssh port (default 22 when set)
	Password string // extra rows only: ssh password (empty = key/agent auth)
	Source   string // "builtin", "ssh" or "extra"
	SshAlias string // value to pass to ssh (alias or user@host) for legacy rows
}

// SSHConfigPath returns the user's OpenSSH client config path,
// honouring $HOME so the function is testable without touching real env.
func SSHConfigPath() string {
	if h, err := os.UserHomeDir(); err == nil {
		return filepath.Join(h, ".ssh", "config")
	}
	return ""
}

// ExtraListPath returns the user-managed custom servers list path.
func ExtraListPath() string {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		if h, err := os.UserHomeDir(); err == nil {
			base = filepath.Join(h, ".config")
		} else {
			base = ".config"
		}
	}
	return filepath.Join(base, "wezterm4neil", "servers.txt")
}

// ParseSSHConfig reads an OpenSSH-style config and returns the list of
// Host tokens (excluding wildcards containing '*' or '?' and excluding
// empty patterns). Order is preserved, duplicates removed (first wins).
func ParseSSHConfig(r io.Reader) ([]string, error) {
	var hosts []string
	seen := map[string]bool{}
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := sc.Text()
		// Drop inline comments after '#' to avoid matching commented-out Hosts.
		if i := strings.Index(line, "#"); i >= 0 {
			line = line[:i]
		}
		trim := strings.TrimSpace(line)
		if trim == "" {
			continue
		}
		fields := strings.Fields(trim)
		if len(fields) < 2 {
			continue
		}
		if !strings.EqualFold(fields[0], "Host") {
			continue
		}
		// A single Host line may declare multiple aliases; each token is a
		// candidate pattern. Skip wildcard patterns.
		for _, h := range fields[1:] {
			if strings.ContainsAny(h, "*?") {
				continue
			}
			if h == "" {
				continue
			}
			if seen[h] {
				continue
			}
			seen[h] = true
			hosts = append(hosts, h)
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return hosts, nil
}

// ParseExtraList reads servers.txt. Each line is either a legacy row
//
//	"alias"
//	"alias|description"
//	"user@host|description"       (alias = ssh target)
//
// or an M2 structured row
//
//	"name|user@host:port|desc|group|password"
//
// Structured rows are recognised by their second field looking like an ssh
// target (contains '@', or carries a :port suffix). Legacy rows — including
// descriptions that themselves contain '|' or other free text — keep the old
// alias/description semantics. Lines starting with '#' or blank are skipped.
func ParseExtraList(r io.Reader) ([]Entry, error) {
	var out []Entry
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		raw := strings.TrimSpace(sc.Text())
		if raw == "" || strings.HasPrefix(raw, "#") {
			continue
		}
		parts := strings.Split(raw, "|")
		first := strings.TrimSpace(parts[0])
		if first == "" {
			continue
		}
		// Bare alias (legacy).
		if len(parts) == 1 {
			out = append(out, Entry{Alias: first, Source: "extra", SshAlias: first})
			continue
		}
		// Structured row: second field is an explicit target.
		if len(parts) >= 2 && looksLikeTarget(parts[1]) {
			user, host, port := parseTarget(parts[1])
			e := Entry{
				Alias:  first,
				Source: "extra",
				User:   user,
				Host:   host,
				Port:   port,
			}
			if len(parts) >= 3 {
				e.Desc = strings.TrimSpace(parts[2])
			}
			if len(parts) >= 4 {
				e.Group = strings.TrimSpace(parts[3])
			}
			if len(parts) >= 5 {
				e.Password = parts[4]
			}
			out = append(out, e)
			continue
		}
		// Legacy alias|description. Preserve the raw remainder after the first
		// pipe (descriptions may contain further pipes and odd spacing).
		desc := ""
		if i := strings.Index(raw, "|"); i >= 0 {
			desc = strings.TrimSpace(raw[i+1:])
		}
		out = append(out, Entry{
			Alias:    first,
			Desc:     desc,
			Source:   "extra",
			SshAlias: first,
		})
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// looksLikeTarget reports whether a servers.txt field is an explicit ssh
// target ("user@host", "host:port", "user@host:port") rather than free text.
// Free-text descriptions almost never contain '@' and only look like hosts
// when they carry a numeric :port suffix, so this keeps legacy rows safe.
func looksLikeTarget(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" || strings.ContainsAny(s, " 	") {
		return false
	}
	if i := strings.LastIndexByte(s, ':'); i >= 0 {
		if p, err := strconv.Atoi(s[i+1:]); err == nil && p > 0 && p < 65536 {
			return true
		}
	}
	return strings.ContainsRune(s, '@')
}

// parseTarget splits "user@host:port" into its parts. Port defaults to 22
// when absent; user may be empty ("host:2222" is legal).
func parseTarget(t string) (user, host string, port int) {
	port = 22
	t = strings.TrimSpace(t)
	if i := strings.LastIndexByte(t, '@'); i >= 0 {
		user = strings.TrimSpace(t[:i])
		t = t[i+1:]
	}
	if i := strings.LastIndexByte(t, ':'); i >= 0 {
		if p, err := strconv.Atoi(t[i+1:]); err == nil && p > 0 && p <= 65535 {
			port = p
			t = t[:i]
		}
	}
	return user, t, port
}

// ParseTarget is the exported form of parseTarget, used by the UI to prefill
// the EDIT overlay for legacy rows whose alias itself is an ssh target.
func ParseTarget(t string) (user, host string, port int) {
	return parseTarget(t)
}

// Line serialises one servers.txt row. Structured entries (explicit Host)
// always use the 5-field form so the file is unambiguous; legacy entries are
// re-emitted in their original short form, keeping round-trips lossless.
func (e Entry) Line() string {
	if e.Host == "" {
		if e.Desc == "" {
			return e.Alias
		}
		return e.Alias + "|" + e.Desc
	}
	port := e.Port
	if port <= 0 || port > 65535 {
		port = 22
	}
	target := e.Host
	if e.User != "" {
		target = e.User + "@" + target
	}
	target += ":" + strconv.Itoa(port)
	return strings.Join([]string{e.Alias, target, e.Desc, e.Group, e.Password}, "|")
}

// SaveExtrasTo atomically writes the user-managed servers.txt file at path
// ("" selects the default location). Only the entries passed in are written —
// callers pass the "extra" subset. The file's parent directory is created.
func SaveExtrasTo(path string, entries []Entry) error {
	if path == "" {
		path = ExtraListPath()
	}
	var b strings.Builder
	for _, e := range entries {
		b.WriteString(e.Line())
		b.WriteByte('\n')
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(b.String()), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// SaveExtras writes entries to the default servers.txt location.
func SaveExtras(entries []Entry) error {
	return SaveExtrasTo("", entries)
}

// Load returns the merged server list (ssh + extra), in order, with
// duplicates removed (ssh entries win, first appearance kept) and the
// built-in rows (localhost, then herdr) kept at the front.
func Load() []Entry {
	return LoadFrom(SSHConfigPath(), ExtraListPath())
}

// LoadFrom is Load with explicit data-source paths ("" skips a source).
// It exists so callers — and tests — can pin down exactly which files are
// read instead of relying on the ambient $HOME / XDG_CONFIG_HOME.
func LoadFrom(sshPath, extraPath string) []Entry {
	var out []Entry
	seen := map[string]bool{}

	if sshPath != "" {
		if f, err := os.Open(sshPath); err == nil {
			hosts, _ := ParseSSHConfig(f)
			_ = f.Close()
			for _, h := range hosts {
				if seen[h] {
					continue
				}
				seen[h] = true
				out = append(out, Entry{
					Alias:    h,
					Desc:     "",
					Source:   "ssh",
					SshAlias: h,
				})
			}
		}
	}

	out = InjectBuiltin(out)

	if extraPath != "" {
		if f, err := os.Open(extraPath); err == nil {
			extras, _ := ParseExtraList(f)
			_ = f.Close()
			for _, e := range extras {
				// "localhost" and "herdr" are reserved for the built-in
				// rows: a user row may never shadow or duplicate either,
				// regardless of case.
				if IsReservedAlias(e.Alias) {
					continue
				}
				if seen[e.Alias] {
					continue
				}
				seen[e.Alias] = true
				out = append(out, e)
			}
		}
	}

	return out
}

// TabName returns the suggested Zellij tab name for an Entry.
// Rule: ssh entries use the host; extra entries prefer the part after '@'
// when present so "root@db1|prod db" still produces a clean "db1" tab name.
func TabName(e Entry) string {
	a := e.Alias
	if e.Source == "extra" {
		if i := strings.Index(a, "@"); i >= 0 && i+1 < len(a) {
			return a[i+1:]
		}
	}
	return a
}

// SSHArg returns the value to pass to "ssh" as the destination: for
// structured extra entries this is "user@host" (or "host"); for everything
// else the legacy alias or SshAlias form is used unchanged. Port is handled
// separately by the action layer via -p.
func SSHArg(e Entry) string {
	if e.Host != "" {
		t := e.Host
		if e.User != "" {
			t = e.User + "@" + t
		}
		return t
	}
	if e.SshAlias != "" {
		return e.SshAlias
	}
	return e.Alias
}

// Validate runs sanity checks against the loaded list. Returns first error
// or nil. Used by smoke tests / debug mode.
func Validate(entries []Entry) error {
	if len(entries) == 0 {
		return fmt.Errorf("no servers discovered (ssh config empty and servers.txt missing)")
	}
	for _, e := range entries {
		if strings.TrimSpace(e.Alias) == "" {
			return fmt.Errorf("empty alias entry from source=%s", e.Source)
		}
	}
	return nil
}

// builtinRows are the reserved built-in rows injected at the front of the
// merged list, in display order: the local machine first, then the herdr
// agent workbench. Both are Source "builtin" — read-only, always-green rows
// that are never written to servers.txt and never enter a group folder.
var builtinRows = []Entry{
	{Alias: "localhost", Desc: "本机", Source: "builtin"},
	{Alias: "herdr", Desc: "Agent 工作台", Source: "builtin"},
}

// IsBuiltin reports whether e is one of the reserved built-in rows
// (localhost / herdr). An ssh-config row that reuses the alias is not
// builtin — it keeps its ssh source and the historical override rule.
func IsBuiltin(e Entry) bool { return e.Source == "builtin" }

// IsHerdr reports whether e is the built-in herdr agent-workbench row.
func IsHerdr(e Entry) bool {
	return IsBuiltin(e) && strings.EqualFold(e.Alias, "herdr")
}

// IsReservedAlias reports whether name (case-insensitive) collides with a
// built-in alias and therefore may never be used by a user-managed row.
func IsReservedAlias(name string) bool {
	for _, b := range builtinRows {
		if strings.EqualFold(b.Alias, name) {
			return true
		}
	}
	return false
}

// InjectBuiltin prepends the built-in rows (localhost, then herdr) unless
// an existing entry already uses that alias — notably an explicit SSH
// "Host localhost" / "Host herdr", which keeps its ssh source and position
// exactly as before. The result keeps the historical guarantee for every
// builtin: reserved names are never duplicated, and in the common case
// localhost is the very first row with herdr right behind it.
func InjectBuiltin(entries []Entry) []Entry {
	taken := map[string]bool{}
	for _, e := range entries {
		if IsReservedAlias(e.Alias) {
			taken[strings.ToLower(e.Alias)] = true
		}
	}
	out := make([]Entry, 0, len(entries)+len(builtinRows))
	for _, b := range builtinRows {
		if !taken[b.Alias] {
			out = append(out, b)
		}
	}
	return append(out, entries...)
}
