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

// P2 hydrate-full + CSR (STRICT): SSR keeps data-key, client hydrates in place.
// Routing table (P1) untouched: user_[id].ks -> user_page still asserted by routing_test.go.

func TestHydrateSSRDataKeyAndEscape(t *testing.T) {
	cfg := routingFixture(t, map[string]string{
		"frontend/pages/home.ks": "func home_page(props) {\n return {key: \"home\", type: \"page\", props: {title: \"<script>alert(1)</script>\", class: \"hero\", id: \"home-id\", href: \"/hi\", text: \"hi <b>there</b>\"}, children: [{key: \"c1\", type: \"text\", props: {text: \"kid\"}, children: []}]}\n}\n",
	})
	vmJSON, err := renderRoute(cfg, "/")
	if err != nil {
		t.Fatal(err)
	}
	html := vmToHTMLWithWatch(vmJSON, "/", false)
	// SSR keeps data-key attributes (not an empty div#app).
	if !strings.Contains(html, `data-key="home"`) {
		t.Fatalf("SSR HTML must keep data-key home, got:\n%s", html[:min(2000, len(html))])
	}
	if !strings.Contains(html, `data-key="c1"`) {
		t.Fatalf("SSR HTML must keep child data-key c1")
	}
	// #vm JSON present.
	if !strings.Contains(html, `id="vm"`) {
		t.Fatal("SSR HTML must contain #vm JSON script")
	}
	// __hydrate present (real hydrate, not empty build).
	if !strings.Contains(html, "__hydrate") {
		t.Fatal("SSR HTML must contain __hydrate")
	}
	// Escape: raw <script>alert must not appear as markup outside the vm JSON/script tags.
	// Strip the #vm JSON block and script blocks, then check the SSR #app region.
	// Simplest robust check: SSR-escaped title must appear as &lt;script&gt;.
	if !strings.Contains(html, "&lt;script&gt;") {
		t.Fatalf("SSR must escape <script> in title/text, got:\n%s", html[:min(3000, len(html))])
	}
	// The SSR #app div must not contain a raw executable <script>alert(1)</script> element.
	// (The #vm JSON + client JS legitimately contain the word "script" in code.)
	appStart := strings.Index(html, `<div id="app"`)
	appEnd := strings.Index(html, `<script id="vm"`)
	if appStart < 0 || appEnd < 0 || appEnd <= appStart {
		t.Fatal("cannot locate #app region")
	}
	appRegion := html[appStart:appEnd]
	if strings.Contains(appRegion, "<script>alert") {
		t.Fatalf("SSR #app region must not contain raw <script>, got:\n%s", appRegion)
	}
	// Full props: class/id/href as real attrs, text escaped.
	for _, want := range []string{`class="hero"`, `id="home-id"`, `href="/hi"`, `hi &lt;b&gt;there&lt;/b&gt;`} {
		if !strings.Contains(appRegion, want) {
			t.Fatalf("SSR #app missing full-prop %q in:\n%s", want, appRegion)
		}
	}
	// Text rendering must use textContent/createTextNode semantics, never innerHTML
	// except the allowlisted js_call("set_html", literal) html slot.
	if !strings.Contains(html, "textContent") {
		t.Fatal("client JS must use textContent for escaped text")
	}
	// Only one code assignment to innerHTML (the allowlisted props.html slot);
	// comments may mention innerHTML for documentation.
	if c := strings.Count(html, ".innerHTML ="); c != 1 {
		t.Fatalf("want exactly 1 allowlisted innerHTML assignment (props.html slot), got %d", c)
	}
	if !strings.Contains(html, `js_call("set_html", literal)`) {
		t.Fatal("client JS must document allowlisted js_call(\"set_html\", literal) slot")
	}
	// Never a reload fallback.
	if strings.Contains(html, "location.reload") {
		t.Fatal("served HTML must not contain location.reload")
	}
}

