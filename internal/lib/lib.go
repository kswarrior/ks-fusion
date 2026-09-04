// Package lib builds and loads ks-fusion library bundles (.kslib),
// the ks-fusion answer to Rust's .rlib files.
//
// A library package (fusion.toml with type = "lib") is a directory of
// .ks sources. `Build` parse-checks every .ks file and packs them into
// one versioned JSON bundle:
//
//	test-releases/<name>-<version>.kslib   (fusion build --release)
//	target/<name>-<version>.kslib          (fusion build, debug)
//
// Apps pull a library in with `import "name"`; the backend finds the
// newest matching bundle in the search dirs and executes its sources.
package lib

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/kswarrior/ks-fusion/internal/config"
	"github.com/kswarrior/ks-fusion/internal/frontend"
)

// Format is the bundle format tag written into every .kslib file.
const Format = "kslib-1"

// Ext is the library bundle file extension.
const Ext = ".kslib"

// File is one source file inside a bundle. Path is slash-separated and
// relative to the package dir (e.g. "src/lib.ks").
type File struct {
	Path   string `json:"path"`
	Source string `json:"source"`
}

// Bundle is the on-disk .kslib format.
type Bundle struct {
	Format  string `json:"format"`
	Name    string `json:"name"`
	Version string `json:"version"`
	Files   []File `json:"files"`
}

// ArtifactName returns "<name>-<version>.kslib".
func ArtifactName(name, version string) string {
	return fmt.Sprintf("%s-%s%s", name, version, Ext)
}

// Collect walks the lib package dir and returns every .ks source,
// sorted by path for deterministic bundles.
func Collect(cfg *config.Config) ([]File, error) {
	var files []File
	err := filepath.Walk(cfg.Dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if !strings.HasSuffix(info.Name(), ".ks") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(cfg.Dir, path)
		if err != nil {
			return err
		}
		files = append(files, File{Path: filepath.ToSlash(rel), Source: string(data)})
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("library %q has no .ks sources in %s", cfg.LibName, cfg.Dir)
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files, nil
}

// Check parse-checks every collected file. It returns the files so the
// caller can bundle them without re-reading.
func Check(cfg *config.Config) ([]File, error) {
	files, err := Collect(cfg)
	if err != nil {
		return nil, err
	}
	for _, f := range files {
		full := filepath.Join(cfg.Dir, filepath.FromSlash(f.Path))
		if _, err := frontend.ParseFile(full); err != nil {
			return nil, fmt.Errorf("build failed: %w", err)
		}
	}
	return files, nil
}

// Build parse-checks the lib package and writes the versioned bundle
// into outDir, returning the bundle file path.
func Build(cfg *config.Config, outDir string) (string, error) {
	if !cfg.IsLib() {
		return "", fmt.Errorf("%s is not a library (set type = \"lib\" in fusion.toml)", cfg.Dir)
	}
	files, err := Check(cfg)
	if err != nil {
		return "", err
	}
	b := &Bundle{Format: Format, Name: cfg.LibName, Version: cfg.Version, Files: files}
	data, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return "", err
	}
	out := filepath.Join(outDir, ArtifactName(b.Name, b.Version))
	if err := os.WriteFile(out, append(data, '\n'), 0o644); err != nil {
		return "", err
	}
	return out, nil
}

// Load reads and validates a .kslib bundle file.
func Load(path string) (*Bundle, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var b Bundle
	if err := json.Unmarshal(data, &b); err != nil {
		return nil, fmt.Errorf("bad library bundle %s: %w", path, err)
	}
	if b.Format != Format {
		return nil, fmt.Errorf("bad library bundle %s: unknown format %q", path, b.Format)
	}
	if b.Name == "" || len(b.Files) == 0 {
		return nil, fmt.Errorf("bad library bundle %s: empty name or sources", path)
	}
	return &b, nil
}

// Find locates the newest bundle for a library name across search dirs.
// It matches "<name>.kslib" and "<name>-<version>.kslib".
func Find(name string, searchDirs []string) (string, error) {
	var best string
	var bestVer []int
	found := false
	seen := map[string]bool{}
	for _, dir := range searchDirs {
		if dir == "" || seen[dir] {
			continue
		}
		seen[dir] = true
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			fn := e.Name()
			var ver string
			switch {
			case fn == name+Ext:
				ver = ""
			case strings.HasPrefix(fn, name+"-") && strings.HasSuffix(fn, Ext):
				ver = strings.TrimSuffix(strings.TrimPrefix(fn, name+"-"), Ext)
			default:
				continue
			}
			v := parseVersion(ver)
			if !found || compareVersion(v, bestVer) > 0 {
				found = true
				best = filepath.Join(dir, fn)
				bestVer = v
			}
		}
	}
	if !found {
		return "", fmt.Errorf("library %q not found (looked in %s)", name, strings.Join(searchDirs, ", "))
	}
	return best, nil
}

func parseVersion(s string) []int {
	if s == "" {
		return nil
	}
	var out []int
	for _, part := range strings.Split(s, ".") {
		n, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil {
			return nil // non-numeric sorts below any numeric version
		}
		out = append(out, n)
	}
	return out
}

// compareVersion: nil (unversioned "name.kslib") sorts below everything.
func compareVersion(a, b []int) int {
	if len(a) == 0 && len(b) == 0 {
		return 0
	}
	if len(a) == 0 {
		return -1
	}
	if len(b) == 0 {
		return 1
	}
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] != b[i] {
			if a[i] < b[i] {
				return -1
			}
			return 1
		}
	}
	switch {
	case len(a) < len(b):
		return -1
	case len(a) > len(b):
		return 1
	}
	return 0
}
