// VM + bundle I/O for the ks-fusion compiler (v0.1 subset).
package compiler

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// ---------------------------------------------------------------------------
// Runtime values (subset; no channels yet)
// ---------------------------------------------------------------------------

type VKind int

const (
	VNil VKind = iota
	VBool
	VInt
	VFloat
	VString
	VArray
	VMap
	VFunc
	VBuiltin
)

type Val struct {
	Kind    VKind
	Bool    bool
	Int     int
	Float   float64
	Str     string
	Arr     []Val
	Map     map[string]Val
	Func    int
	Builtin string
}

func Nil() Val               { return Val{Kind: VNil} }
func BoolV(b bool) Val       { return Val{Kind: VBool, Bool: b} }
func IntV(n int) Val         { return Val{Kind: VInt, Int: n} }
func FloatV(f float64) Val   { return Val{Kind: VFloat, Float: f} }
func StrV(s string) Val      { return Val{Kind: VString, Str: s} }
func ArrV(a []Val) Val       { return Val{Kind: VArray, Arr: a} }
func MapV(m map[string]Val) Val {
	if m == nil {
		m = map[string]Val{}
	}
	return Val{Kind: VMap, Map: m}
}

func typeName(v Val) string {
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
	}
	return "unknown"
}

func (v Val) display() string {
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
		parts := make([]string, len(v.Arr))
		for i, e := range v.Arr {
			parts[i] = e.inspect()
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case VMap:
		keys := make([]string, 0, len(v.Map))
		for k := range v.Map {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		parts := make([]string, 0, len(keys))
		for _, k := range keys {
			parts = append(parts, strconv.Quote(k)+": "+v.Map[k].inspect())
		}
		return "{" + strings.Join(parts, ", ") + "}"
	case VFunc:
		return "<func>"
	case VBuiltin:
		return "<builtin " + v.Builtin + ">"
	}
	return "nil"
}

func (v Val) inspect() string {
	if v.Kind == VString {
		return strconv.Quote(v.Str)
	}
	return v.display()
}

func isTruthy(v Val) bool {
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
		return len(v.Arr) != 0
	case VMap:
		return len(v.Map) != 0
	default:
		return true
	}
}

func deepEqual(a, b Val) bool {
	if a.Kind != b.Kind {
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
		if len(a.Arr) != len(b.Arr) {
			return false
		}
		for i := range a.Arr {
			if !deepEqual(a.Arr[i], b.Arr[i]) {
				return false
			}
		}
		return true
	case VMap:
		if len(a.Map) != len(b.Map) {
			return false
		}
		for k, av := range a.Map {
			bv, ok := b.Map[k]
			if !ok || !deepEqual(av, bv) {
				return false
			}
		}
		return true
	case VFunc:
		return a.Func == b.Func
	case VBuiltin:
		return a.Builtin == b.Builtin
	}
	return false
}

func isNum(v Val) bool { return v.Kind == VInt || v.Kind == VFloat }
func toFloat(v Val) float64 {
	if v.Kind == VFloat {
		return v.Float
	}
	return float64(v.Int)
}

// ---------------------------------------------------------------------------
// Bundle I/O + disassembler
// ---------------------------------------------------------------------------

// Save writes a bundle to path (JSON, 0o644).
func Save(b *Bundle, path string) error {
	b.Format = Format
	data, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o644)
}

// Load reads a bundle file, validating the format tag.
func Load(path string) (*Bundle, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Unmarshal(data, path)
}

// Unmarshal parses bundle bytes.
func Unmarshal(data []byte, where string) (*Bundle, error) {
	var b Bundle
	if err := json.Unmarshal(data, &b); err != nil {
		return nil, fmt.Errorf("bad bytecode bundle %s: %w", where, err)
	}
	if b.Format != Format {
		return nil, fmt.Errorf("bad bytecode bundle %s: unknown format %q", where, b.Format)
	}
	if len(b.Funcs) == 0 {
		return nil, fmt.Errorf("bad bytecode bundle %s: no functions", where)
	}
	if b.Main < 0 || b.Main >= len(b.Funcs) {
		return nil, fmt.Errorf("bad bytecode bundle %s: bad main index", where)
	}
	return &b, nil
}

