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
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--help" || a == "-h":
			fmt.Println("usage: fusion run-web [appdir] [--port N]\n  SSR frontend/ as HTML+JSON with hot-reload hint (dev server)")
			return nil
		case a == "--port" || a == "-p":
			if i+1 >= len(args) {
				return fmt.Errorf("usage: fusion run-web [appdir] [--port N]")
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
	return tools.RunWeb(dir, port)
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
