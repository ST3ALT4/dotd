// Package tests — uniquePath (collision-free naming) tests.
package tests

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ST3ALT4/dotd/src"
)

func TestUniquePath_NoCollision(t *testing.T) {
	dir := t.TempDir()
	got := src.UniquePath(dir, "file.txt")
	want := filepath.Join(dir, "file.txt")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestUniquePath_SingleCollision(t *testing.T) {
	dir := t.TempDir()
	touch(t, filepath.Join(dir, "file.txt"))

	got := src.UniquePath(dir, "file.txt")
	want := filepath.Join(dir, "file (1).txt")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestUniquePath_MultipleCollisions(t *testing.T) {
	dir := t.TempDir()
	touch(t, filepath.Join(dir, "file.txt"))
	touch(t, filepath.Join(dir, "file (1).txt"))
	touch(t, filepath.Join(dir, "file (2).txt"))

	got := src.UniquePath(dir, "file.txt")
	want := filepath.Join(dir, "file (3).txt")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestUniquePath_PreservesExtension(t *testing.T) {
	dir := t.TempDir()
	touch(t, filepath.Join(dir, "archive.tar.gz"))

	// filepath.Ext only strips the last extension (.gz), so base = "archive.tar".
	got := src.UniquePath(dir, "archive.tar.gz")
	want := filepath.Join(dir, "archive.tar (1).gz")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestUniquePath_NoExtensionFile(t *testing.T) {
	dir := t.TempDir()
	touch(t, filepath.Join(dir, "README"))

	got := src.UniquePath(dir, "README")
	want := filepath.Join(dir, "README (1)")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestUniquePath_SymlinkCountsAsOccupied(t *testing.T) {
	dir := t.TempDir()

	// Create a symlink at "link.pdf" — uniquePath uses Lstat so it must treat
	// the symlink as an existing file.
	target := filepath.Join(dir, "real.pdf")
	touch(t, target)
	link := filepath.Join(dir, "link.pdf")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks not supported: %v", err)
	}

	got := src.UniquePath(dir, "link.pdf")
	want := filepath.Join(dir, "link (1).pdf")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
