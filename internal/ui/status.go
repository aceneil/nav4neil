// M3 server-connection status: a small coloured square in front of each
// server row that shows whether that server's Zellij tab is open right now,
// or (outside Zellij / when polling fails) what the last open attempt did.
//
// Colour rules (see README legend):
//
//	green  — a Zellij tab named after this server is open (polled from
//	         `zellij action dump-layout`), or, in fallback mode, the last
//	         open attempt succeeded
//	yellow — no matching tab / never opened (default)
//	red    — the most recent open attempt failed; cleared by the next
//	         successful open or when a matching tab shows up again
//
// The glyph is a 256-colour "▮" so it renders on both 256-colour and
// truecolor terminals; when colour is unavailable (TERM=dumb, NO_COLOR,
// empty TERM) it degrades to a plain "·". Layout never depends on colour:
// the escape sequences live in the glyph cell only and the padded row body
// is built separately, so alignment and truncation stay intact.
package ui

import (
	"encoding/json"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/aceneil/nav4neil/internal/action"
	"github.com/aceneil/nav4neil/internal/servers"
)

// svState is the connection status shown for one server row.
type svState int

const (
	svYellow svState = iota // no matching tab and nothing recorded (default)
	svGreen                 // matching tab open, or last open succeeded (fallback)
	svRed                   // last open attempt failed
)

// 256-colour palette indices: picked for readability on dark terminals.
// The same codes are understood by truecolor terminals (they simply ignore
// the extra precision), which is why no profile detection is needed.
const (
	svGreen256  = 78  // #5fd787
	svYellow256 = 214 // #ffaf00
	svRed256    = 203 // #ff5f5f

	// editPtr256 paints the servers-pane pointer while M6 edit mode is
	// active (the "green cursor" affordance).
	editPtr256 = 46 // #00ff00
)

// pointerCell returns glyph (a one-cell pointer ▶/▷) painted green when the
// servers pane is in M6 edit mode and colour is available. The escape codes
// live inside the returned cell only, so padded row bodies never see ANSI.
func pointerCell(glyph string, edit bool) string {
	if !edit || !colorEnabled() {
		return glyph
	}
	return "\x1b[38;5;" + strconv.Itoa(editPtr256) + "m" + glyph + "\x1b[0m"
}

// svGlyph renders the status cell for st: a coloured "▮" when colour is
// available, a plain "·" otherwise. Either way it occupies exactly one
// terminal cell.
func svGlyph(st svState) string {
	if !colorEnabled() {
		return "·"
	}
	idx := svYellow256
	switch st {
	case svGreen:
		idx = svGreen256
	case svRed:
		idx = svRed256
	}
	return "\x1b[38;5;" + strconv.Itoa(idx) + "m▮\x1b[0m"
}

// colorEnabled reports whether ANSI colour output should be used. The
// narrowest environments (dumb terminals, NO_COLOR, no TERM at all) get a
// plain middle dot instead of a coloured block.
func colorEnabled() bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	switch os.Getenv("TERM") {
	case "", "dumb":
		return false
	}
	return true
}

// tabNameRe matches a `tab name="..."` attribute on a tab node of the KDL
// layout text emitted by `zellij action dump-layout` (verified against
// Zellij 0.45.1). Only tab nodes declare the tab's own name; pane nodes may
// carry their own `name=` (e.g. pane name="🚀 快捷服务") and must not count.
var tabNameRe = regexp.MustCompile(`(?m)^[ \t]*tab\b[^\n{]*\bname\s*=\s*"([^"]+)"`)

// parseTabNames extracts the set of open Zellij tab names from the raw
// stdout of `zellij action dump-layout`. It understands both the JSON
// serialisation newer Zellij builds can produce and the KDL text format of
// Zellij 0.45.x (tab name="X" { ... }). Output that parses to neither —
// e.g. a failed/empty dump — yields no names, never an error.
func parseTabNames(out string) []string {
	trimmed := strings.TrimSpace(out)
	if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") {
		if names := tabNamesFromJSON(trimmed); len(names) > 0 {
			return names
		}
	}
	var names []string
	for _, m := range tabNameRe.FindAllStringSubmatch(out, -1) {
		if m[1] != "" {
			names = append(names, m[1])
		}
	}
	return names
}

// jsonTab is the minimal shape of a tab record in a JSON layout dump.
type jsonTab struct {
	Name string `json:"name"`
}

// tabNamesFromJSON tries the plausible JSON shapes for a layout dump:
//
//	{"tabs": [{"name": "a"}, ...]}
//	[{"name": "a"}, ...]
//
// It returns nil when the text is not one of those shapes.
func tabNamesFromJSON(s string) []string {
	var object struct {
		Tabs []jsonTab `json:"tabs"`
	}
	if err := json.Unmarshal([]byte(s), &object); err == nil {
		return namesOfJSONTabs(object.Tabs)
	}
	var arr []jsonTab
	if err := json.Unmarshal([]byte(s), &arr); err == nil {
		return namesOfJSONTabs(arr)
	}
	return nil
}

func namesOfJSONTabs(tabs []jsonTab) []string {
	var names []string
	for _, t := range tabs {
		if t.Name != "" {
			names = append(names, t.Name)
		}
	}
	return names
}

// serverState resolves the effective status for one server entry from the
// model's polled tab set and open-attempt ledger.
//
// Order matters:
//  1. A matching tab is open → green (a live tab always beats a stale red).
//  2. The last open attempt failed → red.
//  3. Fallback ledger (not polling / last dump failed): last open success → green.
//  4. Everything else → yellow.
func (m *Model) serverState(e servers.Entry) svState {
	if m.liveTabs && m.tabsOpen[action.Tab(e)] {
		return svGreen
	}
	if m.svFailed[e.Alias] {
		return svRed
	}
	if !m.liveTabs && m.svOK[e.Alias] {
		return svGreen
	}
	return svYellow
}
