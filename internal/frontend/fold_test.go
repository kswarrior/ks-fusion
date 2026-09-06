package frontend

import "testing"

func TestFoldConstants(t *testing.T) {
	p, err := ParseSource("let x = 1 + 2\nlet s = \"a\" + \"b\"\nassert(2 * 3 == 6)\n", "t.ks")
	if err != nil {
		t.Fatal(err)
	}
	FoldProgram(p)
	if p.Statements[0].Expr.Kind != ExprInt || p.Statements[0].Expr.IntVal != 3 {
		t.Fatalf("want folded 3, got %+v", p.Statements[0].Expr)
	}
	if p.Statements[1].Expr.Kind != ExprString || p.Statements[1].Expr.StrVal != "ab" {
		t.Fatalf("want folded ab, got %+v", p.Statements[1].Expr)
	}
}

func TestFoldIdempotent(t *testing.T) {
	src := "let x = (1 + 2) * 3\n"
	p, _ := ParseSource(src, "t.ks")
	FoldProgram(p)
	first := p.Statements[0].Expr.IntVal
	FoldProgram(p)
	if p.Statements[0].Expr.IntVal != first || first != 9 {
		t.Fatalf("want 9 idempotent, got %d", first)
	}
}
