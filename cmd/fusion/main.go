// Command fusion is the ks-fusion tool (in Go).
// Usage:
// fusion new [--lib] <dir>         scaffold app (default) or library (--lib)
// fusion run [appdir]              run the app (.ks files)
// fusion build [dir] [--release] [--out DIR]
//
//	app: parse-check entries + verify dependencies
//	lib: parse-check sources and pack test-releases/<name>-<ver>.kslib
//	     (--release) or target/<name>-<ver>.kslib (debug, like cargo)
//
// fusion compile <file.ks> [--out file.ksb] [--dis] [--run]
// compile the .ks subset to bytecode (.ksb-1); run with `fusion file.ksb`
//
// fusion help
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/kswarrior/ks-fusion/internal/backend"
	"github.com/kswarrior/ks-fusion/internal/compiler"
	"github.com/kswarrior/ks-fusion/internal/config"
	"github.com/kswarrior/ks-fusion/internal/frontend"
	"github.com/kswarrior/ks-fusion/internal/lib"
	"github.com/kswarrior/ks-fusion/internal/tools"
)

func main() {
	if len(os.Args) < 2 {
		help()
		return
	}
	// Direct file mode: `fusion prog.ks`, `fusion lib.kslib` or secure `fusion lib.ksx`
	// (also what the `#!/usr/bin/env fusion` shebang invokes).
	// Bytecode mode: `fusion prog.ksb` runs a `fusion compile` bundle.
	if a := os.Args[1]; !strings.HasPrefix(a, "-") && strings.HasSuffix(a, compiler.Ext) {
		if err := compiler.RunFile(a); err != nil {
			fmt.Println("error:", err)
			os.Exit(1)
		}
		return
	}
	if a := os.Args[1]; !strings.HasPrefix(a, "-") &&
		(strings.HasSuffix(a, ".ks") || strings.HasSuffix(a, lib.Ext) || strings.HasSuffix(a, lib.SecureExt)) {
		if err := backend.RunFile(a); err != nil {
			fmt.Println("error:", err)
			os.Exit(1)
		}
		return
	}
	switch os.Args[1] {
	case "new":
		isLib := false
		var dirs []string
		for _, a := range os.Args[2:] {
			if a == "--lib" {
				isLib = true
			} else {
				dirs = append(dirs, a)
			}
		}
		if len(dirs) < 1 {
			fmt.Println("usage: fusion new [--lib] <dir>")
			os.Exit(1)
		}
		var err error
		if isLib {
			err = cmdNewLib(dirs[0])
		} else {
			err = cmdNew(dirs[0])
		}
		if err != nil {
			fmt.Println("error:", err)
			os.Exit(1)
		}
	case "run":
		dir := "."
		race := false
		cpuprofile := ""
		debug := false
		args := os.Args[2:]
		for i := 0; i < len(args); i++ {
			a := args[i]
			switch {
			case a == "--race":
				race = true
			case a == "--debug":
				debug = true
			case a == "--cpuprofile":
				if i+1 >= len(args) {
					fmt.Println("usage: fusion run [appdir] [--race] [--debug] [--cpuprofile FILE]")
					os.Exit(1)
				}
				i++
				cpuprofile = args[i]
			case strings.HasPrefix(a, "--cpuprofile="):
				cpuprofile = strings.TrimPrefix(a, "--cpuprofile=")
			case a == "--help" || a == "-h":
				fmt.Println("usage: fusion run [appdir] [--race] [--debug] [--cpuprofile FILE]")
				return
			case strings.HasPrefix(a, "-"):
				fmt.Printf("unknown flag %q\n", a)
				help()
				os.Exit(1)
			default:
				if dir != "." {
					fmt.Println("usage: fusion run [appdir] [--race] [--debug] [--cpuprofile FILE] (single target only)")
					os.Exit(1)
				}
				dir = a
			}
		}
		if err := cmdRunWithRaceProfile(dir, race, cpuprofile, debug); err != nil {
			fmt.Println("error:", err)
			os.Exit(1)
		}
	case "launch":
		if err := cmdLaunch(os.Args[2:]); err != nil {
			fmt.Println("error:", err)
			os.Exit(1)
		}
	case "test":
		if err := cmdTest(os.Args[2:]); err != nil {
			fmt.Println("error:", err)
			os.Exit(1)
		}
	case "build":
		dir := "."
		release := false
		secure := false
		password := ""
		keyFile := ""
		out := ""
		bin := false
		binOut := ""
		target := ""
		args := os.Args[2:]
		for i := 0; i < len(args); i++ {
			a := args[i]
			switch {
			case a == "--release":
				release = true
			case a == "--secure":
				secure = true
			case a == "--bin":
				bin = true
			case a == "--help" || a == "-h":
				fmt.Println("usage: fusion build [dir] [--release] [--secure [--password PWD] [--key-file FILE]] [--out DIR] [--bin [--target OS/ARCH] [-o FILE]]")
				return
			case a == "--password":
				if i+1 >= len(args) {
					fmt.Println("usage: fusion build [dir] [--secure [--password PWD] [--key-file FILE]]")
					os.Exit(1)
				}
				i++
				password = args[i]
				secure = true
			case strings.HasPrefix(a, "--password="):
				password = strings.TrimPrefix(a, "--password=")
				secure = true
			case a == "--key-file":
				if i+1 >= len(args) {
					fmt.Println("usage: fusion build [dir] [--secure [--password PWD] [--key-file FILE]]")
					os.Exit(1)
				}
				i++
				keyFile = args[i]
				secure = true
			case strings.HasPrefix(a, "--key-file="):
				keyFile = strings.TrimPrefix(a, "--key-file=")
				secure = true
			case a == "--out" || a == "-o":
				if i+1 >= len(args) {
					fmt.Println("usage: fusion build [dir] [--release] [--secure ...] [--out DIR] [--bin [--target OS/ARCH] [-o FILE]]")
					os.Exit(1)
				}
				i++
				out = args[i]
			case strings.HasPrefix(a, "--out="):
				out = strings.TrimPrefix(a, "--out=")
			case a == "--target":
				if i+1 >= len(args) {
					fmt.Println("usage: fusion build --bin [--target OS/ARCH]")
					os.Exit(1)
				}
				i++
				target = args[i]
				bin = true
			case strings.HasPrefix(a, "--target="):
				target = strings.TrimPrefix(a, "--target=")
				bin = true
			case strings.HasPrefix(a, "-"):
				fmt.Printf("unknown flag %q\n", a)
				help()
				os.Exit(1)
			default:
				dir = a
			}
		}
		if keyFile != "" {
			pw, err := lib.ReadKeyFile(keyFile)
			if err != nil {
				fmt.Println("error:", err)
				os.Exit(1)
			}
			password = pw
			secure = true
		}
		if bin {
			if out != "" {
				binOut = out
			}
			if err := cmdBuildBin(dir, binOut, target); err != nil {
				fmt.Println("error:", err)
				os.Exit(1)
			}
			break
		}
		if err := cmdBuildSecure(dir, release, secure, password, out); err != nil {
			fmt.Println("error:", err)
			os.Exit(1)
		}
	case "compile":
		if err := cmdCompile(os.Args[2:]); err != nil {
			fmt.Println("error:", err)
			os.Exit(1)
		}
	case "fmt":
		if err := cmdFmt(os.Args[2:]); err != nil {
			fmt.Println("error:", err)
			os.Exit(1)
		}
	case "vet":
		if err := cmdVet(os.Args[2:]); err != nil {
			fmt.Println("error:", err)
			os.Exit(1)
		}
	case "doc":
		if err := cmdDoc(os.Args[2:]); err != nil {
			fmt.Println("error:", err)
			os.Exit(1)
		}
	case "check":
		if err := cmdCheck(os.Args[2:]); err != nil {
			fmt.Println("error:", err)
			os.Exit(1)
		}
	case "repl":
		if err := cmdRepl(os.Args[2:]); err != nil {
			fmt.Println("error:", err)
			os.Exit(1)
		}
	case "bench":
		if err := cmdBench(os.Args[2:]); err != nil {
			fmt.Println("error:", err)
			os.Exit(1)
		}
	case "vendor":
		if err := cmdVendor(os.Args[2:]); err != nil {
			fmt.Println("error:", err)
			os.Exit(1)
		}
	case "run-web", "web", "serve":
		if err := cmdWeb(os.Args[2:]); err != nil {
			fmt.Println("error:", err)
			os.Exit(1)
		}
	case "build-js", "js":
		if err := cmdBuildJS(os.Args[2:]); err != nil {
			fmt.Println("error:", err)
			os.Exit(1)
		}
	case "build-ssg", "ssg":
		if err := cmdSSG(os.Args[2:]); err != nil {
			fmt.Println("error:", err)
			os.Exit(1)
		}
	case "publish":
		if err := cmdPublish(os.Args[2:]); err != nil {
			fmt.Println("error:", err)
			os.Exit(1)
		}
	case "pull":
		if err := cmdPull(os.Args[2:]); err != nil {
			fmt.Println("error:", err)
			os.Exit(1)
		}
	case "yank":
		if err := cmdYank(os.Args[2:]); err != nil {
			fmt.Println("error:", err)
			os.Exit(1)
		}
	case "registry":
		if err := cmdRegistry(os.Args[2:]); err != nil {
			fmt.Println("error:", err)
			os.Exit(1)
		}
	case "audit":
		if err := cmdAudit(os.Args[2:]); err != nil {
			fmt.Println("error:", err)
			os.Exit(1)
		}
	case "lsp":
		if err := cmdLSP(os.Args[2:]); err != nil {
			fmt.Println("error:", err)
			os.Exit(1)
		}
	case "debug":
		if err := cmdDebug(os.Args[2:]); err != nil {
			fmt.Println("error:", err)
			os.Exit(1)
		}
	case "version", "--version", "-V":
		fmt.Println("ks-fusion", toolVersion)
	case "help", "--help", "-h":
		help()
	default:
		fmt.Printf("unknown command %q\n", os.Args[1])
		help()
		os.Exit(1)
	}
}

