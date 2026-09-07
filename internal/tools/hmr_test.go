package tools

// P4 HMR debounce + incremental cache tests.
// Invariants kept: never location.reload, P1 routing props, hydrate JS intact.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)



func TestHMRDebounceSkipsUnchanged(t *testing.T) {
	ResetFrontendCachesForTests()
	cfg := routingFixture(t, map[string]string{
		"frontend/pages/home.ks": "func home_page(props) {\n return {key: \"home\", type: \"page\", props: {title: \"hello\"}, children: []}\n}\n",
	})
	vm1, err := renderRoute(cfg, "/")
	if err != nil {
		t.Fatalf("first render: %v", err)
	}
	hits1, renders1 := RouteCacheStats()
	if renders1 != 1 {
		t.Fatalf("want 1 render after first tick, got renders=%d hits=%d", renders1, hits1)
	}
	// Second tick with no change must skip re-exec (cache hit).
	vm2, err := renderRoute(cfg, "/")
	if err != nil {
		t.Fatalf("second render: %v", err)
	}
	if vm1 != vm2 {
		t.Fatalf("cached VM must be identical")
	}
	hits2, renders2 := RouteCacheStats()
	if hits2 != 1 {
		t.Fatalf("want 1 cache hit after second tick, got hits=%d renders=%d", hits2, renders2)
	}
	if renders2 != 1 {
		t.Fatalf("second tick must not re-render, got renders=%d", renders2)
	}
	// Debounce: coalesced single render per tick — two rapid ticks with no
	// change still total a single render.
	vm3, err := renderRoute(cfg, "/")
	if err != nil {
		t.Fatal(err)
	}
	if vm3 != vm1 {
		t.Fatal("third tick must also hit cache")
	}
	hits3, renders3 := RouteCacheStats()
	if renders3 != 1 || hits3 != 2 {
		t.Fatalf("want renders=1 hits=2 after 3 ticks, got renders=%d hits=%d", renders3, hits3)
	}
}

