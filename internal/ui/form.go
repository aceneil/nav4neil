// NEW/EDIT server overlay form.
//
// The form is a fixed-size box rendered over the regular View when
// Model.form != nil. While it is open every key and mouse event is routed
// here (see handleKey/handleMouse in ui.go) so the underlying list cannot
// change underneath the overlay.
//
// Layout (10 rows tall, box width ≤ 58):
//
//	┌ NEW SERVER ────────────────┐
//	│  Name   : value_            │   rows 1..6 = six text fields
//	│  Host   : value             │
//	│  Port   : 22                │
//	│  User   : value             │
//	│  Pass   : value             │
//	│  Group  : value             │
//	│  hint / last error          │
//	│       [Enable]   [Cancel]   │   row 8 = buttons
//	└─────────────────────────────┘
//
// Keys: Tab / arrows move the focus ring (fields + Enable + Cancel),
// Enter submits (Enable; Enter on Cancel cancels), Esc cancels, printable
// keys edit the focused field, Backspace deletes its last rune.
package ui

import (
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/aceneil/nav4neil/internal/servers"
)

// formMode distinguishes the NEW overlay from the EDIT overlay.
type formMode int

const (
	formNew formMode = iota
	formEdit
)

// formField is one focusable control inside the overlay. Indexes 0..5 are
// text fields (also indexes into serverForm.values); 6 and 7 are buttons.
type formField int

const (
	fieldName formField = iota
	fieldHost
	fieldPort
	fieldUser
	fieldPass
	fieldGroup
	fieldEnable // button: save + apply
	fieldCancel // button: close without saving
	fieldTotal
)

// fieldLabels are displayed in the overlay rows, in values[] order.
var fieldLabels = [6]string{"Name", "Host", "Port", "User", "Pass", "Group"}

// formBoxH is the fixed overlay height in rows.
const formBoxH = 10

// serverForm holds the mutable overlay state.
type serverForm struct {
	mode    formMode
	desc    string // description lives outside the six text fields
	oldName string // EDIT: alias of the row being replaced
	values  [6]string
	focus   formField
	err     string // last validation/save error, shown in the hint row
}

// openServerForm shows the overlay. When edit is true the overlay is
// prefilled from e (a Source "extra" entry) and enabling will replace that
// row; otherwise it starts blank with Port defaulted to 22.
func (m *Model) openServerForm(e servers.Entry, edit bool) {
	f := &serverForm{mode: formNew, focus: fieldName, values: [6]string{fieldPort: "22"}}
	if edit {
		f.mode = formEdit
		f.oldName = e.Alias
		f.desc = e.Desc
		user, host, port := e.User, e.Host, e.Port
		if e.Host == "" && e.SshAlias != "" {
			// Legacy row: the alias itself may be the ssh target.
			user, host, port = servers.ParseTarget(e.SshAlias)
		}
		f.values[fieldName] = e.Alias
		f.values[fieldHost] = host
		f.values[fieldUser] = user
		if port <= 0 || port > 65535 {
			port = 22
		}
		f.values[fieldPort] = strconv.Itoa(port)
		f.values[fieldPass] = e.Password
		f.values[fieldGroup] = e.Group
	}
	m.form = f
	m.status = ""
}

// isTextField reports whether the current focus is one of the six inputs.
func (f *serverForm) isTextField() bool { return f.focus < fieldEnable }

// deleteLastRune removes the final rune of the focused text field.
func (f *serverForm) deleteLastRune() {
	if !f.isTextField() {
		return
	}
	v := f.values[f.focus]
	if v == "" {
		return
	}
	r := []rune(v)
	f.values[f.focus] = string(r[:len(r)-1])
}

// handleFormKey routes one key while the overlay is open. Enter submits
// (except on the Cancel button), Esc cancels.
func (m *Model) handleFormKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	f := m.form
	if f == nil {
		return m, nil
	}
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	switch msg.String() {
	case "esc":
		m.form = nil
	case "enter":
		switch f.focus {
		case fieldCancel:
			m.form = nil
		default:
			m.enableForm()
		}
	case "tab", "down", "right":
		f.focus = (f.focus + 1) % fieldTotal
	case "shift+tab", "up", "left":
		f.focus = (f.focus - 1 + fieldTotal) % fieldTotal
	case "backspace":
		f.deleteLastRune()
	default:
		m.formTypeKey(msg)
	}
	return m, nil
}

