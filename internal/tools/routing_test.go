package tools

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kswarrior/ks-fusion/internal/config"
)

// routingFixture builds a minimal app dir with the given frontend files.
// files maps relative path under app dir (e.g. "frontend/pages/home.ks") to content.
func routingFixture(t *testing.T, files map[string]string) *config.Config {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "fusion.toml"), []byte("[package]\nname = \"r\"\nversion = \"0.1.0\"\nentry_backend = \"backend/main.ks\"\nentry_frontend = \"frontend/main.ks\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for rel, src := range files {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cfg, err := config.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func renderJSON(t *testing.T, cfg *config.Config, route string) map[string]any {
	t.Helper()
	vmJSON, err := renderRoute(cfg, route)
	if err != nil {
		t.Fatalf("renderRoute(%q): %v", route, err)
	}
	var v map[string]any
	if err := json.Unmarshal([]byte(vmJSON), &v); err != nil {
		t.Fatalf("vm not JSON for %q: %v (%s)", route, err, vmJSON)
	}
	return v
}

func propsOf(t *testing.T, vm map[string]any) map[string]any {
	t.Helper()
	p, _ := vm["props"].(map[string]any)
	if p == nil {
		t.Fatalf("vm missing props: %v", vm)
	}
	return p
}

func TestRoutingDynamicParam(t *testing.T) {
	cfg := routingFixture(t, map[string]string{
		"frontend/pages/home.ks":    "func home_page(props) {\n return {key: \"home\", type: \"page\", props: {title: \"hello\"}, children: []}\n}\n",
		"frontend/pages/user_[id].ks": "func user_page(props) {\n let id = props?.id ?? \"missing\"\n return {key: \"user\", type: \"page\", props: {id: id}, children: []}\n}\n",
	})
	vm := renderJSON(t, cfg, "/user/7")
	if vm["key"] != "user" {
		t.Fatalf("want key user, got %v", vm)
	}
	if propsOf(t, vm)["id"] != "7" {
		t.Fatalf("want id 7, got %v", vm["props"])
	}
	// generalized foo_[bar].ks -> foo_page with param bar
	cfg2 := routingFixture(t, map[string]string{
		"frontend/pages/home.ks":       "func home_page(props) {\n return {key: \"home\", type: \"page\", props: {}, children: []}\n}\n",
		"frontend/pages/product_[sku].ks": "func product_page(props) {\n let sku = props?.sku ?? \"missing\"\n return {key: \"product\", type: \"page\", props: {sku: sku}, children: []}\n}\n",
	})
	vm2 := renderJSON(t, cfg2, "/product/abc123")
	if vm2["key"] != "product" {
		t.Fatalf("want product, got %v", vm2)
	}
	if propsOf(t, vm2)["sku"] != "abc123" {
		t.Fatalf("want sku abc123, got %v", vm2["props"])
	}
}

func TestRoutingQueryParsing(t *testing.T) {
	cfg := routingFixture(t, map[string]string{
		"frontend/pages/home.ks": "func home_page(props) {\n return {key: \"home\", type: \"page\", props: {title: \"hello\"}, children: []}\n}\n",
		"frontend/pages/echo.ks": "func echo_page(props) {\n let a = props?.query?.a ?? \"missing\"\n let b = props?.query?.b ?? \"missing\"\n return {key: \"echo\", type: \"page\", props: {a: a, b: b}, children: []}\n}\n",
	})
	vm := renderJSON(t, cfg, "/echo?a=1&b=2")
	p := propsOf(t, vm)
	if p["a"] != "1" || p["b"] != "2" {
		t.Fatalf("want query a=1 b=2, got %v", p)
	}
	// ?format=json still renders JSON (handler-level), props carry query too
	vm2, status, err := renderRouteWithStatus(cfg, "/echo?a=1&format=json")
	if err != nil {
		t.Fatalf("withStatus: %v", err)
	}
	if status != 200 {
		t.Fatalf("want 200, got %d", status)
	}
	var v2 map[string]any
	if err := json.Unmarshal([]byte(vm2), &v2); err != nil {
		t.Fatal(err)
	}
}

