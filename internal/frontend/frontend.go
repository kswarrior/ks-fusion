// Package frontend is the ks-fusion compiler frontend:
// full lexer + recursive-descent parser for .ks files.
//
// Language summary:
//
//	types: nil, bool, int, float, string, array, map, func, chan
//	       (+ number/any/ok/err aliases for annotations and `is`)
//	stmts: let (with optional `: type`), assign (+= -= *= /= %=), print, sleep, go,
//	       if/else, while, for-in, for-c-style, func (with optional param/return types), return,
//	       break, continue, import, try/catch/finally, switch,
//	       select (recv/send/timeout/default), defer, block { }, expr-statement
//	exprs: literals, vars, a+b - * / % **, in, is, ??, == != < <= > >=,
//	       and/or/not (also && || !), unary - !,
//	       calls f(...), index a[i], slice a[l:r], field m.key,
//	       safe access a?.b / a?.[i], arrays [..], maps {k: v, ..}, func literals
//	comments: # ..., // ..., /* ... */
//	strings: "double" and 'single'; numbers: 0xFF 0b11 0o17 1_000 1e3 .5
package frontend

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// ---------------------------------------------------------------------------
// AST
// ---------------------------------------------------------------------------

// Expr kinds.
type ExprKind int

const (
	ExprString ExprKind = iota
	ExprInt
	ExprVar
	ExprAdd
	ExprFloat
	ExprBool
	ExprNil
	ExprSub
	ExprMul
	ExprDiv
	ExprMod
	ExprPow
	ExprIn
	ExprIs
	ExprCoalesce
	ExprEq
	ExprNe
	ExprLt
	ExprLe
	ExprGt
	ExprGe
	ExprAnd
	ExprOr
	ExprNot
	ExprNeg
	ExprCall
	ExprIndex
	ExprSlice
	ExprArray
	ExprMap
	ExprFunc
)

// Expr is an expression node.
type Expr struct {
	Kind       ExprKind
	StrVal     string
	IntVal     int
	FloatVal   float64
	BoolVal    bool
	Name       string
	Left       *Expr
	Right      *Expr
	Safe       bool  // true for `?.` nil-safe index/field access
	SliceStart *Expr // nil = omitted start
	SliceEnd   *Expr // nil = omitted end
	Args       []*Expr
	Callee     *Expr
	Elements   []*Expr
	MapKeys    []string
	MapVals    []*Expr
	FuncParams []string
	FuncParamTypes []string // parallel to FuncParams, "" = any
	FuncReturnType string   // "" = any
	FuncBody   *Stmt
}

// Stmt kinds.
type StmtKind int

const (
	StmtLet StmtKind = iota
	StmtAssign
	StmtPrint
	StmtGo
	StmtSleep
	StmtIf
	StmtWhile
	StmtForIn
	StmtForC
	StmtFunc
	StmtReturn
	StmtBreak
	StmtContinue
	StmtBlock
	StmtExpr
	StmtImport
	StmtTry
	StmtSwitch
	StmtSelect
	StmtDefer
)

// SwitchCase is one `case`/`default` branch of a switch statement.
type SwitchCase struct {
	Values    []*Expr // nil for default
	Body      *Stmt
	IsDefault bool
	Line      int
}

// SelectCase is one `case`/`default` branch of a select statement.
//
//	Kind "recv":    Chan is the channel expr, Bind is the optional
//	                receive variable (`case v = recv(c)`).
//	Kind "send":    Chan is the channel expr, Value is the value expr
//	                (`case send(c, v)`).
//	Kind "timeout": Timeout is the ms expr (`case timeout(100)`).
//	Kind "default": Body only (non-blocking fallback).
type SelectCase struct {
	Kind    string // "recv" | "send" | "timeout" | "default"
	Chan    *Expr  // recv/send channel expr
	Value   *Expr  // send value expr
	Timeout *Expr  // timeout ms expr
	Bind    string // recv bind var ("" = discard)
	Body    *Stmt
	Line    int
}

// Stmt is one statement node.
type Stmt struct {
	Kind    StmtKind
	Name    string   // let/assign var, func name
	Names   []string // for-in vars, func params (def)
	ParamTypes []string // parallel to Names for func def, "" = any
	ReturnType string   // func def return annotation, "" = any
	TypeAnn string   // let type annotation, "" = any
	Expr    *Expr    // let value, assign value, return, while cond, if cond, for iter/cond, sleep value, expr-stmt, switch target, defer call
	Exprs   []*Expr  // print args
	Inner   *Stmt    // go inner
	Body    *Stmt    // func/while/for body (block)
	Then    *Stmt    // if then (block), try block
	Else    *Stmt    // if else (block or if)
	Init    *Stmt    // for-c init (may be nil)
	Post    *Stmt    // for-c post (may be nil)
	List        []*Stmt  // block statements
	Cases       []*SwitchCase
	SelectCases []*SelectCase // select branches
	StrVal      string        // import path
	Catch   string // try: catch variable ("" = none)
	CaBody  *Stmt  // try: catch body (nil = no catch)
	FinBody *Stmt  // try: finally body (nil = none)
	Op      string // assign op: = += -= *= /= %=
	Line    int
	SleepMs int // kept for compat: set when sleep arg is int literal
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
	toks, err := lex(src, path)
	if err != nil {
		return nil, err
	}
	p := &parser{toks: toks, path: path}
	stmts, err := p.parseProgram()
	if err != nil {
		return nil, err
	}
	return &Program{Statements: stmts, Path: path}, nil
}

// ParseExpr parses a single expression string (kept for compat/tests).
func ParseExpr(s string) (*Expr, error) {
	toks, err := lex(s, "<expr>")
	if err != nil {
		return nil, err
	}
	p := &parser{toks: toks, path: "<expr>"}
	e, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	if !p.atEnd() {
		return nil, fmt.Errorf("<expr>: unexpected %q after expression", p.peek().Lit)
	}
	return e, nil
}

// ---------------------------------------------------------------------------
// Lexer
// ---------------------------------------------------------------------------

type tokKind int

const (
	tEOF tokKind = iota
	tIdent
	tInt
	tFloat
	tString
	tNewline
	tSemi
	tPlus
	tMinus
	tStar
	tSlash
	tPercent
	tEq
	tNe
	tLt
	tLe
	tGt
	tGe
	tAssign
	tPlusAssign
	tMinusAssign
	tStarAssign
	tSlashAssign
	tModAssign
	tBang
	tAndOp
	tOrOp
	tStarStar
	tDot
	tComma
	tColon
	tLParen
	tRParen
	tLBracket
	tRBracket
	tLBrace
	tRBrace
	// keywords
	tLet
	tFunc
	tReturn
	tIf
	tElse
	tWhile
	tFor
	tIn
	tBreak
	tContinue
	tPrint
	tSleep
	tGo
	tImport
	tTrue
	tFalse
	tNil
	tAnd
	tOr
	tNot
	tTry
	tCatch
	tFinally
	tSwitch
	tCase
	tDefault
	tDefer
	tSelect
	tQuestion
	tCoalesce
	tQuestionDot
)

type token struct {
	K    tokKind
	Lit  string
	Line int
}

