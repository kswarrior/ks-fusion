package tools

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/kswarrior/ks-fusion/internal/backend"
	"github.com/kswarrior/ks-fusion/internal/config"
	"github.com/kswarrior/ks-fusion/internal/frontend"
)

// RunWeb serves frontend/ as SSR HTML+JSON (v2.2 P2 step).
// Routes: / -> frontend/pages/home.ks home_page, /hi -> hi_page, /api/* -> backend/api/*.ks
func RunWeb(appDir string, port int) error { return RunWebWithWatch(appDir, port, false) }

// RunWebWithWatch adds --watch polling + SSE keyed patches (v2.5 DOM-diff).
func RunWebWithWatch(appDir string, port int, watch bool) error {
	cfg, err := config.Load(appDir)
	if err != nil {
		return err
	}
	watcher := newWebWatcher(cfg.Dir)
	if watch {
		go watcher.loop()
	}
	mux := http.NewServeMux()
	isr := newISRCache()
	_ = attachWebRoutes(mux, cfg, watcher, isr, watch)
	addr := fmt.Sprintf(":%d", port)
	extra := "SSR + /api/*, ?format=json"
	if watch {
		extra += ", --watch SSE keyed patches"
	}
	fmt.Printf("ks-fusion web: serving %s at http://localhost%s (%s)\n", cfg.Name, addr, extra)
	return http.ListenAndServe(addr, mux)
}

// buildWebMux assembles the web routes (extracted for httptest coverage).
// It returns the mux plus a stop func for the background ISR loop.
func buildWebMux(cfg *config.Config, watcher *webWatcher, isr *isrCache, watch bool) (*http.ServeMux, func()) {
	mux := http.NewServeMux()
	stop := attachWebRoutes(mux, cfg, watcher, isr, watch)
	return mux, stop
}

func attachWebRoutes(mux *http.ServeMux, cfg *config.Config, watcher *webWatcher, isr *isrCache, watch bool) func() {
	isr.regen = func(route, format string) (string, string, string, bool) {
		vmJSON, err := renderRoute(cfg, route)
		if err != nil {
			return "", "", "", false
		}
		if format == "json" {
			return vmJSON, "application/json", vmJSON, true
		}
		return vmToHTMLWithWatchDir(vmJSON, route, watch, cfg.Dir), "text/html; charset=utf-8", vmJSON, true
	}
	mux.HandleFunc("/events", func(w http.ResponseWriter, r *http.Request) {
		route := r.URL.Query().Get("route")
		if route == "" {
			route = "/"
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		fl, _ := w.(http.Flusher)
		if fl != nil {
			fl.Flush() // commit headers so subscribers don't hang
		}
		last := watcher.version()
		var lastSent string
		// P4: sseTickInterval kept at 300ms (TODO WS push). Debounce: at most
		// one renderRoute+Diff per tick per route — multiple watcher.version
		// bumps within the same tick window coalesce into this single render
		// (last=ver after the tick). Unchanged routes additionally hit the
		// per-route render cache inside renderRoute (content-hash analogue).
		tick := time.NewTicker(sseTickInterval)
		defer tick.Stop()
		for {
			select {
			case <-r.Context().Done():
				return
			case <-tick.C:
				if v := watcher.version(); v != last {
					// HMR patch (v2.5): keyed server diff, client patches DOM.
					tickStart := time.Now()
					vmJSON, err := renderRoute(cfg, route)
					maybeLogSlowHMRTick(tickStart, route)
					if err != nil {
						// render broken: tell the client to banner, never force-reload
						fmt.Fprintf(w, "data: %s\n\n", `{"reload":true}`)
					} else if lastSent == "" {
						fmt.Fprintf(w, "data: %s\n\n", `{"vm":`+vmJSON+`}`)
						lastSent = vmJSON
					} else if ops, derr := DiffViewModels(lastSent, vmJSON); derr != nil {
						fmt.Fprintf(w, "data: %s\n\n", `{"vm":`+vmJSON+`}`)
						lastSent = vmJSON
					} else {
						opsJSON, _ := json.Marshal(ops)
						fmt.Fprintf(w, "data: %s\n\n", `{"ops":`+string(opsJSON)+`,"vm":`+vmJSON+`}`)
						lastSent = vmJSON
					}
					if fl != nil {
						fl.Flush()
					}
					last = v
				}
			}
		}
	})
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, "/api/")
		name = strings.Trim(name, "/")
		if name == "" {
			http.Error(w, "missing api name", 404)
			return
		}
		// pass query as map
		q := map[string]string{}
		for k, vs := range r.URL.Query() {
			if len(vs) > 0 {
				q[k] = vs[0]
			}
		}
		out, err := runAPIRouteWithQuery(cfg, name, q)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(out))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		format := r.URL.Query().Get("format")
		// ISR (v2.4 TTL, v2.5 background regen): fresh HIT, else revalidate in
		// background and serve stale while it refreshes.
		if hit, body, ctype := isr.get(r.URL.Path, format); hit {
			w.Header().Set("X-Cache", "HIT")
			w.Header().Set("Content-Type", ctype)
			_, _ = w.Write([]byte(body))
			return
		}
		if staleBody, staleType, ok := isr.getStale(r.URL.Path, format); ok {
			isr.kickRefresh(r.URL.Path, format)
			w.Header().Set("X-Cache", "STALE")
			w.Header().Set("Content-Type", staleType)
			_, _ = w.Write([]byte(staleBody))
			return
		}
		// P1: pass full path+query so page funcs receive props {path, query, params}.
		fullRoute := r.URL.Path
		if r.URL.RawQuery != "" {
			fullRoute += "?" + r.URL.RawQuery
		}
		vmJSON, status, err := renderRouteWithStatus(cfg, fullRoute)
		el := time.Since(start)
		w.Header().Set("X-Render-Time", el.String())
		maybeLogSlowTTFR(start, fullRoute)
		if err != nil {
			// 404 (unknown route, no 404_page): JSON error body, status 404.
			// X-Render-Time already set above and preserved.
			if isNotFound(err) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusNotFound)
				body, _ := json.Marshal(map[string]string{"error": err.Error()})
				_, _ = w.Write(body)
				return
			}
			// serve stale on render failure when available (no error page flip)
			if staleBody, staleType, ok := isr.getStale(r.URL.Path, format); ok {
				w.Header().Set("X-Cache", "STALE")
				w.Header().Set("Content-Type", staleType)
				_, _ = w.Write([]byte(staleBody))
				return
			}
			http.Error(w, err.Error(), 500)
			return
		}
		if r.URL.Query().Get("format") == "json" {
			w.Header().Set("Content-Type", "application/json")
			isr.put(r.URL.Path, "json", vmJSON, vmJSON)
			if status == http.StatusNotFound {
				w.WriteHeader(http.StatusNotFound)
			}
			_, _ = w.Write([]byte(vmJSON))
			return
		}
		html := vmToHTMLWithWatchDir(vmJSON, r.URL.Path, watch, cfg.Dir)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		isr.put(r.URL.Path, "html", html, vmJSON)
		if status == http.StatusNotFound {
			w.WriteHeader(http.StatusNotFound)
		}
		_, _ = w.Write([]byte(html))
	})
	// background ISR regen (v2.5): refresh entries expiring within 10s, every 5s
	return isr.startBackground(5*time.Second, 10*time.Second)
}

// routeNotFoundError signals HTTP 404 (unknown route, no 404_page).
// Distinct from 500 render errors so handlers return 404 + JSON body.
type routeNotFoundError struct {
	route string
	msg   string
}

func (e *routeNotFoundError) Error() string { return e.msg }

func isNotFound(err error) bool {
	if err == nil {
		return false
	}
	var nf *routeNotFoundError
	if errors.As(err, &nf) {
		return true
	}
	// Backwards compat: pre-P1 "unknown route" errors are 404, not 500.
	if strings.Contains(err.Error(), "unknown route") {
		return true
	}
	return false
}

// Routing table (P1 STRICT, user_[id].ks convention approved):
//
//	File                        -> Func        -> Route         -> Props
//	frontend/pages/home.ks      -> home_page   -> /             -> {path, query, params:{}}
//	frontend/pages/hi.ks        -> hi_page     -> /hi           -> {path, query, params:{}}
//	frontend/pages/user_[id].ks -> user_page   -> /user/7       -> {id:"7", path:"/user/7", query:{...}, params:{id:"7"}}
//	frontend/pages/foo_[bar].ks -> foo_page    -> /foo/<bar>    -> {bar:...} (generalized)
//	/<name>                     -> <name>_page -> /<name>       -> {path, query, params:{}}
//	/user/* (no user_[id].ks)   -> home_page   -> /user/*       -> backwards-compat fallback (200)
//	unknown                     -> 404_page    -> (any)         -> {path, query, params} with HTTP 404
//	unknown (no 404_page)       -> 404 error                   -> JSON {"error":...} with HTTP 404
//
// Choice documented: file `foo_[bar].ks` defines func `foo_page` (NOT
// `foo_[bar]_page`), matching the existing `home.ks` -> `home_page` pattern
// (file stem minus dynamic suffix + "_page"). Nested [id] folders deferred.
func splitRouteQuery(route string) (path, rawQuery string) {
	if route == "" {
		return "/", ""
	}
	if i := strings.Index(route, "?"); i >= 0 {
		p := route[:i]
		q := route[i+1:]
		if p == "" {
			p = "/"
		}
		return p, q
	}
	return route, ""
}