// toolVersion is the toolchain/language version printed by help/version.
// Keep in sync with docs (README, docs/vs.md, docs/futures.md).
const toolVersion = "v2.5"

func help() {
	fmt.Println(`ks-fusion v2.5 (Go)
Commands:
  fusion new [--lib] <dir>   create app (backend/ frontend/ fusion.toml)
                             or library with --lib (src/lib.ks, type="lib")
  fusion run [appdir] [--race] [--debug] [--cpuprofile FILE]
                             run backend + frontend together
  fusion launch [target] [--backend] [--frontend] [--config FILE] [--race]
                             target is app dir (default ".") or config file
                             (fusion.toml / custom.toml, must live in app root:
                             entry paths resolve relative to its folder).
                             No flag = both together; --backend = only backend;
                             --frontend = only frontend; both flags = both.
  fusion build [dir] [--release] [--secure [--password PWD] [--key-file FILE]] [--out DIR] [--bin [--target OS/ARCH] [-o FILE]]
                             app: parse-check + cache + verify [dependencies]
                             (semver ^ ~ >= + fusion.lock + registry); --bin: single
                             static executable (embeds .ks + .kslib); --target:
                             linux/amd64,arm64,darwin,windows/amd64,wasm
                             lib: pack .kslib bundle into test-releases/
                                  (--release) or target/ (debug), like cargo
                                  --secure: opaque .ksx bundle (AES-256-GCM, no
                                  source in clear); no password = obfuscation,
                                  --password/--key-file/FUSION_KEY = real safety
   fusion compile <file.ks> [--out file.ksb] [--dis] [--run]
                              compile the .ks subset to bytecode (.ksb-1);
                              outside the subset, the compiler says so —
                              run those files with the interpreter instead
   fusion test [target]        run *_test.ks files (assert, TAP output)
                              target is dir (default ".") or a single .ks file
   fusion fmt [target] [--check]  format .ks (idempotent; --check for CI)
   fusion vet [target] [--deny-warns]  vet .ks (unused, arity, unknown, frontend env)
   fusion doc [target] [--out FILE]  docs from # comments + func sigs
   fusion check [target]      strict check (parse + arity + :type)
   fusion repl                interactive .ks (multiline via braces)
   fusion bench [target] [--n N] [--cpuprofile FILE]  bench + profile
   fusion vendor [appdir]     copy .kslib deps into vendor/ (offline)
   fusion publish [libdir] [--registry DIR]  publish .kslib + sha256 + index
   fusion pull <name[@spec]> [--out DIR]  fetch + verify sha256
   fusion yank <name[@ver]> [--remove]  yank registry version
   fusion registry            list registry packages
   fusion run-web [appdir] [--port N] [--watch]  SSR + /api/* + SSE HMR patch
   fusion build-js [appdir] [--out DIR]  transpile pages to JS per-route + hashes
   fusion build-ssg [appdir] [--out DIR]  pre-render routes to HTML+JSON (ISR)
   fusion audit [appdir]      check lock vs registry (yanked/updates/checksums)
   fusion lsp                 LSP (hover/goto-def/rename/diagnostics/format) for VS Code
   fusion debug <file.ks> [--break LINE] [--trace]  breakpoints + trace + globals
   fusion prog.ks|lib.kslib|lib.ksx   run a single file directly.
                              .kslib bundles start with #!/usr/bin/env fusion,
                              so: chmod +x lib.kslib && ./lib.kslib
                              .ksx secure bundles look like random bytes
                              (needs fusion on PATH + FUSION_KEY if password was set;
                              --bin needs no runtime)
   fusion version|--version   print toolchain version
   fusion help`)
}