func TestRoutingBackwardsCompat(t *testing.T) {
	cfg := routingFixture(t, map[string]string{
		"frontend/pages/home.ks": "func home_page(props) {\n return {key: \"home\", type: \"page\", props: {title: \"hello\"}, children: []}\n}\n",
		"frontend/pages/hi.ks":   "func hi_page(props) {\n return {key: \"hi\", type: \"text\", props: {title: \"hi\"}, children: []}\n}\n",
	})
	if vm := renderJSON(t, cfg, "/"); vm["key"] != "home" {
		t.Fatalf("want home for /, got %v", vm)
	}
	if vm := renderJSON(t, cfg, "/hi"); vm["key"] != "hi" {
		t.Fatalf("want hi for /hi, got %v", vm)
	}
	// /user/* falls back to home_page when no user_[id].ks exists (200)
	vm := renderJSON(t, cfg, "/user/7")
	if vm["key"] != "home" {
		t.Fatalf("want home fallback for /user/7, got %v", vm)
	}
	// /<name> -> <name>_page
	cfg2 := routingFixture(t, map[string]string{
		"frontend/pages/home.ks":  "func home_page(props) {\n return {key: \"home\", type: \"page\", props: {}, children: []}\n}\n",
		"frontend/pages/about.ks": "func about_page(props) {\n return {key: \"about\", type: \"page\", props: {}, children: []}\n}\n",
	})
	if vm := renderJSON(t, cfg2, "/about"); vm["key"] != "about" {
		t.Fatalf("want about, got %v", vm)
	}
}

func TestRouting404(t *testing.T) {
	// no 404_page: renderRoute returns 404 error (not 500)
	cfg := routingFixture(t, map[string]string{
		"frontend/pages/home.ks": "func home_page(props) {\n return {key: \"home\", type: \"page\", props: {}, children: []}\n}\n",
	})
	if _, err := renderRoute(cfg, "/nope"); err == nil {
		t.Fatal("want 404 error for unknown route")
	} else if !isNotFound(err) {
		t.Fatalf("want notFound error, got %v", err)
	}
	if _, status, err := renderRouteWithStatus(cfg, "/nope"); err == nil || status != 404 {
		t.Fatalf("want 404 status+err, got status=%d err=%v", status, err)
	}
	// HTTP: 404 status, JSON error body, X-Render-Time preserved
	mux, stop := buildWebMux(cfg, newWebWatcher(cfg.Dir), newISRCache(), false)
	defer stop()
	srv := httptest.NewServer(mux)
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/nope")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 404 {
		t.Fatalf("want HTTP 404, got %d (%s)", resp.StatusCode, body)
	}
	if rt := resp.Header.Get("X-Render-Time"); rt == "" {
		t.Fatal("want X-Render-Time preserved on 404")
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Fatalf("want JSON error body, got Content-Type %q body %q", ct, body)
	}
	var eb map[string]any
	if err := json.Unmarshal(body, &eb); err != nil || eb["error"] == nil {
		t.Fatalf("want {\"error\":...}, got %q err=%v", body, err)
	}
	// with 404_page (notfound_page alias): renders fallback VM but HTTP 404
	cfg2 := routingFixture(t, map[string]string{
		"frontend/pages/home.ks": "func home_page(props) {\n return {key: \"home\", type: \"page\", props: {}, children: []}\n}\n",
		"frontend/pages/404.ks":  "func notfound_page(props) {\n let path = props?.path ?? \"missing\"\n return {key: \"notfound\", type: \"page\", props: {path: path}, children: []}\n}\n",
	})
	vmJSON, status, err := renderRouteWithStatus(cfg2, "/nope")
	if err != nil {
		t.Fatalf("404_page should render, got err %v", err)
	}
	if status != 404 {
		t.Fatalf("want 404 status for 404_page render, got %d", status)
	}
	var vm map[string]any
	if err := json.Unmarshal([]byte(vmJSON), &vm); err != nil {
		t.Fatal(err)
	}
	// NOTE: hello-app wraps pages in app_layout when present; here no layout,
	// so key stays notfound. With app_layout present the outer key is "app".
	if vm["key"] != "notfound" {
		t.Fatalf("want notfound vm, got %v", vm)
	}
	mux2, stop2 := buildWebMux(cfg2, newWebWatcher(cfg2.Dir), newISRCache(), false)
	defer stop2()
	srv2 := httptest.NewServer(mux2)
	defer srv2.Close()
	resp2, err := http.Get(srv2.URL + "/nope?format=json")
	if err != nil {
		t.Fatal(err)
	}
	body2, _ := io.ReadAll(resp2.Body)
	resp2.Body.Close()
	if resp2.StatusCode != 404 {
		t.Fatalf("want HTTP 404 for 404_page, got %d (%s)", resp2.StatusCode, body2)
	}
	if rt := resp2.Header.Get("X-Render-Time"); rt == "" {
		t.Fatal("want X-Render-Time on 404_page response")
	}
}

