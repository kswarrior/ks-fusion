// Package compiler is the ks-fusion ahead-of-time step toward Go/Rust parity.
//
// v0.2 (expanded subset): .ks source -> portable bytecode (.ksb-1, JSON) -> stack VM.
// It proves the pipeline (parse -> compile -> save -> load -> run) and now
// covers slices, `is`/`?.`/`??`, typed params/lets (runtime-checked, nilable),
// and `switch` (desugared to an Eq-chain, first match wins).
//
// Supported subset (everything else is a clear compile error, still runs
// in the interpreter):
//
//	literals: nil bool int float string, arrays, maps (string keys)
//	vars: let (+ `: type` check), =, += -= *= /= %= (var + index targets)
//	exprs: + - * / % **, == != < <= > >=, in, is, ??, and or not/!, unary -,
//	       a[i]/m.key (incl. `?.` safe), a[l:r] slices
//	calls: user funcs (+ typed params) + builtins (assert len range str int float type)
//	index: a[i], m.key, m["k"], s[i]
//	stmts: print, if/else, while, for-in (array/map/string), for-c,
//	       func/return (+ typed params), break/continue, switch, blocks
//
// Not yet: go/chan/select, import, try/catch, defer, sleep,
// closures capturing outer locals, struct/enum declarations.
package compiler

import (
	"fmt"

	"github.com/kswarrior/ks-fusion/internal/frontend"
)

// Format is the bytecode bundle format tag.
const Format = "ksb-1"

// Ext is the bytecode file extension.
const Ext = ".ksb"

// Op is a bytecode operation.
type Op int

const (
	OpConst Op = iota
	OpGetGlobal
	OpSetGlobal
	OpDefineGlobal
	OpGetLocal
	OpSetLocal
	OpPop
	OpAdd
	OpSub
	OpMul
	OpDiv
	OpMod
	OpPow
	OpNeg
	OpNot
	OpEq
	OpNe
	OpLt
	OpLe
	OpGt
	OpGe
	OpIn
	OpJump
	OpJumpIfFalse
	OpJumpIfTrue
	OpCall
	OpMakeFunc
	OpReturn
	OpPrint
	OpArray
	OpMap
	OpIndex
	OpSetIndex
	OpSleep // reserved: emitted never in v0.1 (sleep is a compile error)
	// v0.2 (full-language roadmap): slices, type tests, nil-coalescing,
	// safe access, typed params, switch/try/defer desugar targets.
	OpSlice
	OpIs
	OpCoalesce
	OpSafeIndex
	OpCheckType
	OpSetupTry
	OpPopTry
	OpDefer
	OpJumpIfNotNil
)

func (o Op) String() string {
	switch o {
	case OpConst:
		return "Const"
	case OpGetGlobal:
		return "GetGlobal"
	case OpSetGlobal:
		return "SetGlobal"
	case OpDefineGlobal:
		return "DefineGlobal"
	case OpGetLocal:
		return "GetLocal"
	case OpSetLocal:
		return "SetLocal"
	case OpPop:
		return "Pop"
	case OpAdd:
		return "Add"
	case OpSub:
		return "Sub"
	case OpMul:
		return "Mul"
	case OpDiv:
		return "Div"
	case OpMod:
		return "Mod"
	case OpPow:
		return "Pow"
	case OpNeg:
		return "Neg"
	case OpNot:
		return "Not"
	case OpEq:
		return "Eq"
	case OpNe:
		return "Ne"
	case OpLt:
		return "Lt"
	case OpLe:
		return "Le"
	case OpGt:
		return "Gt"
	case OpGe:
		return "Ge"
	case OpIn:
		return "In"
	case OpJump:
		return "Jump"
	case OpJumpIfFalse:
		return "JumpIfFalse"
	case OpJumpIfTrue:
		return "JumpIfTrue"
	case OpCall:
		return "Call"
	case OpMakeFunc:
		return "MakeFunc"
	case OpReturn:
		return "Return"
	case OpPrint:
		return "Print"
	case OpArray:
		return "Array"
	case OpMap:
		return "Map"
	case OpIndex:
		return "Index"
	case OpSetIndex:
		return "SetIndex"
	case OpSleep:
		return "Sleep"
	case OpSlice:
		return "Slice"
	case OpIs:
		return "Is"
	case OpCoalesce:
		return "Coalesce"
	case OpSafeIndex:
		return "SafeIndex"
	case OpCheckType:
		return "CheckType"
	case OpSetupTry:
		return "SetupTry"
	case OpPopTry:
		return "PopTry"
	case OpDefer:
		return "Defer"
	case OpJumpIfNotNil:
		return "JumpIfNotNil"
	}
	return "Unknown"
}

// Instr is one bytecode instruction. Arg meaning depends on Op:
// Const/GetGlobal/SetGlobal/DefineGlobal/GetLocal/SetLocal/MakeFunc/Print/
// Array/Map/Call/Jumps use Arg; other ops ignore it.
type Instr struct {
	Op   Op     `json:"op"`
	Arg  int    `json:"arg"`
	Line int    `json:"line"`
	Name string `json:"name,omitempty"` // globals/funcs: resolved name (debug)
}

// Const kinds.
const (
	CKNil    = "nil"
	CKBool   = "bool"
	CKInt    = "int"
	CKFloat  = "float"
	CKString = "string"
	CKFunc   = "func"
)

// Const is a constant pool entry. Func holds an index into Bundle.Funcs.
type Const struct {
	Kind  string  `json:"kind"`
	Bool  bool    `json:"bool,omitempty"`
	Int   int     `json:"int,omitempty"`
	Float float64 `json:"float,omitempty"`
	Str   string  `json:"str,omitempty"`
	Func  int     `json:"func,omitempty"`
}

// Chunk is the bytecode for one function.
type Chunk struct {
	Code   []Instr `json:"code"`
	Consts []Const `json:"consts"`
}

// Func is one compiled function.
type Func struct {
	Name   string   `json:"name"`
	Params []string `json:"params"`
	Chunk  Chunk    `json:"chunk"`
}

// Bundle is the on-disk .ksb format.
type Bundle struct {
	Format  string   `json:"format"`
	Name    string   `json:"name"`
	Funcs   []Func   `json:"funcs"`
	Globals []string `json:"globals"`
	Main    int      `json:"main"`
}

// ---------------------------------------------------------------------------
// Compiler
// ---------------------------------------------------------------------------

type local struct {
	name  string
	depth int
}

