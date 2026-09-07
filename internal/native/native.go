// Package native transpiles a strict, statically-typed subset of .ks to Go
// source, which `go build` turns into real machine code (native-0.1).
//
// Subset v0.1: int/float/string/bool scalars, let/assign (+= -= *= /= %=),
// print, sleep, if/else, while, for-in over range(n[, b[, step]]), for-c,
// named funcs + closures (no captures), return/break/continue, len(string).
// Everything else (go/chan/select/import/defer/try/switch/maps/arrays,
// nil, ?./??, is/in, dynamic typing) is rejected with a clear `line N:`
// error telling the user the file runs in the interpreter instead.
//
// Semantics mirror the interpreter: `/` always yields float, `%` needs
// ints, division by zero panics with "division by zero", int**int with a
// non-negative literal exponent uses exponentiation by squaring, print
// joins Display forms with spaces.
package native

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/kswarrior/ks-fusion/internal/frontend"
)

// Version is the native backend subset version.
const Version = "0.1"

// ntype is a native static type.
type ntype int

const (
	ntInt ntype = iota
	ntFloat
	ntString
	ntBool
	ntVoid
)

func (t ntype) String() string {
	switch t {
	case ntInt:
		return "int"
	case ntFloat:
		return "float"
	case ntString:
		return "string"
	case ntBool:
		return "bool"
	default:
		return "void"
	}
}

func (t ntype) goType() string {
	switch t {
	case ntInt:
		return "int64"
	case ntFloat:
		return "float64"
	case ntString:
		return "string"
	case ntBool:
		return "bool"
	default:
		return ""
	}
}

// parseAnn maps a .ks `: type` annotation to a native type.
// Only plain int/float/string/bool are supported (no `?`, unions,
// generics, nominals — nil is not representable for scalars).
func parseAnn(ann string, line int) (ntype, error) {
	switch ann {
	case "int":
		return ntInt, nil
	case "float":
		return ntFloat, nil
	case "string":
		return ntString, nil
	case "bool":
		return ntBool, nil
	case "":
		return ntVoid, fmt.Errorf("line %d: native needs a `: type` here (int|float|string|bool)", line)
	default:
		return ntVoid, fmt.Errorf("line %d: type %q is not in the native subset (want int|float|string|bool) — runs in interpreter", line, ann)
	}
}

// goKeywords rejects .ks identifiers that would not compile as Go names.
var goKeywords = map[string]bool{
	"break": true, "case": true, "chan": true, "const": true,
	"continue": true, "default": true, "defer": true, "else": true,
	"fallthrough": true, "for": true, "func": true, "go": true,
	"goto": true, "if": true, "import": true, "interface": true,
	"map": true, "package": true, "range": true, "return": true,
	"select": true, "struct": true, "switch": true, "type": true,
	"var": true, "main": true, "init": true, "nil": true,
	"true": true, "false": true, "iota": true,
}

func checkName(name string, what string, line int) error {
	if name == "" {
		return fmt.Errorf("line %d: empty %s name", line, what)
	}
	if goKeywords[name] {
		return fmt.Errorf("line %d: %s %q collides with a Go keyword — rename it (runs in interpreter)", line, what, name)
	}
	return nil
}

type funcSig struct {
	params []ntype
	ret    ntype
}

type gen struct {
	sb         strings.Builder
	funcs      map[string]*funcSig
	closures   []map[string]*funcSig // closure sigs, scoped parallel to vars
	vars       []map[string]ntype    // scope stack
	loop       int
	funcRet    ntype // current function return type (ntVoid outside funcs)
	inFunc     bool
	pending    map[string]bool // funcs whose return type is not final yet
	inferring  string          // func whose return type is being inferred
	useMath    bool
	useTime    bool
	useUTF8    bool
	useStrconv bool
	tmp        int
}

func (g *gen) lookup(name string) (ntype, bool) {
	for i := len(g.vars) - 1; i >= 0; i-- {
		if t, ok := g.vars[i][name]; ok {
			return t, true
		}
	}
	return ntVoid, false
}

func (g *gen) define(name string, t ntype) {
	g.vars[len(g.vars)-1][name] = t
}

func (g *gen) pushScope() {
	g.vars = append(g.vars, map[string]ntype{})
	g.closures = append(g.closures, map[string]*funcSig{})
}
func (g *gen) popScope() {
	g.vars = g.vars[:len(g.vars)-1]
	g.closures = g.closures[:len(g.closures)-1]
}

// lookupClosure finds a closure/func-literal signature, innermost first.
func (g *gen) lookupClosure(name string) (*funcSig, bool) {
	for i := len(g.closures) - 1; i >= 0; i-- {
		if s, ok := g.closures[i][name]; ok {
			return s, true
		}
	}
	return nil, false
}

func (g *gen) defineClosure(name string, sig *funcSig) {
	g.closures[len(g.closures)-1][name] = sig
}

func (g *gen) tmpName() string {
	g.tmp++
	return fmt.Sprintf("ksT%d", g.tmp)
}

// TranspileToGo parses path and emits equivalent Go source for the native
// subset, or a `line N:` error when the file needs the interpreter.
func TranspileToGo(path string) (string, error) {
	prog, err := frontend.ParseFile(path)
	if err != nil {
		return "", err
	}
	return TranspileProgram(prog)
}

// TranspileSource parses src and emits equivalent Go source.
func TranspileSource(src, path string) (string, error) {
	prog, err := frontend.ParseSource(src, path)
	if err != nil {
		return "", err
	}
	return TranspileProgram(prog)
}

