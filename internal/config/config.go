// Package config reads fusion.toml for apps made in ks-fusion.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Config describes one ks-fusion package (app or lib, like cargo).
type Config struct {
	Name          string
	Version       string
	Type          string // "app" (default) or "lib" (like `cargo new --lib`)
	BackendEntry  string
	FrontendEntry string
	LibEntry      string            // entry .ks file for libs (default "src/lib.ks")
	LibName       string            // override published lib name (default = Name)
	Dependencies  map[string]string // name -> version ("1.0.0") or path dep ("path:../libs/x")
	Dir           string
}

// IsLib reports whether this package is a library.
func (c *Config) IsLib() bool { return c.Type == "lib" }

// LibDir returns the directory holding the lib's .ks sources.
func (c *Config) LibDir() string {
	return filepath.Join(c.Dir, filepath.FromSlash(filepath.Dir(c.LibEntry)))
}

// Load reads <appDir>/fusion.toml. Uses stdlib only (tiny parser).
func Load(appDir string) (*Config, error) {
	c := &Config{
		Name:          filepath.Base(appDir),
		Version:       "0.1.0",
		Type:          "app",
		BackendEntry:  "backend/main.ks",
		FrontendEntry: "frontend/main.ks",
		LibEntry:      "src/lib.ks",
		Dir:           appDir,
		Dependencies:  map[string]string{},
	}
	c.LibName = c.Name
	path := filepath.Join(appDir, "fusion.toml")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("missing fusion.toml in %s: %w", appDir, err)
	}
	section := "package"
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(stripInlineComment(raw))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.TrimSpace(line[1 : len(line)-1])
			continue
		}
		kv := strings.SplitN(line, "=", 2)
		if len(kv) != 2 {
			continue
		}
		key := strings.TrimSpace(kv[0])
		val := strings.Trim(strings.TrimSpace(kv[1]), "\"'")
		switch section {
		case "package":
			switch key {
			case "name":
				c.Name = val
				if c.LibName == "" || c.LibName == filepath.Base(appDir) {
					c.LibName = val
				}
			case "version":
				c.Version = val
			case "type":
				t := strings.ToLower(val)
				if t != "app" && t != "lib" {
					return nil, fmt.Errorf("invalid fusion.toml in %s: type must be \"app\" or \"lib\", got %q", appDir, val)
				}
				c.Type = t
			case "entry_backend", "backend":
				c.BackendEntry = val
			case "entry_frontend", "frontend":
				c.FrontendEntry = val
			}
		case "lib":
			switch key {
			case "name":
				c.LibName = val
			case "path":
				c.LibEntry = val
			}
		case "dependencies":
			// name = "1.0.0"  (versioned, resolved from test-releases/)
			// name = { path = "../libs/mylib" }  (path dep, stored as "path:...")
			c.Dependencies[key] = parseDepValue(strings.TrimSpace(kv[1]))
		}
	}
	if c.LibName == "" {
		c.LibName = c.Name
	}
	if strings.TrimSpace(c.BackendEntry) == "" {
		return nil, fmt.Errorf("invalid fusion.toml in %s: entry_backend is empty", appDir)
	}
	if strings.TrimSpace(c.FrontendEntry) == "" {
		return nil, fmt.Errorf("invalid fusion.toml in %s: entry_frontend is empty", appDir)
	}
	return c, nil
}

// parseDepValue parses a [dependencies] value:
// `"1.0.0"` stays a version string, `{ path = "../x" }` becomes `"path:../x"`.
func parseDepValue(v string) string {
	v = strings.TrimSpace(v)
	if strings.HasPrefix(v, "{") {
		inner := strings.Trim(strings.TrimSuffix(strings.TrimPrefix(v, "{"), "}"), " ")
		for _, part := range strings.Split(inner, ",") {
			kv := strings.SplitN(part, "=", 2)
			if len(kv) != 2 {
				continue
			}
			if strings.TrimSpace(kv[0]) == "path" {
				return "path:" + strings.Trim(strings.TrimSpace(kv[1]), "\"'")
			}
		}
		return v
	}
	return strings.Trim(v, "\"'")
}

// LibPath returns absolute lib entry file.
func (c *Config) LibPath() string {
	return filepath.Join(c.Dir, filepath.FromSlash(c.LibEntry))
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
// or `'...'` is preserved.
func stripInlineComment(s string) string {
	// Track " and ' separately.
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