const fusionTomlTmpl = `# App made in ks-fusion
[package]
name = "%s"
version = "1.0.0"
entry_backend = "backend/main.ks"
entry_frontend = "frontend/main.ks"
`

const fusionLibTomlTmpl = `# Library made in ks-fusion (like ` + "`cargo new --lib`" + `)
[package]
name = "%s"
version = "0.1.0"
type = "lib"

[lib]
name = "%s"
path = "src/lib.ks"
`

const libTmpl = `# src/lib.ks - %s library entry
# Import me with: import "%s" (after ` + "`fusion build --release`" + `)

func greet(name) {
  return "hello " + name + " from %s"
}

func add(a, b) {
  return a + b
}
`

const backendTmpl = `# backend/main.ks - logic with Go-like concurrency
let app = "hello-app"
print "backend: starting " + app

func fib(n) {
  if n < 2 {
    return n
  }
  return fib(n - 1) + fib(n - 2)
}
print "fib(10) =", fib(10)

let ch = chan(2)
go func() {
  for i in range(3) {
    send(ch, i * 10)
  }
  close(ch)
}()
for v in range(3) {
  print "backend: job", recv(ch)
}
print "backend: ok"
`

const frontendTmpl = `# frontend/main.ks - entry: route table + layout only (P0).
# File name = route name. No business logic here; pages/components own it.
import "frontend/store/app.ks"
import "frontend/components/header.ks"
import "frontend/layouts/app.ks"
import "frontend/pages/home.ks"
import "frontend/pages/hi.ks"

func render_console(vm) {
  let t = vm?.type ?? "unknown"
  if t == "page" {
    let p = vm.props
    print p.title
    print "count = " + p.count
    print "user:", p.user.name
    for i, tag in p.user.tags {
      print "tag", i, "=", tag
    }
    return nil
  }
  if t == "text" {
    print vm.props?.title ?? "hi"
    return nil
  }
  print json_stringify(vm)
  return nil
}

let route = env("ROUTE", "/")
let r = app_fetch_user()
assert(is_ok(r))

if route == "/" {
  let vm = home_page(app_state())
  let app = app_layout(vm)
  assert(app.key == "app")
  render_console(vm)
} else if route == "/hi" {
  render_console(hi_page({}))
} else {
  error("unknown route: " + route)
}
print "frontend: ok"
`