var keywords = map[string]tokKind{
	"let":      tLet,
	"func":     tFunc,
	"return":   tReturn,
	"if":       tIf,
	"else":     tElse,
	"while":    tWhile,
	"for":      tFor,
	"in":       tIn,
	"break":    tBreak,
	"continue": tContinue,
	"print":    tPrint,
	"sleep":    tSleep,
	"go":       tGo,
	"import":   tImport,
	"true":     tTrue,
	"false":    tFalse,
	"nil":      tNil,
	"and":      tAnd,
	"or":       tOr,
	"not":      tNot,
	"try":      tTry,
	"catch":    tCatch,
	"finally":  tFinally,
	"switch":   tSwitch,
	"case":     tCase,
	"default":  tDefault,
	"defer":    tDefer,
	"select":   tSelect,
}

func isDigitForBase(c byte, kind byte) bool {
	switch kind {
	case 'x', 'X':
		return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'
	case 'b', 'B':
		return c == '0' || c == '1'
	default: // octal
		return c >= '0' && c <= '7'
	}
}

func lex(src, path string) ([]token, error) {
	var toks []token
	line := 1
	i := 0
	n := len(src)
	add := func(k tokKind, lit string) {
		toks = append(toks, token{K: k, Lit: lit, Line: line})
	}
	for i < n {
		c := src[i]
		// newlines
		if c == '\n' {
			add(tNewline, "\n")
			line++
			i++
			continue
		}
		if c == ' ' || c == '\t' || c == '\r' {
			i++
			continue
		}
		// # comment
		if c == '#' {
			for i < n && src[i] != '\n' {
				i++
			}
			continue
		}
		// /* block comment */
		if c == '/' && i+1 < n && src[i+1] == '*' {
			startLine := line
			i += 2
			closed := false
			for i < n {
				if src[i] == '*' && i+1 < n && src[i+1] == '/' {
					closed = true
					i += 2
					break
				}
				if src[i] == '\n' {
					line++
				}
				i++
			}
			if !closed {
				return nil, fmt.Errorf("%s:%d: unterminated block comment", path, startLine)
			}
			continue
		}
		// strings ("double" and 'single', same escapes)
		if c == '"' || c == '\'' {
			quote := c
			startLine := line
			i++
			var sb strings.Builder
			closed := false
			for i < n {
				ch := src[i]
				if ch == quote {
					closed = true
					i++
					break
				}
				if ch == '\\' {
					i++
					if i >= n {
						break
					}
					esc := src[i]
					switch esc {
					case 'n':
						sb.WriteByte('\n')
					case 't':
						sb.WriteByte('\t')
					case 'r':
						sb.WriteByte('\r')
					case '"':
						sb.WriteByte('"')
					case '\'':
						sb.WriteByte('\'')
					case '\\':
						sb.WriteByte('\\')
					case '0':
						sb.WriteByte(0)
					default:
						sb.WriteByte(esc)
					}
					i++
					continue
				}
				if ch == '\n' {
					line++
				}
				sb.WriteByte(ch)
				i++
			}
			if !closed {
				return nil, fmt.Errorf("%s:%d: unterminated string", path, startLine)
			}
			toks = append(toks, token{K: tString, Lit: sb.String(), Line: startLine})
			continue
		}
		// numbers: ints (decimal, 0x hex, 0b binary, 0o octal),
		// floats (3.5, .5, 1e3, 2.5e-2); underscores allowed (1_000).
		if c >= '0' && c <= '9' {
			start := i
			startLine := line
			// prefixed ints
			if c == '0' && i+1 < n && (src[i+1] == 'x' || src[i+1] == 'X' || src[i+1] == 'b' || src[i+1] == 'B' || src[i+1] == 'o' || src[i+1] == 'O') {
				kind := src[i+1]
				i += 2
				ds := i
				for i < n && (src[i] == '_' || isDigitForBase(src[i], kind)) {
					i++
				}
				raw := strings.ReplaceAll(src[ds:i], "_", "")
				if raw == "" {
					return nil, fmt.Errorf("%s:%d: bad number %q", path, startLine, src[start:i])
				}
				var base int
				switch kind {
				case 'x', 'X':
					base = 16
				case 'b', 'B':
					base = 2
				default:
					base = 8
				}
				v, err := strconv.ParseInt(raw, base, 64)
				if err != nil {
					return nil, fmt.Errorf("%s:%d: bad number %q", path, startLine, src[start:i])
				}
				add(tInt, strconv.FormatInt(v, 10))
				continue
			}
			for i < n && (src[i] >= '0' && src[i] <= '9' || src[i] == '_') {
				i++
			}
			isFloat := false
			if i < n && src[i] == '.' && i+1 < n && src[i+1] >= '0' && src[i+1] <= '9' {
				isFloat = true
				i++ // dot
				for i < n && (src[i] >= '0' && src[i] <= '9' || src[i] == '_') {
					i++
				}
			}
			if i < n && (src[i] == 'e' || src[i] == 'E') {
				j := i + 1
				if j < n && (src[j] == '+' || src[j] == '-') {
					j++
				}
				if j < n && src[j] >= '0' && src[j] <= '9' {
					isFloat = true
					i = j
					for i < n && (src[i] >= '0' && src[i] <= '9' || src[i] == '_') {
						i++
					}
				}
			}
			raw := strings.ReplaceAll(src[start:i], "_", "")
			if isFloat {
				if _, err := strconv.ParseFloat(raw, 64); err != nil {
					return nil, fmt.Errorf("%s:%d: bad float %q", path, startLine, src[start:i])
				}
				add(tFloat, raw)
			} else {
				if _, err := strconv.Atoi(raw); err != nil {
					return nil, fmt.Errorf("%s:%d: bad integer %q", path, startLine, src[start:i])
				}
				add(tInt, raw)
			}
			continue
		}
		// leading-dot float: .5
		if c == '.' && i+1 < n && src[i+1] >= '0' && src[i+1] <= '9' {
			start := i
			i++ // dot
			for i < n && (src[i] >= '0' && src[i] <= '9' || src[i] == '_') {
				i++
			}
			if i < n && (src[i] == 'e' || src[i] == 'E') {
				j := i + 1
				if j < n && (src[j] == '+' || src[j] == '-') {
					j++
				}
				if j < n && src[j] >= '0' && src[j] <= '9' {
					i = j
					for i < n && (src[i] >= '0' && src[i] <= '9' || src[i] == '_') {
						i++
					}
				}
			}
			raw := strings.ReplaceAll(src[start:i], "_", "")
			if _, err := strconv.ParseFloat(raw, 64); err != nil {
				return nil, fmt.Errorf("%s:%d: bad float %q", path, line, src[start:i])
			}
			add(tFloat, raw)
			continue
		}
		// idents / keywords
		if c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') {
			start := i
			for i < n {
				ch := src[i]
				if ch == '_' || (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') {
					i++
				} else {
					break
				}
			}
			word := src[start:i]
			if k, ok := keywords[word]; ok {
				add(k, word)
			} else {
				add(tIdent, word)
			}
			continue
		}
		// two-char operators first
		two := ""
		if i+1 < n {
			two = src[i : i+2]
		}
		switch two {
		case "==":
			add(tEq, two)
			i += 2
			continue
		case "!=":
			add(tNe, two)
			i += 2
			continue
		case "**":
			add(tStarStar, two)
			i += 2
			continue
		case "<=":
			add(tLe, two)
			i += 2
			continue
		case ">=":
			add(tGe, two)
			i += 2
			continue
		case "&&":
			add(tAndOp, two)
			i += 2
			continue
		case "||":
			add(tOrOp, two)
			i += 2
			continue
		case "+=":
			add(tPlusAssign, two)
			i += 2
			continue
		case "-=":
			add(tMinusAssign, two)
			i += 2
			continue
		case "*=":
			add(tStarAssign, two)
			i += 2
			continue
		case "/=":
			if i+2 < n && src[i+2] == '/' {
				// `//` comment, not `/=`
			} else {
				add(tSlashAssign, two)
				i += 2
				continue
			}
		case "%=":
			add(tModAssign, two)
			i += 2
			continue
		case "??":
			add(tCoalesce, two)
			i += 2
			continue
		case "?.":
			add(tQuestionDot, two)
			i += 2
			continue
		}
		// `//` comment
		if c == '/' && i+1 < n && src[i+1] == '/' {
			for i < n && src[i] != '\n' {
				i++
			}
			continue
		}
		switch c {
		case '+':
			add(tPlus, "+")
		case '-':
			add(tMinus, "-")
		case '*':
			add(tStar, "*")
		case '/':
			add(tSlash, "/")
		case '%':
			add(tPercent, "%")
		case '<':
			add(tLt, "<")
		case '>':
			add(tGt, ">")
		case '=':
			add(tAssign, "=")
		case '!':
			add(tBang, "!")
		case '.':
			add(tDot, ".")
		case '?':
			add(tQuestion, "?")
		case ',':
			add(tComma, ",")
		case ':':
			add(tColon, ":")
		case ';':
			add(tSemi, ";")
		case '(':
			add(tLParen, "(")
		case ')':
			add(tRParen, ")")
		case '[':
			add(tLBracket, "[")
		case ']':
			add(tRBracket, "]")
		case '{':
			add(tLBrace, "{")
		case '}':
			add(tRBrace, "}")
		default:
			return nil, fmt.Errorf("%s:%d: unexpected character %q", path, line, string(c))
		}
		i++
	}
	toks = append(toks, token{K: tEOF, Lit: "", Line: line})
	return toks, nil
}