type loopCtx struct {
	breaks       []int
	continues    []int
	continueAt   int // >=0 when known before body; -1 defers to continues list
	savedDepth   int
	savedNLocals int
	tryDepth     int // lexically enclosing trys at loop/switch entry
}

type funcCtx struct {
	fn         *Func
	locals     []local
	depth      int
	returnType string
}

type compiler struct {
	funcs   []*Func
	globals []string
	gindex  map[string]int
	frames  []*funcCtx
	loops   []loopCtx
	tryDepth int // lexically enclosing try regions (for break/continue pops)
	isMain  bool
	src     string
}

func newCompiler() *compiler {
	return &compiler{gindex: map[string]int{}}
}

func (c *compiler) cur() *funcCtx { return c.frames[len(c.frames)-1] }

func (c *compiler) emit(op Op, arg, line int) int {
	f := c.cur().fn
	f.Chunk.Code = append(f.Chunk.Code, Instr{Op: op, Arg: arg, Line: line})
	return len(f.Chunk.Code) - 1
}

func (c *compiler) emitNamed(op Op, arg int, name string, line int) int {
	f := c.cur().fn
	f.Chunk.Code = append(f.Chunk.Code, Instr{Op: op, Arg: arg, Line: line, Name: name})
	return len(f.Chunk.Code) - 1
}

func (c *compiler) patch(pos, target int) {
	c.cur().fn.Chunk.Code[pos].Arg = target
}

func (c *compiler) addConst(k Const) int {
	f := c.cur().fn
	// dedupe ints/floats/strings/bools/nil for smaller bundles
	for i, e := range f.Chunk.Consts {
		if e.Kind == k.Kind && e.Bool == k.Bool && e.Int == k.Int &&
			e.Float == k.Float && e.Str == k.Str && e.Func == k.Func {
			return i
		}
	}
	f.Chunk.Consts = append(f.Chunk.Consts, k)
	return len(f.Chunk.Consts) - 1
}

func (c *compiler) globalIndex(name string) int {
	if i, ok := c.gindex[name]; ok {
		return i
	}
	i := len(c.globals)
	c.globals = append(c.globals, name)
	c.gindex[name] = i
	return i
}

// resolve returns (slot, isLocal, isEnclosingLocal).
func (c *compiler) resolve(name string) (int, bool, bool) {
	cur := c.cur()
	for i := len(cur.locals) - 1; i >= 0; i-- {
		if cur.locals[i].name == name {
			return i, true, false
		}
	}
	// check enclosing functions: capture = unsupported in v0.1
	for fi := len(c.frames) - 2; fi >= 0; fi-- {
		for _, l := range c.frames[fi].locals {
			if l.name == name {
				return -1, false, true
			}
		}
		// enclosing function names themselves are globals, not captures
		if c.frames[fi].fn.Name == name {
			return -1, false, false
		}
	}
	return -1, false, false
}

func (c *compiler) defineLocal(name string) int {
	cur := c.cur()
	slot := len(cur.locals)
	cur.locals = append(cur.locals, local{name: name, depth: cur.depth})
	return slot
}

func (c *compiler) beginScope() { c.cur().depth++ }

func (c *compiler) endScope(line int) {
	cur := c.cur()
	for len(cur.locals) > 0 && cur.locals[len(cur.locals)-1].depth > cur.depth-1 &&
		cur.depth > 0 {
		// pop locals defined in the closing scope
		top := cur.locals[len(cur.locals)-1]
		if top.depth >= cur.depth {
			cur.locals = cur.locals[:len(cur.locals)-1]
			c.emit(OpPop, 0, line)
		} else {
			break
		}
	}
	cur.depth--
}

// CompileProgram compiles a parsed program (subset) to a Bundle.
func CompileProgram(p *frontend.Program) (*Bundle, error) {
	c := newCompiler()
	// NOTE: Funcs are heap-allocated individually: frames keep raw *Func
	// pointers, so c.funcs must never move them (no []Func + append).
	c.funcs = append(c.funcs, &Func{Name: "<main>"})
	c.frames = append(c.frames, &funcCtx{fn: c.funcs[0]})
	c.isMain = true
	// Pre-register top-level globals so functions compiled before the
	// textual definition still resolve (calls happen after all defs run).
	for _, st := range p.Statements {
		if st.Kind == frontend.StmtLet || st.Kind == frontend.StmtFunc {
			c.globalIndex(st.Name)
		}
	}
	for _, st := range p.Statements {
		if err := c.compileStmt(st); err != nil {
			return nil, err
		}
	}
	// main falls through returning nil
	c.emit(OpConst, c.addConst(Const{Kind: CKNil}), 0)
	c.emit(OpReturn, 0, 0)
	funcs := make([]Func, len(c.funcs))
	for i, f := range c.funcs {
		funcs[i] = *f
	}
	b := &Bundle{Format: Format, Name: p.Path, Funcs: funcs, Globals: c.globals, Main: 0}
	return b, nil
}

// CompileSource parses and compiles source text.
func CompileSource(src, path string) (*Bundle, error) {
	p, err := frontend.ParseSource(src, path)
	if err != nil {
		return nil, err
	}
	return CompileProgram(p)
}

// ---------------------------------------------------------------------------
// Statements
// ---------------------------------------------------------------------------

