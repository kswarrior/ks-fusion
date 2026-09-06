package tools

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/kswarrior/ks-fusion/internal/config"
)

// ---------------------------------------------------------------------------
// semver resolver + fusion.lock (v2.2)
// ---------------------------------------------------------------------------

func parseVer(s string) ([]int, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, false
	}
	// strip leading v
	s = strings.TrimPrefix(s, "v")
	parts := strings.Split(s, ".")
	out := make([]int, 0, len(parts))
	for _, p := range parts {
		// strip pre-release/build suffix
		if i := strings.IndexAny(p, "-+"); i >= 0 {
			p = p[:i]
		}
		p = strings.TrimSpace(p)
		if p == "" {
			return nil, false
		}
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return nil, false
		}
		out = append(out, n)
	}
	return out, true
}

func cmpVer(a, b []int) int {
	n := len(a)
	if len(b) > n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		av, bv := 0, 0
		if i < len(a) {
			av = a[i]
		}
		if i < len(b) {
			bv = b[i]
		}
		if av != bv {
			if av < bv {
				return -1
			}
			return 1
		}
	}
	return 0
}

// satisfiesSemver reports whether version ver satisfies spec.
// spec forms: "1.0.0" exact, "^1.2.3", "~1.2.3", ">=1.0.0", ">1.0.0", "<=x", "<x", "*", "" (any).
func satisfiesSemver(ver, spec string) bool {
	spec = strings.TrimSpace(strings.Trim(spec, "\"'"))
	ver = strings.TrimSpace(strings.Trim(ver, "\"'"))
	if spec == "" || spec == "*" {
		return true
	}
	if strings.HasPrefix(spec, "path:") {
		return true
	}
	vv, ok := parseVer(ver)
	if !ok {
		return false
	}
	// comma-separated constraints (all must hold), e.g. ">=1.0.0, <2.0.0"
	if strings.Contains(spec, ",") {
		for _, part := range strings.Split(spec, ",") {
			if !satisfiesSemver(ver, strings.TrimSpace(part)) {
				return false
			}
		}
		return true
	}
	switch {
	case strings.HasPrefix(spec, "^"):
		base, ok := parseVer(strings.TrimPrefix(spec, "^"))
		if !ok {
			return false
		}
		if cmpVer(vv, base) < 0 {
			return false
		}
		// ^0.0.x pins patch, ^0.x pins minor, ^x pins major
		if len(base) > 0 && base[0] != 0 {
			return vv[0] == base[0]
		}
		if len(base) > 1 && base[1] != 0 {
			return len(vv) > 1 && vv[0] == 0 && vv[1] == base[1]
		}
		return cmpVer(vv, base) == 0
	case strings.HasPrefix(spec, "~"):
		base, ok := parseVer(strings.TrimPrefix(spec, "~"))
		if !ok {
			return false
		}
		if cmpVer(vv, base) < 0 {
			return false
		}
		if len(base) >= 2 {
			return len(vv) >= 2 && vv[0] == base[0] && vv[1] == base[1]
		}
		return len(vv) >= 1 && vv[0] == base[0]
	case strings.HasPrefix(spec, ">="):
		base, ok := parseVer(strings.TrimPrefix(spec, ">="))
		if !ok {
			return false
		}
		return cmpVer(vv, base) >= 0
	case strings.HasPrefix(spec, "<="):
		base, ok := parseVer(strings.TrimPrefix(spec, "<="))
		if !ok {
			return false
		}
		return cmpVer(vv, base) <= 0
	case strings.HasPrefix(spec, ">"):
		base, ok := parseVer(strings.TrimPrefix(spec, ">"))
		if !ok {
			return false
		}
		return cmpVer(vv, base) > 0
	case strings.HasPrefix(spec, "<"):
		base, ok := parseVer(strings.TrimPrefix(spec, "<"))
		if !ok {
			return false
		}
		return cmpVer(vv, base) < 0
	case strings.HasPrefix(spec, "="):
		base, ok := parseVer(strings.TrimPrefix(spec, "="))
		if !ok {
			return false
		}
		return cmpVer(vv, base) == 0
	default:
		base, ok := parseVer(spec)
		if !ok {
			return false
		}
		return cmpVer(vv, base) == 0
	}
}