func normalizePath(p string) string {
	if p == "" {
		return "/"
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	for len(p) > 1 && strings.HasSuffix(p, "/") {
		p = strings.TrimSuffix(p, "/")
	}
	for strings.Contains(p, "//") {
		p = strings.ReplaceAll(p, "//", "/")
	}
	return p
}

func parseQueryMap(rawQuery string) map[string]string {
	out := map[string]string{}
	if rawQuery == "" {
		return out
	}
	// Include ALL query keys verbatim (including format=json): format still
	// controls HTML-vs-JSON response in the handler, but remains visible in
	// props.query for transparency. First value wins (matches /api/ behavior).
	if vals, err := url.ParseQuery(rawQuery); err == nil {
		for k, vs := range vals {
			if len(vs) > 0 {
				out[k] = vs[0]
			} else {
				out[k] = ""
			}
		}
		return out
	}
	for _, part := range strings.Split(rawQuery, "&") {
		if part == "" {
			continue
		}
		kv := strings.SplitN(part, "=", 2)
		k := kv[0]
		v := ""
		if len(kv) == 2 {
			v = kv[1]
		}
		if uk, err := url.QueryUnescape(k); err == nil {
			k = uk
		}
		if uv, err := url.QueryUnescape(v); err == nil {
			v = uv
		}
		if _, exists := out[k]; !exists {
			out[k] = v
		}
	}
	return out
}

// parseDynamicFile parses a pages-dir stem like "user_[id]" into
// prefix "user", param "id", func "user_page". ok=false if not dynamic.
func parseDynamicFile(base string) (prefix, param, funcName string, ok bool) {
	idx := strings.Index(base, "_[")
	if idx < 0 {
		return "", "", "", false
	}
	prefix = base[:idx]
	rest := base[idx+2:]
	end := strings.Index(rest, "]")
	if end < 0 {
		return "", "", "", false
	}
	param = rest[:end]
	if prefix == "" || param == "" {
		return "", "", "", false
	}
	if strings.Contains(param, "/") || strings.Contains(param, "[") {
		return "", "", "", false
	}
	cleanPrefix := strings.ReplaceAll(prefix, "-", "_")
	return prefix, param, cleanPrefix + "_page", true
}

// findDynamicRoute scans frontend/pages for foo_[bar].ks whose prefix matches
// seg0 (dash-insensitive). Returns func foo_page + param name.
func findDynamicRoute(pagesDir, seg0 string) (funcName, paramName string, ok bool) {
	ents, err := os.ReadDir(pagesDir)
	if err != nil {
		return "", "", false
	}
	cleanSeg := strings.ReplaceAll(seg0, "-", "_")
	for _, e := range ents {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".ks") {
			continue
		}
		base := strings.TrimSuffix(e.Name(), ".ks")
		prefix, param, fn, isDyn := parseDynamicFile(base)
		if !isDyn {
			continue
		}
		if strings.ReplaceAll(prefix, "-", "_") == cleanSeg {
			return fn, param, true
		}
	}
	return "", "", false
}

func unescapeSegment(s string) string {
	if u, err := url.PathUnescape(s); err == nil {
		return u
	}
	return s
}

func buildPropsValue(path string, query, params map[string]string) backend.Value {
	qm := map[string]backend.Value{}
	for k, v := range query {
		qm[k] = backend.StrV(v)
	}
	pm := map[string]backend.Value{}
	for k, v := range params {
		pm[k] = backend.StrV(v)
	}
	top := map[string]backend.Value{
		"path":   backend.StrV(path),
		"query":  backend.MapV(qm),
		"params": backend.MapV(pm),
	}
	// Flatten path params top-level for convenience (props.id).
	for k, v := range params {
		if _, exists := top[k]; !exists {
			top[k] = backend.StrV(v)
		}
	}
	return backend.MapV(top)
}

// lookup404Func returns the 404 handler func value.
// Tries "404_page" per spec, then "notfound_page" alias.
// NOTE: `func 404_page` is currently a .ks parse error (identifiers may not
// start with a digit: `want '(', got "404"`), so the alias is the working
// path until the parser allows leading-digit idents. Both are tried.
func lookup404Func(in *backend.Interpreter) (backend.Value, bool) {
	if fn, ok := in.Lookup("404_page"); ok {
		return fn, true
	}
	if fn, ok := in.Lookup("notfound_page"); ok {
		return fn, true
	}
	return backend.Value{}, false
}

func renderRoute(cfg *config.Config, route string) (string, error) {
	vmJSON, _, err := renderRouteWithStatus(cfg, route)
	return vmJSON, err
}

// renderRouteWithStatus renders route (which may include "?a=1&b=2").
// Returns (vmJSON, httpStatus, error): status is 200 on page hit,
// 404 when the 404_page fallback rendered, error is non-nil only on failure
// (404 error when no page nor 404_page, 500 otherwise).
//
// P4: per-route render cache (route -> {vmJSON, status, mtimeHash}) so
// unchanged routes skip re-exec on HMR ticks (content-hash incremental
// cache analogue). Failures are never cached. Incremental parse reuses
// cached progs for unchanged files (mtime+size).
func renderRouteWithStatus(cfg *config.Config, route string) (string, int, error) {
	dir := cfg.Dir
	// P4: list once, hash exec set, check route cache before executing.
	allFiles := listFrontendFiles(dir)
	execFiles := execFrontendFiles(dir, allFiles)
	hash := frontendMtimeHash(execFiles)
	if vmJSON, status, ok := routeCacheGet(dir, route, hash); ok {
		return vmJSON, status, nil
	}
	vmJSON, status, err := renderRouteWithStatusUncached(cfg, route, allFiles)
	if err != nil {
		return "", status, err
	}
	routeCachePut(dir, route, hash, vmJSON, status)
	// Prune deleted files from the parse cache (correctness on deletes).
	live := map[string]bool{}
	for _, f := range allFiles {
		live[f] = true
	}
	pruneFrontendParseCache(filepath.Join(dir, "frontend"), live)
	return vmJSON, status, nil
}

// renderRouteWithStatusUncached is the original render path (parse via
// incremental cache, exec fresh). allFiles may be nil (then it lists).
func renderRouteWithStatusUncached(cfg *config.Config, route string, allFiles []string) (string, int, error) {
	// Load store + components + pages on demand, run page funcs via interpreter.
	// Simple: parse frontend/main.ks deps? Instead directly run page funcs.
	dir := cfg.Dir
	rawPath, rawQuery := splitRouteQuery(route)
	path := normalizePath(rawPath)
	queryMap := parseQueryMap(rawQuery)
	// collect all frontend .ks files to load into one interpreter
	files := allFiles
	if files == nil {
		files = listFrontendFiles(dir)
	}
	sort.Strings(files)
	in := backend.New()
	// set ROUTE env for main.ks compat (path only, no query)
	_ = os.Setenv("ROUTE", path)
	for _, f := range files {
		// skip main.ks (it runs route table with prints); load libs only
		if filepath.Base(f) == "main.ks" && filepath.Dir(f) == filepath.Join(dir, "frontend") {
			continue
		}
		// P4 incremental parse: reuse cached prog when mtime+size match.
		prog, err := getCachedFrontendProgram(f, f)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			// Preserve original behaviour: unreadable files are skipped,
			// parse errors fail the render.
			if _, statErr := os.Stat(f); statErr != nil {
				continue
			}
			return "", http.StatusInternalServerError, err
		}
		// exec with baseDir so imports inside work
		// use ExecProgram directly to keep same interpreter globals
		in.SetBaseDir(dir)
		if err := in.ExecProgram(prog); err != nil {
			return "", http.StatusInternalServerError, fmt.Errorf("%s: %w", f, err)
		}
	}
	// pick page func + path params (P1 dynamic routing)
	funcName := "home_page"
	params := map[string]string{}
	if path == "/" {
		funcName = "home_page"
	} else {
		trimmed := strings.Trim(path, "/")
		segs := strings.Split(trimmed, "/")
		if len(segs) == 1 {
			clean := strings.ReplaceAll(segs[0], "-", "_")
			funcName = clean + "_page"
		} else if len(segs) == 2 {
			first, second := segs[0], unescapeSegment(segs[1])
			if fn, paramName, ok := findDynamicRoute(filepath.Join(dir, "frontend", "pages"), first); ok {
				// /user/7 with user_[id].ks -> user_page with {id:"7"}
				funcName = fn
				params = map[string]string{paramName: second}
			} else if first == "user" {
				// backwards compat: /user/* falls back to home_page when
				// no user_[id].ks exists (200, not 404).
				funcName = "home_page"
				params = map[string]string{"id": second}
			} else {
				clean := strings.ReplaceAll(strings.ReplaceAll(trimmed, "-", "_"), "/", "_")
				funcName = clean + "_page"
			}
		} else {
			// try dynamic: /<name> -> <name>_page (multi-segment joins with _)
			clean := strings.Trim(path, "/")
			clean = strings.ReplaceAll(clean, "-", "_")
			clean = strings.ReplaceAll(clean, "/", "_")
			funcName = clean + "_page"
		}
	}
	propsVal := buildPropsValue(path, queryMap, params)
	// Use eval via backend: call func value from globals
	fnVal, ok := in.Lookup(funcName)
	if !ok {
		// 404: unknown route tries <name>_page; if missing, try 404_page.
		if fn404, ok404 := lookup404Func(in); ok404 {
			vm404, err := in.Call(fn404, []backend.Value{propsVal})
			if err != nil {
				return "", http.StatusInternalServerError, err
			}
			vm404 = applyLayouts(in, vm404)
			j, err := backend.ValueToJSONable(vm404)
			if err != nil {
				return "", http.StatusInternalServerError, err
			}
			data, err := json.Marshal(j)
			if err != nil {
				return "", http.StatusInternalServerError, err
			}
			return string(data), http.StatusNotFound, nil
		}
		return "", http.StatusNotFound, &routeNotFoundError{
			route: path,
			msg:   fmt.Sprintf("404 page not found: %q (no %s nor 404_page)", path, funcName),
		}
	}
	vm, err := in.Call(fnVal, []backend.Value{propsVal})
	if err != nil {
		return "", http.StatusInternalServerError, err
	}
	// Nested layouts (v2.4, Next.js analogue): wrap page with layout funcs.
	// Convention: page vm may carry `layout: "admin"` -> call admin_layout(page);
	// else if app_layout exists, wrap once (layouts/app.ks). _app.ks wraps all when present.
	vm = applyLayouts(in, vm)
	j, err := backend.ValueToJSONable(vm)
	if err != nil {
		return "", http.StatusInternalServerError, err
	}
	data, err := json.Marshal(j)
	if err != nil {
		return "", http.StatusInternalServerError, err
	}
	return string(data), http.StatusOK, nil
}

