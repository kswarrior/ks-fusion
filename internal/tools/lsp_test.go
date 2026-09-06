package tools

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLSPDiagnosticsParseError(t *testing.T) {
	uri := "file:///tmp/lsp-diag-test.ks"
	setOpenDoc(uri, "let x = \nprint 1\n")
	defer dropOpenDoc(uri)
	params := diagnosticsParams(uri)
	diags := params["diagnostics"].([]any)
	if len(diags) == 0 {
		t.Fatal("want parse diagnostic for broken source")
	}
	first := diags[0].(map[string]any)
	if first["severity"] != 1 {
		t.Fatalf("want severity 1, got %v", first["severity"])
	}
	if first["source"] != "fusion-parse" {
		t.Fatalf("want fusion-parse source, got %v", first["source"])
	}
}

func TestLSPDiagnosticsClean(t *testing.T) {
	uri := "file:///tmp/lsp-diag-clean.ks"
	setOpenDoc(uri, "let x = 1\nprint x\n")
	defer dropOpenDoc(uri)
	params := diagnosticsParams(uri)
	if len(params["diagnostics"].([]any)) != 0 {
		t.Fatalf("want no diagnostics, got %v", params["diagnostics"])
	}
}

func TestLSPRenameAcrossFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "fusion.toml"), []byte("[package]\nname = \"r\"\nversion = \"0.1.0\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := filepath.Join(dir, "a.ks")
	b := filepath.Join(dir, "b.ks")
	if err := os.WriteFile(a, []byte("func greet(name) {\n return name\n}\nprint greet(\"x\")\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b, []byte("print greet(\"y\")\nlet greeting = 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	uri := "file://" + filepath.ToSlash(a)
	changes, err := renameEdits(uri, 0, 6, "hello")
	if err != nil {
		t.Fatal(err)
	}
	// def + use in a.ks (2 edits), use in b.ks (1 edit); `greeting` must not match
	ga := changes["file://"+filepath.ToSlash(a)].([]any)
	gb := changes["file://"+filepath.ToSlash(b)].([]any)
	if len(ga) != 2 {
		t.Fatalf("want 2 edits in a.ks, got %d", len(ga))
	}
	if len(gb) != 1 {
		t.Fatalf("want 1 edit in b.ks, got %d", len(gb))
	}
	// verify a range is real (def on line 0)
	r0 := ga[0].(map[string]any)["range"].(map[string]any)
	start := r0["start"].(map[string]any)
	if start["line"] != 0 {
		t.Fatalf("want def at line 0, got %v", start)
	}
}

func TestLSPRenameBadName(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "fusion.toml"), []byte("[package]\nname = \"r\"\nversion = \"0.1.0\"\n"), 0o644)
	a := filepath.Join(dir, "a.ks")
	os.WriteFile(a, []byte("let x = 1\n"), 0o644)
	if _, err := renameEdits("file://"+filepath.ToSlash(a), 0, 4, "not a name"); err == nil {
		t.Fatal("want error for bad new name")
	}
}

func TestLSPFormatting(t *testing.T) {
	uri := "file:///tmp/lsp-fmt-test.ks"
	raw := "let x = 1   \nprint x\n"
	setOpenDoc(uri, raw)
	defer dropOpenDoc(uri)
	edits := formattingEdits(uri)
	if len(edits) != 1 {
		t.Fatalf("want 1 formatting edit, got %d", len(edits))
	}
	newText := edits[0].(map[string]any)["newText"].(string)
	if newText != FormatSource(raw) {
		t.Fatalf("want canonical format, got %q", newText)
	}
	setOpenDoc(uri, FormatSource(raw))
	if got := formattingEdits(uri); len(got) != 0 {
		t.Fatalf("want no edits when clean, got %d", len(got))
	}
}
