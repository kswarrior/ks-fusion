package tools

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func buildJSFixture(t *testing.T, files map[string]string, strict bool) (string, string) {
	t.Helper()
	cfg := routingFixture(t, files)
	out := t.TempDir()
	var err error
	if strict {
		err = BuildJS(cfg.Dir, out)
	} else {
		err = BuildJSWithOptions(cfg.Dir, out, BuildJSOptions{Strict: false})
	}
	if err != nil {
		t.Fatalf("BuildJS(strict=%v): %v", strict, err)
	}
	return cfg.Dir, out
}

func readManifest(t *testing.T, out string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(out, "manifest.json"))
	if err != nil {
		t.Fatalf("manifest.json missing: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("manifest invalid JSON: %v", err)
	}
	return m
}

func TestBuildJSManifestSizesHashes(t *testing.T) {
	_, out := buildJSFixture(t, map[string]string{
		"frontend/pages/home.ks":    "func home_page(props) {\n return {key: \"home\", type: \"page\", props: {title: \"hello\"}, children: []}\n}\n",
		"frontend/pages/hi.ks":      "func hi_page(props) {\n return {key: \"hi\", type: \"text\", props: {title: \"hi\"}, children: []}\n}\n",
		"frontend/pages/user_[id].ks": "func user_page(props) {\n let id = props?.id ?? \"missing\"\n return {key: \"user\", type: \"page\", props: {id: id}, children: []}\n}\n",
	}, true)
	m := readManifest(t, out)
	for _, route := range []string{"index", "hi", "user_[id]"} {
		raw, ok := m[route]
		if !ok {
			t.Fatalf("manifest missing route %q: %v", route, m)
		}
		entry, _ := raw.(map[string]any)
		if entry == nil {
			t.Fatalf("manifest[%q] not an object: %v", route, raw)
		}
		sizeF, _ := entry["size"].(float64)
		sha, _ := entry["sha256"].(string)
		if sizeF <= 0 || sha == "" {
			t.Fatalf("manifest[%q] bad size/sha: %v", route, entry)
		}
		data, err := os.ReadFile(filepath.Join(out, route+".js"))
		if err != nil {
			t.Fatalf("missing %s.js: %v", route, err)
		}
		if int(sizeF) != len(data) {
			t.Fatalf("manifest[%q].size=%v want %d", route, sizeF, len(data))
		}
		sum := sha256.Sum256(data)
		if hex.EncodeToString(sum[:]) != sha {
			t.Fatalf("manifest[%q].sha mismatch", route)
		}
	}
}

func TestBuildJSTreeShakeExcludesUnused(t *testing.T) {
	_, out := buildJSFixture(t, map[string]string{
		"frontend/pages/home.ks": `func home_page(props) {
  let x = used_helper({})
  let y = shared_used
  return {key: "home", type: "page", props: {v: x, y: y}, children: []}
}
`,
		"frontend/components/helpers.ks": "func used_helper(p) {\n return 1\n}\nfunc unused_helper(p) {\n return 2\n}\n",
		"frontend/store/app.ks":          "let shared_used = \"u\"\nlet shared_unused = \"x\"\n",
		"frontend/layouts/app.ks":        "func app_layout(page) {\n return {key: \"app\", type: \"layout\", props: {}, children: [page]}\n}\n",
		"frontend/layouts/admin.ks":      "func admin_layout(page) {\n return {key: \"admin\", type: \"layout\", props: {}, children: [page]}\n}\n",
	}, true)
	data, err := os.ReadFile(filepath.Join(out, "index.js"))
	if err != nil {
		t.Fatal(err)
	}
	js := string(data)
	for _, want := range []string{"used_helper", "shared_used", "home_page", "app_layout"} {
		if !strings.Contains(js, want) {
			t.Fatalf("want used %q in bundle, got:\n%s", want, js)
		}
	}
	for _, bad := range []string{"unused_helper", "shared_unused", "admin_layout"} {
		if strings.Contains(js, bad) {
			t.Fatalf("tree-shake must exclude %q, got:\n%s", bad, js)
		}
	}
}

