package tools

// P4 benches: render / HMR tick / diff / build-js.
// Reproduce: go test ./internal/tools/ -bench 'BenchmarkRender|BenchmarkDiff|BenchmarkBuildJS|BenchmarkHMR' -benchtime 10x -run XXX

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/kswarrior/ks-fusion/internal/config"
)

func benchWriteFiles(b *testing.B, dir string, files map[string]string) {
	b.Helper()
	if err := os.WriteFile(filepath.Join(dir, "fusion.toml"), []byte("[package]\nname = \"b\"\nversion = \"0.1.0\"\nentry_backend = \"backend/main.ks\"\nentry_frontend = \"frontend/main.ks\"\n"), 0o644); err != nil {
		b.Fatal(err)
	}
	for rel, src := range files {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			b.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
			b.Fatal(err)
		}
	}
}

func benchAppConfig(b *testing.B) *config.Config {
	b.Helper()
	// Prefer real hello-app for realistic numbers (layouts+store+components).
	if _, err := os.Stat(filepath.Join("..", "..", "tests", "hello-app", "fusion.toml")); err == nil {
		cfg, err := config.Load(filepath.Join("..", "..", "tests", "hello-app"))
		if err != nil {
			b.Fatal(err)
		}
		return cfg
	}
	dir := b.TempDir()
	benchWriteFiles(b, dir, map[string]string{
		"frontend/pages/home.ks":    "func home_page(props) {\n return {key: \"home\", type: \"page\", props: {title: \"hello\"}, children: []}\n}\n",
		"frontend/pages/hi.ks":      "func hi_page(props) {\n return {key: \"hi\", type: \"text\", props: {title: \"hi\"}, children: []}\n}\n",
		"frontend/pages/user_[id].ks": "func user_page(props) {\n let id = props?.id ?? \"missing\"\n return {key: \"user\", type: \"page\", props: {id: id}, children: []}\n}\n",
	})
	cfg, err := config.Load(dir)
	if err != nil {
		b.Fatal(err)
	}
	return cfg
}

func benchTempApp(b *testing.B) string {
	b.Helper()
	dir := b.TempDir()
	benchWriteFiles(b, dir, map[string]string{
		"frontend/pages/home.ks":    "func home_page(props) {\n return {key: \"home\", type: \"page\", props: {title: \"hello\"}, children: []}\n}\n",
		"frontend/pages/hi.ks":      "func hi_page(props) {\n return {key: \"hi\", type: \"text\", props: {title: \"hi\"}, children: []}\n}\n",
		"frontend/pages/user_[id].ks": "func user_page(props) {\n let id = props?.id ?? \"missing\"\n return {key: \"user\", type: \"page\", props: {id: id}, children: []}\n}\n",
	})
	return dir
}

// BenchmarkRenderRouteRoot measures full render of "/" (uncached exec,
// parse cached after warmup — the TTFR path).
func BenchmarkRenderRouteRoot(b *testing.B) {
	cfg := benchAppConfig(b)
	ResetFrontendCachesForTests()
	if _, err := renderRoute(cfg, "/"); err != nil {
		b.Fatalf("warmup /: %v", err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Bypass route cache to measure true exec cost (HMR miss path);
		// parse cache stays warm (incremental re-parse analogue).
		if _, _, err := renderRouteWithStatusUncached(cfg, "/", nil); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkRenderRouteHi measures full render of "/hi".
func BenchmarkRenderRouteHi(b *testing.B) {
	cfg := benchAppConfig(b)
	ResetFrontendCachesForTests()
	if _, err := renderRoute(cfg, "/hi"); err != nil {
		// hello-app has /hi; temp fixture too. Skip only on unexpected error.
		b.Fatalf("warmup /hi: %v", err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, _, err := renderRouteWithStatusUncached(cfg, "/hi", nil); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkRenderRouteUser measures full render of dynamic "/user/7"
// (P1 props path through the cache layer).
func BenchmarkRenderRouteUser(b *testing.B) {
	cfg := benchAppConfig(b)
	ResetFrontendCachesForTests()
	if _, err := renderRoute(cfg, "/user/7"); err != nil {
		b.Fatalf("warmup /user/7: %v", err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, _, err := renderRouteWithStatusUncached(cfg, "/user/7", nil); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkHMRTickCached measures the debounced tick with no change:
// route-cache hit (hash + lookup, no exec) — the common SSE tick cost.
func BenchmarkHMRTickCached(b *testing.B) {
	cfg := benchAppConfig(b)
	ResetFrontendCachesForTests()
	if _, err := renderRoute(cfg, "/"); err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := renderRoute(cfg, "/"); err != nil {
			b.Fatal(err)
		}
	}
}

func buildLargeVM(n int, changed bool) string {
	kids := make([]any, 0, n)
	for i := 0; i < n; i++ {
		text := "row"
		if changed && i == n/2 {
			text = "row-changed"
		}
		kids = append(kids, map[string]any{
			"key":      fmt.Sprintf("r%d", i),
			"type":     "text",
			"props":    map[string]any{"text": text},
			"children": []any{},
		})
	}
	vm := map[string]any{
		"key":      "home",
		"type":     "page",
		"props":    map[string]any{"title": "big"},
		"children": kids,
	}
	data, _ := json.Marshal(vm)
	return string(data)
}

// BenchmarkDiffLarge200 measures keyed diff over a 200-child VM with a
// single text change (HMR patch path).
func BenchmarkDiffLarge200(b *testing.B) {
	oldVM := buildLargeVM(200, false)
	newVM := buildLargeVM(200, true)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ops, err := DiffViewModels(oldVM, newVM)
		if err != nil {
			b.Fatal(err)
		}
		if len(ops) == 0 {
			b.Fatal("want non-empty ops for changed row")
		}
	}
}

// BenchmarkBuildJS measures per-route transpile+emit (same out dir, so
// content-hash skip applies on writes — parse+emit still run).
func BenchmarkBuildJS(b *testing.B) {
	appDir := benchTempApp(b)
	out := b.TempDir()
	if err := BuildJS(appDir, out); err != nil {
		b.Fatalf("warmup BuildJS: %v", err)
	}
	// Log route JS sizes from manifest (budgets context).
	if data, err := os.ReadFile(filepath.Join(out, "manifest.json")); err == nil {
		b.Logf("manifest: %s", string(data))
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := BuildJS(appDir, out); err != nil {
			b.Fatal(err)
		}
	}
}
