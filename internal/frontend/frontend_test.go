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

func TestParseFuncLitAndImport(t *testing.T) {	p, err := ParseSource("let f = func(x) {\n return x + 1\n}\nimport \"lib.ks\"\nbreak\ncontinue\nreturn 1\n", "test.ks")
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
