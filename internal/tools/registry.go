package tools

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/kswarrior/ks-fusion/internal/config"
	"github.com/kswarrior/ks-fusion/internal/lib"
)

// Registry (v2.3): file-based central registry with checksums + yank + namespaces.
// Layout:
//   <root>/<scope/name>/<version>.kslib
//   <root>/<scope/name>/<version>.sha256
//   <root>/index.json  [{name,version,sha256,yanked,published}]
// Roots (first writable wins for publish, all searched for pull):
//   $FUSION_REGISTRY, ./registry, $HOME/.fusion/registry

type RegistryEntry struct {
	Name      string `json:"name"`
	Version   string `json:"version"`
	SHA256    string `json:"sha256"`
	Yanked    bool   `json:"yanked"`
	Published string `json:"published"`
	Path      string `json:"path"`
}

func registryRoots() []string {
	var roots []string
	if v := os.Getenv("FUSION_REGISTRY"); v != "" {
		roots = append(roots, v)
	}
	roots = append(roots, "registry")
	if h, err := os.UserHomeDir(); err == nil {
		roots = append(roots, filepath.Join(h, ".fusion", "registry"))
	}
	return roots
}

func registryIndexPath(root string) string { return filepath.Join(root, "index.json") }

func loadIndex(root string) []RegistryEntry {
	data, err := os.ReadFile(registryIndexPath(root))
	if err != nil {
		return nil
	}
	var out []RegistryEntry
	if err := json.Unmarshal(data, &out); err != nil {
		return nil
	}
	return out
}

func saveIndex(root string, entries []RegistryEntry) error {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return err
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Name == entries[j].Name {
			vi, _ := parseVer(entries[i].Version)
			vj, _ := parseVer(entries[j].Version)
			return cmpVer(vi, vj) < 0
		}
		return entries[i].Name < entries[j].Name
	})
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(registryIndexPath(root), append(data, '\n'), 0o644)
}

