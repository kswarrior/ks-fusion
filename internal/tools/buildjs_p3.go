package tools

// P3 build-js correctness (STRICT mode).
//
// Dep graph: loads all frontend/**/*.ks except frontend/main.ks (same set as
// renderRoute), builds per-route used-func closure (BFS from <route>_page +
// app_layout/_app_layout + string-referenced *_layout) and emits only used
// funcs/globals (tree-shake analogue). Dynamic user_[id].ks keeps its
// filename (user_[id].js) with a // route comment documenting /user/:id.
//
// STRICT (default true): any unsupported Stmt/Expr returns an error naming
// file:line + construct (e.g. "for-c not in JS subset, see .ks source").
// --strict=false preserves the old v2.2 // unsupported / null behaviour.
//
// CSS passthrough: props.class / props.className strings pass through
// untouched (maps emit verbatim). If frontend/styles/*.css exists, files are
// copied to <out>/styles/ and listed in manifest["styles"] (link hint, not
// bundled): add `<link rel="stylesheet" href="styles/<name>.css">` in HTML.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/kswarrior/ks-fusion/internal/config"
	"github.com/kswarrior/ks-fusion/internal/frontend"
)

// BuildJSOptions controls build-js strictness.
type BuildJSOptions struct {
	// Strict fails on unsupported constructs (default true).
	// False restores v2.2 lenient output (// unsupported comments, null).
	Strict bool
}

// BuildJS transpiles the safe .ks subset to per-route JS (P3 STRICT default).
func BuildJS(appDir, out string) error {
	return BuildJSWithOptions(appDir, out, BuildJSOptions{Strict: true})
}

// BuildJSWithOptions is BuildJS with explicit strictness.
func BuildJSWithOptions(appDir, out string, opts BuildJSOptions) error {
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
	frontendDir := filepath.Join(cfg.Dir, "frontend")
	pagesDir := filepath.Join(frontendDir, "pages")
	if st, err := os.Stat(pagesDir); err != nil || !st.IsDir() {
		return fmt.Errorf("no frontend/pages in %s", cfg.Dir)
	}

	funcs, globals, fileProgs, err := loadFrontendLibs(frontendDir)
	if err != nil {
		return err
	}

	ents, err := os.ReadDir(pagesDir)
	if err != nil {
		return err
	}
	type manifestEntry struct {
		Size int    `json:"size"`
		SHA  string `json:"sha256"`
	}
	manifest := map[string]any{}
	for _, e := range ents {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".ks") {
			continue
		}
		if strings.HasSuffix(e.Name(), "_test.ks") {
			continue
		}
		base := strings.TrimSuffix(e.Name(), ".ks")
		pagePath := filepath.Join(pagesDir, e.Name())
		prog := fileProgs[relFrontendKey(frontendDir, pagePath)]
		if prog == nil {
			// Fallback: parse directly (should not happen).
			src, rerr := os.ReadFile(pagePath)
			if rerr != nil {
				return rerr
			}
			prog, err = frontend.ParseSource(string(src), e.Name())
			if err != nil {
				return fmt.Errorf("build-js %s: %w", e.Name(), err)
			}
		}
		entry, err := entryFuncForPage(base, prog, funcs)
		if err != nil {
			return fmt.Errorf("build-js %s: %w", e.Name(), err)
		}
		usedFuncs, usedGlobals, usesRange, usesLen, usesIn, err := closureForRoute(entry, prog, funcs, globals)
		if err != nil {
			return err
		}
		js, err := emitRouteJS(base, pagePath, entry, prog, funcs, globals, usedFuncs, usedGlobals, usesRange, usesLen, usesIn, opts.Strict)
		if err != nil {
			return err
		}
		min := buildJSMinify(js)

		// Output filename: home -> index, everything else keeps its stem
		// (dynamic user_[id].ks -> user_[id].js, documented in header).
		route := base
		if route == "home" {
			route = "index"
		}
		dst := filepath.Join(out, route+".js")
		sum := sha256.Sum256([]byte(min))
		hexsum := hex.EncodeToString(sum[:])
		if old, rerr := os.ReadFile(dst); rerr == nil {
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

	// CSS passthrough: copy frontend/styles/*.css -> <out>/styles/, list in manifest.
	if styles, serr := copyStylesPassthrough(frontendDir, out); serr != nil {
		return serr
	} else if len(styles) > 0 {
		manifest["styles"] = styles
	}

	man, _ := json.MarshalIndent(manifest, "", "  ")
	_ = os.WriteFile(filepath.Join(out, "manifest.json"), append(man, '\n'), 0o644)
	// Count routes (exclude the styles hint).
	nroutes := 0
	for k := range manifest {
		if k != "styles" {
			nroutes++
		}
	}
	fmt.Printf("build-js ok: %d routes in %s\n", nroutes, out)
	return nil
}

type jsFuncDef struct {
	Stmt *frontend.Stmt
	File string // display path for errors (e.g. frontend/pages/home.ks)
}

type jsGlobalDef struct {
	Stmt *frontend.Stmt
	File string
}

func relFrontendKey(frontendDir, abs string) string {
	rel, err := filepath.Rel(frontendDir, abs)
	if err != nil {
		return abs
	}
	return rel
}

// loadFrontendLibs parses all frontend/**/*.ks except frontend/main.ks
// (same set as renderRoute) into func/global tables.
func loadFrontendLibs(frontendDir string) (map[string]*jsFuncDef, map[string]*jsGlobalDef, map[string]*frontend.Program, error) {
	var files []string
	_ = filepath.Walk(frontendDir, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if !info.IsDir() && strings.HasSuffix(info.Name(), ".ks") {
			files = append(files, p)
		}
		return nil
	})
	sort.Strings(files)
	funcs := map[string]*jsFuncDef{}
	globals := map[string]*jsGlobalDef{}
	progs := map[string]*frontend.Program{}
	for _, f := range files {
		rel, _ := filepath.Rel(frontendDir, f)
		// Skip entry main.ks (route table + console renderer, not shippable).
		if rel == "main.ks" {
			continue
		}
		if strings.HasSuffix(filepath.Base(f), "_test.ks") {
			continue
		}
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		// Display path matches renderRoute errors (absolute) but STRICT tests
		// only need file:line; use slash-joined frontend-relative for clarity.
		display := filepath.ToSlash(filepath.Join("frontend", rel))
		prog, err := frontend.ParseSource(string(data), display)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("build-js %s: %w", display, err)
		}
		progs[rel] = prog
		for _, st := range prog.Statements {
			if st == nil {
				continue
			}
			switch st.Kind {
			case frontend.StmtFunc:
				// Last definition wins (matches interpreter sequential exec).
				funcs[st.Name] = &jsFuncDef{Stmt: st, File: display}
			case frontend.StmtLet:
				globals[st.Name] = &jsGlobalDef{Stmt: st, File: display}
			}
		}
	}
	return funcs, globals, progs, nil
}

