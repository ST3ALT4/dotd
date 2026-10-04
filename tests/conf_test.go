// Package tests contains integration-style tests for the src package.
// Tests that touch the filesystem use t.TempDir() so all artefacts are
// cleaned up automatically when the test exits.
package tests

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ST3ALT4/dotd/src"
)

// ── DefaultConfig ─────────────────────────────────────────────────────────────

func TestDefaultConfig_NotEmpty(t *testing.T) {
	cfg := src.DefaultConfig()
	if len(cfg.Files) == 0 {
		t.Fatal("DefaultConfig returned no file entries")
	}
}

func TestDefaultConfig_ExtensionsHaveLeadingDot(t *testing.T) {
	for _, f := range src.DefaultConfig().Files {
		if !strings.HasPrefix(f.Extension, ".") {
			t.Errorf("extension %q missing leading dot", f.Extension)
		}
	}
}

func TestDefaultConfig_LocationsAreRelative(t *testing.T) {
	for _, f := range src.DefaultConfig().Files {
		if filepath.IsAbs(f.Location) {
			t.Errorf("location %q must be relative", f.Location)
		}
	}
}

// ── InitConfig ────────────────────────────────────────────────────────────────

// overrideHome temporarily redirects os.UserHomeDir results by setting HOME.
func overrideHome(t *testing.T, dir string) {
	t.Helper()
	orig := os.Getenv("HOME")
	t.Setenv("HOME", dir)
	t.Cleanup(func() { os.Setenv("HOME", orig) })
}

func TestInitConfig_CreatesDefaultWhenMissing(t *testing.T) {
	home := t.TempDir()
	overrideHome(t, home)

	cfg, err := src.InitConfig()
	if err != nil {
		t.Fatalf("InitConfig: %v", err)
	}
	if len(cfg.Files) == 0 {
		t.Fatal("expected non-empty config")
	}

	// Confirm the file was actually written.
	confFile := filepath.Join(home, ".config", "dotd", "conf.json")
	if _, err := os.Stat(confFile); err != nil {
		t.Fatalf("config file not created: %v", err)
	}
}