// ---------------------------------------------------------------------------
// Parser
// ---------------------------------------------------------------------------

type parser struct {
	toks []token
	pos  int
	path string
}

func (p *parser) peek() token { return p.toks[p.pos] }
func (p *parser) peekAt(off int) token {
	if p.pos+off >= len(p.toks) {
		return p.toks[len(p.toks)-1]
	}
	return p.toks[p.pos+off]
}
func (p *parser) next() token {
	t := p.toks[p.pos]
	if p.pos < len(p.toks)-1 {
		p.pos++
	}
	return t
}
func (p *parser) atEnd() bool { return p.peek().K == tEOF }
func (p *parser) errf(t token, format string, args ...any) error {
	return fmt.Errorf("%s:%d: %s", p.path, t.Line, fmt.Sprintf(format, args...))
}

func (p *parser) skipSeps() {
	for {
		k := p.peek().K
		if k == tNewline || k == tSemi {
			p.next()
		} else {
			return
		}
	}
}

func (p *parser) expect(k tokKind, what string) (token, error) {
	t := p.peek()
	if t.K != k {
		return t, p.errf(t, "want %s, got %q", what, t.Lit)
	}
	return p.next(), nil
}

// parenWrapsStmt reports whether the `(` at peek opens a group whose
// matching `)` ends the statement (newline/semi/}/EOF). Used to tell
// `print(a, b)` apart from `print (a)+b`.
func (p *parser) parenWrapsStmt() bool {
	depth := 0
	for i := 0; ; i++ {
		t := p.peekAt(i)
		switch t.K {
		case tLParen:
			depth++
		case tRParen:
			depth--
			if depth == 0 {
				after := p.peekAt(i + 1).K
				return after == tNewline || after == tSemi || after == tEOF || after == tRBrace
			}
		case tEOF:
			return false
		}
		if i > 10000 {
			return false
		}
	}
}

// parseProgram := { stmt } EOF
func (p *parser) parseProgram() ([]*Stmt, error) {
	var out []*Stmt
	p.skipSeps()
	for !p.atEnd() {
		if p.peek().K == tRBrace {
			return nil, p.errf(p.peek(), "unexpected %q", "}")
		}
		st, err := p.parseStmt()
		if err != nil {
			return nil, err
		}
		out = append(out, st)
		p.skipSeps()
	}
	return out, nil
}

func isAssignOp(k tokKind) (string, bool) {
	switch k {
	case tAssign:
		return "=", true
	case tPlusAssign:
		return "+=", true
	case tMinusAssign:
		return "-=", true
	case tStarAssign:
		return "*=", true
	case tSlashAssign:
		return "/=", true
	case tModAssign:
		return "%=", true
	}
	return "", false
}

func (p *parser) parseStmt() (*Stmt, error) {
	t := p.peek()
	switch t.K {
	case tLet:
		return p.parseLet()
	case tFunc:
		// named func statement if `func ident (` else expression statement
		if p.peekAt(1).K == tIdent && p.peekAt(2).K == tLParen {
			return p.parseFuncStmt()
		}
		return p.parseExprOrAssignStmt()
	case tReturn:
		return p.parseReturn()
	case tBreak:
		p.next()
		return &Stmt{Kind: StmtBreak, Line: t.Line}, nil
	case tContinue:
		p.next()
		return &Stmt{Kind: StmtContinue, Line: t.Line}, nil
	case tIf:
		return p.parseIf()
	case tWhile:
		return p.parseWhile()
	case tFor:
		return p.parseFor()
	case tPrint:
		return p.parsePrint()
	case tSleep:
		return p.parseSleep()
	case tGo:
		return p.parseGo()
	case tImport:
		return p.parseImport()
	case tTry:
		return p.parseTry()
	case tSwitch:
		return p.parseSwitch()
	case tSelect:
		return p.parseSelect()
	case tDefer:
		return p.parseDefer()
	case tLBrace:
		return p.parseBlock()
	case tNewline, tSemi:
		// should have been skipped, but tolerate
		p.next()
		return p.parseStmt()
	case tEOF:
		return nil, p.errf(t, "unexpected end of file")
	case tRBrace:
		return nil, p.errf(t, "unexpected %q", "}")
	default:
		return p.parseExprOrAssignStmt()
	}
}

func (p *parser) parseLet() (*Stmt, error) {
	lt := p.next() // let
	nameTok := p.peek()
	if nameTok.K != tIdent {
		return nil, p.errf(nameTok, "bad let: want `let x = ...`, got %q", nameTok.Lit)
	}
	p.next()
	// optional `: type` annotation, e.g. `let x: int = 10`
	typeAnn := ""
	if p.peek().K == tColon {
		p.next()
		tn, err := p.parseTypeName()
		if err != nil {
			return nil, err
		}
		typeAnn = tn
	}
	// allow `let x` (= nil) and `let x: int` (= nil, nullable)
	if p.peek().K == tNewline || p.peek().K == tSemi || p.peek().K == tEOF || p.peek().K == tRBrace {
		return &Stmt{Kind: StmtLet, Name: nameTok.Lit, Expr: &Expr{Kind: ExprNil}, TypeAnn: typeAnn, Line: lt.Line}, nil
	}
	if _, err := p.expect(tAssign, "`=`"); err != nil {
		return nil, fmt.Errorf("%s:%d: bad let: want `let x = ...`: %v", p.path, lt.Line, err)
	}
	e, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	return &Stmt{Kind: StmtLet, Name: nameTok.Lit, Expr: e, TypeAnn: typeAnn, Line: lt.Line}, nil
}