// formTypeKey appends a printable key to the focused text field.
func (m *Model) formTypeKey(msg tea.KeyMsg) {
	f := m.form
	if f == nil || !f.isTextField() {
		return
	}
	switch msg.Type {
	case tea.KeyRunes:
		if len(msg.Runes) == 1 && msg.Runes[0] >= 32 {
			f.values[f.focus] += string(msg.Runes)
		}
	case tea.KeySpace:
		f.values[f.focus] += " "
	}
}

// handleFormMouse routes mouse events while the overlay is open: clicking a
// field row focuses that field, clicking Enable saves, Cancel closes.
func (m *Model) handleFormMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if m.form == nil || msg.Type != tea.MouseLeft {
		return m, nil
	}
	x0, y0, _ := m.formGeometry()
	relY := msg.Y - y0
	relX := msg.X - x0 - 1 // column inside the content area (after the border)
	switch {
	case relY >= 1 && relY <= 6 && msg.X >= x0+1:
		m.form.focus = formField(relY - 1)
	case relY == 8:
		switch {
		case relX >= 6 && relX < 14:
			m.enableForm()
		case relX >= 17 && relX < 25:
			m.form = nil
		}
	}
	return m, nil
}

// enableForm validates the form and, when valid, persists the change to
// servers.txt, reloads the list and closes the overlay.
func (m *Model) enableForm() {
	f := m.form
	if f == nil {
		return
	}
	name := strings.TrimSpace(f.values[fieldName])
	host := strings.TrimSpace(f.values[fieldHost])
	user := strings.TrimSpace(f.values[fieldUser])
	group := strings.TrimSpace(f.values[fieldGroup])
	password := f.values[fieldPass]

	if name == "" {
		f.err = "Name is required"
		return
	}
	if host == "" {
		f.err = "Host is required"
		return
	}
	if strings.ContainsAny(name, "|\n\r") || strings.HasPrefix(name, "#") {
		f.err = "Name must not contain | / newline / start with #"
		return
	}
	if strings.EqualFold(name, "localhost") {
		f.err = "'localhost' is built-in and reserved"
		return
	}
	port := 22
	if ps := strings.TrimSpace(f.values[fieldPort]); ps != "" {
		p, err := strconv.Atoi(ps)
		if err != nil || p < 1 || p > 65535 {
			f.err = "Port must be 1-65535"
			return
		}
		port = p
	}

	// A name already taken by any other row (ssh config or servers.txt)
	// would silently vanish after reload thanks to dedup, so reject it.
	for _, e := range m.serversAll {
		if f.mode == formEdit && e.Source == "extra" && e.Alias == f.oldName {
			continue // the row being replaced
		}
		if strings.EqualFold(e.Alias, name) {
			f.err = "name already exists: " + e.Alias
			return
		}
	}

	// Persist: keep all extras except the replaced row, then append.
	next := make([]servers.Entry, 0, len(m.serversAll)+1)
	for _, e := range m.serversAll {
		if e.Source != "extra" {
			continue
		}
		if f.mode == formEdit && e.Alias == f.oldName {
			continue
		}
		next = append(next, e)
	}
	next = append(next, servers.Entry{
		Alias:    name,
		Desc:     f.desc,
		Group:    group,
		User:     user,
		Host:     host,
		Port:     port,
		Password: password,
		Source:   "extra",
	})

	path := m.extraSavePath()
	if err := servers.SaveExtrasTo(path, next); err != nil {
		f.err = "save failed: " + err.Error()
		return
	}

	m.form = nil
	m.reloadServers()
	for i, e := range m.serversView {
		if e.Alias == name {
			m.serverCursor = i
			break
		}
	}
	m.status = "saved " + name
}

// extraSavePath returns the servers.txt path used for writes, honouring the
// test override.
func (m *Model) extraSavePath() string {
	if m.extraPathOverride != "" {
		return m.extraPathOverride
	}
	return servers.ExtraListPath()
}

