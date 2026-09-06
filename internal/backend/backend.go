// Package backend is the ks-fusion backend:
// full tree-walk interpreter with functions, closures, arrays, maps,
// control flow, a complete builtin standard library and Go-like
// concurrency (`go` + channels + `select`).
package backend

import (
	"bufio"
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
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
	Name       string
	Params     []string
	ParamTypes []string
	ReturnType string
	Body       *frontend.Stmt
	Closure    *Env
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
	case VFunc, VBuiltin:
		return "func"
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

// validType reports whether name is a known type: base names, unions and
// generics thereof, or a struct/enum nominal declared in this interpreter.
// Keep the package-level validType for contexts without an interpreter
// (all such calls validate base/union/generic shapes only).
func validType(name string) bool {
	return validTypeIn(name, nil)
}

func validTypeIn(name string, in *Interpreter) bool {
	// unions + generics (v2.4): int|string, array<int>, map<string,int>
	if hasTopPipe(name) {
		for _, part := range splitTopPipe(name) {
			if !validTypeIn(part, in) {
				return false
			}
		}
		return true
	}
	if base, args, ok := parseTypeGeneric(name); ok {
		switch base {
		case "array":
			return len(args) == 1 && validTypeIn(args[0], in)
		case "map":
			return len(args) == 2 && validTypeIn(args[0], in) && validTypeIn(args[1], in)
		}
		return false
	}
	switch name {
	case "nil", "bool", "int", "float", "number", "string",
		"array", "map", "func", "chan", "any", "ok", "err":
		return true
	}
	if in != nil {
		return in.hasNominalType(name)
	}
	return false
}

// hasNominalType reports whether name names a declared struct or enum.
func (in *Interpreter) hasNominalType(name string) bool {
	in.typeMu.RLock()
	defer in.typeMu.RUnlock()
	if _, ok := in.structs[name]; ok {
		return true
	}
	_, ok := in.enums[name]
	return ok
}

func hasTopPipe(s string) bool {
	depth := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '<':
			depth++
		case '>':
			depth--
		case '|':
			if depth == 0 {
				return true
			}
		}
	}
	return false
}

func splitTopPipe(s string) []string {
	var out []string
	depth := 0
	start := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '<':
			depth++
		case '>':
			depth--
		case '|':
			if depth == 0 {
				out = append(out, strings.TrimSpace(s[start:i]))
				start = i + 1
			}
		}
	}
	out = append(out, strings.TrimSpace(s[start:]))
	return out
}

func parseTypeGeneric(s string) (string, []string, bool) {
	idx := -1
	for i := 0; i < len(s); i++ {
		if s[i] == '<' {
			idx = i
			break
		}
	}
	if idx < 0 || !strings.HasSuffix(s, ">") {
		return "", nil, false
	}
	base := strings.TrimSpace(s[:idx])
	inner := s[idx+1 : len(s)-1]
	var args []string
	depth := 0
	start := 0
	for i := 0; i < len(inner); i++ {
		switch inner[i] {
		case '<':
			depth++
		case '>':
			depth--
		case ',':
			if depth == 0 {
				args = append(args, strings.TrimSpace(inner[start:i]))
				start = i + 1
			}
		}
	}
	args = append(args, strings.TrimSpace(inner[start:]))
	return base, args, true
}

func isOkValue(v Value) bool {
	if v.Kind != VMap {
		return false
	}
	v.Map.Mu.RLock()
	defer v.Map.Mu.RUnlock()
	_, hasOk := v.Map.Vals["ok"]
	_, hasErr := v.Map.Vals["err"]
	return hasOk && !hasErr
}

func isErrValue(v Value) bool {
	if v.Kind != VMap {
		return false
	}
	v.Map.Mu.RLock()
	defer v.Map.Mu.RUnlock()
	_, hasErr := v.Map.Vals["err"]
	return hasErr
}

func (in *Interpreter) matchesTypeStrict(v Value, typ string) bool {
	// unions (v2.4)
	if hasTopPipe(typ) {
		for _, part := range splitTopPipe(typ) {
			if in.matchesTypeStrict(v, part) {
				return true
			}
		}
		return false
	}
	// generics (v2.4): array<T>, map<string,T>
	if base, args, ok := parseTypeGeneric(typ); ok {
		switch base {
		case "array":
			if v.Kind != VArray || len(args) != 1 {
				return false
			}
			v.Arr.Mu.RLock()
			defer v.Arr.Mu.RUnlock()
			for _, e := range v.Arr.Items {
				if e.Kind == VNil {
					continue // nullable elements pass
				}
				if !in.matchesTypeStrict(e, args[0]) {
					return false
				}
			}
			return true
		case "map":
			if v.Kind != VMap || len(args) != 2 {
				return false
			}
			v.Map.Mu.RLock()
			defer v.Map.Mu.RUnlock()
			for _, e := range v.Map.Vals {
				if e.Kind == VNil {
					continue
				}
				if !in.matchesTypeStrict(e, args[1]) {
					return false
				}
			}
			return true
		}
		return false
	}
	// nominal struct/enum types (v2.5)
	if in != nil {
		if def := in.structDef(typ); def != nil {
			return in.matchesStruct(v, def)
		}
		if def := in.enumDef(typ); def != nil {
			return v.Kind == VString && def.has(v.Str)
		}
	}
	switch typ {
	case "any":
		return true
	case "nil":
		return v.Kind == VNil
	case "bool":
		return v.Kind == VBool
	case "int":
		return v.Kind == VInt
	case "float":
		return v.Kind == VFloat
	case "number":
		return v.Kind == VInt || v.Kind == VFloat
	case "string":
		return v.Kind == VString
	case "array":
		return v.Kind == VArray
	case "map":
		return v.Kind == VMap
	case "func":
		return v.Kind == VFunc || v.Kind == VBuiltin
	case "chan":
		return v.Kind == VChan
	case "ok":
		return isOkValue(v)
	case "err":
		return isErrValue(v)
	}
	return false
}

func (in *Interpreter) matchesTypeNullable(v Value, typ string) bool {
	if typ == "" || typ == "any" {
		return true
	}
	if v.Kind == VNil {
		return true
	}
	return in.matchesTypeStrict(v, typ)
}

func (in *Interpreter) checkTypeNullable(v Value, typ, what string) error {
	if typ == "" || typ == "any" {
		return nil
	}
	if !validTypeIn(typ, in) {
		return fmt.Errorf("unknown type %q", typ)
	}
	if !in.matchesTypeNullable(v, typ) {
		return fmt.Errorf("%s wants %s, got %s", what, typ, TypeName(v))
	}
	return nil
}

// structDef/enumDef look up declared nominal types.
func (in *Interpreter) structDef(name string) *StructDef {
	in.typeMu.RLock()
	defer in.typeMu.RUnlock()
	return in.structs[name]
}

func (in *Interpreter) enumDef(name string) *EnumDef {
	in.typeMu.RLock()
	defer in.typeMu.RUnlock()
	return in.enums[name]
}

func (e *EnumDef) has(variant string) bool {
	for _, m := range e.Variants {
		if m == variant {
			return true
		}
	}
	return false
}

// matchesStruct validates a map value against a struct shape: every declared
// field present with a matching (nullable) type, no unknown fields.
func (in *Interpreter) matchesStruct(v Value, def *StructDef) bool {
	if v.Kind != VMap {
		return false
	}
	v.Map.Mu.RLock()
	defer v.Map.Mu.RUnlock()
	for _, f := range def.Fields {
		fv, ok := v.Map.Vals[f.Name]
		if !ok {
			return false
		}
		if fv.Kind == VNil {
			continue
		}
		if !in.matchesTypeStrict(fv, f.Type) {
			return false
		}
	}
	for k := range v.Map.Vals {
		known := false
		for _, f := range def.Fields {
			if f.Name == k {
				known = true
				break
			}
		}
		if !known {
			return false
		}
	}
	return true
}

// ---------------------------------------------------------------------------
// Environments (lexical scopes, goroutine-safe via interpreter lock)
// ---------------------------------------------------------------------------

// Env is a lexical scope.
type Env struct {
	Parent   *Env
	Vars     map[string]Value
	TypeAnns map[string]string
	// Defer support: function call frames (isCall) collect deferred calls
	// registered by `defer` and run them LIFO when the call returns.
	isCall bool
	defers []deferredCall
}

// deferredCall is one `defer f(args)` pending on a call frame.
// The function and arguments are evaluated when `defer` runs;
// the call happens when the enclosing function returns.
type deferredCall struct {
	fn   Value
	args []Value
	line int
}

func newEnv(parent *Env) *Env {
	return &Env{Parent: parent, Vars: map[string]Value{}, TypeAnns: map[string]string{}}
}

func newEnvSized(parent *Env, size int) *Env {
	if size < 0 {
		size = 0
	}
	return &Env{Parent: parent, Vars: make(map[string]Value, size), TypeAnns: map[string]string{}}
}

// ---------------------------------------------------------------------------
// Interpreter
// ---------------------------------------------------------------------------

// Interpreter holds global state. Safe for concurrent `go` use.
//
// Perf: variable scopes are goroutine-private until the first `go`
// statement runs. While conc is false the interpreter uses lock-free
// map access (single goroutine, no races). The first `go` flips conc
// to true permanently and all later scope access takes the global
// RWMutex. This keeps the single-threaded fast path (fib, loops)
// free of atomic/mutex overhead per lookup.
// StructDef is a `struct Name { field: type, ... }` declaration (v2.5).
type StructDef struct {
	Name   string
	Fields []StructField
	Line   int
}

// StructField is one named, typed struct field.
type StructField struct {
	Name string
	Type string
}

// EnumDef is an `enum Name { A, B, ... }` declaration (v2.5).
type EnumDef struct {
	Name     string
	Variants []string
	Line     int
}

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
	conc     atomic.Bool
	// Custom nominal types from struct/enum declarations (v2.5).
	// Guarded by typeMu: definitions run on the main flow but `go`
	// blocks share the interpreter.
	typeMu  sync.RWMutex
	structs map[string]*StructDef
	enums   map[string]*EnumDef
}

func New() *Interpreter {
	in := &Interpreter{
		globals:  newEnv(nil),
		imported: map[string]bool{},
		structs:  map[string]*StructDef{},
		enums:    map[string]*EnumDef{},
	}
	in.defineBuiltins(in.globals)
	return in
}

// Run executes a parsed program and waits for `go` statements.
func Run(p *frontend.Program) error {
	return RunWithDir(p, "")
}

// RunWithDir executes a program with imports resolved relative to dir
// (typically the app dir). If dir is "", it defaults to the program's dir
// when the program came from a real file path.
func RunWithDir(p *frontend.Program, dir string) error {
	in := New()
	if dir != "" {
		in.baseDir = dir
	} else if p.Path != "" && p.Path != "<expr>" {
		// Bare names like "test.ks" (unit tests) carry no directory;
		// only adopt the dir when the path actually has one.
		if d := filepath.Dir(p.Path); d != "" && d != "." {
			in.baseDir = d
		}
	}
	if err := in.ExecProgram(p); err != nil {
		return err
	}
	in.wg.Wait()
	return in.fail()
}

