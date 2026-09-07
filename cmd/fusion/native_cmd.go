package main

import (
	"fmt"
	"strings"

	"github.com/kswarrior/ks-fusion/internal/tools"
)

func cmdNative(args []string) error {
	src := ""
	out := ""
	target := ""
	strip := false
	emit := false
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--help" || a == "-h":
			fmt.Println("usage: fusion native <file.ks> [-o FILE] [--target OS/ARCH] [--strip] [--emit]\n  compile the native strict subset to real machine code via Go codegen (native-0.1).\n  subset: int/float/string/bool, let/assign/print/sleep, if/while/for-in-range/for-c,\n  typed funcs + closures, len(string). Outside it the transpiler says so —\n  run those files with the interpreter instead.\n  --emit prints the generated Go source without building.")
			return nil
		case a == "--strip":
			strip = true
		case a == "--emit":
			emit = true
		case a == "-o" || a == "--out":
			if i+1 >= len(args) {
				return fmt.Errorf("usage: fusion native <file.ks> [-o FILE]")
			}
			i++
			out = args[i]
		case strings.HasPrefix(a, "--out="):
			out = strings.TrimPrefix(a, "--out=")
		case a == "--target":
			if i+1 >= len(args) {
				return fmt.Errorf("usage: fusion native <file.ks> [--target OS/ARCH]")
			}
			i++
			target = args[i]
		case strings.HasPrefix(a, "--target="):
			target = strings.TrimPrefix(a, "--target=")
		case strings.HasPrefix(a, "-"):
			return fmt.Errorf("unknown flag %q (usage: fusion native <file.ks> [-o FILE] [--target OS/ARCH] [--strip] [--emit])", a)
		default:
			if src != "" {
				return fmt.Errorf("usage: fusion native <file.ks> (single file only)")
			}
			src = a
		}
	}
	if src == "" {
		return fmt.Errorf("usage: fusion native <file.ks> [-o FILE] [--target OS/ARCH] [--strip] [--emit]")
	}
	if !strings.HasSuffix(src, ".ks") {
		return fmt.Errorf("native needs a .ks file, got %q", src)
	}
	return tools.BuildNative(src, out, target, strip, emit)
}
