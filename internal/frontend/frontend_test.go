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