func TestHMRInvalidatesOnChange(t *testing.T) {
	ResetFrontendCachesForTests()
	cfg := routingFixture(t, map[string]string{
		"frontend/pages/home.ks": "func home_page(props) {\n return {key: \"home\", type: \"page\", props: {title: \"v1\"}, children: []}\n}\n",
	})
	vm1, err := renderRoute(cfg, "/")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(vm1, "v1") {
		t.Fatalf("want v1, got %s", vm1)
	}
	// Ensure mtime advances (coarse FS): sleep + different size guarantees
	// mtime+size hash change for the incremental cache.
	time.Sleep(20 * time.Millisecond)
	pagePath := filepath.Join(cfg.Dir, "frontend", "pages", "home.ks")
	if err := os.WriteFile(pagePath, []byte("func home_page(props) {\n return {key: \"home\", type: \"page\", props: {title: \"v2-changed-longer\"}, children: []}\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Force mtime forward in case FS granularity is coarse.
	now := time.Now().Add(2 * time.Second)
	_ = os.Chtimes(pagePath, now, now)
	vm2, err := renderRoute(cfg, "/")
	if err != nil {
		t.Fatalf("render after change: %v", err)
	}
	if !strings.Contains(vm2, "v2-changed-longer") {
		t.Fatalf("want v2 after change, got %s", vm2)
	}
	_, renders := RouteCacheStats()
	if renders != 2 {
		t.Fatalf("change must re-render (renders=2), got %d", renders)
	}
	ph, pm := FrontendParseCacheStats()
	if pm == 0 {
		t.Fatalf("want at least one parse miss, got hits=%d misses=%d", ph, pm)
	}
}

func TestHMRIncrementalParseReusesUnchanged(t *testing.T) {
	ResetFrontendCachesForTests()
	cfg := routingFixture(t, map[string]string{
		"frontend/pages/home.ks":       "func home_page(props) {\n return {key: \"home\", type: \"page\", props: {title: \"hi\"}, children: []}\n}\n",
		"frontend/components/head.ks":  "func header_render(p) {\n return {key: \"h\", type: \"text\", props: {text: \"h\"}, children: []}\n}\n",
	})
	if _, err := renderRoute(cfg, "/"); err != nil {
		t.Fatal(err)
	}
	h1, m1 := FrontendParseCacheStats()
	if m1 == 0 {
		t.Fatalf("first render must parse-miss, got hits=%d misses=%d", h1, m1)
	}
	// Change only home.ks; head.ks prog must be reused (hit on next render).
	time.Sleep(20 * time.Millisecond)
	pagePath := filepath.Join(cfg.Dir, "frontend", "pages", "home.ks")
	if err := os.WriteFile(pagePath, []byte("func home_page(props) {\n return {key: \"home\", type: \"page\", props: {title: \"hi2-extended\"}, children: []}\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_ = os.Chtimes(pagePath, time.Now().Add(2*time.Second), time.Now().Add(2*time.Second))
	if _, err := renderRoute(cfg, "/?x=1"); err != nil {
		t.Fatal(err)
	}
	h2, m2 := FrontendParseCacheStats()
	if h2 <= h1 {
		t.Fatalf("unchanged file must be parse-cache hit: before hits=%d misses=%d, after hits=%d misses=%d", h1, m1, h2, m2)
	}
}

func TestHMRNewFileAndDelete(t *testing.T) {
	ResetFrontendCachesForTests()
	cfg := routingFixture(t, map[string]string{
		"frontend/pages/home.ks": "func home_page(props) {\n return {key: \"home\", type: \"page\", props: {}, children: []}\n}\n",
	})
	if _, err := renderRoute(cfg, "/newpage"); err == nil {
		t.Fatal("unknown route must 404 before file exists")
	}
	// New file appears: must be picked up (parse miss, render ok).
	newPath := filepath.Join(cfg.Dir, "frontend", "pages", "newpage.ks")
	if err := os.WriteFile(newPath, []byte("func newpage_page(props) {\n return {key: \"newpage\", type: \"page\", props: {}, children: []}\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	vm, err := renderRoute(cfg, "/newpage")
	if err != nil {
		t.Fatalf("new file must render: %v", err)
	}
	if !strings.Contains(vm, "newpage") {
		t.Fatalf("want newpage VM, got %s", vm)
	}
	// Delete: must 404 again (pruned, no stale hit).
	if err := os.Remove(newPath); err != nil {
		t.Fatal(err)
	}
	if _, err := renderRoute(cfg, "/newpage"); err == nil {
		t.Fatal("deleted file must 404 again")
	} else if !isNotFound(err) {
		t.Fatalf("want notFound after delete, got %v", err)
	}
}

func TestHMRKeepsRoutingAndNoReload(t *testing.T) {
	ResetFrontendCachesForTests()
	cfg := routingFixture(t, map[string]string{
		"frontend/pages/home.ks":    "func home_page(props) {\n return {key: \"home\", type: \"page\", props: {title: \"hello\"}, children: []}\n}\n",
		"frontend/pages/user_[id].ks": "func user_page(props) {\n let id = props?.id ?? \"missing\"\n return {key: \"user\", type: \"page\", props: {id: id}, children: []}\n}\n",
	})
	// P1 routing props intact through the cache layer (both cold + hit).
	vm1, err := renderRoute(cfg, "/user/7")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(vm1, `"7"`) {
		t.Fatalf("P1 user_[id] props broken (cold), got %s", vm1)
	}
	vm2, err := renderRoute(cfg, "/user/7")
	if err != nil {
		t.Fatal(err)
	}
	if vm1 != vm2 {
		t.Fatal("cached user route must match cold render")
	}
	// Hydrate JS intact + never location.reload.
	html := vmToHTMLWithWatch(vm1, "/user/7", true)
	for _, want := range []string{"__hydrate", "__applyPatch", "data-key"} {
		if !strings.Contains(html, want) {
			t.Fatalf("hydrate JS missing %q after P4", want)
		}
	}
	if strings.Contains(html, "location.reload") {
		t.Fatal("never-location.reload invariant broken")
	}
}
