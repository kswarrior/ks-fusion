package frontend

import "testing"

func TestParseLetPrint(t *testing.T) {
	p, err := ParseSource("let x = 10\nprint x", "test.ks")
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Statements) != 2 {
		t.Fatalf("want 2 stmts, got %d", len(p.Statements))
	}
	if p.Statements[0].Kind != StmtLet || p.Statements[0].Name != "x" {
		t.Fatalf("bad let stmt: %+v", p.Statements[0])
	}
	if p.Statements[1].Kind != StmtPrint {
		t.Fatalf("bad print stmt: %+v", p.Statements[1])
	}
}

func TestParseGoAndSleep(t *testing.T) {
	p, err := ParseSource("go print \"hi\"\nsleep 100", "test.ks")
	if err != nil {
		t.Fatal(err)
	}
	if p.Statements[0].Kind != StmtGo || p.Statements[0].Inner == nil {
		t.Fatalf("bad go stmt: %+v", p.Statements[0])
	}
	if p.Statements[1].Kind != StmtSleep || p.Statements[1].SleepMs != 100 {
		t.Fatalf("bad sleep stmt: %+v", p.Statements[1])
	}
}

func TestParseAdd(t *testing.T) {
	p, err := ParseSource("let y = 1 + 2\nprint \"a\" + y", "test.ks")
	if err != nil {
		t.Fatal(err)
	}
	if p.Statements[0].Expr.Kind != ExprAdd {
		t.Fatalf("want add expr, got %+v", p.Statements[0].Expr)
	}
}

func TestParseError(t *testing.T) {
	if _, err := ParseSource("???", "test.ks"); err == nil {
		t.Fatal("want error for bad statement")
	}
	if _, err := ParseSource("let = 5", "test.ks"); err == nil {
		t.Fatal("want error for bad let")
	}
}

func TestParseFuncIfWhile(t *testing.T) {
	src := "func add(a, b) {\n return a + b\n}\nif x > 1 {\n print x\n} else {\n print 0\n}\nwhile x > 0 {\n x = x - 1\n}\n"
	p, err := ParseSource(src, "test.ks")
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Statements) != 3 {
		t.Fatalf("want 3 stmts, got %d", len(p.Statements))
	}
	if p.Statements[0].Kind != StmtFunc || p.Statements[0].Name != "add" {
		t.Fatalf("bad func: %+v", p.Statements[0])
	}
	if len(p.Statements[0].Names) != 2 {
		t.Fatalf("want 2 params, got %+v", p.Statements[0].Names)
	}
	if p.Statements[1].Kind != StmtIf || p.Statements[1].Else == nil {
		t.Fatalf("bad if: %+v", p.Statements[1])
	}
	if p.Statements[2].Kind != StmtWhile {
		t.Fatalf("bad while: %+v", p.Statements[2])
	}
}

func TestParseForLoops(t *testing.T) {
	p, err := ParseSource("for x in [1, 2] {\n print x\n}\nfor i = 0; i < 3; i = i + 1 {\n print i\n}\n", "test.ks")
	if err != nil {
		t.Fatal(err)
	}
	if p.Statements[0].Kind != StmtForIn {
		t.Fatalf("want for-in, got %+v", p.Statements[0])
	}
	if p.Statements[1].Kind != StmtForC {
		t.Fatalf("want for-c, got %+v", p.Statements[1])
	}
	p2, err := ParseSource("for k, v in {a: 1} {\n print k\n}\n", "test.ks")
	if err != nil {
		t.Fatal(err)
	}
	if len(p2.Statements[0].Names) != 2 {
		t.Fatalf("want 2 loop vars, got %+v", p2.Statements[0].Names)
	}
}

func TestParseCollections(t *testing.T) {
	p, err := ParseSource("let a = [1, 2.5, \"x\", true, nil]\nlet m = {name: \"a\", \"k\": 1}\nprint a[0]\nprint m.name\n", "test.ks")
	if err != nil {
		t.Fatal(err)
	}
	if p.Statements[0].Expr.Kind != ExprArray {
		t.Fatalf("want array, got %+v", p.Statements[0].Expr)
	}
	if p.Statements[1].Expr.Kind != ExprMap {
		t.Fatalf("want map, got %+v", p.Statements[1].Expr)
	}
	if p.Statements[2].Expr.Kind != ExprIndex {
		t.Fatalf("want index, got %+v", p.Statements[2].Expr)
	}
}

func TestParsePrecedence(t *testing.T) {
	p, err := ParseSource("let x = 1 + 2 * 3 == 7\nlet y = not false and false\nlet z = -3 * 2\n", "test.ks")
	if err != nil {
		t.Fatal(err)
	}
	if p.Statements[0].Expr.Kind != ExprEq {
		t.Fatalf("want == at top, got %+v", p.Statements[0].Expr)
	}
	if p.Statements[2].Expr.Kind != ExprMul {
		t.Fatalf("want * at top for -3*2, got %+v", p.Statements[2].Expr)
	}
}

