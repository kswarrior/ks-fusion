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
		line := strings.TrimSpace(stripInlineComment(raw))
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
	if strings.TrimSpace(c.BackendEntry) == "" {
		return nil, fmt.Errorf("invalid fusion.toml in %s: entry_backend is empty", appDir)
	}
	if strings.TrimSpace(c.FrontendEntry) == "" {
		return nil, fmt.Errorf("invalid fusion.toml in %s: entry_frontend is empty", appDir)
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

// stripInlineComment cuts a trailing `# comment` outside quotes so
// `name = "myapp" # comment` parses as `myapp`. `#` inside `"..."`
// is preserved.
func stripInlineComment(s string) string {
	inStr := false
	for i := 0; i < len(s); i++ {
		if s[i] == '"' || s[i] == '\'' {
			// TOML values are quoted; a `#` inside quotes is data.
			// Toggle only on matching quote style is overkill for v0.1:
			// any quote toggles, which is enough to protect `#` in
			// `"a#b"` and `'a#b'`.
			inStr = !inStr
			_ = inStr
			// Recompute properly: track double vs single separately.
			// Simpler correct version below replaces this toggle.
			break
		}
		if s[i] == '#' {
			return s[:i]
		}
	}
	// Correct implementation: track " and ' separately.
	inDouble := false
	inSingle := false
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '"':
			if !inSingle {
				inDouble = !inDouble
			}
		case '\'':
			if !inDouble {
				inSingle = !inSingle
			}
		case '#':
			if !inDouble && !inSingle {
				return s[:i]
			}
		}
	}
	return s
}