// entryFuncForPage resolves the page entry func for a pages/ stem.
func entryFuncForPage(base string, prog *frontend.Program, funcs map[string]*jsFuncDef) (string, error) {
	if base == "404" {
		if _, ok := funcs["404_page"]; ok {
			return "404_page", nil
		}
		if _, ok := funcs["notfound_page"]; ok {
			return "notfound_page", nil
		}
	}
	if prefix, _, fn, ok := parseDynamicFile(base); ok {
		_ = prefix
		if _, exists := funcs[fn]; exists {
			return fn, nil
		}
		// Fall through to file-local fallback (keeps filename, clear error otherwise).
	}
	clean := strings.ReplaceAll(base, "-", "_")
	expected := clean + "_page"
	if _, ok := funcs[expected]; ok {
		return expected, nil
	}
	// Fallback: first *_page func defined in this file.
	var cands []string
	if prog != nil {
		for _, st := range prog.Statements {
			if st != nil && st.Kind == frontend.StmtFunc && strings.HasSuffix(st.Name, "_page") {
				cands = append(cands, st.Name)
			}
		}
	}
	sort.Strings(cands)
	if len(cands) > 0 {
		return cands[0], nil
	}
	return "", fmt.Errorf("no page func found (want %s)", expected)
}

// closureForRoute BFSes from entry + layouts over var/call refs.
func closureForRoute(entry string, prog *frontend.Program, funcs map[string]*jsFuncDef, globals map[string]*jsGlobalDef) (map[string]bool, map[string]bool, bool, bool, bool, error) {
	usedFuncs := map[string]bool{}
	usedGlobals := map[string]bool{}
	usesRange := false
	usesLen := false
	usesIn := false

	queue := []string{entry}
	if _, ok := funcs["app_layout"]; ok {
		queue = append(queue, "app_layout")
	}
	if _, ok := funcs["_app_layout"]; ok {
		queue = append(queue, "_app_layout")
	}
	visited := map[string]bool{}
	// Collect string literals seen in used code for explicit layout refs.
	var seenStrings []string

	for len(queue) > 0 {
		name := queue[0]
		queue = queue[1:]
		if visited[name] {
			continue
		}
		visited[name] = true
		if fd, ok := funcs[name]; ok {
			usedFuncs[name] = true
			s := &jsRefScan{vars: map[string]bool{}}
			scanStmtRefs(fd.Stmt, s)
			seenStrings = append(seenStrings, s.strs...)
			if s.hasIn {
				usesIn = true
			}
			for v := range s.vars {
				if v == "range" {
					usesRange = true
				}
				if v == "len" {
					usesLen = true
				}
				if _, ok := funcs[v]; ok {
					if !visited[v] {
						queue = append(queue, v)
					}
				} else if _, ok := globals[v]; ok {
					if !visited[v] {
						queue = append(queue, v)
					}
				}
			}
		} else if gd, ok := globals[name]; ok {
			usedGlobals[name] = true
			s := &jsRefScan{vars: map[string]bool{}}
			if gd.Stmt.Expr != nil {
				scanExprRefs(gd.Stmt.Expr, s)
			}
			seenStrings = append(seenStrings, s.strs...)
			if s.hasIn {
				usesIn = true
			}
			for v := range s.vars {
				if v == "range" {
					usesRange = true
				}
				if v == "len" {
					usesLen = true
				}
				if _, ok := funcs[v]; ok {
					if !visited[v] {
						queue = append(queue, v)
					}
				} else if _, ok := globals[v]; ok {
					if !visited[v] {
						queue = append(queue, v)
					}
				}
			}
		} else if name == entry {
			return nil, nil, false, false, false, fmt.Errorf("page func %q not found", entry)
		}
		// Unknown names (builtins like ok/range/len, params, locals) are
		// leaves: no queue push. range/len flags already recorded above.
	}

	// Explicit layout refs: if used code contains string "admin" and
	// admin_layout exists, pull it (and its deps) into the closure.
	for _, s := range seenStrings {
		ln := s + "_layout"
		if _, ok := funcs[ln]; ok && !visited[ln] {
			sub, subG, rR, rL, rI, err := closureForRoute(ln, nil, funcs, globals)
			if err != nil {
				continue
			}
			for k := range sub {
				usedFuncs[k] = true
			}
			for k := range subG {
				usedGlobals[k] = true
			}
			usesRange = usesRange || rR
			usesLen = usesLen || rL
			usesIn = usesIn || rI
		}
	}
	return usedFuncs, usedGlobals, usesRange, usesLen, usesIn, nil
}