// Marshal returns the encoded bundle.
func Marshal(b *Bundle) ([]byte, error) {
	b.Format = Format
	return json.MarshalIndent(b, "", "  ")
}

// Disassemble renders human-readable bytecode (for `fusion compile --dis`).
func Disassemble(b *Bundle) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "== %s (%s), %d funcs, %d globals ==\n", b.Name, b.Format, len(b.Funcs), len(b.Globals))
	for fi, f := range b.Funcs {
		fmt.Fprintf(&sb, "-- func %d %s(%s) --\n", fi, f.Name, strings.Join(f.Params, ", "))
		for i, in := range f.Chunk.Code {
			arg := ""
			switch in.Op {
			case OpConst:
				if in.Arg >= 0 && in.Arg < len(f.Chunk.Consts) {
					cc := f.Chunk.Consts[in.Arg]
					arg = fmt.Sprintf("%d (%s)", in.Arg, constString(cc))
				} else {
					arg = fmt.Sprintf("%d", in.Arg)
				}
			case OpGetGlobal, OpSetGlobal, OpDefineGlobal, OpGetLocal, OpSetLocal,
				OpMakeFunc, OpPrint, OpArray, OpMap, OpCall,
				OpJump, OpJumpIfFalse, OpJumpIfTrue:
				if in.Name != "" {
					arg = fmt.Sprintf("%d <%s>", in.Arg, in.Name)
				} else {
					arg = fmt.Sprintf("%d", in.Arg)
				}
			}
			fmt.Fprintf(&sb, "%04d %-12s %s\n", i, in.Op, arg)
		}
	}
	return sb.String()
}

func constString(cc Const) string {
	switch cc.Kind {
	case CKNil:
		return "nil"
	case CKBool:
		if cc.Bool {
			return "true"
		}
		return "false"
	case CKInt:
		return strconv.Itoa(cc.Int)
	case CKFloat:
		return strconv.FormatFloat(cc.Float, 'f', -1, 64)
	case CKString:
		return strconv.Quote(cc.Str)
	case CKFunc:
		return fmt.Sprintf("func#%d", cc.Func)
	}
	return cc.Kind
}

// ---------------------------------------------------------------------------
// VM
// ---------------------------------------------------------------------------

const (
	maxFrames = 1024
	maxSteps  = 20_000_000
)

type frame struct {
	funcIndex int
	ip        int
	base      int
}

type VM struct {
	bundle   *Bundle
	globals  []Val
	defined  []bool
	stack    []Val
	frames   []frame
	builtins map[string]func(args []Val) (Val, error)
}

func newVM(b *Bundle) *VM {
	vm := &VM{
		bundle:  b,
		globals: make([]Val, len(b.Globals)),
		defined: make([]bool, len(b.Globals)),
	}
	vm.builtinsInit(vm)
	return vm
}

func (vm *VM) builtinsInit(_ *VM) {
	vm.builtins = map[string]func(args []Val) (Val, error){
		"assert":             bAssert,
		"len":                bLen,
		"range":              bRange,
		"str":                bStr,
		"int":                bInt,
		"float":              bFloat,
		"type":               bType,
		"__iter_len":         bIterLen,
		"__iter_get":         bIterGet,
		"__iter_key":         bIterKey,
		"__iter_val":         bIterVal,
		"__map_keys_or_nil":  bMapKeysOrNil,
	}
}

func (vm *VM) push(v Val) { vm.stack = append(vm.stack, v) }

func (vm *VM) pop() (Val, error) {
	if len(vm.stack) == 0 {
		return Nil(), fmt.Errorf("stack underflow")
	}
	v := vm.stack[len(vm.stack)-1]
	vm.stack = vm.stack[:len(vm.stack)-1]
	return v, nil
}

