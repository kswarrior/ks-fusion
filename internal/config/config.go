// Package config reads fusion.toml for apps made in ks-fusion.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Config describes one ks-fusion app.
type Config struct {
	Name          string
	Version       string
	BackendEntry  string
	FrontendEntry string
	Dir           string
}

// Load reads <appDir>/fusion.toml. Uses stdlib only (tiny parser).
func Load(appDir string) (*Config, error) {
	c := &Config{
		Name:          filepath.Base(appDir),
		Version:       "0.1.0",
		BackendEntry:  "backend/main.ks",
		FrontendEntry: "frontend/main.ks",
		Dir:           appDir,
	}
	path := filepath.Join(appDir, "fusion.toml")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("missing fusion.toml in %s: %w", appDir, err)
	}
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "[") {
			continue
		}
		kv := strings.SplitN(line, "=", 2)
		if len(kv) != 2 {
			continue
		}
		key := strings.TrimSpace(kv[0])
		val := strings.Trim(strings.TrimSpace(kv[1]), "\"'")
		switch key {
		case "name":
			c.Name = val
		case "version":
			c.Version = val
		case "entry_backend", "backend":
			c.BackendEntry = val
		case "entry_frontend", "frontend":
			c.FrontendEntry = val
		}
	}
	return c, nil
}

// BackendPath returns absolute backend entry file.
func (c *Config) BackendPath() string {
	return filepath.Join(c.Dir, filepath.FromSlash(c.BackendEntry))
}

// FrontendPath returns absolute frontend entry file.
func (c *Config) FrontendPath() string {
	return filepath.Join(c.Dir, filepath.FromSlash(c.FrontendEntry))
}