type jsRefScan struct {
	vars  map[string]bool
	strs  []string
	hasIn bool
	hasIs bool
}

func scanStmtRefs(st *frontend.Stmt, s *jsRefScan) {
	if st == nil {
		return
	}
	switch st.Kind {
	case frontend.StmtLet, frontend.StmtReturn, frontend.StmtWhile:
		if st.Expr != nil {
			scanExprRefs(st.Expr, s)
		}
	case frontend.StmtAssign:
		if st.Expr != nil {
			scanExprRefs(st.Expr, s)
		}
		for _, e := range st.Exprs {
			scanExprRefs(e, s)
		}
	case frontend.StmtFunc:
		scanStmtRefs(st.Body, s)
	case frontend.StmtBlock:
		for _, x := range st.List {
			scanStmtRefs(x, s)
		}
	case frontend.StmtIf:
		if st.Expr != nil {
			scanExprRefs(st.Expr, s)
		}
		scanStmtRefs(st.Then, s)
		scanStmtRefs(st.Else, s)
	case frontend.StmtForIn:
		if st.Expr != nil {
			scanExprRefs(st.Expr, s)
		}
		scanStmtRefs(st.Body, s)
	case frontend.StmtForC:
		scanStmtRefs(st.Init, s)
		if st.Expr != nil {
			scanExprRefs(st.Expr, s)
		}
		scanStmtRefs(st.Post, s)
		scanStmtRefs(st.Body, s)
	case frontend.StmtPrint:
		for _, e := range st.Exprs {
			scanExprRefs(e, s)
		}
		if st.Expr != nil {
			scanExprRefs(st.Expr, s)
		}
	case frontend.StmtExpr:
		if st.Expr != nil {
			scanExprRefs(st.Expr, s)
		}
	case frontend.StmtGo:
		scanStmtRefs(st.Inner, s)
	case frontend.StmtSleep, frontend.StmtDefer:
		if st.Expr != nil {
			scanExprRefs(st.Expr, s)
		}
	case frontend.StmtTry:
		scanStmtRefs(st.Then, s)
		scanStmtRefs(st.CaBody, s)
		scanStmtRefs(st.FinBody, s)
	case frontend.StmtSwitch:
		if st.Expr != nil {
			scanExprRefs(st.Expr, s)
		}
		for _, c := range st.Cases {
			if c == nil {
				continue
			}
			for _, v := range c.Values {
				scanExprRefs(v, s)
			}
			scanStmtRefs(c.Body, s)
		}
	case frontend.StmtSelect:
		for _, c := range st.SelectCases {
			if c == nil {
				continue
			}
			scanExprRefs(c.Chan, s)
			scanExprRefs(c.Value, s)
			scanExprRefs(c.Timeout, s)
			scanStmtRefs(c.Body, s)
		}
	}
}