// TranspileProgram emits Go source for an already-parsed program.
func TranspileProgram(prog *frontend.Program) (string, error) {
	g := &gen{funcs: map[string]*funcSig{}, pending: map[string]bool{}}
	// Phase 1: register top-level func signatures (annotations required).
	for _, st := range prog.Statements {
		if st.Kind != frontend.StmtFunc {
			continue
		}
		if err := checkName(st.Name, "func", st.Line); err != nil {
			return "", err
		}
		if _, dup := g.funcs[st.Name]; dup {
			return "", fmt.Errorf("line %d: duplicate func %q", st.Line, st.Name)
		}
		var params []ntype
		for i, p := range st.Names {
			if err := checkName(p, "param", st.Line); err != nil {
				return "", err
			}
			pt := ""
			if i < len(st.ParamTypes) {
				pt = st.ParamTypes[i]
			}
			t, err := parseAnn(pt, st.Line)
			if err != nil {
				return "", fmt.Errorf("line %d: func %q param %q %v — runs in interpreter", st.Line, st.Name, p, err)
			}
			params = append(params, t)
		}
		g.funcs[st.Name] = &funcSig{params: params}
		g.pending[st.Name] = true
	}
	// Phase 1b: annotated return types are final before any body is read,
	// so annotated (even recursive) calls check during every later walk.
	for _, st := range prog.Statements {
		if st.Kind != frontend.StmtFunc || st.ReturnType == "" {
			continue
		}
		want, err := parseAnn(st.ReturnType, st.Line)
		if err != nil {
			return "", err
		}
		g.funcs[st.Name].ret = want
		delete(g.pending, st.Name)
	}
	// Phase 2: determine return types (annotations verified, else inferred).
	for _, st := range prog.Statements {
		if st.Kind != frontend.StmtFunc {
			continue
		}
		ret, err := g.inferFuncRet(st)
		if err != nil {
			return "", err
		}
		g.funcs[st.Name].ret = ret
	}
	// Phase 3: emit.
	var body strings.Builder
	old := g.sb
	g.sb = body
	g.pushScope()
	for _, st := range prog.Statements {
		if st.Kind == frontend.StmtFunc {
			continue
		}
		if err := g.emitTop(st); err != nil {
			return "", err
		}
	}
	g.popScope()
	bodyStr := g.sb.String()
	g.sb = old

	var funcs strings.Builder
	for _, st := range prog.Statements {
		if st.Kind != frontend.StmtFunc {
			continue
		}
		if err := g.emitFunc(st, &funcs, true); err != nil {
			return "", err
		}
	}

	var out strings.Builder
	out.WriteString("// Code generated by fusion native (native-" + Version + "); DO NOT EDIT.\n")
	out.WriteString("package main\n\nimport (\n\t\"fmt\"\n")
	if g.useMath {
		out.WriteString("\t\"math\"\n")
	}
	if g.useStrconv {
		out.WriteString("\t\"strconv\"\n")
	}
	if g.useTime {
		out.WriteString("\t\"time\"\n")
	}
	if g.useUTF8 {
		out.WriteString("\t\"unicode/utf8\"\n")
	}
	out.WriteString(")\n\n")
	out.WriteString("func ksDiv(a, b float64) float64 {\n\tif b == 0 {\n\t\tpanic(\"division by zero\")\n\t}\n\treturn a / b\n}\n\n")
	out.WriteString("func ksMod(a, b int64) int64 {\n\tif b == 0 {\n\t\tpanic(\"division by zero\")\n\t}\n\treturn a % b\n}\n\n")
	out.WriteString("func ksPowI(a, b int64) int64 {\n\tres := int64(1)\n\tx := a\n\te := b\n\tfor e > 0 {\n\t\tif e&1 == 1 {\n\t\t\tres *= x\n\t\t}\n\t\te >>= 1\n\t\tif e > 0 {\n\t\t\tx *= x\n\t\t}\n\t}\n\treturn res\n}\n\n")
	out.WriteString(funcs.String())
	out.WriteString("func main() {\n")
	out.WriteString(bodyStr)
	out.WriteString("}\n")
	return out.String(), nil
}

// inferFuncRet determines a top-level func's return type: the declared
// annotation when present (verified against the body), otherwise unified
// from the body. Unannotated recursion cannot be inferred and is rejected.
func (g *gen) inferFuncRet(st *frontend.Stmt) (ntype, error) {
	sig := g.funcs[st.Name]
	if st.ReturnType != "" {
		want, err := parseAnn(st.ReturnType, st.Line)
		if err != nil {
			return ntVoid, err
		}
		sig.ret = want // registered before walking, so recursion checks
		if err := g.verifyReturns(st, st.Body, want); err != nil {
			return ntVoid, err
		}
		return want, nil
	}
	if callsSelf(st.Body, st.Name) {
		return ntVoid, fmt.Errorf("line %d: recursive func %q needs a `: type` return annotation in native subset", st.Line, st.Name)
	}
	// Seed scope with params so bodies type-check.
	saved := g.vars
	g.vars = []map[string]ntype{{}}
	for i, p := range st.Names {
		g.vars[0][p] = sig.params[i]
	}
	types, err := g.collectReturns(st.Body)
	g.vars = saved
	if err != nil {
		return ntVoid, err
	}
	if len(types) == 0 {
		return ntVoid, nil
	}
	ret := types[0]
	for _, t := range types[1:] {
		var err error
		ret, err = unify(ret, t, st.Line)
		if err != nil {
			return ntVoid, fmt.Errorf("line %d: func %q %v", st.Line, st.Name, err)
		}
	}
	return ret, nil
}

// verifyReturns checks every return in body against the declared type.
func (g *gen) verifyReturns(st *frontend.Stmt, body *frontend.Stmt, want ntype) error {
	saved := g.vars
	g.vars = []map[string]ntype{{}}
	sig := g.funcs[st.Name]
	if sig == nil {
		sig, _ = g.lookupClosure(st.Name)
	}
	if sig != nil {
		for i, p := range st.Names {
			if i < len(sig.params) {
				g.vars[0][p] = sig.params[i]
			}
		}
	}
	types, err := g.collectReturns(body)
	g.vars = saved
	if err != nil {
		return err
	}
	for _, t := range types {
		if want == ntVoid {
			if t != ntVoid {
				return fmt.Errorf("line %d: value return in void func %q", st.Line, st.Name)
			}
			continue
		}
		if t == ntVoid || !assignable(want, t) {
			return fmt.Errorf("line %d: return %s in %s func %q", st.Line, t, want, st.Name)
		}
	}
	return nil
}

// callsSelf reports whether a body calls name (used to reject unannotated
// recursion, whose return type cannot be inferred in one pass).
func callsSelf(st *frontend.Stmt, name string) bool {
	found := false
	var walkStmt func(s *frontend.Stmt)
	var walkExpr func(e *frontend.Expr)
	walkExpr = func(e *frontend.Expr) {
		if e == nil || found {
			return
		}
		if e.Kind == frontend.ExprCall {
			if c := e.Callee; c != nil && c.Kind == frontend.ExprVar && c.Name == name {
				found = true
				return
			}
		}
		walkExpr(e.Left)
		walkExpr(e.Right)
		walkExpr(e.Callee)
		walkExpr(e.SliceStart)
		walkExpr(e.SliceEnd)
		for _, a := range e.Args {
			walkExpr(a)
		}
		for _, el := range e.Elements {
			walkExpr(el)
		}
		for _, v := range e.MapVals {
			walkExpr(v)
		}
		if e.FuncBody != nil {
			walkStmt(e.FuncBody)
		}
	}
	walkStmt = func(s *frontend.Stmt) {
		if s == nil || found {
			return
		}
		walkExpr(s.Expr)
		for _, e := range s.Exprs {
			walkExpr(e)
		}
		if s.Inner != nil {
			walkStmt(s.Inner)
		}
		for _, c := range children(s) {
			walkStmt(c)
		}
	}
	walkStmt(st)
	return found
}

