// Package backend is the ks-fusion backend v1.0:
// full tree-walk interpreter with functions, closures, arrays, maps,
// control flow, builtins and Go-like concurrency (`go` + channels).
package backend

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/kswarrior/ks-fusion/internal/frontend"
	"github.com/kswarrior/ks-fusion/internal/lib"
)

// ---------------------------------------------------------------------------
// Values
// ---------------------------------------------------------------------------

// ValKind is the runtime type tag.
type ValKind int

const (
	VNil ValKind = iota
	VBool
	VInt
	VFloat
	VString
	VArray
	VMap
	VFunc
	VBuiltin
	VChan
)

// Value is a runtime value.
type Value struct {
	Kind    ValKind
	Bool    bool
	Int     int
	Float   float64
	Str     string
	Arr     *ArrayObj
	Map     *MapObj
	Func    *FuncObj
	Builtin *BuiltinObj
	Chan    *ChanObj
}

// ArrayObj is a shared mutable array.
type ArrayObj struct {
	Mu    sync.RWMutex
	Items []Value
}

// MapObj is a shared mutable map (string keys).
type MapObj struct {
	Mu   sync.RWMutex
	Vals map[string]Value
}

// FuncObj is a user function with closure.
type FuncObj struct {
	Name    string
	Params  []string
	Body    *frontend.Stmt
	Closure *Env
}

// BuiltinObj wraps a Go builtin.
type BuiltinObj struct {
	Name string
	Fn   func(in *Interpreter, args []Value) (Value, error)
}

// ChanObj wraps a Go channel of Values.
type ChanObj struct {
	Mu     sync.Mutex
	Ch     chan Value
	Closed bool
}

func Nil() Value               { return Value{Kind: VNil} }
func BoolV(b bool) Value       { return Value{Kind: VBool, Bool: b} }
func IntV(n int) Value         { return Value{Kind: VInt, Int: n} }
func FloatV(f float64) Value   { return Value{Kind: VFloat, Float: f} }
func StrV(s string) Value      { return Value{Kind: VString, Str: s} }
func ArrV(items []Value) Value { return Value{Kind: VArray, Arr: &ArrayObj{Items: items}} }
func MapV(m map[string]Value) Value {
	if m == nil {
		m = map[string]Value{}
	}
	return Value{Kind: VMap, Map: &MapObj{Vals: m}}
}

// TypeName returns ks type name.
func TypeName(v Value) string {
	switch v.Kind {
	case VNil:
		return "nil"
	case VBool:
		return "bool"
	case VInt:
		return "int"
	case VFloat:
		return "float"
	case VString:
		return "string"
	case VArray:
		return "array"
	case VMap:
		return "map"
	case VFunc:
		return "func"
	case VBuiltin:
		return "builtin"
	case VChan:
		return "chan"
	}
	return "unknown"
}