func TestRoutingExplicitLayout(t *testing.T) {
	cfg := routingFixture(t, map[string]string{
		"frontend/pages/home.ks": "func home_page(props) {\n return {key: \"home\", type: \"page\", props: {}, children: []}\n}\n",
		"frontend/pages/admin.ks": "func admin_page(props) {\n return {key: \"admin\", type: \"page\", props: {layout: \"admin\"}, children: []}\n}\n",
		"frontend/layouts/admin.ks": "func admin_layout(page) {\n return {key: \"admin-wrap\", type: \"layout\", props: {}, children: [page]}\n}\n",
		"frontend/layouts/app.ks":   "func app_layout(page) {\n return {key: \"app\", type: \"layout\", props: {}, children: [page]}\n}\n",
	})
	vm := renderJSON(t, cfg, "/admin")
	// explicit layout wins: outer key admin-wrap (returns early, not app)
	if vm["key"] != "admin-wrap" {
		t.Fatalf("want admin-wrap, got %v", vm)
	}
	kids, _ := vm["children"].([]any)
	if len(kids) != 1 {
		t.Fatalf("want 1 child, got %v", vm)
	}
	if kid, _ := kids[0].(map[string]any); kid["key"] != "admin" {
		t.Fatalf("want inner admin, got %v", kids)
	}
}

func TestRoutingSSGSkipsDynamic(t *testing.T) {
	cfg := routingFixture(t, map[string]string{
		"frontend/pages/home.ks":      "func home_page(props) {\n return {key: \"home\", type: \"page\", props: {}, children: []}\n}\n",
		"frontend/pages/hi.ks":        "func hi_page(props) {\n return {key: \"hi\", type: \"page\", props: {}, children: []}\n}\n",
		"frontend/pages/user_[id].ks": "func user_page(props) {\n let id = props?.id ?? \"x\"\n return {key: \"user\", type: \"page\", props: {id: id}, children: []}\n}\n",
	})
	_ = cfg
	// reuse cfg.Dir as app dir for BuildSSG
	out := t.TempDir()
	// find app dir via config
	c2, err := config.Load(cfg.Dir)
	if err != nil {
		t.Fatal(err)
	}
	_ = c2
	if err := BuildSSG(cfg.Dir, out); err != nil {
		t.Fatalf("BuildSSG must not fail on dynamic: %v", err)
	}
	if _, err := os.Stat(filepath.Join(out, "index.html")); err != nil {
		t.Fatalf("want index.html, got %v", err)
	}
	// dynamic must not produce a static file
	for _, bad := range []string{"user_[id].html", "user_[id].json"} {
		if _, err := os.Stat(filepath.Join(out, bad)); err == nil {
			t.Fatalf("dynamic %s must be skipped", bad)
		}
	}
}
