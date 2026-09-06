// RENAME GROUP overlay form (M6 edit mode).
//
// In edit mode pressing →/l while the cursor rests on a ▾ group/ folder row
// opens this overlay: a single "Group" text field prefilled with the current
// group label. Saving rewrites the Group column of every servers.txt entry
// in that folder to the new name (empty = ungroup those servers back to the
// flat list); the folder keeps its expanded/collapsed state. The overlay
// reuses the server form's box geometry, height and button hit zones so the
// two modals look and behave alike.
//
// Layout (10 rows tall, same box as NEW/EDIT SERVER):
//
//	┌ RENAME GROUP ──────────────┐
//	│  Group  : value_            │   row 1 = the single text field
//	│                             │   rows 2..6 blank (keeps the geometry)
//	│  hint / last error          │
//	│       [Enable]   [Cancel]   │   row 8 = buttons
//	└─────────────────────────────┘
//
// Keys: Tab / arrows move the focus ring (field + Enable + Cancel),
// Enter submits (Enable; Enter on Cancel cancels), Esc cancels, printable
// keys edit the field, Backspace deletes its last rune.
package ui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/aceneil/nav4neil/internal/servers"
)

// groupField is one focusable control inside the RENAME GROUP overlay:
// index 0 is the text field, 1 and 2 the buttons.
type groupField int

const (
	gFieldName groupField = iota
	gFieldEnable
	gFieldCancel
	gFieldTotal
)

// groupForm holds the mutable state of the RENAME GROUP overlay.
type groupForm struct {
	oldName string // group label being renamed
	value   string // proposed new label ("" = ungroup)
	focus   groupField
	err     string // last validation/save error, shown in the hint row
}

// openGroupForm shows the RENAME GROUP overlay for the folder oldName.
func (m *Model) openGroupForm(oldName string) {
	m.gform = &groupForm{oldName: oldName, value: oldName, focus: gFieldName}
	m.status = ""
}

// isTextField reports whether the current focus is the text input.
func (f *groupForm) isTextField() bool { return f.focus == gFieldName }

// deleteLastRune removes the final rune of the text field.
func (f *groupForm) deleteLastRune() {
	if !f.isTextField() {
		return
	}
	v := f.value
	if v == "" {
		return
	}
	r := []rune(v)
	f.value = string(r[:len(r)-1])
}

// handleGroupFormKey routes one key while the RENAME GROUP overlay is open.
// Enter submits (except on the Cancel button), Esc cancels.
func (m *Model) handleGroupFormKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	f := m.gform
	if f == nil {
		return m, nil
	}
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	switch msg.String() {
	case "esc":
		m.gform = nil
	case "enter":
		switch f.focus {
		case gFieldCancel:
			m.gform = nil
		default:
			m.applyGroupRename()
		}
	case "tab", "down", "right":
		f.focus = (f.focus + 1) % gFieldTotal
	case "shift+tab", "up", "left":
		f.focus = (f.focus - 1 + gFieldTotal) % gFieldTotal
	case "backspace":
		f.deleteLastRune()
	default:
		m.groupFormTypeKey(msg)
	}
	return m, nil
}

// groupFormTypeKey appends a printable key to the text field.
func (m *Model) groupFormTypeKey(msg tea.KeyMsg) {
	f := m.gform
	if f == nil || !f.isTextField() {
		return
	}
	switch msg.Type {
	case tea.KeyRunes:
		if len(msg.Runes) == 1 && msg.Runes[0] >= 32 {
			f.value += string(msg.Runes)
		}
	case tea.KeySpace:
		f.value += " "
	}
}

// handleGroupFormMouse routes mouse events while the RENAME GROUP overlay is
// open: clicking the field row focuses it, clicking Enable saves, Cancel
// closes. Button hit zones mirror the server form's (content columns 6 and
// 17 on row 8).
func (m *Model) handleGroupFormMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if m.gform == nil || msg.Type != tea.MouseLeft {
		return m, nil
	}
	x0, y0, _ := m.formGeometry()
	relY := msg.Y - y0
	relX := msg.X - x0 - 1 // column inside the content area (after the border)
	switch {
	case relY == 1 && msg.X >= x0+1:
		m.gform.focus = gFieldName
	case relY == 8:
		switch {
		case relX >= 6 && relX < 14:
			m.applyGroupRename()
		case relX >= 17 && relX < 25:
			m.gform = nil
		}
	}
	return m, nil
}

