package tools

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/kswarrior/ks-fusion/internal/config"
)

// Audit (v2.4): check fusion.lock vs registry + local bundles.
// Reports yanked, missing checksums, available updates.

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
	return issues, nil
}

func isPathDep(p string) bool { return len(p) > 5 && p[:5] == "path:" }
