package ui

// M11 tests: the launch menu (bare `nav4neil` → pick servers / files / all),
// the pure menu policy used by main() to decide whether that menu appears at
// all, and the `all`-mode focus switch (Tab / 1 / 2) plus its always-on hint
// row. The menu is a separate bubbletea model (menu.go), so it is driven here
// directly with key messages — no terminal required.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// teaMsgWindowSize builds the resize message bubbletea delivers in the real
// program; the menu uses it to pin its rendering width.
func teaMsgWindowSize(w, h int) tea.WindowSizeMsg {
	return tea.WindowSizeMsg{Width: w, Height: h}
}

// TestMenu_OptionOrderAndSectionMapping pins the menu rows and the index →
// Section mapping Enter commits: servers → SectionServers, files →
// SectionFiles, all → SectionBoth (the historical two-pane layout).
func TestMenu_OptionOrderAndSectionMapping(t *testing.T) {
	want := []struct {
		label string
		sec   Section
	}{
		{"servers", SectionServers},
		{"files", SectionFiles},
		{"all", SectionBoth},
	}
	if len(MenuOptions) != len(want) {
		t.Fatalf("menu must offer %d options, got %d: %+v", len(want), len(MenuOptions), MenuOptions)
	}
	for i, w := range want {
		if got := MenuOptions[i].Label; got != w.label {
			t.Errorf("option %d label = %q, want %q", i, got, w.label)
		}
		if MenuOptions[i].Desc == "" {
			t.Errorf("option %d (%s) must carry a description", i, w.label)
		}
		sec, ok := MenuSectionAt(i)
		if !ok {
			t.Errorf("MenuSectionAt(%d) must map", i)
			continue
		}
		if sec != w.sec {
			t.Errorf("MenuSectionAt(%d) = %v, want %v", i, sec, w.sec)
		}
	}
	if _, ok := MenuSectionAt(-1); ok {
		t.Error("a negative index must not map to a section")
	}
	if _, ok := MenuSectionAt(len(MenuOptions)); ok {
		t.Error("an out-of-range index must not map to a section")
	}
}

// TestMenu_DefaultHighlightAndEnterCommitsServers: the first row (servers)
// is highlighted by default, nothing is committed before Enter, and Enter
// quits the picker with the servers section.
func TestMenu_DefaultHighlightAndEnterCommitsServers(t *testing.T) {
	m := NewMenuModel()
	if m.Cursor() != 0 {
		t.Fatalf("default highlight must be the first row (servers), got %d", m.Cursor())
	}
	if _, ok := m.Section(); ok {
		t.Fatal("no section may be committed before Enter")
	}
	_, cmd := m.Update(teaKeyMsg("enter"))
	if cmd == nil {
		t.Fatal("Enter must quit the menu program (nil cmd)")
	}
	sec, ok := m.Section()
	if !ok || sec != SectionServers {
		t.Fatalf("Enter on the default row must commit SectionServers, got %v (ok=%v)", sec, ok)
	}
	if m.Cancelled() {
		t.Fatal("Enter must not count as a cancel")
	}
}

// TestMenu_KeysMoveAndCommitEveryRow drives ↑/↓ and j/k (with wrap-around)
// and checks that Enter commits whatever row is highlighted.
func TestMenu_KeysMoveAndCommitEveryRow(t *testing.T) {
	cases := []struct {
		name       string
		keys       []string
		wantCursor int
		want       Section
	}{
		{"enter on the default row", []string{"enter"}, 0, SectionServers},
		{"down then enter", []string{"down", "enter"}, 1, SectionFiles},
		{"j j then enter", []string{"j", "j", "enter"}, 2, SectionBoth},
		{"up wraps to the last row", []string{"up", "enter"}, 2, SectionBoth},
		{"k wraps to the last row", []string{"k", "enter"}, 2, SectionBoth},
		{"down wraps back to the first row", []string{"down", "down", "down", "enter"}, 0, SectionServers},
		{"down then up stays on the first row", []string{"down", "up", "enter"}, 0, SectionServers},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := NewMenuModel()
			for _, k := range c.keys {
				m.Update(teaKeyMsg(k))
			}
			if m.Cursor() != c.wantCursor {
				t.Fatalf("cursor = %d, want %d", m.Cursor(), c.wantCursor)
			}
			sec, ok := m.Section()
			if !ok || sec != c.want {
				t.Fatalf("committed section = %v (ok=%v), want %v", sec, ok, c.want)
			}
		})
	}
}