// applyGroupRename validates the new group label and, when valid, rewrites
// the Group column of every extra entry currently under oldName to the new
// name, reloads the list and closes the overlay. An empty label ungroups the
// members (Group becomes "" → flat rows).
func (m *Model) applyGroupRename() {
	f := m.gform
	if f == nil {
		return
	}
	name := strings.TrimSpace(f.value)
	if name != "" && (strings.ContainsAny(name, "|\n\r") || strings.HasPrefix(name, "#")) {
		f.err = "Group must not contain | / newline / start with #"
		return
	}
	if name == f.oldName {
		m.gform = nil
		m.status = "group unchanged: " + name
		return
	}

	next := make([]servers.Entry, 0, len(m.serversAll)+1)
	for _, e := range m.serversAll {
		if e.Source != "extra" {
			continue
		}
		if e.Group == f.oldName {
			e.Group = name
		}
		next = append(next, e)
	}
	if err := servers.SaveExtrasTo(m.extraSavePath(), next); err != nil {
		f.err = "save failed: " + err.Error()
		return
	}

	// Carry the folder's collapsed state over to the new label.
	if m.collapsed == nil {
		m.collapsed = map[string]bool{}
	}
	wasCollapsed := m.collapsed[f.oldName]
	delete(m.collapsed, f.oldName)
	if name != "" && wasCollapsed {
		m.collapsed[name] = true
	}

	m.gform = nil
	m.reloadServers()
	if name == "" {
		m.snapCursorToSelection()
		m.status = "group cleared: " + f.oldName + " (servers are now flat)"
		return
	}
	for i, r := range m.srvRows {
		if r.kind == srvRowGroup && r.group == name {
			m.srvCursor = i
			break
		}
	}
	m.status = "group renamed: " + f.oldName + " → " + name
}

// withGroupOverlay merges the box returned by gformBoxLines over a base view
// (identical to withFormOverlay, driving the RENAME GROUP overlay).
func (m *Model) withGroupOverlay(base string) string {
	lines := strings.Split(base, "\n")
	for len(lines) < m.height {
		lines = append(lines, blank(m.width))
	}
	x0, y0, bw := m.formGeometry()
	box := m.gformBoxLines(bw)
	for i, row := range box {
		yy := y0 + i
		if yy < 0 || yy >= len(lines) {
			break
		}
		lines[yy] = placeBoxRow(row, x0, m.width)
	}
	return strings.Join(lines, "\n")
}

// gformBoxLines renders the ten RENAME GROUP rows, each exactly bw runes
// wide. Rows 2..6 are blank so the box shares the server form's geometry and
// the button row sits at the same absolute offset.
func (m *Model) gformBoxLines(bw int) []string {
	f := m.gform
	title := " RENAME GROUP "
	fill := bw - 2 - len(title)
	if fill < 0 {
		fill = 0
	}
	lines := make([]string, 0, formBoxH)
	lines = append(lines, "┌"+title+strings.Repeat("─", fill)+"┐")
	lines = append(lines, m.gformFieldRow(bw))
	for i := 0; i < 5; i++ { // keep rows 2..6 of the 10-row box blank
		lines = append(lines, borderRow("", bw))
	}
	hint := f.err
	if hint == "" {
		hint = "New group name for every server in this folder (empty = ungroup)"
	}
	lines = append(lines, borderRow("  "+hint, bw))
	lines = append(lines, m.gformButtonRow(bw))
	lines = append(lines, "└"+strings.Repeat("─", max(0, bw-2))+"┘")
	return lines
}

// gformFieldRow renders the single Group text-field row of the overlay.
func (m *Model) gformFieldRow(bw int) string {
	f := m.gform
	prefix := "  " + padRight("Group", 6) + ": "
	avail := bw - 2 - len(prefix)
	if avail < 1 {
		avail = 1
	}
	val := f.value
	if f.focus == gFieldName {
		val += "_"
	}
	val = tailRunes(val, avail)
	return "│" + padRight(prefix+val, avail) + "│"
}

// gformButtonRow renders the Enable/Cancel row (same hit zones as the server
// form's button row: content columns 6 and 17).
func (m *Model) gformButtonRow(bw int) string {
	f := m.gform
	enable := "[Enable]"
	if f.focus == gFieldEnable {
		enable = "<Enable>"
	}
	cancel := "[Cancel]"
	if f.focus == gFieldCancel {
		cancel = "<Cancel>"
	}
	content := "      " + enable + "   " + cancel
	return borderRow(content, bw)
}