// validTypeNames is the set of type annotation / `is` names.
func validTypeName(s string) bool {
	switch s {
	case "nil", "bool", "int", "float", "number", "string",
		"array", "map", "func", "chan", "any", "ok", "err":
		return true
	}
	return false
}

// parseTypeName parses a single type name (ident) with optional trailing `?`
// nullable marker (accepted and ignored: annotations are nullable by default).
func (p *parser) parseTypeName() (string, error) {
	t := p.peek()
	if t.K != tIdent {
		return "", p.errf(t, "want type name (nil|bool|int|float|number|string|array|map|func|chan|any|ok|err), got %q", t.Lit)
	}
	p.next()
	if !validTypeName(t.Lit) {
		return "", p.errf(t, "unknown type %q (want nil|bool|int|float|number|string|array|map|func|chan|any|ok|err)", t.Lit)
	}
	// trailing `?` (e.g. `int?`) = nullable marker, accepted for familiarity.
	if p.peek().K == tQuestion {
		p.next()
	}
	return t.Lit, nil
}

func (p *parser) parseFuncStmt() (*Stmt, error) {
	ft := p.next()   // func
	name := p.next() // ident (checked by caller)
	params, paramTypes, err := p.parseParams()
	if err != nil {
		return nil, err
	}
	// optional return type: `func f(): int { ... }`
	returnType := ""
	if p.peek().K == tColon {
		p.next()
		tn, err := p.parseTypeName()
		if err != nil {
			return nil, err
		}
		returnType = tn
	}
	body, err := p.parseBlock()
	if err != nil {
		return nil, err
	}
	return &Stmt{Kind: StmtFunc, Name: name.Lit, Names: params, ParamTypes: paramTypes, ReturnType: returnType, Body: body, Line: ft.Line}, nil
}

func (p *parser) parseParams() ([]string, []string, error) {
	if _, err := p.expect(tLParen, "`(`"); err != nil {
		return nil, nil, err
	}
	var out []string
	var types []string
	p.skipSeps()
	if p.peek().K == tRParen {
		p.next()
		return out, types, nil
	}
	for {
		// allow newlines inside parens
		for p.peek().K == tNewline {
			p.next()
		}
		t := p.peek()
		if t.K != tIdent {
			return nil, nil, p.errf(t, "bad parameter %q, want name", t.Lit)
		}
		p.next()
		out = append(out, t.Lit)
		pt := ""
		if p.peek().K == tColon {
			p.next()
			tn, err := p.parseTypeName()
			if err != nil {
				return nil, nil, err
			}
			pt = tn
		}
		types = append(types, pt)
		for p.peek().K == tNewline {
			p.next()
		}
		if p.peek().K == tComma {
			p.next()
			continue
		}
		if p.peek().K == tRParen {
			p.next()
			return out, types, nil
		}
		return nil, nil, p.errf(p.peek(), "want `,` or `)` in parameter list, got %q", p.peek().Lit)
	}
}

func (p *parser) parseBlock() (*Stmt, error) {
	lt := p.peek()
	if _, err := p.expect(tLBrace, "`{`"); err != nil {
		return nil, err
	}
	var list []*Stmt
	p.skipSeps()
	for p.peek().K != tRBrace {
		if p.atEnd() {
			return nil, p.errf(lt, "unterminated block, missing `}`")
		}
		st, err := p.parseStmt()
		if err != nil {
			return nil, err
		}
		list = append(list, st)
		p.skipSeps()
	}
	p.next() // }
	return &Stmt{Kind: StmtBlock, List: list, Line: lt.Line}, nil
}

func (p *parser) parseReturn() (*Stmt, error) {
	rt := p.next()
	k := p.peek().K
	if k == tNewline || k == tSemi || k == tEOF || k == tRBrace {
		return &Stmt{Kind: StmtReturn, Expr: &Expr{Kind: ExprNil}, Line: rt.Line}, nil
	}
	e, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	return &Stmt{Kind: StmtReturn, Expr: e, Line: rt.Line}, nil
}

func (p *parser) parseIf() (*Stmt, error) {
	it := p.next() // if
	cond, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	then, err := p.parseBlock()
	if err != nil {
		return nil, err
	}
	// skip newlines before else (allow `}\nelse {`)
	saved := p.pos
	p.skipSeps()
	if p.peek().K != tElse {
		// No else: rewind so outer skipSeps handles separators.
		p.pos = saved
		return &Stmt{Kind: StmtIf, Expr: cond, Then: then, Line: it.Line}, nil
	}
	p.next() // else
	// else if / else
	if p.peek().K == tIf {
		elseIf, err := p.parseIf()
		if err != nil {
			return nil, err
		}
		return &Stmt{Kind: StmtIf, Expr: cond, Then: then, Else: elseIf, Line: it.Line}, nil
	}
	elseBlock, err := p.parseBlock()
	if err != nil {
		return nil, err
	}
	return &Stmt{Kind: StmtIf, Expr: cond, Then: then, Else: elseBlock, Line: it.Line}, nil
}

func (p *parser) parseWhile() (*Stmt, error) {
	wt := p.next()
	cond, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	body, err := p.parseBlock()
	if err != nil {
		return nil, err
	}
	return &Stmt{Kind: StmtWhile, Expr: cond, Body: body, Line: wt.Line}, nil
}

func (p *parser) parseFor() (*Stmt, error) {
	ft := p.next() // for
	// Detect for-in: IDENT [, IDENT] in
	if p.peek().K == tIdent {
		off := 1
		var names []string
		names = append(names, p.peekAt(0).Lit)
		if p.peekAt(1).K == tComma && p.peekAt(2).K == tIdent && p.peekAt(3).K == tIn {
			names = append(names, p.peekAt(2).Lit)
			p.next()
			p.next()
			p.next()
			p.next() // in
			iter, err := p.parseExpr()
			if err != nil {
				return nil, err
			}
			body, err := p.parseBlock()
			if err != nil {
				return nil, err
			}
			_ = off
			return &Stmt{Kind: StmtForIn, Names: names, Expr: iter, Body: body, Line: ft.Line}, nil
		}
		if p.peekAt(1).K == tIn {
			p.next()
			p.next() // in
			iter, err := p.parseExpr()
			if err != nil {
				return nil, err
			}
			body, err := p.parseBlock()
			if err != nil {
				return nil, err
			}
			return &Stmt{Kind: StmtForIn, Names: names, Expr: iter, Body: body, Line: ft.Line}, nil
		}
	}
	// C-style: init ; cond ; post block
	var init *Stmt
	if p.peek().K == tSemi {
		// empty init
	} else if p.peek().K == tLBrace {
		return nil, p.errf(p.peek(), "bad for: want `for x in ...` or `for init; cond; post { }`")
	} else {
		// init: let-stmt or assign/expr without consuming `;`
		if p.peek().K == tLet {
			st, err := p.parseLet()
			if err != nil {
				return nil, err
			}
			init = st
		} else {
			st, err := p.parseMiniStmt()
			if err != nil {
				return nil, err
			}
			init = st
		}
	}
	if _, err := p.expect(tSemi, "`;`"); err != nil {
		return nil, p.errf(p.peek(), "bad for: want `for init; cond; post { }`, missing `;`")
	}
	var cond *Expr
	// allow newlines after ;
	for p.peek().K == tNewline {
		p.next()
	}
	if p.peek().K == tSemi || p.peek().K == tLBrace {
		cond = &Expr{Kind: ExprBool, BoolVal: true}
	} else {
		e, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		cond = e
	}
	if _, err := p.expect(tSemi, "`;`"); err != nil {
		return nil, p.errf(p.peek(), "bad for: want `for init; cond; post { }`, missing second `;`")
	}
	var post *Stmt
	for p.peek().K == tNewline {
		p.next()
	}
	if p.peek().K == tLBrace {
		// empty post
	} else {
		st, err := p.parseMiniStmt()
		if err != nil {
			return nil, err
		}
		post = st
	}
	body, err := p.parseBlock()
	if err != nil {
		return nil, err
	}
	return &Stmt{Kind: StmtForC, Init: init, Expr: cond, Post: post, Body: body, Line: ft.Line}, nil
}

