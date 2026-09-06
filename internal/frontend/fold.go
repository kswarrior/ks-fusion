package frontend

// Constant folding (v2.2 perf): fold literal binary ops at parse time.
// 1+2 -> 3, 2.5+1 -> 3.5, "a"+"b" -> "ab", true and false -> false, etc.
// Idempotent, never changes semantics (division by zero left unfolded).
func FoldProgram(p *Program) {
	for _, st := range p.Statements {
		foldStmt(st)
	}
}

func foldStmt(st *Stmt) {
	if st == nil {
		return
	}
	if st.Expr != nil {
		st.Expr = foldExpr(st.Expr)
	}
	for _, e := range st.Exprs {
		_ = e
	}
	for i, e := range st.Exprs {
		st.Exprs[i] = foldExpr(e)
	}
	if st.Inner != nil {
		foldStmt(st.Inner)
	}
	if st.Body != nil {
		foldStmt(st.Body)
	}
	if st.Then != nil {
		foldStmt(st.Then)
	}
	if st.Else != nil {
		foldStmt(st.Else)
	}
	if st.Init != nil {
		foldStmt(st.Init)
	}
	if st.Post != nil {
		foldStmt(st.Post)
	}
	if st.CaBody != nil {
		foldStmt(st.CaBody)
	}
	if st.FinBody != nil {
		foldStmt(st.FinBody)
	}
	for _, s := range st.List {
		foldStmt(s)
	}
	for _, c := range st.Cases {
		for i, v := range c.Values {
			c.Values[i] = foldExpr(v)
		}
		foldStmt(c.Body)
	}
	for _, c := range st.SelectCases {
		if c.Chan != nil {
			c.Chan = foldExpr(c.Chan)
		}
		if c.Value != nil {
			c.Value = foldExpr(c.Value)
		}
		if c.Timeout != nil {
			c.Timeout = foldExpr(c.Timeout)
		}
		foldStmt(c.Body)
	}
}

func isLit(e *Expr) bool {
	if e == nil {
		return false
	}
	switch e.Kind {
	case ExprString, ExprInt, ExprFloat, ExprBool, ExprNil:
		return true
	}
	return false
}

func foldExpr(e *Expr) *Expr {
	if e == nil {
		return nil
	}
	// recurse first
	if e.Left != nil {
		e.Left = foldExpr(e.Left)
	}
	if e.Right != nil {
		e.Right = foldExpr(e.Right)
	}
	for i, a := range e.Args {
		e.Args[i] = foldExpr(a)
	}
	if e.Callee != nil {
		e.Callee = foldExpr(e.Callee)
	}
	for i, el := range e.Elements {
		e.Elements[i] = foldExpr(el)
	}
	for i, mv := range e.MapVals {
		e.MapVals[i] = foldExpr(mv)
	}
	if e.SliceStart != nil {
		e.SliceStart = foldExpr(e.SliceStart)
	}
	if e.SliceEnd != nil {
		e.SliceEnd = foldExpr(e.SliceEnd)
	}
	if e.FuncBody != nil {
		foldStmt(e.FuncBody)
		return e
	}
	// try fold binary ops with literal operands
	if e.Left != nil && e.Right != nil && isLit(e.Left) && isLit(e.Right) {
		if folded, ok := foldBinary(e.Kind, e.Left, e.Right); ok {
			return folded
		}
	}
	// fold unary not/-/! on literals
	if (e.Kind == ExprNot || e.Kind == ExprNeg) && e.Left != nil && isLit(e.Left) {
		if folded, ok := foldUnary(e.Kind, e.Left); ok {
			return folded
		}
	}
	return e
}