const frontendStoreTmpl = `# frontend/store/app.ks - shared state + helpers (no view code).
# P0: single state map threaded as context; fetches return ok()/err().

let app_title = "Hello from ks-fusion"

func app_state() {
  return {
    title: app_title,
    count: 1 + 2,
    user: {name: "ada", tags: ["ks", "fusion"]}
  }
}

func app_fetch_user() {
  let s = app_state()
  return ok(s.user)
}
`

const frontendHeaderTmpl = `# frontend/components/header.ks - one func per component.
# Contract: (props: map) -> view-model {key, type, props, children}.

func header_render(props) {
  let title = props?.title ?? "untitled"
  return {key: "header", type: "header", props: {title: title}, children: []}
}
`

const frontendLayoutTmpl = `# frontend/layouts/app.ks - shared wrapper (header/footer once).
# Contract: (page: map) -> view-model.

func app_layout(page) {
  return {key: "app", type: "layout", props: {}, children: [page]}
}
`

const frontendHomeTmpl = `# frontend/pages/home.ks - "/" route.
# Contract: (props: map) -> view-model. Uses store + header component.

func home_page(props) {
  let title = props?.title ?? app_title
  let user = props?.user ?? {name: "ada", tags: ["ks", "fusion"]}
  let head = header_render({title: title})
  return {
    key: "home",
    type: "page",
    props: {title: title, count: 1 + 2, user: user},
    children: [head],
    head: {title: title}
  }
}
`

const frontendHiTmpl = `# frontend/pages/hi.ks - "/hi" route.
# Contract: (props: map) -> view-model.

func hi_page(props) {
  return {key: "hi", type: "text", props: {title: "hi"}, children: []}
}
`

func cmdNew(dir string) error {
	for _, d := range []string{
		filepath.Join(dir, "backend"),
		filepath.Join(dir, "frontend"),
		filepath.Join(dir, "frontend", "pages"),
		filepath.Join(dir, "frontend", "components"),
		filepath.Join(dir, "frontend", "layouts"),
		filepath.Join(dir, "frontend", "store"),
	} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return err
		}
	}
	name := filepath.Base(dir)
	if err := os.WriteFile(filepath.Join(dir, "fusion.toml"), []byte(fmt.Sprintf(fusionTomlTmpl, name)), 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "backend", "main.ks"), []byte(backendTmpl), 0o644); err != nil {
		return err
	}
	frontendFiles := map[string]string{
		filepath.Join(dir, "frontend", "main.ks"):               frontendTmpl,
		filepath.Join(dir, "frontend", "store", "app.ks"):       frontendStoreTmpl,
		filepath.Join(dir, "frontend", "components", "header.ks"): frontendHeaderTmpl,
		filepath.Join(dir, "frontend", "layouts", "app.ks"):     frontendLayoutTmpl,
		filepath.Join(dir, "frontend", "pages", "home.ks"):      frontendHomeTmpl,
		filepath.Join(dir, "frontend", "pages", "hi.ks"):        frontendHiTmpl,
	}
	for path, content := range frontendFiles {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			return err
		}
	}
	fmt.Println("created", dir, "with backend/ frontend/{main,pages,components,layouts,store}/ fusion.toml")
	return nil
}

// cmdNewLib scaffolds a library package, like `cargo new --lib`.
func cmdNewLib(dir string) error {
	if err := os.MkdirAll(filepath.Join(dir, "src"), 0o755); err != nil {
		return err
	}
	name := filepath.Base(dir)
	if err := os.WriteFile(filepath.Join(dir, "fusion.toml"), []byte(fmt.Sprintf(fusionLibTomlTmpl, name, name)), 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "src", "lib.ks"), []byte(fmt.Sprintf(libTmpl, name, name, name)), 0o644); err != nil {
		return err
	}
	fmt.Println("created library", dir, "with src/lib.ks fusion.toml (type = \"lib\")")
	return nil
}

