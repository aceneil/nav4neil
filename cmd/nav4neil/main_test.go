package main

// M11 tests for the CLI surface: flag parsing still works after the
// parseOptions refactor, and the launch-menu policy is correct for a bare
// `nav4neil`, for `--section …` callers (layout scripts must never see a
// menu) and for the --menu / --no-menu overrides. The menu itself is covered
// in internal/ui (menu.go + m11_menu_test.go); here we pin the wiring:
// `--section` sets section.set even when the value is the default `both`.

import (
	"bytes"
	"errors"
	"flag"
	"io"
	"os"
	"testing"

	"github.com/aceneil/nav4neil/internal/ui"
)

// menuDecision mirrors main()'s menu gate for one parsed command line.
func menuDecision(t *testing.T, opts *options, interactive bool) bool {
	t.Helper()
	return ui.ShouldShowMenu(ui.MenuPolicy{
		SectionProvided: opts.section.set,
		Force:           opts.menu,
		Suppress:        opts.noMenu,
	}, interactive)
}

// TestParseOptions_LaunchMenuPolicy: a bare `nav4neil` asks for the menu;
// any explicit --section (including `--section both` / `all`) suppresses it;
// --menu forces it, --no-menu suppresses it and wins over --menu.
func TestParseOptions_LaunchMenuPolicy(t *testing.T) {
	cases := []struct {
		name        string
		args        []string
		wantSection ui.Section
		wantSet     bool
		wantMenu    bool
	}{
		{"bare launch → menu", nil, ui.SectionBoth, false, true},
		{"--section servers → no menu", []string{"--section", "servers"}, ui.SectionServers, true, false},
		{"--section files → no menu", []string{"--section=files"}, ui.SectionFiles, true, false},
		{"explicit --section both → no menu", []string{"--section=both"}, ui.SectionBoth, true, false},
		{"--section all → no menu", []string{"--section", "all"}, ui.SectionBoth, true, false},
		{"--menu forces the menu", []string{"--menu"}, ui.SectionBoth, false, true},
		{"--no-menu skips it (both layout)", []string{"--no-menu"}, ui.SectionBoth, false, false},
		{"--no-menu wins over --menu", []string{"--menu", "--no-menu"}, ui.SectionBoth, false, false},
		{"--menu forces it next to --section", []string{"--menu", "--section", "servers"}, ui.SectionServers, true, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			opts, err := parseOptions(c.args, io.Discard)
			if err != nil {
				t.Fatalf("parseOptions(%v): %v", c.args, err)
			}
			if opts.section.set != c.wantSet {
				t.Errorf("section.set = %v, want %v (raw %q)", opts.section.set, c.wantSet, opts.section.value)
			}
			sec, err := ui.ParseSection(opts.section.value)
			if err != nil {
				t.Fatalf("ParseSection(%q): %v", opts.section.value, err)
			}
			if sec != c.wantSection {
				t.Errorf("section = %v, want %v", sec, c.wantSection)
			}
			if got := menuDecision(t, opts, true); got != c.wantMenu {
				t.Errorf("menu shown = %v, want %v", got, c.wantMenu)
			}
			// Headless (--list / no TTY) launches never show a menu.
			if menuDecision(t, opts, false) {
				t.Error("a non-interactive launch must never show the menu")
			}
			// The default section stays "both" so the layout/exec semantics
			// of a bare launch are unchanged when the menu is skipped.
			if !c.wantSet && opts.section.value != "both" {
				t.Errorf("default section value = %q, want \"both\"", opts.section.value)
			}
		})
	}
}

// TestParseOptions_ExistingFlagsStillParse guards the refactor: the flags the
// layout scripts and installers rely on keep their values.
func TestParseOptions_ExistingFlagsStillParse(t *testing.T) {
	args := []string{"--start-dir", "/tmp/x", "--ws-port", "41234", "--no-ws", "--list"}
	opts, err := parseOptions(args, io.Discard)
	if err != nil {
		t.Fatalf("parseOptions(%v): %v", args, err)
	}
	if opts.startDir != "/tmp/x" {
		t.Errorf("startDir = %q, want /tmp/x", opts.startDir)
	}
	if opts.wsPort != 41234 {
		t.Errorf("wsPort = %d, want 41234", opts.wsPort)
	}
	if !opts.noWS {
		t.Error("--no-ws must set noWS")
	}
	if !opts.listMode {
		t.Error("--list must set listMode")
	}
	if opts.showHelp || opts.showVer {
		t.Error("--help/--version must stay false")
	}
	if opts.section.set {
		t.Error("no --section was passed: section.set must stay false")
	}
}

// TestParseOptions_Defaults: without flags the start dir falls back to $HOME
// and the ws port to the package default.
func TestParseOptions_Defaults(t *testing.T) {
	t.Setenv("HOME", "/home/tester")
	opts, err := parseOptions(nil, io.Discard)
	if err != nil {
		t.Fatalf("parseOptions(nil): %v", err)
	}
	if opts.startDir != "/home/tester" {
		t.Errorf("startDir default = %q, want $HOME", opts.startDir)
	}
	if opts.wsPort <= 0 {
		t.Errorf("wsPort default = %d, want a positive port", opts.wsPort)
	}
}

// TestParseOptions_HelpAndErrors: --help is a normal bool, -h returns
// flag.ErrHelp (main exits 0), and an unknown flag is an error (main exits 2).
func TestParseOptions_HelpAndErrors(t *testing.T) {
	opts, err := parseOptions([]string{"--help"}, io.Discard)
	if err != nil || !opts.showHelp {
		t.Fatalf("--help must set showHelp without error: %v %+v", err, opts)
	}

	var buf bytes.Buffer
	if _, err := parseOptions([]string{"-h"}, &buf); !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("-h must return flag.ErrHelp, got %v", err)
	}
	if !bytes.Contains(buf.Bytes(), []byte("Usage of nav4neil")) {
		t.Errorf("-h must print the usage; got:\n%s", buf.String())
	}

	if _, err := parseOptions([]string{"--bogus"}, io.Discard); err == nil {
		t.Error("an unknown flag must be an error")
	}
}

// TestHasTerminal_IsFalseWithoutATty keeps the headless path honest: the
// tests run without a controlling terminal (go test's stdin is not a tty in
// CI), so hasTerminal() must report false there — that is the flag main()
// uses to skip both the menu and the TUI.
func TestHasTerminal_IsFalseWithoutATty(t *testing.T) {
	f, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	old := os.Stdin
	os.Stdin = f
	defer func() { os.Stdin = old }()
	if hasTerminal() {
		t.Error("/dev/null must not be reported as a terminal")
	}
}