// TestMenu_EscAndQQuitWithoutChoosing: Esc/q/Ctrl+C leave the launch without
// a section, which main() turns into "exit the program".
func TestMenu_EscAndQQuitWithoutChoosing(t *testing.T) {
	for _, key := range []string{"esc", "q", "ctrl+c"} {
		m := NewMenuModel()
		_, cmd := m.Update(teaKeyMsg(key))
		if cmd == nil {
			t.Fatalf("%s must quit the menu program", key)
		}
		if !m.Cancelled() {
			t.Errorf("%s must mark the menu as cancelled", key)
		}
		if _, ok := m.Section(); ok {
			t.Errorf("%s must not commit a section", key)
		}
	}
}

// TestMenu_ViewListsEveryOptionAndKeys: the rendered menu shows all three
// choices, the ▶ highlight on the first row, and the key help; every line
// fits the terminal width.
func TestMenu_ViewListsEveryOptionAndKeys(t *testing.T) {
	m := NewMenuModel()
	m.Update(teaMsgWindowSize(72, 20))
	v := m.View()
	for _, want := range []string{"nav4neil", "servers", "files", "all", "Enter", "Esc", "j/k"} {
		if !strings.Contains(v, want) {
			t.Errorf("menu view missing %q:\n%s", want, v)
		}
	}
	if !strings.Contains(v, "▶ servers") {
		t.Errorf("the first row must carry the ▶ highlight:\n%s", v)
	}

	// Moving down moves the highlight with it.
	m.Update(teaKeyMsg("down"))
	v = m.View()
	if !strings.Contains(v, "▶ files") || strings.Contains(v, "▶ servers") {
		t.Errorf("↓ must move the ▶ marker to the files row:\n%s", v)
	}

	// Nothing may overflow a narrow terminal.
	m.Update(teaMsgWindowSize(20, 10))
	for _, line := range strings.Split(m.View(), "\n") {
		if len([]rune(line)) > 20 {
			t.Errorf("menu line wider than the terminal (%d runes): %q", len([]rune(line)), line)
		}
	}
}

// TestShouldShowMenu pins the rule that keeps layout scripts working: the
// menu appears only for a bare, interactive launch.
func TestShouldShowMenu(t *testing.T) {
	cases := []struct {
		name        string
		policy      MenuPolicy
		interactive bool
		want        bool
	}{
		{"bare launch on a TTY shows the menu", MenuPolicy{}, true, true},
		{"explicit --section suppresses it", MenuPolicy{SectionProvided: true}, true, false},
		{"explicit --section both suppresses it too", MenuPolicy{SectionProvided: true, Force: false}, true, false},
		{"--no-menu suppresses it", MenuPolicy{Suppress: true}, true, false},
		{"--no-menu wins over --menu", MenuPolicy{Force: true, Suppress: true}, true, false},
		{"--menu forces it without a section", MenuPolicy{Force: true}, true, true},
		{"--menu forces it even with --section", MenuPolicy{Force: true, SectionProvided: true}, true, true},
		{"no TTY (pipe/CI) never shows a menu", MenuPolicy{}, false, false},
		{"no TTY with --menu still shows nothing", MenuPolicy{Force: true}, false, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ShouldShowMenu(c.policy, c.interactive); got != c.want {
				t.Errorf("ShouldShowMenu(%+v, interactive=%v) = %v, want %v",
					c.policy, c.interactive, got, c.want)
			}
		})
	}
}

// TestParseSection_AllAlias: the menu's "all" row is also accepted on the
// command line as `--section all`.
func TestParseSection_AllAlias(t *testing.T) {
	for _, in := range []string{"all", "ALL", " All "} {
		got, err := ParseSection(in)
		if err != nil {
			t.Errorf("ParseSection(%q) unexpected error: %v", in, err)
			continue
		}
		if got != SectionBoth {
			t.Errorf("ParseSection(%q) = %v, want SectionBoth", in, got)
		}
	}
}