func (vm *VM) peek() (Val, error) {
	if len(vm.stack) == 0 {
		return Nil(), fmt.Errorf("stack underflow")
	}
	return vm.stack[len(vm.stack)-1], nil
}

func constToVal(cc Const) (Val, error) {
	switch cc.Kind {
	case CKNil:
		return Nil(), nil
	case CKBool:
		return BoolV(cc.Bool), nil
	case CKInt:
		return IntV(cc.Int), nil
	case CKFloat:
		return FloatV(cc.Float), nil
	case CKString:
		return StrV(cc.Str), nil
	case CKFunc:
		return Val{Kind: VFunc, Func: cc.Func}, nil
	}
	return Nil(), fmt.Errorf("bad const kind %q", cc.Kind)
}

// Run executes a bundle to completion.
func Run(b *Bundle) error {
	vm := newVM(b)
	vm.frames = append(vm.frames, frame{funcIndex: b.Main, ip: 0, base: 0})
	steps := 0
	for len(vm.frames) > 0 {
		steps++
		if steps > maxSteps {
			return fmt.Errorf("execution limit exceeded (infinite loop?)")
		}
		fr := &vm.frames[len(vm.frames)-1]
		fn := &vm.bundle.Funcs[fr.funcIndex]
		if fr.ip < 0 || fr.ip >= len(fn.Chunk.Code) {
			return fmt.Errorf("bad jump in %s", fn.Name)
		}
		in := fn.Chunk.Code[fr.ip]
		fr.ip++
		if err := vm.exec(in); err != nil {
			if in.Line > 0 {
				return fmt.Errorf("line %d: %v", in.Line, err)
			}
			return err
		}
	}
	return nil
}

// RunSource compiles and runs source text (used by tests and --run).
func RunSource(src, path string) error {
	b, err := CompileSource(src, path)
	if err != nil {
		return err
	}
	return Run(b)
}

// RunFile loads a .ksb file and runs it.
func RunFile(path string) error {
	b, err := Load(path)
	if err != nil {
		return err
	}
	return Run(b)
}

