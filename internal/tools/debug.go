package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/kswarrior/ks-fusion/internal/backend"
	"github.com/kswarrior/ks-fusion/internal/frontend"
)

// Debugger (v2.5): line breakpoints + step trace + stack snapshot over the
// tree-walk interpreter. Unlike `fusion run --debug` (vet dump only), this
// pauses at breakpoints, prints globals, and records an execution trace.

// Breakpoint is one stopped line.
type BreakHit struct {
	Line int
	Kind string
}

// DebugResult is the outcome of a debug run.
type DebugResult struct {
	Hits  []BreakHit
	Trace []string // "line N (kind)" when trace enabled
	Vars  map[string]string
}

// DebugFile runs a .ks file with breakpoints. Every executed statement fires
// OnStmt; lines in breaks record a hit (execution continues — non-interactive
// mode for CI/tests). When trace is true, every statement is recorded.
// Returns hits, trace, and final globals snapshot.
func DebugFile(path string, breaks []int, trace bool) (*DebugResult, error) {
	prog, err := frontend.ParseFile(path)
	if err != nil {
		return nil, err
	}
	want := map[int]bool{}
	for _, b := range breaks {
		want[b] = true
	}
	res := &DebugResult{Vars: map[string]string{}}
	in := backend.New()
	if d := filepath.Dir(path); d != "" && d != "." {
		if abs, err := filepath.Abs(d); err == nil {
			in.SetBaseDir(abs)
		}
	}
	in.OnStmt = func(line int, kind string) error {
		if trace {
			res.Trace = append(res.Trace, fmt.Sprintf("line %d (%s)", line, kind))
		}
		if want[line] {
			res.Hits = append(res.Hits, BreakHit{Line: line, Kind: kind})
		}
		return nil
	}
	if err := in.ExecProgram(prog); err != nil {
		return res, err
	}
	in.WgWait()
	// snapshot globals
	for _, name := range in.Globals() {
		if v, ok := in.Lookup(name); ok {
			res.Vars[name] = v.Display()
		}
	}
	return res, nil
}

// DebugSource is DebugFile over inline source (tests).
func DebugSource(src string, breaks []int, trace bool) (*DebugResult, error) {
	f, err := os.CreateTemp("", "ksdbg-*.ks")
	if err != nil {
		return nil, err
	}
	defer os.Remove(f.Name())
	if _, err := f.WriteString(src); err != nil {
		return nil, err
	}
	f.Close()
	return DebugFile(f.Name(), breaks, trace)
}

// FormatHits renders breakpoint hits for CLI output.
func FormatHits(res *DebugResult) string {
	var b strings.Builder
	for _, h := range res.Hits {
		fmt.Fprintf(&b, "break line %d (%s)\n", h.Line, h.Kind)
	}
	names := make([]string, 0, len(res.Vars))
	for k := range res.Vars {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		fmt.Fprintf(&b, "%s = %s\n", k, res.Vars[k])
	}
	return b.String()
}