// parseMiniStmt parses a single simple statement without trailing separators,
// used for for-C init/post: let, assignment, or expression.
func (p *parser) parseMiniStmt() (*Stmt, error) {
	if p.peek().K == tLet {
		return p.parseLet()
	}
	// try assignment vs expr: parse expr then check assign op
	startTok := p.peek()
	e, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	if op, ok := isAssignOp(p.peek().K); ok {
		opTok := p.next()
		rhs, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		name, ok := assignTargetName(e)
		if ok {
			return &Stmt{Kind: StmtAssign, Name: name, Expr: rhs, Op: op, Line: opTok.Line}, nil
		}
		// indexed/field assignment: store target in Expr, value in Exprs[0].
		return &Stmt{Kind: StmtAssign, Expr: e, Exprs: []*Expr{rhs}, Op: op, Line: opTok.Line}, nil
	}
	return &Stmt{Kind: StmtExpr, Expr: e, Line: startTok.Line}, nil
}

func assignTargetName(e *Expr) (string, bool) {
	if e.Kind == ExprVar {
		return e.Name, true
	}
	return "", false
}

func (p *parser) parsePrint() (*Stmt, error) {
	pt := p.next() // print
	// optional parens: print("hi"), print(a, b)
	// Only treat `(` as wrapping the whole arg list when the matching `)`
	// ends the statement; otherwise `print (1+2)*3` is an expression
	// starting with a parenthesized group.
	if p.peek().K == tLParen && p.parenWrapsStmt() {
		p.next()
		var args []*Expr
		// allow empty print()
		for p.peek().K == tNewline {
			p.next()
		}
		if p.peek().K == tRParen {
			p.next()
			return &Stmt{Kind: StmtPrint, Exprs: args, Line: pt.Line}, nil
		}
		for {
			for p.peek().K == tNewline {
				p.next()
			}
			e, err := p.parseExpr()
			if err != nil {
				return nil, err
			}
			args = append(args, e)
			for p.peek().K == tNewline {
				p.next()
			}
			if p.peek().K == tComma {
				p.next()
				continue
			}
			if p.peek().K == tRParen {
				p.next()
				break
			}
			return nil, p.errf(p.peek(), "want `,` or `)` in print, got %q", p.peek().Lit)
		}
		return &Stmt{Kind: StmtPrint, Exprs: args, Line: pt.Line}, nil
	}
	// bare print (newline)
	k := p.peek().K
	if k == tNewline || k == tSemi || k == tEOF || k == tRBrace {
		return &Stmt{Kind: StmtPrint, Line: pt.Line}, nil
	}
	first, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	args := []*Expr{first}
	for p.peek().K == tComma {
		p.next()
		for p.peek().K == tNewline {
			p.next()
		}
		e, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		args = append(args, e)
	}
	// compat: single-expr print keeps Expr set too
	st := &Stmt{Kind: StmtPrint, Exprs: args, Line: pt.Line}
	if len(args) == 1 {
		st.Expr = args[0]
	}
	return st, nil
}

func (p *parser) parseSleep() (*Stmt, error) {
	st := p.next() // sleep
	k := p.peek().K
	if k == tNewline || k == tSemi || k == tEOF || k == tRBrace {
		return nil, p.errf(p.peek(), "bad sleep: want `sleep 500` (ms)")
	}
	// Single expression (parens handled by expression parser,
	// so both `sleep 100` and `sleep(100)` work).
	e, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	out := &Stmt{Kind: StmtSleep, Expr: e, Line: st.Line}
	if e.Kind == ExprInt {
		out.SleepMs = e.IntVal
	}
	return out, nil
}

func (p *parser) parseGo() (*Stmt, error) {
	gt := p.next() // go
	k := p.peek().K
	if k == tNewline || k == tSemi || k == tEOF || k == tRBrace {
		return nil, p.errf(p.peek(), "go needs a statement, e.g. `go print \"hi\"`")
	}
	inner, err := p.parseStmt()
	if err != nil {
		return nil, err
	}
	return &Stmt{Kind: StmtGo, Inner: inner, Line: gt.Line}, nil
}

func (p *parser) parseImport() (*Stmt, error) {
	it := p.next() // import
	t := p.peek()
	if t.K != tString {
		return nil, p.errf(t, "bad import: want `import \"file.ks\"`, got %q", t.Lit)
	}
	p.next()
	return &Stmt{Kind: StmtImport, StrVal: t.Lit, Line: it.Line}, nil
}

func (p *parser) parseTry() (*Stmt, error) {
	tt := p.next() // try
	body, err := p.parseBlock()
	if err != nil {
		return nil, err
	}
	st := &Stmt{Kind: StmtTry, Then: body, Line: tt.Line}
	// allow `} catch` / `} finally` across newlines
	saved := p.pos
	p.skipSeps()
	if p.peek().K == tCatch {
		p.next() // catch
		// optional variable: `catch e { }` or bare `catch { }`
		if p.peek().K == tIdent {
			st.Catch = p.next().Lit
		}
		cb, err := p.parseBlock()
		if err != nil {
			return nil, err
		}
		st.CaBody = cb
		saved = p.pos
		p.skipSeps()
	}
	if p.peek().K == tFinally {
		p.next() // finally
		fb, err := p.parseBlock()
		if err != nil {
			return nil, err
		}
		st.FinBody = fb
	} else {
		p.pos = saved
	}
	if st.CaBody == nil && st.FinBody == nil {
		return nil, p.errf(tt, "try needs `catch` and/or `finally`")
	}
	return st, nil
}