func (vm *VM) exec(in Instr) error {
	fr := &vm.frames[len(vm.frames)-1]
	fn := &vm.bundle.Funcs[fr.funcIndex]
	switch in.Op {
	case OpConst:
		if in.Arg < 0 || in.Arg >= len(fn.Chunk.Consts) {
			return fmt.Errorf("bad const index %d", in.Arg)
		}
		v, err := constToVal(fn.Chunk.Consts[in.Arg])
		if err != nil {
			return err
		}
		vm.push(v)
	case OpGetGlobal:
		if in.Arg < 0 || in.Arg >= len(vm.globals) {
			return fmt.Errorf("bad global index %d", in.Arg)
		}
		if vm.defined[in.Arg] {
			vm.push(vm.globals[in.Arg])
			return nil
		}
		if bf, ok := vm.builtins[in.Name]; ok {
			_ = bf
			vm.push(Val{Kind: VBuiltin, Builtin: in.Name})
			return nil
		}
		// builtin by const-name fallback (e.g. __iter_*)
		if in.Name != "" {
			if _, ok := vm.builtins[in.Name]; ok {
				vm.push(Val{Kind: VBuiltin, Builtin: in.Name})
				return nil
			}
		}
		return fmt.Errorf("unknown variable %q", in.Name)
	case OpDefineGlobal, OpSetGlobal:
		v, err := vm.pop()
		if err != nil {
			return err
		}
		if in.Arg < 0 || in.Arg >= len(vm.globals) {
			return fmt.Errorf("bad global index %d", in.Arg)
		}
		vm.globals[in.Arg] = v
		vm.defined[in.Arg] = true
	case OpGetLocal:
		idx := fr.base + in.Arg
		if idx < 0 || idx >= len(vm.stack) {
			return fmt.Errorf("bad local slot %d", in.Arg)
		}
		vm.push(vm.stack[idx])
	case OpSetLocal:
		v, err := vm.pop()
		if err != nil {
			return err
		}
		idx := fr.base + in.Arg
		if idx < 0 || idx >= len(vm.stack) {
			return fmt.Errorf("bad local slot %d", in.Arg)
		}
		vm.stack[idx] = v
	case OpPop:
		if _, err := vm.pop(); err != nil {
			return err
		}
	case OpAdd, OpSub, OpMul, OpDiv, OpMod, OpPow:
		r, err := vm.pop()
		if err != nil {
			return err
		}
		l, err := vm.pop()
		if err != nil {
			return err
		}
		v, err := arith(in.Op, l, r)
		if err != nil {
			return err
		}
		vm.push(v)
	case OpNeg:
		v, err := vm.pop()
		if err != nil {
			return err
		}
		switch v.Kind {
		case VInt:
			vm.push(IntV(-v.Int))
		case VFloat:
			vm.push(FloatV(-v.Float))
		default:
			return fmt.Errorf("cannot negate %s", typeName(v))
		}
	case OpNot:
		v, err := vm.pop()
		if err != nil {
			return err
		}
		// peek semantics? OpNot pops and pushes bool (used for !/not).
		vm.push(BoolV(!isTruthy(v)))
	case OpEq, OpNe:
		r, err := vm.pop()
		if err != nil {
			return err
		}
		l, err := vm.pop()
		if err != nil {
			return err
		}
		eq := deepEqual(l, r)
		if in.Op == OpNe {
			eq = !eq
		}
		vm.push(BoolV(eq))
	case OpLt, OpLe, OpGt, OpGe:
		r, err := vm.pop()
		if err != nil {
			return err
		}
		l, err := vm.pop()
		if err != nil {
			return err
		}
		v, err := cmp(in.Op, l, r)
		if err != nil {
			return err
		}
		vm.push(v)
	case OpIn:
		r, err := vm.pop()
		if err != nil {
			return err
		}
		l, err := vm.pop()
		if err != nil {
			return err
		}
		v, err := inOp(l, r)
		if err != nil {
			return err
		}
		vm.push(v)
	case OpJump:
		fr.ip = in.Arg
	case OpJumpIfFalse:
		v, err := vm.peek()
		if err != nil {
			return err
		}
		if !isTruthy(v) {
			fr.ip = in.Arg
		}
	case OpJumpIfTrue:
		v, err := vm.peek()
		if err != nil {
			return err
		}
		if isTruthy(v) {
			fr.ip = in.Arg
		}
	case OpCall:
		return vm.opCall(in.Arg)
	case OpMakeFunc:
		return fmt.Errorf("closures not yet supported by VM v0.1")
	case OpReturn:
		v, err := vm.pop()
		if err != nil {
			return err
		}
		base := fr.base
		vm.stack = vm.stack[:base]
		vm.frames = vm.frames[:len(vm.frames)-1]
		if len(vm.frames) == 0 {
			return nil
		}
		vm.push(v)
	case OpPrint:
		n := in.Arg
		if n < 0 || n > len(vm.stack) {
			return fmt.Errorf("bad print arity %d", n)
		}
		args := make([]Val, n)
		for i := n - 1; i >= 0; i-- {
			v, err := vm.pop()
			if err != nil {
				return err
			}
			args[i] = v
		}
		parts := make([]string, n)
		for i, a := range args {
			parts[i] = a.display()
		}
		fmt.Println(strings.Join(parts, " "))
	case OpArray:
		n := in.Arg
		if n < 0 || n > len(vm.stack) {
			return fmt.Errorf("bad array size %d", n)
		}
		items := make([]Val, n)
		for i := n - 1; i >= 0; i-- {
			v, err := vm.pop()
			if err != nil {
				return err
			}
			items[i] = v
		}
		vm.push(ArrV(items))
	case OpMap:
		n := in.Arg
		if 2*n > len(vm.stack) {
			return fmt.Errorf("bad map size %d", n)
		}
		m := map[string]Val{}
		pairs := make([]Val, 2*n)
		for i := 2*n - 1; i >= 0; i-- {
			v, err := vm.pop()
			if err != nil {
				return err
			}
			pairs[i] = v
		}
		for i := 0; i < n; i++ {
			k := pairs[2*i]
			v := pairs[2*i+1]
			if k.Kind != VString {
				return fmt.Errorf("map key must be string, got %s", typeName(k))
			}
			m[k.Str] = v
		}
		vm.push(MapV(m))
	case OpIndex:
		idx, err := vm.pop()
		if err != nil {
			return err
		}
		obj, err := vm.pop()
		if err != nil {
			return err
		}
		v, err := indexVal(obj, idx)
		if err != nil {
			return err
		}
		vm.push(v)
	case OpSetIndex:
		val, err := vm.pop()
		if err != nil {
			return err
		}
		idx, err := vm.pop()
		if err != nil {
			return err
		}
		obj, err := vm.pop()
		if err != nil {
			return err
		}
		if err := setIndex(obj, idx, val); err != nil {
			return err
		}
	case OpSleep:
		return fmt.Errorf("sleep not yet supported by VM v0.1")
	}
	return nil
}

