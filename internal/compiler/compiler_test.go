package compiler

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func mustCompile(t *testing.T, src string) *Bundle {
	t.Helper()
	b, err := CompileSource(src, "test.ks")
	if err != nil {
		t.Fatalf("compile %q failed: %v", src, err)
	}
	return b
}

func mustRun(t *testing.T, src string) {
	t.Helper()
	if err := RunSource(src, "test.ks"); err != nil {
		t.Fatalf("run %q failed: %v", src, err)
	}
}

func mustFailCompile(t *testing.T, src string) {
	t.Helper()
	if _, err := CompileSource(src, "test.ks"); err == nil {
		t.Fatalf("want compile error for %q", src)
	}
}

func mustFailRun(t *testing.T, src string) {
	t.Helper()
	b, err := CompileSource(src, "test.ks")
	if err != nil {
		return // compile-time rejection also counts as failure
	}
	if err := Run(b); err == nil {
		t.Fatalf("want runtime error for %q", src)
	}
}

func TestCompileArith(t *testing.T) {
	mustRun(t, "assert(1 + 2 * 3 == 7)\n")
	mustRun(t, "assert((1 + 2) * 3 == 9)\n")
	mustRun(t, "assert(7 / 2 == 3.5)\n")
	mustRun(t, "assert(7 % 3 == 1)\n")
	mustRun(t, "assert(2 ** 10 == 1024)\n")
	mustRun(t, "assert(-(3 + 2) == -5)\n")
	mustRun(t, "assert(\"hi \" + 42 == \"hi 42\")\n")
	mustRun(t, "assert([1] + [2, 3] == [1, 2, 3])\n")
	mustRun(t, "assert(1 < 2 and 2 < 3)\n")
	mustRun(t, "assert(not false)\nassert(!false)\n")
	mustRun(t, "assert(2 in [1, 2])\nassert(\"ell\" in \"hello\")\n")
	mustFailRun(t, "assert(1 / 0 == 0)\n")
}

func TestCompileVars(t *testing.T) {
	mustRun(t, "let x = 1\nx = x + 2\nassert(x == 3)\n")
	mustRun(t, "let x = 1\nx += 4\nx -= 1\nx *= 3\nassert(x == 12)\n")
	mustRun(t, "let y = 7\ny /= 2\nassert(y == 3.5)\n")
	mustRun(t, "let z = 7\nz %= 3\nassert(z == 1)\n")
	mustRun(t, "let a = [1, 2]\na[0] = 9\nassert(a == [9, 2])\n")
	mustRun(t, "let m = {a: 1}\nm.b = 2\nassert(m == {a: 1, b: 2})\n")
	mustFailCompile(t, "y = 2\n")
}

func TestCompileFuncRecursion(t *testing.T) {
	mustRun(t, "func fact(n) {\n if n <= 1 { return 1 }\n return n * fact(n - 1)\n}\nassert(fact(5) == 120)\n")
	mustRun(t, "func add(a, b) { return a + b }\nassert(add(2, 3) == 5)\n")
	mustRun(t, "func fib(n) {\n if n < 2 { return n }\n return fib(n-1) + fib(n-2)\n}\nassert(fib(10) == 55)\n")
	mustRun(t, "let double = func(x) { return x * 2 }\nassert(double(21) == 42)\n")
	mustFailRun(t, "func f(a) { return a }\nf(1, 2)\n")
}

func TestCompileControlFlow(t *testing.T) {
	mustRun(t, "let x = 2\nlet r = 0\nif x == 1 { r = 1 } else if x == 2 { r = 2 } else { r = 3 }\nassert(r == 2)\n")
	mustRun(t, "let n = 0\nwhile n < 5 { n = n + 1 }\nassert(n == 5)\n")
	mustRun(t, "let s = 0\nfor i in range(5) { s = s + i }\nassert(s == 10)\n")
	mustRun(t, "let t = 0\nfor v in [1, 2, 3] { t = t + v }\nassert(t == 6)\n")
	mustRun(t, "let k = \"\"\nfor ch in \"hey\" { k = k + ch }\nassert(k == \"hey\")\n")
	mustRun(t, "let m = {b: 2, a: 1}\nlet s = \"\"\nfor k, v in m { s = s + k }\nassert(s == \"ab\")\n")
	mustRun(t, "let s = 0\nfor i = 0; i < 5; i = i + 1 { s = s + i }\nassert(s == 10)\n")
	mustRun(t, "let s = 0\nfor i in range(10) {\n if i == 2 { continue }\n if i == 4 { break }\n s = s + 1\n}\nassert(s == 3)\n")
	mustRun(t, "let i = 0\nfor i = 0; i < 10; i = i + 1 {\n if i == 3 { break }\n}\nassert(i == 3)\n")
}