func scanExprRefs(e *frontend.Expr, s *jsRefScan) {
	if e == nil {
		return
	}
	switch e.Kind {
	case frontend.ExprVar:
		s.vars[e.Name] = true
	case frontend.ExprString:
		s.strs = append(s.strs, e.StrVal)
	case frontend.ExprIn:
		s.hasIn = true
		scanExprRefs(e.Left, s)
		scanExprRefs(e.Right, s)
	case frontend.ExprIs:
		s.hasIs = true
		scanExprRefs(e.Left, s)
		scanExprRefs(e.Right, s)
	case frontend.ExprCall:
		scanExprRefs(e.Callee, s)
		for _, a := range e.Args {
			scanExprRefs(a, s)
		}
	case frontend.ExprIndex:
		scanExprRefs(e.Left, s)
		scanExprRefs(e.Right, s)
	case frontend.ExprSlice:
		scanExprRefs(e.Left, s)
		scanExprRefs(e.SliceStart, s)
		scanExprRefs(e.SliceEnd, s)
	case frontend.ExprArray:
		for _, el := range e.Elements {
			scanExprRefs(el, s)
		}
	case frontend.ExprMap:
		for _, v := range e.MapVals {
			scanExprRefs(v, s)
		}
	case frontend.ExprFunc:
		scanStmtRefs(e.FuncBody, s)
	case frontend.ExprNot, frontend.ExprNeg:
		// Not/Neg use Left in this frontend (see webjs.go exprToJS).
		scanExprRefs(e.Left, s)
		scanExprRefs(e.Right, s)
	default:
		scanExprRefs(e.Left, s)
		scanExprRefs(e.Right, s)
		for _, a := range e.Args {
			scanExprRefs(a, s)
		}
		for _, el := range e.Elements {
			scanExprRefs(el, s)
		}
		for _, v := range e.MapVals {
			scanExprRefs(v, s)
		}
		scanExprRefs(e.Callee, s)
		scanExprRefs(e.SliceStart, s)
		scanExprRefs(e.SliceEnd, s)
		scanStmtRefs(e.FuncBody, s)
	}
}

// emitRouteJS renders the per-route bundle (used lets + used funcs).
func emitRouteJS(base, pagePath, entry string, prog *frontend.Program, funcs map[string]*jsFuncDef, globals map[string]*jsGlobalDef, usedFuncs, usedGlobals map[string]bool, usesRange, usesLen, usesIn bool, strict bool) (string, error) {
	var b strings.Builder
	b.WriteString("// generated by fusion build-js (P3 subset, strict=" + fmt.Sprintf("%v", strict) + ")\n")
	// Document dynamic filename convention (P1 STRICT): file user_[id].ks
	// defines user_page for /user/:id but ships as user_[id].js.
	if _, param, fn, ok := parseDynamicFile(base); ok {
		b.WriteString(fmt.Sprintf("// route: /%s/:%s (dynamic %s.ks -> %s, output %s.js)\n", strings.Split(base, "_[")[0], param, base, fn, base))
	} else if base == "home" {
		b.WriteString("// route: / (home.ks -> home_page, output index.js)\n")
	} else if base == "404" {
		b.WriteString(fmt.Sprintf("// route: fallback 404 (%s -> %s)\n", base+".ks", entry))
	} else {
		b.WriteString(fmt.Sprintf("// route: /%s (%s.ks -> %s)\n", base, base, entry))
	}
	if usesRange {
		b.WriteString("function range(start, end, step) { if (end === undefined) { end = start; start = 0; step = 1; } else if (step === undefined) { step = 1; } if (step === 0) throw new Error(\"range step cannot be 0\"); var out = []; if (step > 0) { for (var i = start; i < end; i += step) out.push(i); } else { for (var i = start; i > end; i += step) out.push(i); } return out; }\n")
	}
	if usesLen {
		b.WriteString("function len(x) { if (x == null) return 0; if (Array.isArray(x) || typeof x === \"string\") return x.length; if (typeof x === \"object\") return Object.keys(x).length; return 0; }\n")
	}
	if usesIn {
		b.WriteString("function __ks_in(a, b) { if (b == null) return false; if (Array.isArray(b) || typeof b === \"string\") return b.includes(a); if (typeof b === \"object\") return (a in b); return false; }\n")
	}
	// CSS hint (not bundled): styles are copied to styles/ + listed in manifest.
	b.WriteString("// styles: <link rel=\"stylesheet\" href=\"styles/*.css\"> when frontend/styles/*.css exists (passthrough, not bundled)\n")

	var gnames []string
	for n := range usedGlobals {
		gnames = append(gnames, n)
	}
	sort.Strings(gnames)
	for _, n := range gnames {
		gd := globals[n]
		s, err := jsStmtToJS(gd.Stmt, 0, gd.File, strict)
		if err != nil {
			return "", err
		}
		if s != "" {
			b.WriteString(s + "\n")
		}
	}
	var fnames []string
	for n := range usedFuncs {
		fnames = append(fnames, n)
	}
	sort.Strings(fnames)
	for _, n := range fnames {
		fd := funcs[n]
		s, err := jsStmtToJS(fd.Stmt, 0, fd.File, strict)
		if err != nil {
			return "", err
		}
		if s != "" {
			b.WriteString(s + "\n")
		}
	}
	return b.String(), nil
}