func listBundles(name string, dirs []string) []struct {
	Path string
	Ver  string
} {
	seen := map[string]bool{}
	var out []struct {
		Path string
		Ver  string
	}
	base := name[strings.LastIndex(name, "/")+1:]
	for _, dir := range dirs {
		if dir == "" || seen[dir] {
			continue
		}
		seen[dir] = true
		ents, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range ents {
			if e.IsDir() {
				// registry namespaced layout: <dir>/<name>/<ver>.kslib
				// check if subdir matches name (flat or scoped)
				sub := filepath.Join(dir, e.Name())
				if e.Name() == name || e.Name() == base {
					subEnts, err := os.ReadDir(sub)
					if err != nil {
						continue
					}
					for _, se := range subEnts {
						if se.IsDir() || !strings.HasSuffix(se.Name(), ".kslib") {
							continue
						}
						ver := strings.TrimSuffix(se.Name(), ".kslib")
						if _, ok := parseVer(ver); !ok {
							continue
						}
						// skip yanked (check index)
						if isYanked(dir, name, ver) {
							continue
						}
						out = append(out, struct {
							Path string
							Ver  string
						}{Path: filepath.Join(sub, se.Name()), Ver: ver})
					}
				}
				// scoped: <dir>/<scope>/<name>
				// try <dir>/<first-part>/<rest> layout generically
				continue
			}
			fn := e.Name()
			var ver string
			switch {
			case fn == base+".kslib":
				ver = ""
			case strings.HasPrefix(fn, base+"-") && strings.HasSuffix(fn, ".kslib"):
				ver = strings.TrimSuffix(strings.TrimPrefix(fn, base+"-"), ".kslib")
			default:
				continue
			}
			out = append(out, struct {
				Path string
				Ver  string
			}{Path: filepath.Join(dir, fn), Ver: ver})
		}
		// also try scoped registry subdir directly: <dir>/<name>/<ver>.kslib
		safeName := strings.ReplaceAll(name, ":", "/")
		subDir := filepath.Join(dir, filepath.FromSlash(safeName))
		if subDir != dir {
			if subEnts, err := os.ReadDir(subDir); err == nil {
				for _, se := range subEnts {
					if se.IsDir() || !strings.HasSuffix(se.Name(), ".kslib") {
						continue
					}
					ver := strings.TrimSuffix(se.Name(), ".kslib")
					if _, ok := parseVer(ver); !ok {
						continue
					}
					if isYanked(dir, name, ver) {
						continue
					}
					dup := false
					for _, o := range out {
						if o.Path == filepath.Join(subDir, se.Name()) {
							dup = true
							break
						}
					}
					if !dup {
						out = append(out, struct {
							Path string
							Ver  string
						}{Path: filepath.Join(subDir, se.Name()), Ver: ver})
					}
				}
			}
		}
	}
	return out
}

func isYanked(root, name, ver string) bool {
	for _, e := range loadIndex(root) {
		if e.Name == name && e.Version == ver && e.Yanked {
			return true
		}
	}
	return false
}

