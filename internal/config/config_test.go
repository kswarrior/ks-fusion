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

func TestLoadLib(t *testing.T) {
	dir := t.TempDir()
	toml := `[package]
name = "hello-lib"
version = "0.1.0"
type = "lib"

[lib]
name = "hello-lib"
path = "src/lib.ks"

[dependencies]
other = "1.2.0"
local = { path = "../local" }
`
	if err := os.WriteFile(filepath.Join(dir, "fusion.toml"), []byte(toml), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !c.IsLib() || c.Type != "lib" {
		t.Fatalf("want lib type, got %+v", c)
	}
	if c.LibName != "hello-lib" || c.LibEntry != "src/lib.ks" {
		t.Fatalf("bad lib section: %+v", c)
	}
	if c.Dependencies["other"] != "1.2.0" {
		t.Fatalf("bad version dep: %+v", c.Dependencies)
	}
	if c.Dependencies["local"] != "path:../local" {
		t.Fatalf("bad path dep: %+v", c.Dependencies)
	}
}

func TestLoadDefaults(t *testing.T) {
	dir := t.TempDir()
	toml := "[package]\nname = \"plain\"\n"
	if err := os.WriteFile(filepath.Join(dir, "fusion.toml"), []byte(toml), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if c.IsLib() || c.Type != "app" || c.LibName != "plain" || c.LibEntry != "src/lib.ks" {
		t.Fatalf("bad defaults: %+v", c)
	}
	if len(c.Dependencies) != 0 {
		t.Fatalf("want no deps, got %+v", c.Dependencies)
	}
}

func TestLoadBadType(t *testing.T) {
	dir := t.TempDir()
	toml := "[package]\nname = \"x\"\ntype = \"exe\"\n"
	if err := os.WriteFile(filepath.Join(dir, "fusion.toml"), []byte(toml), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(dir); err == nil {
		t.Fatal("want error for bad package type")
	}
}