// unify merges two types (int+float promotes to float).
func unify(a, b ntype, line int) (ntype, error) {
	if a == b {
		return a, nil
	}
	if (a == ntInt && b == ntFloat) || (a == ntFloat && b == ntInt) {
		return ntFloat, nil
	}
	return ntVoid, fmt.Errorf("conflicting types %s vs %s in native subset — runs in interpreter", a, b)
}

func (g *gen) collectReturns(st *frontend.Stmt) ([]ntype, error) {
	var out []ntype
	var walk func(s *frontend.Stmt) error
	walk = func(s *frontend.Stmt) error {
		if s == nil {
			return nil
		}
		switch s.Kind {
		case frontend.StmtReturn:
			if s.Expr == nil {
				out = append(out, ntVoid)
				return nil
			}
			t, err := g.typeOf(s.Expr)
			if err != nil {
				return err
			}
			out = append(out, t)
			return nil
		case frontend.StmtFunc:
			// Nested func: its returns belong to the inner func, which is
			// checked separately at emission. Skip, do not descend.
			return nil
		case frontend.StmtGo:
			return fmt.Errorf("line %d: not in the native subset — runs in interpreter", s.Line)
		case frontend.StmtImport, frontend.StmtTry, frontend.StmtSwitch,
			frontend.StmtSelect, frontend.StmtDefer, frontend.StmtStruct, frontend.StmtEnum:
			return fmt.Errorf("line %d: not in the native subset — runs in interpreter", s.Line)
		}
		for _, c := range children(s) {
			if err := walk(c); err != nil {
				return err
			}
		}
		return nil
	}
	if err := walk(st); err != nil {
		return nil, err
	}
	return out, nil
}

// children returns nested statements for traversal (not expressions).
func children(s *frontend.Stmt) []*frontend.Stmt {
	var out []*frontend.Stmt
	out = append(out, s.Body, s.Then, s.Else, s.Init, s.Post, s.Inner, s.CaBody, s.FinBody)
	out = append(out, s.List...)
	for _, c := range s.Cases {
		out = append(out, c.Body)
	}
	for _, c := range s.SelectCases {
		out = append(out, c.Body)
	}
	var keep []*frontend.Stmt
	for _, c := range out {
		if c != nil {
			keep = append(keep, c)
		}
	}
	return keep
}

// typeOf infers the static type of an expression, rejecting anything
// outside the native subset.
func (g *gen) typeOf(e *frontend.Expr) (ntype, error) {
	if e == nil {
		return ntVoid, fmt.Errorf("missing expression in native subset")
	}
	switch e.Kind {
	case frontend.ExprInt:
		return ntInt, nil
	case frontend.ExprFloat:
		return ntFloat, nil
	case frontend.ExprString:
		return ntString, nil
	case frontend.ExprBool:
		return ntBool, nil
	case frontend.ExprNil:
		return ntVoid, fmt.Errorf("nil is not in the native subset — runs in interpreter")
	case frontend.ExprVar:
		if _, isFunc := g.lookupClosure(e.Name); isFunc {
			return ntVoid, fmt.Errorf("%q is a func — call it", e.Name)
		}
		t, ok := g.lookup(e.Name)
		if !ok {
			return ntVoid, fmt.Errorf("unknown variable %q (native needs declaration before use)", e.Name)
		}
		return t, nil
	case frontend.ExprAdd, frontend.ExprSub, frontend.ExprMul, frontend.ExprDiv:
		return g.arithType(e)
	case frontend.ExprMod:
		lt, err := g.typeOf(e.Left)
		if err != nil {
			return ntVoid, err
		}
		rt, err := g.typeOf(e.Right)
		if err != nil {
			return ntVoid, err
		}
		if lt != ntInt || rt != ntInt {
			return ntVoid, fmt.Errorf("%% needs ints in native subset — runs in interpreter")
		}
		return ntInt, nil
	case frontend.ExprPow:
		lt, err := g.typeOf(e.Left)
		if err != nil {
			return ntVoid, err
		}
		rt, err := g.typeOf(e.Right)
		if err != nil {
			return ntVoid, err
		}
		if !isNum(lt) || !isNum(rt) {
			return ntVoid, fmt.Errorf("** needs numbers in native subset — runs in interpreter")
		}
		if lt == ntFloat || rt == ntFloat {
			return ntFloat, nil
		}
		if e.Right.Kind != frontend.ExprInt || e.Right.IntVal < 0 {
			return ntVoid, fmt.Errorf("int ** needs a non-negative int literal exponent in native-0.1 — runs in interpreter")
		}
		return ntInt, nil
	case frontend.ExprNeg:
		t, err := g.typeOf(e.Right)
		if err != nil {
			return ntVoid, err
		}
		if !isNum(t) {
			return ntVoid, fmt.Errorf("unary - needs a number in native subset — runs in interpreter")
		}
		return t, nil
	case frontend.ExprNot:
		t, err := g.typeOf(e.Right)
		if err != nil {
			return ntVoid, err
		}
		if t != ntBool {
			return ntVoid, fmt.Errorf("!/not needs bool in native subset (no truthiness) — runs in interpreter")
		}
		return ntBool, nil
	case frontend.ExprAnd, frontend.ExprOr:
		lt, err := g.typeOf(e.Left)
		if err != nil {
			return ntVoid, err
		}
		rt, err := g.typeOf(e.Right)
		if err != nil {
			return ntVoid, err
		}
		if lt != ntBool || rt != ntBool {
			return ntVoid, fmt.Errorf("and/or need bool operands in native subset (they return operands in .ks) — runs in interpreter")
		}
		return ntBool, nil
	case frontend.ExprEq, frontend.ExprNe:
		lt, err := g.typeOf(e.Left)
		if err != nil {
			return ntVoid, err
		}
		rt, err := g.typeOf(e.Right)
		if err != nil {
			return ntVoid, err
		}
		if isNum(lt) && isNum(rt) {
			return ntBool, nil
		}
		if lt == rt && (lt == ntString || lt == ntBool) {
			return ntBool, nil
		}
		return ntVoid, fmt.Errorf("== needs two numbers or two strings/bools in native subset — runs in interpreter")
	case frontend.ExprLt, frontend.ExprLe, frontend.ExprGt, frontend.ExprGe:
		lt, err := g.typeOf(e.Left)
		if err != nil {
			return ntVoid, err
		}
		rt, err := g.typeOf(e.Right)
		if err != nil {
			return ntVoid, err
		}
		if !isNum(lt) || !isNum(rt) {
			return ntVoid, fmt.Errorf("comparisons need numbers in native-0.1 — runs in interpreter")
		}
		return ntBool, nil
	case frontend.ExprCall:
		return g.callType(e)
	case frontend.ExprFunc:
		return ntVoid, fmt.Errorf("func literal needs a `let name = func...` binding in native subset — runs in interpreter")
	case frontend.ExprIn, frontend.ExprIs, frontend.ExprCoalesce,
		frontend.ExprIndex, frontend.ExprSlice, frontend.ExprArray, frontend.ExprMap:
		return ntVoid, fmt.Errorf("not in the native-0.1 subset — runs in interpreter")
	}
	return ntVoid, fmt.Errorf("not in the native-0.1 subset — runs in interpreter")
}

