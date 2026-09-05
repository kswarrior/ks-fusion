package backend

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kswarrior/ks-fusion/internal/config"
	"github.com/kswarrior/ks-fusion/internal/frontend"
	kslib "github.com/kswarrior/ks-fusion/internal/lib"
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
	mustRun(t, "let x = 1\nx += 4\nassert(x == 5)\n")
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

func TestRunLibImport(t *testing.T) {
	// Build a real .kslib bundle into <appdir>/test-releases, then
	// import it by bare library name like `import "mylib"`.
	appDir := t.TempDir()
	libDir := filepath.Join(appDir, "mylib")
	src := "func double(x) {\n return x * 2\n}\nlet magic = 7\n"
	if err := os.MkdirAll(filepath.Join(libDir, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	toml := "[package]\nname = \"mylib\"\nversion = \"0.2.0\"\ntype = \"lib\"\n"
	if err := os.WriteFile(filepath.Join(libDir, "fusion.toml"), []byte(toml), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(libDir, "src", "lib.ks"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(libDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := kslib.Build(cfg, filepath.Join(appDir, "test-releases")); err != nil {
		t.Fatal(err)
	}
	p, err := frontend.ParseSource("import \"mylib\"\nassert(double(21) == 42)\nassert(magic == 7)\n", filepath.Join(appDir, "main.ks"))
	if err != nil {
		t.Fatal(err)
	}
	if err := RunWithDir(p, appDir); err != nil {
		t.Fatal(err)
	}
	// Unknown lib must fail with a helpful error.
	q, err := frontend.ParseSource("import \"nope\"\n", filepath.Join(appDir, "main.ks"))
	if err != nil {
		t.Fatal(err)
	}
	if err := RunWithDir(q, appDir); err == nil {
		t.Fatal("want error for unknown library")
	}
}

func TestRunPowInSlice(t *testing.T) {
	mustRun(t, "assert(2 ** 10 == 1024)\nassert(2 ** 3 ** 2 == 512)\nassert(-2 ** 2 == -4)\nassert(4 ** 0.5 == 2)\n")
	mustRun(t, "assert(2 in [1, 2, 3])\nassert(\"ell\" in \"hello\")\nassert(\"a\" in {a: 1})\nassert(!(9 in [1, 2]))\n")
	mustRun(t, "assert([1,2,3,4][1:3] == [2, 3])\nassert([1,2,3][:2] == [1, 2])\nassert([1,2,3][1:] == [2, 3])\nassert(\"hello\"[1:3] == \"el\")\nassert(\"hello\"[-2:] == \"lo\")\nassert([1,2,3][:] == [1, 2, 3])\n")
	mustFail(t, "print 1 ** \"a\"\n")
	mustFail(t, "print 1 in 2\n")
	mustFail(t, "print 1[0:2]\n")
}

func TestRunTryCatchFinally(t *testing.T) {
	mustRun(t, "try {\n error(\"boom\")\n} catch e {\n assert(e == \"boom\")\n}\n")
	mustRun(t, "let seen = \"\"\ntry {\n error(\"x\")\n} catch e {\n seen = e\n} finally {\n seen = seen + \"!\"\n}\nassert(seen == \"x!\")\n")
	mustRun(t, "try {\n let x = 1 / 0\n} catch e {\n assert(true)\n}\n")
	mustRun(t, "let f = []\ntry {\n f = [1]\n} finally {\n f = [2]\n}\nassert(f == [2])\n")
	mustRun(t, "print \"ok\"\ntry {\n print \"fine\"\n} catch e {\n error(\"must not catch\")\n}\n")
	// return still propagates (finally runs)
	mustRun(t, "func f() {\n try {\n return 1\n } finally {\n print \"cleanup\"\n }\n}\nassert(f() == 1)\n")
	// control flow is not caught
	mustRun(t, "for i in range(3) {\n try {\n break\n } catch e {\n error(\"must not catch break\")\n }\n}\n")
	// uncaught error still propagates
	mustFail(t, "try {\n error(\"nope\")\n} catch e {\n error(\"worse\")\n}\n")
	mustFail(t, "try {\n error(\"nope\")\n} finally {\n print 1\n}\n")
}

func TestRunSwitch(t *testing.T) {
	mustRun(t, "let x = 2\nlet r = \"\"\nswitch x {\n case 1 { r = \"one\" }\n case 2, 3 { r = \"few\" }\n default { r = \"many\" }\n}\nassert(r == \"few\")\n")
	mustRun(t, "let r = \"\"\nswitch 99 {\n case 1 { r = \"one\" }\n default { r = \"dflt\" }\n}\nassert(r == \"dflt\")\n")
	mustRun(t, "let r = \"\"\nswitch 99 {\n case 1 { r = \"one\" }\n}\nassert(r == \"\")\n")
	mustRun(t, "for i in range(3) {\n switch i {\n case 1 { break }\n default { continue }\n }\n assert(i == 1)\n}\n")
	// default must be last: this source must not run (parse error counts).
	if p, err := frontend.ParseSource("switch 1 {\n default { print 1 }\n case 2 { print 2 }\n}\n", "test.ks"); err == nil {
		if err := Run(p); err == nil {
			t.Fatal("want error for default-before-case switch")
		}
	}
}

func TestRunDefer(t *testing.T) {
	mustRun(t, "func f() {\n defer print \"a\"\n print \"b\"\n return 7\n}\nassert(f() == 7)\n")
	mustRun(t, "let log = []\nfunc f() {\n defer push(log, \"second\")\n push(log, \"first\")\n}\nf()\nassert(log == [\"first\", \"second\"])\n")
	mustRun(t, "func g() {\n defer print \"cleanup\"\n error(\"fail\")\n}\ntry {\n g()\n} catch e {\n assert(e == \"fail\")\n}\n")
	mustRun(t, "func h() {\n let c = chan(1)\n defer close(c)\n send(c, 1)\n assert(recv(c) == 1)\n}\nh()\n")
	mustFail(t, "defer print \"top\"\n")
}

func TestRunStdlib(t *testing.T) {
	mustRun(t, "assert(bool(\"\") == false)\nassert(bool(\"x\") == true)\nassert(chr(65) == \"A\")\nassert(ord(\"A\") == 65)\nassert(hex(255) == \"0xff\")\n")
	mustRun(t, "assert(split(\"a,b\", \",\") == [\"a\", \"b\"])\nassert(join([\"a\",\"b\"], \"-\") == \"a-b\")\nassert(upper(\"hi\") == \"HI\")\nassert(lower(\"HI\") == \"hi\")\nassert(trim(\"  x  \") == \"x\")\nassert(contains(\"hello\", \"ell\"))\nassert(starts_with(\"hi\", \"h\"))\nassert(ends_with(\"hi\", \"i\"))\nassert(replace(\"aaa\", \"a\", \"b\") == \"bbb\")\nassert(substr(\"hello\", 1, 3) == \"ell\")\nassert(index_of(\"hello\", \"ll\") == 2)\nassert(repeat(\"ab\", 3) == \"ababab\")\n")
	mustRun(t, "let a = [3, 1, 2]\nsort(a)\nassert(a == [1, 2, 3])\nreverse(a)\nassert(a == [3, 2, 1])\nassert(slice(a, 0, 2) == [3, 2])\ninsert(a, 0, 9)\nassert(a[0] == 9)\nassert(remove(a, 0) == 9)\nclear(a)\nassert(len(a) == 0)\n")
	mustRun(t, "let m = {a: 1}\nassert(get(m, \"zz\", 9) == 9)\nassert(merge({a: 1}, {b: 2}) == {a: 1, b: 2})\nassert(delete(m, \"a\") == true)\nassert(has(m, \"a\") == false)\n")
	mustRun(t, "assert(abs(-5) == 5)\nassert(min(3, 1, 2) == 1)\nassert(max([1, 9, 2]) == 9)\nassert(floor(2.9) == 2)\nassert(ceil(2.1) == 3)\nassert(round(2.5) == 3)\nassert(sqrt(16) == 4)\nassert(pow(2, 10) == 1024)\nassert(pi() > 3.14)\nassert(now() > 0)\nassert(rand() < 1)\nassert(randint(1, 6) >= 1)\n")
	mustRun(t, "assert(bit_and(6, 3) == 2)\nassert(bit_or(6, 3) == 7)\nassert(bit_xor(6, 3) == 5)\nassert(bit_shl(1, 3) == 8)\nassert(bit_shr(8, 2) == 2)\nassert(bit_not(0) == -1)\n")
	mustRun(t, "assert(map([1,2,3], func(x) { return x * 2 }) == [2, 4, 6])\nassert(filter([1,2,3,4], func(x) { return x % 2 == 0 }) == [2, 4])\nassert(reduce([1,2,3], func(a, b) { return a + b }, 0) == 6)\nassert(apply(func(a, b) { return a + b }, [2, 3]) == 5)\n")
	mustRun(t, "assert(json_parse(json_stringify({a: [1, 2]})) == {a: [1, 2]})\n")
	mustRun(t, "let c = chan(1)\nassert(try_send(c, 7) == true)\nassert(try_send(c, 8) == false)\nassert(recv(c) == 7)\nassert(chan_len(c) == 0)\nassert(chan_cap(c) == 1)\nassert(len(c) == 0)\n")
	mustRun(t, "assert(type(len) == \"func\")\n")
}

func TestRunStdlibFiles(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "note.txt")
	prog := "assert(write_file(\"" + file + "\", \"hi\") == 2)\n" +
		"assert(read_file(\"" + file + "\") == \"hi\")\n" +
		"assert(append_file(\"" + file + "\", \"!\") == 1)\n" +
		"assert(read_file(\"" + file + "\") == \"hi!\")\n" +
		"assert(exists(\"" + file + "\"))\n" +
		"assert(contains(list_dir(\"" + dir + "\"), \"note.txt\"))\n" +
		"remove(\"" + file + "\")\n" +
		"assert(exists(\"" + file + "\") == false)\n"
	mustRun(t, prog)
}

func TestRunSelect(t *testing.T) {
	// Ready buffered recv wins over timeout.
	mustRun(t, "let c1 = chan(1)\nlet c2 = chan(1)\nsend(c1, 42)\nselect {\n case v = recv(c1) { assert(v == 42) }\n case recv(c2) { error(\"wrong branch\") }\n case timeout(500) { error(\"should not time out\") }\n}\n")
	// Discard form (no bind).
	mustRun(t, "let c = chan(1)\nsend(c, 1)\nselect {\n case recv(c) { assert(true) }\n}\n")
	// Either of two ready branches may win; both asserts hold.
	mustRun(t, "let a = chan(1)\nlet b = chan(1)\nsend(a, 1)\nsend(b, 2)\nselect {\n case v = recv(a) { assert(v == 1) }\n case v = recv(b) { assert(v == 2) }\n}\n")
	// Buffered send case is immediately ready; value is delivered.
	mustRun(t, "let c = chan(1)\nselect {\n case send(c, 8) { assert(true) }\n case timeout(100) { error(\"should have sent\") }\n}\nassert(recv(c) == 8)\n")
	// Rendezvous with a sender goroutine (received, so no leaked goroutine).
	mustRun(t, "let c = chan(0)\ngo send(c, 7)\nselect {\n case v = recv(c) { assert(v == 7) }\n case timeout(2000) { error(\"should have received\") }\n}\n")
	// Recv on a closed channel is immediately ready with nil.
	mustRun(t, "let c = chan(1)\nclose(c)\nselect {\n case v = recv(c) { assert(v == nil) }\n case timeout(100) { error(\"closed recv should be ready\") }\n}\n")
	// Timeout fires when nothing is ready.
	mustRun(t, "let c = chan(0)\nselect {\n case recv(c) { error(\"nothing to receive\") }\n case timeout(20) { assert(true) }\n}\n")
	// Default never blocks (timeouts are skipped on this path).
	mustRun(t, "let c = chan(0)\nselect {\n case recv(c) { error(\"nothing to receive\") }\n case timeout(10000) { error(\"default should win\") }\n default { assert(true) }\n}\n")
	// Nil channel disables a case: fan-in drain over two closing channels.
	mustRun(t, "let a = chan(2)\nlet b = chan(2)\nsend(a, 1)\nsend(a, 2)\nsend(b, 3)\nclose(a)\nclose(b)\nlet total = 0\nwhile a != nil or b != nil {\n select {\n case v = recv(a) { if v == nil { a = nil } else { total = total + v } }\n case v = recv(b) { if v == nil { b = nil } else { total = total + v } }\n }\n}\nassert(total == 6)\n")
	// break ends the select only (like switch); the loop keeps going.
	mustRun(t, "let c = chan(1)\nsend(c, 1)\nclose(c)\nlet steps = []\nwhile len(steps) < 2 {\n select {\n case v = recv(c) { push(steps, v)\n break }\n }\n}\nassert(steps == [1, nil])\n")
	// Send on a closed channel fails instead of panicking.
	mustFail(t, "let c = chan(1)\nclose(c)\nselect {\n case send(c, 1) { print 1 }\n}\n")
}

func TestRunChanIteration(t *testing.T) {
	// `for v in ch` drains until close (like Go's `for v := range ch`).
	mustRun(t, "let ch = chan(3)\nsend(ch, 10)\nsend(ch, 20)\nclose(ch)\nlet total = 0\nfor v in ch {\n total = total + v\n}\nassert(total == 30)\n")
	// break/continue work inside channel loops.
	mustRun(t, "let ch = chan(3)\nsend(ch, 1)\nsend(ch, 2)\nsend(ch, 3)\nclose(ch)\nlet seen = []\nfor v in ch {\n if v == 3 { continue }\n push(seen, v)\n if v == 2 { break }\n}\nassert(seen == [1, 2])\n")
	// Closed empty channel: loop body never runs.
	mustRun(t, "let ch = chan(1)\nclose(ch)\nlet n = 0\nfor v in ch {\n n = n + 1\n}\nassert(n == 0)\n")
	// Two loop vars are meaningless for channels.
	mustFail(t, "let ch = chan(1)\nfor k, v in ch {\n print k\n}\n")
}

func TestRunChanTimeoutBuiltins(t *testing.T) {
	// recv_timeout returns the value when one is available.
	mustRun(t, "let c = chan(1)\nsend(c, 9)\nassert(recv_timeout(c, 100) == 9)\n")
	// recv_timeout yields nil on timeout (and on drained close, like recv).
	mustRun(t, "let c = chan(0)\nassert(recv_timeout(c, 20) == nil)\nlet d = chan(1)\nclose(d)\nassert(recv_timeout(d, 20) == nil)\n")
	// send_timeout reports whether the send happened.
	mustRun(t, "let free = chan(1)\nassert(send_timeout(free, 1, 50) == true)\nassert(recv(free) == 1)\nlet stuck = chan(0)\nassert(send_timeout(stuck, 1, 20) == false)\n")
	mustFail(t, "let c = chan(1)\nclose(c)\nprint send_timeout(c, 1, 10)\n")
	// chan_closed introspection.
	mustRun(t, "let c = chan(1)\nassert(chan_closed(c) == false)\nclose(c)\nassert(chan_closed(c))\n")
	mustFail(t, "print chan_closed(7)\n")
	mustFail(t, "print recv_timeout(7, 10)\n")
}