// ResolveDep finds newest bundle for name satisfying spec.
func ResolveDep(name, spec string, dirs []string) (path, ver string, err error) {
	if strings.HasPrefix(spec, "path:") {
		return spec, spec, nil
	}
	cands := listBundles(name, dirs)
	if len(cands) == 0 {
		return "", "", fmt.Errorf("library %q not found", name)
	}
	// filter by spec (empty ver only matches * / empty spec)
	var filtered []struct {
		Path string
		Ver  string
	}
	for _, c := range cands {
		if c.Ver == "" {
			if spec == "" || spec == "*" {
				filtered = append(filtered, c)
			}
			continue
		}
		if satisfiesSemver(c.Ver, spec) {
			filtered = append(filtered, c)
		}
	}
	if len(filtered) == 0 {
		return "", "", fmt.Errorf("library %q: no version matching %q", name, spec)
	}
	sort.Slice(filtered, func(i, j int) bool {
		vi, oki := parseVer(filtered[i].Ver)
		vj, okj := parseVer(filtered[j].Ver)
		if !oki {
			return false
		}
		if !okj {
			return true
		}
		return cmpVer(vi, vj) > 0
	})
	return filtered[0].Path, filtered[0].Ver, nil
}

// WriteLock writes fusion.lock with resolved versions.
func WriteLock(appDir string, paths, versions map[string]string) error {
	type entry struct {
		Name    string `json:"name"`
		Version string `json:"version"`
		Path    string `json:"path"`
	}
	var list []entry
	for k, p := range paths {
		ver := versions[k]
		if ver == "" {
			ver = p
		}
		list = append(list, entry{Name: k, Version: ver, Path: p})
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })
	data, err := json.MarshalIndent(map[string]any{"version": 1, "packages": list}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(appDir, "fusion.lock"), append(data, '\n'), 0o644)
}

// ResolveAll resolves all deps of cfg, writes fusion.lock, returns path map.
// Searches local dirs + registry (with yank filtering + checksum index).
func ResolveAll(cfg *config.Config) (map[string]string, error) {
	dirs := []string{
		filepath.Join(cfg.Dir, "test-releases"),
		filepath.Join(cfg.Dir, "target"),
		filepath.Join(cfg.Dir, "release"),
		filepath.Join(cfg.Dir, "vendor"),
		"test-releases",
		"target",
		"release",
		"vendor",
	}
	// registry roots: each <root>/<name> dir holds <ver>.kslib files (flat search)
	for _, r := range registryRoots() {
		// add root itself (for flat names) — listBundles handles subdirs? add per-name below
		dirs = append(dirs, r)
		// private registry token hint (P2): require token if FUSION_REGISTRY_PRIVATE=1
		if os.Getenv("FUSION_REGISTRY_PRIVATE") == "1" && os.Getenv("FUSION_REGISTRY_TOKEN") == "" {
			// don't fail build, just note (private auth left to token)
			continue
		}
	}
	resolved := map[string]string{}
	paths := map[string]string{}
	for name, spec := range cfg.Dependencies {
		if strings.HasPrefix(spec, "path:") {
			paths[name] = spec
			resolved[name] = spec
			continue
		}
		p, ver, err := ResolveDep(name, spec, dirs)
		if err != nil {
			return nil, err
		}
		paths[name] = p
		resolved[name] = ver
	}
	if len(cfg.Dependencies) > 0 {
		_ = WriteLock(cfg.Dir, paths, resolved)
	}
	return paths, nil
}

