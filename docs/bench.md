# Benchmarks (v2.5, real measurements)

Measured 2026-09-06 on `linux/amd64` (Intel Xeon 6973P-C, `go test -bench`).
Reproduce: `go test ./internal/backend/ -bench BenchmarkInterp -benchtime 10x`
and `go test ./internal/compiler/ -bench BenchmarkVM -benchtime 10x`.

## Interpreter (tree-walk, full language)

| Bench | Time/op (1x) | What it measures |
|---|---|---|
| `BenchmarkInterpFib20` | ~20.2ms | `fib(20)` recursion (calls + branches) |
| `BenchmarkInterpLoop10k` | ~5.6ms | `for i in range(10000)` scalar loop |
| `BenchmarkInterpMapFilter` | ~0.76ms | `map(filter(range(1000)))` builtin chain |

## VM v0.2 (bytecode, expanded subset)

| Bench | Time/op (1x) | vs interpreter | What it measures |
|---|---|---|---|
| `BenchmarkVMFib20` | ~8.7ms | **2.3x faster** | same `fib(20)` via bytecode |
| `BenchmarkVMLoop10k` | ~8.8ms | 1.6x slower | same 10k loop (`for-in` desugar via `__iter_*` calls) |

## What this proves (Perf)

- VM v0.2 covers slices, `is`/`?.`/`??`, typed params/lets, `switch`,
  plus O(log n) int `**` (was O(n) in v0.1): `internal/compiler/vm.go`
  (`arith` exponentiation by squaring), `internal/compiler/compiler.go`
  (`OpSlice/OpIs/OpSafeIndex/OpCheckType/OpJumpIfNotNil`, `compileSwitch`).
- Fib shows a real 2x+ bytecode win on call-heavy code (the 5–20x AOT
  target needs native codegen + full-language coverage; this is step one).
- Loop shows the current cost: `for-in` desugars to `__iter_*` builtin
  calls per iteration. A range-int fast path in the VM (no call per
  iteration) is the next Perf item.
- Remaining interpreter-only (run with `fusion run`, not `fusion compile`):
  `go`/`chan`/`select`, `import`, `try/catch`, `defer`, `sleep`,
  `struct`/`enum` declarations. Each fails compile with a clear
  "runs in interpreter" error (`compiler.go:compileStmt`).

See `docs/vs.md` §1 for the full v0.2 subset list + reject table.
