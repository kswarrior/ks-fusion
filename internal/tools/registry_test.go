package tools

import (
	"os"
	"path/filepath"
	"testing"
)

func findRoot(t *testing.T) string {
	t.Helper()
	dir, _ := os.Getwd()
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Skip("no module root")
		}
		dir = parent
	}
}

func TestRegistryPublishPull(t *testing.T) {
	tmpReg := t.TempDir()
	tmpOut := t.TempDir()
	root := findRoot(t)
	old := os.Getenv("FUSION_REGISTRY")
	os.Setenv("FUSION_REGISTRY", tmpReg)
	defer os.Setenv("FUSION_REGISTRY", old)
	// publish hello-lib
	if _, err := Publish(filepath.Join(root, "tests/hello-lib"), tmpReg); err != nil {
		t.Fatalf("publish failed: %v", err)
	}
	// pull
	dst, err := Pull("hello-lib", "0.1.0", tmpOut)
	if err != nil {
		// try with env registry
		t.Fatalf("pull failed: %v", err)
	}
	if _, err := os.Stat(dst); err != nil {
		t.Fatalf("pulled file missing: %v", err)
	}
	// yank
	if err := Yank("hello-lib", "0.1.0", false); err != nil {
		t.Fatalf("yank failed: %v", err)
	}
	// pull yanked should fail
	if _, err := Pull("hello-lib", "0.1.0", filepath.Join(tmpOut, "x")); err == nil {
		t.Fatalf("want pull yanked to fail")
	}
}

func TestAuditHashRecompute(t *testing.T) {
	tmpReg := t.TempDir()
	root := findRoot(t)
	old := os.Getenv("FUSION_REGISTRY")
	os.Setenv("FUSION_REGISTRY", tmpReg)
	defer os.Setenv("FUSION_REGISTRY", old)
	if _, err := Publish(filepath.Join(root, "tests/hello-lib"), tmpReg); err != nil {
		t.Fatalf("publish failed: %v", err)
	}
	if got := VerifyRegistry(); len(got) != 0 {
		t.Fatalf("want clean registry, got %v", got)
	}
	// tamper with the bundle bytes: index + sidecar must disagree now
	entries := loadIndex(tmpReg)
	if len(entries) == 0 {
		t.Fatal("no index entries after publish")
	}
	fp := filepath.Join(tmpReg, filepath.FromSlash(entries[0].Path))
	data, err := os.ReadFile(fp)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fp, append(data, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	got := VerifyRegistry()
	if len(got) == 0 {
		t.Fatal("want checksum mismatch after tamper")
	}
	found := false
	for _, s := range got {
		if len(s) >= 8 && containsStr(s, "mismatch") {
			found = true
		}
	}
	if !found {
		t.Fatalf("want mismatch issue, got %v", got)
	}
}

func containsStr(hay, needle string) bool {
	if len(needle) == 0 {
		return true
	}
	for i := 0; i+len(needle) <= len(hay); i++ {
		if hay[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}

func TestAuditTransitive(t *testing.T) {
	tmpReg := t.TempDir()
	appDir := t.TempDir()
	old := os.Getenv("FUSION_REGISTRY")
	os.Setenv("FUSION_REGISTRY", tmpReg)
	defer os.Setenv("FUSION_REGISTRY", old)
	// base lib with no deps
	baseDir := filepath.Join(t.TempDir(), "base-lib")
	mkLib(t, baseDir, "base-lib", "0.1.0", "func base() {\n return 1\n}\n")
	if _, err := Publish(baseDir, tmpReg); err != nil {
		t.Fatalf("publish base: %v", err)
	}
	// mid lib importing base (transitive dep)
	midDir := filepath.Join(t.TempDir(), "mid-lib")
	mkLib(t, midDir, "mid-lib", "0.1.0", "import \"base-lib\"\nfunc mid() {\n return base()\n}\n")
	midBundle, err := Publish(midDir, tmpReg)
	if err != nil {
		t.Fatalf("publish mid: %v", err)
	}
	// app lock lists only mid (transitive base missing)
	os.WriteFile(filepath.Join(appDir, "fusion.toml"), []byte("[package]\nname = \"app\"\nversion = \"0.1.0\"\nentry_backend = \"main.ks\"\nentry_frontend = \"main.ks\"\n"), 0o644)
	os.WriteFile(filepath.Join(appDir, "main.ks"), []byte("print 1\n"), 0o644)
	lock := "{\"version\": 1, \"packages\": [{\"name\": \"mid-lib\", \"version\": \"0.1.0\", \"path\": \"" + midBundle + "\"}]}"
	// lock paths must be slash-separated for audit Join portability
	os.WriteFile(filepath.Join(appDir, "fusion.lock"), []byte(lock), 0o644)
	issues, err := Audit(appDir)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, s := range issues {
		if containsStr(s, "base-lib") && containsStr(s, "transitive") {
			found = true
		}
	}
	if !found {
		t.Fatalf("want transitive base-lib issue, got %v", issues)
	}
}

func mkLib(t *testing.T, dir, name, version, src string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	toml := "[package]\nname = \"" + name + "\"\nversion = \"" + version + "\"\ntype = \"lib\"\n\n[lib]\nname = \"" + name + "\"\npath = \"src/lib.ks\"\n"
	if err := os.WriteFile(filepath.Join(dir, "fusion.toml"), []byte(toml), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "src", "lib.ks"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestSSG(t *testing.T) {
	root := findRoot(t)
	app := filepath.Join(root, "tests/hello-app")
	out := t.TempDir()
	if err := BuildSSG(app, out); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(out, "index.html")); err != nil {
		t.Fatalf("ssg index missing: %v", err)
	}
}

func TestCache(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "fusion.toml"), []byte("[package]\nname=\"x\"\nversion=\"0.1.0\"\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "a.ks"), []byte("print 1\n"), 0o644)
	hit, _, err := CheckCache(dir)
	if err != nil {
		t.Fatal(err)
	}
	if hit {
		t.Fatalf("want miss first")
	}
	if err := WriteCache(dir); err != nil {
		t.Fatal(err)
	}
	hit, _, err = CheckCache(dir)
	if err != nil || !hit {
		t.Fatalf("want hit after write: %v %v", hit, err)
	}
}

func TestCacheVendorBusts(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "fusion.toml"), []byte("[package]\nname=\"x\"\nversion=\"0.1.0\"\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "a.ks"), []byte("print 1\n"), 0o644)
	if err := os.MkdirAll(filepath.Join(dir, "vendor"), 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, "vendor", "lib.kslib"), []byte("v1"), 0o644)
	if err := WriteCache(dir); err != nil {
		t.Fatal(err)
	}
	if hit, _, _ := CheckCache(dir); !hit {
		t.Fatal("want hit before vendor swap")
	}
	// swapping vendored content must bust the cache (v2.5 skipped vendor/)
	os.WriteFile(filepath.Join(dir, "vendor", "lib.kslib"), []byte("v2"), 0o644)
	if hit, _, _ := CheckCache(dir); hit {
		t.Fatal("want miss after vendor swap (vendor-aware cache)")
	}
}