func buildJSMinify(s string) string {
	var lines []string
	for _, l := range strings.Split(s, "\n") {
		t := strings.TrimSpace(l)
		if t == "" {
			continue
		}
		lines = append(lines, t)
	}
	return strings.Join(lines, "\n") + "\n"
}

func jsSubsetError(file string, line int, construct string) error {
	return fmt.Errorf("build-js %s:%d: %s not in JS subset, see .ks source", file, line, construct)
}

func jsStmtToJS(st *frontend.Stmt, depth int, file string, strict bool) (string, error) {
	if st == nil {
		return "", nil
	}
	ind := strings.Repeat("  ", depth)
	line := st.Line
	switch st.Kind {
	case frontend.StmtLet:
		rhs, err := jsExprToJS(st.Expr, file, line, strict)
		if err != nil {
			return "", err
		}
		return ind + "let " + st.Name + " = " + rhs + ";", nil
	case frontend.StmtAssign:
		op := st.Op
		if op == "" {
			op = "="
		}
		if st.Name != "" {
			rhs, err := jsExprToJS(st.Expr, file, line, strict)
			if err != nil {
				return "", err
			}
			return ind + st.Name + " " + op + " " + rhs + ";", nil
		}
		// Indexed/field target: `a[i] = v`, `m.key += v`.
		if st.Expr != nil && len(st.Exprs) == 1 {
			lhs, err := jsExprToJS(st.Expr, file, line, strict)
			if err != nil {
				return "", err
			}
			rhs, err := jsExprToJS(st.Exprs[0], file, line, strict)
			if err != nil {
				return "", err
			}
			return ind + lhs + " " + op + " " + rhs + ";", nil
		}
		if strict {
			return "", jsSubsetError(file, line, "assign target")
		}
		return ind + "// unsupported assign", nil
	case frontend.StmtFunc:
		params := strings.Join(st.Names, ", ")
		body, err := jsStmtToJS(st.Body, depth, file, strict)
		if err != nil {
			return "", err
		}
		return ind + "function " + st.Name + "(" + params + ") " + body, nil
	case frontend.StmtBlock:
		var b strings.Builder
		b.WriteString("{\n")
		for _, s := range st.List {
			js, err := jsStmtToJS(s, depth+1, file, strict)
			if err != nil {
				return "", err
			}
			if js == "" {
				continue
			}
			b.WriteString(js + "\n")
		}
		b.WriteString(ind + "}")
		return b.String(), nil
	case frontend.StmtIf:
		cond, err := jsExprToJS(st.Expr, file, line, strict)
		if err != nil {
			return "", err
		}
		then, err := jsStmtToJS(st.Then, depth, file, strict)
		if err != nil {
			return "", err
		}
		s := ind + "if (" + cond + ") " + then
		if st.Else != nil {
			els, err := jsStmtToJS(st.Else, depth, file, strict)
			if err != nil {
				return "", err
			}
			s += " else " + els
		}
		return s, nil
	case frontend.StmtWhile:
		cond, err := jsExprToJS(st.Expr, file, line, strict)
		if err != nil {
			return "", err
		}
		body, err := jsStmtToJS(st.Body, depth, file, strict)
		if err != nil {
			return "", err
		}
		return ind + "while (" + cond + ") " + body, nil
	case frontend.StmtForIn:
		if len(st.Names) == 1 || len(st.Names) == 2 {
			iter, err := jsExprToJS(st.Expr, file, line, strict)
			if err != nil {
				return "", err
			}
			body, err := jsStmtToJS(st.Body, depth, file, strict)
			if err != nil {
				return "", err
			}
			if len(st.Names) == 1 {
				return ind + "for (let " + st.Names[0] + " of " + iter + ") " + body, nil
			}
			return ind + "for (let [" + st.Names[0] + ", " + st.Names[1] + "] of Object.entries(" + iter + ")) " + body, nil
		}
		if strict {
			return "", jsSubsetError(file, line, "for-in")
		}
		return ind + "// unsupported for-in", nil
	case frontend.StmtForC:
		if strict {
			return "", jsSubsetError(file, line, "for-c")
		}
		return ind + "// for-c (see .ks source)", nil
	case frontend.StmtReturn:
		if st.Expr != nil {
			v, err := jsExprToJS(st.Expr, file, line, strict)
			if err != nil {
				return "", err
			}
			return ind + "return " + v + ";", nil
		}
		return ind + "return;", nil
	case frontend.StmtBreak:
		return ind + "break;", nil
	case frontend.StmtContinue:
		return ind + "continue;", nil
	case frontend.StmtPrint:
		var args []string
		for _, e := range st.Exprs {
			a, err := jsExprToJS(e, file, line, strict)
			if err != nil {
				return "", err
			}
			args = append(args, a)
		}
		if st.Expr != nil && len(st.Exprs) == 0 {
			a, err := jsExprToJS(st.Expr, file, line, strict)
			if err != nil {
				return "", err
			}
			args = append(args, a)
		}
		return ind + "console.log(" + strings.Join(args, ", ") + ");", nil
	case frontend.StmtExpr:
		v, err := jsExprToJS(st.Expr, file, line, strict)
		if err != nil {
			return "", err
		}
		if v == "" {
			return "", nil
		}
		return ind + v + ";", nil
	case frontend.StmtImport:
		// Resolved via dep graph; no JS emission.
		return "", nil
	case frontend.StmtGo:
		if strict {
			return "", jsSubsetError(file, line, "go")
		}
		return ind + "// unsupported go", nil
	case frontend.StmtSleep:
		if strict {
			return "", jsSubsetError(file, line, "sleep")
		}
		return ind + "// unsupported sleep", nil
	case frontend.StmtTry:
		if strict {
			return "", jsSubsetError(file, line, "try/catch")
		}
		return ind + "// unsupported try/catch", nil
	case frontend.StmtSwitch:
		if strict {
			return "", jsSubsetError(file, line, "switch")
		}
		return ind + "// unsupported switch", nil
	case frontend.StmtSelect:
		if strict {
			return "", jsSubsetError(file, line, "select")
		}
		return ind + "// unsupported select", nil
	case frontend.StmtDefer:
		if strict {
			return "", jsSubsetError(file, line, "defer")
		}
		return ind + "// unsupported defer", nil
	case frontend.StmtStruct:
		if strict {
			return "", jsSubsetError(file, line, "struct")
		}
		return ind + "// unsupported struct", nil
	case frontend.StmtEnum:
		if strict {
			return "", jsSubsetError(file, line, "enum")
		}
		return ind + "// unsupported enum", nil
	default:
		if strict {
			return "", jsSubsetError(file, line, "stmt")
		}
		return ind + "// unsupported stmt", nil
	}
}

