package backend

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kswarrior/ks-fusion/internal/frontend"
)

func mustRunExt(t *testing.T, src string) {
	t.Helper()
	p, err := frontend.ParseSource(src, "test.ks")
	if err != nil {
		t.Fatal(err)
	}
	// fold like runtime
	frontend.FoldProgram(p)
	if err := Run(p); err != nil {
		t.Fatal(err)
	}
}

func TestExtRegex(t *testing.T) {
	mustRunExt(t, `assert(regex_match("foobar", "foo.*"))`)
	mustRunExt(t, `let m = regex_find("a1b22", "[0-9]+")`+"\n"+`assert(len(m) == 2)`)
	mustRunExt(t, `assert(regex_replace("aaa", "a", "b") == "bbb")`)
	mustRunExt(t, `let p = regex_split("a,b,c", ",")`+"\n"+`assert(len(p) == 3)`)
}

func TestExtCrypto(t *testing.T) {
	mustRunExt(t, `assert(sha256("hello") == "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824")`)
	mustRunExt(t, `assert(md5("hello") == "5d41402abc4b2a76b9719d911017c592")`)
	mustRunExt(t, `let h = hmac_sha256("msg", "key")`+"\n"+`assert(len(h) == 64)`)
	mustRunExt(t, `assert(base64_decode(base64_encode("hi")) == "hi")`)
	mustRunExt(t, `assert(hex_decode(hex_encode("hi")) == "hi")`)
	mustRunExt(t, `let u = uuid()`+"\n"+`assert(len(u) == 36)`)
	mustRunExt(t, `let b = random_bytes(8)`+"\n"+`assert(len(b) == 8)`)
}

func TestExtFS(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "a.txt")
	mustRunExt(t, `write_file("`+f+`", "hi")`)
	mustRunExt(t, `let s = stat("`+f+`")`+"\n"+`assert(s.size == 2)`)
	mustRunExt(t, `cp("`+f+`", "`+f+`.2")`+"\n"+`assert(exists("`+f+`.2"))`)
	mustRunExt(t, `mv("`+f+`.2", "`+f+`.3")`+"\n"+`assert(exists("`+f+`.3"))`)
	mustRunExt(t, `let g = glob("`+dir+`/*.txt")`+"\n"+`assert(len(g) >= 1)`)
	mustRunExt(t, `assert(path_join("a", "b") == "a/b" or path_join("a", "b") == "a\\b")`)
	mustRunExt(t, `let p = abs_path(".")`+"\n"+`assert(len(p) > 0)`)
}

func TestExtProcessTime(t *testing.T) {
	mustRunExt(t, `let r = exec("echo", ["hi"])`+"\n"+`assert(r.code == 0)`)
	mustRunExt(t, `let r = shell("echo hi")`+"\n"+`assert(r.code == 0)`)
	mustRunExt(t, `let c = cwd()`+"\n"+`assert(len(c) > 0)`)
	mustRunExt(t, `let e = env_all()`+"\n"+`assert(len(keys(e)) > 0)`)
	mustRunExt(t, `let s = format_time(0, "date")`+"\n"+`assert(s == "1970-01-01")`)
	mustRunExt(t, `let ms = parse_time("1970-01-01", "2006-01-02")`+"\n"+`assert(ms == 0)`)
	mustRunExt(t, `let p = time_parts(0)`+"\n"+`assert(p.year == 1970)`)
}

func TestExtDB(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "kv.json")
	mustRunExt(t, `db_put("`+db+`", "k", 42)`+"\n"+`assert(db_get("`+db+`", "k") == 42)`)
	mustRunExt(t, `assert(db_get("`+db+`", "missing", "dflt") == "dflt")`)
	mustRunExt(t, `let ks = db_list("`+db+`")`+"\n"+`assert(len(ks) == 1)`)
	mustRunExt(t, `assert(db_delete("`+db+`", "k") == true)`+"\n"+`assert(db_get("`+db+`", "k") == nil)`)
	_ = os.Remove(db)
}

func TestExtConcurrencyTypes(t *testing.T) {
	mustRunExt(t, `let v = with_timeout(1000, func() { return 7 })`+"\n"+`assert(v == 7)`)
	mustRunExt(t, `let out = parallel([1, 2, 3], func(x) { return x * 2 })`+"\n"+`assert(out == [2, 4, 6])`)
	mustRunExt(t, `assert(struct_validate({name: "a", age: 1}, {name: "string", age: "int"}))`)
	mustRunExt(t, `let e = enum_create(["a", "b"])`+"\n"+`assert(enum_valid(e, "a"))`)
	mustRunExt(t, `assert(is_number(1) and is_number(2.5) and not is_number("x"))`)
	mustRunExt(t, `assert_eq(1, 1)`+"\n"+`assert_ne(1, 2)`+"\n"+`assert_contains("hello", "ell")`)
	mustRunExt(t, `assert(trim_prefix("foobar", "foo") == "bar")`)
}

func TestBuiltinCount(t *testing.T) {
	if BuiltinCount() < 130 {
		t.Fatalf("want >=130 builtins after v2.2, got %d", BuiltinCount())
	}
}
