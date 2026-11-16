package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// Config is the persisted upstreamcheck configuration.
type Config struct {
	Vendor   VendorConfig   `toml:"vendor"`
	Manifest ManifestConfig `toml:"manifest"`
	Leaks    LeaksConfig    `toml:"leaks"`
}

type VendorConfig struct {
	Paths         []string `toml:"paths"`
	BaseRef       string   `toml:"base_ref"`
	TargetRef     string   `toml:"target_ref"`
	BootstrapBase string   `toml:"bootstrap_base"`
	UpstreamDir   string   `toml:"upstream_dir"`
}

type ManifestConfig struct {
	Dir        string `toml:"dir"`
	Features   string `toml:"features"`
	Divergence string `toml:"divergence"`
	OutOfScope string `toml:"out_of_scope"`
	Review     string `toml:"review"`
}

type LeaksConfig struct {
	Security string `toml:"security"`
}

// Load reads a configuration file. Relative paths in the file are resolved
// relative to the directory containing the file.
func Load(path string) (*Config, string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, "", err
	}

	var cfg Config
	if _, err := toml.DecodeFile(abs, &cfg); err != nil {
		return nil, "", fmt.Errorf("read config %s: %w", abs, err)
	}

	dir := filepath.Dir(abs)
	if cfg.Manifest.Dir == "" {
		cfg.Manifest.Dir = "manifest"
	}
	if !filepath.IsAbs(cfg.Manifest.Dir) {
		cfg.Manifest.Dir = filepath.Join(dir, cfg.Manifest.Dir)
	}
	if cfg.Manifest.Features == "" {
		cfg.Manifest.Features = "features.tsv"
	}
	if cfg.Manifest.Divergence == "" {
		cfg.Manifest.Divergence = "divergence.tsv"
	}
	if cfg.Manifest.OutOfScope == "" {
		cfg.Manifest.OutOfScope = "out-of-scope.tsv"
	}
	if cfg.Manifest.Review == "" {
		cfg.Manifest.Review = "needs-review.tsv"
	}
	if cfg.Leaks.Security == "" {
		cfg.Leaks.Security = "../../SECURITY.md"
	}
	if !filepath.IsAbs(cfg.Leaks.Security) {
		cfg.Leaks.Security = filepath.Join(dir, cfg.Leaks.Security)
	}
	if cfg.Vendor.BaseRef == "" {
		cfg.Vendor.BaseRef = "vendor-base/2.34.1"
	}
	if cfg.Vendor.TargetRef == "" {
		cfg.Vendor.TargetRef = "vendor/2.54.2"
	}
	if len(cfg.Vendor.Paths) == 0 {
		cfg.Vendor.Paths = DefaultVendorPaths()
	}

	return &cfg, dir, nil
}

// DefaultVendorPaths returns the build closure that is imported from upstream.
func DefaultVendorPaths() []string {
	return []string{
		"CMakeLists.txt",
		"Source",
		"Tools/CMakeLists.txt",
		"Tools/PlatformGTK.cmake",
		"Tools/MiniBrowser/gtk",
		"Tools/glib",
	}
}

// DefaultDownstreamPaths are downstream files that are not part of vendor
// snapshots but are still worth tracking in the divergence manifest.
func DefaultDownstreamPaths() []string {
	return []string{
		"build.sh",
		"clean.sh",
		"search.sh",
		".gitignore",
	}
}

// ManifestPath resolves a manifest file name inside the configured directory.
func (c *Config) ManifestPath(name string) string {
	if filepath.IsAbs(name) {
		return name
	}
	return filepath.Join(c.Manifest.Dir, name)
}

// InVendorScope reports whether path is part of the imported upstream closure.
func (c *Config) InVendorScope(path string) bool {
	path = filepath.ToSlash(path)
	for _, prefix := range c.Vendor.Paths {
		prefix = strings.TrimSuffix(filepath.ToSlash(prefix), "/")
		if path == prefix || strings.HasPrefix(path, prefix+"/") {
			return true
		}
	}
	return false
}

// InScope reports whether divergence for path should be enforced by lint.
func (c *Config) InScope(path string) bool {
	if c.InVendorScope(path) {
		return true
	}
	for _, p := range DefaultDownstreamPaths() {
		if filepath.ToSlash(path) == p {
			return true
		}
	}
	return false
}

// ScopeFor returns a stable scope label for a path.
func (c *Config) ScopeFor(path string) string {
	path = filepath.ToSlash(path)
	switch {
	case c.InVendorScope(path):
		if strings.HasPrefix(path, "Source/") {
			return "source"
		}
		if strings.HasPrefix(path, "Tools/") {
			return "tools"
		}
		return "vendor-root"
	case path == "build.sh" || path == "clean.sh" || path == "search.sh" || path == ".gitignore":
		return "downstream"
	case strings.HasPrefix(path, "Documentation/"):
		return "docs"
	case strings.HasPrefix(path, "Tools/"):
		return "tools-out-of-scope"
	default:
		return "downstream"
	}
}

// FindRepoRoot locates the repository root from the current directory.
func FindRepoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("not inside a git repository")
		}
		dir = parent
	}
}
