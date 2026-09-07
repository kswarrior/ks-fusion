# Benchmarks (v2.7: VM v0.3; v2.5 Xeon numbers kept for reference)

Reproduce: `go test ./internal/backend/ -bench BenchmarkInterp -benchtime 10x`
and `go test ./internal/compiler/ -bench BenchmarkVM -benchtime 10x`.

## v2.7 measurements (VM v0.3, AMD EPYC 7763, `linux/amd64`, 2026-09-07)

| Bench | Interpreter | VM v0.3 | VM vs interp | What it measures |
|---|---|---|---|---|
| `fib(20)` recursion | ~20.0ms | ~12.0ms | **≈1.7x faster** | calls + branches (`BenchmarkInterpFib20` / `BenchmarkVMFib20`) |
| `for i in range(10000)` scalar loop | ~7.0ms | ~5.0ms | **≈1.4x faster** | integer loop, no allocs (`BenchmarkInterpLoop10k` / `BenchmarkVMLoop10k`) |
| `map(filter(range(1000)))` builtin chain | ~1.16ms | n/a (interp only) | — | `map`/`filter` are interpreter-only builtins |

(Earlier single runs on this box: fib 39.2 vs 11.4 ≈ 3.4x, loop 6.7 vs 5.1 ≈
1.3x. Noisy cloud box — treat every figure as ≈±30%, direction is stable:
both VM benches beat the interpreter on every run since v0.3.)

## v2.5 reference (VM v0.2, Intel Xeon 6973P-C, 2026-09-06)

| Bench | Interpreter | VM v0.2 | VM vs interp |
|---|---|---|---|
| `fib(20)` | ~16–20ms | ~7.8–8.7ms | **≈2x faster** |
| `for i in range(10000)` | ~5.3–5.6ms | ~7.9–8.8ms | **≈0.7x (slower)** |
| `map(filter(range(1000)))` | ~0.76ms | n/a | — |

## What changed in v0.3 (Perf)

- **Loop regression fixed.** v0.2 desugared every `for-in` to `__iter_len` +
  `__iter_get` builtin calls per iteration *on top of* a fully allocated
  `range(n)` array. v0.3 detects `for [k,] v in range(e) | range(a, b)` at
  compile time (`compiler.go:isRangeLoop`, mirroring `backend.rangeArgs`
  detection so both engines agree on every input) and emits a call-free
  integer loop (`compiler.go:compileForInRange`: hidden counter + end slots,
  one `Lt` + slot binds per iteration, existing opcodes only — no `.ksb`
  format change). 3-arg `range(a, b, step)` and non-range iterables keep the
  generic path (same values, slower).
- **Two rejects removed.** `sleep` compiles now (statement + call forms,
  `vm.go:bSleep` mirrors `backend.toMillis` incl. negative/non-int errors).
  `try/catch` without `finally` compiles now (`compiler.go:compileTry` +
  real `OpSetupTry`/`OpPopTry` in the VM: handler stack with frame/stack
  unwind, catch binds the raw error string like the interpreter, control
  flow never caught, `break`/`continue` pop records via lexical tryDepth,
  `OpReturn` drops frame-local records). `try/finally` stays
  interpreter-only with a clear error.
- **One parity fix.** VM `assert(x, msg)` now reports `assert failed: msg`
  like the interpreter (was bare `msg`).
- **VM builtins 7 → 8** (`assert/len/range/str/int/float/type` + `sleep`;
  plus 5 hidden `__iter_*` helpers).
- **Rejects 7 → 6** (`go`, `import`, `select`, `defer`, `struct`/`enum`
  decls, `try/finally` form). `grep -n "runs in interpreter"
  internal/compiler/compiler.go` lists them.
- **What this proves.** Both VM benches now beat the interpreter on every
  run (fib ≈1.7–3.4x, loop ≈1.3–1.4x). That is real, measured progress
  *inside* Perf 7 — not a step to 8, let alone 10: a Go-hosted stack VM
  cannot touch LLVM-native Rust/C/C++ on compute (see `docs/vs.md` §1).
  Perf 8 still needs full-VM coverage (concurrency, `import`/`defer`,
  nominal checks) with consistent wins; Perf 10 needs a native backend
  (LLVM/Cranelift or full AOT — months of work, explicitly not started).

Remaining interpreter-only (run with `fusion run`, not `fusion compile`):
`go`/`chan`/`select`, `import`, `try/finally`, `defer`,
`struct`/`enum` declarations. Each fails compile with a clear
"runs in interpreter" error (`compiler.go:compileStmt`).

See `docs/vs.md` §1 for the full v0.3 subset list + reject table.
