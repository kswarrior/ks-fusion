package tools

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Build cache (v2.3, vendor-aware v2.6): hash .ks + fusion.toml, skip redundant parse-check/repack.
// v2.6 fix: vendor/ swaps now bust the cache (v2.5 wrongly skipped vendor/,
// so swapping a vendored .kslib never invalidated). We hash every file under
// vendor/ (any extension) plus the main .ks set.

type buildCache struct {
	Hash string `json:"hash"`
	When string `json:"when"`
}

func cachePath(appDir string) string {
	return filepath.Join(appDir, "target", ".fusion-cache.json")
}

func hashDir(appDir string) (string, error) {
	h := sha256.New()
	// fusion.toml
	if data, err := os.ReadFile(filepath.Join(appDir, "fusion.toml")); err == nil {
		h.Write(data)
	}
	// fusion.lock
	if data, err := os.ReadFile(filepath.Join(appDir, "fusion.lock")); err == nil {
		h.Write(data)
	}
	err := filepath.Walk(appDir, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			if info.Name() == ".git" || info.Name() == "target" || info.Name() == "vendor" || info.Name() == "test-releases" {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(info.Name(), ".ks") {
			data, err := os.ReadFile(p)
			if err != nil {
				return nil
			}
			rel, _ := filepath.Rel(appDir, p)
			h.Write([]byte(rel))
			h.Write([]byte{0})
			h.Write(data)
			h.Write([]byte{0})
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	// vendor/ (v2.6): hash every vendored file so swaps bust the cache.
	// vendor/ holds .kslib/.ksx bundles (not .ks), hence the separate walk.
	vendorDir := filepath.Join(appDir, "vendor")
	if fi, err := os.Stat(vendorDir); err == nil && fi.IsDir() {
		_ = filepath.Walk(vendorDir, func(p string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return nil
			}
			data, err := os.ReadFile(p)
			if err != nil {
				return nil
			}
			rel, _ := filepath.Rel(appDir, p)
			h.Write([]byte("vendor:"))
			h.Write([]byte(rel))
			h.Write([]byte{0})
			h.Write(data)
			h.Write([]byte{0})
			return nil
		})
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// CheckCache returns true if cache hit (hash matches).
func CheckCache(appDir string) (bool, string, error) {
	want, err := hashDir(appDir)
	if err != nil {
		return false, "", err
	}
	data, err := os.ReadFile(cachePath(appDir))
	if err != nil {
		return false, want, nil
	}
	var c buildCache
	if err := json.Unmarshal(data, &c); err != nil {
		return false, want, nil
	}
	return c.Hash == want, want, nil
}

// WriteCache stores current hash.
func WriteCache(appDir string) error {
	h, err := hashDir(appDir)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(appDir, "target"), 0o755); err != nil {
		return err
	}
	data, _ := json.Marshal(buildCache{Hash: h, When: time.Now().UTC().Format("2006-01-02T15:04:05Z")})
	return os.WriteFile(cachePath(appDir), append(data, '\n'), 0o644)
}
