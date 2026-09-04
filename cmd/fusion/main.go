// Command fusion is the ks-fusion tool (v1.0, in Go).
// Usage:
// fusion new <appdir>   scaffold backend/ frontend/ fusion.toml
// fusion run [appdir]   run the app (.ks files)
// fusion build [appdir] check the app (parse only)
// fusion help
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/kswarrior/ks-fusion/internal/backend"
	"github.com/kswarrior/ks-fusion/internal/config"
	"github.com/kswarrior/ks-fusion/internal/frontend"
)

func main() {
	if len(os.Args) < 2 {
		help()
		return
	}
	switch os.Args[1] {
	case "new":
		if len(os.Args) < 3 {
			fmt.Println("usage: fusion new <appdir>")
			os.Exit(1)
		}
		if err := cmdNew(os.Args[2]); err != nil {
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
		if len(os.Args) >= 3 {
			dir = os.Args[2]
		}
		if err := cmdBuild(dir); err != nil {
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
	fmt.Println(`ks-fusion v1.0 (Go)
Commands:
  fusion new <appdir>   create backend/ frontend/ fusion.toml
  fusion run [appdir]   run backend + frontend together
  fusion build [appdir] parse-check only
  fusion help`)
}

const fusionTomlTmpl = `# App made in ks-fusion
[package]
name = "%s"
version = "1.0.0"
entry_backend = "backend/main.ks"
entry_frontend = "frontend/main.ks"
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

func cmdBuild(dir string) error {
	cfg, err := config.Load(dir)
	if err != nil {
		return err
	}
	for _, f := range []string{cfg.BackendPath(), cfg.FrontendPath()} {
		if _, err := frontend.ParseFile(f); err != nil {
			return fmt.Errorf("build failed: %w", err)
		}
		fmt.Println("ok:", f)
	}
	fmt.Printf("build ok: %s v%s\n", cfg.Name, cfg.Version)
	return nil
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
