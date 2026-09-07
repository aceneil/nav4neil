// M7 deterministic right-side pane targeting.
//
// Problem: plain `zellij action new-pane` (no direction) asks Zellij to use
// the biggest available space. Once every right-hand pane drops below
// Zellij's minimum split size it stops splitting and silently *stacks* new
// panes inside the existing columns — observed in the wild as "at most two
// right panes, everything after that hides in a stack". `move-focus right`
// alone cannot fix that either: it always lands on the nav-adjacent pane,
// which is not necessarily the pane we want to grow next.
//
// M7 approach — pick the biggest pane in the right-hand (non-nav) area of
// the ACTIVE tab and split it with a forced direction, so every click opens
// one more *visible* pane:
//
//  1. Parse the raw stdout of `zellij action dump-layout`. On Zellij
//     0.45.x that output is KDL text describing the pane tree (verified by
//     actually running it) — there is no JSON and no per-pane id or
//     absolute x/y/w/h in the dump, only nested split nodes with relative
//     sizes. The parser below reconstructs a relative rectangle per
//     terminal pane (fractions of the tab's content area).
//  2. Exclude the left nav column (x≈0, where the nav4neil panes live) and
//     the plugin bars (tab-bar/status-bar); among the remaining panes pick
//     the biggest one. A same-area tie prefers the leftmost/topmost pane.
//  3. dump-layout carries no pane ids, so the target is expressed as a
//     cursor path instead of focus-pane-id: RightMoves× `zellij action
//     move-focus right` walks focus column by column onto the target, then
//     `zellij action new-pane --direction right -- <argv>` forces a tiled
//     split of exactly that pane (Zellij halves the focused pane and puts
//     the new one next to it). Repeating clicks keep splitting the current
//     biggest right pane, so the right area grows column by column instead
//     of stacking in place.
//  4. The walk is only sound while every column strictly between the nav
//     column and the target holds exactly one full-height pane (which is
//     the invariant our own forced vertical splits maintain). Any other
//     shape — legacy stacked columns, row splits, an unparseable dump, a
//     multi-tab session whose active tab cannot be identified — yields
//     Usable()==false and the caller falls back to the legacy M6 sequence
//     (move-focus right + direction-less new-pane).
package action

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Target describes where the next right-hand pane should be opened.
//
// Usable reports whether the layout could be analysed AND the best right
// pane is reachable with a pure rightward cursor walk. When Usable is true
// the caller should emit RightMoves× `move-focus right` followed by a
// `new-pane --direction right`; when false it must fall back to the legacy
// M6 sequence. BestName is informational (the pane's name attribute, may be
// empty).
type Target struct {
	Usable     bool
	RightMoves int // 1 = nav-adjacent pane, 2 = one column further right, …
	BestName   string
	Reason     string // why Usable is false, or a short note when true
}

// UsableTarget reports whether the target analysis may drive the open plan.
func (t Target) UsableTarget() bool { return t.Usable && t.RightMoves >= 1 }

// paneRect is one terminal pane with its relative rectangle inside the
// active tab's content area (all values are fractions of the content size).
type paneRect struct {
	x, y, w, h float64
	name       string
	command    string
	stacked    bool
}

// layoutNode is a structural node of the dump-layout KDL tree. Only the
// shape matters: pane/tab nodes with their size/split attributes.
type layoutNode struct {
	kind  string // "layout" | "tab" | "pane" | "plugin" | "new_tab_template"
	attrs map[string]string
	kids  []*layoutNode
}

func (n *layoutNode) attr(name string) string {
	if n == nil || n.attrs == nil {
		return ""
	}
	return n.attrs[name]
}

var (
	// structuralTokens are the KDL node kinds that change the pane tree.
	structuralTokens = map[string]bool{
		"layout": true, "tab": true, "pane": true, "plugin": true,
		"new_tab_template": true,
	}
	// attrRe matches `name="value"` / `name=value` pairs inside a KDL line.
	attrRe = regexp.MustCompile(`([a-zA-Z_][a-zA-Z0-9_-]*)\s*=\s*("([^"]*)"|[^\s"'{};]+)`)
	// percentRe parses size values: "20%" or a bare integer.
	percentRe = regexp.MustCompile(`^([0-9]+(?:\.[0-9]+)?)%$`)
)

