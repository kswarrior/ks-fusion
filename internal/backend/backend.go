// Package backend is the ks-fusion backend v0.1:
// tree-walk interpreter with Go-like concurrency (`go` statement).
package backend

import (
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/kswarrior/ks-fusion/internal/frontend"
)

// Value is a runtime value (int or string only in v0.1).
type Value struct {
	IsInt bool
	Int   int
	Str   string
}

func (v Value) String() string {
	if v.IsInt {
		return strconv.Itoa(v.Int)
	}
	return v.Str
}

// Interpreter holds variables. Safe for concurrent `go` use.
type Interpreter struct {
	mu   sync.Mutex
	vars map[string]Value
	wg   sync.WaitGroup
	merr sync.Mutex
	err  error
}

func New() *Interpreter {
	return &Interpreter{vars: map[string]Value{}}
}

// Run executes a parsed program and waits for `go` statements.
func Run(p *frontend.Program) error {
	in := New()
	for _, st := range p.Statements {
		in.exec(st)
	}
	in.wg.Wait()
	return in.fail()
}

func (in *Interpreter) fail() error {
	in.merr.Lock()
	defer in.merr.Unlock()
	return in.err
}

func (in *Interpreter) setErr(err error) {
	in.merr.Lock()
	defer in.merr.Unlock()
	if in.err == nil {
		in.err = err
	}
}

func (in *Interpreter) exec(st *frontend.Stmt) {
	if in.fail() != nil {
		return
	}
	switch st.Kind {
	case frontend.StmtLet, frontend.StmtAssign:
		v, err := in.eval(st.Expr)
		if err != nil {
			in.setErr(fmt.Errorf("line %d: %w", st.Line, err))
			return
		}
		in.mu.Lock()
		in.vars[st.Name] = v
		in.mu.Unlock()
	case frontend.StmtPrint:
		v, err := in.eval(st.Expr)
		if err != nil {
			in.setErr(fmt.Errorf("line %d: %w", st.Line, err))
			return
		}
		// Print outside the vars lock: fmt is goroutine-safe and we
		// must not block variable access during I/O.
		fmt.Println(v.String())
	case frontend.StmtSleep:
		time.Sleep(time.Duration(st.SleepMs) * time.Millisecond)
	case frontend.StmtGo:
		inner := st.Inner
		in.wg.Add(1)
		go func() {
			defer in.wg.Done()
			in.exec(inner)
		}()
	default:
		in.setErr(fmt.Errorf("line %d: unknown statement", st.Line))
	}
}

func (in *Interpreter) eval(e *frontend.Expr) (Value, error) {
	switch e.Kind {
	case frontend.ExprString:
		return Value{Str: e.StrVal}, nil
	case frontend.ExprInt:
		return Value{IsInt: true, Int: e.IntVal}, nil
	case frontend.ExprVar:
		in.mu.Lock()
		v, ok := in.vars[e.Name]
		in.mu.Unlock()
		if !ok {
			return Value{}, fmt.Errorf("unknown variable %q", e.Name)
		}
		return v, nil
	case frontend.ExprAdd:
		l, err := in.eval(e.Left)
		if err != nil {
			return Value{}, err
		}
		r, err := in.eval(e.Right)
		if err != nil {
			return Value{}, err
		}
		if l.IsInt && r.IsInt {
			return Value{IsInt: true, Int: l.Int + r.Int}, nil
		}
		// string concat (ints auto-converted)
		return Value{Str: l.String() + r.String()}, nil
	}
	return Value{}, fmt.Errorf("bad expression")
}