func TestBuildJSStrictFailsForCSelect(t *testing.T) {
	// for-c must fail STRICT with file:line + construct.
	cfg := routingFixture(t, map[string]string{
		"frontend/pages/bad.ks": "func bad_page(props) {\n for let i = 0; i < 3; i = i + 1 {\n print i\n }\n return {key: \"bad\", type: \"page\", props: {}, children: []}\n}\n",
	})
	out := t.TempDir()
	err := BuildJS(cfg.Dir, out)
	if err == nil {
		t.Fatal("STRICT for-c must fail")
	}
	msg := err.Error()
	if !strings.Contains(msg, "for-c") {
		t.Fatalf("error must name for-c, got %q", msg)
	}
	if !strings.Contains(msg, "bad.ks") || !strings.Contains(msg, ":") {
		t.Fatalf("error must name file:line, got %q", msg)
	}
	// select must fail STRICT.
	cfg2 := routingFixture(t, map[string]string{
		"frontend/pages/sel.ks": "func sel_page(props) {\n select {\n case timeout(10) { print \"t\" }\n }\n return {key: \"s\", type: \"page\", props: {}, children: []}\n}\n",
	})
	if err := BuildJS(cfg2.Dir, t.TempDir()); err == nil {
		t.Fatal("STRICT select must fail")
	} else if !strings.Contains(err.Error(), "select") {
		t.Fatalf("error must name select, got %q", err)
	}
	// --strict=false preserves old lenient output.
	out3 := t.TempDir()
	if err := BuildJSWithOptions(cfg.Dir, out3, BuildJSOptions{Strict: false}); err != nil {
		t.Fatalf("lenient for-c must not fail: %v", err)
	}
	data, _ := os.ReadFile(filepath.Join(out3, "bad.js"))
	if !strings.Contains(string(data), "for-c") {
		t.Fatalf("lenient output must keep // for-c marker, got:\n%s", data)
	}
	out4 := t.TempDir()
	if err := BuildJSWithOptions(cfg2.Dir, out4, BuildJSOptions{Strict: false}); err != nil {
		t.Fatalf("lenient select must not fail: %v", err)
	}
	data4, _ := os.ReadFile(filepath.Join(out4, "sel.js"))
	if !strings.Contains(string(data4), "unsupported select") {
		t.Fatalf("lenient output must keep // unsupported select, got:\n%s", data4)
	}
}

func TestBuildJSBudgets(t *testing.T) {
	// Fail >250KB.
	big := strings.Repeat("a", 260*1024)
	cfg := routingFixture(t, map[string]string{
		"frontend/pages/big.ks": "func big_page(props) {\n let s = \"" + big + "\"\n return {key: \"big\", type: \"page\", props: {s: s}, children: []}\n}\n",
	})
	if err := BuildJS(cfg.Dir, t.TempDir()); err == nil {
		t.Fatal("budget >250KB must fail")
	} else if !strings.Contains(err.Error(), "budget fail") {
		t.Fatalf("want budget fail error, got %q", err)
	}
	// Warn >100KB but <250KB succeeds.
	mid := strings.Repeat("b", 120*1024)
	cfg2 := routingFixture(t, map[string]string{
		"frontend/pages/mid.ks": "func mid_page(props) {\n let s = \"" + mid + "\"\n return {key: \"mid\", type: \"page\", props: {s: s}, children: []}\n}\n",
	})
	out2 := t.TempDir()
	if err := BuildJS(cfg2.Dir, out2); err != nil {
		t.Fatalf("budget 120KB must only warn, got %v", err)
	}
	fi, err := os.Stat(filepath.Join(out2, "mid.js"))
	if err != nil || fi.Size() <= 100*1024 {
		t.Fatalf("want mid.js >100KB, got %v size=%v", err, fiSize(fi))
	}
}

func fiSize(fi os.FileInfo) int64 {
	if fi == nil {
		return -1
	}
	return fi.Size()
}

func TestBuildJSClassPassthrough(t *testing.T) {
	_, out := buildJSFixture(t, map[string]string{
		"frontend/pages/home.ks": "func home_page(props) {\n return {key: \"c\", type: \"div\", props: {class: \"hero highlight\", className: \"alt\"}, children: []}\n}\n",
	}, true)
	data, _ := os.ReadFile(filepath.Join(out, "index.js"))
	js := string(data)
	if !strings.Contains(js, `"hero highlight"`) {
		t.Fatalf("props.class must pass through untouched, got:\n%s", js)
	}
	if !strings.Contains(js, `"alt"`) {
		t.Fatalf("props.className must pass through untouched, got:\n%s", js)
	}
}

