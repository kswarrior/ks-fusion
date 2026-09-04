// Package frontend is the ks-fusion compiler frontend v1.0:
// full lexer + recursive-descent parser for .ks files.
//
// Language summary (v1.0):
//   types: nil, bool, int, float, string, array, map, func, chan
//   stmts: let, assign (+= -= *= /= %=), print, sleep, go,
//          if/else, while, for-in, for-c-style, func, return,
//          break, continue, import, block { }, expr-statement
//   exprs: literals, vars, a+b - * / %, == != < <= > >=,
//          and/or/not (also && || !), unary - !,
//          calls f(...), index a[i], field m.key,
//          arrays [..], maps {k: v, ..}, func literals
//   comments: # ... and // ...
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
	Args       []*Expr
	Callee     *Expr
	Elements   []*Expr
	MapKeys    []string
	MapVals    []*Expr
	FuncParams []string
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
)

// Stmt is one statement node.
type Stmt struct {
	Kind     StmtKind
	Name     string   // let/assign var, func name
	Names    []string // for-in vars, func params (def)
	Expr     *Expr    // let value, assign value, return, while cond, if cond, for iter/cond, sleep value, expr-stmt
	Exprs    []*Expr  // print args
	Inner    *Stmt    // go inner
	Body     *Stmt    // func/while/for body (block)
	Then     *Stmt    // if then (block)
	Else     *Stmt    // if else (block or if)
	Init     *Stmt    // for-c init (may be nil)
	Post     *Stmt    // for-c post (may be nil)
	List     []*Stmt  // block statements
	StrVal   string   // import path
	Op       string   // assign op: = += -= *= /= %=
	Line     int
	SleepMs  int // kept for compat: set when sleep arg is int literal
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
		// strings
		if c == '"' {
			startLine := line
			i++
			var sb strings.Builder
			closed := false
			for i < n {
				ch := src[i]
				if ch == '"' {
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
					case '\\':
						sb.WriteByte('\\')
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
		// numbers
		if c >= '0' && c <= '9' {
			start := i
			for i < n && src[i] >= '0' && src[i] <= '9' {
				i++
			}
			if i < n && src[i] == '.' && i+1 < n && src[i+1] >= '0' && src[i+1] <= '9' {
				i++ // dot
				for i < n && src[i] >= '0' && src[i] <= '9' {
					i++
				}
				add(tFloat, src[start:i])
			} else {
				add(tInt, src[start:i])
			}
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
	// allow `let x` (= nil)
	if p.peek().K == tNewline || p.peek().K == tSemi || p.peek().K == tEOF || p.peek().K == tRBrace {
		return &Stmt{Kind: StmtLet, Name: nameTok.Lit, Expr: &Expr{Kind: ExprNil}, Line: lt.Line}, nil
	}
	if _, err := p.expect(tAssign, "`=`"); err != nil {
		return nil, fmt.Errorf("%s:%d: bad let: want `let x = ...`: %v", p.path, lt.Line, err)
	}
	e, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	return &Stmt{Kind: StmtLet, Name: nameTok.Lit, Expr: e, Line: lt.Line}, nil
}

func (p *parser) parseFuncStmt() (*Stmt, error) {
	ft := p.next() // func
	name := p.next() // ident (checked by caller)
	params, err := p.parseParams()
	if err != nil {
		return nil, err
	}
	body, err := p.parseBlock()
	if err != nil {
		return nil, err
	}
	return &Stmt{Kind: StmtFunc, Name: name.Lit, Names: params, Body: body, Line: ft.Line}, nil
}

func (p *parser) parseParams() ([]string, error) {
	if _, err := p.expect(tLParen, "`(`"); err != nil {
		return nil, err
	}
	var out []string
	p.skipSeps()
	if p.peek().K == tRParen {
		p.next()
		return out, nil
	}
	for {
		// allow newlines inside parens
		for p.peek().K == tNewline {
			p.next()
		}
		t := p.peek()
		if t.K != tIdent {
			return nil, p.errf(t, "bad parameter %q, want name", t.Lit)
		}
		p.next()
		out = append(out, t.Lit)
		for p.peek().K == tNewline {
			p.next()
		}
		if p.peek().K == tComma {
			p.next()
			continue
		}
		if p.peek().K == tRParen {
			p.next()
			return out, nil
		}
		return nil, p.errf(p.peek(), "want `,` or `)` in parameter list, got %q", p.peek().Lit)
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
		p.pos = saved
		// rewind to right after then-block but keep single newline handling:
		// find actual position after then block: it is saved-? Simpler: restore
		// and consume at most one separator set only if else follows.
		// We already know no else, so restore to right after block + seps? No:
		// outer skipSeps will handle them. Restore to after-block.
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
	start := p.pos
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
		// indexed/field assignment: keep target expr in Exprs? Use Expr=target, Exprs=[rhs]?
		// Encode as StmtAssign with Name="" and Expr=rhs, Inner holds target? Instead
		// store target in Then.Body? Simplest: store target expr in Expr, value in Exprs[0].
		// But StmtAssign.Name is used for plain vars. For complex targets we stash
		// target in a dedicated way: use Names=nil, Expr=target, Exprs=[value].
		_ = start
		return &Stmt{Kind: StmtAssign, Expr: e, Exprs: []*Expr{rhs}, Op: op, Line: opTok.Line}, nil
	}
	return &Stmt{Kind: StmtExpr, Expr: e, Line: eLine(e)}, nil
}

func assignTargetName(e *Expr) (string, bool) {
	if e.Kind == ExprVar {
		return e.Name, true
	}
	return "", false
}

func eLine(e *Expr) int { return 0 }

func (p *parser) parsePrint() (*Stmt, error) {
	pt := p.next() // print
	// optional parens: print("hi"), print(a, b)
	if p.peek().K == tLParen {
		// Lookahead: is this `print (expr, ...)`? Consume and parse list.
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
	var e *Expr
	if p.peek().K == tLParen {
		p.next()
		for p.peek().K == tNewline {
			p.next()
		}
		inner, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		for p.peek().K == tNewline {
			p.next()
		}
		if _, err := p.expect(tRParen, "`)`"); err != nil {
			return nil, err
		}
		e = inner
	} else {
		inner, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		e = inner
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

// parseExprOrAssignStmt: assignment (incl. indexed, op=) or expression statement.
func (p *parser) parseExprOrAssignStmt() (*Stmt, error) {
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
	// bare identifier alone is an error like v0.1 ("unknown statement")?
	// Keep friendly: lone var/calls are expression statements (no-op except calls).
	// But a lone unknown word previously errored via "unknown statement".
	// Preserve error for lone identifier that is not a call? No - allow it.
	return &Stmt{Kind: StmtExpr, Expr: e, Line: opLine(e)}, nil
}

func opLine(e *Expr) int { return 0 }

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
	return p.parsePostfix()
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
			idx, err := p.parseExpr()
			if err != nil {
				return nil, err
			}
			for p.peek().K == tNewline {
				p.next()
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
	params, err := p.parseParams()
	if err != nil {
		return nil, err
	}
	body, err := p.parseBlock()
	if err != nil {
		return nil, err
	}
	return &Expr{Kind: ExprFunc, FuncParams: params, FuncBody: body}, nil
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