func applyLayouts(in *backend.Interpreter, vm backend.Value) backend.Value {
	// explicit layout field wins
	if name := vmLayoutName(vm); name != "" {
		if fn, ok := in.Lookup(name + "_layout"); ok {
			if out, err := in.Call(fn, []backend.Value{vm}); err == nil {
				return out
			} else {
				fmt.Printf("layout %s_layout failed: %v\n", name, err)
			}
		}
	}
	// _app.ks global wrapper (nested) then app_layout
	for _, lname := range []string{"_app_layout", "app_layout"} {
		if fn, ok := in.Lookup(lname); ok {
			if out, err := in.Call(fn, []backend.Value{vm}); err == nil {
				// only adopt if looks like view-model (map with type/children)
				if isViewModel(out) {
					vm = out
				}
			} else {
				fmt.Printf("layout %s failed: %v\n", lname, err)
			}
		}
	}
	return vm
}

func vmLayoutName(vm backend.Value) string {
	j, err := backend.ValueToJSONable(vm)
	if err != nil {
		return ""
	}
	if m, ok := j.(map[string]any); ok {
		if l, ok := m["layout"].(string); ok {
			return l
		}
		if props, ok := m["props"].(map[string]any); ok {
			if l, ok := props["layout"].(string); ok {
				return l
			}
		}
	}
	return ""
}

func isViewModel(v backend.Value) bool {
	j, err := backend.ValueToJSONable(v)
	if err != nil {
		return false
	}
	m, ok := j.(map[string]any)
	if !ok {
		return false
	}
	_, hasType := m["type"]
	_, hasChildren := m["children"]
	_, hasKey := m["key"]
	return hasType || hasChildren || hasKey
}

func runAPIRoute(cfg *config.Config, name string) (string, error) {
	return runAPIRouteWithQuery(cfg, name, nil)
}

func runAPIRouteWithQuery(cfg *config.Config, name string, query map[string]string) (string, error) {
	// Convention (v2.3): backend/api/<name>.ks defines func api_<name>(req) -> map.
	// req = {query: {...}}. If func missing, fallback: run file, return {"ok":true}.
	p := filepath.Join(cfg.Dir, "backend", "api", name+".ks")
	if _, err := os.Stat(p); err != nil {
		return "", fmt.Errorf("unknown api %q", name)
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return "", err
	}
	prog, err := frontend.ParseSource(string(data), p)
	if err != nil {
		return "", err
	}
	in := backend.New()
	in.SetBaseDir(cfg.Dir)
	if err := in.ExecProgram(prog); err != nil {
		return "", err
	}
	fnName := "api_" + strings.ReplaceAll(strings.ReplaceAll(name, "-", "_"), "/", "_")
	if fn, ok := in.Lookup(fnName); ok {
		qm := map[string]backend.Value{}
		for k, v := range query {
			qm[k] = backend.StrV(v)
		}
		req := backend.MapV(map[string]backend.Value{"query": backend.MapV(qm), "path": backend.StrV("/api/" + name)})
		out, err := in.Call(fn, []backend.Value{req})
		if err != nil {
			return "", err
		}
		j, err := backend.ValueToJSONable(out)
		if err != nil {
			return "", err
		}
		b, _ := json.Marshal(j)
		return string(b), nil
	}
	return `{"ok":true}`, nil
}

func vmToHTML(vmJSON, route string) string { return vmToHTMLWithWatch(vmJSON, route, false) }

// --- P2 hydrate-full SSR helpers (additive, routing table untouched) ---

// tagForVMType maps a view-model type to an HTML tag. Known HTML tags pass
// through; component/page/layout types fall back to div (data-type preserved).
// Client JS mirrors this map exactly so SSR + hydrate agree on tags.
func tagForVMType(t string) string {
	switch t {
	case "a", "button", "img", "input", "span", "p", "h1", "h2", "h3", "h4",
		"ul", "li", "header", "footer", "section", "article", "nav", "main",
		"form", "label", "textarea", "select", "option", "table", "tr", "td", "div":
		return t
	case "text":
		return "span"
	default:
		return "div"
	}
}

func isVoidTag(tag string) bool {
	switch tag {
	case "img", "input", "br", "hr":
		return true
	default:
		return false
	}
}

// propAttrString stringifies a VM prop for a data-prop-* attribute.
// Scalars use JS-String-compatible forms; objects use canonical JSON
// (mirrors client JSON.stringify for objects).
func propAttrString(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case bool:
		if x {
			return "true"
		}
		return "false"
	case float64, float32, int, int64, int32:
		b, err := json.Marshal(v)
		if err == nil {
			return string(b)
		}
		return fmt.Sprintf("%v", v)
	default:
		b, err := json.Marshal(v)
		if err == nil {
			return string(b)
		}
		return fmt.Sprintf("%v", v)
	}
}

