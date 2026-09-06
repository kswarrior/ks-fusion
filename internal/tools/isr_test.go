package tools

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kswarrior/ks-fusion/internal/config"
)

func bufioReader(r io.Reader) *bufio.Reader { return bufio.NewReader(r) }

func vmWithRevalidate(title string, secs float64) string {
	return fmt.Sprintf(`{"key":"home","type":"page","props":{"title":%q,"revalidate":%v},"children":[]}`, title, secs)
}

func TestISRPutGetExpiry(t *testing.T) {
	c := newISRCache()
	vm := vmWithRevalidate("hi", 50)
	c.put("/", "html", "<html>hi</html>", vm)
	if hit, body, ctype := c.get("/", "html"); !hit || body != "<html>hi</html>" || ctype != "text/html; charset=utf-8" {
		t.Fatalf("want fresh HIT, got hit=%v %q %q", hit, body, ctype)
	}
	if _, _, ok := c.getStale("/", "html"); !ok {
		t.Fatal("want stale entry present")
	}
	// no-revalidate VMs are never cached
	c.put("/x", "json", `{}`, `{"key":"a","type":"t"}`)
	if hit, _, _ := c.get("/x", "json"); hit {
		t.Fatal("want miss for VM without revalidate")
	}
}

func TestISRBackgroundRegen(t *testing.T) {
	c := newISRCache()
	calls := 0
	c.regen = func(route, format string) (string, string, string, bool) {
		calls++
		return fmt.Sprintf("body-%d", calls), "text/html; charset=utf-8", vmWithRevalidate("t", 5), true
	}
	c.put("/r", "html", "body-0", vmWithRevalidate("t", 0.15))
	stop := c.startBackground(20*time.Millisecond, time.Hour)
	defer stop()
	deadline := time.Now().Add(3 * time.Second)
	for {
		c.mu.Lock()
		body := c.m["/r?format=html"].body
		c.mu.Unlock()
		if body != "body-0" {
			return // background loop refreshed the entry
		}
		if time.Now().After(deadline) {
			t.Fatalf("background regen did not refresh (calls=%d)", calls)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestISRStaleWhileRevalidate(t *testing.T) {
	c := newISRCache()
	refreshed := make(chan struct{}, 1)
	c.regen = func(route, format string) (string, string, string, bool) {
		select {
		case refreshed <- struct{}{}:
		default:
		}
		return "fresh", "text/html; charset=utf-8", vmWithRevalidate("t", 50), true
	}
	c.put("/s", "html", "stale", vmWithRevalidate("t", 0.05))
	time.Sleep(120 * time.Millisecond) // let it expire
	if hit, _, _ := c.get("/s", "html"); hit {
		t.Fatal("want miss after expiry")
	}
	staleBody, _, ok := c.getStale("/s", "html")
	if !ok || staleBody != "stale" {
		t.Fatalf("want stale body, got %q %v", staleBody, ok)
	}
	c.kickRefresh("/s", "html")
	select {
	case <-refreshed:
	case <-time.After(3 * time.Second):
		t.Fatal("kickRefresh did not run regen")
	}
}

func webFixture(t *testing.T) *config.Config {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "fusion.toml"), []byte("[package]\nname = \"w\"\nversion = \"0.1.0\"\nentry_backend = \"backend/main.ks\"\nentry_frontend = \"frontend/main.ks\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "frontend", "pages"), 0o755); err != nil {
		t.Fatal(err)
	}
	page := "func home_page(props) {\n return {key: \"home\", type: \"page\", props: {title: \"hello\"}, children: []}\n}\n"
	if err := os.WriteFile(filepath.Join(dir, "frontend", "pages", "home.ks"), []byte(page), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestWebNoReloadFallback(t *testing.T) {
	cfg := webFixture(t)
	mux, stop := buildWebMux(cfg, newWebWatcher(cfg.Dir), newISRCache(), true)
	defer stop()
	srv := httptest.NewServer(mux)
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	html := string(body)
	if strings.Contains(html, "location.reload") {
		t.Fatal("served HTML must not contain a reload fallback")
	}
	for _, want := range []string{"__applyPatch", "__currentVM", "data-key"} {
		if !strings.Contains(html, want) {
			t.Fatalf("served HTML missing %q", want)
		}
	}
}

func TestSSEPatchMessage(t *testing.T) {
	cfg := webFixture(t)
	watcher := newWebWatcher(cfg.Dir)
	mux, stop := buildWebMux(cfg, watcher, newISRCache(), true)
	defer stop()
	srv := httptest.NewServer(mux)
	defer srv.Close()
	// prime baseline through the JSON endpoint
	resp, err := http.Get(srv.URL + "/?format=json")
	if err != nil {
		t.Fatal(err)
	}
	base, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	var baseline map[string]any
	if err := json.Unmarshal(base, &baseline); err != nil {
		t.Fatalf("baseline not JSON: %v", err)
	}
	// subscribe first, then change the page twice: full vm, then keyed ops
	pagePath := filepath.Join(cfg.Dir, "frontend", "pages", "home.ks")
	type result struct {
		resp *http.Response
		err  error
	}
	done := make(chan result, 1)
	go func() {
		req, _ := http.NewRequest("GET", srv.URL+"/events?route=/", nil)
		resp, err := http.DefaultClient.Do(req)
		done <- result{resp, err}
	}()
	time.Sleep(200 * time.Millisecond) // let the handler subscribe (last=ver=0)
	writePage := func(title string) {
		t.Helper()
		if err := os.WriteFile(pagePath, []byte("func home_page(props) {\n return {key: \"home\", type: \"page\", props: {title: \""+title+"\"}, children: []}\n}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		watcher.ver++
	}
	var resp2 *http.Response
	readFrame := func() map[string]any {
		t.Helper()
		var frame strings.Builder
		br := bufioReader(resp2.Body)
		for {
			line, err := br.ReadString('\n')
			if err != nil {
				t.Fatalf("SSE frame: %v", err)
			}
			if strings.TrimSpace(line) == "" {
				break
			}
			frame.WriteString(line)
		}
		first := frame.String()
		if !strings.HasPrefix(first, "data: ") {
			t.Fatalf("want SSE data frame, got %q", first[:min(64, len(first))])
		}
		payload := strings.TrimSpace(strings.TrimPrefix(first, "data: "))
		if strings.Contains(payload, "location.reload") {
			t.Fatal("SSE must not carry reload fallback")
		}
		var msg map[string]any
		if err := json.Unmarshal([]byte(payload), &msg); err != nil {
			t.Fatalf("SSE payload not JSON: %v (%q)", err, payload[:min(120, len(payload))])
		}
		return msg
	}
	select {
	case r := <-done:
		if r.err != nil {
			t.Fatal(r.err)
		}
		resp2 = r.resp
	case <-time.After(5 * time.Second):
		t.Fatal("SSE subscribe timed out")
	}
	defer resp2.Body.Close()
	writePage("changed")
	msg1 := readFrame()
	vm1, hasVM := msg1["vm"].(map[string]any)
	if !hasVM {
		t.Fatalf("first message must carry full vm, got %v", msg1)
	}
	if props, _ := vm1["props"].(map[string]any); props["title"] != "changed" {
		t.Fatalf("want title changed, got %v", vm1["props"])
	}
	writePage("changed2")
	msg2 := readFrame()
	ops, hasOps := msg2["ops"].([]any)
	if !hasOps || len(ops) == 0 {
		t.Fatalf("second message must carry keyed ops, got %v", msg2)
	}
	foundProp := false
	for _, o := range ops {
		if om, ok := o.(map[string]any); ok && om["op"] == "setProp" {
			foundProp = true
		}
	}
	if !foundProp {
		t.Fatalf("want setProp op for title change, got %v", ops)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