func isNum(t ntype) bool { return t == ntInt || t == ntFloat }

// arithType types + - * / (string concat only for +).
func (g *gen) arithType(e *frontend.Expr) (ntype, error) {
	lt, err := g.typeOf(e.Left)
	if err != nil {
		return ntVoid, err
	}
	rt, err := g.typeOf(e.Right)
	if err != nil {
		return ntVoid, err
	}
	if e.Kind == frontend.ExprAdd && lt == ntString && rt == ntString {
		return ntString, nil
	}
	if e.Kind == frontend.ExprDiv {
		if !isNum(lt) || !isNum(rt) {
			return ntVoid, fmt.Errorf("/ needs numbers in native subset — runs in interpreter")
		}
		return ntFloat, nil // `/` always yields float, like the interpreter
	}
	if !isNum(lt) || !isNum(rt) {
		return ntVoid, fmt.Errorf("arithmetic needs numbers in native subset — runs in interpreter")
	}
	if lt == ntFloat || rt == ntFloat {
		return ntFloat, nil
	}
	return ntInt, nil
}

// callType types user-func calls and len(string).
func (g *gen) callType(e *frontend.Expr) (ntype, error) {
	name := e.Callee
	if name == nil || name.Kind != frontend.ExprVar {
		return ntVoid, fmt.Errorf("method calls are not in the native-0.1 subset — runs in interpreter")
	}
	if name.Name == "len" {
		if len(e.Args) != 1 {
			return ntVoid, fmt.Errorf("len wants 1 arg in native subset")
		}
		at, err := g.typeOf(e.Args[0])
		if err != nil {
			return ntVoid, err
		}
		if at != ntString {
			return ntVoid, fmt.Errorf("len needs a string in native-0.1 (no arrays yet) — runs in interpreter")
		}
		return ntInt, nil
	}
	sig, ok := g.funcs[name.Name]
	if !ok {
		sig, ok = g.lookupClosure(name.Name)
	}
	if !ok {
		return ntVoid, fmt.Errorf("unknown func %q in native subset (no builtins besides len) — runs in interpreter", name.Name)
	}
	if len(e.Args) != len(sig.params) {
		return ntVoid, fmt.Errorf("func %q wants %d args, got %d", name.Name, len(sig.params), len(e.Args))
	}
	for i, a := range e.Args {
		at, err := g.typeOf(a)
		if err != nil {
			return ntVoid, err
		}
		if !assignable(sig.params[i], at) {
			return ntVoid, fmt.Errorf("func %q arg %d: %s is not %s", name.Name, i+1, at, sig.params[i])
		}
	}
	return sig.ret, nil
}

// assignable reports whether a value of type got fits a slot of type want.
// Native-0.1 is strict: types must match exactly (the interpreter also
// rejects `let a: float = 7`). int+float mixing happens only inside
// arithmetic operators, which promote to float like the interpreter.
func assignable(want, got ntype) bool {
	return want == got
}

// ---------------------------------------------------------------------------
// emission
// ---------------------------------------------------------------------------

func (g *gen) emit(format string, args ...any) {
	fmt.Fprintf(&g.sb, format, args...)
}

// emitTop emits a top-level (main body) statement.
func (g *gen) emitTop(st *frontend.Stmt) error {
	switch st.Kind {
	case frontend.StmtReturn:
		return fmt.Errorf("line %d: return outside function", st.Line)
	case frontend.StmtBreak, frontend.StmtContinue:
		return fmt.Errorf("line %d: break/continue outside loop", st.Line)
	}
	return g.emitStmt(st)
}

func (g *gen) emitFunc(st *frontend.Stmt, out *strings.Builder, top bool) error {
	sig := g.funcs[st.Name]
	saved := g.sb
	g.sb = *out
	g.pushScope()
	for i, p := range st.Names {
		g.define(p, sig.params[i])
	}
	g.funcRet = sig.ret
	g.inFunc = true
	var params []string
	for i, p := range st.Names {
		params = append(params, p+" "+sig.params[i].goType())
	}
	if sig.ret == ntVoid {
		g.emit("func %s(%s) {\n", st.Name, strings.Join(params, ", "))
	} else {
		g.emit("func %s(%s) %s {\n", st.Name, strings.Join(params, ", "), sig.ret.goType())
	}
	if err := g.emitBlockBody(st.Body); err != nil {
		return err
	}
	g.emit("}\n\n")
	g.popScope()
	g.inFunc = false
	*out = g.sb
	g.sb = saved
	return nil
}

// emitBlockBody emits the statements of a block (already scoped by caller
// except function bodies, which push their own scope in emitFunc).
func (g *gen) emitBlockBody(b *frontend.Stmt) error {
	if b == nil {
		return nil
	}
	if b.Kind != frontend.StmtBlock {
		return fmt.Errorf("line %d: expected block", b.Line)
	}
	g.pushScope()
	for _, s := range b.List {
		if err := g.emitStmt(s); err != nil {
			return err
		}
	}
	g.popScope()
	return nil
}