// renderVMNodeToHTML renders one VM node (and children) to SSR HTML.
// Escapes all text/attrs via html.EscapeString (never raw except the
// allowlisted props.html slot, which only literal strings reach because
// `fusion vet` frontend-set-html rejects non-literals).
func renderVMNodeToHTML(v any) string {
	m, ok := v.(map[string]any)
	if !ok {
		return ""
	}
	key, _ := m["key"].(string)
	typ, _ := m["type"].(string)
	if typ == "" {
		typ = "div"
	}
	tag := tagForVMType(typ)
	props, _ := m["props"].(map[string]any)
	if props == nil {
		props = map[string]any{}
	}
	children, _ := m["children"].([]any)

	var b strings.Builder
	b.WriteString("<" + tag)
	if key != "" {
		b.WriteString(` data-key="` + html.EscapeString(key) + `"`)
	}
	b.WriteString(` data-type="` + html.EscapeString(typ) + `"`)
	if c, ok := props["class"].(string); ok && c != "" {
		b.WriteString(` class="` + html.EscapeString(c) + `"`)
	}
	if id, ok := props["id"].(string); ok && id != "" {
		b.WriteString(` id="` + html.EscapeString(id) + `"`)
	}
	if href, ok := props["href"].(string); ok && href != "" {
		b.WriteString(` href="` + html.EscapeString(href) + `"`)
	}
	if src, ok := props["src"].(string); ok && src != "" {
		b.WriteString(` src="` + html.EscapeString(src) + `"`)
	}
	if val, ok := props["value"]; ok && val != nil {
		if s, ok := val.(string); ok {
			b.WriteString(` value="` + html.EscapeString(s) + `"`)
		} else {
			b.WriteString(` value="` + html.EscapeString(propAttrString(val)) + `"`)
		}
	}
	if ph, ok := props["placeholder"].(string); ok && ph != "" {
		b.WriteString(` placeholder="` + html.EscapeString(ph) + `"`)
	}
	if alt, ok := props["alt"].(string); ok && alt != "" {
		b.WriteString(` alt="` + html.EscapeString(alt) + `"`)
	}
	action := ""
	if a, ok := props["on_click"].(string); ok && a != "" {
		action = a
	}
	if a, ok := props["onClick"].(string); ok && a != "" {
		action = a
	}
	if action != "" {
		b.WriteString(` data-action="` + html.EscapeString(action) + `"`)
	}
	// data-prop-* fallback for the rest (sorted for determinism).
	skip := map[string]bool{
		"title": true, "text": true, "html": true,
		"class": true, "id": true, "href": true, "src": true,
		"value": true, "placeholder": true, "alt": true,
		"on_click": true, "onClick": true,
	}
	var keys []string
	for k := range props {
		if !skip[k] {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	for _, k := range keys {
		sv := propAttrString(props[k])
		b.WriteString(` data-prop-` + html.EscapeString(k) + `="` + html.EscapeString(sv) + `"`)
	}
	if isVoidTag(tag) {
		b.WriteString(">")
		return b.String()
	}
	b.WriteString(">")
	if t, ok := props["title"].(string); ok && t != "" {
		b.WriteString("<h1>" + html.EscapeString(t) + "</h1>")
	}
	if tx, ok := props["text"].(string); ok && tx != "" {
		b.WriteString(`<span class="txt">` + html.EscapeString(tx) + `</span>`)
	}
	if h, ok := props["html"].(string); ok && h != "" {
		// Allowlisted raw HTML slot only: non-literal values are rejected by
		// `fusion vet` (frontend-set-html), so reaching here means a literal.
		b.WriteString(h)
	}
	renderChromeBlocks(&b, typ, key, props)
	b.WriteString(`<div class="kids">`)
	if len(children) > 100 {
		for i := 0; i < 100; i++ {
			b.WriteString(renderVMNodeToHTML(children[i]))
		}
		moreKey := key + ":more"
		b.WriteString(`<button data-key="` + html.EscapeString(moreKey) + `" data-action="show_more">Show more (100/` + fmt.Sprintf("%d", len(children)) + `)</button>`)
	} else {
		for _, c := range children {
			b.WriteString(renderVMNodeToHTML(c))
		}
	}
	b.WriteString(`</div>`)
	b.WriteString("</" + tag + ">")
	return b.String()
}

// renderChromeBlocks emits visible content for well-known prop shapes so
// run-web pages look like a real website instead of bare titles:
//
//	props.links [{path,label}]  -> nav with real <a> links
//	props.items [{path,label,active}] -> side nav links
//	props.rows [strings]        -> visible <ul> list
//	props.label/value           -> label + value spans
//	props.h/p                   -> <h2> + <p>
//
// Blocks trigger on props presence (not node type), so .ks components may
// use any semantic type. All text is escaped. Containers carry data-key
// "{key}:nav|:rows|:statbody|:secbody" so the client renderer
// (build/paintProps/hydrate) finds and reconciles the exact same structure
// — SSR and client must stay identical.
func renderChromeBlocks(b *strings.Builder, typ, key string, props map[string]any) {
	activeOf := func(item map[string]any) bool {
		if a, ok := item["active"].(bool); ok && a {
			return true
		}
		if cur, ok := props["active"].(string); ok && cur != "" {
			if p, ok := item["path"].(string); ok && p == cur {
				return true
			}
		}
		return false
	}
	renderNav := func(cls, ckey string, items []any) {
		b.WriteString(`<nav class="` + cls + `" data-key="` + html.EscapeString(ckey) + `">`)
		for i, it := range items {
			m, ok := it.(map[string]any)
			if !ok {
				continue
			}
			path, _ := m["path"].(string)
			label, _ := m["label"].(string)
			if path == "" && label == "" {
				continue
			}
			if label == "" {
				label = path
			}
			cls := ""
			if activeOf(m) {
				cls = ` class="active"`
			}
			b.WriteString(`<a href="` + html.EscapeString(path) + `" data-key="` +
				html.EscapeString(fmt.Sprintf("%s-%d", ckey, i)) + `"` + cls + `>` +
				html.EscapeString(label) + `</a>`)
		}
		b.WriteString(`</nav>`)
	}
	// Each block triggers on props presence (any semantic type works).
	// links and items render independently so a block may carry both.
	if links, ok := props["links"].([]any); ok && len(links) > 0 {
		renderNav("nav", key+":nav", links)
	}
	if items, ok := props["items"].([]any); ok && len(items) > 0 {
		cls := "sidenav"
		if typ != "sidebar" && typ != "nav" && typ != "header" && typ != "div" && typ != "layout" && typ != "page" {
			cls = "nav"
		}
		if _, hasLinks := props["links"]; !hasLinks && cls == "sidenav" {
			renderNav(cls, key+":nav", items)
		} else if hasLinks, _ := props["links"]; hasLinks == nil {
			renderNav(cls, key+":nav", items)
		} else {
			renderNav(cls, key+":nav-items", items)
		}
	}

// vmToSSRHTML renders a VM JSON doc to SSR inner HTML for #app.
func vmToSSRHTML(vmJSON string) string {
	var v any
	if err := json.Unmarshal([]byte(vmJSON), &v); err != nil {
		return ""
	}
	return renderVMNodeToHTML(v)
}

// initialStateJSON embeds the backend use_state snapshot for hydration.
// Divergence (documented): backend globalState is a process-global Go map
// populated during SSR renders; client __state is a per-browser copy
// snapshotted at SSR time. Client set_state updates only the browser copy
// and schedules a CSR re-fetch (?format=json + patch); it never writes back
// to the backend map. This avoids cross-client leakage and keeps CSR snappy.
func initialStateJSON() string {
	snap := backend.SnapshotGlobalState()
	if len(snap) == 0 {
		return "{}"
	}
	b, err := json.Marshal(snap)
	if err != nil {
		return "{}"
	}
	return string(b)
}

// fusionDefaultCSS is the built-in run-web stylesheet (no dependencies).
// Layout is driven by data-type selectors so SSR and the client renderer
// (which only reproduces props, never extra classes) always agree.
func fusionDefaultCSS() string {
	return `*{box-sizing:border-box}
body{margin:0;font-family:system-ui,-apple-system,"Segoe UI",Roboto,sans-serif;background:#eef1f7;color:#1b2333}
div[data-type="layout"]>div.kids{display:flex;flex-wrap:wrap;min-height:100vh;align-content:flex-start}
header[data-type="header"]{flex:1 1 100%;background:#16213a;color:#fff;padding:14px 22px}
header[data-type="header"] h1{margin:0 0 10px;font-size:22px}
nav.nav{display:flex;gap:6px;flex-wrap:wrap}
nav.nav a{color:#cdd8f3;text-decoration:none;padding:7px 14px;border-radius:8px;font-size:14px}
nav.nav a.active{background:#3b5bd6;color:#fff}
nav.nav a:hover{background:#2a3f7d;color:#fff}
nav.sidenav{flex:0 0 210px;background:#fff;border-right:1px solid #e3e8f5;padding:14px;display:flex;flex-direction:column;gap:6px}
nav.sidenav a{color:#33415e;text-decoration:none;padding:9px 12px;border-radius:8px;font-size:14px}
nav.sidenav a.active{background:#e7edff;color:#1f3fb8;font-weight:600}
nav.sidenav a:hover{background:#f0f3fb}
div[data-type="page"]{flex:1 1 0;min-width:280px;padding:22px}
div[data-type="page"] h1{margin:0 0 6px;font-size:26px}
ul.rows{list-style:none;margin:14px 0;padding:0;display:grid;gap:8px}
ul.rows li{background:#fff;border:1px solid #e3e8f5;border-radius:8px;padding:10px 14px}
div[data-type="page"]>div.kids{display:grid;grid-template-columns:repeat(auto-fill,minmax(150px,1fr));gap:12px;margin-top:6px}
div[data-type="stat"]{min-width:0}
.statbody{background:#16213a;color:#fff;border-radius:10px;padding:12px 14px;display:flex;flex-direction:column;gap:4px}
.stat-label{font-size:11px;text-transform:uppercase;letter-spacing:.08em;color:#9fb0d8}
.stat-value{font-size:24px;font-weight:700}
div[data-type="section"]{grid-column:1/-1;min-width:0}
.secbody{background:#fff;border:1px solid #e3e8f5;border-radius:10px;padding:14px 18px}
.secbody h2{margin:0 0 6px;font-size:17px}
.secbody p{margin:0;color:#475569}
#fusion-banner{background:#b91c1c;color:#fff;padding:10px 16px}`
}

// appExtraCSS inlines an app's own frontend/styles/*.css after the defaults
// (no static file server needed). Files containing a literal </style are
// skipped so markup can never break out of the style block.
func appExtraCSS(appDir string) string {
	if appDir == "" {
		return ""
	}
	ents, err := os.ReadDir(filepath.Join(appDir, "frontend", "styles"))
	if err != nil {
		return ""
	}
	var names []string
	for _, e := range ents {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".css") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	var b strings.Builder
	for _, n := range names {
		data, err := os.ReadFile(filepath.Join(appDir, "frontend", "styles", n))
		if err != nil {
			continue
		}
		if strings.Contains(string(data), "</style") {
			continue
		}
		b.WriteString("\n/* " + n + " */\n")
		b.WriteString(string(data))
	}
	return b.String()
}

// vmToHTMLWithWatchDir renders a full HTML page including app CSS.
// vmToHTMLWithWatch keeps the old signature for tests and older callers.
func vmToHTMLWithWatchDir(vmJSON, route string, watch bool, appDir string) string {
	html := vmToHTMLWithWatch(vmJSON, route, watch)
	css := fusionDefaultCSS() + appExtraCSS(appDir)
	return strings.Replace(html, "/*FUSION_CSS*/", css, 1)
}

func vmToHTMLWithWatch(vmJSON, route string, watch bool) string {
	var v any
	_ = json.Unmarshal([]byte(vmJSON), &v)
	pretty, _ := json.MarshalIndent(v, "", "  ")
	prettyStr := strings.ReplaceAll(string(pretty), "</", "<\\/")
	title := "ks-fusion"
	if m, ok := v.(map[string]any); ok {
		if props, ok := m["props"].(map[string]any); ok {
			if t, ok := props["title"].(string); ok && t != "" {
				title = t
			}
		}
	}
	titleEsc := html.EscapeString(title)
	routeEsc := html.EscapeString(route)
	ssrHTML := vmToSSRHTML(vmJSON)
	stateJSON := initialStateJSON()
	watchScript := ""
	if watch {
		watchScript = `<script>
// v2.5 watch: SSE keyed patches applied as DOM diff (no reload path).
var es = new EventSource('/events?route=' + encodeURIComponent("` + route + `"));
es.onmessage = function(e){
  try{
    var msg = JSON.parse(e.data);
    if(msg.ops && msg.vm && window.__applyPatch){ window.__currentVM = msg.vm; document.getElementById('vm').textContent = JSON.stringify(msg.vm); window.__applyPatch(msg.ops); }
    else if(msg.vm && window.__renderVM){ window.__renderVM(msg.vm, document.getElementById('app')); }
    else if(msg.reload){ window.__banner('stale client: refresh to update'); }
  }catch(err){ window.__banner('update failed: ' + err.message); }
};
</script>`
	}
	return fmt.Sprintf(`<!doctype html>
<html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>%s</title><style>/*FUSION_CSS*/</style></head>
<body>
<div id="app" data-route="%s">%s</div>
<script id="vm" type="application/json">%s</script>
<script>
// P2 hydrate-full + CSR (STRICT): SSR keeps data-key; hydrate walks existing DOM.
// Safety: escape all text by default via textContent and createTextNode semantics,
// never innerHTML except the allowlisted props.html slot (js_call("set_html", literal)
// only: only literal strings reach it because fusion vet frontend-set-html rejects
// non-literals; server already vets).
(function(){
  var vm = JSON.parse(document.getElementById('vm').textContent);
  var app = document.getElementById('app');
  // Divergence from backend global map (stdlib_ext2.go): backend use_state is a
  // process-global Go map populated during SSR; client __state is a per-browser
  // snapshot embedded at SSR time. Client set_state updates only the browser copy
  // and schedules a CSR re-fetch plus patch; it never writes back to backend.
  var __state = %s;
  var __byKey = {};
  var __mounts = [];
  var __csrScheduled = false;
  window.__currentVM = vm;
  window.use_state = function(k, init){ if(!(k in __state)) __state[k]=init; return __state[k]; };
  window.set_state = function(k, v){ __state[k]=v; if(!__csrScheduled){ __csrScheduled=true; setTimeout(function(){ __csrScheduled=false; if(window.__csrRefresh) window.__csrRefresh(); },0); } return v; };
  window.on_mount = function(fn){ __mounts.push(fn); };
  window.fetch_json = function(path){
    // fetch_json GET-only (server: json_parse(http_get(url))): validates JSON shape.
    return fetch(path).then(function(r){ if(!r.ok) throw new Error('fetch_json failed status '+r.status); return r.json(); }).then(function(data){
      if(data!==null && typeof data==='object') return data;
      window.__banner('fetch_json shape invalid for '+path+': want object or array');
      throw new Error('fetch_json shape invalid');
    });
  };
  window.__banner = function(msg){
    var b = document.getElementById('fusion-banner');
    if(!b){ b = document.createElement('div'); b.id = 'fusion-banner'; b.setAttribute('role','alert'); document.body.insertBefore(b, document.body.firstChild); }
    b.textContent = msg;
  };
  function csrUrl(){
    var path = window.location.pathname || '/';
    var q = window.location.search || '';
    if(q.indexOf('format=json')>=0) return path+q;
    if(q) return path+q+'&format=json';
    return path+'?format=json';
  }
  window.__csrRefresh = function(){
    fetch(csrUrl()).then(function(r){ if(!r.ok) throw new Error('csr refresh status '+r.status); return r.json(); }).then(function(newVM){
      window.__hydrate(newVM);
    }).catch(function(e){ window.__banner('csr failed: '+e.message); });
  };
  window.__action = function(action, key){
    if(action==='show_more'){ return window.__expandMore(key); }
    var url = csrUrl();
    var opts = {};
    try{ opts = {headers:{'X-Fusion-Action': String(action), 'X-Fusion-Key': String(key||'')}}; }catch(e){}
    fetch(url, opts).then(function(r){ if(!r.ok) throw new Error('action failed status '+r.status); return r.json(); }).then(function(newVM){
      window.__hydrate(newVM);
    }).catch(function(e){ window.__banner('action failed: '+e.message); });
  };
  function findChildByKey(box, key){
    var list = box.children;
    for(var i=0;i<list.length;i++){ if(list[i].getAttribute && list[i].getAttribute('data-key')===key) return list[i]; }
    return null;
  }
  function tagForType(t){
    var known = {a:1,button:1,img:1,input:1,span:1,p:1,h1:1,h2:1,h3:1,h4:1,ul:1,li:1,header:1,footer:1,section:1,article:1,nav:1,main:1,form:1,label:1,textarea:1,select:1,option:1,table:1,tr:1,td:1,div:1};
    if(t && known[t]) return t;
    if(t==='text') return 'span';
    return 'div';
  }
  function isVoidTag(tag){ return tag==='img'||tag==='input'||tag==='br'||tag==='hr'; }
  function strVal(v){ if(v===null||v===undefined) return ''; if(typeof v==='object') return JSON.stringify(v); return String(v); }
  function firstChildByTag(el, tag, cls){
    var list = el.children;
    for(var i=0;i<list.length;i++){
      if(list[i].tagName===tag && (!cls || list[i].className===cls)) return list[i];
    }
    return null;
  }
  function kidsBox(el){
    var k = firstChildByTag(el, 'DIV', 'kids');
    if(!k){ k = document.createElement('div'); k.className = 'kids'; el.appendChild(k); }
    return k;
  }
  function applySingleProp(el, prop, value){
    if(prop==='title'){
      var h = firstChildByTag(el, 'H1', '');
      if(value==null){ if(h) h.parentNode.removeChild(h); }
      else { if(!h){ h = document.createElement('h1'); el.insertBefore(h, el.firstChild); } h.textContent = String(value); }
    } else if(prop==='text'){
      var t = null;
      var list = el.children;
      for(var i=0;i<list.length;i++){ if(list[i].tagName==='SPAN' && list[i].className==='txt'){ t=list[i]; break; } }
      if(value==null){ if(t) t.parentNode.removeChild(t); }
      else { if(!t){ t = document.createElement('span'); t.className = 'txt'; var kb = kidsBox(el); el.insertBefore(t, kb); } t.textContent = String(value); }
    } else if(prop==='html'){
      // allowlisted js_call("set_html", literal) only: server vets via frontend-set-html.
      // All other text uses textContent (escaped by browser), never innerHTML.
      var hb = firstChildByTag(el, 'DIV', 'html');
      if(value==null){ if(hb) hb.parentNode.removeChild(hb); }
      else { if(!hb){ hb = document.createElement('div'); hb.className = 'html'; var kb2 = kidsBox(el); el.insertBefore(hb, kb2); } hb.innerHTML = String(value); }
    } else if(prop==='class'){ if(value==null) el.removeAttribute('class'); else el.setAttribute('class', String(value)); }
    else if(prop==='id'){ if(value==null) el.removeAttribute('id'); else el.setAttribute('id', String(value)); }
    else if(prop==='href'){ if(value==null) el.removeAttribute('href'); else el.setAttribute('href', String(value)); }
    else if(prop==='src'){ if(value==null) el.removeAttribute('src'); else el.setAttribute('src', String(value)); }
    else if(prop==='value'){ if(value==null){ el.removeAttribute('value'); if('value' in el) el.value=''; } else { el.setAttribute('value', String(value)); if('value' in el) el.value=String(value); } }
    else if(prop==='placeholder'){ if(value==null) el.removeAttribute('placeholder'); else el.setAttribute('placeholder', String(value)); }
    else if(prop==='alt'){ if(value==null) el.removeAttribute('alt'); else el.setAttribute('alt', String(value)); }
    else if(prop==='on_click'||prop==='onClick'){
      if(value==null){ el.removeAttribute('data-action'); el.onclick=null; }
      else { el.setAttribute('data-action', String(value)); (function(a,k){ el.onclick=function(ev){ if(ev&&ev.preventDefault) ev.preventDefault(); window.__action(a,k); }; })(String(value), el.getAttribute('data-key')); }
    } else {
      if(value==null) el.removeAttribute('data-prop-'+prop);
      else el.setAttribute('data-prop-'+prop, strVal(value));
    }
  }
  function paintProps(el, node){
    var props = node.props || {};
    applySingleProp(el, 'title', props.title!=null?props.title:null);
    applySingleProp(el, 'text', props.text!=null?props.text:null);
    if(props.html!=null) applySingleProp(el, 'html', props.html);
    else applySingleProp(el, 'html', null);
    var realKeys = ['class','id','href','src','value','placeholder','alt','on_click','onClick'];
    for(var i=0;i<realKeys.length;i++){ var rk=realKeys[i]; if(rk in props) applySingleProp(el, rk, props[rk]); else applySingleProp(el, rk, null); }
    for(var pk in props){
      if(!props.hasOwnProperty(pk)) continue;
      if(pk==='title'||pk==='text'||pk==='html'||pk==='class'||pk==='id'||pk==='href'||pk==='src'||pk==='value'||pk==='placeholder'||pk==='alt'||pk==='on_click'||pk==='onClick') continue;
      applySingleProp(el, pk, props[pk]);
    }
    var attrs = el.attributes;
    var toRemove = [];
    for(var a=0;a<attrs.length;a++){
      var an = attrs[a].name;
      if(an.indexOf('data-prop-')===0){
        var pn = an.slice(10);
        if(!(pn in props)) toRemove.push(an);
      }
    }
    for(var r=0;r<toRemove.length;r++) el.removeAttribute(toRemove[r]);
    if(el.hasAttribute('data-action') && !('on_click' in props) && !('onClick' in props)){
      if(el.getAttribute('data-action')!=='show_more'){ el.removeAttribute('data-action'); el.onclick=null; }
    }
    refreshBlocks(el, node);
  }
  // refreshBlocks rebuilds the visible chrome blocks (nav links, rows,
  // stat/section bodies) from node.props — the exact client mirror of the
  // server renderChromeBlocks, so SSR, CSR patches and hydrate agree.
  // All text goes through textContent (escaped by the browser), never
  // innerHTML. Blocks live outside div.kids so keyed diffing never touches
  // them; they refresh on paint (build/hydrate) and on prop patches.
  function blockKid(el, ckey){
    var list = el.children;
    for(var i=0;i<list.length;i++){
      if(list[i].getAttribute && list[i].getAttribute('data-key')===ckey) return list[i];
    }
    return null;
  }
  function dropBlock(el, ckey){
    var n = blockKid(el, ckey);
    if(n && n.parentNode) n.parentNode.removeChild(n);
  }
  function placeBlock(el, n){
    var kb = null;
    for(var j=0;j<el.children.length;j++){
      var c = el.children[j];
      if(c.tagName==='DIV' && c.className==='kids'){ kb = c; break; }
    }
    if(kb) el.insertBefore(n, kb); else el.appendChild(n);
  }
  function itemActive(item, props){
    if(item && item.active===true) return true;
    if(props && typeof props.active==='string' && props.active!=='' && item && item.path===props.active) return true;
    return false;
  }
  function buildNav(doc, ckey, cls, items, props){
    var nav = doc.createElement('nav');
    nav.setAttribute('data-key', ckey);
    nav.setAttribute('class', cls);
    for(var i=0;i<items.length;i++){
      var it = items[i]||{};
      if(!it.path && !it.label) continue;
      var a = doc.createElement('a');
      a.setAttribute('href', it.path||'');
      a.setAttribute('data-key', ckey+'-'+i);
      if(itemActive(it, props)) a.setAttribute('class', 'active');
      a.textContent = it.label||it.path||'';
      nav.appendChild(a);
    }
    return nav;
  }
  function refreshBlocks(el, node){
    if(!el || !node) return;
    var props = node.props||{};
    var key = node.key||'';
    var typ = node.type||'';
    var navArr = (typ==='header') ? props.links : ((typ==='sidebar') ? props.items : null);
    if(navArr && navArr.length){
      var cls = (typ==='header') ? 'nav' : 'sidenav';
      var fresh = buildNav(document, key+':nav', cls, navArr, props);
      var old = blockKid(el, key+':nav');
      if(old) el.replaceChild(fresh, old); else placeBlock(el, fresh);
    } else { dropBlock(el, key+':nav'); }
    if(typ==='page' && props.rows && props.rows.length){
      var ul = blockKid(el, key+':rows');
      if(!ul){ ul = document.createElement('ul'); ul.setAttribute('data-key', key+':rows'); ul.setAttribute('class', 'rows'); placeBlock(el, ul); }
      else ul.setAttribute('class', 'rows');
      while(ul.firstChild) ul.removeChild(ul.firstChild);
      for(var ri=0; ri<props.rows.length; ri++){
        var li = document.createElement('li');
        li.setAttribute('data-key', key+':rows-'+ri);
        li.textContent = strVal(props.rows[ri]);
        ul.appendChild(li);
      }
    } else { dropBlock(el, key+':rows'); }
    if(typ==='stat'){
      var sb = blockKid(el, key+':statbody');
      if(!sb){ sb = document.createElement('div'); sb.setAttribute('data-key', key+':statbody'); placeBlock(el, sb); }
      sb.setAttribute('class', 'statbody');
      while(sb.firstChild) sb.removeChild(sb.firstChild);
      var lb = document.createElement('span'); lb.setAttribute('class', 'stat-label'); lb.textContent = strVal(props.label); sb.appendChild(lb);
      var vl = document.createElement('span'); vl.setAttribute('class', 'stat-value'); vl.textContent = strVal(props.value); sb.appendChild(vl);
    } else { dropBlock(el, key+':statbody'); }
    if(typ==='section' && (props.h || props.p)){
      var sc = blockKid(el, key+':secbody');
      if(!sc){ sc = document.createElement('div'); sc.setAttribute('data-key', key+':secbody'); placeBlock(el, sc); }
      sc.setAttribute('class', 'secbody');
      while(sc.firstChild) sc.removeChild(sc.firstChild);
      if(props.h){ var h2 = document.createElement('h2'); h2.textContent = strVal(props.h); sc.appendChild(h2); }
      if(props.p){ var pp = document.createElement('p'); pp.textContent = strVal(props.p); sc.appendChild(pp); }
    } else { dropBlock(el, key+':secbody'); }
  }
  function findNodeByKey(vm, k){
    if(!vm) return null;
    if(vm.key===k) return vm;
    var ch = vm.children||[];
    for(var i=0;i<ch.length;i++){ var r = findNodeByKey(ch[i], k); if(r) return r; }
    return null;
  }
  function build(node){
    var tag = tagForType(node.type||'div');
    var el = document.createElement(tag);
    el.setAttribute('data-key', node.key);
    el.setAttribute('data-type', node.type || 'div');
    try{ __byKey[node.key] = el; }catch(e){}
    paintProps(el, node);
    if(el.getAttribute('data-action')==='show_more' && !el.onclick){
      (function(k){ el.onclick=function(ev){ if(ev&&ev.preventDefault) ev.preventDefault(); window.__action('show_more', k); }; })(node.key);
    }
    if(isVoidTag(tag)) return el;
    var box = kidsBox(el);
    var kids = node.children || [];
    if(kids.length > 100){
      for(var i=0;i<100;i++){ box.appendChild(build(kids[i])); }
      var moreBtn = document.createElement('button');
      moreBtn.setAttribute('data-key', node.key + ':more');
      moreBtn.setAttribute('data-action', 'show_more');
      moreBtn.textContent = 'Show more (100/' + kids.length + ')';
      (function(pk){ moreBtn.onclick=function(ev){ if(ev&&ev.preventDefault) ev.preventDefault(); window.__action('show_more', pk); }; })(node.key);
      try{ __byKey[node.key+':more'] = moreBtn; }catch(e2){}
      box.appendChild(moreBtn);
    } else {
      for(var j=0;j<kids.length;j++){ box.appendChild(build(kids[j])); }
    }
    return el;
  }
  function dropKeys(el){
    if(el.nodeType !== 1) return;
    if(el.hasAttribute && el.hasAttribute('data-key')) delete __byKey[el.getAttribute('data-key')];
    var kids = el.children;
    for(var i=0; i<kids.length; i++) dropKeys(kids[i]);
  }
  function keyedChildren(box){
    var out = [];
    for(var i=0;i<box.children.length;i++){
      var c = box.children[i];
      if(c.getAttribute && c.getAttribute('data-key')) out.push(c);
    }
    return out;
  }
  function applyPatch(ops){
    ops.forEach(function(op){
      var el = __byKey[op.key];
      if(op.op === 'setText' && el){
        applySingleProp(el, 'text', op.value);
      } else if(op.op === 'setProp' && el){
        applySingleProp(el, op.prop, op.value);
      } else if(op.op === 'replace' && el){
        var fresh = build(op.value);
        dropKeys(el);
        if(el.parentNode) el.parentNode.replaceChild(fresh, el);
      } else if(op.op === 'insert'){
        var parent = __byKey[op.parent];
        if(!parent) return;
        var box = kidsBox(parent);
        var node = build(op.value);
        var kids = keyedChildren(box);
        var ref = kids[op.index];
        if(ref) box.insertBefore(node, ref); else {
          var more = findChildByKey(box, op.parent+':more');
          if(more) box.insertBefore(node, more); else box.appendChild(node);
        }
      } else if(op.op === 'remove' && el){
        dropKeys(el);
        if(el.parentNode) el.parentNode.removeChild(el);
      } else if(op.op === 'move' && el){
        var p = __byKey[op.parent];
        if(!p) return;
        var bx = kidsBox(p);
        var ks = keyedChildren(bx);
        var rf = ks[op.index];
        if(rf === el) return;
        bx.removeChild(el);
        ks = keyedChildren(bx);
        rf = ks[op.index];
        if(rf) bx.insertBefore(el, rf); else bx.appendChild(el);
      }
    });
    // Reconcile visible chrome blocks for touched subtrees against the
    // (already updated) current VM — mirrors SSR renderChromeBlocks.
    var seen = {};
    ops.forEach(function(op){ if(op.key) seen[op.key]=true; if(op.parent) seen[op.parent]=true; });
    for(var sk in seen){
      if(!seen.hasOwnProperty(sk)) continue;
      var sel = __byKey[sk];
      if(!sel) continue;
      var sn = findNodeByKey(window.__currentVM, sk);
      if(sn) refreshBlocks(sel, sn);
    }
  }
  window.__applyPatch = applyPatch;
  function hydrateEl(el, node){
    if(!el || !node) return;
    try{ __byKey[node.key] = el; }catch(e){}
    paintProps(el, node);
    if(el.getAttribute('data-action')==='show_more' && !el.onclick){
      (function(k){ el.onclick=function(ev){ if(ev&&ev.preventDefault) ev.preventDefault(); window.__action('show_more', k); }; })(node.key);
    }
    var tag = tagForType(node.type||'div');
    if(isVoidTag(tag)) return;
    var box = kidsBox(el);
    var kids = node.children || [];
    var visible = kids.length>100 ? kids.slice(0,100) : kids;
    var want = {};
    for(var vi=0;vi<visible.length;vi++) want[visible[vi].key]=true;
    var existing = keyedChildren(box);
    for(var ei=0;ei<existing.length;ei++){
      var ek = existing[ei].getAttribute('data-key');
      if(ek===node.key+':more') continue;
      if(!want[ek]){ dropKeys(existing[ei]); if(existing[ei].parentNode) existing[ei].parentNode.removeChild(existing[ei]); }
    }
    for(var idx=0;idx<visible.length;idx++){
      var cn = visible[idx];
      var ce = findChildByKey(box, cn.key);
      if(ce){
        var ordered = keyedChildren(box);
        var curPos = -1;
        for(var op=0;op<ordered.length;op++){ if(ordered[op]===ce){ curPos=op; break; } }
        if(curPos!==idx){
          var ref = keyedChildren(box)[idx];
          if(ref && ref!==ce) box.insertBefore(ce, ref);
          else if(!ref){
            var m = findChildByKey(box, node.key+':more');
            if(m) box.insertBefore(ce, m); else box.appendChild(ce);
          }
        }
        hydrateEl(ce, cn);
      } else {
        var fresh = build(cn);
        var ref2 = keyedChildren(box)[idx];
        if(ref2) box.insertBefore(fresh, ref2);
        else { var m2 = findChildByKey(box, node.key+':more'); if(m2) box.insertBefore(fresh, m2); else box.appendChild(fresh); }
      }
    }
    var moreKey = node.key+':more';
    var moreEl = findChildByKey(box, moreKey);
    if(kids.length>100){
      if(!moreEl){
        var nb = document.createElement('button');
        nb.setAttribute('data-key', moreKey);
        nb.setAttribute('data-action', 'show_more');
        nb.textContent = 'Show more ('+Math.min(100,kids.length)+'/'+kids.length+')';
        (function(pk){ nb.onclick=function(ev){ if(ev&&ev.preventDefault) ev.preventDefault(); window.__action('show_more', pk); }; })(node.key);
        try{ __byKey[moreKey]=nb; }catch(e){}
        box.appendChild(nb);
      } else {
        var shownCount = 0;
        for(var sc=0;sc<box.children.length;sc++){ var cc=box.children[sc]; if(cc.getAttribute&&cc.getAttribute('data-key')&&cc.getAttribute('data-key')!==moreKey) shownCount++; }
        if(shownCount>=kids.length){ if(moreEl.parentNode) moreEl.parentNode.removeChild(moreEl); delete __byKey[moreKey]; }
        else moreEl.textContent = 'Show more ('+shownCount+'/'+kids.length+')';
      }
    } else if(moreEl){ if(moreEl.parentNode) moreEl.parentNode.removeChild(moreEl); delete __byKey[moreKey]; }
  }
  window.__expandMore = function(parentKey){
    var cur = window.__currentVM;
    if(!cur) return;
    function find(n,k){
      if(!n) return null;
      if(n.key===k) return n;
      var ch=n.children||[];
      for(var i=0;i<ch.length;i++){ var r=find(ch[i],k); if(r) return r; }
      return null;
    }
    var node = find(cur, parentKey);
    if(!node) return;
    var parentEl = __byKey[parentKey];
    if(!parentEl) return;
    var box = kidsBox(parentEl);
    var moreEl = findChildByKey(box, parentKey+':more');
    var count=0;
    for(var i=0;i<box.children.length;i++){ var c=box.children[i]; if(c.getAttribute&&c.getAttribute('data-key')&&c.getAttribute('data-key')!==parentKey+':more') count++; }
    var kids=node.children||[];
    var next=count+100;
    if(next>kids.length) next=kids.length;
    var frag=document.createDocumentFragment();
    for(var j=count;j<next;j++) frag.appendChild(build(kids[j]));
    if(moreEl) box.insertBefore(frag, moreEl);
    else box.appendChild(frag);
    if(next>=kids.length){ if(moreEl&&moreEl.parentNode) moreEl.parentNode.removeChild(moreEl); }
    else if(moreEl) moreEl.textContent='Show more ('+next+'/'+kids.length+')';
  };
  window.__hydrate = function(newVM){
    var rootKey = newVM.key;
    var indexed = app.querySelectorAll('[data-key]');
    __byKey = {};
    for(var i=0;i<indexed.length;i++) __byKey[indexed[i].getAttribute('data-key')]=indexed[i];
    var rootEl = __byKey[rootKey];
    if(!rootEl){
      while(app.firstChild) app.removeChild(app.firstChild);
      var fresh = build(newVM);
      app.appendChild(fresh);
      rootEl = fresh;
    } else {
      hydrateEl(rootEl, newVM);
      var tops=[];
      for(var t=0;t<app.children.length;t++) tops.push(app.children[t]);
      for(var ti=0;ti<tops.length;ti++){ if(tops[ti]!==rootEl) app.removeChild(tops[ti]); }
    }
    window.__currentVM = newVM;
    try{ document.getElementById('vm').textContent = JSON.stringify(newVM); }catch(e){}
    return rootEl;
  };
  window.__renderVM = function(v, root){
    while(root.firstChild) root.removeChild(root.firstChild);
    __byKey = {};
    root.appendChild(build(v));
    window.__currentVM = v;
    try{ document.getElementById('vm').textContent = JSON.stringify(v); }catch(e){}
  };
  function runMounts(){
    for(var i=0;i<__mounts.length;i++){ try{ __mounts[i](); }catch(e){ window.__banner('mount failed: '+e.message); } }
    __mounts=[];
  }
  function boot(){
    try{ window.__hydrate(vm); }catch(e){ window.__banner('hydrate failed: '+e.message); try{ window.__renderVM(vm, app); }catch(e2){ window.__banner('render failed: '+e2.message); } }
    runMounts();
  }
  if(document.readyState==='loading'){ document.addEventListener('DOMContentLoaded', boot); } else { boot(); }
})();
</script>
%s
<!-- SSR in %s -->
</body></html>`, titleEsc, routeEsc, ssrHTML, prettyStr, stateJSON, watchScript, time.Now().Format(time.RFC3339))
}

type webWatcher struct {
	dir     string
	mu      chan int
	ver     int
	lastMod map[string]time.Time
}

func newWebWatcher(dir string) *webWatcher {
	return &webWatcher{dir: dir, mu: make(chan int, 1), lastMod: map[string]time.Time{}}
}

func (w *webWatcher) version() int { return w.ver }

func (w *webWatcher) snapshot() map[string]time.Time {
	out := map[string]time.Time{}
	_ = filepath.Walk(w.dir, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if !info.IsDir() && strings.HasSuffix(info.Name(), ".ks") {
			out[p] = info.ModTime()
		}
		return nil
	})
	return out
}

func (w *webWatcher) loop() {
	w.lastMod = w.snapshot()
	for {
		// P4: poll interval kept at 400ms (TODO WS/fsnotify push).
		time.Sleep(webWatchPollInterval)
		cur := w.snapshot()
		changed := len(cur) != len(w.lastMod)
		if !changed {
			for k, v := range cur {
				if old, ok := w.lastMod[k]; !ok || !old.Equal(v) {
					changed = true
					break
				}
			}
		}
		if changed {
			w.lastMod = cur
			w.ver++
		}
	}
}

// ISR cache (v2.4 opt-in TTL, v2.5 background regen): route -> rendered
// body with revalidate TTL. A background loop refreshes entries before they
// expire; handlers serve stale bodies while a refresh is in flight or when a
// re-render fails.
type isrEntry struct {
	body    string
	ctype   string
	expires time.Time
}

type isrCache struct {
	mu         sync.Mutex
	m          map[string]isrEntry
	refreshing map[string]bool
	// regen rebuilds one cache key; set by the server.
	regen func(route, format string) (body, ctype, vmJSON string, ok bool)
}

func newISRCache() *isrCache { return &isrCache{m: map[string]isrEntry{}, refreshing: map[string]bool{}} }

// startBackground refreshes entries expiring within `ahead` every `interval`.
// Stop via the returned func (tests) or leave running for the server lifetime.
func (c *isrCache) startBackground(interval, ahead time.Duration) func() {
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		tick := time.NewTicker(interval)
		defer tick.Stop()
		for {
			select {
			case <-stop:
				return
			case <-tick.C:
				c.refreshSoon(ahead)
			}
		}
	}()
	return func() { close(stop); <-done }
}

func (c *isrCache) refreshSoon(ahead time.Duration) {
	c.mu.Lock()
	var keys []string
	now := time.Now()
	for k, e := range c.m {
		if e.expires.Sub(now) <= ahead && !c.refreshing[k] {
			c.refreshing[k] = true
			keys = append(keys, k)
		}
	}
	regen := c.regen
	c.mu.Unlock()
	for _, k := range keys {
		go func(key string) {
			defer func() {
				c.mu.Lock()
				delete(c.refreshing, key)
				c.mu.Unlock()
			}()
			if regen == nil {
				return
			}
			route, format := splitISRKey(key)
			body, ctype, vmJSON, ok := regen(route, format)
			if !ok {
				return // keep stale entry; handler serves it on errors
			}
			ttl := isrTTL(vmJSON)
			if ttl <= 0 {
				return
			}
			c.mu.Lock()
			c.m[key] = isrEntry{body: body, ctype: ctype, expires: time.Now().Add(ttl)}
			c.mu.Unlock()
		}(k)
	}
}

func splitISRKey(key string) (route, format string) {
	// keys are route + "?format=" + format
	if i := strings.LastIndex(key, "?format="); i >= 0 {
		return key[:i], key[i+len("?format="):]
	}
	return key, ""
}

func isrTTL(vmJSON string) time.Duration {
	// convention: view-model props.revalidate = seconds (Next.js ISR analogue)
	var v map[string]any
	if err := json.Unmarshal([]byte(vmJSON), &v); err != nil {
		return 0
	}
	var props map[string]any
	if p, ok := v["props"].(map[string]any); ok {
		props = p
	} else {
		props = v
	}
	if rv, ok := props["revalidate"]; ok {
		switch n := rv.(type) {
		case float64:
			if n > 0 && n < 86400*30 {
				return time.Duration(n * float64(time.Second))
			}
		}
	}
	return 0
}

func (c *isrCache) get(route, format string) (bool, string, string) {
	key := route + "?format=" + format
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.m[key]
	if !ok || time.Now().After(e.expires) {
		return false, "", ""
	}
	return true, e.body, e.ctype
}

// getStale returns the entry even when expired (serve-stale-while-revalidate).
func (c *isrCache) getStale(route, format string) (string, string, bool) {
	key := route + "?format=" + format
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.m[key]
	if !ok {
		return "", "", false
	}
	return e.body, e.ctype, true
}

// kickRefresh triggers one async refresh of key unless already running.
func (c *isrCache) kickRefresh(route, format string) {
	key := route + "?format=" + format
	c.mu.Lock()
	if c.refreshing[key] || c.regen == nil {
		c.mu.Unlock()
		return
	}
	c.refreshing[key] = true
	regen := c.regen
	c.mu.Unlock()
	go func() {
		defer func() {
			c.mu.Lock()
			delete(c.refreshing, key)
			c.mu.Unlock()
		}()
		body, ctype, vmJSON, ok := regen(route, format)
		if !ok {
			return
		}
		if ttl := isrTTL(vmJSON); ttl > 0 {
			c.mu.Lock()
			c.m[key] = isrEntry{body: body, ctype: ctype, expires: time.Now().Add(ttl)}
			c.mu.Unlock()
		}
	}()
}

func (c *isrCache) put(route, format, body, vmJSON string) {
	ttl := isrTTL(vmJSON)
	if ttl <= 0 {
		return
	}
	key := route + "?format=" + format
	c.mu.Lock()
	defer c.mu.Unlock()
	ctype := "text/html; charset=utf-8"
	if format == "json" {
		ctype = "application/json"
	}
	c.m[key] = isrEntry{body: body, ctype: ctype, expires: time.Now().Add(ttl)}
}

// BuildSSG pre-renders routes to target/ssg/*.html + *.json (v2.3 P5).
func BuildSSG(appDir, out string) error {
	cfg, err := config.Load(appDir)
	if err != nil {
		return err
	}
	if out == "" {
		out = filepath.Join(cfg.Dir, "target", "ssg")
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		return err
	}
	// v2.7: seed only "/" — every other route is discovered from
	// frontend/pages/*.ks below (a hardcoded "/hi" here used to emit a
	// phantom hi.html for apps with no hi.ks).
	routes := []string{"/"}
	// add each static page file as route (P1: skip dynamic foo_[bar].ks)
	if ents, err := os.ReadDir(filepath.Join(cfg.Dir, "frontend", "pages")); err == nil {
		for _, e := range ents {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".ks") || strings.HasSuffix(e.Name(), "_test.ks") {
				continue
			}
			base := strings.TrimSuffix(e.Name(), ".ks")
			// P1 SSG parity: dynamic routes need a param value; skip, not fatal.
			if _, _, _, isDyn := parseDynamicFile(base); isDyn {
				fmt.Printf("ssg skip dynamic %s: dynamic route excluded from SSG\n", e.Name())
				continue
			}
			if strings.Contains(base, "[") || strings.Contains(base, "]") {
				fmt.Printf("ssg skip dynamic %s: dynamic route excluded from SSG\n", e.Name())
				continue
			}
			var r string
			if base == "home" {
				r = "/"
			} else {
				r = "/" + base
			}
			found := false
			for _, x := range routes {
				if x == r {
					found = true
					break
				}
			}
			if !found {
				routes = append(routes, r)
			}
		}
	}
	for _, r := range routes {
		vmJSON, err := renderRoute(cfg, r)
		if err != nil {
			fmt.Printf("ssg skip %s: %v\n", r, err)
			continue
		}
		html := vmToHTMLWithWatchDir(vmJSON, r, false, cfg.Dir)
		name := strings.Trim(r, "/")
		if name == "" {
			name = "index"
		}
		name = strings.ReplaceAll(name, "/", "_")
		if err := os.WriteFile(filepath.Join(out, name+".html"), []byte(html), 0o644); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(out, name+".json"), []byte(vmJSON), 0o644); err != nil {
			return err
		}
		fmt.Printf("ssg: %s -> %s.{html,json} (%d bytes)\n", r, filepath.Join(out, name), len(html))
	}
	fmt.Printf("ssg ok: %d routes in %s\n", len(routes), out)
	return nil
}

