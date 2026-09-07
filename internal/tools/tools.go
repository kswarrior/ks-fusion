// Package tools implements fusion fmt/vet/doc/check/repl/bench (v2.2).
package tools

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/kswarrior/ks-fusion/internal/backend"
	"github.com/kswarrior/ks-fusion/internal/frontend"
)

// ---------------------------------------------------------------------------
// fmt
// ---------------------------------------------------------------------------

// FormatSource returns canonical formatting of .ks source.
// Rules (v1): tabs->2 spaces, trim trailing whitespace, re-indent by brace
// depth (outside strings/comments), braces `} else {` same line normalisation,
// single trailing newline. Idempotent.
func FormatSource(src string) string {
	lines := strings.Split(src, "\n")
	out := make([]string, 0, len(lines))
	depth := 0
	for _, raw := range lines {
		// expand tabs
		line := strings.ReplaceAll(raw, "\t", "  ")
		// trim trailing whitespace
		line = strings.TrimRight(line, " \t\r")
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			out = append(out, "")
			continue
		}
		// dedent for leading closing braces
		leadClose := 0
		for _, ch := range trimmed {
			if ch == '}' {
				leadClose++
			} else {
				break
			}
		}
		d := depth - leadClose
		if d < 0 {
			d = 0
		}
		indent := strings.Repeat("  ", d)
		// normalize spaces around = == , : (outside strings) - light pass
		normalized := normalizeSpaces(trimmed)
		out = append(out, indent+normalized)
		// update depth by brace balance outside strings/comments
		depth += braceDelta(line)
		if depth < 0 {
			depth = 0
		}
	}
	// collapse 3+ blank lines to max 1 blank, ensure single trailing newline
	var cleaned []string
	blanks := 0
	for _, l := range out {
		if strings.TrimSpace(l) == "" {
			blanks++
			if blanks <= 1 {
				cleaned = append(cleaned, "")
			}
			continue
		}
		blanks = 0
		cleaned = append(cleaned, l)
	}
	s := strings.Join(cleaned, "\n")
	s = strings.TrimRight(s, "\n") + "\n"
	return s
}

func braceDelta(line string) int {
	open, close := 0, 0
	inD, inS := false, false
	inBlock := false
	// strip line comments (# and //) outside strings
	for i := 0; i < len(line); i++ {
		c := line[i]
		if inBlock {
			if c == '*' && i+1 < len(line) && line[i+1] == '/' {
				inBlock = false
				i++
			}
			continue
		}
		if inD {
			if c == '\\' {
				i++
				continue
			}
			if c == '"' {
				inD = false
			}
			continue
		}
		if inS {
			if c == '\\' {
				i++
				continue
			}
			if c == '\'' {
				inS = false
			}
			continue
		}
		if c == '"' {
			inD = true
			continue
		}
		if c == '\'' {
			inS = true
			continue
		}
		if c == '/' && i+1 < len(line) && line[i+1] == '*' {
			inBlock = true
			i++
			continue
		}
		if c == '#' {
			break
		}
		if c == '/' && i+1 < len(line) && line[i+1] == '/' {
			break
		}
		if c == '{' {
			open++
		} else if c == '}' {
			close++
		}
	}
	return open - close
}

func normalizeSpaces(s string) string {
	// light normalisation: ensure single space after commas, around = (not ==, !=, <=, >=)
	// Keep it conservative to stay idempotent and never touch strings.
	var b strings.Builder
	inD, inS := false, false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inD {
			b.WriteByte(c)
			if c == '\\' && i+1 < len(s) {
				i++
				b.WriteByte(s[i])
				continue
			}
			if c == '"' {
				inD = false
			}
			continue
		}
		if inS {
			b.WriteByte(c)
			if c == '\\' && i+1 < len(s) {
				i++
				b.WriteByte(s[i])
				continue
			}
			if c == '\'' {
				inS = false
			}
			continue
		}
		if c == '"' {
			inD = true
			b.WriteByte(c)
			continue
		}
		if c == '\'' {
			inS = true
			b.WriteByte(c)
			continue
		}
		// comma -> ", "
		if c == ',' {
			b.WriteByte(',')
			if i+1 < len(s) && s[i+1] != ' ' && s[i+1] != '\n' {
				b.WriteByte(' ')
			}
			continue
		}
		b.WriteByte(c)
	}
	// fix "}else{" -> "} else {" and "){" -> ") {"
	r := b.String()
	r = strings.ReplaceAll(r, "}else{", "} else {")
	r = strings.ReplaceAll(r, "}else {", "} else {")
	r = strings.ReplaceAll(r, "} else{", "} else {")
	r = strings.ReplaceAll(r, "){", ") {")
	return r
}

