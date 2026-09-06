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
