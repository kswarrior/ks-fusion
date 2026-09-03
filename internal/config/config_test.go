package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad(t *testing.T) {
	dir := t.TempDir()
	toml := `[package]
name = "myapp"
version = "0.2.0"
entry_backend = "backend/main.ks"
entry_frontend = "frontend/main.ks"
`
	if err := os.WriteFile(filepath.Join(dir, "fusion.toml"), []byte(toml), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if c.Name != "myapp" || c.Version != "0.2.0" {
		t.Fatalf("bad config: %+v", c)
	}
	if c.BackendEntry != "backend/main.ks" || c.FrontendEntry != "frontend/main.ks" {
		t.Fatalf("bad entries: %+v", c)
	}
}

func TestLoadMissing(t *testing.T) {
	if _, err := Load(t.TempDir()); err == nil {
		t.Fatal("want error when fusion.toml missing")
	}
}
