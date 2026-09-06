package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/kswarrior/ks-fusion/internal/tools"
)

func cmdFmt(args []string) error {
	target := "."
	check := false
	for _, a := range args {
		switch {
		case a == "--check":
			check = true
		case a == "--help" || a == "-h":
			fmt.Println("usage: fusion fmt [target] [--check]\n  format .ks files (idempotent); --check lists dirty without writing")
			return nil
		case strings.HasPrefix(a, "-"):
			return fmt.Errorf("unknown flag %q (usage: fusion fmt [target] [--check])", a)
		default:
			if target != "." {
				return fmt.Errorf("usage: fusion fmt [target] (single target only)")
			}
			target = a
		}
	}
	dirty, changed, err := tools.FmtTarget(target, check)
	if err != nil {
		return err
	}
	if check {
		for _, d := range dirty {
			fmt.Println("dirty:", d)
		}
		if len(dirty) > 0 {
			return fmt.Errorf("fmt --check: %d files need formatting", len(dirty))
		}
		fmt.Println("fmt --check: clean")
		return nil
	}
	fmt.Printf("fmt ok: %d files formatted\n", changed)
	return nil
}

func cmdVet(args []string) error {
	target := "."
	denyWarns := false
	for _, a := range args {
		switch {
		case a == "--deny-warns":
			denyWarns = true
		case a == "--help" || a == "-h":
			fmt.Println("usage: fusion vet [target] [--deny-warns]\n  vet .ks: unused let, arity, unknown var, frontend env()")
			return nil
		case strings.HasPrefix(a, "-"):
			return fmt.Errorf("unknown flag %q (usage: fusion vet [target] [--deny-warns])", a)
		default:
			if target != "." {
				return fmt.Errorf("usage: fusion vet [target] (single target only)")
			}
			target = a
		}
	}
	issues, err := tools.VetTarget(target, denyWarns)
	if err != nil {
		return err
	}
	errs := 0
	warns := 0
	for _, is := range issues {
		fmt.Println(is.String())
		if is.IsError {
			errs++
		} else {
			warns++
		}
	}
	if errs > 0 {
		return fmt.Errorf("vet failed: %d errors, %d warnings", errs, warns)
	}
	if denyWarns && warns > 0 {
		return fmt.Errorf("vet failed: %d warnings (deny-warns)", warns)
	}
	fmt.Printf("vet ok: %d warnings, 0 errors\n", warns)
	return nil
}

func cmdDoc(args []string) error {
	target := "."
	out := ""
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--help" || a == "-h":
			fmt.Println("usage: fusion doc [target] [--out FILE]")
			return nil
		case a == "--out" || a == "-o":
			if i+1 >= len(args) {
				return fmt.Errorf("usage: fusion doc [target] [--out FILE]")
			}
			i++
			out = args[i]
		case strings.HasPrefix(a, "--out="):
			out = strings.TrimPrefix(a, "--out=")
		case strings.HasPrefix(a, "-"):
			return fmt.Errorf("unknown flag %q", a)
		default:
			if target != "." {
				return fmt.Errorf("usage: fusion doc [target] (single target only)")
			}
			target = a
		}
	}
	s, err := tools.DocTarget(target)
	if err != nil {
		return err
	}
	if out != "" {
		if err := os.WriteFile(out, []byte(s), 0o644); err != nil {
			return err
		}
		fmt.Println("doc written to", out)
		return nil
	}
	fmt.Print(s)
	return nil
}

func cmdCheck(args []string) error {
	target := "."
	for _, a := range args {
		switch {
		case a == "--help" || a == "-h":
			fmt.Println("usage: fusion check [target]\n  strict check: parse + arity + :type + is narrowing (vet errors)")
			return nil
		case strings.HasPrefix(a, "-"):
			return fmt.Errorf("unknown flag %q", a)
		default:
			if target != "." {
				return fmt.Errorf("usage: fusion check [target] (single target only)")
			}
			target = a
		}
	}
	errs, err := tools.CheckTarget(target)
	if err != nil {
		return err
	}
	for _, is := range errs {
		fmt.Println(is.String())
	}
	if len(errs) > 0 {
		return fmt.Errorf("check failed: %d errors", len(errs))
	}
	fmt.Println("check ok")
	return nil
}

func cmdRepl(args []string) error {
	for _, a := range args {
		if a == "--help" || a == "-h" {
			fmt.Println("usage: fusion repl\n  interactive .ks (history + multiline via brace balance)")
			return nil
		}
		if strings.HasPrefix(a, "-") {
			return fmt.Errorf("unknown flag %q", a)
		}
	}
	return tools.Repl()
}

func cmdBench(args []string) error {
	target := "."
	n := 20
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--help" || a == "-h":
			fmt.Println("usage: fusion bench [target] [--n N]\n  run .ks N times and report timing")
			return nil
		case a == "--n" || a == "-n":
			if i+1 >= len(args) {
				return fmt.Errorf("usage: fusion bench [target] [--n N]")
			}
			i++
			v, err := strconv.Atoi(args[i])
			if err != nil || v <= 0 {
				return fmt.Errorf("bad --n %q", args[i])
			}
			n = v
		case strings.HasPrefix(a, "--n="):
			v, err := strconv.Atoi(strings.TrimPrefix(a, "--n="))
			if err != nil || v <= 0 {
				return fmt.Errorf("bad --n %q", a)
			}
			n = v
		case strings.HasPrefix(a, "-"):
			return fmt.Errorf("unknown flag %q", a)
		default:
			if target != "." {
				return fmt.Errorf("usage: fusion bench [target] (single target only)")
			}
			target = a
		}
	}
	return tools.Bench(target, n)
}