func (p *parser) parseSwitch() (*Stmt, error) {
	st := p.next() // switch
	target, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	if _, err := p.expect(tLBrace, "`{`"); err != nil {
		return nil, err
	}
	out := &Stmt{Kind: StmtSwitch, Expr: target, Line: st.Line}
	p.skipSeps()
	seenDefault := false
	for p.peek().K != tRBrace {
		if p.atEnd() {
			return nil, p.errf(st, "unterminated switch, missing `}`")
		}
		t := p.peek()
		if t.K == tCase {
			if seenDefault {
				return nil, p.errf(t, "default must be the last branch in switch")
			}
			p.next()
			var vals []*Expr
			for {
				v, err := p.parseExpr()
				if err != nil {
					return nil, err
				}
				vals = append(vals, v)
				if p.peek().K == tComma {
					p.next()
					continue
				}
				break
			}
			body, err := p.parseBlock()
			if err != nil {
				return nil, err
			}
			out.Cases = append(out.Cases, &SwitchCase{Values: vals, Body: body, Line: t.Line})
		} else if t.K == tDefault {
			if seenDefault {
				return nil, p.errf(t, "duplicate default in switch")
			}
			seenDefault = true
			p.next()
			body, err := p.parseBlock()
			if err != nil {
				return nil, err
			}
			out.Cases = append(out.Cases, &SwitchCase{Body: body, IsDefault: true, Line: t.Line})
		} else {
			return nil, p.errf(t, "want `case` or `default` in switch, got %q", t.Lit)
		}
		p.skipSeps()
	}
	p.next() // }
	if len(out.Cases) == 0 {
		return nil, p.errf(st, "switch needs at least one case/default")
	}
	return out, nil
}

// parseSelect parses Go-like channel multiplexing:
//
//	select {
//	  case v = recv(c1) { print v }  # receive + bind (bind optional)
//	  case recv(c2) { print "got" }  # receive + discard
//	  case send(c3, 42) { print "sent" }
//	  case timeout(100) { print "timed out" }
//	  default { print "none ready" }
//	}
//
// Rules (Go semantics): at least one case/default; `default` (if any)
// must be last and unique; with `default` the select never blocks
// (timeouts are skipped); without `default` it blocks until one case
// is ready (multiple timeouts allowed, earliest wins; ready cases are
// chosen uniformly at random).
func (p *parser) parseSelect() (*Stmt, error) {
	st := p.next() // select
	if _, err := p.expect(tLBrace, "`{`"); err != nil {
		return nil, err
	}
	out := &Stmt{Kind: StmtSelect, Line: st.Line}
	p.skipSeps()
	seenDefault := false
	for p.peek().K != tRBrace {
		if p.atEnd() {
			return nil, p.errf(st, "unterminated select, missing `}`")
		}
		t := p.peek()
		if t.K == tCase {
			if seenDefault {
				return nil, p.errf(t, "default must be the last branch in select")
			}
			p.next() // case
			sc, err := p.parseSelectCase(t)
			if err != nil {
				return nil, err
			}
			out.SelectCases = append(out.SelectCases, sc)
		} else if t.K == tDefault {
			if seenDefault {
				return nil, p.errf(t, "duplicate default in select")
			}
			seenDefault = true
			p.next() // default
			body, err := p.parseBlock()
			if err != nil {
				return nil, err
			}
			out.SelectCases = append(out.SelectCases, &SelectCase{Kind: "default", Body: body, Line: t.Line})
		} else {
			return nil, p.errf(t, "want `case` or `default` in select, got %q", t.Lit)
		}
		p.skipSeps()
	}
	p.next() // }
	if len(out.SelectCases) == 0 {
		return nil, p.errf(st, "select needs at least one case/default")
	}
	return out, nil
}

// parseSelectCase parses one `case <op>` header (the leading `case`
// was already consumed) plus its `{ block }`.
func (p *parser) parseSelectCase(t token) (*SelectCase, error) {
	// Bind form: `case v = recv(c) { ... }`
	if p.peek().K == tIdent && p.peekAt(1).K == tAssign {
		bind := p.next().Lit
		p.next() // =
		nm := p.peek()
		if nm.K != tIdent || nm.Lit != "recv" {
			return nil, p.errf(nm, "bad select case: want `v = recv(ch)`, got %q", nm.Lit)
		}
		if !isIdent(bind) {
			return nil, p.errf(nm, "bad select bind %q, want variable name", bind)
		}
		p.next() // recv
		if _, err := p.expect(tLParen, "`(`"); err != nil {
			return nil, err
		}
		ch, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(tRParen, "`)`"); err != nil {
			return nil, err
		}
		body, err := p.parseBlock()
		if err != nil {
			return nil, err
		}
		return &SelectCase{Kind: "recv", Chan: ch, Bind: bind, Body: body, Line: t.Line}, nil
	}
	nm := p.peek()
	if nm.K != tIdent {
		return nil, p.errf(nm, "bad select case: want `recv(ch)`, `send(ch, v)` or `timeout(ms)`, got %q", nm.Lit)
	}
	switch nm.Lit {
	case "recv":
		p.next()
		if _, err := p.expect(tLParen, "`(`"); err != nil {
			return nil, err
		}
		ch, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(tRParen, "`)`"); err != nil {
			return nil, err
		}
		body, err := p.parseBlock()
		if err != nil {
			return nil, err
		}
		return &SelectCase{Kind: "recv", Chan: ch, Body: body, Line: t.Line}, nil
	case "send":
		p.next()
		if _, err := p.expect(tLParen, "`(`"); err != nil {
			return nil, err
		}
		ch, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(tComma, "`,`"); err != nil {
			return nil, err
		}
		val, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(tRParen, "`)`"); err != nil {
			return nil, err
		}
		body, err := p.parseBlock()
		if err != nil {
			return nil, err
		}
		return &SelectCase{Kind: "send", Chan: ch, Value: val, Body: body, Line: t.Line}, nil
	case "timeout":
		p.next()
		if _, err := p.expect(tLParen, "`(`"); err != nil {
			return nil, err
		}
		ms, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(tRParen, "`)`"); err != nil {
			return nil, err
		}
		body, err := p.parseBlock()
		if err != nil {
			return nil, err
		}
		return &SelectCase{Kind: "timeout", Timeout: ms, Body: body, Line: t.Line}, nil
	default:
		return nil, p.errf(nm, "bad select case: want `recv(ch)`, `send(ch, v)` or `timeout(ms)`, got %q", nm.Lit)
	}
}

func (p *parser) parseDefer() (*Stmt, error) {
	dt := p.next() // defer
	k := p.peek().K
	if k == tNewline || k == tSemi || k == tEOF || k == tRBrace {
		return nil, p.errf(p.peek(), "defer needs a call, e.g. `defer close(ch)`")
	}
	// Fast path: `defer f(args)` evaluates f and args now (Go semantics).
	saved := p.pos
	if e, err := p.parseExpr(); err == nil && e.Kind == ExprCall {
		return &Stmt{Kind: StmtDefer, Expr: e, Line: dt.Line}, nil
	}
	// Otherwise any statement works, e.g. `defer print "done"`: wrap it
	// in a zero-arg func call executed when the function returns.
	p.pos = saved
	inner, err := p.parseStmt()
	if err != nil {
		return nil, err
	}
	if inner.Kind == StmtDefer {
		return nil, p.errf(dt, "defer defer is not allowed")
	}
	wrapped := &Expr{Kind: ExprCall,
		Callee: &Expr{Kind: ExprFunc, FuncBody: &Stmt{Kind: StmtBlock, List: []*Stmt{inner}, Line: inner.Line}},
	}
	return &Stmt{Kind: StmtDefer, Expr: wrapped, Line: dt.Line}, nil
}