// oldBuildJS is the v2.2 prototype (kept for reference; P3 BuildJS lives in buildjs_p3.go).
func oldBuildJS(appDir, out string) error {
	cfg, err := config.Load(appDir)
	if err != nil {
		return err
	}
	if out == "" {
		out = filepath.Join(cfg.Dir, "target", "js")
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		return err
	}
	pagesDir := filepath.Join(cfg.Dir, "frontend", "pages")
	ents, err := os.ReadDir(pagesDir)
	if err != nil {
		return fmt.Errorf("no frontend/pages in %s: %w", cfg.Dir, err)
	}
	type manifestEntry struct {
		Size int    `json:"size"`
		SHA  string `json:"sha256"`
	}
	manifest := map[string]manifestEntry{}
	for _, e := range ents {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".ks") {
			continue
		}
		src, err := os.ReadFile(filepath.Join(pagesDir, e.Name()))
		if err != nil {
			return err
		}
		prog, err := frontend.ParseSource(string(src), e.Name())
		if err != nil {
			return fmt.Errorf("build-js %s: %w", e.Name(), err)
		}
		js := transpileToJS(prog)
		route := strings.TrimSuffix(e.Name(), ".ks")
		if route == "home" {
			route = "index"
		}
		dst := filepath.Join(out, route+".js")
		// minify analogue: trim lines, drop comments/blank
		min := minifyJS(js)
		// content-hash incremental cache (v2.4): skip write when unchanged
		sum := sha256.Sum256([]byte(min))
		hexsum := hex.EncodeToString(sum[:])
		if old, err := os.ReadFile(dst); err == nil {
			osum := sha256.Sum256(old)
			if hex.EncodeToString(osum[:]) == hexsum {
				fmt.Printf("build-js: %s unchanged (%d bytes, %s)\n", e.Name(), len(min), hexsum[:12])
				manifest[route] = manifestEntry{Size: len(min), SHA: hexsum}
				continue
			}
		}
		if err := os.WriteFile(dst, []byte(min), 0o644); err != nil {
			return err
		}
		manifest[route] = manifestEntry{Size: len(min), SHA: hexsum}
		fmt.Printf("build-js: %s -> %s (%d bytes, %s)\n", e.Name(), dst, len(min), hexsum[:12])
		if len(min) > 250*1024 {
			return fmt.Errorf("budget fail: route %s JS %d bytes > 250KB", route, len(min))
		} else if len(min) > 100*1024 {
			fmt.Printf("warn: route %s JS %d bytes > 100KB budget\n", route, len(min))
		}
	}
	// manifest (sizes + content hashes)
	man, _ := json.MarshalIndent(manifest, "", "  ")
	_ = os.WriteFile(filepath.Join(out, "manifest.json"), append(man, '\n'), 0o644)
	fmt.Printf("build-js ok: %d routes in %s\n", len(manifest), out)
	return nil
}

