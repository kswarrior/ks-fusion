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
	// try/catch compiles since v0.3 (see TestCompileTryCatch); try/finally
	// stays interpreter-only.
	mustFailCompile(t, "try {\n print 1\n} finally {\n print 2\n}\n")
	mustFailCompile(t, "func f() {\n defer print 1\n}\n")
	// sleep compiles since v0.3 (see TestCompileSleep).
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
	// nominal struct/enum annotations: VM skips the check (interpreter
	// validates); maps must not fail with "wants User, got map".
	mustRun(t, "let u: User = {name: \"a\"}\nassert(u.name == \"a\")\n")
}

func TestCompileTryCatch(t *testing.T) {
	// caught error binds its message; success skips the handler.
	mustRun(t, "let r = \"\"\ntry {\n r = \"ok\"\n} catch e {\n r = \"bad:\" + e\n}\nassert(r == \"ok\")\n")
	mustRun(t, "let r = \"\"\ntry {\n print nope\n} catch e {\n r = \"caught\"\n}\nassert(r == \"caught\")\n")
	mustRun(t, "let r = \"\"\ntry {\n assert(false, \"boom\")\n} catch e {\n r = e\n}\nassert(r == \"assert failed: boom\")\n")
	// catch without a variable.
	mustRun(t, "let r = 0\ntry {\n print nope\n} catch {\n r = 1\n}\nassert(r == 1)\n")
	// bare try without catch/finally is transparent.
	mustRun(t, "let r = 0\ntry {\n r = 7\n}\nassert(r == 7)\n")
	mustFailRun(t, "try {\n print nope\n}\n")
	// errors inside calls unwind through frames to the handler.
	mustRun(t, "func f() {\n print nope\n}\nlet r = \"\"\ntry {\n f()\n} catch e {\n r = \"caught\"\n}\nassert(r == \"caught\")\n")
	// errors in the handler propagate (no double-catch).
	mustFailRun(t, "try {\n print nope\n} catch e {\n print alsobad\n}\n")
	// nested trys: inner catches inner failures only.
	mustRun(t, "let r = \"\"\ntry {\n try {\n print nope\n } catch e {\n r = \"inner\"\n }\n print alsobad\n} catch e {\n r = r + \"+outer\"\n}\nassert(r == \"inner+outer\")\n")
	// return inside try never triggers the catch (mirrors interpreter).
	mustRun(t, "func f() {\n try {\n return 42\n } catch e {\n return -1\n }\n}\nassert(f() == 42)\n")
	// break/continue out of a try inside a loop pop the record cleanly.
	mustRun(t, "let s = 0\nfor i in range(5) {\n try {\n if i == 2 { break }\n s = s + 1\n } catch e {\n s = 99\n }\n}\nassert(s == 2)\n")
	mustRun(t, "let s = 0\nfor i in range(5) {\n try {\n if i < 3 { continue }\n s = s + 1\n } catch e {\n s = 99\n }\n}\nassert(s == 2)\n")
	// a caught error per iteration does not poison later iterations.
	mustRun(t, "let s = 0\nfor i in range(4) {\n try {\n if i == 1 { print nope }\n s = s + 1\n } catch e {\n s = s + 10\n }\n}\nassert(s == 13)\n")
	// try inside a function called from a loop.
	mustRun(t, "func g(x) {\n try {\n if x == 1 { print nope }\n return x * 10\n } catch e {\n return -1\n }\n}\nassert(g(2) == 20)\nassert(g(1) == -1)\nassert(g(3) == 30)\n")
	// finally forms stay interpreter-only with a clear error.
	mustFailCompile(t, "try {\n print 1\n} catch e {\n print e\n} finally {\n print 2\n}\n")
}

func TestCompileSleep(t *testing.T) {
	mustRun(t, "sleep 1\nsleep(1)\n")
	mustRun(t, "let s = 0\nfor i in range(3) {\n sleep 1\n s = s + i\n}\nassert(s == 3)\n")
	mustFailRun(t, "sleep(-1)\n")
	mustFailRun(t, "sleep(\"x\")\n")
}

func TestCompileV02Switch(t *testing.T) {
	mustRun(t, "let r = \"\"\nswitch 2 {\n case 1 { r = \"one\" }\n case 2, 3 { r = \"few\" }\n default { r = \"dflt\" }\n}\nassert(r == \"few\")\n")
	mustRun(t, "let r = \"\"\nswitch 99 {\n case 1 { r = \"one\" }\n default { r = \"dflt\" }\n}\nassert(r == \"dflt\")\n")
	mustRun(t, "for i in range(3) {\n switch i {\n case 1 { break }\n default { continue }\n }\n assert(i == 1)\n}\n")
}

func TestCompileRangeFastPath(t *testing.T) {
	// 1-arg and 2-arg forms, incl. variable bounds (evaluated once).
	mustRun(t, "let s = 0\nfor i in range(10000) { s = s + i }\nassert(s == 49995000)\n")
	mustRun(t, "let s = 0\nfor i in range(3, 7) { s = s + i }\nassert(s == 18)\n")
	mustRun(t, "let n = 5\nlet s = 0\nfor i in range(n) { s = s + 1 }\nassert(s == 5)\n")
	mustRun(t, "let a = 2\nlet b = 5\nlet s = 0\nfor i in range(a, b) { s = s + i }\nassert(s == 9)\n")
	// two-var form: k = 0-based index, v = value (mirrors interpreter).
	mustRun(t, "let sk = 0\nlet sv = 0\nfor k, v in range(4) { sk = sk + k\n sv = sv + v }\nassert(sk == 6)\nassert(sv == 6)\n")
	mustRun(t, "let sk = 0\nlet sv = 0\nfor k, v in range(2, 5) { sk = sk + k\n sv = sv + v }\nassert(sk == 3)\nassert(sv == 9)\n")
	// empty ranges run zero times.
	mustRun(t, "let s = 0\nfor i in range(0) { s = 99 }\nassert(s == 0)\n")
	mustRun(t, "let s = 0\nfor i in range(5, 2) { s = 99 }\nassert(s == 0)\n")
	mustRun(t, "let s = 0\nfor i in range(-3) { s = 99 }\nassert(s == 0)\n")
	// break/continue/nesting/shadowing match the generic path.
	mustRun(t, "let s = 0\nfor i in range(10) {\n if i == 2 { continue }\n if i == 4 { break }\n s = s + 1\n}\nassert(s == 3)\n")
	mustRun(t, "let s = 0\nfor i in range(3) {\n for j in range(3) { s = s + 1 }\n}\nassert(s == 9)\n")
	mustRun(t, "let i = 99\nfor i in range(3) { }\nassert(i == 99)\n")
	// assigning the loop var inside the body does not corrupt iteration
	// (counter lives in a hidden slot, as in the generic path).
	mustRun(t, "let s = \"\"\nfor i in range(5) {\n s = s + str(i) + \",\"\n i = 100\n}\nassert(s == \"0,1,2,3,4,\")\n")
	// 3-arg range keeps the generic path (same values, incl. step).
	mustRun(t, "let s = 0\nfor i in range(0, 10, 3) { s = s + i }\nassert(s == 18)\n")
	// non-int bounds are runtime errors in both engines.
	mustFailRun(t, "for i in range(\"x\") { print i }\n")
	mustFailRun(t, "for i in range(2.5) { print i }\n")
	mustFailRun(t, "for i in range(nil) { print i }\n")
}