func (vm *VM) opCall(argc int) error {
	if argc < 0 || argc+1 > len(vm.stack) {
		return fmt.Errorf("bad call arity %d", argc)
	}
	args := make([]Val, argc)
	for i := argc - 1; i >= 0; i-- {
		v, err := vm.pop()
		if err != nil {
			return err
		}
		args[i] = v
	}
	callee, err := vm.pop()
	if err != nil {
		return err
	}
	switch callee.Kind {
	case VBuiltin:
		bf, ok := vm.builtins[callee.Builtin]
		if !ok {
			return fmt.Errorf("unknown function %q", callee.Builtin)
		}
		v, err := bf(args)
		if err != nil {
			return err
		}
		vm.push(v)
		return nil
	case VFunc:
		if callee.Func < 0 || callee.Func >= len(vm.bundle.Funcs) {
			return fmt.Errorf("bad function #%d", callee.Func)
		}
		fn := &vm.bundle.Funcs[callee.Func]
		if len(args) != len(fn.Params) {
			name := fn.Name
			if name == "" {
				name = "func"
			}
			return fmt.Errorf("function %q wants %d args, got %d", name, len(fn.Params), len(args))
		}
		if len(vm.frames) >= maxFrames {
			return fmt.Errorf("stack overflow (recursion too deep)")
		}
		base := len(vm.stack)
		for _, a := range args {
			vm.stack = append(vm.stack, a)
		}
		vm.frames = append(vm.frames, frame{funcIndex: callee.Func, ip: 0, base: base})
		return nil
	}
	return fmt.Errorf("cannot call %s", typeName(callee))
}

// ---------------------------------------------------------------------------
// Arithmetic / compare / index
// ---------------------------------------------------------------------------

