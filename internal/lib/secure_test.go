package lib

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kswarrior/ks-fusion/internal/config"
)

func TestSecureRoundtripDefaultKey(t *testing.T) {
	dir := t.TempDir()
	writeLib(t, dir, "mylib", "0.1.0", map[string]string{
		"src/lib.ks": "func greet(n) {\n return \"hi \" + n\n}\n",
	})
	cfg, err := config.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	out, err := BuildSecure(cfg, filepath.Join(dir, "out"), "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(out, SecureExt) {
		t.Fatalf("want .ksx artifact, got %s", out)
	}
	b, err := LoadSecure(out, "")
	if err != nil {
		t.Fatal(err)
	}
	if b.Name != "mylib" || len(b.Files) != 1 {
		t.Fatalf("bad bundle: %+v", b)
	}
	// Opaque on disk: no source, no format tag, no symbol names.
	raw, _ := os.ReadFile(out)
	s := string(raw)
	for _, leak := range []string{"hi ", "greet", "kslib-1", "fusion", "func"} {
		if strings.Contains(s, leak) {
			t.Fatalf("secure bundle leaks %q", leak)
		}
	}
	// LoadAny handles both formats.
	if _, err := LoadAny(out, ""); err != nil {
		t.Fatalf("LoadAny .ksx: %v", err)
	}
}

func TestSecurePasswordRoundtrip(t *testing.T) {
	dir := t.TempDir()
	writeLib(t, dir, "mylib", "0.1.0", map[string]string{
		"src/lib.ks": "let secret = 42\n",
	})
	cfg, err := config.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	out, err := BuildSecure(cfg, filepath.Join(dir, "out"), "s3cr3t")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := LoadSecure(out, "s3cr3t"); err != nil {
		t.Fatalf("correct password must load: %v", err)
	}
	if _, err := LoadSecure(out, "wrong"); err == nil {
		t.Fatal("wrong password must fail")
	}
	if _, err := LoadSecure(out, ""); err == nil {
		t.Fatal("missing password must fail for password-encrypted bundle")
	}
	// Tamper: flip last byte, GCM auth must fail.
	raw, _ := os.ReadFile(out)
	raw[len(raw)-1] ^= 0xFF
	tampered := filepath.Join(dir, "tampered.ksx")
	if err := os.WriteFile(tampered, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadSecure(tampered, "s3cr3t"); err == nil {
		t.Fatal("tampered bundle must fail auth")
	}
}

func TestFindPrefersSecureOnTie(t *testing.T) {
	d := t.TempDir()
	writeLib(t, filepath.Join(d, "pkg"), "mylib", "0.1.0", map[string]string{"src/lib.ks": "let x = 1\n"})
	cfg, err := config.Load(filepath.Join(d, "pkg"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Build(cfg, d); err != nil {
		t.Fatal(err)
	}
	if _, err := BuildSecure(cfg, d, ""); err != nil {
		t.Fatal(err)
	}
	got, err := Find("mylib", []string{d})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(got, SecureExt) {
		t.Fatalf("want .ksx to win tie, got %s", got)
	}
}
