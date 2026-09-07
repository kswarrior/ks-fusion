package tools

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestBuildBinE2E builds a minimal app to a single executable and runs it.
// Closes the v2.5 Maturity gap ("--bin E2E untested"): proves `fusion build
// --bin` embeds .ks sources and the binary runs them without a runtime.
// Needs a Go toolchain (same requirement as `fusion build --bin` itself);
// skips cleanly when `go` is absent.
func TestBuildBinE2E(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not on PATH (needed by fusion build --bin)")
	}
	dir := t.TempDir()
	toml := "[package]\nname = \"e2e-bin\"\nversion = \"0.1.0\"\nentry_backend = \"backend/main.ks\"\nentry_frontend = \"frontend/main.ks\"\n"
	if err := os.WriteFile(filepath.Join(dir, "fusion.toml"), []byte(toml), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "backend"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "frontend"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "backend", "main.ks"), []byte("print \"e2e-backend-ok\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "frontend", "main.ks"), []byte("print \"e2e-frontend-ok\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "e2e-bin")
	if err := BuildBin(dir, out, ""); err != nil {
		t.Fatalf("BuildBin: %v", err)
	}
	fi, err := os.Stat(out)
	if err != nil || fi.Size() == 0 {
		t.Fatalf("want non-empty binary at %s: %v", out, err)
	}
	cmd := exec.Command(out)
	b, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run built bin: %v\n%s", err, b)
	}
	got := string(b)
	if !strings.Contains(got, "e2e-backend-ok") || !strings.Contains(got, "e2e-frontend-ok") {
		t.Fatalf("want backend+frontend output, got %q", got)
	}
	// `--strip` sibling: same app, smaller binary, same output.
	stripOut := filepath.Join(dir, "e2e-bin-strip")
	if err := BuildBinWithOptions(dir, stripOut, "", true); err != nil {
		t.Fatalf("BuildBinWithOptions(strip): %v", err)
	}
	sfi, err := os.Stat(stripOut)
	if err != nil || sfi.Size() == 0 {
		t.Fatalf("want non-empty stripped binary at %s: %v", stripOut, err)
	}
	if sfi.Size() >= fi.Size() {
		t.Fatalf("want stripped binary smaller: full=%d strip=%d", fi.Size(), sfi.Size())
	}
	scmd := exec.Command(stripOut)
	sb, err := scmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run stripped bin: %v\n%s", err, sb)
	}
	sgot := string(sb)
	if !strings.Contains(sgot, "e2e-backend-ok") || !strings.Contains(sgot, "e2e-frontend-ok") {
		t.Fatalf("want backend+frontend output from stripped bin, got %q", sgot)
	}
}