// Display is print-friendly rendering (strings raw).
func (v Value) Display() string {
	switch v.Kind {
	case VNil:
		return "nil"
	case VBool:
		if v.Bool {
			return "true"
		}
		return "false"
	case VInt:
		return strconv.Itoa(v.Int)
	case VFloat:
		return strconv.FormatFloat(v.Float, 'f', -1, 64)
	case VString:
		return v.Str
	case VArray:
		v.Arr.Mu.RLock()
		defer v.Arr.Mu.RUnlock()
		parts := make([]string, len(v.Arr.Items))
		for i, e := range v.Arr.Items {
			parts[i] = e.Inspect()
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case VMap:
		v.Map.Mu.RLock()
		defer v.Map.Mu.RUnlock()
		keys := make([]string, 0, len(v.Map.Vals))
		for k := range v.Map.Vals {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		parts := make([]string, 0, len(keys))
		for _, k := range keys {
			parts = append(parts, strconv.Quote(k)+": "+v.Map.Vals[k].Inspect())
		}
		return "{" + strings.Join(parts, ", ") + "}"
	case VFunc:
		if v.Func.Name != "" {
			return "<func " + v.Func.Name + ">"
		}
		return "<func>"
	case VBuiltin:
		return "<builtin " + v.Builtin.Name + ">"
	case VChan:
		return "<chan>"
	}
	return "nil"
}

// Inspect renders nested values (strings quoted).
func (v Value) Inspect() string {
	if v.Kind == VString {
		return strconv.Quote(v.Str)
	}
	return v.Display()
}

// String keeps v0.1 compat (display).
func (v Value) String() string { return v.Display() }

// IsTruthy implements Python-like truthiness:
// nil, false, 0, 0.0, "", empty array/map are falsy; else truthy.
func IsTruthy(v Value) bool {
	switch v.Kind {
	case VNil:
		return false
	case VBool:
		return v.Bool
	case VInt:
		return v.Int != 0
	case VFloat:
		return v.Float != 0
	case VString:
		return v.Str != ""
	case VArray:
		v.Arr.Mu.RLock()
		defer v.Arr.Mu.RUnlock()
		return len(v.Arr.Items) != 0
	case VMap:
		v.Map.Mu.RLock()
		defer v.Map.Mu.RUnlock()
		return len(v.Map.Vals) != 0
	default:
		return true
	}
}

func deepEqual(a, b Value) bool {
	if a.Kind != b.Kind {
		// numeric cross-compare
		if isNum(a) && isNum(b) {
			return toFloat(a) == toFloat(b)
		}
		return false
	}
	switch a.Kind {
	case VNil:
		return true
	case VBool:
		return a.Bool == b.Bool
	case VInt:
		return a.Int == b.Int
	case VFloat:
		return a.Float == b.Float
	case VString:
		return a.Str == b.Str
	case VArray:
		a.Arr.Mu.RLock()
		b.Arr.Mu.RLock()
		defer a.Arr.Mu.RUnlock()
		defer b.Arr.Mu.RUnlock()
		if len(a.Arr.Items) != len(b.Arr.Items) {
			return false
		}
		for i := range a.Arr.Items {
			if !deepEqual(a.Arr.Items[i], b.Arr.Items[i]) {
				return false
			}
		}
		return true
	case VMap:
		a.Map.Mu.RLock()
		b.Map.Mu.RLock()
		defer a.Map.Mu.RUnlock()
		defer b.Map.Mu.RUnlock()
		if len(a.Map.Vals) != len(b.Map.Vals) {
			return false
		}
		for k, av := range a.Map.Vals {
			bv, ok := b.Map.Vals[k]
			if !ok || !deepEqual(av, bv) {
				return false
			}
		}
		return true
	case VFunc:
		return a.Func == b.Func
	case VBuiltin:
		return a.Builtin == b.Builtin
	case VChan:
		return a.Chan == b.Chan
	}
	return false
}

func isNum(v Value) bool { return v.Kind == VInt || v.Kind == VFloat }
func toFloat(v Value) float64 {
	if v.Kind == VFloat {
		return v.Float
	}
	return float64(v.Int)
}

// ---------------------------------------------------------------------------
// Environments (lexical scopes, goroutine-safe via interpreter lock)
// ---------------------------------------------------------------------------

// Env is a lexical scope.
type Env struct {
	Parent *Env
	Vars   map[string]Value
}

func newEnv(parent *Env) *Env { return &Env{Parent: parent, Vars: map[string]Value{}} }

// ---------------------------------------------------------------------------
// Interpreter
// ---------------------------------------------------------------------------

// Interpreter holds global state. Safe for concurrent `go` use.
type Interpreter struct {
	mu       sync.RWMutex
	globals  *Env
	wg       sync.WaitGroup
	merr     sync.Mutex
	err      error
	outMu    sync.Mutex
	baseDir  string
	impMu    sync.Mutex
	imported map[string]bool
}

func New() *Interpreter {
	in := &Interpreter{globals: newEnv(nil), imported: map[string]bool{}}
	in.defineBuiltins(in.globals)
	return in
}

// Run executes a parsed program and waits for `go` statements.
func Run(p *frontend.Program) error {
	return RunWithDir(p, "")
}

// RunWithDir executes a program with imports resolved relative to dir
// (typically the app dir). If dir is "", it defaults to the program's dir.
func RunWithDir(p *frontend.Program, dir string) error {
	in := New()
	if dir != "" {
		in.baseDir = dir
	} else if p.Path != "" && p.Path != "<expr>" && p.Path != "test.ks" {
		in.baseDir = filepath.Dir(p.Path)
	}
	if err := in.ExecProgram(p); err != nil {
		return err
	}
	in.wg.Wait()
	return in.fail()
}

// ExecProgram runs program statements in globals (exported for imports/embedding).
func (in *Interpreter) ExecProgram(p *frontend.Program) error {
	for _, st := range p.Statements {
		if err := in.execStmt(in.globals, st); err != nil {
			if isControl(err) {
				return fmt.Errorf("line %d: %s outside function/loop", st.Line, controlName(err))
			}
			return withLine(st.Line, err)
		}
		if f := in.fail(); f != nil {
			return f
		}
	}
	return nil
}

func (in *Interpreter) fail() error {
	in.merr.Lock()
	defer in.merr.Unlock()
	return in.err
}

func (in *Interpreter) setErr(err error) {
	if err == nil {
		return
	}
	in.merr.Lock()
	defer in.merr.Unlock()
	if in.err == nil {
		in.err = err
	}
}

// control signals
type ctrlKind int

const (
	ctrlReturn ctrlKind = iota
	ctrlBreak
	ctrlContinue
)

type ctrlError struct {
	kind ctrlKind
	val  Value
}

func (e *ctrlError) Error() string {
	switch e.kind {
	case ctrlReturn:
		return "return"
	case ctrlBreak:
		return "break"
	default:
		return "continue"
	}
}

var (
	errBreak    = &ctrlError{kind: ctrlBreak}
	errContinue = &ctrlError{kind: ctrlContinue}
)

func isControl(err error) bool {
	_, ok := err.(*ctrlError)
	return ok
}
func controlName(err error) string {
	if c, ok := err.(*ctrlError); ok {
		switch c.kind {
		case ctrlReturn:
			return "return"
		case ctrlBreak:
			return "break"
		default:
			return "continue"
		}
	}
	return "control"
}

func withLine(line int, err error) error {
	if err == nil {
		return nil
	}
	if _, ok := err.(*ctrlError); ok {
		return err
	}
	msg := err.Error()
	// avoid double "line N:"
	if strings.Contains(msg, "line ") {
		return err
	}
	if line > 0 {
		return fmt.Errorf("line %d: %w", line, err)
	}
	return err
}

// env helpers (locked)
func (in *Interpreter) lookup(env *Env, name string) (Value, bool) {
	in.mu.RLock()
	defer in.mu.RUnlock()
	for e := env; e != nil; e = e.Parent {
		if v, ok := e.Vars[name]; ok {
			return v, true
		}
	}
	return Nil(), false
}

func (in *Interpreter) define(env *Env, name string, v Value) {
	in.mu.Lock()
	defer in.mu.Unlock()
	env.Vars[name] = v
}

func (in *Interpreter) assign(env *Env, name string, v Value) bool {
	in.mu.Lock()
	defer in.mu.Unlock()
	for e := env; e != nil; e = e.Parent {
		if _, ok := e.Vars[name]; ok {
			e.Vars[name] = v
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Statement execution
// ---------------------------------------------------------------------------

func (in *Interpreter) execStmt(env *Env, st *frontend.Stmt) error {
	if in.fail() != nil {
		return in.fail()
	}
	switch st.Kind {
	case frontend.StmtLet:
		v, err := in.eval(env, st.Expr)
		if err != nil {
			return err
		}
		in.define(env, st.Name, v)
		return nil
	case frontend.StmtAssign:
		return in.execAssign(env, st)
	case frontend.StmtPrint:
		args := st.Exprs
		if len(args) == 0 && st.Expr != nil {
			args = []*frontend.Expr{st.Expr}
		}
		parts := make([]string, 0, len(args))
		for _, a := range args {
			v, err := in.eval(env, a)
			if err != nil {
				return err
			}
			parts = append(parts, v.Display())
		}
		in.outMu.Lock()
		fmt.Println(strings.Join(parts, " "))
		in.outMu.Unlock()
		return nil
	case frontend.StmtSleep:
		var e *frontend.Expr
		if st.Expr != nil {
			e = st.Expr
		} else {
			// compat fallback
			time.Sleep(time.Duration(st.SleepMs) * time.Millisecond)
			return nil
		}
		v, err := in.eval(env, e)
		if err != nil {
			return err
		}
		ms, err := toMillis(v)
		if err != nil {
			return err
		}
		time.Sleep(time.Duration(ms) * time.Millisecond)
		return nil
	case frontend.StmtGo:
		inner := st.Inner
		child := newEnv(env)
		in.wg.Add(1)
		go func() {
			defer in.wg.Done()
			if err := in.execStmt(child, inner); err != nil {
				if !isControl(err) {
					in.setErr(withLine(inner.Line, err))
				} else {
					in.setErr(withLine(inner.Line, fmt.Errorf("%s outside function/loop", controlName(err))))
				}
			}
		}()
		return nil
	case frontend.StmtBlock:
		child := newEnv(env)
		for _, s := range st.List {
			if err := in.execStmt(child, s); err != nil {
				return err
			}
		}
		return nil
	case frontend.StmtIf:
		c, err := in.eval(env, st.Expr)
		if err != nil {
			return err
		}
		if IsTruthy(c) {
			return in.execStmt(newEnv(env), st.Then)
		}
		if st.Else != nil {
			// else-if is a StmtIf: exec in same env; else-block: new scope
			if st.Else.Kind == frontend.StmtIf {
				return in.execStmt(env, st.Else)
			}
			return in.execStmt(newEnv(env), st.Else)
		}
		return nil
	case frontend.StmtWhile:
		child := newEnv(env)
		for {
			c, err := in.eval(child, st.Expr)
			if err != nil {
				return err
			}
			if !IsTruthy(c) {
				return nil
			}
			err = in.execStmt(newEnv(child), st.Body)
			if err != nil {
				if ce, ok := err.(*ctrlError); ok {
					switch ce.kind {
					case ctrlBreak:
						return nil
					case ctrlContinue:
						continue
					default:
						return err
					}
				}
				return err
			}
		}
	case frontend.StmtForIn:
		return in.execForIn(env, st)
	case frontend.StmtForC:
		return in.execForC(env, st)
	case frontend.StmtFunc:
		fn := &FuncObj{Name: st.Name, Params: append([]string{}, st.Names...), Body: st.Body, Closure: env}
		in.define(env, st.Name, Value{Kind: VFunc, Func: fn})
		return nil
	case frontend.StmtReturn:
		v, err := in.eval(env, st.Expr)
		if err != nil {
			return err
		}
		return &ctrlError{kind: ctrlReturn, val: v}
	case frontend.StmtBreak:
		return &ctrlError{kind: ctrlBreak}
	case frontend.StmtContinue:
		return &ctrlError{kind: ctrlContinue}
	case frontend.StmtExpr:
		_, err := in.eval(env, st.Expr)
		return err
	case frontend.StmtImport:
		return in.execImport(env, st.StrVal)
	default:
		return fmt.Errorf("unknown statement")
	}
}

func assignRHS(st *frontend.Stmt) *frontend.Expr {
	if st.Name != "" {
		return st.Expr
	}
	if len(st.Exprs) > 0 {
		return st.Exprs[0]
	}
	return nil
}

func (in *Interpreter) execAssign(env *Env, st *frontend.Stmt) error {
	// plain variable?
	if st.Name != "" {
		v, err := in.eval(env, assignRHS(st))
		if err != nil {
			return err
		}
		if st.Op != "" && st.Op != "=" {
			old, ok := in.lookup(env, st.Name)
			if !ok {
				return fmt.Errorf("unknown variable %q", st.Name)
			}
			v, err = applyAssignOp(old, st.Op, v)
			if err != nil {
				return err
			}
		}
		if st.Op == "" || st.Op == "=" {
			if !in.assign(env, st.Name, v) {
				return fmt.Errorf("unknown variable %q (try `let %s = ...` first)", st.Name, st.Name)
			}
			return nil
		}
		if !in.assign(env, st.Name, v) {
			return fmt.Errorf("unknown variable %q", st.Name)
		}
		return nil
	}
	// indexed target: Expr is target (Index), Exprs[0] is value
	if st.Expr == nil || len(st.Exprs) == 0 {
		return fmt.Errorf("bad assignment")
	}
	val, err := in.eval(env, st.Exprs[0])
	if err != nil {
		return err
	}
	return in.assignTarget(env, st.Expr, val, st.Op)
}

func (in *Interpreter) assignTarget(env *Env, target *frontend.Expr, val Value, op string) error {
	if target.Kind != frontend.ExprIndex {
		return fmt.Errorf("bad assignment target")
	}
	obj, err := in.eval(env, target.Left)
	if err != nil {
		return err
	}
	idx, err := in.eval(env, target.Right)
	if err != nil {
		return err
	}
	switch obj.Kind {
	case VArray:
		if idx.Kind != VInt {
			return fmt.Errorf("array index must be int, got %s", TypeName(idx))
		}
		obj.Arr.Mu.Lock()
		defer obj.Arr.Mu.Unlock()
		if idx.Int < 0 || idx.Int >= len(obj.Arr.Items) {
			return fmt.Errorf("index %d out of range (len %d)", idx.Int, len(obj.Arr.Items))
		}
		if op == "" || op == "=" {
			obj.Arr.Items[idx.Int] = val
			return nil
		}
		newV, err := applyAssignOp(obj.Arr.Items[idx.Int], op, val)
		if err != nil {
			return err
		}
		obj.Arr.Items[idx.Int] = newV
		return nil
	case VMap:
		if idx.Kind != VString {
			return fmt.Errorf("map key must be string, got %s", TypeName(idx))
		}
		obj.Map.Mu.Lock()
		defer obj.Map.Mu.Unlock()
		if op == "" || op == "=" {
			obj.Map.Vals[idx.Str] = val
			return nil
		}
		old, ok := obj.Map.Vals[idx.Str]
		if !ok {
			return fmt.Errorf("unknown key %q", idx.Str)
		}
		newV, err := applyAssignOp(old, op, val)
		if err != nil {
			return err
		}
		obj.Map.Vals[idx.Str] = newV
		return nil
	default:
		return fmt.Errorf("cannot index %s", TypeName(obj))
	}
}

func applyAssignOp(old Value, op string, rhs Value) (Value, error) {
	switch op {
	case "+=":
		return addValues(old, rhs)
	case "-=":
		return subValues(old, rhs)
	case "*=":
		return mulValues(old, rhs)
	case "/=":
		return divValues(old, rhs)
	case "%=":
		return modValues(old, rhs)
	}
	return Nil(), fmt.Errorf("bad assign op %q", op)
}

func (in *Interpreter) execForIn(env *Env, st *frontend.Stmt) error {
	iter, err := in.eval(env, st.Expr)
	if err != nil {
		return err
	}
	loopEnv := newEnv(env)
	// Per-iteration env for loop vars (Go 1.22 semantics), so `go`
	// closures capture the current iteration's values.
	runBody := func(iterEnv *Env) error {
		err := in.execStmt(newEnv(iterEnv), st.Body)
		if err != nil {
			if ce, ok := err.(*ctrlError); ok {
				switch ce.kind {
				case ctrlBreak:
					return errBreak
				case ctrlContinue:
					return errContinue
				default:
					return err
				}
			}
			return err
		}
		return nil
	}
	two := len(st.Names) == 2
	switch iter.Kind {
	case VArray:
		iter.Arr.Mu.RLock()
		items := make([]Value, len(iter.Arr.Items))
		copy(items, iter.Arr.Items)
		iter.Arr.Mu.RUnlock()
		for i, item := range items {
			iterEnv := newEnv(loopEnv)
			if two {
				in.define(iterEnv, st.Names[0], IntV(i))
				in.define(iterEnv, st.Names[1], item)
			} else {
				in.define(iterEnv, st.Names[0], item)
			}
			if err := runBody(iterEnv); err != nil {
				if err == errBreak {
					return nil
				}
				if err == errContinue {
					continue
				}
				return err
			}
		}
		return nil
	case VMap:
		iter.Map.Mu.RLock()
		keys := make([]string, 0, len(iter.Map.Vals))
		for k := range iter.Map.Vals {
			keys = append(keys, k)
		}
		vals := make(map[string]Value, len(keys))
		for _, k := range keys {
			vals[k] = iter.Map.Vals[k]
		}
		iter.Map.Mu.RUnlock()
		sort.Strings(keys)
		for _, k := range keys {
			iterEnv := newEnv(loopEnv)
			if two {
				in.define(iterEnv, st.Names[0], StrV(k))
				in.define(iterEnv, st.Names[1], vals[k])
			} else {
				in.define(iterEnv, st.Names[0], StrV(k))
			}
			if err := runBody(iterEnv); err != nil {
				if err == errBreak {
					return nil
				}
				if err == errContinue {
					continue
				}
				return err
			}
		}
		return nil
	case VString:
		chars := []rune(iter.Str)
		for i, r := range chars {
			iterEnv := newEnv(loopEnv)
			if two {
				in.define(iterEnv, st.Names[0], IntV(i))
				in.define(iterEnv, st.Names[1], StrV(string(r)))
			} else {
				in.define(iterEnv, st.Names[0], StrV(string(r)))
			}
			if err := runBody(iterEnv); err != nil {
				if err == errBreak {
					return nil
				}
				if err == errContinue {
					continue
				}
				return err
			}
		}
		return nil
	default:
		return fmt.Errorf("cannot iterate %s (try array, map or string)", TypeName(iter))
	}
}

func (in *Interpreter) execForC(env *Env, st *frontend.Stmt) error {
	loopEnv := newEnv(env)
	if st.Init != nil {
		// implicit define for `for i = 0; ...`
		if st.Init.Kind == frontend.StmtAssign && st.Init.Name != "" {
			v, err := in.eval(loopEnv, assignRHS(st.Init))
			if err != nil {
				return err
			}
			if _, ok := in.lookup(loopEnv, st.Init.Name); !ok {
				in.define(loopEnv, st.Init.Name, v)
			} else {
				if err := in.execStmt(loopEnv, st.Init); err != nil {
					return err
				}
			}
		} else if err := in.execStmt(loopEnv, st.Init); err != nil {
			return err
		}
	}
	for {
		cond := BoolV(true)
		if st.Expr != nil {
			c, err := in.eval(loopEnv, st.Expr)
			if err != nil {
				return err
			}
			cond = c
		}
		if !IsTruthy(cond) {
			return nil
		}
		err := in.execStmt(newEnv(loopEnv), st.Body)
		if err != nil {
			if ce, ok := err.(*ctrlError); ok {
				switch ce.kind {
				case ctrlBreak:
					return nil
				case ctrlContinue:
					// still run post
				default:
					return err
				}
			} else {
				return err
			}
		}
		if st.Post != nil {
			// post may assign to loop var (exists) - normal exec
			if st.Post.Kind == frontend.StmtAssign && st.Post.Name != "" {
				if _, ok := in.lookup(loopEnv, st.Post.Name); !ok {
					// define (defensive)
					v, err := in.eval(loopEnv, assignRHS(st.Post))
					if err != nil {
						return err
					}
					in.define(loopEnv, st.Post.Name, v)
				} else if err := in.execStmt(loopEnv, st.Post); err != nil {
					return err
				}
			} else if err := in.execStmt(loopEnv, st.Post); err != nil {
				if ce, ok := err.(*ctrlError); ok {
					if ce.kind == ctrlReturn {
						return err
					}
					return fmt.Errorf("%s outside loop", controlName(err))
				}
				return err
			}
		}
	}
}

func (in *Interpreter) execImport(env *Env, path string) error {
	if path == "" {
		return fmt.Errorf("bad import: empty path")
	}
	// Library import: `import "name"` (no .ks suffix) resolves a built
	// .kslib bundle, e.g. test-releases/hello-lib-0.1.0.kslib.
	// Make with `fusion new --lib` + `fusion build --release`.
	if !strings.HasSuffix(path, ".ks") && !strings.HasSuffix(path, lib.Ext) {
		return in.execLibImport(env, path)
	}
	if strings.HasSuffix(path, lib.Ext) {
		return in.execBundleFile(env, path)
	}
	// Resolve relative to baseDir (app dir), its parent (for legacy
	// backend/-relative layouts), and CWD. Imports are app-root
	// relative, e.g. `import "shared/util.ks"`.
	candidates := []string{path}
	if in.baseDir != "" && !filepath.IsAbs(path) {
		candidates = append([]string{filepath.Join(in.baseDir, path)}, candidates...)
		parent := filepath.Dir(in.baseDir)
		if parent != "" && parent != in.baseDir {
			candidates = append(candidates, filepath.Join(parent, path))
		}
	}
	var data []byte
	var full string
	var err error
	for _, c := range candidates {
		var e error
		data, e = os.ReadFile(c)
		if e == nil {
			full = c
			err = nil
			break
		}
		err = e
	}
	if err != nil {
		return fmt.Errorf("import %q failed: %v", path, err)
	}
	abs := full
	if !filepath.IsAbs(abs) {
		if a, e := filepath.Abs(abs); e == nil {
			abs = a
		}
	}
	in.impMu.Lock()
	if in.imported[abs] {
		in.impMu.Unlock()
		return nil
	}
	in.imported[abs] = true
	in.impMu.Unlock()
	return in.execSource(env, string(data), full)
}

// libSearchDirs lists where `import "name"` looks for .kslib bundles:
// beside the app (test-releases/ like `cargo build --release`,
// target/ for debug builds, release/ legacy) and beside the CWD.
func (in *Interpreter) libSearchDirs() []string {
	var dirs []string
	seen := map[string]bool{}
	add := func(d string) {
		if d == "" || seen[d] {
			return
		}
		seen[d] = true
		dirs = append(dirs, d)
	}
	for _, base := range []string{in.baseDir, "."} {
		if base == "" {
			continue
		}
		add(filepath.Join(base, "test-releases"))
		add(filepath.Join(base, "target"))
		add(filepath.Join(base, "release"))
	}
	return dirs
}

func (in *Interpreter) execLibImport(env *Env, name string) error {
	if strings.ContainsAny(name, "/\\") || strings.Contains(name, ".") {
		return fmt.Errorf("import %q failed: library names are bare words like \"hello-lib\" (built with `fusion build --release`)", name)
	}
	found, err := lib.Find(name, in.libSearchDirs())
	if err != nil {
		return fmt.Errorf("import %q failed: %v (build the lib with `fusion build --release <libdir>`)", name, err)
	}
	return in.execBundleFile(env, found)
}

// execBundleFile loads a .kslib bundle file (by path) and executes its
// sources once, in bundle order, in the importing scope.
func (in *Interpreter) execBundleFile(env *Env, path string) error {
	candidates := []string{path}
	if in.baseDir != "" && !filepath.IsAbs(path) {
		candidates = append([]string{filepath.Join(in.baseDir, path)}, candidates...)
		parent := filepath.Dir(in.baseDir)
		if parent != "" && parent != in.baseDir {
			candidates = append(candidates, filepath.Join(parent, path))
		}
	}
	var full string
	var lastErr error
	for _, c := range candidates {
		st, statErr := os.Stat(c)
		if statErr == nil && !st.IsDir() {
			full = c
			lastErr = nil
			break
		}
		if statErr != nil {
			lastErr = statErr
		} else {
			lastErr = fmt.Errorf("not a file")
		}
	}
	if full == "" {
		// Maybe a bare "name.kslib" resolvable via search dirs.
		if found, err := lib.Find(strings.TrimSuffix(filepath.Base(path), lib.Ext), in.libSearchDirs()); err == nil {
			full = found
		} else if lastErr != nil {
			return fmt.Errorf("import %q failed: %v", path, lastErr)
		} else {
			return fmt.Errorf("import %q failed", path)
		}
	}
	abs := full
	if !filepath.IsAbs(abs) {
		if a, e := filepath.Abs(abs); e == nil {
			abs = a
		}
	}
	in.impMu.Lock()
	if in.imported[abs] {
		in.impMu.Unlock()
		return nil
	}
	in.imported[abs] = true
	in.impMu.Unlock()
	b, err := lib.Load(full)
	if err != nil {
		return err
	}
	for _, f := range b.Files {
		prog, err := frontend.ParseSource(f.Source, full+"!/"+f.Path)
		if err != nil {
			return err
		}
		for _, st := range prog.Statements {
			if err := in.execStmt(env, st); err != nil {
				return withLine(st.Line, err)
			}
		}
	}
	return nil
}

func (in *Interpreter) execSource(env *Env, src, path string) error {
	prog, err := frontend.ParseSource(src, path)
	if err != nil {
		return err
	}
	for _, st := range prog.Statements {
		if err := in.execStmt(env, st); err != nil {
			return withLine(st.Line, err)
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Expression evaluation
// ---------------------------------------------------------------------------

func (in *Interpreter) eval(env *Env, e *frontend.Expr) (Value, error) {
	switch e.Kind {
	case frontend.ExprString:
		return StrV(e.StrVal), nil
	case frontend.ExprInt:
		return IntV(e.IntVal), nil
	case frontend.ExprFloat:
		return FloatV(e.FloatVal), nil
	case frontend.ExprBool:
		return BoolV(e.BoolVal), nil
	case frontend.ExprNil:
		return Nil(), nil
	case frontend.ExprVar:
		v, ok := in.lookup(env, e.Name)
		if !ok {
			if b, ok := builtinByName(e.Name); ok {
				return Value{Kind: VBuiltin, Builtin: b}, nil
			}
			return Nil(), fmt.Errorf("unknown variable %q", e.Name)
		}
		return v, nil
	case frontend.ExprAdd:
		l, err := in.eval(env, e.Left)
		if err != nil {
			return Nil(), err
		}
		r, err := in.eval(env, e.Right)
		if err != nil {
			return Nil(), err
		}
		return addValues(l, r)
	case frontend.ExprSub:
		l, err := in.eval(env, e.Left)
		if err != nil {
			return Nil(), err
		}
		r, err := in.eval(env, e.Right)
		if err != nil {
			return Nil(), err
		}
		return subValues(l, r)
	case frontend.ExprMul:
		l, err := in.eval(env, e.Left)
		if err != nil {
			return Nil(), err
		}
		r, err := in.eval(env, e.Right)
		if err != nil {
			return Nil(), err
		}
		return mulValues(l, r)
	case frontend.ExprDiv:
		l, err := in.eval(env, e.Left)
		if err != nil {
			return Nil(), err
		}
		r, err := in.eval(env, e.Right)
		if err != nil {
			return Nil(), err
		}
		return divValues(l, r)
	case frontend.ExprMod:
		l, err := in.eval(env, e.Left)
		if err != nil {
			return Nil(), err
		}
		r, err := in.eval(env, e.Right)
		if err != nil {
			return Nil(), err
		}
		return modValues(l, r)
	case frontend.ExprEq:
		l, err := in.eval(env, e.Left)
		if err != nil {
			return Nil(), err
		}
		r, err := in.eval(env, e.Right)
		if err != nil {
			return Nil(), err
		}
		return BoolV(deepEqual(l, r)), nil
	case frontend.ExprNe:
		l, err := in.eval(env, e.Left)
		if err != nil {
			return Nil(), err
		}
		r, err := in.eval(env, e.Right)
		if err != nil {
			return Nil(), err
		}
		return BoolV(!deepEqual(l, r)), nil
	case frontend.ExprLt, frontend.ExprLe, frontend.ExprGt, frontend.ExprGe:
		l, err := in.eval(env, e.Left)
		if err != nil {
			return Nil(), err
		}
		r, err := in.eval(env, e.Right)
		if err != nil {
			return Nil(), err
		}
		return cmpValues(e.Kind, l, r)
	case frontend.ExprAnd:
		l, err := in.eval(env, e.Left)
		if err != nil {
			return Nil(), err
		}
		if !IsTruthy(l) {
			return l, nil
		}
		return in.eval(env, e.Right)
	case frontend.ExprOr:
		l, err := in.eval(env, e.Left)
		if err != nil {
			return Nil(), err
		}
		if IsTruthy(l) {
			return l, nil
		}
		return in.eval(env, e.Right)
	case frontend.ExprNot:
		v, err := in.eval(env, e.Right)
		if err != nil {
			return Nil(), err
		}
		return BoolV(!IsTruthy(v)), nil
	case frontend.ExprNeg:
		v, err := in.eval(env, e.Right)
		if err != nil {
			return Nil(), err
		}
		switch v.Kind {
		case VInt:
			return IntV(-v.Int), nil
		case VFloat:
			return FloatV(-v.Float), nil
		}
		return Nil(), fmt.Errorf("cannot negate %s", TypeName(v))
	case frontend.ExprCall:
		return in.evalCall(env, e)
	case frontend.ExprIndex:
		obj, err := in.eval(env, e.Left)
		if err != nil {
			return Nil(), err
		}
		idx, err := in.eval(env, e.Right)
		if err != nil {
			return Nil(), err
		}
		return indexValue(obj, idx)
	case frontend.ExprArray:
		items := make([]Value, 0, len(e.Elements))
		for _, el := range e.Elements {
			v, err := in.eval(env, el)
			if err != nil {
				return Nil(), err
			}
			items = append(items, v)
		}
		return ArrV(items), nil
	case frontend.ExprMap:
		m := map[string]Value{}
		for i, k := range e.MapKeys {
			v, err := in.eval(env, e.MapVals[i])
			if err != nil {
				return Nil(), err
			}
			m[k] = v
		}
		return MapV(m), nil
	case frontend.ExprFunc:
		fn := &FuncObj{Params: append([]string{}, e.FuncParams...), Body: e.FuncBody, Closure: env}
		return Value{Kind: VFunc, Func: fn}, nil
	}
	return Nil(), fmt.Errorf("bad expression")
}

func (in *Interpreter) evalCall(env *Env, e *frontend.Expr) (Value, error) {
	fn, err := in.eval(env, e.Callee)
	if err != nil {
		return Nil(), err
	}
	args := make([]Value, 0, len(e.Args))
	for _, a := range e.Args {
		v, err := in.eval(env, a)
		if err != nil {
			return Nil(), err
		}
		args = append(args, v)
	}
	switch fn.Kind {
	case VFunc:
		if len(args) != len(fn.Func.Params) {
			return Nil(), fmt.Errorf("function %q wants %d args, got %d", fn.Func.Name, len(fn.Func.Params), len(args))
		}
		callEnv := newEnv(fn.Func.Closure)
		for i, p := range fn.Func.Params {
			callEnv.Vars[p] = args[i]
		}
		// lock-free define (single goroutine owns callEnv); direct map write ok
		// but other goroutines may read closure parents via lookup (locked) - safe.
		err := in.execStmt(callEnv, fn.Func.Body)
		if err != nil {
			if ce, ok := err.(*ctrlError); ok && ce.kind == ctrlReturn {
				return ce.val, nil
			}
			return Nil(), err
		}
		return Nil(), nil
	case VBuiltin:
		return fn.Builtin.Fn(in, args)
	default:
		return Nil(), fmt.Errorf("cannot call %s", TypeName(fn))
	}
}

func indexValue(obj, idx Value) (Value, error) {
	switch obj.Kind {
	case VArray:
		if idx.Kind != VInt {
			return Nil(), fmt.Errorf("array index must be int, got %s", TypeName(idx))
		}
		obj.Arr.Mu.RLock()
		defer obj.Arr.Mu.RUnlock()
		if idx.Int < 0 || idx.Int >= len(obj.Arr.Items) {
			return Nil(), fmt.Errorf("index %d out of range (len %d)", idx.Int, len(obj.Arr.Items))
		}
		return obj.Arr.Items[idx.Int], nil
	case VMap:
		if idx.Kind != VString {
			return Nil(), fmt.Errorf("map key must be string, got %s", TypeName(idx))
		}
		obj.Map.Mu.RLock()
		defer obj.Map.Mu.RUnlock()
		v, ok := obj.Map.Vals[idx.Str]
		if !ok {
			return Nil(), fmt.Errorf("unknown key %q", idx.Str)
		}
		return v, nil
	case VString:
		if idx.Kind != VInt {
			return Nil(), fmt.Errorf("string index must be int, got %s", TypeName(idx))
		}
		runes := []rune(obj.Str)
		if idx.Int < 0 || idx.Int >= len(runes) {
			return Nil(), fmt.Errorf("index %d out of range (len %d)", idx.Int, len(runes))
		}
		return StrV(string(runes[idx.Int])), nil
	}
	return Nil(), fmt.Errorf("cannot index %s", TypeName(obj))
}

// arithmetic
func addValues(l, r Value) (Value, error) {
	if l.Kind == VInt && r.Kind == VInt {
		return IntV(l.Int + r.Int), nil
	}
	if isNum(l) && isNum(r) {
		return FloatV(toFloat(l) + toFloat(r)), nil
	}
	if l.Kind == VString || r.Kind == VString {
		// string concat (ints auto-converted) - v0.1 compat: "hi " + x
		return StrV(l.Display() + r.Display()), nil
	}
	if l.Kind == VArray && r.Kind == VArray {
		l.Arr.Mu.RLock()
		r.Arr.Mu.RLock()
		out := make([]Value, 0, len(l.Arr.Items)+len(r.Arr.Items))
		out = append(out, l.Arr.Items...)
		out = append(out, r.Arr.Items...)
		l.Arr.Mu.RUnlock()
		r.Arr.Mu.RUnlock()
		return ArrV(out), nil
	}
	return Nil(), fmt.Errorf("cannot add %s and %s", TypeName(l), TypeName(r))
}

func subValues(l, r Value) (Value, error) {
	if l.Kind == VInt && r.Kind == VInt {
		return IntV(l.Int - r.Int), nil
	}
	if isNum(l) && isNum(r) {
		return FloatV(toFloat(l) - toFloat(r)), nil
	}
	return Nil(), fmt.Errorf("cannot subtract %s and %s", TypeName(l), TypeName(r))
}

func mulValues(l, r Value) (Value, error) {
	if l.Kind == VInt && r.Kind == VInt {
		return IntV(l.Int * r.Int), nil
	}
	if isNum(l) && isNum(r) {
		return FloatV(toFloat(l) * toFloat(r)), nil
	}
	return Nil(), fmt.Errorf("cannot multiply %s and %s", TypeName(l), TypeName(r))
}

func divValues(l, r Value) (Value, error) {
	if !isNum(l) || !isNum(r) {
		return Nil(), fmt.Errorf("cannot divide %s and %s", TypeName(l), TypeName(r))
	}
	d := toFloat(r)
	if d == 0 {
		return Nil(), fmt.Errorf("division by zero")
	}
	return FloatV(toFloat(l) / d), nil
}

func modValues(l, r Value) (Value, error) {
	if l.Kind == VInt && r.Kind == VInt {
		if r.Int == 0 {
			return Nil(), fmt.Errorf("division by zero")
		}
		return IntV(l.Int % r.Int), nil
	}
	return Nil(), fmt.Errorf("cannot mod %s and %s (need ints)", TypeName(l), TypeName(r))
}

func cmpValues(kind frontend.ExprKind, l, r Value) (Value, error) {
	if isNum(l) && isNum(r) {
		a, b := toFloat(l), toFloat(r)
		switch kind {
		case frontend.ExprLt:
			return BoolV(a < b), nil
		case frontend.ExprLe:
			return BoolV(a <= b), nil
		case frontend.ExprGt:
			return BoolV(a > b), nil
		default:
			return BoolV(a >= b), nil
		}
	}
	if l.Kind == VString && r.Kind == VString {
		switch kind {
		case frontend.ExprLt:
			return BoolV(l.Str < r.Str), nil
		case frontend.ExprLe:
			return BoolV(l.Str <= r.Str), nil
		case frontend.ExprGt:
			return BoolV(l.Str > r.Str), nil
		default:
			return BoolV(l.Str >= r.Str), nil
		}
	}
	return Nil(), fmt.Errorf("cannot compare %s and %s", TypeName(l), TypeName(r))
}

func toMillis(v Value) (int, error) {
	switch v.Kind {
	case VInt:
		if v.Int < 0 {
			return 0, fmt.Errorf("bad sleep: want `sleep 500` (ms >= 0)")
		}
		return v.Int, nil
	case VFloat:
		if v.Float < 0 {
			return 0, fmt.Errorf("bad sleep: want `sleep 500` (ms >= 0)")
		}
		return int(v.Float), nil
	}
	return 0, fmt.Errorf("bad sleep: want int ms, got %s", TypeName(v))
}

// ---------------------------------------------------------------------------
// Builtins
// ---------------------------------------------------------------------------

func (in *Interpreter) defineBuiltins(env *Env) {
	for _, b := range allBuiltins() {
		env.Vars[b.Name] = Value{Kind: VBuiltin, Builtin: b}
	}
}

func builtinByName(name string) (*BuiltinObj, bool) {
	for _, b := range allBuiltins() {
		if b.Name == name {
			return b, true
		}
	}
	return nil, false
}

func allBuiltins() []*BuiltinObj {
	return []*BuiltinObj{
		{Name: "len", Fn: bLen},
		{Name: "str", Fn: bStr},
		{Name: "int", Fn: bInt},
		{Name: "float", Fn: bFloat},
		{Name: "type", Fn: bType},
		{Name: "range", Fn: bRange},
		{Name: "push", Fn: bPush},
		{Name: "pop", Fn: bPop},
		{Name: "keys", Fn: bKeys},
		{Name: "values", Fn: bValues},
		{Name: "has", Fn: bHas},
		{Name: "chan", Fn: bChan},
		{Name: "send", Fn: bSend},
		{Name: "recv", Fn: bRecv},
		{Name: "close", Fn: bClose},
		{Name: "sleep", Fn: bSleep},
		{Name: "assert", Fn: bAssert},
		{Name: "error", Fn: bError},
	}
}

func needArgs(name string, args []Value, min, max int) error {
	if len(args) < min || (max >= 0 && len(args) > max) {
		if min == max {
			return fmt.Errorf("%s wants %d args, got %d", name, min, len(args))
		}
		if max < 0 {
			return fmt.Errorf("%s wants at least %d args, got %d", name, min, len(args))
		}
		return fmt.Errorf("%s wants %d..%d args, got %d", name, min, max, len(args))
	}
	return nil
}

func bLen(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("len", args, 1, 1); err != nil {
		return Nil(), err
	}
	switch args[0].Kind {
	case VString:
		return IntV(utf8.RuneCountInString(args[0].Str)), nil
	case VArray:
		args[0].Arr.Mu.RLock()
		defer args[0].Arr.Mu.RUnlock()
		return IntV(len(args[0].Arr.Items)), nil
	case VMap:
		args[0].Map.Mu.RLock()
		defer args[0].Map.Mu.RUnlock()
		return IntV(len(args[0].Map.Vals)), nil
	}
	return Nil(), fmt.Errorf("len wants string/array/map, got %s", TypeName(args[0]))
}

func bStr(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("str", args, 1, 1); err != nil {
		return Nil(), err
	}
	return StrV(args[0].Display()), nil
}

func bInt(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("int", args, 1, 1); err != nil {
		return Nil(), err
	}
	a := args[0]
	switch a.Kind {
	case VInt:
		return a, nil
	case VFloat:
		return IntV(int(a.Float)), nil
	case VBool:
		if a.Bool {
			return IntV(1), nil
		}
		return IntV(0), nil
	case VString:
		s := strings.TrimSpace(a.Str)
		if n, err := strconv.Atoi(s); err == nil {
			return IntV(n), nil
		}
		if f, err := strconv.ParseFloat(s, 64); err == nil {
			return IntV(int(f)), nil
		}
		return Nil(), fmt.Errorf("int(%q) failed", a.Str)
	}
	return Nil(), fmt.Errorf("int(%s) failed", TypeName(a))
}

func bFloat(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("float", args, 1, 1); err != nil {
		return Nil(), err
	}
	a := args[0]
	switch a.Kind {
	case VFloat:
		return a, nil
	case VInt:
		return FloatV(float64(a.Int)), nil
	case VBool:
		if a.Bool {
			return FloatV(1), nil
		}
		return FloatV(0), nil
	case VString:
		s := strings.TrimSpace(a.Str)
		if f, err := strconv.ParseFloat(s, 64); err == nil {
			return FloatV(f), nil
		}
		return Nil(), fmt.Errorf("float(%q) failed", a.Str)
	}
	return Nil(), fmt.Errorf("float(%s) failed", TypeName(a))
}

func bType(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("type", args, 1, 1); err != nil {
		return Nil(), err
	}
	return StrV(TypeName(args[0])), nil
}

func bRange(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("range", args, 1, 3); err != nil {
		return Nil(), err
	}
	for _, a := range args {
		if a.Kind != VInt {
			return Nil(), fmt.Errorf("range wants ints, got %s", TypeName(a))
		}
	}
	var start, end, step int
	if len(args) == 1 {
		start, end, step = 0, args[0].Int, 1
	} else if len(args) == 2 {
		start, end, step = args[0].Int, args[1].Int, 1
	} else {
		start, end, step = args[0].Int, args[1].Int, args[2].Int
	}
	if step == 0 {
		return Nil(), fmt.Errorf("range step cannot be 0")
	}
	var out []Value
	if step > 0 {
		for i := start; i < end; i += step {
			out = append(out, IntV(i))
		}
	} else {
		for i := start; i > end; i += step {
			out = append(out, IntV(i))
		}
	}
	if out == nil {
		out = []Value{}
	}
	return ArrV(out), nil
}

func bPush(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("push", args, 2, 2); err != nil {
		return Nil(), err
	}
	if args[0].Kind != VArray {
		return Nil(), fmt.Errorf("push wants array, got %s", TypeName(args[0]))
	}
	args[0].Arr.Mu.Lock()
	args[0].Arr.Items = append(args[0].Arr.Items, args[1])
	n := len(args[0].Arr.Items)
	args[0].Arr.Mu.Unlock()
	return IntV(n), nil
}

func bPop(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("pop", args, 1, 1); err != nil {
		return Nil(), err
	}
	if args[0].Kind != VArray {
		return Nil(), fmt.Errorf("pop wants array, got %s", TypeName(args[0]))
	}
	args[0].Arr.Mu.Lock()
	defer args[0].Arr.Mu.Unlock()
	if len(args[0].Arr.Items) == 0 {
		return Nil(), fmt.Errorf("pop from empty array")
	}
	v := args[0].Arr.Items[len(args[0].Arr.Items)-1]
	args[0].Arr.Items = args[0].Arr.Items[:len(args[0].Arr.Items)-1]
	return v, nil
}

func bKeys(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("keys", args, 1, 1); err != nil {
		return Nil(), err
	}
	if args[0].Kind != VMap {
		return Nil(), fmt.Errorf("keys wants map, got %s", TypeName(args[0]))
	}
	args[0].Map.Mu.RLock()
	defer args[0].Map.Mu.RUnlock()
	keys := make([]string, 0, len(args[0].Map.Vals))
	for k := range args[0].Map.Vals {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]Value, 0, len(keys))
	for _, k := range keys {
		out = append(out, StrV(k))
	}
	return ArrV(out), nil
}

func bValues(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("values", args, 1, 1); err != nil {
		return Nil(), err
	}
	if args[0].Kind != VMap {
		return Nil(), fmt.Errorf("values wants map, got %s", TypeName(args[0]))
	}
	args[0].Map.Mu.RLock()
	defer args[0].Map.Mu.RUnlock()
	keys := make([]string, 0, len(args[0].Map.Vals))
	for k := range args[0].Map.Vals {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]Value, 0, len(keys))
	for _, k := range keys {
		out = append(out, args[0].Map.Vals[k])
	}
	return ArrV(out), nil
}

func bHas(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("has", args, 2, 2); err != nil {
		return Nil(), err
	}
	if args[0].Kind != VMap {
		return Nil(), fmt.Errorf("has wants map, got %s", TypeName(args[0]))
	}
	if args[1].Kind != VString {
		return Nil(), fmt.Errorf("has key must be string, got %s", TypeName(args[1]))
	}
	args[0].Map.Mu.RLock()
	defer args[0].Map.Mu.RUnlock()
	_, ok := args[0].Map.Vals[args[1].Str]
	return BoolV(ok), nil
}

func bChan(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("chan", args, 0, 1); err != nil {
		return Nil(), err
	}
	size := 0
	if len(args) == 1 {
		if args[0].Kind != VInt || args[0].Int < 0 {
			return Nil(), fmt.Errorf("chan size must be int >= 0")
		}
		size = args[0].Int
	}
	return Value{Kind: VChan, Chan: &ChanObj{Ch: make(chan Value, size)}}, nil
}

func bSend(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("send", args, 2, 2); err != nil {
		return Nil(), err
	}
	if args[0].Kind != VChan {
		return Nil(), fmt.Errorf("send wants chan, got %s", TypeName(args[0]))
	}
	args[0].Chan.Mu.Lock()
	ch := args[0].Chan.Ch
	closed := args[0].Chan.Closed
	args[0].Chan.Mu.Unlock()
	if closed {
		return Nil(), fmt.Errorf("send on closed channel")
	}
	// blocking send outside locks
	ch <- args[1]
	return Nil(), nil
}

func bRecv(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("recv", args, 1, 1); err != nil {
		return Nil(), err
	}
	if args[0].Kind != VChan {
		return Nil(), fmt.Errorf("recv wants chan, got %s", TypeName(args[0]))
	}
	v, ok := <-args[0].Chan.Ch
	if !ok {
		return Nil(), nil
	}
	return v, nil
}

func bClose(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("close", args, 1, 1); err != nil {
		return Nil(), err
	}
	if args[0].Kind != VChan {
		return Nil(), fmt.Errorf("close wants chan, got %s", TypeName(args[0]))
	}
	args[0].Chan.Mu.Lock()
	defer args[0].Chan.Mu.Unlock()
	if args[0].Chan.Closed {
		return Nil(), fmt.Errorf("close of closed channel")
	}
	args[0].Chan.Closed = true
	close(args[0].Chan.Ch)
	return Nil(), nil
}

func bSleep(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("sleep", args, 1, 1); err != nil {
		return Nil(), err
	}
	ms, err := toMillis(args[0])
	if err != nil {
		return Nil(), err
	}
	time.Sleep(time.Duration(ms) * time.Millisecond)
	return Nil(), nil
}

func bAssert(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("assert", args, 1, 2); err != nil {
		return Nil(), err
	}
	if !IsTruthy(args[0]) {
		if len(args) == 2 {
			return Nil(), fmt.Errorf("assert failed: %s", args[1].Display())
		}
		return Nil(), fmt.Errorf("assert failed")
	}
	return Nil(), nil
}

func bError(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("error", args, 1, 1); err != nil {
		return Nil(), err
	}
	return Nil(), fmt.Errorf("%s", args[0].Display())
}