// Vendor copies resolved bundles into vendor/.
func VendorApp(appDir string) error {
	cfg, err := config.Load(appDir)
	if err != nil {
		return err
	}
	paths, err := ResolveAll(cfg)
	if err != nil {
		return err
	}
	vendorDir := filepath.Join(cfg.Dir, "vendor")
	if err := os.MkdirAll(vendorDir, 0o755); err != nil {
		return err
	}
	for name, p := range paths {
		if strings.HasPrefix(p, "path:") {
			continue
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		dst := filepath.Join(vendorDir, filepath.Base(p))
		if err := os.WriteFile(dst, data, 0o644); err != nil {
			return err
		}
		fmt.Printf("vendored %s -> %s\n", name, dst)
	}
	return nil
}

// ---------------------------------------------------------------------------
// build --bin (single static executable via go build embedding)
// ---------------------------------------------------------------------------

func BuildBin(appDir, out, target string) error {
	cfg, err := config.Load(appDir)
	if err != nil {
		return err
	}
	if cfg.IsLib() {
		return fmt.Errorf("%s is a lib; --bin needs an app (backend+frontend)", cfg.Source)
	}
	// collect all .ks under app dir (embed for imports)
	var ksFiles []string
	err = filepath.Walk(cfg.Dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if info.Name() == ".git" || info.Name() == "target" || info.Name() == "test-releases" || info.Name() == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(info.Name(), ".ks") {
			ksFiles = append(ksFiles, path)
		}
		return nil
	})
	if err != nil {
		return err
	}
	if len(ksFiles) == 0 {
		return fmt.Errorf("no .ks files in %s", cfg.Dir)
	}
	// read fusion.toml
	tomlData, err := os.ReadFile(cfg.Source)
	if err != nil {
		return err
	}
	var embeds []binEmbedded
	for _, f := range ksFiles {
		rel, err := filepath.Rel(cfg.Dir, f)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(f)
		if err != nil {
			return err
		}
		embeds = append(embeds, binEmbedded{Rel: filepath.ToSlash(rel), Data: string(data)})
	}
	// embed resolved .kslib deps so --bin runs without external test-releases/
	if len(cfg.Dependencies) > 0 {
		if paths, err := ResolveAll(cfg); err == nil {
			for _, p := range paths {
				if strings.HasPrefix(p, "path:") {
					continue
				}
				data, err := os.ReadFile(p)
				if err != nil {
					continue
				}
				embeds = append(embeds, binEmbedded{Rel: "test-releases/" + filepath.Base(p), Data: string(data)})
			}
		}
	}
	// embed fusion.lock if present
	if lockData, err := os.ReadFile(filepath.Join(cfg.Dir, "fusion.lock")); err == nil {
		embeds = append(embeds, binEmbedded{Rel: "fusion.lock", Data: string(lockData)})
	}
	// reproducible builds (v2.4): deterministic order
	sort.Slice(embeds, func(i, j int) bool { return embeds[i].Rel < embeds[j].Rel })
	// generate main.go inside module (internal/ import rule: must build within module)
	modRoot := findModuleRoot()
	tmpName := fmt.Sprintf("tmp-fusion-bin-%d", time.Now().UnixNano())
	tmpDir := filepath.Join(modRoot, tmpName)
	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)
	gen := buildBinMain(cfg.Name, string(tomlData), embeds, cfg.BackendEntry, cfg.FrontendEntry)
	if err := os.WriteFile(filepath.Join(tmpDir, "main.go"), []byte(gen), 0o644); err != nil {
		return err
	}
	if out == "" {
		out = cfg.Name
		if strings.Contains(target, "windows") {
			out += ".exe"
		}
	}
	absOut, err := filepath.Abs(out)
	if err != nil {
		return err
	}
	cmd := exec.Command("go", "build", "-trimpath", "-o", absOut, "./"+tmpName)
	cmd.Dir = modRoot
	// reproducible (v2.4): -trimpath + CGO off + no VCS stamping
	env := os.Environ()
	env = append(env, "GOFLAGS=-trimpath", "CGO_ENABLED=0")
	if target != "" && target != "host" {
		goos, goarch := parseTarget(target)
		if goos == "" {
			return fmt.Errorf("bad --target %q (want linux/amd64, linux/arm64, darwin/amd64, darwin/arm64, windows/amd64, wasm)", target)
		}
		env = append(env, "GOOS="+goos, "GOARCH="+goarch)
		if goos == "js" {
			env = append(env, "GOOS=js", "GOARCH=wasm")
		}
		// disable cgo for static binaries
		env = append(env, "CGO_ENABLED=0")
	}
	cmd.Env = env
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("go build failed: %w", err)
	}
	fmt.Printf("built bin: %s (target %s)\n", absOut, targetOrHost(target))
	return nil
}