func TestCompileCollections(t *testing.T) {
	mustRun(t, "assert(len([1, 2, 3]) == 3)\nassert(len(\"hey\") == 3)\n")
	mustRun(t, "let a = [10, 20]\nassert(a[1] == 20)\nassert(a[0] + a[1] == 30)\n")
	mustRun(t, "let m = {name: \"ada\"}\nassert(m.name == \"ada\")\nassert(m[\"name\"] == \"ada\")\n")
	mustRun(t, "assert(range(3) == [0, 1, 2])\nassert(range(1, 4) == [1, 2, 3])\n")
	mustRun(t, "assert(type(1) == \"int\")\nassert(type(\"s\") == \"string\")\nassert(str(7) == \"7\")\n")
	mustFailRun(t, "let a = [1]\nassert(a[9] == 0)\n")
}

func TestCompileUnsupported(t *testing.T) {
	mustFailCompile(t, "go print \"hi\"\n")
	mustFailCompile(t, "import \"lib.ks\"\n")
	mustFailCompile(t, "try {\n print 1\n} catch e {\n print e\n}\n")
	mustFailCompile(t, "func f() {\n defer print 1\n}\n")
	mustFailCompile(t, "sleep 10\n")
	// v0.2 covers switch/slices/is/??/?./typed — these must compile now.
	mustRun(t, "switch 1 {\n case 1 { print 1 }\n}\n")
	mustRun(t, "let a = [1,2]\nassert(a[0:1] == [1])\n")
	mustRun(t, "assert(1 is int)\nassert(nil?.x ?? 7 == 7)\n")
	mustRun(t, "func add(a: int, b: int): int { return a + b }\nassert(add(1, 2) == 3)\n")
}

func TestCompileSaveLoadRoundtrip(t *testing.T) {
	b := mustCompile(t, "func fib(n) {\n if n < 2 { return n }\n return fib(n-1)+fib(n-2)\n}\nassert(fib(10) == 55)\n")
	dir := t.TempDir()
	path := filepath.Join(dir, "fib.ksb")
	if err := Save(b, path); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := Run(loaded); err != nil {
		t.Fatalf("run loaded bundle failed: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Unmarshal(raw, path); err != nil {
		t.Fatal(err)
	}
	if _, err := Unmarshal([]byte(`{"format":"nope"}`), path); err == nil {
		t.Fatal("want error for bad format tag")
	}
}

func TestCompileDisassemble(t *testing.T) {
	b := mustCompile(t, "let x = 1 + 2\nprint x\n")
	out := Disassemble(b)
	if !strings.Contains(out, "Add") || !strings.Contains(out, "Print") {
		t.Fatalf("disassembly missing ops:\n%s", out)
	}
}

func TestCompileV02Slices(t *testing.T) {
	mustRun(t, "assert([1,2,3,4][1:3] == [2, 3])\n")
	mustRun(t, "assert([1,2,3][:2] == [1, 2])\nassert([1,2,3][1:] == [2, 3])\n")
	mustRun(t, "assert(\"hello\"[1:3] == \"el\")\nassert(\"hello\"[-2:] == \"lo\")\n")
	mustFailRun(t, "let x = 1\nassert(x[0:1] == 1)\n")
}

func TestCompileV02IsCoalesceSafe(t *testing.T) {
	mustRun(t, "assert(1 is int)\nassert(1 is \"int\")\nassert(\"a\" is not int)\n")
	mustRun(t, "assert(1 is number and 2.5 is number)\n")
	mustRun(t, "assert((nil ?? 7) == 7)\nassert((5 ?? 7) == 5)\n")
	mustRun(t, "let m = {a: 1}\nassert(m?.a == 1)\nassert(m?.missing == nil)\n")
	mustRun(t, "let a = [1, 2]\nassert(a?.[9] ?? 99 == 99)\n")
}

func TestCompileV02Typed(t *testing.T) {
	mustRun(t, "let x: int = 5\nassert(x == 5)\n")
	mustRun(t, "func add(a: int, b: int): int { return a + b }\nassert(add(2, 3) == 5)\n")
	mustFailRun(t, "let x: int = \"nope\"\n")
	mustFailRun(t, "func add(a: int): int { return a }\nprint add(\"bad\")\n")
}

func TestCompileV02Switch(t *testing.T) {
	mustRun(t, "let r = \"\"\nswitch 2 {\n case 1 { r = \"one\" }\n case 2, 3 { r = \"few\" }\n default { r = \"many\" }\n}\nassert(r == \"few\")\n")
	mustRun(t, "let r = \"\"\nswitch 99 {\n case 1 { r = \"one\" }\n default { r = \"dflt\" }\n}\nassert(r == \"dflt\")\n")
	mustRun(t, "for i in range(3) {\n switch i {\n case 1 { break }\n default { continue }\n }\n assert(i == 1)\n}\n")
}
