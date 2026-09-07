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
	sb      strings.Builder
	funcs   map[string]*funcSig
	vars    []map[string]ntype // scope stack
	loop    int
	funcRet ntype // current function return type (ntVoid outside funcs)
	inFunc  bool
	useMath bool
	useTime bool
	useUTF8 bool
	tmp     int
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

func (g *gen) pushScope() { g.vars = append(g.vars, map[string]ntype{}) }
func (g *gen) popScope()  { g.vars = g.vars[:len(g.vars)-1] }

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
	g := &gen{funcs: map[string]*funcSig{}}
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
	}
	// Phase 2: infer return types by walking bodies (no emission).
	for _, st := range prog.Statements {
		if st.Kind != frontend.StmtFunc {
			continue
		}
		ret, err := g.inferFuncRet(st)
		if err != nil {
			return "", err
		}
		sig := g.funcs[st.Name]
		if st.ReturnType != "" {
			want, err := parseAnn(st.ReturnType, st.Line)
			if err != nil {
				return "", err
			}
			if ret == ntVoid {
				// No returns in body: declared type must still hold only
				// if the body never returns a value (checked at emit).
			}
			sig.ret = want
		} else {
			sig.ret = ret
		}
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
	out.WriteString("\t\"strconv\"\n")
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

// inferFuncRet unifies all return-statement types in a func body.
// No returns at all means void.
func (g *gen) inferFuncRet(st *frontend.Stmt) (ntype, error) {
	// Seed scope with params so bodies type-check.
	saved := g.vars
	g.vars = []map[string]ntype{{}}
	sig := g.funcs[st.Name]
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
		case frontend.StmtFunc, frontend.StmtGo:
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
		t, err := g.typeOf(e.Left)
		if err != nil {
			return ntVoid, err
		}
		if !isNum(t) {
			return ntVoid, fmt.Errorf("unary - needs a number in native subset — runs in interpreter")
		}
		return t, nil
	case frontend.ExprNot:
		t, err := g.typeOf(e.Left)
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
	name, ok := e.Callee.(*frontend.Expr)
	if !ok || name.Kind != frontend.ExprVar {
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

// assignable reports whether a value of type got fits a slot of type want
// (int promotes to float, nothing else converts implicitly).
func assignable(want, got ntype) bool {
	if want == got {
		return true
	}
	return want == ntFloat && got == ntInt
}
