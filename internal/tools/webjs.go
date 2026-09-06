package tools

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/kswarrior/ks-fusion/internal/backend"
	"github.com/kswarrior/ks-fusion/internal/config"
	"github.com/kswarrior/ks-fusion/internal/frontend"
)

// RunWeb serves frontend/ as SSR HTML+JSON (v2.2 P2 step).
// Routes: / -> frontend/pages/home.ks home_page, /hi -> hi_page, /api/* -> backend/api/*.ks
func RunWeb(appDir string, port int) error { return RunWebWithWatch(appDir, port, false) }

// RunWebWithWatch adds --watch polling + SSE reload (v2.3 HMR-lite).
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
	mux.HandleFunc("/events", func(w http.ResponseWriter, r *http.Request) {
		route := r.URL.Query().Get("route")
		if route == "" {
			route = "/"
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		fl, _ := w.(http.Flusher)
		last := watcher.version()
		tick := time.NewTicker(300 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-r.Context().Done():
				return
			case <-tick.C:
				if v := watcher.version(); v != last {
					// HMR patch (v2.4): send fresh view-model, client patches DOM
					if vmJSON, err := renderRoute(cfg, route); err == nil {
						// debounce: 1 render/tick already via 300ms poll
						fmt.Fprintf(w, "data: %s\n\n", vmJSON)
					} else {
						fmt.Fprintf(w, "data: reload %d\n\n", v)
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
		// ISR (v2.4): serve cached HTML/JSON when revalidate TTL fresh
		if hit, body, ctype := isr.get(r.URL.Path, r.URL.Query().Get("format")); hit {
			w.Header().Set("X-Cache", "HIT")
			w.Header().Set("Content-Type", ctype)
			_, _ = w.Write([]byte(body))
			return
		}
		vmJSON, err := renderRoute(cfg, r.URL.Path)
		el := time.Since(start)
		w.Header().Set("X-Render-Time", el.String())
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		if r.URL.Query().Get("format") == "json" {
			w.Header().Set("Content-Type", "application/json")
			isr.put(r.URL.Path, "json", vmJSON, vmJSON)
			_, _ = w.Write([]byte(vmJSON))
			return
		}
		html := vmToHTMLWithWatch(vmJSON, r.URL.Path, watch)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		isr.put(r.URL.Path, "html", html, vmJSON)
		_, _ = w.Write([]byte(html))
	})
	addr := fmt.Sprintf(":%d", port)
	extra := "SSR + /api/*, ?format=json"
	if watch {
		extra += ", --watch SSE reload"
	}
	fmt.Printf("ks-fusion web: serving %s at http://localhost%s (%s)\n", cfg.Name, addr, extra)
	return http.ListenAndServe(addr, mux)
}

func renderRoute(cfg *config.Config, route string) (string, error) {
	// Load store + components + pages on demand, run home_page/hi_page via interpreter.
	// Simple: parse frontend/main.ks deps? Instead directly run page funcs.
	dir := cfg.Dir
	// collect all frontend .ks files to load into one interpreter
	var files []string
	_ = filepath.Walk(filepath.Join(dir, "frontend"), func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if !info.IsDir() && strings.HasSuffix(info.Name(), ".ks") {
			files = append(files, path)
		}
		return nil
	})
	sort.Strings(files)
	in := backend.New()
	// set ROUTE env for main.ks compat
	_ = os.Setenv("ROUTE", route)
	for _, f := range files {
		// skip main.ks (it runs route table with prints); load libs only
		if filepath.Base(f) == "main.ks" && filepath.Dir(f) == filepath.Join(dir, "frontend") {
			continue
		}
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		prog, err := frontend.ParseSource(string(data), f)
		if err != nil {
			return "", err
		}
		// exec with baseDir so imports inside work
		// use ExecProgram directly to keep same interpreter globals
		in.SetBaseDir(dir)
		if err := in.ExecProgram(prog); err != nil {
			return "", fmt.Errorf("%s: %w", f, err)
		}
	}
	// pick page func
	funcName := "home_page"
	if route == "/hi" {
		funcName = "hi_page"
	} else if strings.HasPrefix(route, "/user/") {
		funcName = "home_page"
	} else if route != "/" {
		// try dynamic: /<name> -> <name>_page
		clean := strings.Trim(route, "/")
		clean = strings.ReplaceAll(clean, "-", "_")
		clean = strings.ReplaceAll(clean, "/", "_")
		funcName = clean + "_page"
	}
	// call page func with props {}
	prog, err := frontend.ParseSource(fmt.Sprintf("let __vm = %s({})\n", funcName), "<web>")
	if err != nil {
		return "", err
	}
	_ = prog
	// Use eval via backend: call func value from globals
	fnVal, ok := in.Lookup(funcName)
	if !ok {
		return "", fmt.Errorf("unknown route %q (no %s)", route, funcName)
	}
	vm, err := in.Call(fnVal, []backend.Value{backend.MapV(map[string]backend.Value{})})
	if err != nil {
		return "", err
	}
	j, err := backend.ValueToJSONable(vm)
	if err != nil {
		return "", err
	}
	data, err := json.Marshal(j)
	if err != nil {
		return "", err
	}
	return string(data), nil
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

func vmToHTMLWithWatch(vmJSON, route string, watch bool) string {
	var v any
	_ = json.Unmarshal([]byte(vmJSON), &v)
	pretty, _ := json.MarshalIndent(v, "", "  ")
	title := "ks-fusion"
	if m, ok := v.(map[string]any); ok {
		if props, ok := m["props"].(map[string]any); ok {
			if t, ok := props["title"].(string); ok && t != "" {
				title = t
			}
		}
	}
	watchScript := ""
	if watch {
		watchScript = `<script>
// v2.3 watch: SSE reload (<100ms aim, full reload v1)
var es = new EventSource('/events');
es.onmessage = function(e){ if((e.data||'').indexOf('reload')===0) location.reload(); };
</script>`
	}
	return fmt.Sprintf(`<!doctype html>
<html><head><meta charset="utf-8"><title>%s</title></head>
<body>
<div id="app" data-route="%s"></div>
<script id="vm" type="application/json">%s</script>
<script>
// v2.3 hydrate: view-model -> DOM + use_state/fetch_json CSR
(function(){
  var vm = JSON.parse(document.getElementById('vm').textContent);
  var app = document.getElementById('app');
  var __state = {};
  window.use_state = function(k, init){ if(!(k in __state)) __state[k]=init; return __state[k]; };
  window.set_state = function(k, v){ __state[k]=v; return v; };
  function render(node, parent){
    if(!node) return;
    var el = document.createElement(node.type === 'page' ? 'div' : (node.type || 'div'));
    if(node.props && node.props.title){ var h=document.createElement('h1'); h.textContent=node.props.title; el.appendChild(h); }
    if(node.children){ node.children.forEach(function(c){ render(c, el); }); }
    parent.appendChild(el);
  }
  try{ render(vm, app); }catch(e){ app.textContent = JSON.stringify(vm); }
})();
</script>
%s
<!-- SSR in %s -->
</body></html>`, title, route, string(pretty), watchScript, time.Now().Format(time.RFC3339))
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
		time.Sleep(400 * time.Millisecond)
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
	routes := []string{"/", "/hi"}
	// add each page file as route
	if ents, err := os.ReadDir(filepath.Join(cfg.Dir, "frontend", "pages")); err == nil {
		for _, e := range ents {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".ks") || strings.HasSuffix(e.Name(), "_test.ks") {
				continue
			}
			base := strings.TrimSuffix(e.Name(), ".ks")
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
		html := vmToHTMLWithWatch(vmJSON, r, false)
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

// BuildJS transpiles safe .ks subset (pages/components) to JS per-route.
func BuildJS(appDir, out string) error {
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
	sizes := map[string]int{}
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
		if err := os.WriteFile(dst, []byte(min), 0o644); err != nil {
			return err
		}
		sizes[route] = len(min)
		fmt.Printf("build-js: %s -> %s (%d bytes)\n", e.Name(), dst, len(min))
		if len(min) > 250*1024 {
			return fmt.Errorf("budget fail: route %s JS %d bytes > 250KB", route, len(min))
		} else if len(min) > 100*1024 {
			fmt.Printf("warn: route %s JS %d bytes > 100KB budget\n", route, len(min))
		}
	}
	// manifest
	man, _ := json.MarshalIndent(sizes, "", "  ")
	_ = os.WriteFile(filepath.Join(out, "manifest.json"), append(man, '\n'), 0o644)
	fmt.Printf("build-js ok: %d routes in %s\n", len(sizes), out)
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
