package main

import (
	"fmt"
	"strings"

	"github.com/kswarrior/ks-fusion/internal/tools"
)

func cmdBuildBin(dir, out, target string) error {
	if dir == "" {
		dir = "."
	}
	return tools.BuildBin(dir, out, target)
}

func cmdVendor(args []string) error {
	target := "."
	for _, a := range args {
		switch {
		case a == "--help" || a == "-h":
			fmt.Println("usage: fusion vendor [appdir]\n  copy resolved .kslib bundles into vendor/ for offline builds")
			return nil
		case strings.HasPrefix(a, "-"):
			return fmt.Errorf("unknown flag %q", a)
		default:
			if target != "." {
				return fmt.Errorf("usage: fusion vendor [appdir] (single target only)")
			}
			target = a
		}
	}
	return tools.VendorApp(target)
}

func cmdWeb(args []string) error {
	dir := "."
	port := 8080
	watch := false
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--help" || a == "-h":
			fmt.Println("usage: fusion run-web [appdir] [--port N] [--watch]\n  SSR frontend/ as HTML+JSON (+ /api/*, ?format=json, /events SSE when --watch)")
			return nil
		case a == "--watch" || a == "-w":
			watch = true
		case a == "--port" || a == "-p":
			if i+1 >= len(args) {
				return fmt.Errorf("usage: fusion run-web [appdir] [--port N] [--watch]")
			}
			i++
			var p int
			if _, err := fmt.Sscanf(args[i], "%d", &p); err != nil {
				return fmt.Errorf("bad --port %q", args[i])
			}
			port = p
		case strings.HasPrefix(a, "--port="):
			var p int
			if _, err := fmt.Sscanf(strings.TrimPrefix(a, "--port="), "%d", &p); err != nil {
				return fmt.Errorf("bad --port %q", a)
			}
			port = p
		case strings.HasPrefix(a, "-"):
			return fmt.Errorf("unknown flag %q", a)
		default:
			if dir != "." {
				return fmt.Errorf("usage: fusion run-web [appdir] (single target only)")
			}
			dir = a
		}
	}
	return tools.RunWebWithWatch(dir, port, watch)
}

func cmdSSG(args []string) error {
	dir := "."
	out := ""
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--help" || a == "-h":
			fmt.Println("usage: fusion build-ssg [appdir] [--out DIR]\n  pre-render routes to HTML+JSON (SSG)")
			return nil
		case a == "--out" || a == "-o":
			if i+1 >= len(args) {
				return fmt.Errorf("usage: fusion build-ssg [appdir] [--out DIR]")
			}
			i++
			out = args[i]
		case strings.HasPrefix(a, "--out="):
			out = strings.TrimPrefix(a, "--out=")
		case strings.HasPrefix(a, "-"):
			return fmt.Errorf("unknown flag %q", a)
		default:
			if dir != "." {
				return fmt.Errorf("usage: fusion build-ssg [appdir] (single target only)")
			}
			dir = a
		}
	}
	return tools.BuildSSG(dir, out)
}

func cmdBuildJS(args []string) error {
	dir := "."
	out := ""
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--help" || a == "-h":
			fmt.Println("usage: fusion build-js [appdir] [--out DIR]\n  transpile safe .ks subset to JS per-route (split/shake/minify analogue)")
			return nil
		case a == "--out" || a == "-o":
			if i+1 >= len(args) {
				return fmt.Errorf("usage: fusion build-js [appdir] [--out DIR]")
			}
			i++
			out = args[i]
		case strings.HasPrefix(a, "--out="):
			out = strings.TrimPrefix(a, "--out=")
		case strings.HasPrefix(a, "-"):
			return fmt.Errorf("unknown flag %q", a)
		default:
			if dir != "." {
				return fmt.Errorf("usage: fusion build-js [appdir] (single target only)")
			}
			dir = a
		}
	}
	return tools.BuildJS(dir, out)
}

func cmdPublish(args []string) error {
	libDir := "."
	reg := ""
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--help" || a == "-h":
			fmt.Println("usage: fusion publish [libdir] [--registry DIR]\n  build lib and publish .kslib to registry with sha256 + index")
			return nil
		case a == "--registry":
			if i+1 >= len(args) {
				return fmt.Errorf("usage: fusion publish [libdir] [--registry DIR]")
			}
			i++
			reg = args[i]
		case strings.HasPrefix(a, "--registry="):
			reg = strings.TrimPrefix(a, "--registry=")
		case strings.HasPrefix(a, "-"):
			return fmt.Errorf("unknown flag %q", a)
		default:
			if libDir != "." {
				return fmt.Errorf("usage: fusion publish [libdir] (single target only)")
			}
			libDir = a
		}
	}
	_, err := tools.Publish(libDir, reg)
	return err
}

func cmdPull(args []string) error {
	if len(args) == 0 {
		fmt.Println("usage: fusion pull <name[@spec]> [--out DIR]")
		return fmt.Errorf("missing package")
	}
	name := ""
	out := ""
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--help" || a == "-h":
			fmt.Println("usage: fusion pull <name[@spec]> [--out DIR]\n  fetch .kslib from registry with sha256 verification")
			return nil
		case a == "--out" || a == "-o":
			if i+1 >= len(args) {
				return fmt.Errorf("usage: fusion pull <name> [--out DIR]")
			}
			i++
			out = args[i]
		case strings.HasPrefix(a, "--out="):
			out = strings.TrimPrefix(a, "--out=")
		case strings.HasPrefix(a, "-"):
			return fmt.Errorf("unknown flag %q", a)
		default:
			if name != "" {
				return fmt.Errorf("usage: fusion pull <name> (single package only)")
			}
			name = a
		}
	}
	spec := "*"
	if j := strings.Index(name, "@"); j >= 0 {
		spec = name[j+1:]
		name = name[:j]
	}
	_, err := tools.Pull(name, spec, out)
	return err
}

func cmdYank(args []string) error {
	if len(args) == 0 {
		fmt.Println("usage: fusion yank <name[@version]> [--remove]")
		return fmt.Errorf("missing package")
	}
	name := ""
	remove := false
	for _, a := range args {
		switch {
		case a == "--help" || a == "-h":
			fmt.Println("usage: fusion yank <name[@version]> [--remove]\n  mark registry version yanked (or --remove to delete)")
			return nil
		case a == "--remove":
			remove = true
		case strings.HasPrefix(a, "-"):
			return fmt.Errorf("unknown flag %q", a)
		default:
			if name != "" {
				return fmt.Errorf("usage: fusion yank <name> (single only)")
			}
			name = a
		}
	}
	ver := ""
	if j := strings.Index(name, "@"); j >= 0 {
		ver = name[j+1:]
		name = name[:j]
	}
	return tools.Yank(name, ver, remove)
}

func cmdRegistry(args []string) error {
	for _, a := range args {
		if a == "--help" || a == "-h" {
			fmt.Println("usage: fusion registry\n  list registry packages (name, version, sha, yanked)")
			return nil
		}
	}
	for _, e := range tools.RegistryList() {
		flag := ""
		if e.Yanked {
			flag = " [yanked]"
		}
		fmt.Printf("%s %s %s%s\n", e.Name, e.Version, e.SHA256[:12], flag)
	}
	return nil
}
