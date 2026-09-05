package servers

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Entry describes one line in the merged server list.
// Source is "ssh" (parsed from ~/.ssh/config) or "extra" (parsed from
// ~/.config/wezterm4neil/servers.txt).
type Entry struct {
	Alias    string // ssh Host name OR extra alias (left of "|")
	Desc     string // free-form description; empty for ssh entries
	Source   string // "ssh" or "extra"
	SshAlias string // value to pass to ssh (alias or user@host)
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

// ParseExtraList reads servers.txt. Each line is either:
//
//	"alias"
//	"alias|description"
//	"user@host|description"  (alias = ssh target)
//
// Lines starting with '#' or blank are skipped.
func ParseExtraList(r io.Reader) ([]Entry, error) {
	var out []Entry
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		raw := strings.TrimSpace(sc.Text())
		if raw == "" || strings.HasPrefix(raw, "#") {
			continue
		}
		alias, desc := raw, ""
		if i := strings.Index(raw, "|"); i >= 0 {
			alias = strings.TrimSpace(raw[:i])
			desc = strings.TrimSpace(raw[i+1:])
		}
		if alias == "" {
			continue
		}
		out = append(out, Entry{
			Alias:    alias,
			Desc:     desc,
			Source:   "extra",
			SshAlias: alias,
		})
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// Load returns the merged server list (ssh + extra), in order, with
// duplicates removed (ssh entries win, first appearance kept).
func Load() []Entry {
	var out []Entry
	seen := map[string]bool{}

	if p := SSHConfigPath(); p != "" {
		if f, err := os.Open(p); err == nil {
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

	if p := ExtraListPath(); p != "" {
		if f, err := os.Open(p); err == nil {
			extras, _ := ParseExtraList(f)
			_ = f.Close()
			for _, e := range extras {
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

// SSHArg returns the value to pass to "ssh" (alias or user@host form).
func SSHArg(e Entry) string {
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