func (g *gen) emitStmt(st *frontend.Stmt) error {
	switch st.Kind {
	case frontend.StmtLet:
		return g.emitLet(st)
	case frontend.StmtAssign:
		return g.emitAssign(st, false)
	case frontend.StmtPrint:
		args := st.Exprs
		if len(args) == 0 && st.Expr != nil {
			args = []*frontend.Expr{st.Expr}
		}
		var parts []string
		for _, a := range args {
			t, err := g.typeOf(a)
			if err != nil {
				return err
			}
			if t == ntVoid {
				return fmt.Errorf("line %d: print of void value", st.Line)
			}
			src, err := g.emitExpr(a, t)
			if err != nil {
				return err
			}
			parts = append(parts, g.printConv(t, src))
		}
		g.emit("fmt.Println(%s)\n", strings.Join(parts, ", "))
		return nil
	case frontend.StmtSleep:
		t, err := g.typeOf(st.Expr)
		if err != nil {
			return err
		}
		if t != ntInt {
			return fmt.Errorf("line %d: sleep needs int ms in native subset", st.Line)
		}
		src, err := g.emitExpr(st.Expr, ntInt)
		if err != nil {
			return err
		}
		g.useTime = true
		g.emit("time.Sleep(time.Duration(%s) * time.Millisecond)\n", src)
		return nil
	case frontend.StmtIf:
		t, err := g.typeOf(st.Expr)
		if err != nil {
			return err
		}
		if t != ntBool {
			return fmt.Errorf("line %d: if needs bool (no truthiness in native) — runs in interpreter", st.Line)
		}
		cond, err := g.emitExpr(st.Expr, ntBool)
		if err != nil {
			return err
		}
		g.emit("if %s {\n", cond)
		if err := g.emitBlockBody(st.Then); err != nil {
			return err
		}
		if st.Else != nil {
			if st.Else.Kind == frontend.StmtIf {
				g.emit("} else ")
				// else-if: emit without extra braces
				saved := g.sb
				var tmp strings.Builder
				g.sb = tmp
				err := g.emitStmt(st.Else)
				tmpStr := g.sb.String()
				g.sb = saved
				if err != nil {
					return err
				}
				g.emit("%s", tmpStr)
			} else {
				g.emit("} else {\n")
				if err := g.emitBlockBody(st.Else); err != nil {
					return err
				}
				g.emit("}\n")
				return nil
			}
		} else {
			g.emit("}\n")
		}
		return nil
	case frontend.StmtWhile:
		t, err := g.typeOf(st.Expr)
		if err != nil {
			return err
		}
		if t != ntBool {
			return fmt.Errorf("line %d: while needs bool (no truthiness in native) — runs in interpreter", st.Line)
		}
		cond, err := g.emitExpr(st.Expr, ntBool)
		if err != nil {
			return err
		}
		g.emit("for %s {\n", cond)
		g.loop++
		err = g.emitBlockBody(st.Body)
		g.loop--
		if err != nil {
			return err
		}
		g.emit("}\n")
		return nil
	case frontend.StmtForIn:
		return g.emitForIn(st)
	case frontend.StmtForC:
		return g.emitForC(st)
	case frontend.StmtFunc:
		// Nested named func: emit as a closure binding.
		if err := checkName(st.Name, "func", st.Line); err != nil {
			return err
		}
		return g.emitNestedFunc(st)
	case frontend.StmtReturn:
		if !g.inFunc {
			return fmt.Errorf("line %d: return outside function", st.Line)
		}
		if st.Expr == nil {
			if g.funcRet != ntVoid {
				return fmt.Errorf("line %d: bare return in non-void func", st.Line)
			}
			g.emit("return\n")
			return nil
		}
		t, err := g.typeOf(st.Expr)
		if err != nil {
			return err
		}
		if g.funcRet == ntVoid {
			return fmt.Errorf("line %d: value return in void func", st.Line)
		}
		if !assignable(g.funcRet, t) {
			return fmt.Errorf("line %d: return %s in %s func", st.Line, t, g.funcRet)
		}
		src, err := g.emitExpr(st.Expr, g.funcRet)
		if err != nil {
			return err
		}
		g.emit("return %s\n", src)
		return nil
	case frontend.StmtBreak:
		if g.loop == 0 {
			return fmt.Errorf("line %d: break outside loop", st.Line)
		}
		g.emit("break\n")
		return nil
	case frontend.StmtContinue:
		if g.loop == 0 {
			return fmt.Errorf("line %d: continue outside loop", st.Line)
		}
		g.emit("continue\n")
		return nil
	case frontend.StmtBlock:
		g.emit("{\n")
		if err := g.emitBlockBody(st); err != nil {
			return err
		}
		g.emit("}\n")
		return nil
	case frontend.StmtExpr:
		t, err := g.typeOf(st.Expr)
		if err != nil {
			return err
		}
		src, err := g.emitExpr(st.Expr, t)
		if err != nil {
			return err
		}
		g.emit("_ = %s\n", src)
		return nil
	default:
		return fmt.Errorf("line %d: not in the native-0.1 subset — runs in interpreter", st.Line)
	}
}

// closureSig infers the signature of a nested func statement or func
// literal and registers it in the current scope (so recursion, the body,
// and later calls in the same scope type-check). Popping the scope drops
// it again, like a variable.
func (g *gen) closureSig(name string, params []string, ptypes []string, retAnn string, body *frontend.Stmt, line int, fn func(sig *funcSig) error) error {
	var ps []ntype
	for i, p := range params {
		if err := checkName(p, "param", line); err != nil {
			return err
		}
		pt := ""
		if i < len(ptypes) {
			pt = ptypes[i]
		}
		t, err := parseAnn(pt, line)
		if err != nil {
			return err
		}
		ps = append(ps, t)
	}
	sig := &funcSig{params: ps, ret: ntVoid}
	g.defineClosure(name, sig)
	if retAnn != "" {
		want, err := parseAnn(retAnn, line)
		if err != nil {
			return err
		}
		sig.ret = want // known upfront, so recursion checks during verify
		fake := &frontend.Stmt{Kind: frontend.StmtFunc, Name: name, Names: params, ParamTypes: ptypes, Body: body, Line: line}
		if err := g.verifyReturns(fake, body, want); err != nil {
			return err
		}
	} else {
		if callsSelf(body, name) {
			return fmt.Errorf("line %d: recursive func needs a `: type` return annotation in native subset", line)
		}
		saved := g.vars
		g.vars = []map[string]ntype{{}}
		for i, p := range params {
			g.vars[0][p] = ps[i]
		}
		types, err := g.collectReturns(body)
		g.vars = saved
		if err != nil {
			return err
		}
		if len(types) > 0 {
			ret := types[0]
			for _, t := range types[1:] {
				var err error
				ret, err = unify(ret, t, line)
				if err != nil {
					return err
				}
			}
			sig.ret = ret
		}
	}
	return fn(sig)
}