// RunFile executes a single .ks source file, .kslib bundle or secure .ksx
// bundle directly. This is what makes shebang executables work:
//
//	#!/usr/bin/env fusion        (.ks files: `#` is already a comment)
//	chmod +x app.kslib && ./app.kslib
func RunFile(path string) error {
	if strings.HasSuffix(path, lib.Ext) || strings.HasSuffix(path, lib.SecureExt) {
		in := New()
		if abs, err := filepath.Abs(path); err == nil {
			in.baseDir = filepath.Dir(abs)
		}
		if err := in.execBundleFile(in.globals, path); err != nil {
			return err
		}
		in.wg.Wait()
		return in.fail()
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	p, err := frontend.ParseSource(string(data), path)
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	return RunWithDir(p, dir)
}

// SetBaseDir sets import base dir (for embedding/web).
func (in *Interpreter) SetBaseDir(dir string) { in.baseDir = dir }

// Lookup finds a global (or builtin) by name.
func (in *Interpreter) Lookup(name string) (Value, bool) {
	// search globals chain
	for env := in.globals; env != nil; env = env.Parent {
		if v, ok := env.Vars[name]; ok {
			return v, true
		}
	}
	if b, ok := builtinByName(name); ok {
		return Value{Kind: VBuiltin, Builtin: b}, true
	}
	return Nil(), false
}

// Call invokes a func value (exported for web/embedding).
func (in *Interpreter) Call(fn Value, args []Value) (Value, error) {
	return in.callValue(fn, args)
}

// ValueToJSONable converts a Value to JSON-encodable Go value (exported for web).
func ValueToJSONable(v Value) (any, error) { return valueToJSON(v) }

// ExecProgram runs program statements in globals (exported for imports/embedding).
// Applies constant folding (v2.2 perf) once per program (idempotent).
func (in *Interpreter) ExecProgram(p *frontend.Program) error {
	frontend.FoldProgram(p)
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
	// Fast path: no `go` ran yet, no concurrent writer exists.
	if !in.conc.Load() {
		return in.err
	}
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
	// avoid double "line N:" (only when this error already carries one)
	if strings.HasPrefix(msg, "line ") {
		return err
	}
	if line > 0 {
		return fmt.Errorf("line %d: %w", line, err)
	}
	return err
}

// env helpers: lock-free while single-threaded (!conc), locked after `go`.
func (in *Interpreter) lookup(env *Env, name string) (Value, bool) {
	if !in.conc.Load() {
		for e := env; e != nil; e = e.Parent {
			if v, ok := e.Vars[name]; ok {
				return v, true
			}
		}
		return Nil(), false
	}
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
	if !in.conc.Load() {
		env.Vars[name] = v
		return
	}
	in.mu.Lock()
	defer in.mu.Unlock()
	env.Vars[name] = v
}

func (in *Interpreter) assign(env *Env, name string, v Value) bool {
	if !in.conc.Load() {
		for e := env; e != nil; e = e.Parent {
			if _, ok := e.Vars[name]; ok {
				e.Vars[name] = v
				return true
			}
		}
		return false
	}
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

func (in *Interpreter) defineTypeAnn(env *Env, name, typ string) {
	if typ == "" {
		return
	}
	if !in.conc.Load() {
		env.TypeAnns[name] = typ
		return
	}
	in.mu.Lock()
	defer in.mu.Unlock()
	env.TypeAnns[name] = typ
}

func (in *Interpreter) lookupTypeAnn(env *Env, name string) (string, bool) {
	if !in.conc.Load() {
		for e := env; e != nil; e = e.Parent {
			if t, ok := e.TypeAnns[name]; ok {
				return t, true
			}
		}
		return "", false
	}
	in.mu.RLock()
	defer in.mu.RUnlock()
	for e := env; e != nil; e = e.Parent {
		if t, ok := e.TypeAnns[name]; ok {
			return t, true
		}
	}
	return "", false
}

// ---------------------------------------------------------------------------
// Statement execution
// ---------------------------------------------------------------------------

func (in *Interpreter) execStmt(env *Env, st *frontend.Stmt) error {
	if f := in.fail(); f != nil {
		return f
	}
	switch st.Kind {
	case frontend.StmtLet:
		v, err := in.eval(env, st.Expr)
		if err != nil {
			return err
		}
		if st.TypeAnn != "" {
			if err := in.checkTypeNullable(v, st.TypeAnn, "let "+st.Name); err != nil {
				return err
			}
		}
		in.define(env, st.Name, v)
		in.defineTypeAnn(env, st.Name, st.TypeAnn)
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
		if inner.Kind == frontend.StmtDefer {
			return fmt.Errorf("go defer is not allowed (defer runs when the enclosing function returns)")
		}
		// Entering concurrent mode: all later scope access is locked.
		in.conc.Store(true)
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
			// Then is always a Block which creates its own scope;
			// no extra env needed (saves 1 map alloc per if).
			return in.execStmt(env, st.Then)
		}
		if st.Else != nil {
			// else-if is a StmtIf: exec in same env; else-block: Block scopes itself.
			return in.execStmt(env, st.Else)
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
			// Body is a Block with its own per-iteration scope.
			err = in.execStmt(child, st.Body)
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
		fn := &FuncObj{Name: st.Name, Params: append([]string{}, st.Names...), ParamTypes: append([]string{}, st.ParamTypes...), ReturnType: st.ReturnType, Body: st.Body, Closure: env}
		// normalize param types length
		for len(fn.ParamTypes) < len(fn.Params) {
			fn.ParamTypes = append(fn.ParamTypes, "")
		}
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
	case frontend.StmtTry:
		return in.execTry(env, st)
	case frontend.StmtSwitch:
		return in.execSwitch(env, st)
	case frontend.StmtSelect:
		return in.execSelect(env, st)
	case frontend.StmtDefer:
		return in.execDefer(env, st)
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
		if ann, ok := in.lookupTypeAnn(env, st.Name); ok && ann != "" {
			if err := in.checkTypeNullable(v, ann, st.Name); err != nil {
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
	// Fast path (v2.4 perf): `for i in range(n)` without array alloc.
	if start, end, step, ok := in.rangeArgs(env, st.Expr); ok {
		loopEnv := newEnv(env)
		two := len(st.Names) == 2
		// for i in range -> single var; for i, v variant not used with range, treat second as value= index?
		if step > 0 {
			for i := start; i < end; i += step {
				iterEnv := newEnv(loopEnv)
				if two {
					in.define(iterEnv, st.Names[0], IntV(i-start))
					in.define(iterEnv, st.Names[1], IntV(i))
				} else {
					in.define(iterEnv, st.Names[0], IntV(i))
				}
				if err := in.execStmt(iterEnv, st.Body); err != nil {
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
		} else {
			for i := start; i > end; i += step {
				iterEnv := newEnv(loopEnv)
				if two {
					in.define(iterEnv, st.Names[0], IntV(start-i))
					in.define(iterEnv, st.Names[1], IntV(i))
				} else {
					in.define(iterEnv, st.Names[0], IntV(i))
				}
				if err := in.execStmt(iterEnv, st.Body); err != nil {
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
		}
		return nil
	}
	iter, err := in.eval(env, st.Expr)
	if err != nil {
		return err
	}
	loopEnv := newEnv(env)
	// Per-iteration env for loop vars (Go 1.22 semantics), so `go`
	// closures capture the current iteration's values.
	// Body is a Block with its own scope; no extra env here.
	runBody := func(iterEnv *Env) error {
		err := in.execStmt(iterEnv, st.Body)
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
	case VChan:
		// Channel iteration: `for v in ch` receives until the channel
		// is closed and drained (like Go's `for v := range ch`).
		// Two loop vars are not meaningful for channels.
		if two {
			return fmt.Errorf("cannot iterate chan with 2 vars (try `for v in ch`)")
		}
		for {
			v, ok := <-iter.Chan.Ch
			if !ok {
				return nil
			}
			iterEnv := newEnv(loopEnv)
			in.define(iterEnv, st.Names[0], v)
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
	default:
		return fmt.Errorf("cannot iterate %s (try array, map, string or chan)", TypeName(iter))
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
		// Body is a Block with its own per-iteration scope.
		err := in.execStmt(loopEnv, st.Body)
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

func (in *Interpreter) execTry(env *Env, st *frontend.Stmt) error {
	// Then/CaBody/FinBody are Blocks which scope themselves.
	err := in.execStmt(env, st.Then)
	if err == nil {
		if st.FinBody != nil {
			return in.execStmt(env, st.FinBody)
		}
		return nil
	}
	// Control flow (return/break/continue) is never caught: run finally
	// for cleanup, then propagate.
	if isControl(err) {
		if st.FinBody != nil {
			if ferr := in.execStmt(env, st.FinBody); ferr != nil {
				return ferr
			}
		}
		return err
	}
	// Runtime error: catch and/or finally.
	if st.CaBody != nil {
		catchEnv := newEnv(env)
		if st.Catch != "" {
			in.define(catchEnv, st.Catch, StrV(err.Error()))
		}
		if cerr := in.execStmt(catchEnv, st.CaBody); cerr != nil {
			if st.FinBody != nil {
				if ferr := in.execStmt(env, st.FinBody); ferr != nil {
					return ferr
				}
			}
			return cerr
		}
		if st.FinBody != nil {
			return in.execStmt(env, st.FinBody)
		}
		return nil
	}
	if st.FinBody != nil {
		if ferr := in.execStmt(env, st.FinBody); ferr != nil {
			return ferr
		}
	}
	return err
}

func (in *Interpreter) execSwitch(env *Env, st *frontend.Stmt) error {
	target, err := in.eval(env, st.Expr)
	if err != nil {
		return err
	}
	var def *frontend.SwitchCase
	for _, c := range st.Cases {
		if c.IsDefault {
			def = c
			continue
		}
		for _, v := range c.Values {
			cv, err := in.eval(env, v)
			if err != nil {
				return err
			}
			if deepEqual(target, cv) {
				// Like Go, `break` inside a case ends the switch.
				// Body is a Block which scopes itself.
				if err := in.execStmt(env, c.Body); err != nil {
					if ce, ok := err.(*ctrlError); ok && ce.kind == ctrlBreak {
						return nil
					}
					return err
				}
				return nil
			}
		}
	}
	if def != nil {
		if err := in.execStmt(env, def.Body); err != nil {
			if ce, ok := err.(*ctrlError); ok && ce.kind == ctrlBreak {
				return nil
			}
			return err
		}
	}
	return nil
}

// execSelect implements Go-like channel multiplexing:
//
//	select {
//	  case v = recv(c1) { ... }  # receive + bind (bind optional)
//	  case recv(c2) { ... }      # receive + discard
//	  case send(c3, 42) { ... }
//	  case timeout(100) { ... }  # ms
//	  default { ... }            # non-blocking fallback, must be last
//	}
//
// Without `default` it blocks until one case is ready (ready cases win
// uniformly at random, like Go); with `default` it never blocks.
// A case on a nil channel is never ready: assigning `ch = nil` after
// drain is the idiomatic way to disable a case (fan-in loops).
// `break` inside a case body ends the select (like `switch`).
func (in *Interpreter) execSelect(env *Env, st *frontend.Stmt) (err error) {
	if len(st.SelectCases) == 0 {
		return fmt.Errorf("select needs at least one case/default")
	}
	// A send on a concurrently-closed channel panics inside
	// reflect.Select; convert that into a regular ks error.
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("select failed: %v", r)
		}
	}()
	type prepared struct {
		sc      *frontend.SelectCase
		ch      Value
		sendVal Value
		ms      int
		// nilCh marks a recv/send case on a nil channel. Like Go, ops on
		// nil channels block forever, so the case just never becomes
		// ready. Assigning `ch = nil` after drain is the idiomatic way
		// to disable a select case (fan-in loops).
		nilCh bool
	}
	var comms []prepared
	var defBody *frontend.Stmt
	var timeouts []prepared
	for _, sc := range st.SelectCases {
		switch sc.Kind {
		case "default":
			defBody = sc.Body
		case "recv":
			ch, everr := in.eval(env, sc.Chan)
			if everr != nil {
				return withLine(sc.Line, everr)
			}
			if ch.Kind == VNil {
				comms = append(comms, prepared{sc: sc, nilCh: true})
				continue
			}
			if ch.Kind != VChan {
				return withLine(sc.Line, fmt.Errorf("select recv wants chan, got %s", TypeName(ch)))
			}
			comms = append(comms, prepared{sc: sc, ch: ch})
		case "send":
			ch, everr := in.eval(env, sc.Chan)
			if everr != nil {
				return withLine(sc.Line, everr)
			}
			if ch.Kind == VNil {
				// Evaluate the value for its side effects, then park
				// the case as never-ready (Go: send on nil blocks).
				if _, everr := in.eval(env, sc.Value); everr != nil {
					return withLine(sc.Line, everr)
				}
				comms = append(comms, prepared{sc: sc, nilCh: true})
				continue
			}
			if ch.Kind != VChan {
				return withLine(sc.Line, fmt.Errorf("select send wants chan, got %s", TypeName(ch)))
			}
			val, everr := in.eval(env, sc.Value)
			if everr != nil {
				return withLine(sc.Line, everr)
			}
			ch.Chan.Mu.Lock()
			closed := ch.Chan.Closed
			ch.Chan.Mu.Unlock()
			if closed {
				return withLine(sc.Line, fmt.Errorf("send on closed channel"))
			}
			comms = append(comms, prepared{sc: sc, ch: ch, sendVal: val})
		case "timeout":
			mv, everr := in.eval(env, sc.Timeout)
			if everr != nil {
				return withLine(sc.Line, everr)
			}
			ms, everr := toMillis(mv)
			if everr != nil {
				return withLine(sc.Line, everr)
			}
			timeouts = append(timeouts, prepared{sc: sc, ms: ms})
		default:
			return fmt.Errorf("bad select case %q", sc.Kind)
		}
	}
	runBody := func(sc *frontend.SelectCase, received Value, hasValue bool) error {
		if sc.Kind == "recv" && sc.Bind != "" {
			caseEnv := newEnv(env)
			if hasValue {
				in.define(caseEnv, sc.Bind, received)
			} else {
				in.define(caseEnv, sc.Bind, Nil())
			}
			if cerr := in.execStmt(caseEnv, sc.Body); cerr != nil {
				if ce, ok := cerr.(*ctrlError); ok && ce.kind == ctrlBreak {
					return nil
				}
				return cerr
			}
			return nil
		}
		if cerr := in.execStmt(env, sc.Body); cerr != nil {
			if ce, ok := cerr.(*ctrlError); ok && ce.kind == ctrlBreak {
				return nil
			}
			return cerr
		}
		return nil
	}
	if defBody != nil {
		// Non-blocking: one reflect.Select with a default clause picks
		// uniformly among ready comms, else falls to default.
		// Timeouts never fire on the non-blocking path (Go semantics:
		// `default` already means "don't wait").
		if len(comms) == 0 {
			return runBody(&frontend.SelectCase{Kind: "default", Body: defBody}, Nil(), false)
		}
		cases := make([]reflect.SelectCase, 0, len(comms)+1)
		order := make([]prepared, 0, len(comms)+1)
		for _, p := range comms {
			if p.nilCh {
				continue // nil channel: never ready
			}
			if p.sc.Kind == "recv" {
				cases = append(cases, reflect.SelectCase{Dir: reflect.SelectRecv, Chan: reflect.ValueOf(p.ch.Chan.Ch)})
			} else {
				cases = append(cases, reflect.SelectCase{Dir: reflect.SelectSend, Chan: reflect.ValueOf(p.ch.Chan.Ch), Send: reflect.ValueOf(p.sendVal)})
			}
			order = append(order, p)
		}
		cases = append(cases, reflect.SelectCase{Dir: reflect.SelectDefault})
		chosen, recvVal, _ := reflect.Select(cases)
		if chosen == len(order) {
			return runBody(&frontend.SelectCase{Kind: "default", Body: defBody}, Nil(), false)
		}
		p := order[chosen]
		if p.sc.Kind == "recv" {
			var v Value
			if rv, ok := recvVal.Interface().(Value); ok {
				v = rv
			} else {
				v = Nil()
			}
			return runBody(p.sc, v, true)
		}
		return runBody(p.sc, Nil(), false)
	}
	// Blocking: wait for the first ready comm or timeout.
	// Nil-channel cases are skipped (never ready, like Go).
	cases := make([]reflect.SelectCase, 0, len(comms)+len(timeouts))
	order := make([]prepared, 0, len(comms)+len(timeouts))
	isTimeout := make([]bool, 0, len(comms)+len(timeouts))
	for _, p := range comms {
		if p.nilCh {
			continue
		}
		if p.sc.Kind == "recv" {
			cases = append(cases, reflect.SelectCase{Dir: reflect.SelectRecv, Chan: reflect.ValueOf(p.ch.Chan.Ch)})
		} else {
			cases = append(cases, reflect.SelectCase{Dir: reflect.SelectSend, Chan: reflect.ValueOf(p.ch.Chan.Ch), Send: reflect.ValueOf(p.sendVal)})
		}
		order = append(order, p)
		isTimeout = append(isTimeout, false)
	}
	for _, p := range timeouts {
		cases = append(cases, reflect.SelectCase{Dir: reflect.SelectRecv, Chan: reflect.ValueOf(time.After(time.Duration(p.ms) * time.Millisecond))})
		order = append(order, p)
		isTimeout = append(isTimeout, true)
	}
	if len(cases) == 0 {
		// Only disabled (nil) cases and no timeout/default:
		// block forever, like Go's `select {}`.
		select {}
	}
	chosen, recvVal, _ := reflect.Select(cases)
	p := order[chosen]
	if isTimeout[chosen] {
		return runBody(p.sc, Nil(), false)
	}
	if p.sc.Kind == "recv" {
		var v Value
		if rv, ok := recvVal.Interface().(Value); ok {
			v = rv
		} else {
			v = Nil()
		}
		return runBody(p.sc, v, true)
	}
	return runBody(p.sc, Nil(), false)
}

func (in *Interpreter) execDefer(env *Env, st *frontend.Stmt) error {
	// Find the enclosing function call frame.
	frame := env
	for frame != nil && !frame.isCall {
		frame = frame.Parent
	}
	if frame == nil {
		return fmt.Errorf("defer outside function (deferred calls run when the function returns)")
	}
	call := st.Expr
	fn, err := in.eval(env, call.Callee)
	if err != nil {
		return err
	}
	if fn.Kind != VFunc && fn.Kind != VBuiltin {
		return fmt.Errorf("defer needs a function, got %s", TypeName(fn))
	}
	args := make([]Value, len(call.Args))
	for i, a := range call.Args {
		v, err := in.eval(env, a)
		if err != nil {
			return err
		}
		args[i] = v
	}
	// frame is goroutine-private (one call frame per invocation);
	// no lock needed even in concurrent mode.
	frame.defers = append(frame.defers, deferredCall{fn: fn, args: args, line: st.Line})
	return nil
}

// runDefers executes a call frame's deferred calls LIFO.
func (in *Interpreter) runDefers(frame *Env) error {
	defers := frame.defers
	frame.defers = nil
	var firstErr error
	for i := len(defers) - 1; i >= 0; i-- {
		if _, err := in.callValue(defers[i].fn, defers[i].args); err != nil {
			if firstErr == nil {
				firstErr = withLine(defers[i].line, err)
			}
		}
		if f := in.fail(); f != nil && firstErr == nil {
			firstErr = f
		}
	}
	return firstErr
}

func (in *Interpreter) execImport(env *Env, path string) error {
	if path == "" {
		return fmt.Errorf("bad import: empty path")
	}
	// Library import: `import "name"` (no .ks suffix) resolves a built
	// .kslib bundle or secure .ksx, e.g. test-releases/hello-lib-0.1.0.ksx.
	// Make with `fusion new --lib` + `fusion build --release` (or --secure).
	if !strings.HasSuffix(path, ".ks") && !strings.HasSuffix(path, lib.Ext) && !strings.HasSuffix(path, lib.SecureExt) {
		return in.execLibImport(env, path)
	}
	if strings.HasSuffix(path, lib.Ext) || strings.HasSuffix(path, lib.SecureExt) {
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

// execBundleFile loads a .kslib bundle or secure .ksx bundle file (by
// path) and executes its sources once, in bundle order, in the importing
// scope. Secure bundles are decrypted strictly in memory (FUSION_KEY env
// or default-secret mode); nothing decrypted touches disk.
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
		// Maybe a bare "name.kslib"/"name.ksx" resolvable via search dirs.
		base := filepath.Base(path)
		base = strings.TrimSuffix(strings.TrimSuffix(base, lib.Ext), lib.SecureExt)
		if found, err := lib.Find(base, in.libSearchDirs()); err == nil {
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
	b, err := lib.LoadAny(full, "")
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
	case frontend.ExprPow:
		l, err := in.eval(env, e.Left)
		if err != nil {
			return Nil(), err
		}
		r, err := in.eval(env, e.Right)
		if err != nil {
			return Nil(), err
		}
		return powValues(l, r)
	case frontend.ExprIn:
		l, err := in.eval(env, e.Left)
		if err != nil {
			return Nil(), err
		}
		r, err := in.eval(env, e.Right)
		if err != nil {
			return Nil(), err
		}
		return inValues(l, r)
	case frontend.ExprIs:
		l, err := in.eval(env, e.Left)
		if err != nil {
			return Nil(), err
		}
		want, err := in.resolveIsType(env, e.Right)
		if err != nil {
			return Nil(), err
		}
		return BoolV(in.matchesTypeStrict(l, want)), nil
	case frontend.ExprCoalesce:
		l, err := in.eval(env, e.Left)
		if err != nil {
			return Nil(), err
		}
		if l.Kind != VNil {
			return l, nil
		}
		return in.eval(env, e.Right)
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
		if e.Safe && obj.Kind == VNil {
			return Nil(), nil
		}
		idx, err := in.eval(env, e.Right)
		if err != nil {
			return Nil(), err
		}
		if e.Safe {
			return indexValueSafe(obj, idx)
		}
		return indexValue(obj, idx)
	case frontend.ExprSlice:
		obj, err := in.eval(env, e.Left)
		if err != nil {
			return Nil(), err
		}
		var start, end *int
		if e.SliceStart != nil {
			sv, err := in.eval(env, e.SliceStart)
			if err != nil {
				return Nil(), err
			}
			if sv.Kind != VInt {
				return Nil(), fmt.Errorf("slice start must be int, got %s", TypeName(sv))
			}
			s := sv.Int
			start = &s
		}
		if e.SliceEnd != nil {
			ev, err := in.eval(env, e.SliceEnd)
			if err != nil {
				return Nil(), err
			}
			if ev.Kind != VInt {
				return Nil(), fmt.Errorf("slice end must be int, got %s", TypeName(ev))
			}
			s := ev.Int
			end = &s
		}
		return sliceValue(obj, start, end)
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
		fn := &FuncObj{Params: append([]string{}, e.FuncParams...), ParamTypes: append([]string{}, e.FuncParamTypes...), ReturnType: e.FuncReturnType, Body: e.FuncBody, Closure: env}
		for len(fn.ParamTypes) < len(fn.Params) {
			fn.ParamTypes = append(fn.ParamTypes, "")
		}
		return Value{Kind: VFunc, Func: fn}, nil
	}
	return Nil(), fmt.Errorf("bad expression")
}

// resolveIsType extracts the type name for `x is T` without requiring
// `T` to be a bound variable: ident `int` means "int", string "int"
// means "int", nil means "nil", otherwise eval and require a string.
func (in *Interpreter) resolveIsType(env *Env, e *frontend.Expr) (string, error) {
	if e == nil {
		return "", fmt.Errorf("is wants a type name (nil|bool|int|float|number|string|array|map|func|chan|any|ok|err)")
	}
	switch e.Kind {
	case frontend.ExprVar:
		if !validType(e.Name) {
			return "", fmt.Errorf("unknown type %q (want nil|bool|int|float|number|string|array|map|func|chan|any|ok|err)", e.Name)
		}
		return e.Name, nil
	case frontend.ExprString:
		if !validType(e.StrVal) {
			return "", fmt.Errorf("unknown type %q (want nil|bool|int|float|number|string|array|map|func|chan|any|ok|err)", e.StrVal)
		}
		return e.StrVal, nil
	case frontend.ExprNil:
		return "nil", nil
	}
	v, err := in.eval(env, e)
	if err != nil {
		return "", err
	}
	if v.Kind != VString {
		return "", fmt.Errorf("is wants a type name string, got %s", TypeName(v))
	}
	if !validType(v.Str) {
		return "", fmt.Errorf("unknown type %q (want nil|bool|int|float|number|string|array|map|func|chan|any|ok|err)", v.Str)
	}
	return v.Str, nil
}

// checkFuncArgs enforces gradual param types (nil passes, like `let x: T`).
func checkFuncArgs(fn *FuncObj, args []Value) error {
	for i, p := range fn.Params {
		if i >= len(fn.ParamTypes) {
			break
		}
		want := fn.ParamTypes[i]
		if want == "" || want == "any" {
			continue
		}
		if i >= len(args) {
			break
		}
		if err := checkTypeNullable(args[i], want, "param "+p); err != nil {
			if fn.Name != "" {
				return fmt.Errorf("function %q: %v", fn.Name, err)
			}
			return err
		}
	}
	return nil
}

func checkFuncReturn(fn *FuncObj, ret Value) error {
	if fn.ReturnType == "" || fn.ReturnType == "any" {
		return nil
	}
	if err := checkTypeNullable(ret, fn.ReturnType, "return"); err != nil {
		if fn.Name != "" {
			return fmt.Errorf("function %q: %v", fn.Name, err)
		}
		return err
	}
	return nil
}

// execFuncBody runs a user function body in an already-prepared callEnv.
// The body is always a Block; its statements run directly in callEnv
// (one scope for params + locals) instead of allocating a redundant
// child Block env per call. Semantics are preserved: `let` in the
// body shadows params in the same map, which is unobservable after
// return, and closures capture callEnv either way.
func (in *Interpreter) execFuncBody(callEnv *Env, body *frontend.Stmt) (Value, error) {
	var ret Value = Nil()
	var rerr error
	if body != nil && body.Kind == frontend.StmtBlock {
		for _, s := range body.List {
			if err := in.execStmt(callEnv, s); err != nil {
				if ce, ok := err.(*ctrlError); ok && ce.kind == ctrlReturn {
					ret = ce.val
					rerr = nil
					break
				}
				rerr = err
				break
			}
		}
	} else if body != nil {
		if err := in.execStmt(callEnv, body); err != nil {
			if ce, ok := err.(*ctrlError); ok && ce.kind == ctrlReturn {
				ret = ce.val
			} else {
				rerr = err
			}
		}
	}
	if derr := in.runDefers(callEnv); derr != nil && rerr == nil {
		return Nil(), derr
	}
	if rerr != nil {
		return Nil(), rerr
	}
	return ret, nil
}

func (in *Interpreter) evalCall(env *Env, e *frontend.Expr) (Value, error) {
	fn, err := in.eval(env, e.Callee)
	if err != nil {
		return Nil(), err
	}
	args := make([]Value, len(e.Args))
	for i, a := range e.Args {
		v, err := in.eval(env, a)
		if err != nil {
			return Nil(), err
		}
		args[i] = v
	}
	switch fn.Kind {
	case VFunc:
		if len(args) != len(fn.Func.Params) {
			return Nil(), fmt.Errorf("function %q wants %d args, got %d", fn.Func.Name, len(fn.Func.Params), len(args))
		}
		if err := in.checkFuncArgs(fn.Func, args); err != nil {
			return Nil(), err
		}
		callEnv := newEnvSized(fn.Func.Closure, len(fn.Func.Params))
		callEnv.isCall = true
		for i, p := range fn.Func.Params {
			callEnv.Vars[p] = args[i]
		}
		// lock-free define (single goroutine owns callEnv); direct map write ok
		// but other goroutines may read closure parents via lookup (locked) - safe.
		ret, err := in.execFuncBody(callEnv, fn.Func.Body)
		if err != nil {
			return Nil(), err
		}
		if err := in.checkFuncReturn(fn.Func, ret); err != nil {
			return Nil(), err
		}
		return ret, nil
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

// indexValueSafe implements `?.`: nil base already handled by the caller.
// Missing map keys and out-of-range array/string indexes yield nil instead
// of an error; genuine type errors (indexing an int, wrong index type)
// still fail so bugs stay visible.
func indexValueSafe(obj, idx Value) (Value, error) {
	switch obj.Kind {
	case VArray:
		if idx.Kind != VInt {
			return Nil(), fmt.Errorf("array index must be int, got %s", TypeName(idx))
		}
		obj.Arr.Mu.RLock()
		defer obj.Arr.Mu.RUnlock()
		if idx.Int < 0 || idx.Int >= len(obj.Arr.Items) {
			return Nil(), nil
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
			return Nil(), nil
		}
		return v, nil
	case VString:
		if idx.Kind != VInt {
			return Nil(), fmt.Errorf("string index must be int, got %s", TypeName(idx))
		}
		runes := []rune(obj.Str)
		if idx.Int < 0 || idx.Int >= len(runes) {
			return Nil(), nil
		}
		return StrV(string(runes[idx.Int])), nil
	case VNil:
		return Nil(), nil
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
	// Fast path: string+string avoids Display() dispatch.
	if l.Kind == VString && r.Kind == VString {
		// Pre-grow via string concat (Go handles this efficiently;
		// hot `s += chunk` loops stay O(n) amortized per append).
		return StrV(l.Str + r.Str), nil
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

// intPow is exponentiation by squaring: O(log n) instead of O(n).
func intPow(base, exp int) int {
	res := 1
	b := base
	e := exp
	for e > 0 {
		if e&1 == 1 {
			res *= b
		}
		e >>= 1
		if e > 0 {
			b *= b
		}
	}
	return res
}

func powValues(l, r Value) (Value, error) {
	if !isNum(l) || !isNum(r) {
		return Nil(), fmt.Errorf("cannot raise %s to %s (need numbers)", TypeName(l), TypeName(r))
	}
	if l.Kind == VInt && r.Kind == VInt && r.Int >= 0 {
		return IntV(intPow(l.Int, r.Int)), nil
	}
	return FloatV(math.Pow(toFloat(l), toFloat(r))), nil
}

func inValues(l, r Value) (Value, error) {
	switch r.Kind {
	case VArray:
		r.Arr.Mu.RLock()
		defer r.Arr.Mu.RUnlock()
		for _, e := range r.Arr.Items {
			if deepEqual(l, e) {
				return BoolV(true), nil
			}
		}
		return BoolV(false), nil
	case VMap:
		if l.Kind != VString {
			return Nil(), fmt.Errorf("cannot check %s in map (need string key)", TypeName(l))
		}
		r.Map.Mu.RLock()
		defer r.Map.Mu.RUnlock()
		_, ok := r.Map.Vals[l.Str]
		return BoolV(ok), nil
	case VString:
		if l.Kind != VString {
			return Nil(), fmt.Errorf("cannot check %s in string (need string)", TypeName(l))
		}
		return BoolV(strings.Contains(r.Str, l.Str)), nil
	}
	return Nil(), fmt.Errorf("cannot check in %s (try array, map or string)", TypeName(r))
}

func sliceValue(obj Value, start, end *int) (Value, error) {
	s := 0
	if start != nil {
		s = *start
	}
	var e *int
	if end != nil {
		e = end
	}
	switch obj.Kind {
	case VArray:
		// Copy only the requested window, not the whole array.
		obj.Arr.Mu.RLock()
		n := len(obj.Arr.Items)
		from, to, _ := normSlice(n, s, e)
		out := make([]Value, to-from)
		copy(out, obj.Arr.Items[from:to])
		obj.Arr.Mu.RUnlock()
		return ArrV(out), nil
	case VString:
		runes := []rune(obj.Str)
		from, to, _ := normSlice(len(runes), s, e)
		return StrV(string(runes[from:to])), nil
	}
	return Nil(), fmt.Errorf("cannot slice %s (try array or string)", TypeName(obj))
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
	for _, b := range cachedBuiltins() {
		env.Vars[b.Name] = Value{Kind: VBuiltin, Builtin: b}
	}
}

// builtinByName is an O(1) map lookup over a process-wide cache.
// The old version rebuilt the ~80-entry slice and linearly scanned
// it on every unresolved variable, allocating on the hot path.
func builtinByName(name string) (*BuiltinObj, bool) {
	b, ok := cachedBuiltinMap()[name]
	return b, ok
}

func allBuiltins() []*BuiltinObj {
	base := []*BuiltinObj{
		{Name: "len", Fn: bLen},
		{Name: "str", Fn: bStr},
		{Name: "int", Fn: bInt},
		{Name: "float", Fn: bFloat},
		{Name: "bool", Fn: bBool},
		{Name: "type", Fn: bType},
		{Name: "chr", Fn: bChr},
		{Name: "ord", Fn: bOrd},
		{Name: "hex", Fn: bHex},
		{Name: "range", Fn: bRange},
		{Name: "push", Fn: bPush},
		{Name: "pop", Fn: bPop},
		{Name: "insert", Fn: bInsert},
		{Name: "remove", Fn: bRemove},
		{Name: "clear", Fn: bClear},
		{Name: "reverse", Fn: bReverse},
		{Name: "sort", Fn: bSort},
		{Name: "slice", Fn: bSlice},
		{Name: "keys", Fn: bKeys},
		{Name: "values", Fn: bValues},
		{Name: "has", Fn: bHas},
		{Name: "delete", Fn: bDelete},
		{Name: "merge", Fn: bMerge},
		{Name: "get", Fn: bGet},
		{Name: "contains", Fn: bContains},
		{Name: "index_of", Fn: bIndexOf},
		{Name: "split", Fn: bSplit},
		{Name: "join", Fn: bJoin},
		{Name: "upper", Fn: bUpper},
		{Name: "lower", Fn: bLower},
		{Name: "trim", Fn: bTrim},
		{Name: "starts_with", Fn: bStartsWith},
		{Name: "ends_with", Fn: bEndsWith},
		{Name: "replace", Fn: bReplace},
		{Name: "substr", Fn: bSubstr},
		{Name: "repeat", Fn: bRepeat},
		{Name: "abs", Fn: bAbs},
		{Name: "min", Fn: bMin},
		{Name: "max", Fn: bMax},
		{Name: "floor", Fn: bFloor},
		{Name: "ceil", Fn: bCeil},
		{Name: "round", Fn: bRound},
		{Name: "sqrt", Fn: bSqrt},
		{Name: "pow", Fn: bPow},
		{Name: "pi", Fn: bPi},
		{Name: "now", Fn: bNow},
		{Name: "rand", Fn: bRand},
		{Name: "randint", Fn: bRandint},
		{Name: "seed", Fn: bSeed},
		{Name: "bit_and", Fn: bBitAnd},
		{Name: "bit_or", Fn: bBitOr},
		{Name: "bit_xor", Fn: bBitXor},
		{Name: "bit_shl", Fn: bBitShl},
		{Name: "bit_shr", Fn: bBitShr},
		{Name: "bit_not", Fn: bBitNot},
		{Name: "map", Fn: bMapFn},
		{Name: "filter", Fn: bFilter},
		{Name: "each", Fn: bEach},
		{Name: "reduce", Fn: bReduce},
		{Name: "apply", Fn: bApply},
		{Name: "json_stringify", Fn: bJsonStringify},
		{Name: "json_parse", Fn: bJsonParse},
		{Name: "input", Fn: bInput},
		{Name: "read_file", Fn: bReadFile},
		{Name: "write_file", Fn: bWriteFile},
		{Name: "append_file", Fn: bAppendFile},
		{Name: "exists", Fn: bExists},
		{Name: "list_dir", Fn: bListDir},
		{Name: "mkdir", Fn: bMkdir},
		{Name: "remove_file", Fn: bRemovePath},
		{Name: "exit", Fn: bExit},
		{Name: "argv", Fn: bArgv},
		{Name: "env", Fn: bEnv},
		{Name: "chan", Fn: bChan},
		{Name: "send", Fn: bSend},
		{Name: "recv", Fn: bRecv},
		{Name: "try_send", Fn: bTrySend},
		{Name: "try_recv", Fn: bTryRecv},
		{Name: "recv_timeout", Fn: bRecvTimeout},
		{Name: "send_timeout", Fn: bSendTimeout},
		{Name: "chan_len", Fn: bChanLen},
		{Name: "chan_cap", Fn: bChanCap},
		{Name: "chan_closed", Fn: bChanClosed},
		{Name: "close", Fn: bClose},
		{Name: "sleep", Fn: bSleep},
		{Name: "assert", Fn: bAssert},
		{Name: "error", Fn: bError},
		{Name: "panic", Fn: bError},
		{Name: "is_type", Fn: bIsType},
		{Name: "assert_type", Fn: bAssertType},
		{Name: "ok", Fn: bOk},
		{Name: "err", Fn: bErr},
		{Name: "is_ok", Fn: bIsOk},
		{Name: "is_err", Fn: bIsErr},
		{Name: "unwrap", Fn: bUnwrap},
		{Name: "unwrap_or", Fn: bUnwrapOr},
	}
	return append(append(append(append(base, extraBuiltins()...), extraBuiltinsV23()...), extraBuiltinsV24()...), extraBuiltinsV25()...)
}

// Process-wide builtin cache: built once, reused by every interpreter.
// allBuiltins() allocates a fresh slice per call; the cache avoids
// that allocation on New() and on every unknown-variable lookup.
//
// The cache is filled lazily (sync.Once) rather than in a package-level
// initializer: eagerly calling allBuiltins() at init time creates an
// initialization cycle (allBuiltins transitively reaches builtinByName,
// which reads the cache).
var (
	builtinCacheOnce sync.Once
	builtinCacheList []*BuiltinObj
	builtinCacheMap  map[string]*BuiltinObj
)

func initBuiltinCache() {
	builtinCacheOnce.Do(func() {
		builtinCacheList = allBuiltins()
		m := make(map[string]*BuiltinObj, len(builtinCacheList))
		for _, b := range builtinCacheList {
			m[b.Name] = b
		}
		builtinCacheMap = m
	})
}

func cachedBuiltins() []*BuiltinObj {
	initBuiltinCache()
	return builtinCacheList
}

func cachedBuiltinMap() map[string]*BuiltinObj {
	initBuiltinCache()
	return builtinCacheMap
}

// BuiltinNames returns all builtin names (exported for vet/LSP).
func BuiltinNames() []string {
	initBuiltinCache()
	out := make([]string, 0, len(builtinCacheList))
	for _, b := range builtinCacheList {
		out = append(out, b.Name)
	}
	return out
}

// BuiltinCount returns number of builtins.
func BuiltinCount() int {
	initBuiltinCache()
	return len(builtinCacheList)
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
	case VChan:
		return IntV(len(args[0].Chan.Ch)), nil
	}
	return Nil(), fmt.Errorf("len wants string/array/map/chan, got %s", TypeName(args[0]))
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

func bIsType(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("is_type", args, 2, 2); err != nil {
		return Nil(), err
	}
	if args[1].Kind != VString {
		return Nil(), fmt.Errorf("is_type wants (value, type-string), got (..., %s)", TypeName(args[1]))
	}
	if !validType(args[1].Str) {
		return Nil(), fmt.Errorf("unknown type %q (want nil|bool|int|float|number|string|array|map|func|chan|any|ok|err)", args[1].Str)
	}
	return BoolV(in.matchesTypeStrict(args[0], args[1].Str)), nil
}

func bAssertType(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("assert_type", args, 2, 2); err != nil {
		return Nil(), err
	}
	if args[1].Kind != VString {
		return Nil(), fmt.Errorf("assert_type wants (value, type-string), got (..., %s)", TypeName(args[1]))
	}
	if !validType(args[1].Str) {
		return Nil(), fmt.Errorf("unknown type %q (want nil|bool|int|float|number|string|array|map|func|chan|any|ok|err)", args[1].Str)
	}
	if !in.matchesTypeStrict(args[0], args[1].Str) {
		return Nil(), fmt.Errorf("assert_type failed: wants %s, got %s", args[1].Str, TypeName(args[0]))
	}
	return args[0], nil
}

func bOk(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("ok", args, 1, 1); err != nil {
		return Nil(), err
	}
	return MapV(map[string]Value{"ok": args[0]}), nil
}

func bErr(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("err", args, 1, 1); err != nil {
		return Nil(), err
	}
	return MapV(map[string]Value{"err": StrV(args[0].Display())}), nil
}

func bIsOk(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("is_ok", args, 1, 1); err != nil {
		return Nil(), err
	}
	return BoolV(isOkValue(args[0])), nil
}

func bIsErr(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("is_err", args, 1, 1); err != nil {
		return Nil(), err
	}
	return BoolV(isErrValue(args[0])), nil
}

func bUnwrap(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("unwrap", args, 1, 1); err != nil {
		return Nil(), err
	}
	v := args[0]
	if isOkValue(v) {
		v.Map.Mu.RLock()
		out := v.Map.Vals["ok"]
		v.Map.Mu.RUnlock()
		return out, nil
	}
	if isErrValue(v) {
		v.Map.Mu.RLock()
		msg := v.Map.Vals["err"].Display()
		v.Map.Mu.RUnlock()
		return Nil(), fmt.Errorf("%s", msg)
	}
	return Nil(), fmt.Errorf("unwrap wants ok(v)/err(e), got %s", TypeName(v))
}

func bUnwrapOr(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("unwrap_or", args, 2, 2); err != nil {
		return Nil(), err
	}
	v := args[0]
	if isOkValue(v) {
		v.Map.Mu.RLock()
		out := v.Map.Vals["ok"]
		v.Map.Mu.RUnlock()
		return out, nil
	}
	if isErrValue(v) {
		return args[1], nil
	}
	return Nil(), fmt.Errorf("unwrap_or wants ok(v)/err(e), got %s", TypeName(v))
}

// ---------------------------------------------------------------------------
// Complete-language standard library
// ---------------------------------------------------------------------------

// shared RNG (global math/rand is fine on modern Go, but an explicit
// locked source keeps `seed(n)` deterministic for tests).
var (
	rngMu sync.Mutex
	rng   = rand.New(rand.NewSource(time.Now().UnixNano()))
)

// callValue invokes a func/builtin value (for map/filter/each/reduce/apply).
func (in *Interpreter) callValue(fn Value, args []Value) (Value, error) {
	switch fn.Kind {
	case VFunc:
		if len(args) != len(fn.Func.Params) {
			return Nil(), fmt.Errorf("function %q wants %d args, got %d", fn.Func.Name, len(fn.Func.Params), len(args))
		}
		if err := in.checkFuncArgs(fn.Func, args); err != nil {
			return Nil(), err
		}
		callEnv := newEnvSized(fn.Func.Closure, len(fn.Func.Params))
		callEnv.isCall = true
		for i, p := range fn.Func.Params {
			callEnv.Vars[p] = args[i]
		}
		ret, err := in.execFuncBody(callEnv, fn.Func.Body)
		if err != nil {
			return Nil(), err
		}
		if err := in.checkFuncReturn(fn.Func, ret); err != nil {
			return Nil(), err
		}
		return ret, nil
	case VBuiltin:
		return fn.Builtin.Fn(in, args)
	default:
		return Nil(), fmt.Errorf("cannot call %s", TypeName(fn))
	}
}

// funcArity reports user-func param count, or -1 for builtins.
func funcArity(fn Value) int {
	if fn.Kind == VFunc {
		return len(fn.Func.Params)
	}
	return -1
}

func bBool(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("bool", args, 1, 1); err != nil {
		return Nil(), err
	}
	return BoolV(IsTruthy(args[0])), nil
}

func bChr(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("chr", args, 1, 1); err != nil {
		return Nil(), err
	}
	if args[0].Kind != VInt {
		return Nil(), fmt.Errorf("chr wants int, got %s", TypeName(args[0]))
	}
	return StrV(string(rune(args[0].Int))), nil
}

func bOrd(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("ord", args, 1, 1); err != nil {
		return Nil(), err
	}
	if args[0].Kind != VString {
		return Nil(), fmt.Errorf("ord wants string, got %s", TypeName(args[0]))
	}
	r := []rune(args[0].Str)
	if len(r) == 0 {
		return Nil(), fmt.Errorf("ord of empty string")
	}
	return IntV(int(r[0])), nil
}

func bHex(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("hex", args, 1, 1); err != nil {
		return Nil(), err
	}
	if args[0].Kind != VInt {
		return Nil(), fmt.Errorf("hex wants int, got %s", TypeName(args[0]))
	}
	return StrV(fmt.Sprintf("0x%x", args[0].Int)), nil
}

// --- collections ---

func bInsert(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("insert", args, 3, 3); err != nil {
		return Nil(), err
	}
	if args[0].Kind != VArray {
		return Nil(), fmt.Errorf("insert wants array, got %s", TypeName(args[0]))
	}
	if args[1].Kind != VInt {
		return Nil(), fmt.Errorf("insert index must be int, got %s", TypeName(args[1]))
	}
	args[0].Arr.Mu.Lock()
	defer args[0].Arr.Mu.Unlock()
	idx := args[1].Int
	if idx < 0 {
		idx += len(args[0].Arr.Items)
	}
	if idx < 0 || idx > len(args[0].Arr.Items) {
		return Nil(), fmt.Errorf("insert index %d out of range (len %d)", args[1].Int, len(args[0].Arr.Items))
	}
	args[0].Arr.Items = append(args[0].Arr.Items[:idx:idx], append([]Value{args[2]}, args[0].Arr.Items[idx:]...)...)
	return IntV(len(args[0].Arr.Items)), nil
}

func bRemove(in *Interpreter, args []Value) (Value, error) {
	// Unified remove: remove(array, idx) -> value, or remove(path) -> nil.
	if len(args) == 1 && args[0].Kind == VString {
		if err := os.Remove(args[0].Str); err != nil {
			return Nil(), err
		}
		return Nil(), nil
	}
	if err := needArgs("remove", args, 2, 2); err != nil {
		return Nil(), err
	}
	if args[0].Kind != VArray {
		return Nil(), fmt.Errorf("remove wants array, got %s", TypeName(args[0]))
	}
	if args[1].Kind != VInt {
		return Nil(), fmt.Errorf("remove index must be int, got %s", TypeName(args[1]))
	}
	args[0].Arr.Mu.Lock()
	defer args[0].Arr.Mu.Unlock()
	idx := args[1].Int
	if idx < 0 {
		idx += len(args[0].Arr.Items)
	}
	if idx < 0 || idx >= len(args[0].Arr.Items) {
		return Nil(), fmt.Errorf("remove index %d out of range (len %d)", args[1].Int, len(args[0].Arr.Items))
	}
	v := args[0].Arr.Items[idx]
	args[0].Arr.Items = append(args[0].Arr.Items[:idx], args[0].Arr.Items[idx+1:]...)
	return v, nil
}

func bClear(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("clear", args, 1, 1); err != nil {
		return Nil(), err
	}
	switch args[0].Kind {
	case VArray:
		args[0].Arr.Mu.Lock()
		args[0].Arr.Items = []Value{}
		args[0].Arr.Mu.Unlock()
		return Nil(), nil
	case VMap:
		args[0].Map.Mu.Lock()
		args[0].Map.Vals = map[string]Value{}
		args[0].Map.Mu.Unlock()
		return Nil(), nil
	}
	return Nil(), fmt.Errorf("clear wants array/map, got %s", TypeName(args[0]))
}

func bReverse(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("reverse", args, 1, 1); err != nil {
		return Nil(), err
	}
	if args[0].Kind != VArray {
		return Nil(), fmt.Errorf("reverse wants array, got %s", TypeName(args[0]))
	}
	args[0].Arr.Mu.Lock()
	defer args[0].Arr.Mu.Unlock()
	for i, j := 0, len(args[0].Arr.Items)-1; i < j; i, j = i+1, j-1 {
		args[0].Arr.Items[i], args[0].Arr.Items[j] = args[0].Arr.Items[j], args[0].Arr.Items[i]
	}
	return args[0], nil
}

func sortLess(a, b Value) (bool, error) {
	if isNum(a) && isNum(b) {
		return toFloat(a) < toFloat(b), nil
	}
	if a.Kind == VString && b.Kind == VString {
		return a.Str < b.Str, nil
	}
	return false, fmt.Errorf("sort needs all numbers or all strings, got %s and %s", TypeName(a), TypeName(b))
}

func isSortedFast(items []Value) bool {
	for i := 1; i < len(items); i++ {
		less, err := sortLess(items[i], items[i-1])
		if err != nil {
			return false
		}
		if less {
			return false
		}
	}
	return true
}

// rangeArgs detects `range(...)` calls for loop fast path (v2.4 perf).
func (in *Interpreter) rangeArgs(env *Env, e *frontend.Expr) (int, int, int, bool) {
	if e == nil || e.Kind != frontend.ExprCall || e.Callee == nil || e.Callee.Kind != frontend.ExprVar || e.Callee.Name != "range" {
		return 0, 0, 0, false
	}
	vals := make([]int, 0, len(e.Args))
	for _, a := range e.Args {
		v, err := in.eval(env, a)
		if err != nil || v.Kind != VInt {
			return 0, 0, 0, false
		}
		vals = append(vals, v.Int)
	}
	var start, end, step int
	switch len(vals) {
	case 1:
		start, end, step = 0, vals[0], 1
	case 2:
		start, end, step = vals[0], vals[1], 1
	case 3:
		start, end, step = vals[0], vals[1], vals[2]
		if step == 0 {
			return 0, 0, 0, false
		}
	default:
		return 0, 0, 0, false
	}
	return start, end, step, true
}

func bSort(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("sort", args, 1, 1); err != nil {
		return Nil(), err
	}
	if args[0].Kind != VArray {
		return Nil(), fmt.Errorf("sort wants array, got %s", TypeName(args[0]))
	}
	args[0].Arr.Mu.Lock()
	defer args[0].Arr.Mu.Unlock()
	items := args[0].Arr.Items
	if len(items) < 2 {
		return args[0], nil
	}
	// Skip when already sorted (v2.4 perf: O(n) best case for sorted inputs).
	if isSortedFast(items) {
		return args[0], nil
	}
	// Validate homogeneity once so the sort closure stays error-free
	// and fast (O(n) check + O(n log n) sort instead of O(n^2)).
	allNum := true
	allStr := true
	for _, v := range items {
		if !isNum(v) {
			allNum = false
		}
		if v.Kind != VString {
			allStr = false
		}
		if !allNum && !allStr {
			// Find the offending pair for a helpful error.
			for i := 1; i < len(items); i++ {
				if _, err := sortLess(items[i], items[i-1]); err != nil {
					return Nil(), err
				}
			}
			return Nil(), fmt.Errorf("sort needs all numbers or all strings")
		}
	}
	if allStr && !allNum {
		sort.SliceStable(items, func(a, b int) bool { return items[a].Str < items[b].Str })
		return args[0], nil
	}
	// Numeric (int/float mix): compare as float. Pure-int fast path
	// avoids float conversion.
	pureInt := true
	for _, v := range items {
		if v.Kind != VInt {
			pureInt = false
			break
		}
	}
	if pureInt {
		sort.SliceStable(items, func(a, b int) bool { return items[a].Int < items[b].Int })
		return args[0], nil
	}
	sort.SliceStable(items, func(a, b int) bool { return toFloat(items[a]) < toFloat(items[b]) })
	return args[0], nil
}

// normSlice resolves Python-like start/end (nil end = rest) for length n.
func normSlice(n, start int, end *int) (int, int, error) {
	s := start
	if s < 0 {
		s += n
	}
	if s < 0 {
		s = 0
	}
	if s > n {
		s = n
	}
	e := n
	if end != nil {
		e = *end
		if e < 0 {
			e += n
		}
		if e < 0 {
			e = 0
		}
		if e > n {
			e = n
		}
	}
	if e < s {
		e = s
	}
	return s, e, nil
}

func bSlice(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("slice", args, 2, 3); err != nil {
		return Nil(), err
	}
	if args[1].Kind != VInt {
		return Nil(), fmt.Errorf("slice start must be int, got %s", TypeName(args[1]))
	}
	var endP *int
	if len(args) == 3 {
		if args[2].Kind != VInt {
			return Nil(), fmt.Errorf("slice end must be int, got %s", TypeName(args[2]))
		}
		e := args[2].Int
		endP = &e
	}
	switch args[0].Kind {
	case VArray:
		// Copy only the requested window.
		args[0].Arr.Mu.RLock()
		n := len(args[0].Arr.Items)
		s, e, _ := normSlice(n, args[1].Int, endP)
		out := make([]Value, e-s)
		copy(out, args[0].Arr.Items[s:e])
		args[0].Arr.Mu.RUnlock()
		return ArrV(out), nil
	case VString:
		r := []rune(args[0].Str)
		s, e, _ := normSlice(len(r), args[1].Int, endP)
		return StrV(string(r[s:e])), nil
	}
	return Nil(), fmt.Errorf("slice wants array/string, got %s", TypeName(args[0]))
}

func bDelete(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("delete", args, 2, 2); err != nil {
		return Nil(), err
	}
	if args[0].Kind != VMap {
		return Nil(), fmt.Errorf("delete wants map, got %s", TypeName(args[0]))
	}
	if args[1].Kind != VString {
		return Nil(), fmt.Errorf("delete key must be string, got %s", TypeName(args[1]))
	}
	args[0].Map.Mu.Lock()
	defer args[0].Map.Mu.Unlock()
	_, ok := args[0].Map.Vals[args[1].Str]
	delete(args[0].Map.Vals, args[1].Str)
	return BoolV(ok), nil
}

func bMerge(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("merge", args, 1, -1); err != nil {
		return Nil(), err
	}
	out := map[string]Value{}
	for _, a := range args {
		if a.Kind != VMap {
			return Nil(), fmt.Errorf("merge wants maps, got %s", TypeName(a))
		}
		a.Map.Mu.RLock()
		for k, v := range a.Map.Vals {
			out[k] = v
		}
		a.Map.Mu.RUnlock()
	}
	return MapV(out), nil
}

func bGet(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("get", args, 2, 3); err != nil {
		return Nil(), err
	}
	if args[0].Kind != VMap {
		return Nil(), fmt.Errorf("get wants map, got %s", TypeName(args[0]))
	}
	if args[1].Kind != VString {
		return Nil(), fmt.Errorf("get key must be string, got %s", TypeName(args[1]))
	}
	args[0].Map.Mu.RLock()
	v, ok := args[0].Map.Vals[args[1].Str]
	args[0].Map.Mu.RUnlock()
	if ok {
		return v, nil
	}
	if len(args) == 3 {
		return args[2], nil
	}
	return Nil(), nil
}

func bContains(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("contains", args, 2, 2); err != nil {
		return Nil(), err
	}
	hay, needle := args[0], args[1]
	switch hay.Kind {
	case VString:
		if needle.Kind != VString {
			return Nil(), fmt.Errorf("contains on string needs string needle, got %s", TypeName(needle))
		}
		return BoolV(strings.Contains(hay.Str, needle.Str)), nil
	case VArray:
		hay.Arr.Mu.RLock()
		defer hay.Arr.Mu.RUnlock()
		for _, e := range hay.Arr.Items {
			if deepEqual(e, needle) {
				return BoolV(true), nil
			}
		}
		return BoolV(false), nil
	case VMap:
		if needle.Kind != VString {
			return Nil(), fmt.Errorf("contains on map needs string key, got %s", TypeName(needle))
		}
		hay.Map.Mu.RLock()
		defer hay.Map.Mu.RUnlock()
		_, ok := hay.Map.Vals[needle.Str]
		return BoolV(ok), nil
	}
	return Nil(), fmt.Errorf("contains wants string/array/map, got %s", TypeName(hay))
}

func bIndexOf(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("index_of", args, 2, 2); err != nil {
		return Nil(), err
	}
	hay, needle := args[0], args[1]
	switch hay.Kind {
	case VString:
		if needle.Kind != VString {
			return Nil(), fmt.Errorf("index_of on string needs string needle, got %s", TypeName(needle))
		}
		// rune-based index
		if needle.Str == "" {
			return IntV(0), nil
		}
		byteIdx := strings.Index(hay.Str, needle.Str)
		if byteIdx < 0 {
			return IntV(-1), nil
		}
		return IntV(utf8.RuneCountInString(hay.Str[:byteIdx])), nil
	case VArray:
		hay.Arr.Mu.RLock()
		defer hay.Arr.Mu.RUnlock()
		for i, e := range hay.Arr.Items {
			if deepEqual(e, needle) {
				return IntV(i), nil
			}
		}
		return IntV(-1), nil
	}
	return Nil(), fmt.Errorf("index_of wants string/array, got %s", TypeName(hay))
}

// --- strings ---

func bSplit(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("split", args, 2, 2); err != nil {
		return Nil(), err
	}
	if args[0].Kind != VString || args[1].Kind != VString {
		return Nil(), fmt.Errorf("split wants (string, string), got (%s, %s)", TypeName(args[0]), TypeName(args[1]))
	}
	parts := strings.Split(args[0].Str, args[1].Str)
	out := make([]Value, 0, len(parts))
	for _, p := range parts {
		out = append(out, StrV(p))
	}
	return ArrV(out), nil
}

func bJoin(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("join", args, 2, 2); err != nil {
		return Nil(), err
	}
	if args[0].Kind != VArray {
		return Nil(), fmt.Errorf("join wants array, got %s", TypeName(args[0]))
	}
	if args[1].Kind != VString {
		return Nil(), fmt.Errorf("join separator must be string, got %s", TypeName(args[1]))
	}
	args[0].Arr.Mu.RLock()
	parts := make([]string, len(args[0].Arr.Items))
	for i, e := range args[0].Arr.Items {
		if e.Kind == VString {
			parts[i] = e.Str
		} else {
			parts[i] = e.Display()
		}
	}
	args[0].Arr.Mu.RUnlock()
	return StrV(strings.Join(parts, args[1].Str)), nil
}

func bUpper(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("upper", args, 1, 1); err != nil {
		return Nil(), err
	}
	if args[0].Kind != VString {
		return Nil(), fmt.Errorf("upper wants string, got %s", TypeName(args[0]))
	}
	return StrV(strings.ToUpper(args[0].Str)), nil
}

func bLower(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("lower", args, 1, 1); err != nil {
		return Nil(), err
	}
	if args[0].Kind != VString {
		return Nil(), fmt.Errorf("lower wants string, got %s", TypeName(args[0]))
	}
	return StrV(strings.ToLower(args[0].Str)), nil
}

func bTrim(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("trim", args, 1, 2); err != nil {
		return Nil(), err
	}
	if args[0].Kind != VString {
		return Nil(), fmt.Errorf("trim wants string, got %s", TypeName(args[0]))
	}
	if len(args) == 1 {
		return StrV(strings.TrimSpace(args[0].Str)), nil
	}
	if args[1].Kind != VString {
		return Nil(), fmt.Errorf("trim cutset must be string, got %s", TypeName(args[1]))
	}
	return StrV(strings.Trim(args[0].Str, args[1].Str)), nil
}

func bStartsWith(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("starts_with", args, 2, 2); err != nil {
		return Nil(), err
	}
	if args[0].Kind != VString || args[1].Kind != VString {
		return Nil(), fmt.Errorf("starts_with wants strings, got (%s, %s)", TypeName(args[0]), TypeName(args[1]))
	}
	return BoolV(strings.HasPrefix(args[0].Str, args[1].Str)), nil
}

func bEndsWith(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("ends_with", args, 2, 2); err != nil {
		return Nil(), err
	}
	if args[0].Kind != VString || args[1].Kind != VString {
		return Nil(), fmt.Errorf("ends_with wants strings, got (%s, %s)", TypeName(args[0]), TypeName(args[1]))
	}
	return BoolV(strings.HasSuffix(args[0].Str, args[1].Str)), nil
}

func bReplace(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("replace", args, 3, 3); err != nil {
		return Nil(), err
	}
	for _, a := range args {
		if a.Kind != VString {
			return Nil(), fmt.Errorf("replace wants strings, got %s", TypeName(a))
		}
	}
	return StrV(strings.ReplaceAll(args[0].Str, args[1].Str, args[2].Str)), nil
}

func bSubstr(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("substr", args, 2, 3); err != nil {
		return Nil(), err
	}
	if args[0].Kind != VString {
		return Nil(), fmt.Errorf("substr wants string, got %s", TypeName(args[0]))
	}
	if args[1].Kind != VInt {
		return Nil(), fmt.Errorf("substr start must be int, got %s", TypeName(args[1]))
	}
	r := []rune(args[0].Str)
	if len(args) == 3 {
		if args[2].Kind != VInt {
			return Nil(), fmt.Errorf("substr length must be int, got %s", TypeName(args[2]))
		}
		if args[2].Int < 0 {
			return Nil(), fmt.Errorf("substr length must be >= 0")
		}
		// substr(s, start, length): negative start counts from the end.
		s := args[1].Int
		if s < 0 {
			s += len(r)
		}
		e := s + args[2].Int
		s2, e2, _ := normSlice(len(r), s, &e)
		return StrV(string(r[s2:e2])), nil
	}
	s, e, _ := normSlice(len(r), args[1].Int, nil)
	return StrV(string(r[s:e])), nil
}

func bRepeat(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("repeat", args, 2, 2); err != nil {
		return Nil(), err
	}
	if args[0].Kind != VString {
		return Nil(), fmt.Errorf("repeat wants string, got %s", TypeName(args[0]))
	}
	if args[1].Kind != VInt || args[1].Int < 0 {
		return Nil(), fmt.Errorf("repeat count must be int >= 0")
	}
	return StrV(strings.Repeat(args[0].Str, args[1].Int)), nil
}

// --- math ---

func bAbs(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("abs", args, 1, 1); err != nil {
		return Nil(), err
	}
	switch args[0].Kind {
	case VInt:
		if args[0].Int < 0 {
			return IntV(-args[0].Int), nil
		}
		return args[0], nil
	case VFloat:
		return FloatV(math.Abs(args[0].Float)), nil
	}
	return Nil(), fmt.Errorf("abs wants number, got %s", TypeName(args[0]))
}

func minMaxVals(name string, args []Value, wantMin bool) (Value, error) {
	if err := needArgs(name, args, 1, -1); err != nil {
		return Nil(), err
	}
	items := args
	if len(args) == 1 && args[0].Kind == VArray {
		args[0].Arr.Mu.RLock()
		items = make([]Value, len(args[0].Arr.Items))
		copy(items, args[0].Arr.Items)
		args[0].Arr.Mu.RUnlock()
		if len(items) == 0 {
			return Nil(), fmt.Errorf("%s of empty array", name)
		}
	}
	allInt := true
	allStr := true
	for _, a := range items {
		if a.Kind != VInt {
			allInt = false
		}
		if a.Kind != VString {
			allStr = false
		}
		if !isNum(a) && a.Kind != VString {
			return Nil(), fmt.Errorf("%s wants numbers or strings, got %s", name, TypeName(a))
		}
	}
	if allStr {
		best := items[0].Str
		for _, a := range items[1:] {
			if wantMin == (a.Str < best) {
				best = a.Str
			}
		}
		return StrV(best), nil
	}
	if allInt {
		best := items[0].Int
		for _, a := range items[1:] {
			if !isNum(a) {
				return Nil(), fmt.Errorf("%s: cannot mix strings and numbers", name)
			}
			if wantMin == (a.Int < best) {
				best = a.Int
			}
		}
		return IntV(best), nil
	}
	bestF := toFloat(items[0])
	if !isNum(items[0]) {
		return Nil(), fmt.Errorf("%s: cannot mix strings and numbers", name)
	}
	for _, a := range items[1:] {
		if !isNum(a) {
			return Nil(), fmt.Errorf("%s: cannot mix strings and numbers", name)
		}
		f := toFloat(a)
		if wantMin == (f < bestF) {
			bestF = f
		}
	}
	return FloatV(bestF), nil
}

func bMin(in *Interpreter, args []Value) (Value, error) { return minMaxVals("min", args, true) }
func bMax(in *Interpreter, args []Value) (Value, error) { return minMaxVals("max", args, false) }

func bFloor(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("floor", args, 1, 1); err != nil {
		return Nil(), err
	}
	if !isNum(args[0]) {
		return Nil(), fmt.Errorf("floor wants number, got %s", TypeName(args[0]))
	}
	return IntV(int(math.Floor(toFloat(args[0])))), nil
}

func bCeil(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("ceil", args, 1, 1); err != nil {
		return Nil(), err
	}
	if !isNum(args[0]) {
		return Nil(), fmt.Errorf("ceil wants number, got %s", TypeName(args[0]))
	}
	return IntV(int(math.Ceil(toFloat(args[0])))), nil
}

func bRound(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("round", args, 1, 1); err != nil {
		return Nil(), err
	}
	if !isNum(args[0]) {
		return Nil(), fmt.Errorf("round wants number, got %s", TypeName(args[0]))
	}
	return IntV(int(math.Round(toFloat(args[0])))), nil
}

func bSqrt(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("sqrt", args, 1, 1); err != nil {
		return Nil(), err
	}
	if !isNum(args[0]) {
		return Nil(), fmt.Errorf("sqrt wants number, got %s", TypeName(args[0]))
	}
	f := toFloat(args[0])
	if f < 0 {
		return Nil(), fmt.Errorf("sqrt of negative number")
	}
	return FloatV(math.Sqrt(f)), nil
}

func bPow(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("pow", args, 2, 2); err != nil {
		return Nil(), err
	}
	if !isNum(args[0]) || !isNum(args[1]) {
		return Nil(), fmt.Errorf("pow wants numbers, got (%s, %s)", TypeName(args[0]), TypeName(args[1]))
	}
	if args[0].Kind == VInt && args[1].Kind == VInt && args[1].Int >= 0 {
		return IntV(intPow(args[0].Int, args[1].Int)), nil
	}
	return FloatV(math.Pow(toFloat(args[0]), toFloat(args[1]))), nil
}

func bPi(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("pi", args, 0, 0); err != nil {
		return Nil(), err
	}
	return FloatV(math.Pi), nil
}

func bNow(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("now", args, 0, 0); err != nil {
		return Nil(), err
	}
	return IntV(int(time.Now().UnixMilli())), nil
}

func bRand(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("rand", args, 0, 0); err != nil {
		return Nil(), err
	}
	rngMu.Lock()
	f := rng.Float64()
	rngMu.Unlock()
	return FloatV(f), nil
}

func bRandint(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("randint", args, 2, 2); err != nil {
		return Nil(), err
	}
	if args[0].Kind != VInt || args[1].Kind != VInt {
		return Nil(), fmt.Errorf("randint wants ints, got (%s, %s)", TypeName(args[0]), TypeName(args[1]))
	}
	lo, hi := args[0].Int, args[1].Int
	if hi < lo {
		return Nil(), fmt.Errorf("randint needs lo <= hi")
	}
	rngMu.Lock()
	n := rng.Intn(hi-lo+1) + lo
	rngMu.Unlock()
	return IntV(n), nil
}

func bSeed(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("seed", args, 1, 1); err != nil {
		return Nil(), err
	}
	if args[0].Kind != VInt {
		return Nil(), fmt.Errorf("seed wants int, got %s", TypeName(args[0]))
	}
	rngMu.Lock()
	rng = rand.New(rand.NewSource(int64(args[0].Int)))
	rngMu.Unlock()
	return Nil(), nil
}

// --- bitwise ---

func needInts(name string, args []Value) error {
	for _, a := range args {
		if a.Kind != VInt {
			return fmt.Errorf("%s wants ints, got %s", name, TypeName(a))
		}
	}
	return nil
}

func bBitAnd(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("bit_and", args, 2, 2); err != nil {
		return Nil(), err
	}
	if err := needInts("bit_and", args); err != nil {
		return Nil(), err
	}
	return IntV(args[0].Int & args[1].Int), nil
}

func bBitOr(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("bit_or", args, 2, 2); err != nil {
		return Nil(), err
	}
	if err := needInts("bit_or", args); err != nil {
		return Nil(), err
	}
	return IntV(args[0].Int | args[1].Int), nil
}

func bBitXor(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("bit_xor", args, 2, 2); err != nil {
		return Nil(), err
	}
	if err := needInts("bit_xor", args); err != nil {
		return Nil(), err
	}
	return IntV(args[0].Int ^ args[1].Int), nil
}

func bBitShl(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("bit_shl", args, 2, 2); err != nil {
		return Nil(), err
	}
	if err := needInts("bit_shl", args); err != nil {
		return Nil(), err
	}
	if args[1].Int < 0 {
		return Nil(), fmt.Errorf("bit_shl shift must be >= 0")
	}
	return IntV(args[0].Int << uint(args[1].Int)), nil
}

func bBitShr(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("bit_shr", args, 2, 2); err != nil {
		return Nil(), err
	}
	if err := needInts("bit_shr", args); err != nil {
		return Nil(), err
	}
	if args[1].Int < 0 {
		return Nil(), fmt.Errorf("bit_shr shift must be >= 0")
	}
	return IntV(args[0].Int >> uint(args[1].Int)), nil
}

func bBitNot(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("bit_not", args, 1, 1); err != nil {
		return Nil(), err
	}
	if err := needInts("bit_not", args); err != nil {
		return Nil(), err
	}
	return IntV(^args[0].Int), nil
}

// --- functional ---

func bMapFn(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("map", args, 2, 2); err != nil {
		return Nil(), err
	}
	if args[0].Kind != VArray {
		return Nil(), fmt.Errorf("map wants array, got %s", TypeName(args[0]))
	}
	if args[1].Kind != VFunc && args[1].Kind != VBuiltin {
		return Nil(), fmt.Errorf("map wants func, got %s", TypeName(args[1]))
	}
	args[0].Arr.Mu.RLock()
	items := make([]Value, len(args[0].Arr.Items))
	copy(items, args[0].Arr.Items)
	args[0].Arr.Mu.RUnlock()
	ar := funcArity(args[1])
	out := make([]Value, 0, len(items))
	for i, e := range items {
		var v Value
		var err error
		if ar == 2 {
			v, err = in.callValue(args[1], []Value{e, IntV(i)})
		} else {
			v, err = in.callValue(args[1], []Value{e})
		}
		if err != nil {
			return Nil(), err
		}
		out = append(out, v)
	}
	return ArrV(out), nil
}

func bFilter(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("filter", args, 2, 2); err != nil {
		return Nil(), err
	}
	if args[0].Kind != VArray {
		return Nil(), fmt.Errorf("filter wants array, got %s", TypeName(args[0]))
	}
	if args[1].Kind != VFunc && args[1].Kind != VBuiltin {
		return Nil(), fmt.Errorf("filter wants func, got %s", TypeName(args[1]))
	}
	args[0].Arr.Mu.RLock()
	items := make([]Value, len(args[0].Arr.Items))
	copy(items, args[0].Arr.Items)
	args[0].Arr.Mu.RUnlock()
	ar := funcArity(args[1])
	out := []Value{}
	for i, e := range items {
		var v Value
		var err error
		if ar == 2 {
			v, err = in.callValue(args[1], []Value{e, IntV(i)})
		} else {
			v, err = in.callValue(args[1], []Value{e})
		}
		if err != nil {
			return Nil(), err
		}
		if IsTruthy(v) {
			out = append(out, e)
		}
	}
	return ArrV(out), nil
}

func bEach(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("each", args, 2, 2); err != nil {
		return Nil(), err
	}
	if args[0].Kind != VArray {
		return Nil(), fmt.Errorf("each wants array, got %s", TypeName(args[0]))
	}
	if args[1].Kind != VFunc && args[1].Kind != VBuiltin {
		return Nil(), fmt.Errorf("each wants func, got %s", TypeName(args[1]))
	}
	args[0].Arr.Mu.RLock()
	items := make([]Value, len(args[0].Arr.Items))
	copy(items, args[0].Arr.Items)
	args[0].Arr.Mu.RUnlock()
	ar := funcArity(args[1])
	for i, e := range items {
		var err error
		if ar == 2 {
			_, err = in.callValue(args[1], []Value{e, IntV(i)})
		} else {
			_, err = in.callValue(args[1], []Value{e})
		}
		if err != nil {
			return Nil(), err
		}
	}
	return Nil(), nil
}

func bReduce(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("reduce", args, 2, 3); err != nil {
		return Nil(), err
	}
	if args[0].Kind != VArray {
		return Nil(), fmt.Errorf("reduce wants array, got %s", TypeName(args[0]))
	}
	if args[1].Kind != VFunc && args[1].Kind != VBuiltin {
		return Nil(), fmt.Errorf("reduce wants func, got %s", TypeName(args[1]))
	}
	args[0].Arr.Mu.RLock()
	items := make([]Value, len(args[0].Arr.Items))
	copy(items, args[0].Arr.Items)
	args[0].Arr.Mu.RUnlock()
	var acc Value
	start := 0
	if len(args) == 3 {
		acc = args[2]
	} else {
		if len(items) == 0 {
			return Nil(), fmt.Errorf("reduce of empty array needs init")
		}
		acc = items[0]
		start = 1
	}
	for _, e := range items[start:] {
		var err error
		acc, err = in.callValue(args[1], []Value{acc, e})
		if err != nil {
			return Nil(), err
		}
	}
	return acc, nil
}

func bApply(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("apply", args, 2, 2); err != nil {
		return Nil(), err
	}
	if args[0].Kind != VFunc && args[0].Kind != VBuiltin {
		return Nil(), fmt.Errorf("apply wants func, got %s", TypeName(args[0]))
	}
	if args[1].Kind != VArray {
		return Nil(), fmt.Errorf("apply args must be array, got %s", TypeName(args[1]))
	}
	args[1].Arr.Mu.RLock()
	callArgs := make([]Value, len(args[1].Arr.Items))
	copy(callArgs, args[1].Arr.Items)
	args[1].Arr.Mu.RUnlock()
	return in.callValue(args[0], callArgs)
}

// --- JSON ---

func valueToJSON(v Value) (any, error) {
	switch v.Kind {
	case VNil:
		return nil, nil
	case VBool:
		return v.Bool, nil
	case VInt:
		return v.Int, nil
	case VFloat:
		return v.Float, nil
	case VString:
		return v.Str, nil
	case VArray:
		v.Arr.Mu.RLock()
		defer v.Arr.Mu.RUnlock()
		out := make([]any, 0, len(v.Arr.Items))
		for _, e := range v.Arr.Items {
			j, err := valueToJSON(e)
			if err != nil {
				return nil, err
			}
			out = append(out, j)
		}
		return out, nil
	case VMap:
		v.Map.Mu.RLock()
		defer v.Map.Mu.RUnlock()
		out := make(map[string]any, len(v.Map.Vals))
		for k, e := range v.Map.Vals {
			j, err := valueToJSON(e)
			if err != nil {
				return nil, err
			}
			out[k] = j
		}
		return out, nil
	default:
		return nil, fmt.Errorf("cannot encode %s as json", TypeName(v))
	}
}

func jsonToValue(a any) Value {
	switch t := a.(type) {
	case nil:
		return Nil()
	case bool:
		return BoolV(t)
	case float64:
		if t == math.Trunc(t) && t >= -9e15 && t <= 9e15 {
			return IntV(int(t))
		}
		return FloatV(t)
	case string:
		return StrV(t)
	case []any:
		out := make([]Value, 0, len(t))
		for _, e := range t {
			out = append(out, jsonToValue(e))
		}
		return ArrV(out)
	case map[string]any:
		m := make(map[string]Value, len(t))
		for k, e := range t {
			m[k] = jsonToValue(e)
		}
		return MapV(m)
	default:
		return StrV(fmt.Sprint(t))
	}
}

func bJsonStringify(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("json_stringify", args, 1, 1); err != nil {
		return Nil(), err
	}
	j, err := valueToJSON(args[0])
	if err != nil {
		return Nil(), err
	}
	data, err := json.Marshal(j)
	if err != nil {
		return Nil(), err
	}
	return StrV(string(data)), nil
}

func bJsonParse(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("json_parse", args, 1, 1); err != nil {
		return Nil(), err
	}
	if args[0].Kind != VString {
		return Nil(), fmt.Errorf("json_parse wants string, got %s", TypeName(args[0]))
	}
	var a any
	if err := json.Unmarshal([]byte(args[0].Str), &a); err != nil {
		return Nil(), fmt.Errorf("json_parse failed: %v", err)
	}
	return jsonToValue(a), nil
}

// --- I/O & OS ---

func bInput(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("input", args, 0, 1); err != nil {
		return Nil(), err
	}
	if len(args) == 1 {
		in.outMu.Lock()
		fmt.Print(args[0].Display())
		in.outMu.Unlock()
	}
	rd := bufio.NewReader(os.Stdin)
	line, err := rd.ReadString('\n')
	if err != nil && len(line) == 0 {
		return Nil(), fmt.Errorf("input failed: %v", err)
	}
	line = strings.TrimRight(line, "\r\n")
	return StrV(line), nil
}

func bReadFile(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("read_file", args, 1, 1); err != nil {
		return Nil(), err
	}
	if args[0].Kind != VString {
		return Nil(), fmt.Errorf("read_file wants path string, got %s", TypeName(args[0]))
	}
	data, err := os.ReadFile(args[0].Str)
	if err != nil {
		return Nil(), err
	}
	return StrV(string(data)), nil
}

func bWriteFile(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("write_file", args, 2, 2); err != nil {
		return Nil(), err
	}
	if args[0].Kind != VString {
		return Nil(), fmt.Errorf("write_file wants path string, got %s", TypeName(args[0]))
	}
	if args[1].Kind != VString {
		return Nil(), fmt.Errorf("write_file data must be string, got %s", TypeName(args[1]))
	}
	if err := os.WriteFile(args[0].Str, []byte(args[1].Str), 0o644); err != nil {
		return Nil(), err
	}
	return IntV(len(args[1].Str)), nil
}

func bAppendFile(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("append_file", args, 2, 2); err != nil {
		return Nil(), err
	}
	if args[0].Kind != VString {
		return Nil(), fmt.Errorf("append_file wants path string, got %s", TypeName(args[0]))
	}
	if args[1].Kind != VString {
		return Nil(), fmt.Errorf("append_file data must be string, got %s", TypeName(args[1]))
	}
	f, err := os.OpenFile(args[0].Str, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return Nil(), err
	}
	n, err := f.WriteString(args[1].Str)
	cerr := f.Close()
	if err != nil {
		return Nil(), err
	}
	if cerr != nil {
		return Nil(), cerr
	}
	return IntV(n), nil
}

func bExists(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("exists", args, 1, 1); err != nil {
		return Nil(), err
	}
	if args[0].Kind != VString {
		return Nil(), fmt.Errorf("exists wants path string, got %s", TypeName(args[0]))
	}
	_, err := os.Stat(args[0].Str)
	return BoolV(err == nil), nil
}

func bListDir(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("list_dir", args, 0, 1); err != nil {
		return Nil(), err
	}
	dir := "."
	if len(args) == 1 {
		if args[0].Kind != VString {
			return Nil(), fmt.Errorf("list_dir wants path string, got %s", TypeName(args[0]))
		}
		dir = args[0].Str
	}
	ents, err := os.ReadDir(dir)
	if err != nil {
		return Nil(), err
	}
	out := make([]Value, 0, len(ents))
	for _, e := range ents {
		out = append(out, StrV(e.Name()))
	}
	return ArrV(out), nil
}

func bMkdir(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("mkdir", args, 1, 1); err != nil {
		return Nil(), err
	}
	if args[0].Kind != VString {
		return Nil(), fmt.Errorf("mkdir wants path string, got %s", TypeName(args[0]))
	}
	if err := os.MkdirAll(args[0].Str, 0o755); err != nil {
		return Nil(), err
	}
	return Nil(), nil
}

func bRemovePath(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("remove_file", args, 1, 1); err != nil {
		return Nil(), err
	}
	if args[0].Kind != VString {
		return Nil(), fmt.Errorf("remove_file wants path string, got %s", TypeName(args[0]))
	}
	if err := os.Remove(args[0].Str); err != nil {
		return Nil(), err
	}
	return Nil(), nil
}

func bExit(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("exit", args, 0, 1); err != nil {
		return Nil(), err
	}
	code := 0
	if len(args) == 1 {
		if args[0].Kind != VInt {
			return Nil(), fmt.Errorf("exit code must be int, got %s", TypeName(args[0]))
		}
		code = args[0].Int
	}
	os.Exit(code)
	return Nil(), nil
}

func bArgv(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("argv", args, 0, 0); err != nil {
		return Nil(), err
	}
	out := make([]Value, 0, len(os.Args)-1)
	for _, a := range os.Args[1:] {
		out = append(out, StrV(a))
	}
	return ArrV(out), nil
}

func bEnv(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("env", args, 1, 2); err != nil {
		return Nil(), err
	}
	if args[0].Kind != VString {
		return Nil(), fmt.Errorf("env name must be string, got %s", TypeName(args[0]))
	}
	v, ok := os.LookupEnv(args[0].Str)
	if ok {
		return StrV(v), nil
	}
	if len(args) == 2 {
		return args[1], nil
	}
	return StrV(""), nil
}

// --- channels (non-blocking + introspection) ---

func bTrySend(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("try_send", args, 2, 2); err != nil {
		return Nil(), err
	}
	if args[0].Kind != VChan {
		return Nil(), fmt.Errorf("try_send wants chan, got %s", TypeName(args[0]))
	}
	args[0].Chan.Mu.Lock()
	ch := args[0].Chan.Ch
	closed := args[0].Chan.Closed
	args[0].Chan.Mu.Unlock()
	if closed {
		return Nil(), fmt.Errorf("send on closed channel")
	}
	select {
	case ch <- args[1]:
		return BoolV(true), nil
	default:
		return BoolV(false), nil
	}
}

func bTryRecv(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("try_recv", args, 1, 1); err != nil {
		return Nil(), err
	}
	if args[0].Kind != VChan {
		return Nil(), fmt.Errorf("try_recv wants chan, got %s", TypeName(args[0]))
	}
	select {
	case v, ok := <-args[0].Chan.Ch:
		if !ok {
			return Nil(), nil
		}
		return v, nil
	default:
		return Nil(), nil
	}
}

func bChanLen(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("chan_len", args, 1, 1); err != nil {
		return Nil(), err
	}
	if args[0].Kind != VChan {
		return Nil(), fmt.Errorf("chan_len wants chan, got %s", TypeName(args[0]))
	}
	return IntV(len(args[0].Chan.Ch)), nil
}

func bChanCap(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("chan_cap", args, 1, 1); err != nil {
		return Nil(), err
	}
	if args[0].Kind != VChan {
		return Nil(), fmt.Errorf("chan_cap wants chan, got %s", TypeName(args[0]))
	}
	return IntV(cap(args[0].Chan.Ch)), nil
}

func bChanClosed(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("chan_closed", args, 1, 1); err != nil {
		return Nil(), err
	}
	if args[0].Kind != VChan {
		return Nil(), fmt.Errorf("chan_closed wants chan, got %s", TypeName(args[0]))
	}
	args[0].Chan.Mu.Lock()
	closed := args[0].Chan.Closed
	args[0].Chan.Mu.Unlock()
	return BoolV(closed), nil
}

// bRecvTimeout blocks up to ms milliseconds for a value:
// `recv_timeout(ch, ms)` returns the value, or nil on timeout
// (or when a closed channel is already drained, like `recv`).
func bRecvTimeout(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("recv_timeout", args, 2, 2); err != nil {
		return Nil(), err
	}
	if args[0].Kind != VChan {
		return Nil(), fmt.Errorf("recv_timeout wants chan, got %s", TypeName(args[0]))
	}
	ms, err := toMillis(args[1])
	if err != nil {
		return Nil(), err
	}
	select {
	case v, ok := <-args[0].Chan.Ch:
		if !ok {
			return Nil(), nil
		}
		return v, nil
	case <-time.After(time.Duration(ms) * time.Millisecond):
		return Nil(), nil
	}
}

// bSendTimeout blocks up to ms milliseconds trying to send:
// `send_timeout(ch, v, ms)` returns true if sent, false on timeout.
func bSendTimeout(in *Interpreter, args []Value) (Value, error) {
	if err := needArgs("send_timeout", args, 3, 3); err != nil {
		return Nil(), err
	}
	if args[0].Kind != VChan {
		return Nil(), fmt.Errorf("send_timeout wants chan, got %s", TypeName(args[0]))
	}
	ms, err := toMillis(args[2])
	if err != nil {
		return Nil(), err
	}
	args[0].Chan.Mu.Lock()
	ch := args[0].Chan.Ch
	closed := args[0].Chan.Closed
	args[0].Chan.Mu.Unlock()
	if closed {
		return Nil(), fmt.Errorf("send on closed channel")
	}
	select {
	case ch <- args[1]:
		return BoolV(true), nil
	case <-time.After(time.Duration(ms) * time.Millisecond):
		return BoolV(false), nil
	}
}