func sha256File(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

// Publish builds lib at libDir and copies bundle into registry root.
func Publish(libDir, registryRoot string) (string, error) {
	cfg, err := config.Load(libDir)
	if err != nil {
		return "", err
	}
	if !cfg.IsLib() {
		return "", fmt.Errorf("%s is not a lib (type=lib required)", libDir)
	}
	if registryRoot == "" {
		roots := registryRoots()
		registryRoot = roots[0]
		// prefer first writable; try ./registry then home
		for _, r := range roots {
			if err := os.MkdirAll(r, 0o755); err == nil {
				registryRoot = r
				break
			}
		}
	}
	// build to temp out then copy
	tmpOut, err := os.MkdirTemp("", "fusion-pub-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmpOut)
	artifact, err := lib.Build(cfg, tmpOut)
	if err != nil {
		return "", err
	}
	sum, err := sha256File(artifact)
	if err != nil {
		return "", err
	}
	// namespace-aware: scope/name -> subdir scope/name
	safeName := strings.ReplaceAll(cfg.LibName, ":", "/")
	destDir := filepath.Join(registryRoot, filepath.FromSlash(safeName))
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return "", err
	}
	dest := filepath.Join(destDir, cfg.Version+".kslib")
	data, err := os.ReadFile(artifact)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(dest, data, 0o644); err != nil {
		return "", err
	}
	if err := os.WriteFile(dest+".sha256", []byte(sum+"  "+filepath.Base(dest)+"\n"), 0o644); err != nil {
		return "", err
	}
	entries := loadIndex(registryRoot)
	found := false
	for i, e := range entries {
		if e.Name == cfg.LibName && e.Version == cfg.Version {
			entries[i].SHA256 = sum
			entries[i].Yanked = false
			entries[i].Published = time.Now().UTC().Format(time.RFC3339)
			entries[i].Path = filepath.ToSlash(safeName + "/" + cfg.Version + ".kslib")
			found = true
			break
		}
	}
	if !found {
		entries = append(entries, RegistryEntry{
			Name: cfg.LibName, Version: cfg.Version, SHA256: sum,
			Published: time.Now().UTC().Format(time.RFC3339),
			Path:      filepath.ToSlash(safeName + "/" + cfg.Version + ".kslib"),
		})
	}
	if err := saveIndex(registryRoot, entries); err != nil {
		return "", err
	}
	fmt.Printf("published %s v%s -> %s (sha256 %s)\n", cfg.LibName, cfg.Version, dest, sum[:12])
	return dest, nil
}

// Pull fetches name[@spec] from registry into outDir (default test-releases), verifies sha256.
func Pull(name, spec, outDir string) (string, error) {
	if outDir == "" {
		outDir = "test-releases"
	}
	if spec == "" {
		spec = "*"
	}
	// support name@spec syntax
	if i := strings.Index(name, "@"); i >= 0 && spec == "*" {
		spec = name[i+1:]
		name = name[:i]
	}
	for _, root := range registryRoots() {
		entries := loadIndex(root)
		var cands []RegistryEntry
		for _, e := range entries {
			if e.Name != name || e.Yanked {
				continue
			}
			if satisfiesSemver(e.Version, spec) {
				cands = append(cands, e)
			}
		}
		if len(cands) == 0 {
			continue
		}
		sort.Slice(cands, func(i, j int) bool {
			vi, _ := parseVer(cands[i].Version)
			vj, _ := parseVer(cands[j].Version)
			return cmpVer(vi, vj) > 0
		})
		best := cands[0]
		src := filepath.Join(root, filepath.FromSlash(best.Path))
		data, err := os.ReadFile(src)
		if err != nil {
			continue
		}
		sum := sha256.Sum256(data)
		got := hex.EncodeToString(sum[:])
		if best.SHA256 != "" && got != best.SHA256 {
			return "", fmt.Errorf("checksum mismatch for %s v%s (want %s got %s)", name, best.Version, best.SHA256, got)
		}
		// also check sidecar if present
		if err := os.MkdirAll(outDir, 0o755); err != nil {
			return "", err
		}
		dst := filepath.Join(outDir, lib.ArtifactName(name[strings.LastIndex(name, "/")+1:], best.Version))
		// handle scoped names: file is <base>-<ver>.kslib
		if err := os.WriteFile(dst, data, 0o644); err != nil {
			return "", err
		}
		fmt.Printf("pulled %s v%s -> %s (verified sha256 %s)\n", name, best.Version, dst, got[:12])
		return dst, nil
	}
	return "", fmt.Errorf("package %q (%s) not found in registry (roots %s)", name, spec, strings.Join(registryRoots(), ", "))
}

// Yank marks version yanked (or removes if --remove).
func Yank(name, version string, remove bool) error {
	roots := registryRoots()
	for _, root := range roots {
		entries := loadIndex(root)
		changed := false
		var kept []RegistryEntry
		for _, e := range entries {
			if e.Name == name && (version == "" || e.Version == version) {
				if remove {
					// delete files
					_ = os.Remove(filepath.Join(root, filepath.FromSlash(e.Path)))
					_ = os.Remove(filepath.Join(root, filepath.FromSlash(e.Path)) + ".sha256")
					changed = true
					continue
				}
				if !e.Yanked {
					e.Yanked = true
					changed = true
				}
			}
			kept = append(kept, e)
		}
		if changed {
			if remove {
				entries = kept
			} else {
				for i, e := range entries {
					if e.Name == name && (version == "" || e.Version == version) {
						entries[i].Yanked = true
					}
				}
			}
			if err := saveIndex(root, entries); err != nil {
				return err
			}
			fmt.Printf("yanked %s %s in %s\n", name, version, root)
			return nil
		}
	}
	return fmt.Errorf("package %q not found", name)
}

// RegistryList lists all entries across roots.
func RegistryList() []RegistryEntry {
	seen := map[string]bool{}
	var out []RegistryEntry
	for _, root := range registryRoots() {
		for _, e := range loadIndex(root) {
			k := e.Name + "@" + e.Version
			if !seen[k] {
				seen[k] = true
				out = append(out, e)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name == out[j].Name {
			vi, _ := parseVer(out[i].Version)
			vj, _ := parseVer(out[j].Version)
			return cmpVer(vi, vj) < 0
		}
		return out[i].Name < out[j].Name
	})
	return out
}
