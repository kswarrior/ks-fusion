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

## Frontend (P4 HMR perf + budgets, 2026-09-07)

Reproduce: `go test ./internal/tools/ -bench 'BenchmarkRender|BenchmarkDiff|BenchmarkBuildJS|BenchmarkHMR' -run XXX`
(Benches live in `internal/tools/webbench_test.go`; HMR debounce/cache tests are `TestHMR*` in `internal/tools/hmr_test.go`.)

Measured on `linux/amd64`, Intel Xeon Platinum 8573C (cloud box, treat as ≈±30%):

| Bench | ns/op | What it measures |
|---|---|---|
| `BenchmarkRenderRouteRoot` (`/` hello-app) | ~270k (~0.27ms) | full `renderRouteWithStatusUncached` incl. cached-parse exec (TTFR path) |
| `BenchmarkRenderRouteHi` (`/hi`) | ~197k (~0.20ms) | full render `/hi` |
| `BenchmarkRenderRouteUser` (`/user/7`) | ~204k (~0.20ms) | full render dynamic `user_page` with `props.id="7"` (P1 path) |
| `BenchmarkHMRTickCached` (`/`, no change) | ~59k (~0.059ms) | debounced SSE tick: frontend mtime-hash + route-cache hit, no exec |
| `BenchmarkDiffLarge200` (200-child, 1 change) | ~691k (~0.69ms) | `DiffViewModels` keyed patch (worst-case virtualized list) |
| `BenchmarkBuildJS` (3 routes, same out) | ~275k (~0.28ms/call) | `BuildJS` parse+emit+hash; writes skipped by content-hash when unchanged |

Approx HMR tick cost: typical small page = cached tick (~0.06ms) + small diff (µs) ≈ **~0.06ms**;
worst case full re-render + 200-child diff ≈ 0.27ms + 0.69ms ≈ **~0.96ms**.
Both are ~100x under the **HMR <100ms** budget (`plan/frontend.md:98-99`).
Slow ticks (>100ms) log via `fmt.Printf` only when `FUSION_DEBUG_HMR=1`
(`internal/tools/webperf_p4.go:maybeLogSlowHMRTick`, watch mode) to avoid noise.

TTFR: `X-Render-Time` header is always set on `/` responses (preserved on 404);
render itself is ~0.2–0.3ms, so **TTFR <1s** holds by ~3000x locally.
TTFR >1s logs only when `FUSION_DEBUG_HMR=1` (`maybeLogSlowTTFR`).

Route JS sizes from manifest (budgets warn >100KB / fail >250KB, enforced in
`internal/tools/buildjs_p3.go`, unchanged):

- hello-app (`fusion build-js tests/hello-app`): `404.js` 492B, `hi.js` 426B,
  `index.js` 881B, `user_[id].js` 586B — all ~100x under warn.
- bench temp fixture (3 minimal pages): `hi.js` 318B, `index.js` 344B,
  `user_[id].js` 396B.

P4 implementation notes (`internal/tools/webperf_p4.go`, `webjs.go`):

- Debounce 1 render/tick: `/events` checks `watcher.version()` once per
  `sseTickInterval` (300ms, `TODO WS push`); multiple mtime bumps in the same
  window coalesce into one `renderRoute`+`DiffViewModels` per route.
- Per-route cache `route -> {vmJSON, status, mtimeHash}` (content-hash analogue):
  unchanged routes (e.g. backend-only change) skip re-exec on tick.
- Incremental parse `path -> {prog, mtime, size}`: only changed files re-parse;
  new files miss, deletes pruned. Failures never cached.
- Poll `webWatchPollInterval` stays 400ms (`TODO WS/fsnotify`).
- Invariants kept: never `location.reload` (banner on render error),
  P1 `user_[id]` routing props, hydrate JS (`__hydrate`/`__applyPatch`/etc.)
  untouched; virtualize >100 rows unchanged.