func (c *compiler) compileStmt(st *frontend.Stmt) error {
	switch st.Kind {
	case frontend.StmtLet:
		return c.compileLet(st)
	case frontend.StmtAssign:
		return c.compileAssign(st)
	case frontend.StmtPrint:
		return c.compilePrint(st)
	case frontend.StmtBlock:
		c.beginScope()
		for _, s := range st.List {
			if err := c.compileStmt(s); err != nil {
				return err
			}
		}
		c.endScope(st.Line)
		return nil
	case frontend.StmtIf:
		return c.compileIf(st)
	case frontend.StmtWhile:
		return c.compileWhile(st)
	case frontend.StmtForIn:
		return c.compileForIn(st)
	case frontend.StmtForC:
		return c.compileForC(st)
	case frontend.StmtFunc:
		return c.compileFuncDefTyped(st.Name, st.Names, st.ParamTypes, st.ReturnType, st.Body, st.Line)
	case frontend.StmtReturn:
		if len(c.frames) == 1 {
			return fmt.Errorf("line %d: return outside function (compiler v0.2)", st.Line)
		}
		if err := c.compileExpr(st.Expr); err != nil {
			return err
		}
		if rt := c.cur().returnType; rt != "" && rt != "any" {
			// runtime return-type check (nil passes): dup via local? CheckType
			// peeks, so emit check then return (value stays).
			c.emitNamed(OpCheckType, 0, rt, st.Line)
		}
		c.emit(OpReturn, 0, st.Line)
		return nil
	case frontend.StmtBreak:
		if len(c.loops) == 0 {
			return fmt.Errorf("line %d: break outside loop (compiler v0.1)", st.Line)
		}
		// pop block locals above loop depth before jumping
		c.emitTryPops(st.Line)
		c.popToLoopDepth(st.Line)
		pos := c.emit(OpJump, -1, st.Line)
		l := len(c.loops) - 1
		c.loops[l].breaks = append(c.loops[l].breaks, pos)
		return nil
	case frontend.StmtContinue:
		if len(c.loops) == 0 {
			return fmt.Errorf("line %d: continue outside loop (compiler v0.1)", st.Line)
		}
		c.emitTryPops(st.Line)
		c.popToLoopDepth(st.Line)
		lc := &c.loops[len(c.loops)-1]
		if lc.continueAt >= 0 {
			c.emit(OpJump, lc.continueAt, st.Line)
		} else {
			pos := c.emit(OpJump, -1, st.Line)
			lc.continues = append(lc.continues, pos)
		}
		return nil
	case frontend.StmtExpr:
		if err := c.compileExpr(st.Expr); err != nil {
			return err
		}
		c.emit(OpPop, 0, st.Line)
		return nil
	case frontend.StmtGo:
		return fmt.Errorf("line %d: `go` not yet supported by compiler v0.2 (runs in interpreter)", st.Line)
	case frontend.StmtSleep:
		// `sleep ms` / `sleep(ms)`: call the sleep builtin, drop the nil.
		// Mirrors backend StmtSleep (eval ms + toMillis); a nil Expr with
		// SleepMs set is the parser's int-literal compat form.
		c.emitGetGlobal("sleep", st.Line)
		if st.Expr != nil {
			if err := c.compileExpr(st.Expr); err != nil {
				return err
			}
		} else {
			c.emit(OpConst, c.addConst(Const{Kind: CKInt, Int: st.SleepMs}), st.Line)
		}
		c.emit(OpCall, 1, st.Line)
		c.emit(OpPop, 0, st.Line)
		return nil
	case frontend.StmtImport:
		return fmt.Errorf("line %d: `import` not yet supported by compiler v0.2 (runs in interpreter)", st.Line)
	case frontend.StmtTry:
		return c.compileTry(st)
	case frontend.StmtSwitch:
		return c.compileSwitch(st)
	case frontend.StmtSelect:
		return fmt.Errorf("line %d: `select` not yet supported by compiler v0.2 (runs in interpreter)", st.Line)
	case frontend.StmtDefer:
		return fmt.Errorf("line %d: `defer` not yet supported by compiler v0.2 (runs in interpreter)", st.Line)
	case frontend.StmtStruct, frontend.StmtEnum:
		return fmt.Errorf("line %d: `struct`/`enum` declarations not yet supported by compiler v0.2 (runs in interpreter)", st.Line)
	}
	return fmt.Errorf("line %d: unknown statement (compiler v0.2)", st.Line)
}

// compileSwitch desugars `switch x { case a,b {..} default {..} }` to a
// hidden-target + Eq-chain (first match wins, no fallthrough). `break`
// inside a case exits the switch; `continue` propagates to the enclosing loop.
func (c *compiler) compileSwitch(st *frontend.Stmt) error {
	c.beginScope()
	if err := c.compileExpr(st.Expr); err != nil {
		return err
	}
	tgtTmp := c.hiddenName("switch")
	tgtSlot := c.defineLocal(tgtTmp)
	_ = tgtSlot
	// switch break context (captures break, forwards continue)
	c.loops = append(c.loops, loopCtx{continueAt: -2, savedDepth: c.cur().depth, savedNLocals: len(c.cur().locals), tryDepth: c.tryDepth})
	var endJumps []int
	for _, cs := range st.Cases {
		if cs.IsDefault {
			// default: always runs (if reached)
			c.beginScope()
			if err := c.compileStmt(cs.Body); err != nil {
				return err
			}
			c.endScope(st.Line)
			continue
		}
		// for each value in `case a, b`: if target == v, run body then jump end
		var nextTests []int
		for _, val := range cs.Values {
			c.emitGetLocal(tgtSlot, tgtTmp, st.Line)
			if err := c.compileExpr(val); err != nil {
				return err
			}
			c.emit(OpEq, 0, st.Line)
			jNext := c.emit(OpJumpIfFalse, -1, st.Line)
			c.emit(OpPop, 0, st.Line)
			// matched: run body
			c.beginScope()
			if err := c.compileStmt(cs.Body); err != nil {
				return err
			}
			c.endScope(st.Line)
			endJumps = append(endJumps, c.emit(OpJump, -1, st.Line))
			c.patch(jNext, len(c.cur().fn.Chunk.Code))
			c.emit(OpPop, 0, st.Line)
			_ = nextTests
		}
	}
	// patch switch breaks to here; forward continues to enclosing loop
	sw := c.loops[len(c.loops)-1]
	c.loops = c.loops[:len(c.loops)-1]
	end := len(c.cur().fn.Chunk.Code)
	for _, b := range sw.breaks {
		c.patch(b, end)
	}
	for _, cp := range endJumps {
		c.patch(cp, end)
	}
	// forward switch-level continues (should be none — `continue` in a case
	// targets the enclosing for/while) to the parent loop context.
	if len(sw.continues) > 0 && len(c.loops) > 0 {
		parent := &c.loops[len(c.loops)-1]
		parent.continues = append(parent.continues, sw.continues...)
	} else if len(sw.continues) > 0 {
		return fmt.Errorf("line %d: continue outside loop (compiler v0.2)", st.Line)
	}
	c.endScope(st.Line)
	return nil
}

func (c *compiler) popToLoopDepth(line int) {
	if len(c.loops) == 0 {
		return
	}
	// Emit pops for the jump path only; do NOT mutate the lexical locals
	// list (following statements in the same block still see them).
	want := c.loops[len(c.loops)-1].savedNLocals
	n := len(c.cur().locals) - want
	for i := 0; i < n; i++ {
		c.emit(OpPop, 0, line)
	}
}