func arith(op Op, l, r Val) (Val, error) {
	switch op {
	case OpAdd:
		if l.Kind == VInt && r.Kind == VInt {
			return IntV(l.Int + r.Int), nil
		}
		if isNum(l) && isNum(r) {
			return FloatV(toFloat(l) + toFloat(r)), nil
		}
		if l.Kind == VString || r.Kind == VString {
			return StrV(l.display() + r.display()), nil
		}
		if l.Kind == VArray && r.Kind == VArray {
			out := make([]Val, 0, len(l.Arr)+len(r.Arr))
			out = append(out, l.Arr...)
			out = append(out, r.Arr...)
			return ArrV(out), nil
		}
		return Nil(), fmt.Errorf("cannot add %s and %s", typeName(l), typeName(r))
	case OpSub:
		if l.Kind == VInt && r.Kind == VInt {
			return IntV(l.Int - r.Int), nil
		}
		if isNum(l) && isNum(r) {
			return FloatV(toFloat(l) - toFloat(r)), nil
		}
		return Nil(), fmt.Errorf("cannot subtract %s and %s", typeName(l), typeName(r))
	case OpMul:
		if l.Kind == VInt && r.Kind == VInt {
			return IntV(l.Int * r.Int), nil
		}
		if isNum(l) && isNum(r) {
			return FloatV(toFloat(l) * toFloat(r)), nil
		}
		return Nil(), fmt.Errorf("cannot multiply %s and %s", typeName(l), typeName(r))
	case OpDiv:
		if !isNum(l) || !isNum(r) {
			return Nil(), fmt.Errorf("cannot divide %s and %s", typeName(l), typeName(r))
		}
		d := toFloat(r)
		if d == 0 {
			return Nil(), fmt.Errorf("division by zero")
		}
		return FloatV(toFloat(l) / d), nil
	case OpMod:
		if l.Kind == VInt && r.Kind == VInt {
			if r.Int == 0 {
				return Nil(), fmt.Errorf("division by zero")
			}
			return IntV(l.Int % r.Int), nil
		}
		return Nil(), fmt.Errorf("cannot mod %s and %s (need ints)", typeName(l), typeName(r))
	case OpPow:
		if !isNum(l) || !isNum(r) {
			return Nil(), fmt.Errorf("cannot raise %s to %s (need numbers)", typeName(l), typeName(r))
		}
		if l.Kind == VInt && r.Kind == VInt && r.Int >= 0 {
			res := 1
			for i := 0; i < r.Int; i++ {
				res *= l.Int
			}
			return IntV(res), nil
		}
		return FloatV(math.Pow(toFloat(l), toFloat(r))), nil
	}
	return Nil(), fmt.Errorf("bad arithmetic op")
}

func cmp(op Op, l, r Val) (Val, error) {
	if isNum(l) && isNum(r) {
		a, b := toFloat(l), toFloat(r)
		switch op {
		case OpLt:
			return BoolV(a < b), nil
		case OpLe:
			return BoolV(a <= b), nil
		case OpGt:
			return BoolV(a > b), nil
		default:
			return BoolV(a >= b), nil
		}
	}
	if l.Kind == VString && r.Kind == VString {
		switch op {
		case OpLt:
			return BoolV(l.Str < r.Str), nil
		case OpLe:
			return BoolV(l.Str <= r.Str), nil
		case OpGt:
			return BoolV(l.Str > r.Str), nil
		default:
			return BoolV(l.Str >= r.Str), nil
		}
	}
	return Nil(), fmt.Errorf("cannot compare %s and %s", typeName(l), typeName(r))
}

func inOp(l, r Val) (Val, error) {
	switch r.Kind {
	case VArray:
		for _, e := range r.Arr {
			if deepEqual(l, e) {
				return BoolV(true), nil
			}
		}
		return BoolV(false), nil
	case VMap:
		if l.Kind != VString {
			return Nil(), fmt.Errorf("cannot check %s in map (need string key)", typeName(l))
		}
		_, ok := r.Map[l.Str]
		return BoolV(ok), nil
	case VString:
		if l.Kind != VString {
			return Nil(), fmt.Errorf("cannot check %s in string (need string)", typeName(l))
		}
		return BoolV(strings.Contains(r.Str, l.Str)), nil
	}
	return Nil(), fmt.Errorf("cannot check in %s (try array, map or string)", typeName(r))
}