func foldBinary(kind ExprKind, l, r *Expr) (*Expr, bool) {
	switch kind {
	case ExprAdd:
		if l.Kind == ExprInt && r.Kind == ExprInt {
			return &Expr{Kind: ExprInt, IntVal: l.IntVal + r.IntVal}, true
		}
		if l.Kind == ExprString && r.Kind == ExprString {
			return &Expr{Kind: ExprString, StrVal: l.StrVal + r.StrVal}, true
		}
		if (l.Kind == ExprInt || l.Kind == ExprFloat) && (r.Kind == ExprInt || r.Kind == ExprFloat) {
			return &Expr{Kind: ExprFloat, FloatVal: toFloatLit(l) + toFloatLit(r)}, true
		}
	case ExprSub:
		if l.Kind == ExprInt && r.Kind == ExprInt {
			return &Expr{Kind: ExprInt, IntVal: l.IntVal - r.IntVal}, true
		}
		if (l.Kind == ExprInt || l.Kind == ExprFloat) && (r.Kind == ExprInt || r.Kind == ExprFloat) {
			return &Expr{Kind: ExprFloat, FloatVal: toFloatLit(l) - toFloatLit(r)}, true
		}
	case ExprMul:
		if l.Kind == ExprInt && r.Kind == ExprInt {
			return &Expr{Kind: ExprInt, IntVal: l.IntVal * r.IntVal}, true
		}
		if (l.Kind == ExprInt || l.Kind == ExprFloat) && (r.Kind == ExprInt || r.Kind == ExprFloat) {
			return &Expr{Kind: ExprFloat, FloatVal: toFloatLit(l) * toFloatLit(r)}, true
		}
	case ExprDiv:
		// avoid folding division by zero (keep runtime error with line info)
		if (r.Kind == ExprInt && r.IntVal == 0) || (r.Kind == ExprFloat && r.FloatVal == 0) {
			return nil, false
		}
		if (l.Kind == ExprInt || l.Kind == ExprFloat) && (r.Kind == ExprInt || r.Kind == ExprFloat) {
			rv := toFloatLit(r)
			if rv == 0 {
				return nil, false
			}
			return &Expr{Kind: ExprFloat, FloatVal: toFloatLit(l) / rv}, true
		}
	case ExprMod:
		if l.Kind == ExprInt && r.Kind == ExprInt && r.IntVal != 0 {
			return &Expr{Kind: ExprInt, IntVal: l.IntVal % r.IntVal}, true
		}
	case ExprEq:
		return &Expr{Kind: ExprBool, BoolVal: litEqual(l, r)}, true
	case ExprNe:
		return &Expr{Kind: ExprBool, BoolVal: !litEqual(l, r)}, true
	case ExprLt:
		if cmp, ok := litCompare(l, r); ok {
			return &Expr{Kind: ExprBool, BoolVal: cmp < 0}, true
		}
	case ExprLe:
		if cmp, ok := litCompare(l, r); ok {
			return &Expr{Kind: ExprBool, BoolVal: cmp <= 0}, true
		}
	case ExprGt:
		if cmp, ok := litCompare(l, r); ok {
			return &Expr{Kind: ExprBool, BoolVal: cmp > 0}, true
		}
	case ExprGe:
		if cmp, ok := litCompare(l, r); ok {
			return &Expr{Kind: ExprBool, BoolVal: cmp >= 0}, true
		}
	case ExprAnd:
		if l.Kind == ExprBool && r.Kind == ExprBool {
			return &Expr{Kind: ExprBool, BoolVal: l.BoolVal && r.BoolVal}, true
		}
	case ExprOr:
		if l.Kind == ExprBool && r.Kind == ExprBool {
			return &Expr{Kind: ExprBool, BoolVal: l.BoolVal || r.BoolVal}, true
		}
	}
	return nil, false
}

func foldUnary(kind ExprKind, e *Expr) (*Expr, bool) {
	switch kind {
	case ExprNot:
		if e.Kind == ExprBool {
			return &Expr{Kind: ExprBool, BoolVal: !e.BoolVal}, true
		}
	case ExprNeg:
		if e.Kind == ExprInt {
			return &Expr{Kind: ExprInt, IntVal: -e.IntVal}, true
		}
		if e.Kind == ExprFloat {
			return &Expr{Kind: ExprFloat, FloatVal: -e.FloatVal}, true
		}
	}
	return nil, false
}

func toFloatLit(e *Expr) float64 {
	if e.Kind == ExprFloat {
		return e.FloatVal
	}
	return float64(e.IntVal)
}

func litEqual(a, b *Expr) bool {
	if a.Kind != b.Kind {
		if (a.Kind == ExprInt || a.Kind == ExprFloat) && (b.Kind == ExprInt || b.Kind == ExprFloat) {
			return toFloatLit(a) == toFloatLit(b)
		}
		return false
	}
	switch a.Kind {
	case ExprInt:
		return a.IntVal == b.IntVal
	case ExprFloat:
		return a.FloatVal == b.FloatVal
	case ExprString:
		return a.StrVal == b.StrVal
	case ExprBool:
		return a.BoolVal == b.BoolVal
	case ExprNil:
		return true
	}
	return false
}

func litCompare(a, b *Expr) (int, bool) {
	if (a.Kind == ExprInt || a.Kind == ExprFloat) && (b.Kind == ExprInt || b.Kind == ExprFloat) {
		av, bv := toFloatLit(a), toFloatLit(b)
		switch {
		case av < bv:
			return -1, true
		case av > bv:
			return 1, true
		default:
			return 0, true
		}
	}
	if a.Kind == ExprString && b.Kind == ExprString {
		switch {
		case a.StrVal < b.StrVal:
			return -1, true
		case a.StrVal > b.StrVal:
			return 1, true
		default:
			return 0, true
		}
	}
	return 0, false
}