func TestHydrateClientSnippetActionMount(t *testing.T) {
	cfg := routingFixture(t, map[string]string{
		"frontend/pages/home.ks": "func home_page(props) {\n return {key: \"home\", type: \"page\", props: {title: \"hi\", on_click: \"refresh\"}, children: []}\n}\n",
	})
	vmJSON, err := renderRoute(cfg, "/")
	if err != nil {
		t.Fatal(err)
	}
	html := vmToHTMLWithWatch(vmJSON, "/", false)
	for _, want := range []string{
		"__hydrate", "__action", "__mounts", "__applyPatch", "__renderVM",
		"__expandMore", "data-action", "on_mount", "fetch_json",
		"use_state", "set_state", "__csrRefresh", "DOMContentLoaded",
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("client snippet missing %q", want)
		}
	}
	// SSR renders on_click as data-action.
	if !strings.Contains(html, `data-action="refresh"`) {
		t.Fatalf("SSR must render on_click as data-action, got:\n%s", html[:min(2000, len(html))])
	}
	// Full-prop fallback: data-prop-* for unknown props.
	cfg2 := routingFixture(t, map[string]string{
		"frontend/pages/home.ks": "func home_page(props) {\n return {key: \"home\", type: \"page\", props: {title: \"hi\", src: \"/a.png\", value: \"v\", placeholder: \"ph\", custom: \"cb\"}, children: []}\n}\n",
	})
	vm2, err := renderRoute(cfg2, "/")
	if err != nil {
		t.Fatal(err)
	}
	html2 := vmToHTMLWithWatch(vm2, "/", false)
	for _, want := range []string{`src="/a.png"`, `value="v"`, `placeholder="ph"`, `data-prop-custom="cb"`} {
		if !strings.Contains(html2, want) {
			t.Fatalf("SSR full props missing %q in:\n%s", want, html2[:min(2500, len(html2))])
		}
	}
	// Virtualized Show more uses the same action path (no reload).
	many := `func home_page(props) {
 let kids = []
 for i in range(150) { kids = kids + [{key: "r" + str(i), type: "text", props: {text: "row"}, children: []}] }
 return {key: "home", type: "page", props: {title: "big"}, children: kids}
}
`
	cfg3 := routingFixture(t, map[string]string{"frontend/pages/home.ks": many})
	vm3, err := renderRoute(cfg3, "/")
	if err != nil {
		t.Fatalf("big list render: %v", err)
	}
	html3 := vmToHTMLWithWatch(vm3, "/", false)
	if !strings.Contains(html3, "show_more") || !strings.Contains(html3, "Show more (100/150)") {
		t.Fatalf("virtualized expander must use show_more action path, got:\n%s", html3[:min(3000, len(html3))])
	}
	if strings.Contains(html3, "location.reload") {
		t.Fatal("expander path must not reload")
	}
}

