// Package frontend is the ks-fusion compiler frontend:
// lexer + parser for .ks files (v0.1, line-based, Python-like).
package frontend

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Expr kinds.
type ExprKind int

const (
	ExprString ExprKind = iota
	ExprInt
	ExprVar
	ExprAdd
)

// Expr is a simple expression node.
type Expr struct {
	Kind   ExprKind
	StrVal string
	IntVal int
	Name   string
	Left   *Expr
	Right  *Expr
}

// Stmt kinds.
type StmtKind int

const (
	StmtLet StmtKind = iota
	StmtAssign
	StmtPrint
	StmtGo
	StmtSleep
)

// Stmt is one statement node.
type Stmt struct {
	Kind    StmtKind
	Name    string // for Let / Assign
	Expr    *Expr  // for Let / Assign / Print
	Inner   *Stmt  // for Go
	SleepMs int    // for Sleep
	Line    int
}

// Program is a parsed .ks file.
type Program struct {
	Statements []*Stmt
	Path       string
}

// ParseFile reads and parses a .ks file.
func ParseFile(path string) (*Program, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ParseSource(string(data), path)
}

// ParseSource parses .ks source text.
func ParseSource(src, path string) (*Program, error) {
	p := &Program{Path: path}
	lines := strings.Split(src, "\n")
	for i, raw := range lines {
		lineNo := i + 1
		line := strings.TrimSpace(stripInlineComment(raw))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		st, err := parseLine(line, lineNo)
		if err != nil {
			return nil, fmt.Errorf("%s:%d: %w", path, lineNo, err)
		}
		p.Statements = append(p.Statements, st)
	}
	return p, nil
}

func parseLine(line string, lineNo int) (*Stmt, error) {
	// go <stmt> -> concurrency like Go
	if line == "go" || strings.HasPrefix(line, "go ") || strings.HasPrefix(line, "go\t") {
		rest := strings.TrimSpace(strings.TrimPrefix(line, "go"))
		if rest == "" {
			return nil, fmt.Errorf("go needs a statement, e.g. `go print \"hi\"`")
		}
		inner, err := parseLine(rest, lineNo)
		if err != nil {
			return nil, err
		}
		return &Stmt{Kind: StmtGo, Inner: inner, Line: lineNo}, nil
	}
	if strings.HasPrefix(line, "let ") || strings.HasPrefix(line, "let\t") {
		rest := strings.TrimSpace(line[3:])
		name, exprStr, err := splitAssign(rest)
		if err != nil {
			return nil, fmt.Errorf("bad let: want `let x = ...`: %w", err)
		}
		if !isIdent(name) {
			return nil, fmt.Errorf("bad variable name %q", name)
		}
		ex, err := ParseExpr(exprStr)
		if err != nil {
			return nil, err
		}
		return &Stmt{Kind: StmtLet, Name: name, Expr: ex, Line: lineNo}, nil
	}
	if line == "print" || strings.HasPrefix(line, "print ") || strings.HasPrefix(line, "print\t") || strings.HasPrefix(line, "print(") {
		rest := strings.TrimSpace(strings.TrimPrefix(line, "print"))
		rest = strings.TrimSpace(strings.TrimPrefix(rest, "("))
		rest = strings.TrimSpace(strings.TrimSuffix(rest, ")"))
		if rest == "" {
			return nil, fmt.Errorf("print needs a value, e.g. `print \"hi\"`")
		}
		ex, err := ParseExpr(rest)
		if err != nil {
			return nil, err
		}
		return &Stmt{Kind: StmtPrint, Expr: ex, Line: lineNo}, nil
	}
	if strings.HasPrefix(line, "sleep ") || strings.HasPrefix(line, "sleep\t") {
		rest := strings.TrimSpace(line[5:])
		ms, err := strconv.Atoi(rest)
		if err != nil || ms < 0 {
			return nil, fmt.Errorf("bad sleep: want `sleep 500` (ms)")
		}
		return &Stmt{Kind: StmtSleep, SleepMs: ms, Line: lineNo}, nil
	}
	if line == "sleep" {
		return nil, fmt.Errorf("bad sleep: want `sleep 500` (ms)")
	}
	if strings.Contains(line, "=") {
		name, exprStr, err := splitAssign(line)
		if err != nil {
			return nil, err
		}
		if !isIdent(name) {
			return nil, fmt.Errorf("bad assignment, want `x = ...`")
		}
		ex, err := ParseExpr(exprStr)
		if err != nil {
			return nil, err
		}
		return &Stmt{Kind: StmtAssign, Name: name, Expr: ex, Line: lineNo}, nil
	}
	return nil, fmt.Errorf("unknown statement %q (try: let, print, sleep, go, x = ...)", line)
}

// ParseExpr parses a simple expression: "str", 123, x, a + b.
func ParseExpr(s string) (*Expr, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, fmt.Errorf("empty expression")
	}
	// addition outside quotes FIRST: otherwise `"a" + "b"` would
	// look like a single string literal (starts and ends with `"`).
	if idx := indexPlusOutsideQuotes(s); idx >= 0 {
		left, err := ParseExpr(strings.TrimSpace(s[:idx]))
		if err != nil {
			return nil, err
		}
		right, err := ParseExpr(strings.TrimSpace(s[idx+1:]))
		if err != nil {
			return nil, err
		}
		return &Expr{Kind: ExprAdd, Left: left, Right: right}, nil
	}
	// string literal
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return &Expr{Kind: ExprString, StrVal: s[1 : len(s)-1]}, nil
	}
	// int literal
	if n, err := strconv.Atoi(s); err == nil {
		return &Expr{Kind: ExprInt, IntVal: n}, nil
	}
	// variable
	if isIdent(s) {
		return &Expr{Kind: ExprVar, Name: s}, nil
	}
	return nil, fmt.Errorf("bad expression %q", s)
}

func splitAssign(s string) (string, string, error) {
	idx := strings.Index(s, "=")
	if idx < 0 {
		return "", "", fmt.Errorf("missing `=`")
	}
	name := strings.TrimSpace(s[:idx])
	expr := strings.TrimSpace(s[idx+1:])
	if name == "" || expr == "" {
		return "", "", fmt.Errorf("want `name = expr`")
	}
	return name, expr, nil
}

func isIdent(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		if r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
			continue
		}
		if i > 0 && r >= '0' && r <= '9' {
			continue
		}
		return false
	}
	return true
}

func indexPlusOutsideQuotes(s string) int {
	inStr := false
	for i := 0; i < len(s); i++ {
		if s[i] == '"' {
			inStr = !inStr
		}
		if !inStr && s[i] == '+' {
			return i
		}
	}
	return -1
}

// stripInlineComment cuts a trailing `# comment` outside string literals.
// `#` inside `"..."` is preserved, e.g. `print "# hi"` keeps the `#`.
func stripInlineComment(s string) string {
	inStr := false
	for i := 0; i < len(s); i++ {
		if s[i] == '"' {
			inStr = !inStr
		}
		if !inStr && s[i] == '#' {
			return s[:i]
		}
	}
	return s
}