// emitTryPops emits OpPopTry for try regions the current break/continue
// exits: any try opened after the target loop/switch entry. tryDepth is
// purely lexical, so this also covers try-in-switch-case (the switch ctx
// carries its entry depth, and switch-level continues forward the same
// already-popped path to the parent loop).
func (c *compiler) emitTryPops(line int) {
	if len(c.loops) == 0 {
		return
	}
	n := c.tryDepth - c.loops[len(c.loops)-1].tryDepth
	for i := 0; i < n; i++ {
		c.emit(OpPopTry, 0, line)
	}
}

func (c *compiler) emitGetGlobal(name string, line int) {
	gi := c.globalIndex(name)
	c.emitNamed(OpGetGlobal, gi, name, line)
}

func (c *compiler) emitGetLocal(slot int, name string, line int) {
	c.emitNamed(OpGetLocal, slot, name, line)
}

func (c *compiler) compileLet(st *frontend.Stmt) error {
	if err := c.compileExpr(st.Expr); err != nil {
		return err
	}
	// v0.2: `let x: T` runtime check (nil passes, like the interpreter).
	if st.TypeAnn != "" && st.TypeAnn != "any" {
		c.emitNamed(OpCheckType, 0, st.TypeAnn, st.Line)
		// CheckType peeks; value stays for Define/Set below. Pop the extra?
		// No: CheckType leaves the value, so Define consumes it. But our
		// CheckType peeks (no pop), so the stack still has one value. Good.
		// Actually CheckType peeks, so we need no extra handling — but it
		// leaves the value; DefineGlobal/SetLocal will pop it? No, Define
		// pops via OpDefineGlobal which pops. For locals, defineLocal does
		// not emit (value already on stack as local slot). Wait: locals are
		// stack slots — value stays. CheckType peeked, so still one value.
		// Pop the check dup? No dup — CheckType does not push. So stack is
		// correct: one value for the binding.
		// However CheckType above peeked; we must pop nothing extra.
		// To keep the value for the binding, do nothing here — but CheckType
		// peeked, so the value remains. For globals, DefineGlobal pops it.
		// For locals, the value is the slot. Both correct. But we emitted
		// CheckType which peeked; the value is still there. Good.
		// Note: CheckType leaves value; no pop needed. Remove the Pop below
		// if added. (Kept as peek semantics.)
		_ = 0
	}
	// Top-level (depth 0) lets are globals; block-scoped and function lets
	// are locals so `{ let x = 1 }` does not leak.
	if len(c.frames) == 1 && c.cur().depth == 0 {
		gi := c.globalIndex(st.Name)
		c.emitNamed(OpDefineGlobal, gi, st.Name, st.Line)
		return nil
	}
	if slot, isLocal, _ := c.resolveInCurrent(st.Name); isLocal {
		_ = slot
		return fmt.Errorf("line %d: %q already defined in this scope (compiler v0.1: shadowing with `let` needs a block)", st.Line, st.Name)
	}
	c.defineLocal(st.Name)
	return nil
}

func (c *compiler) resolveInCurrent(name string) (int, bool, bool) {
	cur := c.cur()
	for i := len(cur.locals) - 1; i >= 0; i-- {
		if cur.locals[i].name == name {
			return i, true, false
		}
	}
	return -1, false, false
}

func (c *compiler) compileAssign(st *frontend.Stmt) error {
	op := st.Op
	if op == "" {
		op = "="
	}
	// indexed target: Expr is target, Exprs[0] is value
	if st.Name == "" {
		if st.Expr == nil || len(st.Exprs) == 0 {
			return fmt.Errorf("line %d: bad assignment (compiler v0.1)", st.Line)
		}
		tgt := st.Expr
		if tgt.Kind != frontend.ExprIndex {
			return fmt.Errorf("line %d: bad assignment target (compiler v0.1)", st.Line)
		}
		if op == "=" {
			if err := c.compileExpr(tgt.Left); err != nil {
				return err
			}
			if err := c.compileExpr(tgt.Right); err != nil {
				return err
			}
			if err := c.compileExpr(st.Exprs[0]); err != nil {
				return err
			}
			c.emit(OpSetIndex, 0, st.Line)
			return nil
		}
		return fmt.Errorf("line %d: indexed %s not yet supported by compiler v0.1", st.Line, op)
	}
	// var target
	if op == "=" {
		if err := c.compileExpr(st.Expr); err != nil {
			return err
		}
		return c.storeVar(st.Name, st.Line)
	}
	// compound: load old, compile rhs, apply op, store
	if err := c.loadVar(st.Name, st.Line); err != nil {
		return err
	}
	if err := c.compileExpr(st.Expr); err != nil {
		return err
	}
	switch op {
	case "+=":
		c.emit(OpAdd, 0, st.Line)
	case "-=":
		c.emit(OpSub, 0, st.Line)
	case "*=":
		c.emit(OpMul, 0, st.Line)
	case "/=":
		c.emit(OpDiv, 0, st.Line)
	case "%=":
		c.emit(OpMod, 0, st.Line)
	default:
		return fmt.Errorf("line %d: bad assign op %q (compiler v0.1)", st.Line, op)
	}
	return c.storeVar(st.Name, st.Line)
}

func (c *compiler) loadVar(name string, line int) error {
	if slot, isLocal, enclosing := c.resolve(name); enclosing {
		return fmt.Errorf("line %d: closure capture of %q not yet supported by compiler v0.1 (use globals/params)", line, name)
	} else if isLocal {
		c.emitNamed(OpGetLocal, slot, name, line)
		return nil
	}
	gi := c.globalIndex(name)
	c.emitNamed(OpGetGlobal, gi, name, line)
	return nil
}

func (c *compiler) storeVar(name string, line int) error {
	if slot, isLocal, enclosing := c.resolve(name); enclosing {
		return fmt.Errorf("line %d: closure capture of %q not yet supported by compiler v0.1", line, name)
	} else if isLocal {
		c.emitNamed(OpSetLocal, slot, name, line)
		return nil
	} else {
		// top-level define via `let` uses DefineGlobal; plain `=` requires existing var
		if len(c.frames) == 1 {
			if _, ok := c.gindex[name]; !ok {
				return fmt.Errorf("line %d: unknown variable %q (try `let %s = ...` first)", line, name, name)
			}
		}
		gi := c.globalIndex(name)
		// inside functions, `x = ...` with unknown name is an error (no implicit globals)
		if len(c.frames) > 1 {
			if _, ok := c.gindex[name]; !ok {
				return fmt.Errorf("line %d: unknown variable %q", line, name)
			}
		}
		c.emitNamed(OpSetGlobal, gi, name, line)
		return nil
	}
}

