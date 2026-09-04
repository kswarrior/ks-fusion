package lib

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kswarrior/ks-fusion/internal/config"
)

func writeLib(t *testing.T, dir, name, version string, files map[string]string) {
	t.Helper()
	toml := "[package]\nname = \"" + name + "\"\nversion = \"" + version + "\"\ntype = \"lib\"\n"
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "fusion.toml"), []byte(toml), 0o644); err != nil {
		t.Fatal(err)
	}
	for rel, src := range files {
		full := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestBuildLoadRoundtrip(t *testing.T) {
	dir := t.TempDir()
	writeLib(t, dir, "hello-lib", "0.1.0", map[string]string{
		"src/lib.ks":   "func greet(n) {\n return \"hi \" + n\n}\n",
		"src/extra.ks": "let answer = 42\n",
	})
	cfg, err := config.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.IsLib() || cfg.LibName != "hello-lib" {
		t.Fatalf("bad lib config: %+v", cfg)
	}
	out, err := Build(cfg, filepath.Join(dir, "test-releases"))
	if err != nil {
		t.Fatal(err)
	}
	b, err := Load(out)
	if err != nil {
		t.Fatal(err)
	}
	if b.Name != "hello-lib" || b.Version != "0.1.0" || len(b.Files) != 2 {
		t.Fatalf("bad bundle: %+v", b)
	}
	if b.Files[0].Path != "src/extra.ks" || b.Files[1].Path != "src/lib.ks" {
		t.Fatalf("files not sorted: %+v", b.Files)
	}
}

func TestBuildIsExecutableShebang(t *testing.T) {
	dir := t.TempDir()
	writeLib(t, dir, "hello-lib", "0.1.0", map[string]string{
		"src/lib.ks": "func greet(n) {\n return \"hi \" + n\n}\n",
	})
	cfg, err := config.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	out, err := Build(cfg, filepath.Join(dir, "test-releases"))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw[:len(Shebang)]) != Shebang {
		t.Fatalf("bundle must start with shebang %q", Shebang)
	}
	// Old plain-JSON bundles (no shebang) must still load.
	plain := filepath.Join(dir, "plain.kslib")
	inner := raw[len(Shebang)+1:]
	if err := os.WriteFile(plain, inner, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(plain); err != nil {
		t.Fatalf("plain JSON bundle must load: %v", err)
	}
	if _, err := Load(out); err != nil {
		t.Fatalf("shebang bundle must load: %v", err)
	}
}

func TestBuildRejectsApp(t *testing.T) {
	dir := t.TempDir()
	toml := "[package]\nname = \"x\"\nversion = \"0.1.0\"\n"
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "fusion.toml"), []byte(toml), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Build(cfg, t.TempDir()); err == nil {
		t.Fatal("want error building app as lib")
	}
}

func TestBuildFailsOnBadSource(t *testing.T) {
	dir := t.TempDir()
	writeLib(t, dir, "bad", "0.1.0", map[string]string{"src/lib.ks": "???\n"})
	cfg, err := config.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Build(cfg, t.TempDir()); err == nil {
		t.Fatal("want error for unparsable lib source")
	}
}

func TestFindPicksNewest(t *testing.T) {
	d1 := t.TempDir()
	d2 := t.TempDir()
	writeLib(t, filepath.Join(d1, "pkg"), "mylib", "0.2.0", map[string]string{"src/lib.ks": "let x = 1\n"})
	writeLib(t, filepath.Join(d2, "pkg"), "mylib", "0.10.0", map[string]string{"src/lib.ks": "let x = 2\n"})
	for _, d := range []string{d1, d2} {
		cfg, err := config.Load(filepath.Join(d, "pkg"))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := Build(cfg, d); err != nil {
			t.Fatal(err)
		}
	}
	got, err := Find("mylib", []string{d1, d2})
	if err != nil {
		t.Fatal(err)
	}
	b, err := Load(got)
	if err != nil {
		t.Fatal(err)
	}
	if b.Version != "0.10.0" {
		t.Fatalf("want newest 0.10.0, got %s (%s)", b.Version, got)
	}
	if _, err := Find("missing", []string{d1, d2}); err == nil {
		t.Fatal("want error for unknown lib")
	}
}