func minifyJS(s string) string {
	var lines []string
	for _, l := range strings.Split(s, "\n") {
		t := strings.TrimSpace(l)
		if t == "" || strings.HasPrefix(t, "//") {
			continue
		}
		lines = append(lines, t)
	}
	return strings.Join(lines, "\n") + "\n"
}

func transpileToJS(prog *frontend.Program) string {
	var b strings.Builder
	b.WriteString("// generated by fusion build-js (v2.2 subset)\n")
	for _, st := range prog.Statements {
		b.WriteString(stmtToJS(st, 0) + "\n")
	}
	return b.String()
}

func stmtToJS(st *frontend.Stmt, depth int) string {
	if st == nil {
		return ""
	}
	ind := strings.Repeat("  ", depth)
	switch st.Kind {
	case frontend.StmtLet:
		return ind + "let " + st.Name + " = " + exprToJS(st.Expr) + ";"
	case frontend.StmtAssign:
		op := st.Op
		if op == "" {
			op = "="
		}
		return ind + st.Name + " " + op + " " + exprToJS(st.Expr) + ";"
	case frontend.StmtFunc:
		params := strings.Join(st.Names, ", ")
		return ind + "function " + st.Name + "(" + params + ") " + stmtToJS(st.Body, depth)
	case frontend.StmtBlock:
		var b strings.Builder
		b.WriteString("{\n")
		for _, s := range st.List {
			b.WriteString(stmtToJS(s, depth+1) + "\n")
		}
		b.WriteString(ind + "}")
		return b.String()
	case frontend.StmtIf:
		s := ind + "if (" + exprToJS(st.Expr) + ") " + stmtToJS(st.Then, depth)
		if st.Else != nil {
			s += " else " + stmtToJS(st.Else, depth)
		}
		return s
	case frontend.StmtWhile:
		return ind + "while (" + exprToJS(st.Expr) + ") " + stmtToJS(st.Body, depth)
	case frontend.StmtForIn:
		// for v in expr -> for (let v of expr)
		if len(st.Names) == 1 {
			return ind + "for (let " + st.Names[0] + " of " + exprToJS(st.Expr) + ") " + stmtToJS(st.Body, depth)
		}
		if len(st.Names) == 2 {
			return ind + "for (let [" + st.Names[0] + ", " + st.Names[1] + "] of Object.entries(" + exprToJS(st.Expr) + ")) " + stmtToJS(st.Body, depth)
		}
		return ind + "// unsupported for-in"
	case frontend.StmtForC:
		// best-effort
		return ind + "// for-c (see .ks source)"
	case frontend.StmtReturn:
		if st.Expr != nil {
			return ind + "return " + exprToJS(st.Expr) + ";"
		}
		return ind + "return;"
	case frontend.StmtPrint:
		var args []string
		for _, e := range st.Exprs {
			args = append(args, exprToJS(e))
		}
		return ind + "console.log(" + strings.Join(args, ", ") + ");"
	case frontend.StmtExpr:
		return ind + exprToJS(st.Expr) + ";"
	default:
		return ind + "// unsupported stmt"
	}
}