// parseLayout parses the KDL text emitted by `zellij action dump-layout`
// into a structural tree. Unknown/informational nodes (cwd, args,
// start_suspended, …) are skipped; braces are not required because the dump
// is reliably indented (4 spaces per level).
func parseLayout(out string) *layoutNode {
	root := &layoutNode{kind: "layout", attrs: map[string]string{}}
	type frame struct {
		node   *layoutNode
		indent int
	}
	stack := []frame{{node: root, indent: -1}}
	for _, raw := range strings.Split(out, "\n") {
		indent := 0
		for indent < len(raw) && raw[indent] == ' ' {
			indent++
		}
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		// Find the structural token at the start of the line.
		end := 0
		for end < len(line) && !isSpaceOrBrace(line[end]) {
			end++
		}
		tok := line[:end]
		if !structuralTokens[tok] {
			continue
		}
		for len(stack) > 1 && stack[len(stack)-1].indent >= indent {
			stack = stack[:len(stack)-1]
		}
		node := &layoutNode{kind: tok, attrs: map[string]string{}}
		for _, m := range attrRe.FindAllStringSubmatch(line, -1) {
			val := m[3]
			if val == "" {
				val = m[2]
			}
			node.attrs[m[1]] = val
		}
		parent := stack[len(stack)-1].node
		parent.kids = append(parent.kids, node)
		if strings.Contains(line, "{") {
			stack = append(stack, frame{node: node, indent: indent})
		}
	}
	return root
}

func isSpaceOrBrace(c byte) bool {
	return c == ' ' || c == '\t' || c == '{' || c == '}'
}

// activeTabNode picks the tab whose layout drives the next open: the one
// marked focus=true, or the only tab of the dump. A multi-tab dump without
// a focus marker is ambiguous → nil.
func activeTabNode(root *layoutNode) *layoutNode {
	var tabs []*layoutNode
	var focused *layoutNode
	var walk func(n *layoutNode)
	walk = func(n *layoutNode) {
		if n.kind == "tab" {
			tabs = append(tabs, n)
			if n.attr("focus") == "true" {
				focused = n
			}
		}
		for _, k := range n.kids {
			walk(k)
		}
	}
	walk(root)
	if focused != nil {
		return focused
	}
	if len(tabs) == 1 {
		return tabs[0]
	}
	return nil
}

// nodeHasPlugin reports whether n or any direct structural child is a
// plugin (used to skip the tab-bar/status-bar rows).
func nodeHasPlugin(n *layoutNode) bool {
	if n.kind == "plugin" {
		return true
	}
	for _, k := range n.kids {
		if k.kind == "plugin" {
			return true
		}
	}
	return false
}

// hasChildPane reports whether n nests further panes below itself.
func hasChildPane(n *layoutNode) bool {
	for _, k := range n.kids {
		if k.kind == "pane" || k.kind == "tab" || k.kind == "plugin" {
			return true
		}
	}
	return false
}

// sizeFraction parses a KDL size attribute into a fraction of the parent
// along the split axis. "20%" → 0.2, "1" → 0.01 (1%), missing → -1.
func sizeFraction(n *layoutNode) float64 {
	s := n.attr("size")
	if s == "" {
		return -1
	}
	if m := percentRe.FindStringSubmatch(s); m != nil {
		f, _ := strconv.ParseFloat(m[1], 64)
		return f / 100.0
	}
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		return f / 100.0
	}
	return -1
}

