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

const frontendTmpl = `# frontend/main.ks - UI text
let title = "Hello from ks-fusion"
print title

let user = {name: "ada", tags: ["ks", "fusion"]}
print "user:", user.name, user.tags

for i, t in user.tags {
  print "tag", i, "=", t
}
print "frontend: ok"
`

func cmdNew(dir string) error {
	for _, d := range []string{filepath.Join(dir, "backend"), filepath.Join(dir, "frontend")} {
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
	if err := os.WriteFile(filepath.Join(dir, "frontend", "main.ks"), []byte(frontendTmpl), 0o644); err != nil {
		return err
	}
	fmt.Println("created", dir, "with backend/ frontend/ fusion.toml")
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
	bp, err := frontend.ParseFile(cfg.BackendPath())
	if err != nil {
		return err
	}
	fp, err := frontend.ParseFile(cfg.FrontendPath())
	if err != nil {
		return err
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