// TestAllMode_TabSwitchesFocusAndHintStaysVisible covers the second M11
// requirement: in `all` mode Tab (and 1/2) move the focus between the servers
// list and the file browser, and a persistent hint row directly above the
// status bar names that key.
func TestAllMode_TabSwitchesFocusAndHintStaysVisible(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := NewModel(dir, nil, SectionBoth)
	m.width, m.height = 80, 24

	fileRow := -1
	for i, it := range m.fileView {
		if it.Name == "a.txt" {
			fileRow = i
			break
		}
	}
	if fileRow < 0 {
		t.Fatalf("a.txt missing from the view: %+v", m.fileView)
	}
	// renderFileRow draws the focus pointer on the cursor row, so pin the
	// file cursor to a.txt before checking which pane owns the focus.
	m.fileCur = fileRow

	assertHint := func(stage string) {
		t.Helper()
		lines := strings.Split(m.View(), "\n")
		if len(lines) < 2 {
			t.Fatalf("%s: view too short:\n%s", stage, m.View())
		}
		hint, status := lines[len(lines)-2], lines[len(lines)-1]
		if !strings.Contains(hint, paneHintText) {
			t.Fatalf("%s: the pane-switch hint must be the row above the status bar, got %q", stage, hint)
		}
		if !strings.Contains(hint, "Tab") || !strings.Contains(hint, "服务器/文件") {
			t.Fatalf("%s: hint must spell out the Tab switch, got %q", stage, hint)
		}
		if !strings.Contains(status, "pane=") {
			t.Fatalf("%s: the last row must stay the status line, got %q", stage, status)
		}
	}

	if m.focus != paneServers {
		t.Fatalf("all mode must start focused on the servers pane, got %d", m.focus)
	}
	assertHint("start")

	// Tab → files: the file pane owns the focus pointer, the server pane
	// falls back to the unfocused ▷ marker.
	m.Update(teaKeyMsg("tab"))
	if m.focus != paneFiles {
		t.Fatalf("Tab must move the focus to the files pane, got %d", m.focus)
	}
	if got := m.renderFileRow(fileRow); !strings.HasPrefix(got, " ▶ ") {
		t.Fatalf("focused file row must start with the ▶ pointer, got %q", got)
	}
	if !strings.Contains(m.View(), "▷") {
		t.Fatalf("the servers pane must show the unfocused ▷ marker:\n%s", m.View())
	}
	assertHint("files focused")

	// Tab again → back to the servers pane (two panes, so it cycles).
	m.Update(teaKeyMsg("tab"))
	if m.focus != paneServers {
		t.Fatalf("the second Tab must cycle back to the servers pane, got %d", m.focus)
	}
	if got := m.renderFileRow(fileRow); !strings.HasPrefix(got, " ▷ ") {
		t.Fatalf("unfocused file row must start with ▷, got %q", got)
	}
	assertHint("servers focused again")

	// 1 / 2 jump straight to one pane.
	m.Update(teaKeyMsg("2"))
	if m.focus != paneFiles {
		t.Fatalf("2 must focus the files pane, got %d", m.focus)
	}
	m.Update(teaKeyMsg("1"))
	if m.focus != paneServers {
		t.Fatalf("1 must focus the servers pane, got %d", m.focus)
	}
	assertHint("after 1/2")
}

// TestPaneHintRowIsAllModeOnly: the sidebar sections have no second pane to
// switch to, so they must not render the hint row.
func TestPaneHintRowIsAllModeOnly(t *testing.T) {
	for _, sec := range []Section{SectionServers, SectionFiles} {
		m := NewModel("/tmp", nil, sec)
		m.width, m.height = 80, 24
		if strings.Contains(m.View(), "Tab: 切换 服务器/文件") {
			t.Errorf("section %v must not render the all-mode pane hint:\n%s", sec, m.View())
		}
	}
}