func TestInitConfig_ReloadsExistingFile(t *testing.T) {
	home := t.TempDir()
	overrideHome(t, home)

	// Create a custom config with a single entry.
	confDir := filepath.Join(home, ".config", "dotd")
	if err := os.MkdirAll(confDir, 0o755); err != nil {
		t.Fatal(err)
	}
	custom := src.Config{
		Files: []src.FileConfig{
			{Extension: ".xyz", Location: "custom/xyz"},
		},
	}
	data, _ := json.MarshalIndent(custom, "", "  ")
	if err := os.WriteFile(filepath.Join(confDir, "conf.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := src.InitConfig()
	if err != nil {
		t.Fatalf("InitConfig: %v", err)
	}
	if len(cfg.Files) != 1 || cfg.Files[0].Extension != ".xyz" {
		t.Fatalf("unexpected config: %+v", cfg.Files)
	}
}

// ── Config.normalize (via InitConfig / parseConfig) ──────────────────────────

func writeRawConfig(t *testing.T, home, raw string) {
	t.Helper()
	dir := filepath.Join(home, ".config", "dotd")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "conf.json"), []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestNormalize_AddsMissingLeadingDot(t *testing.T) {
	home := t.TempDir()
	overrideHome(t, home)
	writeRawConfig(t, home, `{"files":[{"extension":"pdf","location":"docs/pdf"}]}`)

	cfg, err := src.InitConfig()
	if err != nil {
		t.Fatalf("InitConfig: %v", err)
	}
	if cfg.Files[0].Extension != ".pdf" {
		t.Errorf("got %q, want .pdf", cfg.Files[0].Extension)
	}
}

func TestNormalize_LowercasesExtension(t *testing.T) {
	home := t.TempDir()
	overrideHome(t, home)
	writeRawConfig(t, home, `{"files":[{"extension":".PDF","location":"docs/pdf"}]}`)

	cfg, err := src.InitConfig()
	if err != nil {
		t.Fatalf("InitConfig: %v", err)
	}
	if cfg.Files[0].Extension != ".pdf" {
		t.Errorf("got %q, want .pdf", cfg.Files[0].Extension)
	}
}

func TestNormalize_RejectsDuplicateExtension(t *testing.T) {
	home := t.TempDir()
	overrideHome(t, home)
	writeRawConfig(t, home, `{"files":[
		{"extension":".pdf","location":"a"},
		{"extension":".pdf","location":"b"}
	]}`)

	if _, err := src.InitConfig(); err == nil {
		t.Fatal("expected error for duplicate extension")
	}
}

func TestNormalize_RejectsAbsoluteLocation(t *testing.T) {
	home := t.TempDir()
	overrideHome(t, home)
	writeRawConfig(t, home, `{"files":[{"extension":".pdf","location":"/absolute/path"}]}`)

	if _, err := src.InitConfig(); err == nil {
		t.Fatal("expected error for absolute location")
	}
}

func TestNormalize_RejectsParentTraversal(t *testing.T) {
	home := t.TempDir()
	overrideHome(t, home)
	writeRawConfig(t, home, `{"files":[{"extension":".pdf","location":"../../etc"}]}`)

	if _, err := src.InitConfig(); err == nil {
		t.Fatal("expected error for parent-traversing location")
	}
}

func TestNormalize_RejectsEmptyExtension(t *testing.T) {
	home := t.TempDir()
	overrideHome(t, home)
	writeRawConfig(t, home, `{"files":[{"extension":"","location":"docs"}]}`)

	if _, err := src.InitConfig(); err == nil {
		t.Fatal("expected error for empty extension")
	}
}

// ── Config.LocationFor ────────────────────────────────────────────────────────

func TestLocationFor_HitAndMiss(t *testing.T) {
	cfg := &src.Config{
		Files: []src.FileConfig{
			{Extension: ".pdf", Location: "docs/pdf"},
			{Extension: ".png", Location: "images/png"},
		},
	}

	loc, ok := cfg.LocationFor(".pdf")
	if !ok || loc != "docs/pdf" {
		t.Errorf("LocationFor(.pdf) = %q, %v", loc, ok)
	}

	// Case-insensitive match.
	loc, ok = cfg.LocationFor(".PDF")
	if !ok || loc != "docs/pdf" {
		t.Errorf("LocationFor(.PDF) = %q, %v", loc, ok)
	}

	_, ok = cfg.LocationFor(".xyz")
	if ok {
		t.Error("LocationFor(.xyz) should return false")
	}
}

// ── CreateFileSystem ──────────────────────────────────────────────────────────

func TestCreateFileSystem_CreatesAllDirs(t *testing.T) {
	root := t.TempDir()
	cfg := &src.Config{
		Files: []src.FileConfig{
			{Extension: ".pdf", Location: "docs/pdf"},
			{Extension: ".png", Location: "images/png"},
		},
	}

	if err := src.CreateFileSystem(root, cfg); err != nil {
		t.Fatalf("CreateFileSystem: %v", err)
	}

	for _, f := range cfg.Files {
		target := filepath.Join(root, f.Location)
		if info, err := os.Stat(target); err != nil || !info.IsDir() {
			t.Errorf("expected directory %s to exist", target)
		}
	}
}

func TestCreateFileSystem_IdempotentOnExistingDirs(t *testing.T) {
	root := t.TempDir()
	cfg := &src.Config{
		Files: []src.FileConfig{
			{Extension: ".pdf", Location: "docs/pdf"},
		},
	}

	// Create once, then again — should not error.
	if err := src.CreateFileSystem(root, cfg); err != nil {
		t.Fatalf("first call: %v", err)
	}
	if err := src.CreateFileSystem(root, cfg); err != nil {
		t.Fatalf("second call: %v", err)
	}
}