func TestHydrateCSRRefetch(t *testing.T) {
	cfg := routingFixture(t, map[string]string{
		"frontend/pages/home.ks":    "func home_page(props) {\n return {key: \"home\", type: \"page\", props: {title: \"hello\"}, children: []}\n}\n",
		"frontend/pages/user_[id].ks": "func user_page(props) {\n let id = props?.id ?? \"missing\"\n return {key: \"user\", type: \"page\", props: {id: id}, children: []}\n}\n",
	})
	mux, stop := buildWebMux(cfg, newWebWatcher(cfg.Dir), newISRCache(), false)
	defer stop()
	srv := httptest.NewServer(mux)
	defer srv.Close()
	// / returns SSR HTML with hydrated #app (P1 routing intact).
	resp, err := http.Get(srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	html := string(body)
	if !strings.Contains(html, `data-key="home"`) || !strings.Contains(html, "__hydrate") {
		t.Fatalf("GET / must be hydrated SSR, got:\n%s", html[:min(2000, len(html))])
	}
	// ?format=json returns the same VM (CSR re-fetch target).
	resp2, err := http.Get(srv.URL + "/?format=json")
	if err != nil {
		t.Fatal(err)
	}
	jbody, _ := io.ReadAll(resp2.Body)
	resp2.Body.Close()
	var vm map[string]any
	if err := json.Unmarshal(jbody, &vm); err != nil {
		t.Fatalf("CSR JSON invalid: %v (%s)", err, jbody)
	}
	if vm["key"] != "home" {
		t.Fatalf("want home VM, got %v", vm)
	}
	// P1 dynamic route still works alongside hydrate (do not break).
	resp3, err := http.Get(srv.URL + "/user/7?format=json")
	if err != nil {
		t.Fatal(err)
	}
	ubody, _ := io.ReadAll(resp3.Body)
	resp3.Body.Close()
	var uvm map[string]any
	if err := json.Unmarshal(ubody, &uvm); err != nil {
		t.Fatal(err)
	}
	if props, _ := uvm["props"].(map[string]any); props["id"] != "7" {
		t.Fatalf("P1 user_[id] CSR broken, want id 7 got %v", uvm)
	}
}

func TestHydrateAPIUserEcho(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "fusion.toml"), []byte("[package]\nname = \"a\"\nversion = \"0.1.0\"\nentry_backend = \"backend/main.ks\"\nentry_frontend = \"frontend/main.ks\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Fixture mirrors tests/hello-app/backend/api/user.ks (func api_user(req) -> {id,name}).
	const userAPI = "func api_user(req) {\n let q = req?.query ?? {}\n let id = q?.id ?? \"unknown\"\n return {id: id, name: \"user-\" + id}\n}\n"
	for _, p := range []string{filepath.Join(dir, "backend", "api"), filepath.Join(dir, "frontend", "pages")} {
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "backend", "api", "user.ks"), []byte(userAPI), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "frontend", "pages", "home.ks"), []byte("func home_page(props) {\n return {key: \"home\", type: \"page\", props: {}, children: []}\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	// Direct route call with query.
	out, err := runAPIRouteWithQuery(cfg, "user", map[string]string{"id": "7"})
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		t.Fatalf("api user not JSON: %v (%s)", err, out)
	}
	if m["id"] != "7" || m["name"] != "user-7" {
		t.Fatalf("want id echo 7/user-7, got %v", m)
	}
	// HTTP path: /api/user?id=7 (usable from fetch_json shim).
	mux, stop := buildWebMux(cfg, newWebWatcher(cfg.Dir), newISRCache(), false)
	defer stop()
	srv := httptest.NewServer(mux)
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/api/user?id=7")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	var hm map[string]any
	if err := json.Unmarshal(body, &hm); err != nil {
		t.Fatalf("/api/user not JSON: %v (%s)", err, body)
	}
	if hm["id"] != "7" {
		t.Fatalf("want HTTP id echo 7, got %v", hm)
	}
}

func TestHydrateFetchJSONGetOnly(t *testing.T) {
	// Server fetch_json is GET-only: json_parse(http_get(url)) (stdlib_ext.go).
	// Client shim mirrors it with a shape check (must be object/array, else banner).
	cfg := routingFixture(t, map[string]string{
		"frontend/pages/home.ks": "func home_page(props) {\n return {key: \"home\", type: \"page\", props: {}, children: []}\n}\n",
	})
	vmJSON, err := renderRoute(cfg, "/")
	if err != nil {
		t.Fatal(err)
	}
	html := vmToHTMLWithWatch(vmJSON, "/", false)
	if !strings.Contains(html, "fetch_json") {
		t.Fatal("client must ship fetch_json shim")
	}
	if !strings.Contains(html, "want object or array") {
		t.Fatal("fetch_json shim must validate shape (object/array) with banner error")
	}
	if !strings.Contains(html, "GET-only") {
		t.Fatal("fetch_json shim must document GET-only")
	}
}