func targetOrHost(t string) string {
	if t == "" {
		return "host"
	}
	return t
}

func parseTarget(t string) (string, string) {
	t = strings.ToLower(strings.TrimSpace(t))
	if t == "wasm" || t == "js/wasm" {
		return "js", "wasm"
	}
	parts := strings.Split(t, "/")
	if len(parts) != 2 {
		return "", ""
	}
	goos, goarch := parts[0], parts[1]
	switch goos {
	case "linux", "darwin", "windows", "js":
	default:
		return "", ""
	}
	switch goarch {
	case "amd64", "arm64", "wasm", "386":
	default:
		return "", ""
	}
	if goos == "js" && goarch != "wasm" {
		return "", ""
	}
	return goos, goarch
}

func findModuleRoot() string {
	// locate go.mod for ks-fusion by walking up from CWD
	dir, _ := os.Getwd()
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			// check module name
			data, _ := os.ReadFile(filepath.Join(dir, "go.mod"))
			if strings.Contains(string(data), "ks-fusion") {
				return dir
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "."
		}
		dir = parent
	}
}

type binEmbedded struct {
	Rel  string
	Data string
}

func buildBinMain(appName, toml string, embeds []binEmbedded, backendEntry, frontendEntry string) string {
	var b strings.Builder
	b.WriteString("package main\n\nimport (\n\t\"fmt\"\n\t\"os\"\n\t\"path/filepath\"\n\n\t\"github.com/kswarrior/ks-fusion/internal/backend\"\n\t\"github.com/kswarrior/ks-fusion/internal/frontend\"\n)\n\n")
	b.WriteString("var embedded = map[string]string{\n")
	for _, e := range embeds {
		b.WriteString(strconv_Quote(e.Rel) + ": " + strconv_Quote(e.Data) + ",\n")
	}
	b.WriteString("}\n\n")
	b.WriteString("var fusionToml = " + strconv_Quote(toml) + "\n\n")
	b.WriteString("func main() {\n")
	b.WriteString("\tdir, err := os.MkdirTemp(\"\", \"" + appName + "-bin-*\")\n")
	b.WriteString("\tif err != nil { fmt.Println(\"error:\", err); os.Exit(1) }\n")
	b.WriteString("\t// no cleanup: keep running while server lives; remove on exit\n")
	b.WriteString("\tdefer os.RemoveAll(dir)\n")
	b.WriteString("\tif err := os.WriteFile(filepath.Join(dir, \"fusion.toml\"), []byte(fusionToml), 0644); err != nil { fmt.Println(\"error:\", err); os.Exit(1) }\n")
	b.WriteString("\tfor rel, data := range embedded {\n\t\tp := filepath.Join(dir, filepath.FromSlash(rel))\n\t\tif err := os.MkdirAll(filepath.Dir(p), 0755); err != nil { fmt.Println(\"error:\", err); os.Exit(1) }\n\t\tif err := os.WriteFile(p, []byte(data), 0644); err != nil { fmt.Println(\"error:\", err); os.Exit(1) }\n\t}\n")
	b.WriteString("\t_ = fusionToml\n")
	b.WriteString("\tfor _, entry := range []string{" + strconv_Quote(backendEntry) + ", " + strconv_Quote(frontendEntry) + "} {\n")
	b.WriteString("\t\tp := filepath.Join(dir, filepath.FromSlash(entry))\n")
	b.WriteString("\t\tprog, err := frontend.ParseFile(p)\n")
	b.WriteString("\t\tif err != nil { fmt.Println(\"error:\", err); os.Exit(1) }\n")
	b.WriteString("\t\tif err := backend.RunWithDir(prog, dir); err != nil { fmt.Println(\"error:\", err); os.Exit(1) }\n")
	b.WriteString("\t}\n}\n")
	return b.String()
}

func strconv_Quote(s string) string {
	// use %q formatting
	return fmt.Sprintf("%q", s)
}