// emitNestedFunc emits `name := func(params) [ret] { body }`.
func (g *gen) emitNestedFunc(st *frontend.Stmt) error {
	if _, dup := g.closures[len(g.closures)-1][st.Name]; dup {
		return fmt.Errorf("line %d: %q already defined in this block", st.Line, st.Name)
	}
	if _, dup := g.vars[len(g.vars)-1][st.Name]; dup {
		return fmt.Errorf("line %d: %q already defined in this block", st.Line, st.Name)
	}
	return g.closureSig(st.Name, st.Names, st.ParamTypes, st.ReturnType, st.Body, st.Line, func(sig *funcSig) error {
		g.pushScope()
		for i, p := range st.Names {
			g.define(p, sig.params[i])
		}
		savedRet, savedIn := g.funcRet, g.inFunc
		g.funcRet, g.inFunc = sig.ret, true
		var params []string
		for i, p := range st.Names {
			params = append(params, p+" "+sig.params[i].goType())
		}
		if sig.ret == ntVoid {
			g.emit("%s := func(%s) {\n", st.Name, strings.Join(params, ", "))
		} else {
			g.emit("%s := func(%s) %s {\n", st.Name, strings.Join(params, ", "), sig.ret.goType())
		}
		err := g.emitBlockBody(st.Body)
		g.emit("}\n")
		g.funcRet, g.inFunc = savedRet, savedIn
		g.popScope()
		return err
	})
}

func (g *gen) emitLet(st *frontend.Stmt) error {
	if err := checkName(st.Name, "var", st.Line); err != nil {
		return err
	}
	// `let f = func...`: closure binding.
	if st.Expr != nil && st.Expr.Kind == frontend.ExprFunc {
		return g.emitLetFunc(st)
	}
	t, err := g.typeOf(st.Expr)
	if err != nil {
		return err
	}
	if t == ntVoid {
		return fmt.Errorf("line %d: cannot bind void value", st.Line)
	}
	if st.TypeAnn != "" {
		want, err := parseAnn(st.TypeAnn, st.Line)
		if err != nil {
			return err
		}
		if !assignable(want, t) {
			return fmt.Errorf("line %d: %s is not %s", st.Line, t, want)
		}
		t = want
	}
	if _, exists := g.vars[len(g.vars)-1][st.Name]; exists {
		return fmt.Errorf("line %d: %q already defined in this block (use = to assign)", st.Line, st.Name)
	}
	src, err := g.emitExpr(st.Expr, t)
	if err != nil {
		return err
	}
	g.define(st.Name, t)
	g.emit("var %s %s = %s\n", st.Name, t.goType(), src)
	return nil
}

// emitLetFunc emits `let f = func(params) [ret] { body }` via a
// pre-declared var so the closure can recurse.
func (g *gen) emitLetFunc(st *frontend.Stmt) error {
	e := st.Expr
	if _, exists := g.vars[len(g.vars)-1][st.Name]; exists {
		return fmt.Errorf("line %d: %q already defined in this block", st.Line, st.Name)
	}
	if _, exists := g.closures[len(g.closures)-1][st.Name]; exists {
		return fmt.Errorf("line %d: %q already defined in this block", st.Line, st.Name)
	}
	return g.closureSig(st.Name, e.FuncParams, e.FuncParamTypes, e.FuncReturnType, e.FuncBody, st.Line, func(sig *funcSig) error {
		g.pushScope()
		for i, p := range e.FuncParams {
			g.define(p, sig.params[i])
		}
		savedRet, savedIn := g.funcRet, g.inFunc
		g.funcRet, g.inFunc = sig.ret, true
		var ps []string
		for i, p := range e.FuncParams {
			ps = append(ps, p+" "+sig.params[i].goType())
		}
		if sig.ret == ntVoid {
			g.emit("var %s func(%s)\n", st.Name, strings.Join(ps, ", "))
			g.emit("%s = func(%s) {\n", st.Name, strings.Join(ps, ", "))
		} else {
			g.emit("var %s func(%s) %s\n", st.Name, strings.Join(ps, ", "), sig.ret.goType())
			g.emit("%s = func(%s) %s {\n", st.Name, strings.Join(ps, ", "), sig.ret.goType())
		}
		err := g.emitBlockBody(e.FuncBody)
		g.emit("}\n")
		g.funcRet, g.inFunc = savedRet, savedIn
		g.popScope()
		return err
	})
}

func (g *gen) emitAssign(st *frontend.Stmt, implicit bool) error {
	op := st.Op
	if op == "" {
		op = "="
	}
	t, ok := g.lookup(st.Name)
	if !ok {
		// for-c `for i = 0; ...` implicitly defines the loop var.
		if !implicit || op != "=" {
			return fmt.Errorf("line %d: assign to unknown %q (native needs let first)", st.Line, st.Name)
		}
		et, err := g.typeOf(st.Expr)
		if err != nil {
			return err
		}
		if et == ntVoid {
			return fmt.Errorf("line %d: cannot bind void value", st.Line)
		}
		src, err := g.emitExpr(st.Expr, et)
		if err != nil {
			return err
		}
		g.define(st.Name, et)
		g.emit("var %s %s = %s\n", st.Name, et.goType(), src)
		return nil
	}
	et, err := g.typeOf(st.Expr)
	if err != nil {
		return err
	}
	if op == "=" {
		if !assignable(t, et) {
			return fmt.Errorf("line %d: %s is not %s (native vars keep their type)", st.Line, et, t)
		}
		src, err := g.emitExpr(st.Expr, t)
		if err != nil {
			return err
		}
		g.emit("%s = %s\n", st.Name, src)
		return nil
	}
	// Compound ops: .ks `x += v` ≡ `x = x + v` (float division included).
	if t != ntInt && t != ntFloat && !(t == ntString && op == "+=") {
		return fmt.Errorf("line %d: %s not supported for %s in native subset", st.Line, op, t)
	}
	if t == ntString {
		rhs, err := g.emitExpr(st.Expr, ntString)
		if err != nil {
			return err
		}
		_ = et
		g.emit("%s += %s\n", st.Name, rhs)
		return nil
	}
	var want ntype
	if op == "/=" {
		want = ntFloat
		if t != ntFloat {
			return fmt.Errorf("line %d: /= always yields float — %q must be float in native subset", st.Line, st.Name)
		}
		rhs, err := g.emitExpr(st.Expr, ntFloat)
		if err != nil {
			return err
		}
		g.emit("%s = ksDiv(%s, %s)\n", st.Name, st.Name, rhs)
		return nil
	}
	want = t
	if !assignable(want, et) {
		return fmt.Errorf("line %d: %s is not %s", st.Line, et, want)
	}
	rhs, err := g.emitExpr(st.Expr, want)
	if err != nil {
		return err
	}
	gop := map[string]string{"+=": "+=", "-=": "-=", "*=": "*=", "%=": "%="}[op]
	if op == "%=" {
		if t != ntInt {
			return fmt.Errorf("line %d: %%= needs ints", st.Line)
		}
		g.emit("%s = ksMod(%s, %s)\n", st.Name, st.Name, rhs)
		return nil
	}
	g.emit("%s %s %s\n", st.Name, gop, rhs)
	return nil
}