func jsExprToJS(e *frontend.Expr, file string, line int, strict bool) (string, error) {
	if e == nil {
		return "null", nil
	}
	js := func(x *frontend.Expr) (string, error) { return jsExprToJS(x, file, line, strict) }
	bin := func(op string) (string, error) {
		l, err := js(e.Left)
		if err != nil {
			return "", err
		}
		r, err := js(e.Right)
		if err != nil {
			return "", err
		}
		return "(" + l + " " + op + " " + r + ")", nil
	}
	switch e.Kind {
	case frontend.ExprString:
		d, _ := json.Marshal(e.StrVal)
		return string(d), nil
	case frontend.ExprInt:
		return fmt.Sprintf("%d", e.IntVal), nil
	case frontend.ExprFloat:
		return fmt.Sprintf("%v", e.FloatVal), nil
	case frontend.ExprBool:
		if e.BoolVal {
			return "true", nil
		}
		return "false", nil
	case frontend.ExprNil:
		return "null", nil
	case frontend.ExprVar:
		return e.Name, nil
	case frontend.ExprAdd:
		return bin("+")
	case frontend.ExprSub:
		return bin("-")
	case frontend.ExprMul:
		return bin("*")
	case frontend.ExprDiv:
		return bin("/")
	case frontend.ExprMod:
		return bin("%")
	case frontend.ExprPow:
		l, err := js(e.Left)
		if err != nil {
			return "", err
		}
		r, err := js(e.Right)
		if err != nil {
			return "", err
		}
		return "Math.pow(" + l + ", " + r + ")", nil
	case frontend.ExprEq:
		return bin("===")
	case frontend.ExprNe:
		return bin("!==")
	case frontend.ExprLt:
		return bin("<")
	case frontend.ExprLe:
		return bin("<=")
	case frontend.ExprGt:
		return bin(">")
	case frontend.ExprGe:
		return bin(">=")
	case frontend.ExprAnd:
		return bin("&&")
	case frontend.ExprOr:
		return bin("||")
	case frontend.ExprNot:
		// Not uses Left in this frontend for prefix `!`/`not`.
		target := e.Left
		if target == nil {
			target = e.Right
		}
		v, err := js(target)
		if err != nil {
			return "", err
		}
		return "(!" + v + ")", nil
	case frontend.ExprNeg:
		target := e.Left
		if target == nil {
			target = e.Right
		}
		v, err := js(target)
		if err != nil {
			return "", err
		}
		return "(-" + v + ")", nil
	case frontend.ExprIn:
		l, err := js(e.Left)
		if err != nil {
			return "", err
		}
		r, err := js(e.Right)
		if err != nil {
			return "", err
		}
		return "__ks_in(" + l + ", " + r + ")", nil
	case frontend.ExprIs:
		l, err := js(e.Left)
		if err != nil {
			return "", err
		}
		if e.Right != nil && e.Right.Kind == frontend.ExprVar {
			switch e.Right.Name {
			case "nil":
				return "((" + l + " === null) || (" + l + " === undefined))", nil
			case "bool":
				return "(typeof " + l + " === \"boolean\")", nil
			case "int":
				return "(Number.isInteger(" + l + "))", nil
			case "float", "number":
				return "(typeof " + l + " === \"number\")", nil
			case "string":
				return "(typeof " + l + " === \"string\")", nil
			case "array":
				return "(Array.isArray(" + l + "))", nil
			case "map":
				return "((" + l + " !== null) && (typeof " + l + " === \"object\") && (!Array.isArray(" + l + ")))", nil
			case "func":
				return "(typeof " + l + " === \"function\")", nil
			case "any":
				return "(true)", nil
			case "ok":
				return "((" + l + " !== null) && (typeof " + l + " === \"object\") && (\"ok\" in " + l + "))", nil
			case "err":
				return "((" + l + " !== null) && (typeof " + l + " === \"object\") && (\"err\" in " + l + "))", nil
			}
		}
		if strict {
			return "", jsSubsetError(file, line, "is")
		}
		return "null", nil
	case frontend.ExprCoalesce:
		return bin("??")
	case frontend.ExprCall:
		return jsCallToJS(e, file, line, strict)
	case frontend.ExprIndex:
		l, err := js(e.Left)
		if err != nil {
			return "", err
		}
		r, err := js(e.Right)
		if err != nil {
			return "", err
		}
		if e.Safe {
			// Optional chaining: field `props?.title` -> props?.["title"].
			if e.Right != nil && e.Right.Kind == frontend.ExprString && isJSIdent(e.Right.StrVal) {
				return l + "?." + e.Right.StrVal, nil
			}
			return l + "?.[" + r + "]", nil
		}
		return l + "[" + r + "]", nil
	case frontend.ExprSlice:
		l, err := js(e.Left)
		if err != nil {
			return "", err
		}
		var s, en string
		if e.SliceStart != nil {
			s, err = js(e.SliceStart)
			if err != nil {
				return "", err
			}
		} else {
			s = ""
		}
		if e.SliceEnd != nil {
			en, err = js(e.SliceEnd)
			if err != nil {
				return "", err
			}
		} else {
			en = ""
		}
		switch {
		case s == "" && en == "":
			return l + ".slice()", nil
		case en == "":
			if s == "" {
				return l + ".slice()", nil
			}
			return l + ".slice(" + s + ")", nil
		case s == "":
			return l + ".slice(0, " + en + ")", nil
		default:
			return l + ".slice(" + s + ", " + en + ")", nil
		}
	case frontend.ExprArray:
		var els []string
		for _, el := range e.Elements {
			v, err := js(el)
			if err != nil {
				return "", err
			}
			els = append(els, v)
		}
		return "[" + strings.Join(els, ", ") + "]", nil
	case frontend.ExprMap:
		var parts []string
		for i, k := range e.MapKeys {
			v, err := js(e.MapVals[i])
			if err != nil {
				return "", err
			}
			// class/className pass through untouched as verbatim strings.
			parts = append(parts, fmt.Sprintf("%q: %s", k, v))
		}
		return "({" + strings.Join(parts, ", ") + "})", nil
	case frontend.ExprFunc:
		body, err := jsStmtToJS(e.FuncBody, 0, file, strict)
		if err != nil {
			return "", err
		}
		return "(function(" + strings.Join(e.FuncParams, ", ") + ") " + body + ")", nil
	default:
		if strict {
			return "", jsSubsetError(file, line, "expr")
		}
		return "null", nil
	}
}