func exprToJS(e *frontend.Expr) string {
	if e == nil {
		return "null"
	}
	switch e.Kind {
	case frontend.ExprString:
		d, _ := json.Marshal(e.StrVal)
		return string(d)
	case frontend.ExprInt:
		return fmt.Sprintf("%d", e.IntVal)
	case frontend.ExprFloat:
		return fmt.Sprintf("%v", e.FloatVal)
	case frontend.ExprBool:
		if e.BoolVal {
			return "true"
		}
		return "false"
	case frontend.ExprNil:
		return "null"
	case frontend.ExprVar:
		return e.Name
	case frontend.ExprAdd:
		return "(" + exprToJS(e.Left) + " + " + exprToJS(e.Right) + ")"
	case frontend.ExprSub:
		return "(" + exprToJS(e.Left) + " - " + exprToJS(e.Right) + ")"
	case frontend.ExprMul:
		return "(" + exprToJS(e.Left) + " * " + exprToJS(e.Right) + ")"
	case frontend.ExprDiv:
		return "(" + exprToJS(e.Left) + " / " + exprToJS(e.Right) + ")"
	case frontend.ExprMod:
		return "(" + exprToJS(e.Left) + " % " + exprToJS(e.Right) + ")"
	case frontend.ExprPow:
		return "Math.pow(" + exprToJS(e.Left) + ", " + exprToJS(e.Right) + ")"
	case frontend.ExprEq:
		return "(" + exprToJS(e.Left) + " === " + exprToJS(e.Right) + ")"
	case frontend.ExprNe:
		return "(" + exprToJS(e.Left) + " !== " + exprToJS(e.Right) + ")"
	case frontend.ExprLt:
		return "(" + exprToJS(e.Left) + " < " + exprToJS(e.Right) + ")"
	case frontend.ExprLe:
		return "(" + exprToJS(e.Left) + " <= " + exprToJS(e.Right) + ")"
	case frontend.ExprGt:
		return "(" + exprToJS(e.Left) + " > " + exprToJS(e.Right) + ")"
	case frontend.ExprGe:
		return "(" + exprToJS(e.Left) + " >= " + exprToJS(e.Right) + ")"
	case frontend.ExprAnd:
		return "(" + exprToJS(e.Left) + " && " + exprToJS(e.Right) + ")"
	case frontend.ExprOr:
		return "(" + exprToJS(e.Left) + " || " + exprToJS(e.Right) + ")"
	case frontend.ExprNot:
		return "(!" + exprToJS(e.Left) + ")"
	case frontend.ExprNeg:
		return "(-" + exprToJS(e.Left) + ")"
	case frontend.ExprIn:
		return "(" + exprToJS(e.Right) + ".includes(" + exprToJS(e.Left) + "))"
	case frontend.ExprIs:
		return "(typeof " + exprToJS(e.Left) + ")"
	case frontend.ExprCoalesce:
		return "(" + exprToJS(e.Left) + " ?? " + exprToJS(e.Right) + ")"
	case frontend.ExprCall:
		callee := exprToJS(e.Callee)
		var args []string
		for _, a := range e.Args {
			args = append(args, exprToJS(a))
		}
		return callee + "(" + strings.Join(args, ", ") + ")"
	case frontend.ExprIndex:
		if e.Right != nil {
			return exprToJS(e.Left) + "[" + exprToJS(e.Right) + "]"
		}
		return exprToJS(e.Left)
	case frontend.ExprSlice:
		return exprToJS(e.Left) + ".slice()"
	case frontend.ExprArray:
		var els []string
		for _, el := range e.Elements {
			els = append(els, exprToJS(el))
		}
		return "[" + strings.Join(els, ", ") + "]"
	case frontend.ExprMap:
		var parts []string
		for i, k := range e.MapKeys {
			parts = append(parts, fmt.Sprintf("%q: %s", k, exprToJS(e.MapVals[i])))
		}
		return "({" + strings.Join(parts, ", ") + "})"
	case frontend.ExprFunc:
		return "(function(" + strings.Join(e.FuncParams, ", ") + ") " + stmtToJS(e.FuncBody, 0) + ")"
	default:
		return "null"
	}
}