// printConv renders a typed Go value as its .ks Display string.
func (g *gen) printConv(t ntype, src string) string {
	switch t {
	case ntInt:
		g.useStrconv = true
		return "strconv.FormatInt(" + src + ", 10)"
	case ntFloat:
		g.useStrconv = true
		return "strconv.FormatFloat(" + src + ", 'f', -1, 64)"
	case ntBool:
		g.useStrconv = true
		return "strconv.FormatBool(" + src + ")"
	default:
		return src
	}
}

// emitExpr emits Go code for e, converting to want when int→float.
func (g *gen) emitExpr(e *frontend.Expr, want ntype) (string, error) {
	got, err := g.typeOf(e)
	if err != nil {
		return "", err
	}
	src, err := g.emitExprRaw(e)
	if err != nil {
		return "", err
	}
	if got == want {
		return src, nil
	}
	if want == ntFloat && got == ntInt {
		return "float64(" + src + ")", nil
	}
	return "", fmt.Errorf("%s is not %s in native subset", got, want)
}

func (g *gen) emitExprRaw(e *frontend.Expr) (string, error) {
	switch e.Kind {
	case frontend.ExprInt:
		return strconv.FormatInt(int64(e.IntVal), 10), nil
	case frontend.ExprFloat:
		return strconv.FormatFloat(e.FloatVal, 'f', -1, 64), nil
	case frontend.ExprString:
		return strconv.Quote(e.StrVal), nil
	case frontend.ExprBool:
		if e.BoolVal {
			return "true", nil
		}
		return "false", nil
	case frontend.ExprVar:
		return e.Name, nil
	case frontend.ExprAdd, frontend.ExprSub, frontend.ExprMul:
		return g.emitArith(e)
	case frontend.ExprDiv:
		l, err := g.emitExpr(e.Left, ntFloat)
		if err != nil {
			return "", err
		}
		r, err := g.emitExpr(e.Right, ntFloat)
		if err != nil {
			return "", err
		}
		return "ksDiv(" + l + ", " + r + ")", nil
	case frontend.ExprMod:
		l, err := g.emitExpr(e.Left, ntInt)
		if err != nil {
			return "", err
		}
		r, err := g.emitExpr(e.Right, ntInt)
		if err != nil {
			return "", err
		}
		return "ksMod(" + l + ", " + r + ")", nil
	case frontend.ExprPow:
		lt, _ := g.typeOf(e.Left)
		rt, _ := g.typeOf(e.Right)
		if lt == ntFloat || rt == ntFloat {
			g.useMath = true
			l, err := g.emitExpr(e.Left, ntFloat)
			if err != nil {
				return "", err
			}
			r, err := g.emitExpr(e.Right, ntFloat)
			if err != nil {
				return "", err
			}
			return "math.Pow(" + l + ", " + r + ")", nil
		}
		l, err := g.emitExpr(e.Left, ntInt)
		if err != nil {
			return "", err
		}
		// typeOf guarantees a non-negative int literal exponent here.
		return "ksPowI(" + l + ", " + strconv.FormatInt(int64(e.Right.IntVal), 10) + ")", nil
	case frontend.ExprNeg:
		t, _ := g.typeOf(e.Right)
		s, err := g.emitExpr(e.Right, t)
		if err != nil {
			return "", err
		}
		return "-(" + s + ")", nil
	case frontend.ExprNot:
		s, err := g.emitExpr(e.Right, ntBool)
		if err != nil {
			return "", err
		}
		return "!(" + s + ")", nil
	case frontend.ExprAnd, frontend.ExprOr:
		op := "&&"
		if e.Kind == frontend.ExprOr {
			op = "||"
		}
		l, err := g.emitExpr(e.Left, ntBool)
		if err != nil {
			return "", err
		}
		r, err := g.emitExpr(e.Right, ntBool)
		if err != nil {
			return "", err
		}
		return "(" + l + " " + op + " " + r + ")", nil
	case frontend.ExprEq, frontend.ExprNe:
		op := "=="
		if e.Kind == frontend.ExprNe {
			op = "!="
		}
		lt, _ := g.typeOf(e.Left)
		rt, _ := g.typeOf(e.Right)
		want := lt
		if rt == ntFloat {
			want = ntFloat
		}
		l, err := g.emitExpr(e.Left, want)
		if err != nil {
			return "", err
		}
		r, err := g.emitExpr(e.Right, want)
		if err != nil {
			return "", err
		}
		return "(" + l + " " + op + " " + r + ")", nil
	case frontend.ExprLt, frontend.ExprLe, frontend.ExprGt, frontend.ExprGe:
		op := map[frontend.ExprKind]string{
			frontend.ExprLt: "<", frontend.ExprLe: "<=",
			frontend.ExprGt: ">", frontend.ExprGe: ">=",
		}[e.Kind]
		lt, _ := g.typeOf(e.Left)
		rt, _ := g.typeOf(e.Right)
		want := lt
		if rt == ntFloat {
			want = ntFloat
		}
		l, err := g.emitExpr(e.Left, want)
		if err != nil {
			return "", err
		}
		r, err := g.emitExpr(e.Right, want)
		if err != nil {
			return "", err
		}
		return "(" + l + " " + op + " " + r + ")", nil
	case frontend.ExprCall:
		return g.emitCall(e)
	}
	return "", fmt.Errorf("not in the native-0.1 subset — runs in interpreter")
}

func (g *gen) emitArith(e *frontend.Expr) (string, error) {
	lt, _ := g.typeOf(e.Left)
	rt, _ := g.typeOf(e.Right)
	want := lt
	if rt == ntFloat {
		want = ntFloat
	}
	op := map[frontend.ExprKind]string{
		frontend.ExprAdd: "+", frontend.ExprSub: "-", frontend.ExprMul: "*",
	}[e.Kind]
	l, err := g.emitExpr(e.Left, want)
	if err != nil {
		return "", err
	}
	r, err := g.emitExpr(e.Right, want)
	if err != nil {
		return "", err
	}
	if want == ntString {
		return "(" + l + " + " + r + ")", nil
	}
	return "(" + l + " " + op + " " + r + ")", nil
}