func TestParsePrintForms(t *testing.T) {
	for _, src := range []string{"print \"hi\"\n", "print(\"hi\")\n", "print(\"a\", \"b\")\n", "print (1 + 2) * 3\n", "print\n"} {
		if _, err := ParseSource(src, "test.ks"); err != nil {
			t.Fatalf("print %q failed: %v", src, err)
		}
	}
}

func TestParseAssignOps(t *testing.T) {
	p, err := ParseSource("let x = 1\nx += 2\nx -= 1\nx *= 3\nx /= 2\nx %= 2\nlet a = [1]\na[0] = 9\nlet m = {k: 1}\nm.k = 2\n", "test.ks")
	if err != nil {
		t.Fatal(err)
	}
	if p.Statements[1].Kind != StmtAssign || p.Statements[1].Op != "+=" {
		t.Fatalf("bad +=: %+v", p.Statements[1])
	}
}

func TestParseFuncLitAndImport(t *testing.T) {
	p, err := ParseSource("let f = func(x) {\n return x + 1\n}\nimport \"lib.ks\"\nbreak\ncontinue\nreturn 1\n", "test.ks")
	_ = p
	// break/continue/return outside func/loop still parse (runtime errors)
	if err != nil {
		t.Fatal(err)
	}
	if p.Statements[0].Expr.Kind != ExprFunc {
		t.Fatalf("want func lit, got %+v", p.Statements[0].Expr)
	}
	if p.Statements[1].Kind != StmtImport {
		t.Fatalf("want import, got %+v", p.Statements[1])
	}
}

func TestParsePowInSlice(t *testing.T) {
	p, err := ParseSource("let x = 2 ** 3 ** 2\nlet y = 2 in [1]\nlet z = a[1:2]\n", "test.ks")
	if err != nil {
		t.Fatal(err)
	}
	if p.Statements[0].Expr.Kind != ExprPow {
		t.Fatalf("want pow, got %+v", p.Statements[0].Expr)
	}
	// right-assoc: 2 ** (3 ** 2)
	if p.Statements[0].Expr.Right.Kind != ExprPow {
		t.Fatalf("want right-assoc **, got %+v", p.Statements[0].Expr.Right)
	}
	if p.Statements[1].Expr.Kind != ExprIn {
		t.Fatalf("want in, got %+v", p.Statements[1].Expr)
	}
	if p.Statements[2].Expr.Kind != ExprSlice {
		t.Fatalf("want slice, got %+v", p.Statements[2].Expr)
	}
	for _, src := range []string{"let a = [1,2]\nprint a[:2]\n", "print a[1:]\n", "print a[:]\n", "print \"hi\"[-2:]\n"} {
		if _, err := ParseSource(src, "test.ks"); err != nil {
			t.Fatalf("slice %q failed: %v", src, err)
		}
	}
}

func TestParseTrySwitchDefer(t *testing.T) {
	p, err := ParseSource("try {\n print 1\n} catch e {\n print e\n} finally {\n print 2\n}\n", "test.ks")
	if err != nil {
		t.Fatal(err)
	}
	if p.Statements[0].Kind != StmtTry || p.Statements[0].CaBody == nil || p.Statements[0].FinBody == nil {
		t.Fatalf("bad try: %+v", p.Statements[0])
	}
	if p.Statements[0].Catch != "e" {
		t.Fatalf("bad catch var: %+v", p.Statements[0])
	}
	p2, err := ParseSource("switch x {\n case 1, 2 { print 1 }\n default { print 2 }\n}\ndefer close(c)\ndefer print \"done\"\n", "test.ks")
	if err != nil {
		t.Fatal(err)
	}
	if p2.Statements[0].Kind != StmtSwitch || len(p2.Statements[0].Cases) != 2 {
		t.Fatalf("bad switch: %+v", p2.Statements[0])
	}
	if p2.Statements[1].Kind != StmtDefer || p2.Statements[2].Kind != StmtDefer {
		t.Fatalf("bad defer: %+v", p2.Statements)
	}
	// try needs catch and/or finally
	if _, err := ParseSource("try {\n print 1\n}\n", "test.ks"); err == nil {
		t.Fatal("want error for bare try")
	}
	// switch needs branches
	if _, err := ParseSource("switch x {\n}\n", "test.ks"); err == nil {
		t.Fatal("want error for empty switch")
	}
}

func TestLexNewForms(t *testing.T) {
	p, err := ParseSource("let a = 'hi'\nlet b = 0xFF\nlet c = 1_000\nlet d = 1e3\nlet e = .5\n/* block */\nprint a\n", "test.ks")
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Statements) != 6 {
		t.Fatalf("want 6 stmts, got %d", len(p.Statements))
	}
	if p.Statements[0].Expr.Kind != ExprString || p.Statements[0].Expr.StrVal != "hi" {
		t.Fatalf("bad single-quote string: %+v", p.Statements[0].Expr)
	}
	if p.Statements[1].Expr.Kind != ExprInt || p.Statements[1].Expr.IntVal != 255 {
		t.Fatalf("bad hex: %+v", p.Statements[1].Expr)
	}
	if p.Statements[3].Expr.Kind != ExprFloat {
		t.Fatalf("bad exponent float: %+v", p.Statements[3].Expr)
	}
	if _, err := ParseSource("/* unterminated", "test.ks"); err == nil {
		t.Fatal("want error for unterminated block comment")
	}
}

