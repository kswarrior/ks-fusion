package native

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kswarrior/ks-fusion/internal/backend"
	"github.com/kswarrior/ks-fusion/internal/frontend"
)

// interp runs src with the tree-walk interpreter and returns stdout
// (captured by swapping os.Stdout around backend.Run).
func interp(t *testing.T, src string) string {
	t.Helper()
	prog, err := frontend.ParseSource(src, "<test>")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = w
	runErr := backend.Run(prog)
	w.Close()
	os.Stdout = old
	out, _ := io.ReadAll(r)
	if runErr != nil {
		t.Fatalf("interp: %v", runErr)
	}
	return string(out)
}

// nativeOut transpiles src to Go, builds it, runs it, returns stdout.
// Skips when no Go toolchain is available.
func nativeOut(t *testing.T, src string) string {
	t.Helper()
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("no go toolchain")
	}
	goSrc, err := TranspileSource(src, "<test>")
	if err != nil {
		t.Fatalf("transpile: %v", err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(goSrc), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module tmpnat\n\ngo 1.24\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "prog")
	cmd := exec.Command("go", "build", "-o", bin, ".")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	cmd = exec.Command(bin)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run: %v\n%s", err, out)
	}
	return string(out)
}

func TestNativeParity(t *testing.T) {
	cases := map[string]string{
		"arith":   "print 1 + 2 * 3\nprint (1 + 2) * 3\nprint 7 / 2\nprint 7 % 3\nprint 2 ** 10\n",
		"float":   "let x = 1.5\nprint x + 2\nprint x * 2\nx += 0.5\nprint x\n",
		"string":  "let s = \"hi \" + \"there\"\nprint s, len(s)\nprint \"a\" + \"b\"\n",
		"bool":    "print true and false\nprint true or false\nprint !false\nprint 1 < 2\nprint 2 == 2\nprint \"a\" != \"b\"\n",
		"if":      "let x = 10\nif x > 5 {\n print \"big\"\n} else if x == 5 {\n print \"five\"\n} else {\n print \"small\"\n}\n",
		"while":   "let i = 0\nlet t = 0\nwhile i < 5 {\n t = t + i\n i = i + 1\n}\nprint t\n",
		"forin":   "let t = 0\nfor i in range(5) {\n t = t + i\n}\nprint t\nfor i in range(2, 10, 3) {\n print i\n}\n",
		"forc":    "let t = 0\nfor i = 0; i < 5; i = i + 1 {\n t += i\n}\nprint t\n",
		"fib":     "func fib(n: int): int {\n if n < 2 {\n return n\n }\n return fib(n - 1) + fib(n - 2)\n}\nprint fib(10)\n",
		"closure": "let double = func(x: int): int {\n return x * 2\n}\nprint double(21)\n",
		"nested":  "func outer(a: int): int {\n func inner(b: int): int {\n return b * 2\n }\n return inner(a) + 1\n}\nprint outer(20)\n",
		"mixdiv":  "let a: float = 7\nprint a / 2\nlet b = 1\nb = 2\nprint b\n",
		"sleep0":  "sleep 1\nprint \"woke\"\n",
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			want := interp(t, src)
			got := nativeOut(t, src)
			if got != want {
				t.Fatalf("mismatch:\ninterp: %q\nnative: %q", want, got)
			}
		})
	}
}

func TestNativeRejects(t *testing.T) {
	cases := []string{
		"let c = chan(1)\n",
		"go print 1\n",
		"import \"x.ks\"\n",
		"try {\n print 1\n} catch e {\n print e\n}\n",
		"switch 1 {\n case 1 {\n print 1\n }\n}\n",
		"let m = {a: 1}\nprint m\n",
		"let a = [1, 2]\nprint a\n",
		"print nil\n",
		"if 1 {\n print 1\n}\n",
		"func f(x) {\n return x\n}\nprint f(1)\n",
		"func f(): int {\n go print 1\n}\n",
		"print 1 ?? 2\n",
		"print 1 is int\n",
		"print 1 in [1]\n",
	}
	for _, src := range cases {
		if _, err := TranspileSource(src, "<test>"); err == nil {
			t.Fatalf("want reject for %q", src)
		} else if !strings.Contains(err.Error(), "interpreter") && !strings.Contains(err.Error(), "native") {
			t.Fatalf("error should point at interpreter/subset, got: %v", err)
		}
	}
}