// placeRectangles walks the active tab subtree and records a relative
// rectangle per terminal (non-plugin) pane.
func placeRectangles(n *layoutNode, x, y, w, h float64, stacked bool, out *[]paneRect) {
	if n == nil || nodeHasPlugin(n) {
		return
	}
	// A stacked container overlays its children on the same rectangle.
	if n.attr("stacked") == "true" {
		for _, k := range n.kids {
			placeRectangles(k, x, y, w, h, true, out)
		}
		return
	}
	var kids []*layoutNode
	for _, k := range n.kids {
		if k.kind == "pane" || (k.kind == "tab" && n.kind == "tab") {
			kids = append(kids, k)
		}
	}
	if len(kids) == 0 {
		if n.kind == "pane" && !stacked {
			*out = append(*out, paneRect{
				x: x, y: y, w: w, h: h,
				name:    n.attr("name"),
				command: n.attr("command"),
				stacked: stacked,
			})
		}
		return
	}
	// Containers lay children out side by side (columns, split_direction
	// "vertical") or stacked vertically (rows — the default, and what the
	// nav column uses for its stacked servers/files panes).
	axisX := n.attr("split_direction") == "vertical"
	length := h
	if axisX {
		length = w
	}
	start := y
	if axisX {
		start = x
	}
	specified := 0.0
	missing := 0
	for _, k := range kids {
		if f := sizeFraction(k); f >= 0 {
			specified += f
		} else {
			missing++
		}
	}
	leftover := 1.0 - specified
	if leftover < 0 {
		leftover = 0
	}
	perMissing := 0.0
	if missing > 0 {
		perMissing = leftover / float64(missing)
	}
	pos := start
	for _, k := range kids {
		f := sizeFraction(k)
		if f < 0 {
			f = perMissing
		}
		seg := length * f
		if axisX {
			placeRectangles(k, pos, y, seg, h, stacked, out)
		} else {
			placeRectangles(k, x, pos, w, seg, stacked, out)
		}
		pos += seg
	}
}

// column is a vertical slice of the tab sharing one x range.
type column struct {
	x      float64
	leaves []paneRect
}

// AnalyzeLayout parses a `zellij action dump-layout` stdout and decides
// where the next right-hand pane should open. See the package comment for
// the model; the summary on the returned Target says whether a rightward
// cursor walk can reach the biggest right pane.
func AnalyzeLayout(dump string) Target {
	root := parseLayout(dump)
	tab := activeTabNode(root)
	if tab == nil {
		return Target{Reason: "no identifiable active tab (multi-tab dump without focus marker?)"}
	}
	var rects []paneRect
	placeRectangles(tab, 0, 0, 1, 1, false, &rects)
	if len(rects) == 0 {
		return Target{Reason: "layout dump parsed but contains no terminal panes"}
	}

	// Group leaves into columns by their left edge (they share the full
	// content height when the right area is cleanly columnar).
	eps := 1e-6
	var cols []column
	for _, r := range rects {
		idx := -1
		for i := range cols {
			if absF(cols[i].x-r.x) < eps {
				idx = i
				break
			}
		}
		if idx < 0 {
			cols = append(cols, column{x: r.x})
			idx = len(cols) - 1
		}
		cols[idx].leaves = append(cols[idx].leaves, r)
	}
	sort.Slice(cols, func(i, j int) bool { return cols[i].x < cols[j].x })

	// The leftmost column (x≈0) is the nav area holding nav4neil itself;
	// everything right of it is a candidate "right main pane".
	if len(cols) < 2 {
		return Target{Reason: "no right-side panes (only the nav column)"}
	}
	navX := cols[0].x
	right := cols[1:]
	if absF(navX) > eps {
		// Should not happen for the sidebar layout, but be safe: treat the
		// whole first column as nav only when it starts at the tab edge.
		return Target{Reason: "unexpected geometry: leftmost column is not at x=0"}
	}

	// Pick the biggest right pane; ties prefer the leftmost/topmost one so
	// equal columns grow from the nav side outward.
	best := -1 // index into cols (1-based columns)
	bestArea := -1.0
	for i, c := range right {
		for _, r := range c.leaves {
			area := r.w * r.h
			if area > bestArea+1e-9 || (absF(area-bestArea) <= 1e-9 && best == -1) {
				bestArea = area
				best = i + 1 // column index, nav = 0
			}
		}
	}
	if best < 1 {
		return Target{Reason: "no selectable pane in the right area"}
	}
	bestName := cols[best].leaves[0].name
	bestNameReason := ""
	if bestName != "" {
		bestNameReason = " (target: " + bestName + ")"
	}

	// A rightward walk is only sound when every column up to and including
	// the target holds exactly one pane (legacy stacks / row splits would
	// make the landing pane ambiguous).
	for i := 1; i <= best; i++ {
		if len(cols[i].leaves) != 1 {
			return Target{
				Usable: false,
				Reason: "right area not cleanly columnar at column " + strconv.Itoa(i) +
					" (stacked or row-split panes present)" + bestNameReason,
			}
		}
	}
	return Target{
		Usable:     true,
		RightMoves: best,
		BestName:   bestName,
		Reason:     "best right pane reached after " + strconv.Itoa(best) + " right move(s)" + bestNameReason,
	}
}

func absF(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}
