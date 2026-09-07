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

func TestVetExhaustiveEnum(t *testing.T) {
	dir := t.TempDir()
	// exhaustive: all variants covered, no default needed
	okSrc := "enum Color { Red, Green, Blue }\nlet c: Color = \"Red\"\nswitch c {\n case \"Red\" { print 1 }\n case \"Green\" { print 2 }\n case \"Blue\" { print 3 }\n}\n"
	if err := os.WriteFile(filepath.Join(dir, "ok.ks"), []byte(okSrc), 0o644); err != nil {
		t.Fatal(err)
	}
	issues, err := VetTarget(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, is := range issues {
		if is.Rule == "exhaustive-switch" {
			t.Fatalf("want exhaustive ok, got %v", issues)
		}
	}
	// non-exhaustive: missing Blue, no default
	os.Remove(filepath.Join(dir, "ok.ks"))
	badSrc := "enum Color { Red, Green, Blue }\nlet c: Color = \"Red\"\nswitch c {\n case \"Red\" { print 1 }\n case \"Green\" { print 2 }\n}\n"
	if err := os.WriteFile(filepath.Join(dir, "bad.ks"), []byte(badSrc), 0o644); err != nil {
		t.Fatal(err)
	}
	issues, _ = VetTarget(dir, false)
	found := false
	for _, is := range issues {
		if is.Rule == "exhaustive-switch" && contains(is.Msg, "Blue") {
			found = true
		}
	}
	if !found {
		t.Fatalf("want exhaustive-switch missing Blue, got %v", issues)
	}
	// default rescues non-exhaustive
	os.Remove(filepath.Join(dir, "bad.ks"))
	defSrc := "enum Color { Red, Green }\nlet c: Color = \"Red\"\nswitch c {\n case \"Red\" { print 1 }\n default { print 9 }\n}\n"
	if err := os.WriteFile(filepath.Join(dir, "def.ks"), []byte(defSrc), 0o644); err != nil {
		t.Fatal(err)
	}
	issues, _ = VetTarget(dir, false)
	for _, is := range issues {
		if is.Rule == "exhaustive-switch" {
			t.Fatalf("default must satisfy exhaustiveness, got %v", issues)
		}
	}
}

func TestVetExhaustiveBool(t *testing.T) {
	dir := t.TempDir()
	src := "let b: bool = true\nswitch b {\n case true { print 1 }\n}\n"
	if err := os.WriteFile(filepath.Join(dir, "b.ks"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	issues, _ := VetTarget(dir, false)
	found := false
	for _, is := range issues {
		if is.Rule == "exhaustive-switch" {
			found = true
		}
	}
	if !found {
		t.Fatalf("want bool exhaustiveness error, got %v", issues)
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

func TestAuditMissingLock(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(dir+"/fusion.toml", []byte("[package]\nname=\"x\"\nversion=\"0.1.0\"\n"), 0o644)
	issues, err := Audit(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) == 0 {
		t.Fatalf("want missing lock issue")
	}
}

func writeFrontend(t *testing.T, dir, name, src string) string {
	t.Helper()
	fe := filepath.Join(dir, "frontend")
	if err := os.MkdirAll(fe, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(fe, name)
	if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func vetRules(issues []VetIssue) map[string][]VetIssue {
	m := map[string][]VetIssue{}
	for _, is := range issues {
		m[is.Rule] = append(m[is.Rule], is)
	}
	return m
}

func TestVetFrontendEnvError(t *testing.T) {
	dir := t.TempDir()
	writeFrontend(t, dir, "bad.ks", "print env(\"SECRET\")\n")
	issues, err := VetTarget(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	m := vetRules(issues)
	got := m["frontend-env"]
	if len(got) == 0 {
		t.Fatalf("want frontend-env error, got %v", issues)
	}
	if !got[0].IsError {
		t.Fatalf("frontend-env must be IsError=true (STRICT), got %+v", got[0])
	}
	// ROUTE exception allowed
	dir2 := t.TempDir()
	writeFrontend(t, dir2, "ok.ks", "let route = env(\"ROUTE\", \"/\")\nprint route\n")
	issues, _ = VetTarget(dir2, false)
	for _, is := range issues {
		if is.Rule == "frontend-env" {
			t.Fatalf("ROUTE must be allowed, got %v", issues)
		}
	}
	// block comment / string / line comment must not flag
	dir3 := t.TempDir()
	writeFrontend(t, dir3, "ok2.ks", "/* env(\"X\") */\nprint \"env( in string\"\n# env(\"Y\")\n// env(\"Z\")\nprint 1\n")
	issues, _ = VetTarget(dir3, false)
	for _, is := range issues {
		if is.Rule == "frontend-env" {
			t.Fatalf("comments/strings must not flag, got %v", issues)
		}
	}
	// `env (` with space must flag
	dir4 := t.TempDir()
	writeFrontend(t, dir4, "sp.ks", "print env (\"SECRET\")\n")
	issues, _ = VetTarget(dir4, false)
	found := false
	for _, is := range issues {
		if is.Rule == "frontend-env" {
			found = true
		}
	}
	if !found {
		t.Fatalf("want frontend-env for `env (` with space, got %v", issues)
	}
}

func TestVetFrontendSetHTML(t *testing.T) {
	// literal ok
	dir := t.TempDir()
	writeFrontend(t, dir, "ok.ks", "set_html(\"<b>hi</b>\")\njs_call(\"set_html\", \"<b>hi</b>\")\n")
	issues, _ := VetTarget(dir, false)
	for _, is := range issues {
		if is.Rule == "frontend-set-html" {
			t.Fatalf("literal set_html must pass, got %v", issues)
		}
	}
	// non-literal errors
	dir2 := t.TempDir()
	writeFrontend(t, dir2, "bad.ks", "let h = \"<b>\"\nset_html(h)\n")
	issues, _ = VetTarget(dir2, false)
	found := false
	for _, is := range issues {
		if is.Rule == "frontend-set-html" && is.IsError {
			found = true
		}
	}
	if !found {
		t.Fatalf("want frontend-set-html error for set_html(h), got %v", issues)
	}
	dir3 := t.TempDir()
	writeFrontend(t, dir3, "bad2.ks", "let h = \"<b>\"\njs_call(\"set_html\", h)\n")
	issues, _ = VetTarget(dir3, false)
	found = false
	for _, is := range issues {
		if is.Rule == "frontend-set-html" && is.IsError {
			found = true
		}
	}
	if !found {
		t.Fatalf("want frontend-set-html for js_call non-literal, got %v", issues)
	}
}

func TestVetFrontendKey(t *testing.T) {
	// missing key error
	dir := t.TempDir()
	writeFrontend(t, dir, "bad.ks", "func f(props) {\n return {type: \"div\", props: {}, children: []}\n}\nprint f({})\n")
	issues, _ := VetTarget(dir, false)
	found := false
	for _, is := range issues {
		if is.Rule == "frontend-key" && is.IsError {
			found = true
		}
	}
	if !found {
		t.Fatalf("want frontend-key missing key error, got %v", issues)
	}
	// literal key ok
	dir2 := t.TempDir()
	writeFrontend(t, dir2, "ok.ks", "func f(props) {\n return {key: \"a\", type: \"div\", props: {}, children: []}\n}\nprint f({})\n")
	issues, _ = VetTarget(dir2, false)
	for _, is := range issues {
		if is.Rule == "frontend-key" {
			t.Fatalf("literal key must pass, got %v", issues)
		}
	}
	// index key error (for-in var)
	dir3 := t.TempDir()
	writeFrontend(t, dir3, "idx.ks", "for i, x in [1, 2] {\n let vm = {key: i, type: \"div\", props: {}, children: []}\n print vm\n}\n")
	issues, _ = VetTarget(dir3, false)
	found = false
	for _, is := range issues {
		if is.Rule == "frontend-key" && is.IsError && contains(is.Msg, "index key") {
			found = true
		}
	}
	if !found {
		t.Fatalf("want frontend-key index key error, got %v", issues)
	}
}

func TestBenchNestedImportAppRoot(t *testing.T) {
	// v2.7: bench must resolve imports from the app root (nearest
	// fusion.toml), like `fusion test` — not from the file's own dir.
	// Three levels deep on purpose: with file-dir resolution even the
	// parent-dir fallback misses (this mirrors tests/hello-app, whose
	// *_test.ks files failed under `fusion bench`).
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "fusion.toml"), []byte("[package]\nname=\"b\"\nversion=\"0.1.0\"\n"), 0o644)
	os.MkdirAll(filepath.Join(dir, "frontend", "store"), 0o755)
	os.MkdirAll(filepath.Join(dir, "frontend", "pages"), 0o755)
	os.WriteFile(filepath.Join(dir, "frontend", "store", "app.ks"), []byte("func bench_helper() {\n return 41\n}\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "frontend", "pages", "nested.ks"), []byte("import \"frontend/store/app.ks\"\nassert(bench_helper() == 41)\n"), 0o644)
	if err := Bench(dir, 1); err != nil {
		t.Fatalf("bench failed: %v", err)
	}
}
