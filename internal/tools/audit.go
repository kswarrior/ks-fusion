package tools

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/kswarrior/ks-fusion/internal/config"
	"github.com/kswarrior/ks-fusion/internal/frontend"
	"github.com/kswarrior/ks-fusion/internal/lib"
)

// bareImportRe is a fallback scanner for `import "name"` when a bundled
// source no longer parses (bundles are parse-checked at build, so the
// parser path below is primary).
var bareImportRe = regexp.MustCompile(`(?m)^\s*import\s+"([^"]+)"`)

// scanBareImports returns bare-word library names imported by source:
// `import "name"` without .ks/.kslib/.ksx suffix and without slashes.
// File-relative and bundle-path imports are app-local and skipped.
func scanBareImports(src string) []string {
	if prog, err := frontend.ParseSource(src, "<audit>"); err == nil {
		var out []string
		seen := map[string]bool{}
		for _, st := range prog.Statements {
			if st.Kind != frontend.StmtImport {
				continue
			}
			name := st.StrVal
			if strings.HasSuffix(name, ".ks") || strings.HasSuffix(name, lib.Ext) || strings.HasSuffix(name, lib.SecureExt) {
				continue
			}
			if strings.ContainsAny(name, "/\\") || strings.Contains(name, ".") {
				continue
			}
			if name == "" || seen[name] {
				continue
			}
			seen[name] = true
			out = append(out, name)
		}
		return out
	}
	var out []string
	seen := map[string]bool{}
	for _, m := range bareImportRe.FindAllStringSubmatch(src, -1) {
		name := m[1]
		if strings.HasSuffix(name, ".ks") || strings.HasSuffix(name, lib.Ext) || strings.HasSuffix(name, lib.SecureExt) {
			continue
		}
		if strings.ContainsAny(name, "/\\") || strings.Contains(name, ".") {
			continue
		}
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	return out
}

// Audit (v2.5): check fusion.lock vs registry + local bundles.
// Reports yanked, missing bundles, available updates, registry integrity
// (index/sha256-sidecar vs recomputed file hash) and transitive closure
// (bare-word `import "lib"` inside locked bundles must themselves resolve).

type lockFile struct {
	Packages []struct {
		Name    string `json:"name"`
		Version string `json:"version"`
		Path    string `json:"path"`
	} `json:"packages"`
}

func Audit(appDir string) (issues []string, err error) {
	cfg, err := config.Load(appDir)
	if err != nil {
		return nil, err
	}
	lockData, err := os.ReadFile(filepath.Join(cfg.Dir, "fusion.lock"))
	if err != nil {
		return []string{"missing fusion.lock (run fusion build)"}, nil
	}
	var lf lockFile
	if err := json.Unmarshal(lockData, &lf); err != nil {
		return nil, fmt.Errorf("bad fusion.lock: %w", err)
	}
	for _, p := range lf.Packages {
		// yanked?
		yanked := false
		var latest string
		for _, root := range registryRoots() {
			for _, e := range loadIndex(root) {
				if e.Name != p.Name {
					continue
				}
				if e.Version == p.Version && e.Yanked {
					yanked = true
				}
				if !e.Yanked {
					if latest == "" {
						latest = e.Version
					} else {
						vi, oki := parseVer(latest)
						vj, okj := parseVer(e.Version)
						if oki && okj && cmpVer(vj, vi) > 0 {
							latest = e.Version
						}
					}
				}
			}
		}
		if yanked {
			issues = append(issues, fmt.Sprintf("%s@%s yanked in registry", p.Name, p.Version))
		}
		// checksum present?
		if p.Path != "" && !isPathDep(p.Path) {
			if _, err := os.Stat(p.Path); err != nil {
				// try resolve via registry/local
				issues = append(issues, fmt.Sprintf("%s@%s bundle missing at %s", p.Name, p.Version, p.Path))
			}
		}
		// update available?
		if latest != "" && latest != p.Version {
			vi, oki := parseVer(p.Version)
			vj, okj := parseVer(latest)
			if oki && okj && cmpVer(vj, vi) > 0 {
				issues = append(issues, fmt.Sprintf("%s update available: %s -> %s", p.Name, p.Version, latest))
			}
		}
		_ = latest
	}
	// private token hint
	if os.Getenv("FUSION_REGISTRY_PRIVATE") == "1" && os.Getenv("FUSION_REGISTRY_TOKEN") == "" {
		issues = append(issues, "private registry without FUSION_REGISTRY_TOKEN")
	}
	// registry integrity: recompute every indexed bundle hash
	issues = append(issues, VerifyRegistry()...)
	// transitive closure over locked bundles
	issues = append(issues, checkTransitive(cfg, lf)...)
	return issues, nil
}

// VerifyRegistry recomputes the sha256 of every indexed bundle file and
// compares it against the index entry and its .sha256 sidecar.
// It returns one issue string per mismatch (empty when healthy).
func VerifyRegistry() []string {
	var issues []string
	for _, root := range registryRoots() {
		for _, e := range loadIndex(root) {
			fp := filepath.Join(root, filepath.FromSlash(e.Path))
			data, err := os.ReadFile(fp)
			if err != nil {
				issues = append(issues, fmt.Sprintf("registry %s@%s bundle unreadable at %s: %v", e.Name, e.Version, e.Path, err))
				continue
			}
			sum := sha256.Sum256(data)
			got := hex.EncodeToString(sum[:])
			if e.SHA256 != "" && got != e.SHA256 {
				issues = append(issues, fmt.Sprintf("registry %s@%s checksum mismatch: index %s recomputed %s", e.Name, e.Version, e.SHA256, got))
			}
			if side, err := os.ReadFile(fp + ".sha256"); err == nil {
				if !strings.Contains(string(side), got) {
					issues = append(issues, fmt.Sprintf("registry %s@%s sidecar mismatch: recomputed %s", e.Name, e.Version, got))
				}
			}
		}
	}
	return issues
}

// checkTransitive loads every locked bundle and verifies that the bare-word
// library imports inside its sources also resolve (lock, registry, or local
// bundle dirs). File-relative imports (`*.ks`, paths) are app-local and skipped.
func checkTransitive(cfg *config.Config, lf lockFile) []string {
	var issues []string
	locked := map[string]bool{}
	for _, p := range lf.Packages {
		locked[p.Name] = true
	}
	seenBundle := map[string]bool{}
	queue := []string{}
	for _, p := range lf.Packages {
		if p.Path != "" && !isPathDep(p.Path) {
			queue = append(queue, p.Path)
		}
	}
	// also seed registry paths for locked name@version
	for len(queue) > 0 {
		bp := queue[0]
		queue = queue[1:]
		abs := bp
		if !filepath.IsAbs(abs) {
			abs = filepath.Join(cfg.Dir, filepath.FromSlash(bp))
		}
		if seenBundle[abs] {
			continue
		}
		seenBundle[abs] = true
		b, err := lib.Load(abs)
		if err != nil {
			continue // missing-bundle issue already reported above
		}
		for _, dep := range bundleLibImports(b) {
			if locked[dep] {
				continue
			}
			// try resolve like the resolver would
			if _, _, err := ResolveDep(dep, "*", resolveSearchDirs(cfg)); err != nil {
				issues = append(issues, fmt.Sprintf("transitive dep %q (imported by %s) not locked and not resolvable: %v", dep, b.Name, err))
			} else {
				issues = append(issues, fmt.Sprintf("transitive dep %q (imported by %s) resolves but is missing from fusion.lock", dep, b.Name))
			}
			locked[dep] = true
		}
	}
	return issues
}

// bundleLibImports returns bare-word library names imported by a bundle's
// sources (`import "name"` without .ks/.kslib/.ksx suffix, no slashes).
func bundleLibImports(b *lib.Bundle) []string {
	var out []string
	seen := map[string]bool{}
	for _, f := range b.Files {
		for _, name := range scanBareImports(f.Source) {
			if !seen[name] {
				seen[name] = true
				out = append(out, name)
			}
		}
	}
	// a bundle importing itself is fine (single-file libs list their own name in examples)
	out2 := out[:0]
	for _, n := range out {
		if n != b.Name {
			out2 = append(out2, n)
		}
	}
	return out2
}

func isPathDep(p string) bool { return len(p) > 5 && p[:5] == "path:" }
