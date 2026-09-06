package compiler

import "testing"

func benchVM(b *testing.B, src string) {
	b.Helper()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := RunSource(src, "bench.ks"); err != nil {
			b.Fatal(err)
		}
	}
}

// Same fib(20) as the interpreter bench: measures VM call/branch speed.
func BenchmarkVMFib20(b *testing.B) {
	benchVM(b, "func fib(n) {\n if n < 2 { return n }\n return fib(n-1) + fib(n-2)\n}\nassert(fib(20) == 6765)\n")
}

// Same 10k loop: measures VM arithmetic + jumps.
func BenchmarkVMLoop10k(b *testing.B) {
	benchVM(b, "let s = 0\nfor i in range(10000) {\n s = s + i\n}\nassert(s == 49995000)\n")
}