// defaultOutDir mirrors cargo: debug bundles go to target/,
// --release bundles go to test-releases/.
func defaultOutDir(release bool) string {
	if release {
		return "test-releases"
	}
	return "target"
}

func cmdBuild(dir string, release bool, out string) error {
	return cmdBuildSecure(dir, release, false, "", out)
}

func cmdBuildSecure(dir string, release, secure bool, password, out string) error {
	cfg, err := config.Load(dir)
	if err != nil {
		return err
	}
	// v2.3 build cache: skip redundant work when hash matches
	// (secure builds always rebuild so --secure never returns a stale plain hit).
	if !secure {
		if hit, _, _ := tools.CheckCache(cfg.Dir); hit {
			fmt.Printf("build cached: %s (no changes)\n", cfg.Dir)
			return nil
		}
	}
	if cfg.IsLib() {
		if out == "" {
			out = defaultOutDir(release)
		}
		if secure {
			artifact, err := lib.BuildSecure(cfg, out, password)
			if err != nil {
				return err
			}
			mode := "opaque (default key)"
			if lib.ResolvePassword(password) != "" {
				mode = "encrypted (password)"
			}
			fmt.Printf("built secure %s v%s: %s (%s)\n", cfg.LibName, cfg.Version, artifact, mode)
			fmt.Printf("use it with: import %q (needs same FUSION_KEY if password was set)\n", cfg.LibName)
			_ = tools.WriteCache(cfg.Dir)
			return nil
		}
		// lib.Build parse-checks every .ks source, so a successful
		// build also means the whole lib is valid.
		artifact, err := lib.Build(cfg, out)
		if err != nil {
			return err
		}
		profile := "debug"
		if release {
			profile = "release"
		}
		fmt.Printf("built %s v%s (%s): %s\n", cfg.LibName, cfg.Version, profile, artifact)
		fmt.Printf("use it with: import %q\n", cfg.LibName)
		_ = tools.WriteCache(cfg.Dir)
		return nil
	}
	if secure {
		return fmt.Errorf("--secure needs a library (type = \"lib\"), got app %s", cfg.Dir)
	}
	for _, f := range []string{cfg.BackendPath(), cfg.FrontendPath()} {
		if _, err := frontend.ParseFile(f); err != nil {
			return fmt.Errorf("build failed: %w", err)
		}
		fmt.Println("ok:", f)
	}
	if err := checkDependencies(cfg); err != nil {
		return err
	}
	_ = tools.WriteCache(cfg.Dir)
	fmt.Printf("build ok: %s v%s\n", cfg.Name, cfg.Version)
	return nil
}