// formGeometry computes where the overlay box is drawn: (x0, y0, boxWidth).
// Rendering (formBoxLines) and click handling share these numbers.
func (m *Model) formGeometry() (x0, y0, bw int) {
	bw = m.width
	if bw > 58 {
		bw = 58
	}
	if bw < 24 {
		bw = m.width
	}
	if bw < 10 {
		bw = 10
	}
	x0 = (m.width - bw) / 2
	if x0 < 0 {
		x0 = 0
	}
	y0 = (m.height - formBoxH) / 2
	if y0 < 0 {
		y0 = 0
	}
	if y0+formBoxH > m.height {
		y0 = m.height - formBoxH
		if y0 < 0 {
			y0 = 0
		}
	}
	return x0, y0, bw
}

// withFormOverlay merges the box returned by formBoxLines over a base view.
// Overlaid rows are wiped entirely (dimming the list) so no pointer glyph or
// text fragment leaks around the box edges.
func (m *Model) withFormOverlay(base string) string {
	lines := strings.Split(base, "\n")
	for len(lines) < m.height {
		lines = append(lines, blank(m.width))
	}
	x0, y0, bw := m.formGeometry()
	box := m.formBoxLines(bw)
	for i, row := range box {
		yy := y0 + i
		if yy < 0 || yy >= len(lines) {
			break
		}
		lines[yy] = placeBoxRow(row, x0, m.width)
	}
	return strings.Join(lines, "\n")
}

// placeBoxRow builds a full-width line containing the box row at column x,
// with the rest of the row blanked out (modal effect).
func placeBoxRow(row string, x, width int) string {
	out := []rune(strings.Repeat(" ", width))
	br := []rune(row)
	if x >= 0 {
		copy(out[x:], br)
	}
	return string(out)
}

// formBoxLines renders the ten overlay rows, each exactly bw runes wide.
func (m *Model) formBoxLines(bw int) []string {
	f := m.form
	title := " NEW SERVER "
	if f.mode == formEdit {
		title = " EDIT SERVER "
	}
	fill := bw - 2 - len(title)
	if fill < 0 {
		fill = 0
	}
	lines := make([]string, 0, formBoxH)
	lines = append(lines, "┌"+title+strings.Repeat("─", fill)+"┐")
	for i := 0; i < 6; i++ {
		lines = append(lines, m.formFieldRow(bw, fieldLabels[i], i))
	}
	hint := f.err
	if hint == "" {
		hint = "Tab/arrows move · Enter save · Esc cancel"
	}
	lines = append(lines, borderRow("  "+hint, bw))
	lines = append(lines, m.formButtonRow(bw))
	lines = append(lines, "└"+strings.Repeat("─", max(0, bw-2))+"┘")
	return lines
}

// formFieldRow renders one text-field row of the overlay.
func (m *Model) formFieldRow(bw int, label string, idx int) string {
	f := m.form
	prefix := "  " + padRight(label, 6) + ": "
	avail := bw - 2 - len(prefix)
	if avail < 1 {
		avail = 1
	}
	val := f.values[idx]
	if f.focus == formField(idx) {
		val += "_"
	}
	val = tailRunes(val, avail)
	return "│" + padRight(prefix+val, avail) + "│"
}

// formButtonRow renders the Enable/Cancel row.
func (m *Model) formButtonRow(bw int) string {
	f := m.form
	enable := "[Enable]"
	if f.focus == fieldEnable {
		enable = "<Enable>"
	}
	cancel := "[Cancel]"
	if f.focus == fieldCancel {
		cancel = "<Cancel>"
	}
	// Button hit zones (see handleFormMouse) start at content columns 6 and
	// 17; keep the spacing here in sync with those numbers.
	content := "      " + enable + "   " + cancel
	return borderRow(content, bw)
}

// borderRow pads/truncates inner content to bw-2 runes and wraps it in │.
func borderRow(inner string, bw int) string {
	avail := bw - 2
	if avail < 0 {
		avail = 0
	}
	return "│" + padRight(inner, avail) + "│"
}

// padRight pads s with spaces up to w runes (byte-safe only for ASCII
// prefixes; values already cut with tailRunes before padding).
func padRight(s string, w int) string {
	r := []rune(s)
	if len(r) >= w {
		return s
	}
	return s + strings.Repeat(" ", w-len(r))
}

// tailRunes returns the last n runes of s (cursor-friendly for input tail).
func tailRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[len(r)-n:])
}
