package backend

import (
	"testing"

	"github.com/kswarrior/ks-fusion/internal/frontend"
)

func mustParse(t *testing.T, src string) *frontend.Program {
	t.Helper()
	p, err := frontend.ParseSource(src, "test.ks")
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestRunBasic(t *testing.T) {
	p := mustParse(t, "let x = 1\nx = x + 2\nprint x\n")
	if err := Run(p); err != nil {
		t.Fatal(err)
	}
}

func TestRunConcurrency(t *testing.T) {
	p := mustParse(t, "go print \"a\"\ngo print \"b\"\nsleep 10\nprint \"done\"\n")
	if err := Run(p); err != nil {
		t.Fatal(err)
	}
}

func TestRunUnknownVar(t *testing.T) {
	p := mustParse(t, "print nope\n")
	if err := Run(p); err == nil {
		t.Fatal("want error for unknown variable")
	}
}