func jsCallToJS(e *frontend.Expr, file string, line int, strict bool) (string, error) {
	_ = strict
	_ = file
	calleeName := ""
	if e.Callee != nil && e.Callee.Kind == frontend.ExprVar {
		calleeName = e.Callee.Name
	}
	args := make([]string, 0, len(e.Args))
	for _, a := range e.Args {
		v, err := jsExprToJS(a, file, line, strict)
		if err != nil {
			return "", err
		}
		args = append(args, v)
	}
	A := func(i int) string {
		if i < len(args) {
			return args[i]
		}
		return ""
	}
	switch calleeName {
	case "str":
		if len(args) == 1 {
			return "String(" + A(0) + ")", nil
		}
	case "int":
		if len(args) == 1 {
			return "Math.trunc(Number(" + A(0) + "))", nil
		}
	case "float":
		if len(args) == 1 {
			return "Number(" + A(0) + ")", nil
		}
	case "bool":
		if len(args) == 1 {
			return "Boolean(" + A(0) + ")", nil
		}
	case "type":
		if len(args) == 1 {
			return "(typeof " + A(0) + ")", nil
		}
	case "json_stringify":
		if len(args) == 1 {
			return "JSON.stringify(" + A(0) + ")", nil
		}
	case "json_parse":
		if len(args) == 1 {
			return "JSON.parse(" + A(0) + ")", nil
		}
	case "keys":
		if len(args) == 1 {
			return "Object.keys(" + A(0) + ")", nil
		}
	case "values":
		if len(args) == 1 {
			return "Object.values(" + A(0) + ")", nil
		}
	case "upper":
		if len(args) == 1 {
			return "(String(" + A(0) + ").toUpperCase())", nil
		}
	case "lower":
		if len(args) == 1 {
			return "(String(" + A(0) + ").toLowerCase())", nil
		}
	case "trim":
		if len(args) == 1 {
			return "(String(" + A(0) + ").trim())", nil
		}
	case "split":
		if len(args) == 2 {
			return "(String(" + A(0) + ").split(" + A(1) + "))", nil
		}
	case "join":
		if len(args) == 2 {
			return "(" + A(0) + ".join(" + A(1) + "))", nil
		}
	case "push":
		if len(args) == 2 {
			return "(" + A(0) + ".push(" + A(1) + "))", nil
		}
	case "pop":
		if len(args) == 1 {
			return "(" + A(0) + ".pop())", nil
		}
	case "ok":
		if len(args) == 1 {
			return "({ok: " + A(0) + "})", nil
		}
	case "err":
		if len(args) == 1 {
			return "({err: String(" + A(0) + ")})", nil
		}
	case "is_ok":
		if len(args) == 1 {
			return "((" + A(0) + " !== null) && (typeof " + A(0) + " === \"object\") && (\"ok\" in " + A(0) + "))", nil
		}
	case "is_err":
		if len(args) == 1 {
			return "((" + A(0) + " !== null) && (typeof " + A(0) + " === \"object\") && (\"err\" in " + A(0) + "))", nil
		}
	}
	callee, err := jsExprToJS(e.Callee, file, line, strict)
	if err != nil {
		return "", err
	}
	return callee + "(" + strings.Join(args, ", ") + ")", nil
}

func isJSIdent(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		if r == '_' || r == '$' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
			continue
		}
		if i > 0 && r >= '0' && r <= '9' {
			continue
		}
		return false
	}
	return true
}

// copyStylesPassthrough copies frontend/styles/*.css to <out>/styles/.
// Returns slash-joined relative paths for the manifest hint.
func copyStylesPassthrough(frontendDir, out string) ([]string, error) {
	srcDir := filepath.Join(frontendDir, "styles")
	ents, err := os.ReadDir(srcDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	dstDir := filepath.Join(out, "styles")
	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		return nil, err
	}
	var rels []string
	for _, e := range ents {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".css") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(srcDir, e.Name()))
		if err != nil {
			return nil, err
		}
		if err := os.WriteFile(filepath.Join(dstDir, e.Name()), data, 0o644); err != nil {
			return nil, err
		}
		rels = append(rels, filepath.ToSlash(filepath.Join("styles", e.Name())))
	}
	sort.Strings(rels)
	return rels, nil
}