func (c *compiler) compilePrint(st *frontend.Stmt) error {
	args := st.Exprs
	if len(args) == 0 && st.Expr != nil {
		args = []*frontend.Expr{st.Expr}
	}
	for _, a := range args {
		if err := c.compileExpr(a); err != nil {
			return err
		}
	}
	c.emit(OpPrint, len(args), st.Line)
	return nil
}

func (c *compiler) compileIf(st *frontend.Stmt) error {
	if err := c.compileExpr(st.Expr); err != nil {
		return err
	}
	jFalse := c.emit(OpJumpIfFalse, -1, st.Line)
	c.emit(OpPop, 0, st.Line)
	c.beginScope()
	if err := c.compileStmt(st.Then); err != nil {
		return err
	}
	c.endScope(st.Line)
	if st.Else != nil {
		jEnd := c.emit(OpJump, -1, st.Line)
		c.patch(jFalse, len(c.cur().fn.Chunk.Code))
		c.emit(OpPop, 0, st.Line)
		if st.Else.Kind == frontend.StmtIf {
			if err := c.compileStmt(st.Else); err != nil {
				return err
			}
		} else {
			c.beginScope()
			if err := c.compileStmt(st.Else); err != nil {
				return err
			}
			c.endScope(st.Line)
		}
		c.patch(jEnd, len(c.cur().fn.Chunk.Code))
		return nil
	}
	c.patch(jFalse, len(c.cur().fn.Chunk.Code))
	c.emit(OpPop, 0, st.Line)
	return nil
}

func (c *compiler) compileWhile(st *frontend.Stmt) error {
	c.beginScope()
	start := len(c.cur().fn.Chunk.Code)
	c.loops = append(c.loops, loopCtx{continueAt: start, savedDepth: c.cur().depth, savedNLocals: len(c.cur().locals), tryDepth: c.tryDepth})
	if err := c.compileExpr(st.Expr); err != nil {
		return err
	}
	jFalse := c.emit(OpJumpIfFalse, -1, st.Line)
	c.emit(OpPop, 0, st.Line)
	if err := c.compileStmt(st.Body); err != nil {
		return err
	}
	c.emit(OpJump, start, st.Line)
	c.patch(jFalse, len(c.cur().fn.Chunk.Code))
	c.emit(OpPop, 0, st.Line)
	l := c.loops[len(c.loops)-1]
	for _, b := range l.breaks {
		c.patch(b, len(c.cur().fn.Chunk.Code))
	}
	c.loops = c.loops[:len(c.loops)-1]
	c.endScope(st.Line)
	return nil
}

// compileForIn desugars to a while loop over hidden iter/idx locals using
// __iter_len/__iter_get/__iter_key/__iter_val builtins at runtime.
func (c *compiler) compileForIn(st *frontend.Stmt) error {
	if isRangeLoop(st) {
		return c.compileForInRange(st)
	}
	c.beginScope()
	iterTmp := c.hiddenName("iter")
	idxTmp := c.hiddenName("idx")
	needKeys := len(st.Names) == 2
	keysTmp := ""
	if needKeys {
		keysTmp = c.hiddenName("keys")
	}
	if err := c.compileExpr(st.Expr); err != nil {
		return err
	}
	iterSlot := c.defineLocal(iterTmp)
	_ = iterSlot
	// idx = 0
	c.emit(OpConst, c.addConst(Const{Kind: CKInt, Int: 0}), st.Line)
	idxSlot := c.defineLocal(idxTmp)
	_ = idxSlot
	keysSlot := -1
	if needKeys {
		// keys = __map_keys_or_nil(iter)
		c.emitGetGlobal("__map_keys_or_nil", st.Line)
		c.emitGetLocal(iterSlot, iterTmp, st.Line)
		c.emit(OpCall, 1, st.Line)
		keysSlot = c.defineLocal(keysTmp)
	}
	// loop vars need real stack slots: push nil placeholders first
	for _, n := range st.Names {
		c.emit(OpConst, c.addConst(Const{Kind: CKNil}), st.Line)
		c.defineLocal(n)
	}
	loopStart := len(c.cur().fn.Chunk.Code)
	c.loops = append(c.loops, loopCtx{continueAt: -1, savedDepth: c.cur().depth, savedNLocals: len(c.cur().locals), tryDepth: c.tryDepth})
	// cond: idx < __iter_len(iter)
	c.emitGetLocal(idxSlot, idxTmp, st.Line)
	c.emitGetGlobal("__iter_len", st.Line)
	c.emitGetLocal(iterSlot, iterTmp, st.Line)
	c.emit(OpCall, 1, st.Line)
	c.emit(OpLt, 0, st.Line)
	jFalse := c.emit(OpJumpIfFalse, -1, st.Line)
	c.emit(OpPop, 0, st.Line)
	// bind loop vars (continue jumps to post below, not here)
	if len(st.Names) == 1 {
		c.emitGetGlobal("__iter_get", st.Line)
		c.emitGetLocal(iterSlot, iterTmp, st.Line)
		c.emitGetLocal(idxSlot, idxTmp, st.Line)
		c.emit(OpCall, 2, st.Line)
		if err := c.storeVar(st.Names[0], st.Line); err != nil {
			return err
		}
	} else {
		// k = __iter_key(iter, keys, idx); v = __iter_val(iter, idx)
		c.emitGetGlobal("__iter_key", st.Line)
		c.emitGetLocal(iterSlot, iterTmp, st.Line)
		c.emitGetLocal(keysSlot, keysTmp, st.Line)
		c.emitGetLocal(idxSlot, idxTmp, st.Line)
		c.emit(OpCall, 3, st.Line)
		if err := c.storeVar(st.Names[0], st.Line); err != nil {
			return err
		}
		c.emitGetGlobal("__iter_val", st.Line)
		c.emitGetLocal(iterSlot, iterTmp, st.Line)
		c.emitGetLocal(idxSlot, idxTmp, st.Line)
		c.emit(OpCall, 2, st.Line)
		if err := c.storeVar(st.Names[1], st.Line); err != nil {
			return err
		}
	}
	if err := c.compileStmt(st.Body); err != nil {
		return err
	}
	// post: idx = idx + 1  (continue jumps here)
	postPos := len(c.cur().fn.Chunk.Code)
	for _, cp := range c.loops[len(c.loops)-1].continues {
		c.patch(cp, postPos)
	}
	c.loops[len(c.loops)-1].continueAt = postPos
	c.emitGetLocal(idxSlot, idxTmp, st.Line)
	c.emit(OpConst, c.addConst(Const{Kind: CKInt, Int: 1}), st.Line)
	c.emit(OpAdd, 0, st.Line)
	c.emitNamed(OpSetLocal, idxSlot, idxTmp, st.Line)
	c.emit(OpJump, loopStart, st.Line)
	c.patch(jFalse, len(c.cur().fn.Chunk.Code))
	c.emit(OpPop, 0, st.Line)
	l := c.loops[len(c.loops)-1]
	for _, b := range l.breaks {
		c.patch(b, len(c.cur().fn.Chunk.Code))
	}
	c.loops = c.loops[:len(c.loops)-1]
	c.endScope(st.Line)
	return nil
}