func TestBuildJSDynamicUserID(t *testing.T) {
	_, out := buildJSFixture(t, map[string]string{
		"frontend/pages/home.ks":      "func home_page(props) {\n return {key: \"home\", type: \"page\", props: {}, children: []}\n}\n",
		"frontend/pages/user_[id].ks": "func user_page(props) {\n let id = props?.id ?? \"x\"\n return {key: \"user\", type: \"page\", props: {id: id}, children: []}\n}\n",
	}, true)
	// Filename keeps user_[id].js (documented, not normalized).
	data, err := os.ReadFile(filepath.Join(out, "user_[id].js"))
	if err != nil {
		t.Fatalf("dynamic must emit user_[id].js: %v", err)
	}
	js := string(data)
	if !strings.Contains(js, "user_page") {
		t.Fatalf("dynamic bundle must contain user_page, got:\n%s", js)
	}
	if !strings.Contains(js, "dynamic") {
		t.Fatalf("dynamic bundle must document user_[id].ks convention, got:\n%s", js)
	}
}

func TestBuildJSForInRange(t *testing.T) {
	_, out := buildJSFixture(t, map[string]string{
		"frontend/pages/home.ks": "func home_page(props) {\n let total = 0\n for i in range(3) {\n total = total + i\n }\n return {key: \"home\", type: \"page\", props: {total: total}, children: []}\n}\n",
	}, true)
	data, _ := os.ReadFile(filepath.Join(out, "index.js"))
	js := string(data)
	if !strings.Contains(js, "range(") {
		t.Fatalf("for-in range(n) must survive STRICT, got:\n%s", js)
	}
	if !strings.Contains(js, "function range(") {
		t.Fatalf("range helper must be emitted, got:\n%s", js)
	}
	if !strings.Contains(js, "for (let i of") {
		t.Fatalf("for-in must lower to for..of, got:\n%s", js)
	}
}

func TestBuildJSStylesPassthrough(t *testing.T) {
	cfg := routingFixture(t, map[string]string{
		"frontend/pages/home.ks":    "func home_page(props) {\n return {key: \"home\", type: \"page\", props: {class: \"hero\"}, children: []}\n}\n",
		"frontend/styles/app.css":   ".hero{color:red}\n",
		"frontend/styles/extra.css": ".x{margin:0}\n",
	})
	out := t.TempDir()
	if err := BuildJS(cfg.Dir, out); err != nil {
		t.Fatalf("BuildJS with styles: %v", err)
	}
	for _, name := range []string{"app.css", "extra.css"} {
		data, err := os.ReadFile(filepath.Join(out, "styles", name))
		if err != nil {
			t.Fatalf("styles/%s must be copied: %v", name, err)
		}
		if len(data) == 0 {
			t.Fatalf("styles/%s empty", name)
		}
	}
	m := readManifest(t, out)
	raw, ok := m["styles"]
	if !ok {
		t.Fatalf("manifest must list styles hint, got %v", m)
	}
	arr, _ := raw.([]any)
	if len(arr) != 2 {
		t.Fatalf("manifest styles want 2 entries, got %v", raw)
	}
	// JS must not bundle CSS (no .hero{ in JS).
	js, _ := os.ReadFile(filepath.Join(out, "index.js"))
	if strings.Contains(string(js), ".hero{color") {
		t.Fatal("CSS must not be bundled into JS")
	}
}

func TestBuildJSHelloAppStrictClean(t *testing.T) {
	// Real hello-app pages must build STRICT-clean with tree-shaking.
	appDir := filepath.Join("..", "..", "tests", "hello-app")
	if _, err := os.Stat(filepath.Join(appDir, "fusion.toml")); err != nil {
		t.Skip("hello-app fixture missing")
	}
	out := t.TempDir()
	if err := BuildJS(appDir, out); err != nil {
		t.Fatalf("hello-app STRICT build: %v", err)
	}
	idx, err := os.ReadFile(filepath.Join(out, "index.js"))
	if err != nil {
		t.Fatal(err)
	}
	js := string(idx)
	for _, want := range []string{"home_page", "header_render", "app_title", "app_layout"} {
		if !strings.Contains(js, want) {
			t.Fatalf("hello-app index.js must contain %q", want)
		}
	}
	// Tree-shake: store helpers not used by routes are excluded.
	for _, bad := range []string{"app_fetch_user", "app_state"} {
		if strings.Contains(js, bad) {
			t.Fatalf("hello-app index.js must tree-shake %q", bad)
		}
	}
	// No silent null/unsupported markers in STRICT output.
	if strings.Contains(js, "// unsupported") || strings.Contains(js, "// for-c") {
		t.Fatalf("STRICT output must not contain unsupported markers:\n%s", js)
	}
	// Dynamic route file kept as user_[id].js.
	if _, err := os.Stat(filepath.Join(out, "user_[id].js")); err != nil {
		t.Fatalf("want user_[id].js: %v", err)
	}
}
