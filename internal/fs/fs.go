// Package fs implements the local file browser used by wznav's lower pane.
// It is intentionally small: read directory listing, sort, expose entries,
// and answer a few navigation primitives (parent, into). It does NOT
// recurse into subdirectories — the UI calls back into ListDir each time
// the user navigates into one.
package fs

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Item describes one directory entry shown in the file pane.
// IsDir is true for directories; Size and ModTime are zero for symlinks
// that we cannot stat (errors are swallowed to keep the UI snappy).
type Item struct {
	Name    string // base name, never includes the parent path
	IsDir   bool
	Size    int64
	ModTime int64 // unix seconds
}

// Browser is a tiny stateful directory cursor. It is safe for single-goroutine
// use (the TUI owns it). Callers should keep it on the stack of the UI model
// rather than passing it between goroutines.
type Browser struct {
	// Current is the absolute path the browser is currently pointing at.
	Current string
}

// New creates a Browser rooted at the given path. If start is empty or
// unusable, it falls back to $HOME, then to "/" as a last resort.
func New(start string) *Browser {
	b := &Browser{}
	if start == "" {
		if h, err := os.UserHomeDir(); err == nil {
			start = h
		} else {
			start = "/"
		}
	}
	if abs, err := filepath.Abs(start); err == nil {
		start = abs
	}
	b.Current = start
	return b
}

// ListDir returns a sorted listing of the current directory.
// Directories come first, then files; both groups sorted case-insensitive.
// Hidden entries (names starting with '.') are kept but pushed to the bottom
// of their group (matching common file-manager UX).
func (b *Browser) ListDir() ([]Item, error) {
	ents, err := os.ReadDir(b.Current)
	if err != nil {
		return nil, err
	}
	var dirs, files []Item
	for _, e := range ents {
		// Skip unreadable entries silently.
		info, ierr := e.Info()
		if ierr != nil {
			continue
		}
		it := Item{
			Name:    e.Name(),
			IsDir:   e.IsDir(),
			Size:    info.Size(),
			ModTime: info.ModTime().Unix(),
		}
		if e.IsDir() {
			dirs = append(dirs, it)
		} else {
			files = append(files, it)
		}
	}
	sort.SliceStable(dirs, func(i, j int) bool { return lessHidden(dirs[i].Name, dirs[j].Name) })
	sort.SliceStable(files, func(i, j int) bool { return lessHidden(files[i].Name, files[j].Name) })
	out := make([]Item, 0, len(dirs)+len(files))
	out = append(out, dirs...)
	out = append(out, files...)
	return out, nil
}

func lessHidden(a, b string) bool {
	ah := strings.HasPrefix(a, ".")
	bh := strings.HasPrefix(b, ".")
	if ah != bh {
		return !ah // visible names come first
	}
	return strings.ToLower(a) < strings.ToLower(b)
}

// Parent moves the cursor up one directory. Returns ErrAtRoot if the cursor
// is already at filesystem root (so the UI can refuse the operation).
var ErrAtRoot = errors.New("already at filesystem root")

func (b *Browser) Parent() error {
	parent := filepath.Dir(b.Current)
	if parent == b.Current {
		return ErrAtRoot
	}
	b.Current = parent
	return nil
}

// Into moves the cursor into the named subdirectory (must be a direct child
// of Current). Returns the absolute path on success.
func (b *Browser) Into(name string) (string, error) {
	target := filepath.Join(b.Current, name)
	st, err := os.Stat(target)
	if err != nil {
		return "", err
	}
	if !st.IsDir() {
		return "", errors.New("not a directory: " + name)
	}
	b.Current = target
	return b.Current, nil
}

// IsDir reports whether the named child of Current is a directory.
// Used by the UI to decide between "open with editor" and "navigate into".
func (b *Browser) IsDir(name string) bool {
	st, err := os.Stat(filepath.Join(b.Current, name))
	return err == nil && st.IsDir()
}

// PathOf returns the absolute path of a child item of Current without
// changing the cursor.
func (b *Browser) PathOf(name string) string {
	return filepath.Join(b.Current, name)
}

// DetectHidden reports whether a name starts with '.'. Exported so tests
// can verify the sort logic.
func DetectHidden(name string) bool { return strings.HasPrefix(name, ".") }

// HasPermission is a quick gate for symlinks / unusual mounts.
func HasPermission(name string) bool {
	_, err := os.Lstat(name)
	if err != nil {
		var pe *fs.PathError
		return !errors.As(err, &pe)
	}
	return true
}