// checkDependencies verifies every [dependencies] entry (cargo-style):
// `name = "1.0.0"` must match a built .kslib bundle (semver: ^ ~ >= > < * supported),
// `name = { path = "..." }` must point at a package with fusion.toml.
// Writes fusion.lock with resolved versions.
func checkDependencies(cfg *config.Config) error {
	if len(cfg.Dependencies) == 0 {
		return nil
	}
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
	names := make([]string, 0, len(cfg.Dependencies))
	for name := range cfg.Dependencies {
		names = append(names, name)
	}
	sortStrings(names)
	paths := map[string]string{}
	vers := map[string]string{}
	for _, name := range names {
		spec := cfg.Dependencies[name]
		if strings.HasPrefix(spec, "path:") {
			rel := strings.TrimPrefix(spec, "path:")
			p := rel
			if !filepath.IsAbs(rel) {
				p = filepath.Join(cfg.Dir, filepath.FromSlash(rel))
			}
			fi, err := os.Stat(filepath.Join(p, "fusion.toml"))
			if err != nil || fi.IsDir() {
				return fmt.Errorf("build failed: dependency %q path %q has no fusion.toml: %v", name, p, err)
			}
			fmt.Printf("dep ok: %s (path %s)\n", name, p)
			paths[name] = spec
			vers[name] = spec
			continue
		}
		// semver-aware resolver (v2.2): newest satisfying spec, not just newest
		found, ver, err := tools.ResolveDep(name, spec, dirs)
		if err != nil {
			// fallback to legacy newest-wins for exact-match ergonomics
			found2, err2 := lib.Find(name, dirs)
			if err2 != nil {
				return fmt.Errorf("build failed: dependency %q (%s) not built: %v", name, spec, err)
			}
			fmt.Printf("dep ok: %s (%s)\n", name, found2)
			paths[name] = found2
			vers[name] = spec
			continue
		}
		fmt.Printf("dep ok: %s (%s, spec %q)\n", name, found, spec)
		_ = ver
		paths[name] = found
		vers[name] = ver
	}
	_ = tools.WriteLock(cfg.Dir, paths, vers)
	return nil
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

func cmdRun(dir string) error {
	return cmdRunWithRace(dir, false)
}

func cmdRunWithRace(dir string, race bool) error {
	return cmdRunWithRaceProfile(dir, race, "", false)
}

func cmdRunWithRaceProfile(dir string, race bool, cpuprofile string, debug bool) error {
	cfg, err := config.Load(dir)
	if err != nil {
		return err
	}
	if cpuprofile != "" {
		f, err := os.Create(cpuprofile)
		if err != nil {
			return err
		}
		defer f.Close()
		if err := startCPUProfile(f); err != nil {
			return err
		}
		defer stopCPUProfile()
		fmt.Println("cpuprofile: writing to", cpuprofile)
	}
	if debug {
		fmt.Printf("debug: %s v%s\n", cfg.Name, cfg.Version)
		for _, e := range []string{cfg.BackendEntry, cfg.FrontendEntry} {
			p := e
			fmt.Printf("debug: entry %s\n", p)
		}
		if issues, err := tools.VetTarget(dir, false); err == nil {
			fmt.Printf("debug: vet %d issues\n", len(issues))
			for _, is := range issues {
				fmt.Println("debug:", is.String())
			}
		}
		os.Setenv("FUSION_DEBUG", "1")
	}
	if race {
		fmt.Println("race: enabled (Go race detector via `go run -race ./cmd/fusion run` + logical chan checks)")
		if issues, err := tools.VetTarget(dir, false); err == nil {
			errs := 0
			for _, is := range issues {
				if is.IsError {
					errs++
				}
			}
			if errs > 0 {
				return fmt.Errorf("race: vet found %d errors, fix before race run", errs)
			}
		}
		os.Setenv("FUSION_RACE", "1")
	}
	err = runWithConfig(cfg, true, true)
	if race {
		fmt.Println("race: ok (no logical races detected; for full Go data-race run: go run -race ./cmd/fusion run " + dir + ")")
	}
	if cpuprofile != "" {
		fmt.Println("cpuprofile: done", cpuprofile)
	}
	return err
}

// cmdLaunch is the package.json-scripts style entry:
// `fusion launch .` runs backend+frontend from ./fusion.toml,
// `fusion launch <configfile>` uses that file (custom name allowed, must live
// in the app root: entry_backend/entry_frontend are relative to its folder),
// `fusion launch --backend .` runs only backend, `--frontend` only frontend.
func cmdLaunch(args []string) error {
	target := "."
	configFlag := ""
	wantBackend := false
	wantFrontend := false
	seenBackend := false
	seenFrontend := false
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--backend" || a == "-b" || a == "--only-backend" || a == "--backend-only":
			wantBackend = true
			seenBackend = true
		case a == "--frontend" || a == "-f" || a == "--only-frontend" || a == "--frontend-only":
			wantFrontend = true
			seenFrontend = true
		case a == "--both" || a == "--all":
			wantBackend = true
			wantFrontend = true
			seenBackend = true
			seenFrontend = true
		case a == "--config" || a == "-c":
			if i+1 >= len(args) {
				return fmt.Errorf("usage: fusion launch [target] [--backend] [--frontend] [--config FILE]")
			}
			i++
			if configFlag != "" {
				return fmt.Errorf("single --config only (got %q and %q)", configFlag, args[i])
			}
			configFlag = args[i]
		case strings.HasPrefix(a, "--config="):
			v := strings.TrimPrefix(a, "--config=")
			if v == "" {
				return fmt.Errorf("usage: fusion launch [target] [--backend] [--frontend] [--config FILE]")
			}
			if configFlag != "" {
				return fmt.Errorf("single --config only (got %q and %q)", configFlag, v)
			}
			configFlag = v
		case a == "--race":
			os.Setenv("FUSION_RACE", "1")
			fmt.Println("race: enabled")
		case a == "--help" || a == "-h":
			fmt.Println(`usage: fusion launch [target] [--backend] [--frontend] [--config FILE] [--race]
  target: app dir (default ".") or config file (fusion.toml, custom.toml).
          The config file must live in the app root: entry_backend /
          entry_frontend are relative to its folder.
  no flag: both backend + frontend together; --backend: only backend;
  --frontend: only frontend; --backend --frontend: both; --race: race checks.`)
			return nil
		case strings.HasPrefix(a, "-"):
			return fmt.Errorf("unknown flag %q (usage: fusion launch [target] [--backend] [--frontend] [--config FILE] [--race])", a)
		default:
			if target != "." {
				return fmt.Errorf("usage: fusion launch [target] [--backend] [--frontend] [--config FILE] (single target only)")
			}
			target = a
		}
	}
	if configFlag != "" {
		if target != "." {
			return fmt.Errorf("pass either a target or --config %q, not both", configFlag)
		}
		target = configFlag
	}
	if !seenBackend && !seenFrontend {
		wantBackend = true
		wantFrontend = true
	}
	cfg, err := resolveLaunchConfig(target)
	if err != nil {
		return err
	}
	return runWithConfig(cfg, wantBackend, wantFrontend)
}

