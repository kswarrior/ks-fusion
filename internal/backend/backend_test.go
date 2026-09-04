package backend

import (
	"os"
	"path/filepath"
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

func mustRun(t *testing.T, src string) {
	t.Helper()
	if err := Run(mustParse(t, src)); err != nil {
		t.Fatal(err)
	}
}

func mustFail(t *testing.T, src string) {
	t.Helper()
	if err := Run(mustParse(t, src)); err == nil {
		t.Fatalf("want error for %q", src)
	}
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

func TestRunFuncRecursion(t *testing.T) {
	mustRun(t, "func fact(n) {\n if n <= 1 {\n return 1\n }\n return n * fact(n - 1)\n}\nlet r = fact(5)\nassert(r == 120)\n")
	mustRun(t, "func add(a, b) {\n return a + b\n}\nassert(add(2, 3) == 5)\n")
}

func TestRunClosures(t *testing.T) {
	mustRun(t, "func counter() {\n let c = 0\n return func() {\n c = c + 1\n return c\n }\n}\nlet n = counter()\nassert(n() == 1)\nassert(n() == 2)\n")
}

func TestRunControlFlow(t *testing.T) {
	mustRun(t, "let x = 2\nif x == 1 {\n x = 10\n} else if x == 2 {\n x = 20\n} else {\n x = 30\n}\nassert(x == 20)\n")
	mustRun(t, "let s = 0\nfor i in range(5) {\n s = s + i\n}\nassert(s == 10)\n")
	mustRun(t, "let s = 0\nfor i = 0; i < 5; i = i + 1 {\n if i == 2 {\n continue\n }\n if i == 4 {\n break\n }\n s = s + 1\n}\nassert(s == 3)\n")
	mustRun(t, "let n = 0\nwhile n < 3 {\n n = n + 1\n}\nassert(n == 3)\n")
	mustRun(t, "let m = {b: 2, a: 1}\nlet ks = keys(m)\nassert(ks[0] == \"a\")\nfor k, v in m {\n assert(has(m, k))\n}\n")
}

func TestRunArraysMaps(t *testing.T) {
	mustRun(t, "let a = [1, 2, 3]\nassert(len(a) == 3)\nassert(a[0] == 1)\na[0] = 9\nassert(a[0] == 9)\n push(a, 4)\nassert(len(a) == 4)\n assert(pop(a) == 4)\nassert([1] + [2] == [1, 2])\n")
	mustRun(t, "let m = {name: \"a\"}\nassert(m.name == \"a\")\nm.age = 3\nassert(m[\"age\"] == 3)\nassert(has(m, \"age\"))\n")
	mustRun(t, "x = 1\n".Replace("x = 1", "let x = 1\nx += 4\nassert(x == 5)"))
}

func TestRunBuiltins(t *testing.T) {
	mustRun(t, "assert(len(\"hi\") == 2)\nassert(len([1,2]) == 2)\nassert(type(1) == \"int\")\nassert(type(1.5) == \"float\")\nassert(type(\"s\") == \"string\")\nassert(int(\"42\") == 42)\nassert(float(\"2.5\") == 2.5)\nassert(str(7) == \"7\")\nassert(range(3) == [0, 1, 2])\n")
	mustRun(t, "sleep 1\nsleep(1)\n")
	mustFail(t, "assert(false, \"boom\")\n")
}

func TestRunChannels(t *testing.T) {
	mustRun(t, "let c = chan(1)\nsend(c, 42)\nassert(recv(c) == 42)\nclose(c)\n")
	mustRun(t, "let c = chan(0)\ngo send(c, 7)\nassert(recv(c) == 7)\n")
}

func TestRunForInGoCapture(t *testing.T) {
	mustRun(t, "let c = chan(10)\nfor i in range(5) {\n go send(c, i)\n}\nsleep 50\nlet s = 0\nfor i in range(5) {\n s = s + recv(c)\n}\nassert(s == 10)\n")
}

func TestRunErrors(t *testing.T) {
	mustFail(t, "print 1 / 0\n")
	mustFail(t, "let a = [1]\nprint a[9]\n")
	mustFail(t, "let x = 1\nprint x()\n")
	mustFail(t, "func f(a) {\n print a\n}\nf(1, 2)\n")
	mustFail(t, "let x = 1\n y = 2\n")
	mustFail(t, "for x in 1 {\n print x\n}\n")
	mustFail(t, "error(\"boom\")\n")
}

func TestRunImport(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "lib.ks"), []byte("func inc(x) {\n return x + 1\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	p, err := frontend.ParseSource("import \"lib.ks\"\nassert(inc(1) == 2)\n", filepath.Join(dir, "main.ks"))
	if err != nil {
		t.Fatal(err)
	}
	if err := RunWithDir(p, dir); err != nil {
		t.Fatal(err)
	}
}