// parseExprOrAssignStmt: assignment (incl. indexed, op=) or expression statement.
func (p *parser) parseExprOrAssignStmt() (*Stmt, error) {
	startTok := p.peek()
	e, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	if op, ok := isAssignOp(p.peek().K); ok {
		opTok := p.next()
		rhs, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		if e.Kind == ExprVar {
			return &Stmt{Kind: StmtAssign, Name: e.Name, Expr: rhs, Op: op, Line: opTok.Line}, nil
		}
		if e.Kind == ExprIndex {
			return &Stmt{Kind: StmtAssign, Expr: e, Exprs: []*Expr{rhs}, Op: op, Line: opTok.Line}, nil
		}
		return nil, p.errf(opTok, "bad assignment target")
	}
	return &Stmt{Kind: StmtExpr, Expr: e, Line: startTok.Line}, nil
}

// ---------------------------------------------------------------------------
// Expressions (precedence climbing)
// ---------------------------------------------------------------------------

func (p *parser) parseExpr() (*Expr, error) { return p.parseOr() }

func (p *parser) parseOr() (*Expr, error) {
	left, err := p.parseAnd()
	if err != nil {
		return nil, err
	}
	for {
		k := p.peek().K
		if k == tOr || k == tOrOp {
			p.next()
			right, err := p.parseAnd()
			if err != nil {
				return nil, err
			}
			left = &Expr{Kind: ExprOr, Left: left, Right: right}
			continue
		}
		return left, nil
	}
}

func (p *parser) parseAnd() (*Expr, error) {
	left, err := p.parseEq()
	if err != nil {
		return nil, err
	}
	for {
		k := p.peek().K
		if k == tAnd || k == tAndOp {
			p.next()
			right, err := p.parseEq()
			if err != nil {
				return nil, err
			}
			left = &Expr{Kind: ExprAnd, Left: left, Right: right}
			continue
		}
		return left, nil
	}
}

func (p *parser) parseEq() (*Expr, error) {
	left, err := p.parseCmp()
	if err != nil {
		return nil, err
	}
	for {
		k := p.peek().K
		if k == tEq || k == tNe {
			p.next()
			right, err := p.parseCmp()
			if err != nil {
				return nil, err
			}
			if k == tEq {
				left = &Expr{Kind: ExprEq, Left: left, Right: right}
			} else {
				left = &Expr{Kind: ExprNe, Left: left, Right: right}
			}
			continue
		}
		return left, nil
	}
}

func (p *parser) parseCmp() (*Expr, error) {
	left, err := p.parseAdd()
	if err != nil {
		return nil, err
	}
	for {
		k := p.peek().K
		var kind ExprKind
		switch k {
		case tLt:
			kind = ExprLt
		case tLe:
			kind = ExprLe
		case tGt:
			kind = ExprGt
		case tGe:
			kind = ExprGe
		case tIn:
			p.next()
			right, err := p.parseAdd()
			if err != nil {
				return nil, err
			}
			left = &Expr{Kind: ExprIn, Left: left, Right: right}
			continue
		default:
			return left, nil
		}
		p.next()
		right, err := p.parseAdd()
		if err != nil {
			return nil, err
		}
		left = &Expr{Kind: kind, Left: left, Right: right}
	}
}

func (p *parser) parseAdd() (*Expr, error) {
	left, err := p.parseMul()
	if err != nil {
		return nil, err
	}
	for {
		k := p.peek().K
		if k == tPlus || k == tMinus {
			p.next()
			right, err := p.parseMul()
			if err != nil {
				return nil, err
			}
			if k == tPlus {
				left = &Expr{Kind: ExprAdd, Left: left, Right: right}
			} else {
				left = &Expr{Kind: ExprSub, Left: left, Right: right}
			}
			continue
		}
		return left, nil
	}
}

func (p *parser) parseMul() (*Expr, error) {
	left, err := p.parseUnary()
	if err != nil {
		return nil, err
	}
	for {
		k := p.peek().K
		if k == tStar || k == tSlash || k == tPercent {
			p.next()
			right, err := p.parseUnary()
			if err != nil {
				return nil, err
			}
			switch k {
			case tStar:
				left = &Expr{Kind: ExprMul, Left: left, Right: right}
			case tSlash:
				left = &Expr{Kind: ExprDiv, Left: left, Right: right}
			default:
				left = &Expr{Kind: ExprMod, Left: left, Right: right}
			}
			continue
		}
		return left, nil
	}
}

func (p *parser) parseUnary() (*Expr, error) {
	k := p.peek().K
	if k == tMinus {
		p.next()
		e, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		return &Expr{Kind: ExprNeg, Right: e}, nil
	}
	if k == tBang || k == tNot {
		p.next()
		e, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		return &Expr{Kind: ExprNot, Right: e}, nil
	}
	return p.parsePow()
}

// parsePow handles `**` (right-associative, tighter than unary on the
// left like Python: -2**2 == -(2**2)).
func (p *parser) parsePow() (*Expr, error) {
	base, err := p.parsePostfix()
	if err != nil {
		return nil, err
	}
	if p.peek().K == tStarStar {
		p.next()
		exp, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		return &Expr{Kind: ExprPow, Left: base, Right: exp}, nil
	}
	return base, nil
}