// resolveLaunchConfig loads either an app dir (with fusion.toml inside)
// or an explicit config file path.
func resolveLaunchConfig(target string) (*config.Config, error) {
	fi, err := os.Stat(target)
	if err != nil {
		return nil, err
	}
	if fi.IsDir() {
		return config.Load(target)
	}
	return config.LoadFile(target)
}

func runWithConfig(cfg *config.Config, runBackend, runFrontend bool) error {
	dir := cfg.Dir
	if cfg.IsLib() {
		return fmt.Errorf("%s is a lib (type = \"lib\"), nothing to launch: use `fusion build %s` then `import %q` from an app", cfg.Source, dir, cfg.LibName)
	}
	if !runBackend && !runFrontend {
		return fmt.Errorf("nothing to run (need --backend and/or --frontend)")
	}
	if runBackend && runFrontend {
		bp, err := frontend.ParseFile(cfg.BackendPath())
		if err != nil {
			return launchEntryError(cfg, "backend", cfg.BackendEntry, cfg.BackendPath(), err)
		}
		fp, err := frontend.ParseFile(cfg.FrontendPath())
		if err != nil {
			return launchEntryError(cfg, "frontend", cfg.FrontendEntry, cfg.FrontendPath(), err)
		}
		fmt.Printf("== %s v%s ==\n", cfg.Name, cfg.Version)
		// Fusion: backend + frontend run together (concurrently).
		// Imports resolve relative to the app dir.
		var wg sync.WaitGroup
		var bErr, fErr error
		wg.Add(2)
		go func() {
			defer wg.Done()
			fmt.Println("[backend]")
			bErr = backend.RunWithDir(bp, dir)
		}()
		go func() {
			defer wg.Done()
			fmt.Println("[frontend]")
			fErr = backend.RunWithDir(fp, dir)
		}()
		wg.Wait()
		if bErr != nil {
			return bErr
		}
		return fErr
	}
	var prog *frontend.Program
	var label string
	var path string
	var entry string
	if runBackend {
		label = "[backend]"
		path = cfg.BackendPath()
		entry = cfg.BackendEntry
	} else {
		label = "[frontend]"
		path = cfg.FrontendPath()
		entry = cfg.FrontendEntry
	}
	var err error
	prog, err = frontend.ParseFile(path)
	if err != nil {
		side := "backend"
		if !runBackend {
			side = "frontend"
		}
		return launchEntryError(cfg, side, entry, path, err)
	}
	fmt.Printf("== %s v%s (%s only) ==\n", cfg.Name, cfg.Version, label)
	fmt.Println(label)
	return backend.RunWithDir(prog, dir)
}

// launchEntryError explains a missing/unparsable entry file, including the
// custom-config pitfall: entries are relative to the config file's folder,
// so the config must live in the app root.
func launchEntryError(cfg *config.Config, side, entry, abs string, err error) error {
	return fmt.Errorf("launch %s: %s entry %q not found at %s (config %s lives in %s; keep the config in the app root so relative entries resolve): %w", cfg.Source, side, entry, abs, cfg.Source, cfg.Dir, err)
}

// cmdTest runs `*_test.ks` files with `assert` and reports TAP.
// `fusion test [target]`: target is a dir (default ".", searched recursively)
// or a single .ks file. Each file runs in a fresh interpreter (no shared
// globals between files); any parse error, failed assert, or uncaught error
// marks that file `not ok`. Exit is non-zero when any file fails.
func cmdTest(args []string) error {
	target := "."
	timeoutSecs := 30
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--help" || a == "-h":
			fmt.Println(`usage: fusion test [target] [--timeout SECS]
  target: dir (default ".", runs every *_test.ks underneath) or a single .ks file
  each file runs isolated; output is TAP (ok / not ok + 1..N plan)
  --timeout SECS: per-file timeout in seconds (default 30, 0 = no timeout);
  a timed-out file is reported `+"`not ok`"+` and the run continues`)
			return nil
		case a == "--timeout":
			if i+1 >= len(args) {
				return fmt.Errorf("usage: fusion test [target] [--timeout SECS]")
			}
			i++
			n, err := strconv.Atoi(args[i])
			if err != nil || n < 0 {
				return fmt.Errorf("bad --timeout %q: want seconds >= 0", args[i])
			}
			timeoutSecs = n
		case strings.HasPrefix(a, "--timeout="):
			n, err := strconv.Atoi(strings.TrimPrefix(a, "--timeout="))
			if err != nil || n < 0 {
				return fmt.Errorf("bad --timeout %q: want seconds >= 0", a)
			}
			timeoutSecs = n
		default:
			if strings.HasPrefix(a, "-") {
				return fmt.Errorf("unknown flag %q (usage: fusion test [target] [--timeout SECS])", a)
			}
			if target != "." {
				return fmt.Errorf("usage: fusion test [target] [--timeout SECS] (single target only)")
			}
			target = a
		}
	}
	files, err := collectTestFiles(target)
	if err != nil {
		return err
	}
	if len(files) == 0 {
		return fmt.Errorf("no *_test.ks files under %s", target)
	}
	fmt.Println("TAP version 13")
	failed := 0
	for i, f := range files {
		name := displayPath(f)
		if err := runTestFileTimeout(f, timeoutSecs); err != nil {
			fmt.Printf("not ok %d - %s (%v)\n", i+1, name, err)
			failed++
			continue
		}
		fmt.Printf("ok %d - %s\n", i+1, name)
	}
	fmt.Printf("1..%d\n", len(files))
	if failed > 0 {
		return fmt.Errorf("test failed: %d of %d files failed", failed, len(files))
	}
	return nil
}

