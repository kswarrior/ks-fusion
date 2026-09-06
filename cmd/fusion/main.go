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
	"strings"
	"sync"

	"github.com/kswarrior/ks-fusion/internal/backend"
	"github.com/kswarrior/ks-fusion/internal/compiler"
	"github.com/kswarrior/ks-fusion/internal/config"
	"github.com/kswarrior/ks-fusion/internal/frontend"
	"github.com/kswarrior/ks-fusion/internal/lib"
)

func main() {
	if len(os.Args) < 2 {
		help()
		return
	}
	// Direct file mode: `fusion prog.ks` or `fusion lib.kslib`
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
		(strings.HasSuffix(a, ".ks") || strings.HasSuffix(a, lib.Ext)) {
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
		if len(os.Args) >= 3 {
			dir = os.Args[2]
		}
		if err := cmdRun(dir); err != nil {
			fmt.Println("error:", err)
			os.Exit(1)
		}
	case "launch":
		if err := cmdLaunch(os.Args[2:]); err != nil {
			fmt.Println("error:", err)
			os.Exit(1)
		}
	case "build":
		dir := "."
		release := false
		out := ""
		args := os.Args[2:]
		for i := 0; i < len(args); i++ {
			a := args[i]
			switch {
			case a == "--release":
				release = true
			case a == "--out" || a == "-o":
				if i+1 >= len(args) {
					fmt.Println("usage: fusion build [dir] [--release] [--out DIR]")
					os.Exit(1)
				}
				i++
				out = args[i]
			case strings.HasPrefix(a, "--out="):
				out = strings.TrimPrefix(a, "--out=")
			case strings.HasPrefix(a, "-"):
				fmt.Printf("unknown flag %q\n", a)
				help()
				os.Exit(1)
			default:
				dir = a
			}
		}
		if err := cmdBuild(dir, release, out); err != nil {
			fmt.Println("error:", err)
			os.Exit(1)
		}
	case "compile":
		if err := cmdCompile(os.Args[2:]); err != nil {
			fmt.Println("error:", err)
			os.Exit(1)
		}
	case "help", "--help", "-h":
		help()
	default:
		fmt.Printf("unknown command %q\n", os.Args[1])
		help()
		os.Exit(1)
	}
}

func help() {
	fmt.Println(`ks-fusion v2.0 (Go)
Commands:
  fusion new [--lib] <dir>   create app (backend/ frontend/ fusion.toml)
                             or library with --lib (src/lib.ks, type="lib")
  fusion run [appdir]        run backend + frontend together
  fusion launch [target] [--backend] [--frontend] [--config FILE]
                             target is app dir (default ".") or config file
                             (fusion.toml / custom.toml, must live in app root:
                             entry paths resolve relative to its folder).
                             No flag = both together; --backend = only backend;
                             --frontend = only frontend; both flags = both.
  fusion build [dir] [--release] [--out DIR]
                             app: parse-check entries + verify [dependencies]
                             lib: pack .kslib bundle into test-releases/
                                  (--release) or target/ (debug), like cargo
  fusion compile <file.ks> [--out file.ksb] [--dis] [--run]
                             compile the .ks subset to bytecode (.ksb-1);
                             outside the subset, the compiler says so —
                             run those files with the interpreter instead
  fusion prog.ks|lib.kslib   run a single file directly.
                             .kslib bundles start with #!/usr/bin/env fusion,
                             so: chmod +x lib.kslib && ./lib.kslib
                             (needs fusion on PATH)
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
	cfg, err := config.Load(dir)
	if err != nil {
		return err
	}
	if cfg.IsLib() {
		if out == "" {
			out = defaultOutDir(release)
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
		return nil
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
	fmt.Printf("build ok: %s v%s\n", cfg.Name, cfg.Version)
	return nil
}

// checkDependencies verifies every [dependencies] entry (cargo-style):
// `name = "1.0.0"` must match a built .kslib bundle, and
// `name = { path = "..." }` must point at a package with fusion.toml.
func checkDependencies(cfg *config.Config) error {
	if len(cfg.Dependencies) == 0 {
		return nil
	}
	dirs := []string{
		filepath.Join(cfg.Dir, "test-releases"),
		filepath.Join(cfg.Dir, "target"),
		filepath.Join(cfg.Dir, "release"),
		"test-releases",
		"target",
		"release",
	}
	names := make([]string, 0, len(cfg.Dependencies))
	for name := range cfg.Dependencies {
		names = append(names, name)
	}
	sortStrings(names)
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
			continue
		}
		found, err := lib.Find(name, dirs)
		if err != nil {
			return fmt.Errorf("build failed: dependency %q (%s) not built: %v", name, spec, err)
		}
		fmt.Printf("dep ok: %s (%s)\n", name, found)
	}
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
	cfg, err := config.Load(dir)
	if err != nil {
		return err
	}
	return runWithConfig(cfg, true, true)
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
		case a == "--help" || a == "-h":
			fmt.Println(`usage: fusion launch [target] [--backend] [--frontend] [--config FILE]
  target: app dir (default ".") or config file (fusion.toml, custom.toml).
          The config file must live in the app root: entry_backend /
          entry_frontend are relative to its folder.
  no flag: both backend + frontend together; --backend: only backend;
  --frontend: only frontend; --backend --frontend: both.`)
			return nil
		case strings.HasPrefix(a, "-"):
			return fmt.Errorf("unknown flag %q (usage: fusion launch [target] [--backend] [--frontend] [--config FILE])", a)
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