func indexVal(obj, idx Val) (Val, error) {
	switch obj.Kind {
	case VArray:
		if idx.Kind != VInt {
			return Nil(), fmt.Errorf("array index must be int, got %s", typeName(idx))
		}
		if idx.Int < 0 || idx.Int >= len(obj.Arr) {
			return Nil(), fmt.Errorf("index %d out of range (len %d)", idx.Int, len(obj.Arr))
		}
		return obj.Arr[idx.Int], nil
	case VMap:
		if idx.Kind != VString {
			return Nil(), fmt.Errorf("map key must be string, got %s", typeName(idx))
		}
		v, ok := obj.Map[idx.Str]
		if !ok {
			return Nil(), fmt.Errorf("unknown key %q", idx.Str)
		}
		return v, nil
	case VString:
		if idx.Kind != VInt {
			return Nil(), fmt.Errorf("string index must be int, got %s", typeName(idx))
		}
		runes := []rune(obj.Str)
		if idx.Int < 0 || idx.Int >= len(runes) {
			return Nil(), fmt.Errorf("index %d out of range (len %d)", idx.Int, len(runes))
		}
		return StrV(string(runes[idx.Int])), nil
	}
	return Nil(), fmt.Errorf("cannot index %s", typeName(obj))
}

func setIndex(obj, idx, val Val) error {
	switch obj.Kind {
	case VArray:
		if idx.Kind != VInt {
			return fmt.Errorf("array index must be int, got %s", typeName(idx))
		}
		if idx.Int < 0 || idx.Int >= len(obj.Arr) {
			return fmt.Errorf("index %d out of range (len %d)", idx.Int, len(obj.Arr))
		}
		obj.Arr[idx.Int] = val
		return nil
	case VMap:
		if idx.Kind != VString {
			return fmt.Errorf("map key must be string, got %s", typeName(idx))
		}
		obj.Map[idx.Str] = val
		return nil
	}
	return fmt.Errorf("cannot index %s", typeName(obj))
}

// ---------------------------------------------------------------------------
// Builtins (subset)
// ---------------------------------------------------------------------------

func bAssert(args []Val) (Val, error) {
	if len(args) < 1 || len(args) > 2 {
		return Nil(), fmt.Errorf("assert wants 1..2 args, got %d", len(args))
	}
	if !isTruthy(args[0]) {
		if len(args) == 2 {
			return Nil(), fmt.Errorf("%s", args[1].display())
		}
		return Nil(), fmt.Errorf("assert failed")
	}
	return Nil(), nil
}

func bLen(args []Val) (Val, error) {
	if len(args) != 1 {
		return Nil(), fmt.Errorf("len wants 1 arg, got %d", len(args))
	}
	switch args[0].Kind {
	case VString:
		return IntV(utf8.RuneCountInString(args[0].Str)), nil
	case VArray:
		return IntV(len(args[0].Arr)), nil
	case VMap:
		return IntV(len(args[0].Map)), nil
	}
	return Nil(), fmt.Errorf("len wants string/array/map, got %s", typeName(args[0]))
}