// FmtFile formats one file in place. Returns true if changed.
func FmtFile(path string) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	formatted := FormatSource(string(data))
	if formatted == string(data) {
		return false, nil
	}
	if err := os.WriteFile(path, []byte(formatted), 0o644); err != nil {
		return false, err
	}
	return true, nil
}

func collectKsFiles(target string) ([]string, error) {
	fi, err := os.Stat(target)
	if err != nil {
		return nil, err
	}
	if !fi.IsDir() {
		if strings.HasSuffix(target, ".ks") {
			return []string{target}, nil
		}
		return nil, fmt.Errorf("not a .ks file: %s", target)
	}
	var out []string
	err = filepath.Walk(target, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if info.Name() == "vendor" || info.Name() == ".git" || info.Name() == "target" {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(info.Name(), ".ks") {
			out = append(out, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(out)
	return out, nil
}

// FmtTarget formats all .ks under target. If check is true, no writes, returns dirty list.
func FmtTarget(target string, check bool) (dirty []string, changed int, err error) {
	files, err := collectKsFiles(target)
	if err != nil {
		return nil, 0, err
	}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			return dirty, changed, err
		}
		formatted := FormatSource(string(data))
		if formatted != string(data) {
			dirty = append(dirty, f)
			if !check {
				if err := os.WriteFile(f, []byte(formatted), 0o644); err != nil {
					return dirty, changed, err
				}
				changed++
			}
		}
	}
	return dirty, changed, nil
}

// ---------------------------------------------------------------------------
// vet
// ---------------------------------------------------------------------------

type VetIssue struct {
	File    string
	Line    int
	Rule    string
	Msg     string
	IsError bool
}

func (v VetIssue) String() string {
	kind := "warn"
	if v.IsError {
		kind = "error"
	}
	return fmt.Sprintf("%s:%d: %s: %s [%s]", v.File, v.Line, v.Rule, v.Msg, kind)
}

type vetScope struct {
	defined map[string]int  // name -> line
	used    map[string]bool
	params  map[string]bool
}

type vetter struct {
	file       string
	issues     []VetIssue
	scopes     []*vetScope
	funcArity  map[string]int
	builtins   map[string]bool
	globalLets map[string]bool
	isFrontend bool
	// v2.5 nominal types for exhaustive-switch: enum name -> variants,
	// var name -> declared type annotation (from `let x: T`).
	enums    map[string][]string
	varTypes map[string]string
	// P0 frontend rules: for-in loop vars (stack) to detect index keys,
	// curLine tracks the nearest enclosing Stmt line (Expr has no Line).
	loopVars []map[string]bool
	curLine  int
}

func newVetter(file string) *vetter {
	bm := map[string]bool{}
	for _, n := range backend.BuiltinNames() {
		bm[n] = true
	}
	// keywords that look like vars but aren't
	for _, k := range []string{"true", "false", "nil"} {
		bm[k] = true
	}
	// NOTE: isFrontend is a minimal strings.Contains(path,"frontend/") check.
	// Temp-path bypass (documented): t.TempDir() paths lack "frontend/", so
	// unit tests must create a frontend/ subdir (or vet via VetTarget on such
	// a layout); otherwise frontend rules silently skip. Kept minimal for P0
	// (no abs-path / alias / case handling). Do not touch webjs.go routing.
	isFE := strings.Contains(filepath.ToSlash(file), "frontend/")
	if isFE {
		// Frontend JS-bridge names (no backend builtin yet): whitelist so the
		// frontend-set-html rule — not unknown-var — owns these calls.
		bm["set_html"] = true
		bm["js_call"] = true
	}
	return &vetter{
		file: file,
		funcArity: map[string]int{},
		builtins: bm,
		isFrontend: isFE,
		enums: map[string][]string{},
		varTypes: map[string]string{},
	}
}

func (v *vetter) pushScope() {
	v.scopes = append(v.scopes, &vetScope{defined: map[string]int{}, used: map[string]bool{}, params: map[string]bool{}})
}
func (v *vetter) popScope() {
	if len(v.scopes) == 0 {
		return
	}
	top := v.scopes[len(v.scopes)-1]
	for name, line := range top.defined {
		if strings.HasPrefix(name, "_") {
			continue
		}
		if !top.used[name] && !top.params[name] {
			v.issues = append(v.issues, VetIssue{File: v.file, Line: line, Rule: "unused-let", Msg: fmt.Sprintf("unused variable %q (prefix _ to silence)", name)})
		}
	}
	v.scopes = v.scopes[:len(v.scopes)-1]
}
func (v *vetter) define(name string, line int, isParam bool) {
	if len(v.scopes) == 0 {
		v.pushScope()
	}
	top := v.scopes[len(v.scopes)-1]
	if _, ok := top.defined[name]; !ok {
		top.defined[name] = line
	}
	if isParam {
		top.params[name] = true
		top.used[name] = true // params start as used to avoid noise; marked truly used on read
		top.used[name] = false
	}
}
func (v *vetter) use(name string) {
	for i := len(v.scopes) - 1; i >= 0; i-- {
		if _, ok := v.scopes[i].defined[name]; ok {
			v.scopes[i].used[name] = true
			return
		}
	}
}
func (v *vetter) isDefined(name string) bool {
	for i := len(v.scopes) - 1; i >= 0; i-- {
		if _, ok := v.scopes[i].defined[name]; ok {
			return true
		}
	}
	if v.builtins[name] {
		return true
	}
	if _, ok := v.funcArity[name]; ok {
		return true
	}
	if v.globalLets != nil && v.globalLets[name] {
		return true
	}
	return false
}

func VetFile(path string) ([]VetIssue, error) {
	return vetFileWithGlobals(path, nil, nil)
}

func vetFileWithGlobals(path string, globalFuncs map[string]int, globalLets map[string]bool) ([]VetIssue, error) {
	return vetFileWithGlobalsEnums(path, globalFuncs, globalLets, nil, nil)
}

func vetFileWithGlobalsEnums(path string, globalFuncs map[string]int, globalLets map[string]bool, globalEnums map[string][]string, globalTypes map[string]string) ([]VetIssue, error) {
	prog, err := frontend.ParseFile(path)
	if err != nil {
		return []VetIssue{{File: path, Line: 0, Rule: "parse", Msg: err.Error(), IsError: true}}, nil
	}
	v := newVetter(path)
	v.globalLets = globalLets
	for k, vs := range globalEnums {
		v.enums[k] = append([]string{}, vs...)
	}
	for k, t := range globalTypes {
		v.varTypes[k] = t
	}
	// pre-pass: collect top-level func names for arity
	for _, st := range prog.Statements {
		if st.Kind == frontend.StmtFunc {
			v.funcArity[st.Name] = len(st.Names)
		}
	}
	// merge globals (flat namespace: all files in app + imported libs share globals)
	hasImport := false
	for _, st := range prog.Statements {
		if st.Kind == frontend.StmtImport {
			hasImport = true
			break
		}
	}
	for name, ar := range globalFuncs {
		if _, ok := v.funcArity[name]; !ok {
			v.funcArity[name] = ar
		}
	}
	v.pushScope()
	// define top-level funcs in outer scope
	for name := range v.funcArity {
		v.define(name, 1, false)
		v.scopes[len(v.scopes)-1].used[name] = true
	}
	for _, st := range prog.Statements {
		v.walkStmt(st)
	}
	v.popScope()
	// if file has imports, unknown-var/arity from libs can't be resolved statically:
	// downgrade those errors to warns to avoid false positives (flat globals + .kslib).
	if hasImport {
		for i := range v.issues {
			if v.issues[i].Rule == "unknown-var" || v.issues[i].Rule == "arity" {
				v.issues[i].IsError = false
			}
		}
	}
	// frontend rules via text scan
	v.frontendTextRules(path)
	// drop unknown-var errors that match globals discovered after (safety)
	sort.Slice(v.issues, func(i, j int) bool {
		if v.issues[i].Line == v.issues[j].Line {
			return v.issues[i].Rule < v.issues[j].Rule
		}
		return v.issues[i].Line < v.issues[j].Line
	})
	return v.issues, nil
}

func (v *vetter) frontendTextRules(path string) {
	// See newVetter NOTE: temp paths without "frontend/" skip these rules.
	if !v.isFrontend {
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	lines := strings.Split(string(data), "\n")
	inBlock := false
	for i, raw := range lines {
		code := stripFrontendCode(raw, &inBlock)
		if !hasEnvCall(code) {
			continue
		}
		// ROUTE is the allowed routing exception: env("ROUTE", ...) in
		// frontend/main.ks only. Check raw line for quoted ROUTE.
		if strings.Contains(raw, `"ROUTE"`) || strings.Contains(raw, `'ROUTE'`) {
			continue
		}
		v.issues = append(v.issues, VetIssue{File: path, Line: i + 1, Rule: "frontend-env", Msg: "env() in frontend/ is server-only; keep secrets in backend/", IsError: true})
	}
}

// stripFrontendCode removes /* */ block comments (via inBlock across lines),
// string contents ("..." and '...', with escapes), and # / // line comments.
// Returns code-only text for env( detection, so "env(" inside strings or
// comments never flags.
func stripFrontendCode(line string, inBlock *bool) string {
	var b strings.Builder
	b.Grow(len(line))
	inD, inS := false, false
	for j := 0; j < len(line); {
		c := line[j]
		if *inBlock {
			if c == '*' && j+1 < len(line) && line[j+1] == '/' {
				*inBlock = false
				j += 2
				continue
			}
			j++
			continue
		}
		if inD {
			if c == '\\' {
				j += 2
				continue
			}
			if c == '"' {
				inD = false
			}
			j++
			continue
		}
		if inS {
			if c == '\\' {
				j += 2
				continue
			}
			if c == '\'' {
				inS = false
			}
			j++
			continue
		}
		if c == '"' {
			inD = true
			j++
			continue
		}
		if c == '\'' {
			inS = true
			j++
			continue
		}
		if c == '/' && j+1 < len(line) && line[j+1] == '*' {
			*inBlock = true
			j += 2
			continue
		}
		if c == '/' && j+1 < len(line) && line[j+1] == '/' {
			break // // comment
		}
		if c == '#' {
			break // # comment
		}
		b.WriteByte(c)
		j++
	}
	return b.String()
}

// hasEnvCall reports `env (` (allow space) with word boundaries.
func hasEnvCall(code string) bool {
	for i := 0; i+3 <= len(code); i++ {
		if code[i] != 'e' || i+3 > len(code) || code[i:i+3] != "env" {
			continue
		}
		if i > 0 && isEnvIdentChar(code[i-1]) {
			continue
		}
		if i+3 < len(code) && isEnvIdentChar(code[i+3]) {
			continue
		}
		j := i + 3
		for j < len(code) && (code[j] == ' ' || code[j] == '\t') {
			j++
		}
		if j < len(code) && code[j] == '(' {
			return true
		}
	}
	return false
}

func isEnvIdentChar(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

func (v *vetter) walkStmt(st *frontend.Stmt) {
	if st == nil {
		return
	}
	prevLine := v.curLine
	v.curLine = st.Line
	defer func() { v.curLine = prevLine }()
	switch st.Kind {
	case frontend.StmtLet:
		if st.Expr != nil {
			v.walkExpr(st.Expr)
		}
		v.define(st.Name, st.Line, false)
		if st.TypeAnn != "" {
			v.varTypes[st.Name] = st.TypeAnn
		}
	case frontend.StmtStruct:
		// register struct name so later `let x: Name` resolves; fields are
		// types only (no runtime vars to walk).
		v.varTypes[st.Name] = "struct"
	case frontend.StmtEnum:
		v.enums[st.Name] = append([]string{}, st.Variants...)
	case frontend.StmtAssign:
		v.walkExpr(st.Expr)
		if !v.isDefined(st.Name) {
			v.issues = append(v.issues, VetIssue{File: v.file, Line: st.Line, Rule: "unknown-var", Msg: fmt.Sprintf("assignment to undefined %q (missing let?)", st.Name), IsError: true})
		} else {
			v.use(st.Name)
		}
	case frontend.StmtPrint:
		for _, e := range st.Exprs {
			v.walkExpr(e)
		}
	case frontend.StmtGo:
		v.walkStmt(st.Inner)
	case frontend.StmtSleep:
		if st.Expr != nil {
			v.walkExpr(st.Expr)
		}
	case frontend.StmtIf:
		v.walkExpr(st.Expr)
		v.pushScope()
		v.walkStmt(st.Then)
		v.popScope()
		if st.Else != nil {
			// else may be block or if
			if st.Else.Kind == frontend.StmtBlock {
				v.pushScope()
				v.walkStmt(st.Else)
				v.popScope()
			} else {
				v.walkStmt(st.Else)
			}
		}
	case frontend.StmtWhile:
		v.walkExpr(st.Expr)
		v.pushScope()
		v.walkStmt(st.Body)
		v.popScope()
	case frontend.StmtForIn:
		v.walkExpr(st.Expr)
		v.pushScope()
		for _, n := range st.Names {
			v.define(n, st.Line, true)
		}
		v.pushLoopVars(st.Names)
		v.walkStmt(st.Body)
		v.popLoopVars()
		v.popScope()
	case frontend.StmtForC:
		v.pushScope()
		if st.Init != nil {
			v.walkStmt(st.Init)
		}
		if st.Expr != nil {
			v.walkExpr(st.Expr)
		}
		if st.Post != nil {
			// post is usually assign; walk without new scope
			v.walkStmt(st.Post)
		}
		if st.Body != nil {
			// body block shares for-c scope (no extra push; body is block which pushes)
			v.walkStmt(st.Body)
		}
		v.popScope()
	case frontend.StmtFunc:
		// define func name in current scope
		v.define(st.Name, st.Line, false)
		v.use(st.Name) // self-mark to avoid unused warning for entry funcs? keep used=true
		v.scopes[len(v.scopes)-1].used[st.Name] = true
		v.funcArity[st.Name] = len(st.Names)
		v.pushScope()
		for _, p := range st.Names {
			v.define(p, st.Line, true)
		}
		v.walkStmt(st.Body)
		v.popScope()
	case frontend.StmtReturn:
		if st.Expr != nil {
			v.walkExpr(st.Expr)
		}
	case frontend.StmtBlock:
		v.pushScope()
		for _, s := range st.List {
			v.walkStmt(s)
		}
		v.popScope()
	case frontend.StmtExpr:
		v.walkExpr(st.Expr)
	case frontend.StmtImport:
		// no vars
	case frontend.StmtTry:
		v.pushScope()
		v.walkStmt(st.Then)
		v.popScope()
		if st.CaBody != nil {
			v.pushScope()
			if st.Catch != "" {
				v.define(st.Catch, st.Line, true)
			}
			v.walkStmt(st.CaBody)
			v.popScope()
		}
		if st.FinBody != nil {
			v.pushScope()
			v.walkStmt(st.FinBody)
			v.popScope()
		}
	case frontend.StmtSwitch:
		v.walkExpr(st.Expr)
		hasDefault := false
		covered := map[string]bool{}
		for _, c := range st.Cases {
			if c.IsDefault {
				hasDefault = true
			}
			for _, val := range c.Values {
				v.walkExpr(val)
				if s, ok := stringLiteral(val); ok {
					covered[s] = true
				}
				if b, ok := boolLiteral(val); ok {
					if b {
						covered["true"] = true
					} else {
						covered["false"] = true
					}
				}
			}
			v.pushScope()
			v.walkStmt(c.Body)
			v.popScope()
		}
		// Real exhaustiveness (v2.5): a `default` always covers; otherwise,
		// a switch on an enum-typed var must name every variant, and a
		// switch on a bool-typed var must cover true+false.
		if hasDefault {
			break
		}
		if enumName := v.switchEnumTarget(st.Expr); enumName != "" {
			variants := v.enums[enumName]
			var missing []string
			for _, m := range variants {
				if !covered[m] {
					missing = append(missing, m)
				}
			}
			if len(missing) > 0 {
				v.issues = append(v.issues, VetIssue{File: v.file, Line: st.Line, Rule: "exhaustive-switch", Msg: fmt.Sprintf("non-exhaustive switch on enum %q: missing %s", enumName, strings.Join(missing, ", "))})
			}
			break
		}
		if v.switchIsBool(st.Expr) {
			if !covered["true"] || !covered["false"] {
				v.issues = append(v.issues, VetIssue{File: v.file, Line: st.Line, Rule: "exhaustive-switch", Msg: "non-exhaustive switch on bool: cover true, false or add default"})
			}
			break
		}
		v.issues = append(v.issues, VetIssue{File: v.file, Line: st.Line, Rule: "exhaustive-switch", Msg: "non-exhaustive switch (add default for enum/union coverage)"})
	case frontend.StmtSelect:
		for _, c := range st.SelectCases {
			switch c.Kind {
			case "recv":
				if c.Chan != nil {
					v.walkExpr(c.Chan)
				}
				if c.Bind != "" {
					v.pushScope()
					v.define(c.Bind, c.Line, true)
					v.walkStmt(c.Body)
					v.popScope()
				} else {
					v.pushScope()
					v.walkStmt(c.Body)
					v.popScope()
				}
			case "send":
				if c.Chan != nil {
					v.walkExpr(c.Chan)
				}
				if c.Value != nil {
					v.walkExpr(c.Value)
				}
				v.pushScope()
				v.walkStmt(c.Body)
				v.popScope()
			case "timeout":
				if c.Timeout != nil {
					v.walkExpr(c.Timeout)
				}
				v.pushScope()
				v.walkStmt(c.Body)
				v.popScope()
			default:
				v.pushScope()
				v.walkStmt(c.Body)
				v.popScope()
			}
		}
	case frontend.StmtDefer:
		v.walkExpr(st.Expr)
	case frontend.StmtBreak, frontend.StmtContinue:
		// StmtStruct/StmtEnum handled at top of walkStmt (no body to walk)
	}
}

// stringLiteral returns the string value when e is a "lit" or 'lit' literal.
func stringLiteral(e *frontend.Expr) (string, bool) {
	if e == nil || e.Kind != frontend.ExprString {
		return "", false
	}
	return e.StrVal, true
}

// boolLiteral returns the bool value when e is a true/false literal.
func boolLiteral(e *frontend.Expr) (bool, bool) {
	if e == nil || e.Kind != frontend.ExprBool {
		return false, false
	}
	return e.BoolVal, true
}

// switchEnumTarget returns the enum type name when the switch target is a
// variable declared with an enum type (`let c: Color`), else "".
func (v *vetter) switchEnumTarget(e *frontend.Expr) string {
	if e == nil || e.Kind != frontend.ExprVar {
		return ""
	}
	t, ok := v.varTypes[e.Name]
	if !ok {
		return ""
	}
	if _, ok := v.enums[t]; ok {
		return t
	}
	return ""
}

// switchIsBool reports whether the switch target is bool-typed.
func (v *vetter) switchIsBool(e *frontend.Expr) bool {
	if e == nil || e.Kind != frontend.ExprVar {
		return false
	}
	t, ok := v.varTypes[e.Name]
	return ok && t == "bool"
}

func (v *vetter) pushLoopVars(names []string) {
	m := map[string]bool{}
	for _, n := range names {
		m[n] = true
	}
	v.loopVars = append(v.loopVars, m)
}

func (v *vetter) popLoopVars() {
	if len(v.loopVars) == 0 {
		return
	}
	v.loopVars = v.loopVars[:len(v.loopVars)-1]
}

func (v *vetter) isLoopVar(name string) bool {
	for i := len(v.loopVars) - 1; i >= 0; i-- {
		if v.loopVars[i][name] {
			return true
		}
	}
	return false
}

func isExprLiteral(e *frontend.Expr) bool {
	if e == nil {
		return false
	}
	switch e.Kind {
	case frontend.ExprString, frontend.ExprInt, frontend.ExprFloat, frontend.ExprBool, frontend.ExprNil:
		return true
	default:
		return false
	}
}

func (v *vetter) frontendLine() int {
	if v.curLine > 0 {
		return v.curLine
	}
	return 1
}

// checkFrontendSetHTML flags non-literal HTML: set_html(non-literal) and
// js_call("set_html", non-literal). Literal strings are allowed (allowlisted
// raw-HTML path per plan/frontend.md safety rule).
func (v *vetter) checkFrontendSetHTML(e *frontend.Expr) {
	if e.Callee == nil || e.Callee.Kind != frontend.ExprVar {
		return
	}
	line := v.frontendLine()
	switch e.Callee.Name {
	case "set_html":
		if len(e.Args) >= 1 && !isExprLiteral(e.Args[0]) {
			v.issues = append(v.issues, VetIssue{File: v.file, Line: line, Rule: "frontend-set-html", Msg: "set_html first arg must be a string literal (non-literal HTML needs sanitizer + allowlist)", IsError: true})
		}
	case "js_call":
		if len(e.Args) >= 2 {
			if s, ok := stringLiteral(e.Args[0]); ok && s == "set_html" {
				if !isExprLiteral(e.Args[1]) {
					v.issues = append(v.issues, VetIssue{File: v.file, Line: line, Rule: "frontend-set-html", Msg: `js_call("set_html", ...) second arg must be a string literal`, IsError: true})
				}
			}
		}
	}
}

// checkFrontendKey flags view-model maps ({type + props/children}) missing
// "key", and key: <for-in loop var> (index keys must be stable, not indices).
func (v *vetter) checkFrontendKey(e *frontend.Expr) {
	hasType, hasPropsOrChildren, hasKey := false, false, false
	keyIdx := -1
	for i, k := range e.MapKeys {
		switch k {
		case "type":
			hasType = true
		case "props", "children":
			hasPropsOrChildren = true
		case "key":
			hasKey = true
			keyIdx = i
		}
	}
	if !(hasType && hasPropsOrChildren) {
		return
	}
	line := v.frontendLine()
	if !hasKey {
		v.issues = append(v.issues, VetIssue{File: v.file, Line: line, Rule: "frontend-key", Msg: `view-model map {type, props, children} missing required "key" (stable key, not index)`, IsError: true})
		return
	}
	if keyIdx >= 0 && keyIdx < len(e.MapVals) {
		kv := e.MapVals[keyIdx]
		if kv != nil && kv.Kind == frontend.ExprVar && v.isLoopVar(kv.Name) {
			v.issues = append(v.issues, VetIssue{File: v.file, Line: line, Rule: "frontend-key", Msg: fmt.Sprintf("index key %q: use stable id, not for-in loop var", kv.Name), IsError: true})
		}
	}
}

func (v *vetter) walkExpr(e *frontend.Expr) {
	if e == nil {
		return
	}
	switch e.Kind {
	case frontend.ExprVar:
		if !v.isDefined(e.Name) {
			v.issues = append(v.issues, VetIssue{File: v.file, Line: 0, Rule: "unknown-var", Msg: fmt.Sprintf("unknown variable %q", e.Name), IsError: true})
		} else {
			v.use(e.Name)
		}
	case frontend.ExprCall:
		// callee
		if e.Callee != nil {
			// arity check for direct var calls to user funcs
			if e.Callee.Kind == frontend.ExprVar {
				if arity, ok := v.funcArity[e.Callee.Name]; ok {
					if len(e.Args) != arity {
						v.issues = append(v.issues, VetIssue{File: v.file, Line: 0, Rule: "arity", Msg: fmt.Sprintf("func %q wants %d args, got %d", e.Callee.Name, arity, len(e.Args)), IsError: true})
					}
				}
				// unknown func check
				if !v.isDefined(e.Callee.Name) {
					v.issues = append(v.issues, VetIssue{File: v.file, Line: 0, Rule: "unknown-var", Msg: fmt.Sprintf("call to undefined %q", e.Callee.Name), IsError: true})
				} else {
					v.use(e.Callee.Name)
				}
				if v.isFrontend {
					v.checkFrontendSetHTML(e)
				}
			} else {
				v.walkExpr(e.Callee)
				if v.isFrontend {
					v.checkFrontendSetHTML(e)
				}
			}
		}
		for _, a := range e.Args {
			v.walkExpr(a)
		}
	case frontend.ExprIndex:
		v.walkExpr(e.Left)
		if e.Right != nil {
			v.walkExpr(e.Right)
		}
	case frontend.ExprSlice:
		v.walkExpr(e.Left)
		if e.SliceStart != nil {
			v.walkExpr(e.SliceStart)
		}
		if e.SliceEnd != nil {
			v.walkExpr(e.SliceEnd)
		}
	case frontend.ExprArray:
		for _, el := range e.Elements {
			v.walkExpr(el)
		}
	case frontend.ExprMap:
		if v.isFrontend {
			v.checkFrontendKey(e)
		}
		for _, mv := range e.MapVals {
			v.walkExpr(mv)
		}
	case frontend.ExprFunc:
		v.pushScope()
		for _, p := range e.FuncParams {
			v.define(p, 0, true)
		}
		v.walkStmt(e.FuncBody)
		v.popScope()
	default:
		// binary/unary: walk Left/Right
		if e.Left != nil {
			v.walkExpr(e.Left)
		}
		if e.Right != nil {
			v.walkExpr(e.Right)
		}
		// Args for safety
		for _, a := range e.Args {
			v.walkExpr(a)
		}
		for _, el := range e.Elements {
			v.walkExpr(el)
		}
		for _, mv := range e.MapVals {
			v.walkExpr(mv)
		}
		if e.Callee != nil && e.Kind != frontend.ExprCall {
			v.walkExpr(e.Callee)
		}
		if e.SliceStart != nil && e.Kind != frontend.ExprSlice {
			v.walkExpr(e.SliceStart)
		}
		if e.SliceEnd != nil && e.Kind != frontend.ExprSlice {
			v.walkExpr(e.SliceEnd)
		}
	}
}

// VetTarget vets all .ks under target (flat-globals aware: collects all
// top-level funcs/lets first so cross-file imports don't false-positive).
func VetTarget(target string, denyWarns bool) ([]VetIssue, error) {
	files, err := collectKsFiles(target)
	if err != nil {
		return nil, err
	}
	globalFuncs := map[string]int{}
	globalLets := map[string]bool{}
	globalEnums := map[string][]string{}
	globalTypes := map[string]string{}
	for _, f := range files {
		prog, err := frontend.ParseFile(f)
		if err != nil {
			continue
		}
		for _, st := range prog.Statements {
			if st.Kind == frontend.StmtFunc {
				if _, ok := globalFuncs[st.Name]; !ok {
					globalFuncs[st.Name] = len(st.Names)
				}
			}
			if st.Kind == frontend.StmtLet {
				globalLets[st.Name] = true
				if st.TypeAnn != "" {
					globalTypes[st.Name] = st.TypeAnn
				}
			}
			if st.Kind == frontend.StmtEnum {
				if _, ok := globalEnums[st.Name]; !ok {
					globalEnums[st.Name] = append([]string{}, st.Variants...)
				}
			}
		}
	}
	var all []VetIssue
	for _, f := range files {
		iss, err := vetFileWithGlobalsEnums(f, globalFuncs, globalLets, globalEnums, globalTypes)
		if err != nil {
			return nil, err
		}
		all = append(all, iss...)
	}
	return all, nil
}

// ---------------------------------------------------------------------------
// doc
// ---------------------------------------------------------------------------

// DocTarget extracts `#` comments + func signatures.
func DocTarget(target string) (string, error) {
	files, err := collectKsFiles(target)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	b.WriteString("# API docs (generated by `fusion doc`)\n\n")
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			return "", err
		}
		prog, err := frontend.ParseSource(string(data), f)
		if err != nil {
			continue
		}
		rel := f
		if r, err := filepath.Rel(".", f); err == nil && !strings.HasPrefix(r, "..") {
			rel = filepath.ToSlash(r)
		}
		b.WriteString("## " + rel + "\n\n")
		// top comments
		lines := strings.Split(string(data), "\n")
		for _, l := range lines {
			t := strings.TrimSpace(l)
			if strings.HasPrefix(t, "#") {
				b.WriteString(strings.TrimSpace(strings.TrimPrefix(t, "#")) + "\n")
			} else if t != "" {
				break
			}
		}
		b.WriteString("\n")
		for _, st := range prog.Statements {
			if st.Kind == frontend.StmtFunc {
				sig := "func " + st.Name + "(" + strings.Join(st.Names, ", ") + ")"
				if st.ReturnType != "" {
					sig += ": " + st.ReturnType
				}
				b.WriteString("- `" + sig + "` (" + rel + ":" + fmt.Sprint(st.Line) + ")\n")
			}
		}
		b.WriteString("\n")
	}
	return b.String(), nil
}

// ---------------------------------------------------------------------------
// check (parse + vet errors + type annotations)
// ---------------------------------------------------------------------------

func CheckTarget(target string) ([]VetIssue, error) {
	issues, err := VetTarget(target, false)
	if err != nil {
		return nil, err
	}
	var errs []VetIssue
	for _, is := range issues {
		if is.IsError {
			errs = append(errs, is)
		}
	}
	return errs, nil
}

// ---------------------------------------------------------------------------
// repl
// ---------------------------------------------------------------------------

func Repl() error {
	fmt.Println("ks-fusion v2.2 repl (type :quit to exit, :help for help)")
	in := backend.New()
	sc := bufio.NewScanner(os.Stdin)
	sc.Buffer(make([]byte, 1024*1024), 1024*1024)
	var buf string
	depth := 0
	fmt.Print("> ")
	for sc.Scan() {
		line := sc.Text()
		t := strings.TrimSpace(line)
		if buf == "" {
			if t == ":quit" || t == ":exit" {
				return nil
			}
			if t == ":help" {
				fmt.Println(":quit exit, :reset clear, blank line runs")
				fmt.Print("> ")
				continue
			}
			if t == ":reset" {
				in = backend.New()
				fmt.Println("reset ok")
				fmt.Print("> ")
				continue
			}
		}
		if buf == "" && t == "" {
			fmt.Print("> ")
			continue
		}
		buf += line + "\n"
		depth += strings.Count(line, "{") - strings.Count(line, "}")
		// run when balanced and (blank line or depth<=0)
		if depth <= 0 {
			prog, err := frontend.ParseSource(buf, "<repl>")
			if err != nil {
				fmt.Println("error:", err)
			} else if err := in.ExecProgram(prog); err != nil {
				fmt.Println("error:", err)
			}
			buf = ""
			depth = 0
		}
		fmt.Print("> ")
	}
	return sc.Err()
}

// ---------------------------------------------------------------------------
// bench
// ---------------------------------------------------------------------------

func Bench(target string, n int) error {
	if n <= 0 {
		n = 20
	}
	files, err := collectKsFiles(target)
	if err != nil {
		// single file passed directly?
		return err
	}
	if len(files) == 0 {
		return fmt.Errorf("no .ks files under %s", target)
	}
	// if target is a file, files has 1 entry
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			return err
		}
		prog, err := frontend.ParseSource(string(data), f)
		if err != nil {
			return fmt.Errorf("bench %s: %w", f, err)
		}
		dir := filepath.Dir(f)
		start := time.Now()
		var lastErr error
		for i := 0; i < n; i++ {
			lastErr = backend.RunWithDir(prog, dir)
			if lastErr != nil {
				break
			}
		}
		el := time.Since(start)
		if lastErr != nil {
			fmt.Printf("bench %s: FAIL (%v)\n", f, lastErr)
			continue
		}
		avg := el / time.Duration(n)
		fmt.Printf("bench %s: %d runs in %s (avg %s/op)\n", f, n, el.Round(time.Millisecond), avg)
	}
	return nil
}
