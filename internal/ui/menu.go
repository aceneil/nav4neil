// M11 launch picker. When nav4neil is started by hand — a bare `nav4neil`
// with no --section flag — main() first shows this one-level menu and then
// runs only the chosen area:
//
//	servers → SectionServers (same as --section servers)
//	files   → SectionFiles   (same as --section files)
//	all     → SectionBoth    (same as --section both = the two-pane layout;
//	                          Tab / 1 / 2 move the focus between the servers
//	                          list and the file browser, and an always-on
//	                          hint row above the status bar says so)
//
// ↑/↓ (or j/k) move, Enter confirms, Esc/q (or Ctrl+C) aborts the launch.
// The first option — servers — is highlighted by default.
//
// Callers that pass --section (even an explicit `both`) never see the menu;
// --menu forces it and --no-menu suppresses it. Headless invocations
// (--list, or stdin that is not a terminal) never show it either — see
// ShouldShowMenu.
//
// The picker is a deliberately separate, tiny bubbletea program rather than
// an overlay inside Model: the Section decides which data the model loads
// (a files-only nav never parses ~/.ssh/config), so the choice has to be
// made before NewModel runs.
package ui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// MenuOption is one row of the M11 launch menu.
type MenuOption struct {
	Section Section // section to launch when this row is confirmed
	Label   string  // short name shown on the row (servers/files/all)
	Desc    string  // one-line explanation
}

// MenuOptions is the fixed first-level menu in display order; the first
// entry is highlighted by default.
var MenuOptions = []MenuOption{
	{Section: SectionServers, Label: "servers", Desc: "只显示服务器列表（等同 --section servers）"},
	{Section: SectionFiles, Label: "files", Desc: "只显示文件浏览（等同 --section files）"},
	{Section: SectionBoth, Label: "all", Desc: "服务器 + 文件两区（等同 --section both；Tab 切换焦点）"},
}

// MenuSectionAt maps a menu row index to the Section it launches. ok is
// false when i is out of range (defensive; callers fall back safely).
func MenuSectionAt(i int) (Section, bool) {
	if i < 0 || i >= len(MenuOptions) {
		return SectionBoth, false
	}
	return MenuOptions[i].Section, true
}

// MenuPolicy captures the CLI flags that influence menu visibility.
type MenuPolicy struct {
	SectionProvided bool // --section was passed (any value, including both)
	Force           bool // --menu
	Suppress        bool // --no-menu
}

// ShouldShowMenu reports whether the launch menu must be shown. interactive
// is false for --list and for a stdin that is not a terminal: those keep the
// historical behaviour and never show a menu. --no-menu always wins, --menu
// forces the menu for any interactive launch, and otherwise the menu appears
// exactly when the caller did not pass --section.
func ShouldShowMenu(p MenuPolicy, interactive bool) bool {
	if !interactive || p.Suppress {
		return false
	}
	if p.Force {
		return true
	}
	return !p.SectionProvided
}

// MenuModel is the first-level launch picker.
type MenuModel struct {
	cursor    int
	chosen    Section
	committed bool
	cancelled bool
	width     int
}

// NewMenuModel returns the picker with the first option highlighted.
func NewMenuModel() *MenuModel { return &MenuModel{} }

// Init implements tea.Model.
func (m *MenuModel) Init() tea.Cmd { return nil }

// Update implements tea.Model: ↑/↓ (j/k) move with wrap-around, Enter
// commits the highlighted row and quits, Esc/q/Ctrl+C quit without a choice.
func (m *MenuModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		return m, nil
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			m.cursor--
			if m.cursor < 0 {
				m.cursor = len(MenuOptions) - 1
			}
		case "down", "j":
			m.cursor = (m.cursor + 1) % len(MenuOptions)
		case "enter":
			if sec, ok := MenuSectionAt(m.cursor); ok {
				m.chosen, m.committed = sec, true
			}
			return m, tea.Quit
		case "esc", "q", "ctrl+c":
			m.cancelled = true
			return m, tea.Quit
		}
	}
	return m, nil
}

// Section returns the committed launch section. ok is false until Enter was
// pressed (a cancelled or still-open menu commits nothing).
func (m *MenuModel) Section() (Section, bool) {
	if !m.committed {
		return SectionBoth, false
	}
	return m.chosen, true
}

// Cancelled reports whether the user aborted the menu with Esc/q/Ctrl+C.
func (m *MenuModel) Cancelled() bool { return m.cancelled }

// Cursor exposes the highlighted row index (tests / callers).
func (m *MenuModel) Cursor() int { return m.cursor }

// View renders the menu.
func (m *MenuModel) View() string {
	w := m.width
	if w <= 0 {
		w = 72
	}
	var b strings.Builder
	b.WriteString(truncRunes("nav4neil · 选择要显示的区域", w))
	b.WriteString("\n\n")
	for i, opt := range MenuOptions {
		ptr := "  "
		if i == m.cursor {
			ptr = "▶ "
		}
		line := ptr + padRunes(opt.Label, 8) + opt.Desc
		b.WriteString(truncRunes(padRunes(line, w), w))
		b.WriteByte('\n')
	}
	b.WriteByte('\n')
	b.WriteString(truncRunes("↑/↓ 或 j/k 移动 · Enter 确认 · Esc/q 退出", w))
	return b.String()
}

// RunMenu shows the picker on the real terminal and returns the chosen
// section. ok is false when the user cancelled (Esc/q) — the caller must
// then exit without starting the TUI.
func RunMenu() (section Section, ok bool, err error) {
	prog := tea.NewProgram(NewMenuModel(), tea.WithAltScreen())
	final, err := prog.Run()
	if err != nil {
		return SectionBoth, false, err
	}
	if mm, isMenu := final.(*MenuModel); isMenu {
		if sec, committed := mm.Section(); committed {
			return sec, true, nil
		}
	}
	return SectionBoth, false, nil
}
