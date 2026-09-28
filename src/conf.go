package src

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const (
	appDirName     = "dotd"
	configFileName = "conf.json"
)

// FileConfig maps a file extension to the folder (relative to the root)
// where files with that extension should be placed.
type FileConfig struct {
	Extension string `json:"extension"`
	Location  string `json:"location"`
}

// Config is the top-level structure of conf.json.
type Config struct {
	Files []FileConfig `json:"files"`
}

// DefaultConfig returns the built-in configuration.
func DefaultConfig() *Config {
	return &Config{
		Files: []FileConfig{
			{Extension: ".pdf", Location: "docs/pdf"},
			{Extension: ".png", Location: "images/png"},
			{Extension: ".jpg", Location: "images/jpg"},
			{Extension: ".txt", Location: "docs/text"},
			{Extension: ".md", Location: "docs/markdown"},
			{Extension: ".xlsx", Location: "docs/excel"},
			{Extension: ".pptx", Location: "docs/presentations"},
			{Extension: ".html", Location: "web/html"},
			{Extension: ".js", Location: "code/javascript"},
			{Extension: ".py", Location: "code/python"},
			{Extension: ".zip", Location: "archives/zip"},
			{Extension: ".tar", Location: "archives/tar"},
		},
	}
}

// configPath returns ~/.config/dotd/conf.json, expanded properly.
// (os.ReadFile does not expand "~", so we resolve the home dir ourselves.)
func configPath() (dir string, file string, err error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", "", fmt.Errorf("finding home directory: %w", err)
	}
	dir = filepath.Join(home, ".config", appDirName)
	return dir, filepath.Join(dir, configFileName), nil
}

// writeConfig serialises cfg as indented JSON to path.
func writeConfig(path string, cfg *Config) error {
	out, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("serialising config: %w", err)
	}
	if err := os.WriteFile(path, append(out, '\n'), 0o644); err != nil {
		return fmt.Errorf("writing config: %w", err)
	}
	return nil
}

// parseConfig deserialises and validates raw JSON bytes.
func parseConfig(data []byte) (*Config, error) {
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}
	if err := cfg.normalize(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// normalize lowercases extensions, ensures the leading dot, and rejects
// empty, duplicate, absolute or parent-escaping locations.
func (c *Config) normalize() error {
	seen := make(map[string]bool, len(c.Files))
	for i := range c.Files {
		f := &c.Files[i]

		ext := strings.ToLower(strings.TrimSpace(f.Extension))
		if ext == "" || ext == "." {
			return fmt.Errorf("entry %d: empty extension", i)
		}
		if !strings.HasPrefix(ext, ".") {
			ext = "." + ext
		}
		if seen[ext] {
			return fmt.Errorf("duplicate extension %q", ext)
		}
		seen[ext] = true
		f.Extension = ext

		loc := filepath.Clean(strings.TrimSpace(f.Location))
		if loc == "." || filepath.IsAbs(loc) || loc == ".." ||
			strings.HasPrefix(loc, ".."+string(filepath.Separator)) {
			return fmt.Errorf("extension %q: invalid location %q (must be a relative path inside the root)", ext, f.Location)
		}
		f.Location = loc
	}
	return nil
}

// LocationFor returns the destination folder for a file extension.
func (c *Config) LocationFor(ext string) (string, bool) {
	ext = strings.ToLower(ext)
	for _, f := range c.Files {
		if f.Extension == ext {
			return f.Location, true
		}
	}
	return "", false
}

// InitConfig loads ~/.config/dotd/conf.json. If the directory or file is
// missing, they are created with the default configuration.
func InitConfig() (*Config, error) {
	dir, file, err := configPath()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(file)
	if err == nil {
		return parseConfig(data)
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("reading %s: %w", file, err)
	}

	// Not found: create directory + default config.
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("creating %s: %w", dir, err)
	}
	cfg := DefaultConfig()
	if err := writeConfig(file, cfg); err != nil {
		return nil, err
	}
	fmt.Println("Created default config at", file)
	return cfg, nil
}

// CreateFileSystem creates every destination folder from the config under
// root. Existing folders are left untouched.
func CreateFileSystem(root string, cfg *Config) error {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return fmt.Errorf("creating root %s: %w", root, err)
	}
	for _, f := range cfg.Files {
		target := filepath.Join(root, f.Location)
		if err := os.MkdirAll(target, 0o755); err != nil {
			return fmt.Errorf("creating %s: %w", target, err)
		}
	}
	return nil
}