var hiddenCounter int

func (c *compiler) hiddenName(base string) string {
	hiddenCounter++
	return fmt.Sprintf("__ks_%s_%d", base, hiddenCounter)
}

// isRangeLoop detects `for [k,] v in range(e) | range(a, b)` for the integer
// fast path. Detection is name-based, mirroring backend.rangeArgs (the
// interpreter fast path), so VM and interpreter agree on every input —
// including a user-shadowed `range`, which both engines treat as the builtin
// here. Three-arg `range(a, b, step)` and non-range iterables stay on the
// generic __iter_* path (same values, just slower).
func isRangeLoop(st *frontend.Stmt) bool {
	if len(st.Names) < 1 || len(st.Names) > 2 {
		return false
	}
	e := st.Expr
	if e == nil || e.Kind != frontend.ExprCall || e.Callee == nil ||
		e.Callee.Kind != frontend.ExprVar || e.Callee.Name != "range" {
		return false
	}
	return len(e.Args) == 1 || len(e.Args) == 2
}

// compileForInRange compiles the range-loop subset to a call-free integer
// loop: hidden counter + end slots, one Lt + slot binds per iteration. No
// array alloc, no __iter_* calls. Semantics mirror backend.execForIn step-1
// path: one var gets the value; two vars get (0-based index, value); empty
// when end <= start; non-int bounds are runtime errors in both engines.
func (c *compiler) compileForInRange(st *frontend.Stmt) error {
	two := len(st.Names) == 2
	c.beginScope()
	ctrTmp := c.hiddenName("rctr")
	endTmp := c.hiddenName("rend")
	// eval bounds once (same single-evaluation as the generic path).
	// Slots are append-only, so captured indices stay valid below.
	var ctrSlot, endSlot int
	if len(st.Expr.Args) == 1 {
		// start = 0
		c.emit(OpConst, c.addConst(Const{Kind: CKInt, Int: 0}), st.Line)
		ctrSlot = c.defineLocal(ctrTmp)
		if err := c.compileExpr(st.Expr.Args[0]); err != nil {
			return err
		}
		c.emitNamed(OpCheckType, 0, "int", st.Line)
		endSlot = c.defineLocal(endTmp)
	} else {
		if err := c.compileExpr(st.Expr.Args[0]); err != nil {
			return err
		}
		c.emitNamed(OpCheckType, 0, "int", st.Line)
		ctrSlot = c.defineLocal(ctrTmp)
		if err := c.compileExpr(st.Expr.Args[1]); err != nil {
			return err
		}
		c.emitNamed(OpCheckType, 0, "int", st.Line)
		endSlot = c.defineLocal(endTmp)
	}
	keyTmp := ""
	keySlot := -1
	if two {
		// key = 0-based index
		keyTmp = c.hiddenName("rkey")
		c.emit(OpConst, c.addConst(Const{Kind: CKInt, Int: 0}), st.Line)
		keySlot = c.defineLocal(keyTmp)
	}
	// loop vars need real stack slots: push nil placeholders first
	for _, n := range st.Names {
		c.emit(OpConst, c.addConst(Const{Kind: CKNil}), st.Line)
		c.defineLocal(n)
	}
	loopStart := len(c.cur().fn.Chunk.Code)
	c.loops = append(c.loops, loopCtx{continueAt: -1, savedDepth: c.cur().depth, savedNLocals: len(c.cur().locals), tryDepth: c.tryDepth})
	// cond: ctr < end
	c.emitGetLocal(ctrSlot, ctrTmp, st.Line)
	c.emitGetLocal(endSlot, endTmp, st.Line)
	c.emit(OpLt, 0, st.Line)
	jFalse := c.emit(OpJumpIfFalse, -1, st.Line)
	c.emit(OpPop, 0, st.Line)
	// bind loop vars (value, then index for the two-var form)
	c.emitGetLocal(ctrSlot, ctrTmp, st.Line)
	if err := c.storeVar(st.Names[len(st.Names)-1], st.Line); err != nil {
		return err
	}
	if two {
		c.emitGetLocal(keySlot, keyTmp, st.Line)
		if err := c.storeVar(st.Names[0], st.Line); err != nil {
			return err
		}
	}
	if err := c.compileStmt(st.Body); err != nil {
		return err
	}
	// post: ctr += 1 (key += 1); continue jumps here
	postPos := len(c.cur().fn.Chunk.Code)
	for _, cp := range c.loops[len(c.loops)-1].continues {
		c.patch(cp, postPos)
	}
	c.loops[len(c.loops)-1].continueAt = postPos
	c.emitGetLocal(ctrSlot, ctrTmp, st.Line)
	c.emit(OpConst, c.addConst(Const{Kind: CKInt, Int: 1}), st.Line)
	c.emit(OpAdd, 0, st.Line)
	c.emitNamed(OpSetLocal, ctrSlot, ctrTmp, st.Line)
	if two {
		c.emitGetLocal(keySlot, keyTmp, st.Line)
		c.emit(OpConst, c.addConst(Const{Kind: CKInt, Int: 1}), st.Line)
		c.emit(OpAdd, 0, st.Line)
		c.emitNamed(OpSetLocal, keySlot, keyTmp, st.Line)
	}
	c.emit(OpJump, loopStart, st.Line)
	c.patch(jFalse, len(c.cur().fn.Chunk.Code))
	c.emit(OpPop, 0, st.Line)
	l := c.loops[len(c.loops)-1]
	for _, b := range l.breaks {
		c.patch(b, len(c.cur().fn.Chunk.Code))
	}
	c.loops = c.loops[:len(c.loops)-1]
	c.endScope(st.Line)
	return nil
}

