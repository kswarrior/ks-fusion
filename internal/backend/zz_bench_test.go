package backend
import (
  "testing"
  "github.com/kswarrior/ks-fusion/internal/frontend"
)
func BenchmarkFib25(b *testing.B) {
  src := "func fib(n) {\n if n <= 1 { return n }\n return fib(n-1) + fib(n-2)\n}\nlet r = fib(20)\n"
  p, _ := frontend.ParseSource(src, "bench.ks")
  b.ResetTimer()
  for i:=0;i<b.N;i++ { Run(p) }
}