func (g *gen) emitCall(e *frontend.Expr) (string, error) {
	callee := e.Callee
	if callee == nil || callee.Kind != frontend.ExprVar {
		return "", fmt.Errorf("method calls are not in the native-0.1 subset — runs in interpreter")
	}
	name := callee.Name
	if name == "len" {
		a, err := g.emitExpr(e.Args[0], ntString)
		if err != nil {
			return "", err
		}
		g.useUTF8 = true
		return "int64(utf8.RuneCountInString(" + a + "))", nil
	}
	sig, ok := g.funcs[name]
	if !ok {
		var found bool
		sig, found = g.lookupClosure(name)
		if !found {
			return "", fmt.Errorf("unknown func %q", name)
		}
	}
	if g.pending[name] && name != g.inferring {
		return ntVoid, fmt.Errorf("func %q used before its return type is known — annotate it with `: type`", name)
	}
	var args []string
	for i, a := range e.Args {
		s, err := g.emitExpr(a, sig.params[i])
		if err != nil {
			return "", err
		}
		args = append(args, s)
	}
	call := name + "(" + strings.Join(args, ", ") + ")"
	if sig.ret == ntVoid {
		// void call in value position is rejected by callers via typeOf;
		// StmtExpr handles it through emitExpr with want=void.
		return call, nil
	}
	return call, nil
}

// emitForIn handles `for v in range(...)` (ints only).
func (g *gen) emitForIn(st *frontend.Stmt) error {
	if len(st.Names) != 1 {
		return fmt.Errorf("line %d: only single-var for-in is native-0.1 (no maps/arrays yet)", st.Line)
	}
	call := st.Expr
	if call == nil || call.Kind != frontend.ExprCall {
		return fmt.Errorf("line %d: for-in needs range(n) in native-0.1 — runs in interpreter", st.Line)
	}
	callee := call.Callee
	if callee == nil || callee.Kind != frontend.ExprVar || callee.Name != "range" {
		return fmt.Errorf("line %d: for-in needs range(n) in native-0.1 — runs in interpreter", st.Line)
	}
	if len(call.Args) < 1 || len(call.Args) > 3 {
		return fmt.Errorf("line %d: range wants 1-3 int args", st.Line)
	}
	for _, a := range call.Args {
		t, err := g.typeOf(a)
		if err != nil {
			return err
		}
		if t != ntInt {
			return fmt.Errorf("line %d: range wants ints in native subset", st.Line)
		}
	}
	if err := checkName(st.Names[0], "var", st.Line); err != nil {
		return err
	}
	start, end, step := "int64(0)", "", "int64(1)"
	switch len(call.Args) {
	case 1:
		s, err := g.emitExpr(call.Args[0], ntInt)
		if err != nil {
			return err
		}
		end = s
	case 2:
		s, err := g.emitExpr(call.Args[0], ntInt)
		if err != nil {
			return err
		}
		start = s
		s, err = g.emitExpr(call.Args[1], ntInt)
		if err != nil {
			return err
		}
		end = s
	case 3:
		s, err := g.emitExpr(call.Args[0], ntInt)
		if err != nil {
			return err
		}
		start = s
		s, err = g.emitExpr(call.Args[1], ntInt)
		if err != nil {
			return err
		}
		end = s
		s, err = g.emitExpr(call.Args[2], ntInt)
		if err != nil {
			return err
		}
		step = s
		if lit, ok := isIntLit(call.Args[2]); ok && lit == 0 {
			return fmt.Errorf("line %d: range step cannot be 0", st.Line)
		}
	}
	// Evaluate bounds once, like the interpreter's range array.
	sT, eT, pT := g.tmpName(), g.tmpName(), g.tmpName()
	g.emit("var %s int64 = %s\n", sT, start)
	g.emit("var %s int64 = %s\n", eT, end)
	g.emit("var %s int64 = %s\n", pT, step)
	g.emit("if %s == 0 {\n", pT)
	g.emit("panic(\"range step cannot be 0\")\n")
	g.emit("}\n")
	v := st.Names[0]
	g.emit("for %s := %s; (%s > 0 && %s < %s) || (%s < 0 && %s > %s); %s += %s {\n",
		v, sT, pT, v, eT, pT, v, eT, v, pT)
	g.pushScope()
	g.define(v, ntInt)
	g.loop++
	err := g.emitBlockBody(st.Body)
	g.loop--
	g.popScope()
	if err != nil {
		return err
	}
	g.emit("}\n")
	return nil
}

func isIntLit(e *frontend.Expr) (int64, bool) {
	if e != nil && e.Kind == frontend.ExprInt {
		return int64(e.IntVal), true
	}
	return 0, false
}

// emitForC handles C-style loops. With an init statement the loop gets
// its own scope block: { init; for ; cond; post { body } }.
func (g *gen) emitForC(st *frontend.Stmt) error {
	if st.Init == nil {
		return g.emitForCInner(st)
	}
	g.emit("{\n")
	g.pushScope()
	if err := g.emitForInit(st.Init); err != nil {
		return err
	}
	if err := g.emitForCInner(st); err != nil {
		return err
	}
	g.popScope()
	g.emit("}\n")
	return nil
}

// emitForInit emits a for-c init statement (let or assign) into the
// loop's scope.
func (g *gen) emitForInit(init *frontend.Stmt) error {
	switch init.Kind {
	case frontend.StmtLet:
		return g.emitLet(init)
	case frontend.StmtAssign:
		return g.emitAssign(init, true)
	default:
		return fmt.Errorf("line %d: bad for init in native subset", init.Line)
	}
}

// emitForPost renders a for-c post statement (assign or call) inline.
func (g *gen) emitForPost(post *frontend.Stmt) (string, error) {
	saved := g.sb
	var tmp strings.Builder
	g.sb = tmp
	var err error
	switch post.Kind {
	case frontend.StmtAssign:
		err = g.emitAssign(post, false)
	case frontend.StmtExpr:
		err = g.emitStmt(post)
	default:
		err = fmt.Errorf("line %d: bad for post in native subset", post.Line)
	}
	s := strings.TrimSuffix(strings.TrimSpace(g.sb.String()), "\n")
	g.sb = saved
	if err != nil {
		return "", err
	}
	return s, nil
}

// emitForCInner emits the `for ; cond; post {` header and body.
// Callers hold the scope where init vars (if any) are defined.
func (g *gen) emitForCInner(st *frontend.Stmt) error {
	cond, err := g.boolCond(st.Expr, st.Line)
	if err != nil {
		return err
	}
	var post string
	if st.Post != nil {
		var err error
		post, err = g.emitForPost(st.Post)
		if err != nil {
			return err
		}
	}
	g.emit("for ; %s; %s {\n", cond, post)
	g.pushScope()
	g.loop++
	err = g.emitBlockBody(st.Body)
	g.loop--
	g.popScope()
	if err != nil {
		return err
	}
	g.emit("}\n")
	return nil
}

func (g *gen) boolCond(e *frontend.Expr, line int) (string, error) {
	t, err := g.typeOf(e)
	if err != nil {
		return "", err
	}
	if t != ntBool {
		return "", fmt.Errorf("line %d: loop condition needs bool (no truthiness in native) — runs in interpreter", line)
	}
	return g.emitExpr(e, ntBool)
}