func (c *compiler) compileForC(st *frontend.Stmt) error {
	c.beginScope()
	if st.Init != nil {
		// `for i = 0; ...` implicitly defines the loop var (like the
		// interpreter); plain `storeVar` would reject unknown names.
		if st.Init.Kind == frontend.StmtAssign && st.Init.Name != "" {
			if _, isLocal, enclosing := c.resolve(st.Init.Name); !isLocal && !enclosing {
				if _, ok := c.gindex[st.Init.Name]; !ok {
					op := st.Init.Op
					if op == "" || op == "=" {
						if err := c.compileExpr(st.Init.Expr); err != nil {
							return err
						}
						c.defineLocal(st.Init.Name)
					} else {
						if err := c.compileStmt(st.Init); err != nil {
							return err
						}
					}
				} else {
					if err := c.compileStmt(st.Init); err != nil {
						return err
					}
				}
			} else {
				if err := c.compileStmt(st.Init); err != nil {
					return err
				}
			}
		} else if err := c.compileStmt(st.Init); err != nil {
			return err
		}
	}
	loopStart := len(c.cur().fn.Chunk.Code)
	// cond (empty = true)
	if st.Expr != nil {
		if err := c.compileExpr(st.Expr); err != nil {
			return err
		}
	} else {
		c.emit(OpConst, c.addConst(Const{Kind: CKBool, Bool: true}), st.Line)
	}
	jFalse := c.emit(OpJumpIfFalse, -1, st.Line)
	c.emit(OpPop, 0, st.Line)
	// continue target (post) unknown until after body: use placeholder list
	c.loops = append(c.loops, loopCtx{continueAt: -1, savedDepth: c.cur().depth, savedNLocals: len(c.cur().locals), tryDepth: c.tryDepth})
	if err := c.compileStmt(st.Body); err != nil {
		return err
	}
	postPos := len(c.cur().fn.Chunk.Code)
	for _, cp := range c.loops[len(c.loops)-1].continues {
		c.patch(cp, postPos)
	}
	c.loops[len(c.loops)-1].continueAt = postPos
	if st.Post != nil {
		if err := c.compileStmt(st.Post); err != nil {
			return err
		}
	}
	c.emit(OpJump, loopStart, st.Line)
	c.patch(jFalse, len(c.cur().fn.Chunk.Code))
	c.emit(OpPop, 0, st.Line)
	l := c.loops[len(c.loops)-1]
	for _, b := range l.breaks {
		c.patch(b, len(c.cur().fn.Chunk.Code))
	}
	c.loops = c.loops[:len(c.loops)-1]
	c.endScope(st.Line)
	return nil
}

func (c *compiler) compileFuncDef(name string, params []string, body *frontend.Stmt, line int) error {
	return c.compileFuncDefTyped(name, params, nil, "", body, line)
}

func (c *compiler) compileFuncDefTyped(name string, params, paramTypes []string, returnType string, body *frontend.Stmt, line int) error {
	fn := &Func{Name: name, Params: append([]string{}, params...)}
	c.funcs = append(c.funcs, fn)
	idx := len(c.funcs) - 1
	// compile body in new frame
	c.frames = append(c.frames, &funcCtx{fn: fn, returnType: returnType})
	for _, p := range params {
		c.defineLocal(p)
	}
	// v0.2 typed params: runtime nilable check on entry (nil passes).
	for i, p := range params {
		var t string
		if i < len(paramTypes) {
			t = paramTypes[i]
		}
		if t == "" || t == "any" {
			continue
		}
		slot, _, _ := c.resolve(p)
		c.emitNamed(OpGetLocal, slot, p, line)
		c.emitNamed(OpCheckType, 0, t, line)
		c.emit(OpPop, 0, line)
	}
	if body.Kind != frontend.StmtBlock {
		if err := c.compileStmt(body); err != nil {
			return err
		}
	} else {
		for _, s := range body.List {
			if err := c.compileStmt(s); err != nil {
				return err
			}
		}
	}
	// implicit return nil
	c.emit(OpConst, c.addConst(Const{Kind: CKNil}), line)
	c.emit(OpReturn, 0, line)
	c.frames = c.frames[:len(c.frames)-1]
	// push func value in enclosing frame
	c.emit(OpConst, c.addConst(Const{Kind: CKFunc, Func: idx}), line)
	if len(c.frames) == 1 && c.frames[0].depth == 0 {
		gi := c.globalIndex(name)
		c.emitNamed(OpDefineGlobal, gi, name, line)
	} else {
		if _, isLocal, _ := c.resolveInCurrent(name); isLocal {
			return fmt.Errorf("line %d: %q already defined (compiler v0.1)", line, name)
		}
		c.defineLocal(name)
	}
	_ = idx
	return nil
}

// isTypeName extracts the type name from an `is` right operand:
// `x is int` (var) or `x is "int"` (string literal).
func isTypeName(e *frontend.Expr) (string, error) {
	switch e.Kind {
	case frontend.ExprVar:
		return e.Name, nil
	case frontend.ExprString:
		return e.StrVal, nil
	}
	return "", fmt.Errorf("`is` type must be a name or string literal (compiler v0.2)")
}

// ---------------------------------------------------------------------------
// Expressions
// ---------------------------------------------------------------------------