func TestParseSelect(t *testing.T) {
	src := "select {\n case v = recv(c1) { print v }\n case recv(c2) { print 1 }\n case send(c3, 42) { print 2 }\n case timeout(100) { print 3 }\n default { print 4 }\n}\n"
	p, err := ParseSource(src, "test.ks")
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Statements) != 1 || p.Statements[0].Kind != StmtSelect {
		t.Fatalf("want select stmt, got %+v", p.Statements)
	}
	cases := p.Statements[0].SelectCases
	if len(cases) != 5 {
		t.Fatalf("want 5 select cases, got %d", len(cases))
	}
	if cases[0].Kind != "recv" || cases[0].Bind != "v" {
		t.Fatalf("bad recv bind case: %+v", cases[0])
	}
	if cases[1].Kind != "recv" || cases[1].Bind != "" {
		t.Fatalf("bad recv discard case: %+v", cases[1])
	}
	if cases[2].Kind != "send" || cases[2].Value == nil {
		t.Fatalf("bad send case: %+v", cases[2])
	}
	if cases[3].Kind != "timeout" || cases[3].Timeout == nil {
		t.Fatalf("bad timeout case: %+v", cases[3])
	}
	if cases[4].Kind != "default" {
		t.Fatalf("bad default case: %+v", cases[4])
	}
	for _, bad := range []string{
		"select {\n}\n",                                    // empty
		"select {\n default { print 1 }\n default { print 2 }\n}\n", // dup default
		"select {\n default { print 1 }\n case recv(c) { print 2 }\n}\n", // default first
		"select {\n case foo(c) { print 1 }\n}\n",          // unknown op
		"select {\n case recv(c) }\n",                      // missing block
		"select {\n case v = send(c, 1) { print 1 }\n}\n",  // bind only with recv
		"select\n",                                         // missing brace
	} {
		if _, err := ParseSource(bad, "test.ks"); err == nil {
			t.Fatalf("want error for %q", bad)
		}
	}
}

func TestParseTypesV21(t *testing.T) {
	p, err := ParseSource("let x: int = 10\nlet y: string\nfunc add(a: int, b: int): int {\n return a + b\n}\nlet f = func(a: string): string {\n return a\n}\n", "test.ks")
	if err != nil {
		t.Fatal(err)
	}
	if p.Statements[0].TypeAnn != "int" {
		t.Fatalf("bad let ann: %+v", p.Statements[0])
	}
	if p.Statements[1].TypeAnn != "string" {
		t.Fatalf("bad let ann: %+v", p.Statements[1])
	}
	fn := p.Statements[2]
	if fn.Kind != StmtFunc || len(fn.ParamTypes) != 2 || fn.ParamTypes[0] != "int" || fn.ReturnType != "int" {
		t.Fatalf("bad func ann: %+v", fn)
	}
	if p.Statements[3].Expr.Kind != ExprFunc || p.Statements[3].Expr.FuncReturnType != "string" {
		t.Fatalf("bad func lit ann: %+v", p.Statements[3].Expr)
	}
	for _, src := range []string{
		"print a?.b\n", "print a?.[0]\n", "print a ?? b\n",
		"print x is int\n", "print x is \"int\"\n", "print x is not int\n",
		"let m: int? = nil\n",
	} {
		if _, err := ParseSource(src, "test.ks"); err != nil {
			t.Fatalf("types %q failed: %v", src, err)
		}
	}
	q, err := ParseSource("let a = x ?? y ?? 1\n", "test.ks")
	if err != nil {
		t.Fatal(err)
	}
	if q.Statements[0].Expr.Kind != ExprCoalesce {
		t.Fatalf("want ??, got %+v", q.Statements[0].Expr)
	}
	r, err := ParseSource("let b = x is int\n", "test.ks")
	if err != nil {
		t.Fatal(err)
	}
	if r.Statements[0].Expr.Kind != ExprIs {
		t.Fatalf("want is, got %+v", r.Statements[0].Expr)
	}
	s, err := ParseSource("print a?.b\n", "test.ks")
	if err != nil {
		t.Fatal(err)
	}
	// print a?.b -> print(ExprIndex Safe)
	pe := s.Statements[0]
	var idx *Expr
	if len(pe.Exprs) > 0 {
		idx = pe.Exprs[0]
	} else {
		idx = pe.Expr
	}
	if idx.Kind != ExprIndex || !idx.Safe {
		t.Fatalf("want safe ?., got %+v", idx)
	}
	// `is` stays contextual: `let is = 1` keeps working
	if _, err := ParseSource("let is = 1\nprint is\n", "test.ks"); err != nil {
		t.Fatalf("is as var failed: %v", err)
	}
	for _, bad := range []string{
		"let x: nope = 1\n",
		"print a ? b\n",
		"print 1 ??\n",
	} {
		if _, err := ParseSource(bad, "test.ks"); err == nil {
			t.Fatalf("want error for %q", bad)
		}
	}
}