func bRange(args []Val) (Val, error) {
	if len(args) < 1 || len(args) > 3 {
		return Nil(), fmt.Errorf("range wants 1..3 args, got %d", len(args))
	}
	for _, a := range args {
		if a.Kind != VInt {
			return Nil(), fmt.Errorf("range wants ints, got %s", typeName(a))
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
	var out []Val
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
		out = []Val{}
	}
	return ArrV(out), nil
}

func bStr(args []Val) (Val, error) {
	if len(args) != 1 {
		return Nil(), fmt.Errorf("str wants 1 arg, got %d", len(args))
	}
	return StrV(args[0].display()), nil
}

func bInt(args []Val) (Val, error) {
	if len(args) != 1 {
		return Nil(), fmt.Errorf("int wants 1 arg, got %d", len(args))
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
	return Nil(), fmt.Errorf("int(%s) failed", typeName(a))
}

func bFloat(args []Val) (Val, error) {
	if len(args) != 1 {
		return Nil(), fmt.Errorf("float wants 1 arg, got %d", len(args))
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
	return Nil(), fmt.Errorf("float(%s) failed", typeName(a))
}

func bType(args []Val) (Val, error) {
	if len(args) != 1 {
		return Nil(), fmt.Errorf("type wants 1 arg, got %d", len(args))
	}
	return StrV(typeName(args[0])), nil
}

func sortedKeys(m map[string]Val) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func bIterLen(args []Val) (Val, error) {
	if len(args) != 1 {
		return Nil(), fmt.Errorf("__iter_len wants 1 arg")
	}
	switch args[0].Kind {
	case VArray:
		return IntV(len(args[0].Arr)), nil
	case VString:
		return IntV(len([]rune(args[0].Str))), nil
	case VMap:
		return IntV(len(args[0].Map)), nil
	}
	return Nil(), fmt.Errorf("cannot iterate %s (try array, map or string)", typeName(args[0]))
}

func bIterGet(args []Val) (Val, error) {
	if len(args) != 2 || args[1].Kind != VInt {
		return Nil(), fmt.Errorf("__iter_get wants (iter, int)")
	}
	iter, i := args[0], args[1].Int
	switch iter.Kind {
	case VArray:
		if i < 0 || i >= len(iter.Arr) {
			return Nil(), fmt.Errorf("index %d out of range (len %d)", i, len(iter.Arr))
		}
		return iter.Arr[i], nil
	case VString:
		runes := []rune(iter.Str)
		if i < 0 || i >= len(runes) {
			return Nil(), fmt.Errorf("index %d out of range (len %d)", i, len(runes))
		}
		return StrV(string(runes[i])), nil
	case VMap:
		keys := sortedKeys(iter.Map)
		if i < 0 || i >= len(keys) {
			return Nil(), fmt.Errorf("index %d out of range (len %d)", i, len(keys))
		}
		return StrV(keys[i]), nil
	}
	return Nil(), fmt.Errorf("cannot iterate %s", typeName(iter))
}

func bIterKey(args []Val) (Val, error) {
	if len(args) != 3 || args[2].Kind != VInt {
		return Nil(), fmt.Errorf("__iter_key wants (iter, keys, int)")
	}
	iter, i := args[0], args[2].Int
	switch iter.Kind {
	case VArray, VString:
		return IntV(i), nil
	case VMap:
		keys := sortedKeys(iter.Map)
		if i < 0 || i >= len(keys) {
			return Nil(), fmt.Errorf("index %d out of range (len %d)", i, len(keys))
		}
		return StrV(keys[i]), nil
	}
	return Nil(), fmt.Errorf("cannot iterate %s", typeName(iter))
}

func bIterVal(args []Val) (Val, error) {
	if len(args) != 2 || args[1].Kind != VInt {
		return Nil(), fmt.Errorf("__iter_val wants (iter, int)")
	}
	iter, i := args[0], args[1].Int
	switch iter.Kind {
	case VArray:
		if i < 0 || i >= len(iter.Arr) {
			return Nil(), fmt.Errorf("index %d out of range (len %d)", i, len(iter.Arr))
		}
		return iter.Arr[i], nil
	case VString:
		runes := []rune(iter.Str)
		if i < 0 || i >= len(runes) {
			return Nil(), fmt.Errorf("index %d out of range (len %d)", i, len(runes))
		}
		return StrV(string(runes[i])), nil
	case VMap:
		keys := sortedKeys(iter.Map)
		if i < 0 || i >= len(keys) {
			return Nil(), fmt.Errorf("index %d out of range (len %d)", i, len(keys))
		}
		return iter.Map[keys[i]], nil
	}
	return Nil(), fmt.Errorf("cannot iterate %s", typeName(iter))
}

func bMapKeysOrNil(args []Val) (Val, error) {
	if len(args) != 1 {
		return Nil(), fmt.Errorf("__map_keys_or_nil wants 1 arg")
	}
	if args[0].Kind != VMap {
		return Nil(), nil
	}
	keys := sortedKeys(args[0].Map)
	out := make([]Val, len(keys))
	for i, k := range keys {
		out[i] = StrV(k)
	}
	return ArrV(out), nil
}
