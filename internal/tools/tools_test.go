package tools

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFmtIdempotent(t *testing.T) {
	src := "let x=1\nif x>0{\nprint x\n}\n"
	f1 := FormatSource(src)
	f2 := FormatSource(f1)
	if f1 != f2 {
		t.Fatalf("fmt not idempotent:\n%q\n%q", f1, f2)
	}
}

func TestFmtCheckClean(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.ks")
	src := FormatSource("let x = 1\nprint x\n")
	if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	dirty, _, err := FmtTarget(dir, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(dirty) != 0 {
		t.Fatalf("want clean, got %v", dirty)
	}
}

func TestVetDetects(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "bad.ks")
	if err := os.WriteFile(p, []byte("let unused_xyz = 1\nprint 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	issues, err := VetTarget(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, is := range issues {
		if is.Rule == "unused-let" {
			found = true
		}
	}
	if !found {
		t.Fatalf("want unused-let, got %v", issues)
	}
	// arity
	p2 := filepath.Join(dir, "arity.ks")
	os.WriteFile(p2, []byte("func f(a, b) { return a }\nf(1)\n"), 0o644)
	issues, _ = VetTarget(dir, false)
	foundArity := false
	for _, is := range issues {
		if is.Rule == "arity" {
			foundArity = true
		}
	}
	if !foundArity {
		t.Fatalf("want arity error, got %v", issues)
	}
}

func TestSemver(t *testing.T) {
	cases := []struct {
		ver, spec string
		want      bool
	}{
		{"0.1.0", "0.1.0", true},
		{"0.1.1", "0.1.0", false},
		{"0.1.5", "^0.1.0", true},
		{"0.2.0", "^0.1.0", false},
		{"1.2.5", "~1.2.0", true},
		{"1.3.0", "~1.2.0", false},
		{"1.0.0", ">=0.5.0", true},
		{"0.1.0", "*", true},
		{"1.2.3", ">=1.0.0, <2.0.0", true},
	}
	for _, c := range cases {
		if got := satisfiesSemver(c.ver, c.spec); got != c.want {
			t.Fatalf("satisfiesSemver(%q,%q)=%v want %v", c.ver, c.spec, got, c.want)
		}
	}
}

func TestDocGenerates(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.ks"), []byte("# hello\nfunc greet(name) { return name }\n"), 0o644)
	s, err := DocTarget(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(s) == 0 || !contains(s, "greet") {
		t.Fatalf("want docs with greet, got %q", s)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}
