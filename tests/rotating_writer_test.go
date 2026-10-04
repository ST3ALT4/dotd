// Package tests — rotating log writer tests.
package tests

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/ST3ALT4/dotd/src"
)

// ── rotatingWriter ────────────────────────────────────────────────────────────

func TestRotatingWriter_WritesData(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")

	rw, err := src.NewRotatingWriter(path, 1024)
	if err != nil {
		t.Fatalf("NewRotatingWriter: %v", err)
	}
	defer rw.Close()

	msg := "hello world\n"
	n, err := rw.Write([]byte(msg))
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if n != len(msg) {
		t.Errorf("wrote %d bytes, want %d", n, len(msg))
	}

	rw.Close()
	data, _ := os.ReadFile(path)
	if string(data) != msg {
		t.Errorf("file content = %q, want %q", data, msg)
	}
}

func TestRotatingWriter_RotatesWhenLimitExceeded(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rot.log")

	// maxSize = 10 bytes so the second write triggers rotation.
	rw, err := src.NewRotatingWriter(path, 10)
	if err != nil {
		t.Fatalf("NewRotatingWriter: %v", err)
	}
	defer rw.Close()

	first := []byte("1234567890") // exactly maxSize
	if _, err := rw.Write(first); err != nil {
		t.Fatalf("first Write: %v", err)
	}

	second := []byte("NEW\n")
	if _, err := rw.Write(second); err != nil {
		t.Fatalf("second Write: %v", err)
	}
	rw.Close()

	// The backup file must contain the first write.
	backup := path + ".1"
	bdata, err := os.ReadFile(backup)
	if err != nil {
		t.Fatalf("backup file missing: %v", err)
	}
	if !bytes.Equal(bdata, first) {
		t.Errorf("backup = %q, want %q", bdata, first)
	}

	// The active log must contain only the second write.
	adata, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("active log missing: %v", err)
	}
	if !bytes.Equal(adata, second) {
		t.Errorf("active log = %q, want %q", adata, second)
	}
}

func TestRotatingWriter_OverwritesPreviousBackup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rot.log")

	// Pre-create a stale backup so we can verify it gets replaced.
	if err := os.WriteFile(path+".1", []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}

	rw, err := src.NewRotatingWriter(path, 5)
	if err != nil {
		t.Fatalf("NewRotatingWriter: %v", err)
	}
	defer rw.Close()

	rw.Write([]byte("12345"))  // fills limit
	rw.Write([]byte("fresh\n")) // triggers rotation
	rw.Close()

	bdata, _ := os.ReadFile(path + ".1")
	if strings.Contains(string(bdata), "stale") {
		t.Error("old backup was not replaced")
	}
}

func TestRotatingWriter_ConcurrentWrites(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "concurrent.log")

	rw, err := src.NewRotatingWriter(path, 1<<20)
	if err != nil {
		t.Fatalf("NewRotatingWriter: %v", err)
	}
	defer rw.Close()

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rw.Write([]byte("line\n")) //nolint:errcheck
		}()
	}
	wg.Wait()

	// No assertion on exact content — just must not panic or deadlock.
}

func TestRotatingWriter_SeedsExistingSize(t *testing.T) {
	// If the log file is already at/near maxSize when the writer is opened,
	// the very first write must trigger rotation rather than silently appending
	// past the cap.
	dir := t.TempDir()
	path := filepath.Join(dir, "big.log")

	// Pre-fill to exactly maxSize.
	existing := bytes.Repeat([]byte("x"), 20)
	if err := os.WriteFile(path, existing, 0o644); err != nil {
		t.Fatal(err)
	}

	rw, err := src.NewRotatingWriter(path, 20) // maxSize == existing size
	if err != nil {
		t.Fatalf("NewRotatingWriter: %v", err)
	}
	defer rw.Close()

	rw.Write([]byte("trigger\n"))
	rw.Close()

	// The original content should now be in the backup.
	bdata, err := os.ReadFile(path + ".1")
	if err != nil {
		t.Fatalf("backup not created: %v", err)
	}
	if !bytes.Equal(bdata, existing) {
		t.Errorf("backup = %q, want pre-existing content", bdata)
	}
}