// collectTestFiles returns sorted *_test.ks paths for a dir (recursive)
// or the single file itself.
func collectTestFiles(target string) ([]string, error) {
	fi, err := os.Stat(target)
	if err != nil {
		return nil, err
	}
	if !fi.IsDir() {
		if !strings.HasSuffix(target, ".ks") {
			return nil, fmt.Errorf("not a .ks file: %s", target)
		}
		return []string{target}, nil
	}
	var out []string
	walkErr := filepath.Walk(target, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if strings.HasSuffix(info.Name(), "_test.ks") {
			out = append(out, path)
		}
		return nil
	})
	if walkErr != nil {
		return nil, walkErr
	}
	sortStrings(out)
	return out, nil
}

// runTestFile parses + runs one test file in a fresh interpreter.
// baseDir is the nearest ancestor holding fusion.toml (so app-root-relative
// imports work from nested test files), else the file's own directory.
func runTestFile(path string) error {
	return runTestFileTimeout(path, 0)
}

// runTestFileTimeout runs one test file with a per-file timeout.
// timeoutSecs <= 0 means no timeout. A timed-out file returns an error and
// the TAP run continues with the next file. Note: a hung interpreter
// goroutine cannot be force-stopped, so it is abandoned (leaked) after the
// timeout fires; `go test` unit tests must not hang instead.
func runTestFileTimeout(path string, timeoutSecs int) error {
	prog, err := frontend.ParseFile(path)
	if err != nil {
		return err
	}
	if timeoutSecs <= 0 {
		return backend.RunWithDir(prog, testAppRoot(path))
	}
	type result struct{ err error }
	ch := make(chan result, 1)
	go func() {
		ch <- result{backend.RunWithDir(prog, testAppRoot(path))}
	}()
	select {
	case r := <-ch:
		return r.err
	case <-time.After(time.Duration(timeoutSecs) * time.Second):
		return fmt.Errorf("timeout after %ds (per-file --timeout)", timeoutSecs)
	}
}

// testAppRoot finds the app dir for a test file: nearest ancestor with
// fusion.toml, else the file's directory.
func testAppRoot(path string) string {
	abs := path
	if !filepath.IsAbs(abs) {
		if a, err := filepath.Abs(abs); err == nil {
			abs = a
		}
	}
	dir := filepath.Dir(abs)
	for {
		if _, err := os.Stat(filepath.Join(dir, "fusion.toml")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return filepath.Dir(abs)
		}
		dir = parent
	}
}

// displayPath shows a path relative to CWD when possible (stable TAP names).
func displayPath(path string) string {
	if rel, err := filepath.Rel(".", path); err == nil && !strings.HasPrefix(rel, "..") {
		return filepath.ToSlash(rel)
	}
	return path
}

func cmdCompile(args []string) error {
	src := ""
	out := ""
	dis := false
	run := false
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--dis":
			dis = true
		case a == "--run":
			run = true
		case a == "--out" || a == "-o":
			if i+1 >= len(args) {
				return fmt.Errorf("usage: fusion compile <file.ks> [--out file.ksb] [--dis] [--run]")
			}
			i++
			out = args[i]
		case strings.HasPrefix(a, "--out="):
			out = strings.TrimPrefix(a, "--out=")
		case strings.HasPrefix(a, "-"):
			return fmt.Errorf("unknown flag %q (usage: fusion compile <file.ks> [--out file.ksb] [--dis] [--run])", a)
		default:
			if src != "" {
				return fmt.Errorf("usage: fusion compile <file.ks> [--out file.ksb] [--dis] [--run]")
			}
			src = a
		}
	}
	if src == "" {
		return fmt.Errorf("usage: fusion compile <file.ks> [--out file.ksb] [--dis] [--run]")
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	b, err := compiler.CompileSource(string(data), src)
	if err != nil {
		return err
	}
	if dis {
		fmt.Print(compiler.Disassemble(b))
	}
	if out == "" && !dis && !run {
		out = strings.TrimSuffix(src, ".ks") + compiler.Ext
	}
	if out != "" {
		if err := compiler.Save(b, out); err != nil {
			return err
		}
		fmt.Println("compiled", src, "->", out)
	}
	if run {
		return compiler.Run(b)
	}
	if out == "" && !dis {
		fmt.Print(compiler.Disassemble(b))
	}
	return nil
}
