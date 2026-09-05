package icon

import "testing"

func TestIconForUsesYaziStyleDirectoriesAndExtensions(t *testing.T) {
	cases := map[string]string{
		"main.go":     fileNameIcons["go"],
		"unknown.xyz": fileIcon,
		"README.md":   fileNameIcons["readme.md"],
	}
	for name, want := range cases {
		if got := IconFor(name, false, false); got != want {
			t.Errorf("IconFor(%q) = %q, want %q", name, got, want)
		}
	}
	if got := IconFor("unknown", true, false); got != directoryIcon {
		t.Errorf("IconFor directory = %q, want %q", got, directoryIcon)
	}
	if got := IconFor("link", false, true); got != linkIcon {
		t.Errorf("IconFor symlink = %q, want %q", got, linkIcon)
	}
}

func TestMappingCoversYaziDefaultScale(t *testing.T) {
	// The source default contains 692 distinct filename entries; this guard
	// keeps accidental table pruning from silently losing full Yazi coverage.
	if len(fileNameIcons) < 680 {
		t.Fatalf("only %d filename icon mappings", len(fileNameIcons))
	}
}
