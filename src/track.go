package src

import (
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
)

// Extensions browsers use for unfinished downloads. These are never moved.
var partialExts = map[string]bool{
	".crdownload": true, // Chrome / Edge
	".part":       true, // Firefox
	".download":   true, // Safari
	".opdownload": true, // Opera
	".partial":    true,
	".tmp":        true,
}

// DownloadsDir returns ~/Downloads.
func DownloadsDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("finding home directory: %w", err)
	}
	return filepath.Join(home, "Downloads"), nil
}

type fileState struct {
	size    int64
	modNano int64
}

// Tracker organises the top level of root according to cfg.
type Tracker struct {
	root    string
	cfg     *Config
	pending map[string]fileState // files seen once, waiting to be stable
	lastMod int64                // root dir mtime (ns) at the last scan
}

// Poll is called on every tick. It costs a single stat() call and only
// runs a full Scan when the directory's mtime changed (a file was added,
// removed or renamed) or when files are still waiting to become stable.
func (t *Tracker) Poll() {
	info, err := os.Stat(t.root)
	if err != nil {
		log.Printf("poll: %v", err)
		return
	}
	// Read mtime *before* scanning: a change that lands mid-scan makes the
	// next Poll see a different value and scan again.
	mod := info.ModTime().UnixNano()
	if mod == t.lastMod && len(t.pending) == 0 {
		return
	}
	t.lastMod = mod
	t.Scan()
}

func NewTracker(root string, cfg *Config) *Tracker {
	return &Tracker{root: root, cfg: cfg, pending: make(map[string]fileState)}
}

// Scan looks at the top level of root once. A file is only moved when its
// size and mtime are unchanged between two consecutive scans, so files that
// are still being written are left alone.
func (t *Tracker) Scan() {
	entries, err := os.ReadDir(t.root)
	if err != nil {
		log.Printf("scan: %v", err)
		return
	}

	seen := make(map[string]bool, len(entries))

	for _, e := range entries {
		name := e.Name()
		if !e.Type().IsRegular() || strings.HasPrefix(name, ".") {
			continue // skip dirs, symlinks, hidden files
		}

		ext := strings.ToLower(filepath.Ext(name))
		if partialExts[ext] {
			continue
		}
		loc, ok := t.cfg.LocationFor(ext)
		if !ok {
			continue
		}

		info, err := e.Info()
		if err != nil {
			continue
		}

		path := filepath.Join(t.root, name)
		seen[path] = true
		cur := fileState{size: info.Size(), modNano: info.ModTime().UnixNano()}

		if prev, ok := t.pending[path]; !ok || prev != cur {
			t.pending[path] = cur
			continue // new or still changing: check again next scan
		}

		dest, err := t.move(path, filepath.Join(t.root, loc))
		if err != nil {
			log.Printf("move %s: %v", name, err)
			delete(t.pending, path) // retried on the next directory change
			continue
		}
		log.Printf("moved %s -> %s", name, dest)
		delete(t.pending, path)
		delete(seen, path)
	}

	// forget files that disappeared
	for p := range t.pending {
		if !seen[p] {
			delete(t.pending, p)
		}
	}
}

// move renames src into dir, adding " (1)", " (2)"... on name collisions.
func (t *Tracker) move(src, dir string) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	dest := uniquePath(dir, filepath.Base(src))
	if err := os.Rename(src, dest); err != nil {
		return "", err
	}
	return dest, nil
}

func uniquePath(dir, name string) string {
	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)
	dest := filepath.Join(dir, name)
	for i := 1; ; i++ {
		if _, err := os.Lstat(dest); errors.Is(err, fs.ErrNotExist) {
			return dest
		}
		dest = filepath.Join(dir, fmt.Sprintf("%s (%d)%s", base, i, ext))
	}
}