func (c *compiler) compileExpr(e *frontend.Expr) error {
	switch e.Kind {
	case frontend.ExprString:
		c.emit(OpConst, c.addConst(Const{Kind: CKString, Str: e.StrVal}), 0)
	case frontend.ExprInt:
		c.emit(OpConst, c.addConst(Const{Kind: CKInt, Int: e.IntVal}), 0)
	case frontend.ExprFloat:
		c.emit(OpConst, c.addConst(Const{Kind: CKFloat, Float: e.FloatVal}), 0)
	case frontend.ExprBool:
		c.emit(OpConst, c.addConst(Const{Kind: CKBool, Bool: e.BoolVal}), 0)
	case frontend.ExprNil:
		c.emit(OpConst, c.addConst(Const{Kind: CKNil}), 0)
	case frontend.ExprVar:
		if err := c.loadVar(e.Name, 0); err != nil {
			return err
		}
	case frontend.ExprAdd, frontend.ExprSub, frontend.ExprMul, frontend.ExprDiv,
		frontend.ExprMod, frontend.ExprPow, frontend.ExprEq, frontend.ExprNe,
		frontend.ExprLt, frontend.ExprLe, frontend.ExprGt, frontend.ExprGe,
		frontend.ExprIn:
		if err := c.compileExpr(e.Left); err != nil {
			return err
		}
		if err := c.compileExpr(e.Right); err != nil {
			return err
		}
		switch e.Kind {
		case frontend.ExprAdd:
			c.emit(OpAdd, 0, 0)
		case frontend.ExprSub:
			c.emit(OpSub, 0, 0)
		case frontend.ExprMul:
			c.emit(OpMul, 0, 0)
		case frontend.ExprDiv:
			c.emit(OpDiv, 0, 0)
		case frontend.ExprMod:
			c.emit(OpMod, 0, 0)
		case frontend.ExprPow:
			c.emit(OpPow, 0, 0)
		case frontend.ExprEq:
			c.emit(OpEq, 0, 0)
		case frontend.ExprNe:
			c.emit(OpNe, 0, 0)
		case frontend.ExprLt:
			c.emit(OpLt, 0, 0)
		case frontend.ExprLe:
			c.emit(OpLe, 0, 0)
		case frontend.ExprGt:
			c.emit(OpGt, 0, 0)
		case frontend.ExprGe:
			c.emit(OpGe, 0, 0)
		case frontend.ExprIn:
			c.emit(OpIn, 0, 0)
		}
	case frontend.ExprAnd:
		if err := c.compileExpr(e.Left); err != nil {
			return err
		}
		jEnd := c.emit(OpJumpIfFalse, -1, 0)
		c.emit(OpPop, 0, 0)
		if err := c.compileExpr(e.Right); err != nil {
			return err
		}
		c.patch(jEnd, len(c.cur().fn.Chunk.Code))
	case frontend.ExprOr:
		if err := c.compileExpr(e.Left); err != nil {
			return err
		}
		jEnd := c.emit(OpJumpIfTrue, -1, 0)
		c.emit(OpPop, 0, 0)
		if err := c.compileExpr(e.Right); err != nil {
			return err
		}
		c.patch(jEnd, len(c.cur().fn.Chunk.Code))
	case frontend.ExprNot:
		if err := c.compileExpr(e.Right); err != nil {
			return err
		}
		c.emit(OpNot, 0, 0)
	case frontend.ExprNeg:
		if err := c.compileExpr(e.Right); err != nil {
			return err
		}
		c.emit(OpNeg, 0, 0)
	case frontend.ExprCall:
		if err := c.compileExpr(e.Callee); err != nil {
			return err
		}
		for _, a := range e.Args {
			if err := c.compileExpr(a); err != nil {
				return err
			}
		}
		c.emit(OpCall, len(e.Args), 0)
	case frontend.ExprIndex:
		if err := c.compileExpr(e.Left); err != nil {
			return err
		}
		if err := c.compileExpr(e.Right); err != nil {
			return err
		}
		if e.Safe {
			c.emit(OpSafeIndex, 0, 0)
		} else {
			c.emit(OpIndex, 0, 0)
		}
	case frontend.ExprIs:
		if err := c.compileExpr(e.Left); err != nil {
			return err
		}
		typeName, err := isTypeName(e.Right)
		if err != nil {
			return err
		}
		c.emitNamed(OpIs, 0, typeName, 0)
	case frontend.ExprCoalesce:
		if err := c.compileExpr(e.Left); err != nil {
			return err
		}
		// short-circuit nil-only: if left != nil, skip right.
		jEnd := c.emit(OpJumpIfNotNil, -1, 0)
		c.emit(OpPop, 0, 0)
		if err := c.compileExpr(e.Right); err != nil {
			return err
		}
		c.patch(jEnd, len(c.cur().fn.Chunk.Code))
	case frontend.ExprSlice:
		if err := c.compileExpr(e.Left); err != nil {
			return err
		}
		// start: nil sentinel when omitted
		if e.SliceStart != nil {
			if err := c.compileExpr(e.SliceStart); err != nil {
				return err
			}
		} else {
			c.emit(OpConst, c.addConst(Const{Kind: CKNil}), 0)
		}
		if e.SliceEnd != nil {
			if err := c.compileExpr(e.SliceEnd); err != nil {
				return err
			}
		} else {
			c.emit(OpConst, c.addConst(Const{Kind: CKNil}), 0)
		}
		c.emit(OpSlice, 0, 0)
	case frontend.ExprArray:
		for _, el := range e.Elements {
			if err := c.compileExpr(el); err != nil {
				return err
			}
		}
		c.emit(OpArray, len(e.Elements), 0)
	case frontend.ExprMap:
		for i, k := range e.MapKeys {
			c.emit(OpConst, c.addConst(Const{Kind: CKString, Str: k}), 0)
			if err := c.compileExpr(e.MapVals[i]); err != nil {
				return err
			}
		}
		c.emit(OpMap, len(e.MapKeys), 0)
	case frontend.ExprFunc:
		// anonymous literal: compile as a hidden function, then unbind the
		// hidden name so only the value stays on the stack for the caller
		// (`let f = func...`, call args, ...) to bind.
		// v0.2: typed params/returns allowed (runtime-checked, nilable).
		name := c.hiddenName("fn")
		if e.FuncBody == nil {
			return fmt.Errorf("bad func literal (compiler v0.2)")
		}
		topLevel := len(c.frames) == 1 && c.frames[0].depth == 0
		if err := c.compileFuncDefTyped(name, e.FuncParams, e.FuncParamTypes, e.FuncReturnType, e.FuncBody, 0); err != nil {
			return err
		}
		if topLevel {
			code := &c.cur().fn.Chunk.Code
			if len(*code) == 0 || (*code)[len(*code)-1].Op != OpDefineGlobal {
				return fmt.Errorf("bad func literal (compiler v0.2)")
			}
			*code = (*code)[:len(*code)-1]
			delete(c.gindex, name)
			c.globals = c.globals[:len(c.globals)-1]
		} else {
			cur := c.cur()
			if len(cur.locals) == 0 || cur.locals[len(cur.locals)-1].name != name {
				return fmt.Errorf("bad func literal (compiler v0.2)")
			}
			cur.locals = cur.locals[:len(cur.locals)-1]
		}
	default:
		return fmt.Errorf("bad expression (compiler v0.2)")
	}
	return nil
}
