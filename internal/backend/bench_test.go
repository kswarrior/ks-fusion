package backend

import (
	"testing"

	"github.com/kswarrior/ks-fusion/internal/frontend"
)

func benchRun(b *testing.B, src string) {
	b.Helper()
	p, err := frontend.ParseSource(src, "bench.ks")
	if err != nil {
		b.Fatal(err)
	}
	frontend.FoldProgram(p)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := Run(p); err != nil {
			b.Fatal(err)
		}
	}
}

// Fib(20) exercises calls + branches (tree-walk hot path).
func BenchmarkInterpFib20(b *testing.B) {
	benchRun(b, "func fib(n) {\n if n < 2 { return n }\n return fib(n-1) + fib(n-2)\n}\nassert(fib(20) == 6765)\n")
}

// Loop 10k scalar iterations (range fast path + int arith).
func BenchmarkInterpLoop10k(b *testing.B) {
	benchRun(b, "let s = 0\nfor i in range(10000) {\n s = s + i\n}\nassert(s == 49995000)\n")
}

// Map/filter/reduce chain (builtin call overhead).
func BenchmarkInterpMapFilter(b *testing.B) {
	benchRun(b, "let r = map(filter(range(1000), func(x) { return x % 2 == 0 }), func(x) { return x * 2 })\nassert(len(r) == 500)\n")
}