func (p *parser) parsePostfix() (*Expr, error) {
	base, err := p.parsePrimary()
	if err != nil {
		return nil, err
	}
	for {
		switch p.peek().K {
		case tLParen:
			p.next()
			var args []*Expr
			for p.peek().K == tNewline {
				p.next()
			}
			if p.peek().K != tRParen {
				for {
					for p.peek().K == tNewline {
						p.next()
					}
					a, err := p.parseExpr()
					if err != nil {
						return nil, err
					}
					args = append(args, a)
					for p.peek().K == tNewline {
						p.next()
					}
					if p.peek().K == tComma {
						p.next()
						continue
					}
					break
				}
			}
			for p.peek().K == tNewline {
				p.next()
			}
			if _, err := p.expect(tRParen, "`)`"); err != nil {
				return nil, err
			}
			base = &Expr{Kind: ExprCall, Callee: base, Args: args}
		case tLBracket:
			p.next()
			for p.peek().K == tNewline {
				p.next()
			}
			// slice with omitted start: a[:2], a[:]
			if p.peek().K == tColon {
				p.next()
				for p.peek().K == tNewline {
					p.next()
				}
				var end *Expr
				if p.peek().K != tRBracket {
					e, err := p.parseExpr()
					if err != nil {
						return nil, err
					}
					end = e
					for p.peek().K == tNewline {
						p.next()
					}
				}
				if p.peek().K == tColon {
					return nil, p.errf(p.peek(), "slice stride not supported, use slice()")
				}
				if _, err := p.expect(tRBracket, "`]`"); err != nil {
					return nil, err
				}
				base = &Expr{Kind: ExprSlice, Left: base, SliceEnd: end}
				continue
			}
			idx, err := p.parseExpr()
			if err != nil {
				return nil, err
			}
			for p.peek().K == tNewline {
				p.next()
			}
			// slice: a[1:3], a[1:], a[1:3] with newlines allowed
			if p.peek().K == tColon {
				p.next()
				for p.peek().K == tNewline {
					p.next()
				}
				var end *Expr
				if p.peek().K != tRBracket {
					e, err := p.parseExpr()
					if err != nil {
						return nil, err
					}
					end = e
					for p.peek().K == tNewline {
						p.next()
					}
				}
				if p.peek().K == tColon {
					return nil, p.errf(p.peek(), "slice stride not supported, use slice()")
				}
				if _, err := p.expect(tRBracket, "`]`"); err != nil {
					return nil, err
				}
				base = &Expr{Kind: ExprSlice, Left: base, SliceStart: idx, SliceEnd: end}
				continue
			}
			if _, err := p.expect(tRBracket, "`]`"); err != nil {
				return nil, err
			}
			base = &Expr{Kind: ExprIndex, Left: base, Right: idx}
		case tDot:
			p.next()
			ft := p.peek()
			if ft.K != tIdent {
				return nil, p.errf(ft, "want field name after `.`, got %q", ft.Lit)
			}
			p.next()
			base = &Expr{Kind: ExprIndex, Left: base,
				Right: &Expr{Kind: ExprString, StrVal: ft.Lit}}
		case tQuestionDot:
			p.next()
			if p.peek().K == tLBracket {
				p.next()
				for p.peek().K == tNewline {
					p.next()
				}
				if p.peek().K == tColon {
					return nil, p.errf(p.peek(), "slice with `?.` not supported, use `(a ?? [])[i:j]`")
				}
				idx, err := p.parseExpr()
				if err != nil {
					return nil, err
				}
				for p.peek().K == tNewline {
					p.next()
				}
				if p.peek().K == tColon {
					return nil, p.errf(p.peek(), "slice with `?.` not supported, use `(a ?? [])[i:j]`")
				}
				if _, err := p.expect(tRBracket, "`]`"); err != nil {
					return nil, err
				}
				base = &Expr{Kind: ExprIndex, Left: base, Right: idx, Safe: true}
				continue
			}
			ft := p.peek()
			if ft.K != tIdent {
				return nil, p.errf(ft, "want field name after `?.`, got %q", ft.Lit)
			}
			p.next()
			base = &Expr{Kind: ExprIndex, Left: base, Safe: true,
				Right: &Expr{Kind: ExprString, StrVal: ft.Lit}}
		default:
			return base, nil
		}
	}
}

func (p *parser) parsePrimary() (*Expr, error) {
	t := p.peek()
	switch t.K {
	case tString:
		p.next()
		return &Expr{Kind: ExprString, StrVal: t.Lit}, nil
	case tInt:
		p.next()
		n, err := strconv.Atoi(t.Lit)
		if err != nil {
			return nil, p.errf(t, "bad integer %q", t.Lit)
		}
		return &Expr{Kind: ExprInt, IntVal: n}, nil
	case tFloat:
		p.next()
		f, err := strconv.ParseFloat(t.Lit, 64)
		if err != nil {
			return nil, p.errf(t, "bad float %q", t.Lit)
		}
		return &Expr{Kind: ExprFloat, FloatVal: f}, nil
	case tTrue:
		p.next()
		return &Expr{Kind: ExprBool, BoolVal: true}, nil
	case tFalse:
		p.next()
		return &Expr{Kind: ExprBool, BoolVal: false}, nil
	case tNil:
		p.next()
		return &Expr{Kind: ExprNil}, nil
	case tIdent:
		p.next()
		return &Expr{Kind: ExprVar, Name: t.Lit}, nil
	case tLParen:
		p.next()
		for p.peek().K == tNewline {
			p.next()
		}
		e, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		for p.peek().K == tNewline {
			p.next()
		}
		if _, err := p.expect(tRParen, "`)`"); err != nil {
			return nil, err
		}
		return e, nil
	case tLBracket:
		return p.parseArray()
	case tLBrace:
		return p.parseMap()
	case tFunc:
		return p.parseFuncLit()
	default:
		if t.K == tEOF {
			return nil, p.errf(t, "unexpected end of expression")
		}
		if t.Lit == "" {
			return nil, p.errf(t, "unexpected end of expression")
		}
		return nil, p.errf(t, "bad expression %q", t.Lit)
	}
}

func (p *parser) parseArray() (*Expr, error) {
	p.next() // [
	var els []*Expr
	for p.peek().K == tNewline {
		p.next()
	}
	if p.peek().K == tRBracket {
		p.next()
		return &Expr{Kind: ExprArray, Elements: els}, nil
	}
	for {
		for p.peek().K == tNewline {
			p.next()
		}
		e, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		els = append(els, e)
		for p.peek().K == tNewline {
			p.next()
		}
		if p.peek().K == tComma {
			p.next()
			continue
		}
		if p.peek().K == tRBracket {
			p.next()
			return &Expr{Kind: ExprArray, Elements: els}, nil
		}
		return nil, p.errf(p.peek(), "want `,` or `]` in array, got %q", p.peek().Lit)
	}
}

func (p *parser) parseMap() (*Expr, error) {
	p.next() // {
	var keys []string
	var vals []*Expr
	for p.peek().K == tNewline || p.peek().K == tSemi {
		p.next()
	}
	if p.peek().K == tRBrace {
		p.next()
		return &Expr{Kind: ExprMap, MapKeys: keys, MapVals: vals}, nil
	}
	for {
		for p.peek().K == tNewline || p.peek().K == tSemi {
			p.next()
		}
		if p.peek().K == tRBrace {
			p.next()
			return &Expr{Kind: ExprMap, MapKeys: keys, MapVals: vals}, nil
		}
		kt := p.peek()
		var key string
		switch kt.K {
		case tString:
			key = kt.Lit
			p.next()
		case tIdent:
			key = kt.Lit
			p.next()
		case tInt:
			key = kt.Lit
			p.next()
		default:
			return nil, p.errf(kt, "bad map key %q, want `\"key\"` or `key`", kt.Lit)
		}
		if _, err := p.expect(tColon, "`:`"); err != nil {
			return nil, err
		}
		for p.peek().K == tNewline {
			p.next()
		}
		v, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		keys = append(keys, key)
		vals = append(vals, v)
		for p.peek().K == tNewline {
			p.next()
		}
		if p.peek().K == tComma || p.peek().K == tSemi {
			p.next()
			continue
		}
		if p.peek().K == tRBrace {
			continue // loop top closes
		}
		return nil, p.errf(p.peek(), "want `,` or `}` in map, got %q", p.peek().Lit)
	}
}

func (p *parser) parseFuncLit() (*Expr, error) {
	p.next() // func
	// optional name (named literal, ignored except for debugging)
	if p.peek().K == tIdent && p.peekAt(1).K == tLParen {
		p.next()
	}
	params, paramTypes, err := p.parseParams()
	if err != nil {
		return nil, err
	}
	// optional return type: `func(a: int): int { ... }`
	returnType := ""
	if p.peek().K == tColon {
		p.next()
		tn, err := p.parseTypeName()
		if err != nil {
			return nil, err
		}
		returnType = tn
	}
	body, err := p.parseBlock()
	if err != nil {
		return nil, err
	}
	return &Expr{Kind: ExprFunc, FuncParams: params, FuncParamTypes: paramTypes, FuncReturnType: returnType, FuncBody: body}, nil
}

// isIdent reports whether s is a valid identifier (kept for compat).
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
