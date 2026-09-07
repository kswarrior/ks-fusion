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

func TestFoldDivZeroPreserved(t *testing.T) {
	for _, src := range []string{"let x = 1 / 0\n", "let x = 1.0 / 0.0\n", "let x = 1 % 0\n"} {
		p, err := ParseSource(src, "t.ks")
		if err != nil {
			t.Fatal(err)
		}
		kindBefore := p.Statements[0].Expr.Kind
		FoldProgram(p)
		if p.Statements[0].Expr.Kind != kindBefore {
			t.Fatalf("want div/mod-by-zero preserved for %q, got %+v", src, p.Statements[0].Expr)
		}
	}
	// Non-zero division still folds.
	q, _ := ParseSource("let x = 7 / 2\n", "t.ks")
	FoldProgram(q)
	if q.Statements[0].Expr.Kind != ExprFloat {
		t.Fatalf("want folded div, got %+v", q.Statements[0].Expr)
	}
}

func TestFoldPowLimit(t *testing.T) {
	p, _ := ParseSource("let x = 2 ** 10\n", "t.ks")
	FoldProgram(p)
	if p.Statements[0].Expr.Kind != ExprInt || p.Statements[0].Expr.IntVal != 1024 {
		t.Fatalf("want 1024, got %+v", p.Statements[0].Expr)
	}
	for _, src := range []string{"let x = 2 ** 31\n", "let x = 2 ** 99\n"} {
		q, _ := ParseSource(src, "t.ks")
		FoldProgram(q)
		if q.Statements[0].Expr.Kind != ExprPow {
			t.Fatalf("want **>30 no-fold for %q, got %+v", src, q.Statements[0].Expr)
		}
	}
}

func TestFoldLen(t *testing.T) {
	p, _ := ParseSource("let x = len(\"abc\")\n", "t.ks")
	FoldProgram(p)
	if p.Statements[0].Expr.Kind != ExprInt || p.Statements[0].Expr.IntVal != 3 {
		t.Fatalf("want len 3, got %+v", p.Statements[0].Expr)
	}
	q, _ := ParseSource("let x = len([1, 2])\n", "t.ks")
	FoldProgram(q)
	if q.Statements[0].Expr.Kind != ExprInt || q.Statements[0].Expr.IntVal != 2 {
		t.Fatalf("want len 2, got %+v", q.Statements[0].Expr)
	}
}

func TestFoldIsInCoalesceAndOrNot(t *testing.T) {
	foldOne := func(src string) *Expr {
		p, err := ParseSource(src, "t.ks")
		if err != nil {
			t.Fatalf("parse %q: %v", src, err)
		}
		FoldProgram(p)
		return p.Statements[0].Expr
	}
	// `is` folds for string-literal type names.
	if e := foldOne("let x = 1 is \"int\"\n"); e.Kind != ExprBool || !e.BoolVal {
		t.Fatalf("want true is-fold, got %+v", e)
	}
	if e := foldOne("let x = 1 is \"string\"\n"); e.Kind != ExprBool || e.BoolVal {
		t.Fatalf("want false is-fold, got %+v", e)
	}
	// `in` folds for literal string haystacks.
	if e := foldOne("let x = \"b\" in \"abc\"\n"); e.Kind != ExprBool || !e.BoolVal {
		t.Fatalf("want true in-fold, got %+v", e)
	}
	if e := foldOne("let x = \"z\" in \"abc\"\n"); e.Kind != ExprBool || e.BoolVal {
		t.Fatalf("want false in-fold, got %+v", e)
	}
	// `??`: non-nil left folds to left, nil left folds to right.
	if e := foldOne("let x = \"a\" ?? \"b\"\n"); e.Kind != ExprString || e.StrVal != "a" {
		t.Fatalf("want coalesce-left, got %+v", e)
	}
	if e := foldOne("let x = nil ?? 1\n"); e.Kind != ExprInt || e.IntVal != 1 {
		t.Fatalf("want coalesce-right, got %+v", e)
	}
	// and/or on bool literals.
	if e := foldOne("let x = true and false\n"); e.Kind != ExprBool || e.BoolVal {
		t.Fatalf("want false and-fold, got %+v", e)
	}
	if e := foldOne("let x = true or false\n"); e.Kind != ExprBool || !e.BoolVal {
		t.Fatalf("want true or-fold, got %+v", e)
	}
	// not/neg on literals (operand lives in Right).
	if e := foldOne("let x = not false\n"); e.Kind != ExprBool || !e.BoolVal {
		t.Fatalf("want true not-fold, got %+v", e)
	}
	if e := foldOne("let x = !true\n"); e.Kind != ExprBool || e.BoolVal {
		t.Fatalf("want false !-fold, got %+v", e)
	}
	if e := foldOne("let x = -5\n"); e.Kind != ExprInt || e.IntVal != -5 {
		t.Fatalf("want -5 neg-fold, got %+v", e)
	}
}
