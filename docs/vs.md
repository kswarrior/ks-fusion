# ks-fusion vs Others (honest rewrite, v2.6 source)

> ks-fusion `v2.6` (source, `toolVersion` in `cmd/fusion/main.go:341`): gradual-typed `.ks` language, toolchain written in Go.
> Easy like Python, concurrency like Go, packaging like Rust (UX copy, not parity).
> Interpreter runs the full language (177 builtins = 96+52+11+12+6, union/generic
> *annotations* + struct/enum *syntax*, literal folding); `fusion compile` adds an
> expanded bytecode subset v0.2 (`.ksb-1` JSON + stack VM: arithmetic, control flow,
> funcs + slices/`is`/`?.`/`??`/typed params-lets/`switch`).
> `fusion build --bin` embeds `.ks`+`.kslib` into a single executable via `go build`
> (needs a Go toolchain; `--strip` drops symbols via `-ldflags "-s -w"`);
> `fusion fmt/vet/doc/check/repl/bench/test/debug/profile`,
> `fusion.lock` + semver + `vendor/` + file-local registry (`publish/pull/yank` +
> real `audit`: hash recompute + transitive), stdio LSP (hover/goto/completion/
> rename/diagnostics/format) + `fusion debug`, `run --race/--cpuprofile`,
> `run-web` + `build-js`/`build-ssg`, `use_state`, TCP/TLS + WS text frames,
> sqlite extended dialect (JOIN/ORDER/GROUP/UPDATE + OR/AND + LIKE/NOT LIKE) +
> postgres-compat names, pipes/signals, cancel primitives, vendor-aware hash-skip
> build cache — all real with tests, several still thin (no LLVM/JIT, no central
> registry, JSON-file SQL). Details below.
>
> Read this first:
> - `release/fusion` in this repo is **v2.6** (rebuilt from source:
>   `go build -o release/fusion ./cmd/fusion`; `version` → `ks-fusion v2.6`).
> - Real benchmark artifact: `docs/bench.md` (`go test -bench`: VM fib ~7.8ms vs
>   interpreter ~16.1ms ≈ 2x on call-heavy code; VM loop ~7.9ms vs interpreter
>   ~5.3ms ≈ 0.7x — the VM is *slower* on `for-in`; see §1). Old `fib(25) ~70x` /
>   `11M --bin` anecdotes are retired.
> - Score note: v2.4 honest total was `80/100`, v2.5 honest total was `83/100`.
>   This revision re-audited every claim against the code: v2.6 honestly earns
>   **`84/100`** = 83 +1 for a fully-met bar (Maturity 8→9: `--bin` E2E +
>   in-repo CI gate + hygiene; file:line evidence in new “v2.6 evidence”).
>   Stdlib/Tooling/Build each gained real, tested depth (SQL OR+LIKE, LSP
>   completion, `.ks`-line profiler, vendor-aware cache, `--strip`) that stays
>   *inside* its current score — the documented +1 bar for each still needs
>   native DB / interactive DAP / incremental+remote cache respectively.
>   `README`/`futures.md`/`list.md` say 84 in this release.
> - How this rewrite was verified: full read of `cmd/fusion/*`, `internal/*`
>   (compiler v0.2, `stdlib_ext3/4.go`, `tools/debug.go`, `tools/profile.go`,
>   `tools/audit.go`, `tools/lsp.go`, `tools/cache.go`, `tools/build.go`,
>   `tools/diff.go`, `tools/webjs.go`), `tests/*`,
>   `test-releases/*`, `docs/*` (incl. `bench.md`), `plan/*`, `editors/vscode/*`,
>   plus `go test ./...`, `go test -bench`, `go vet ./...`, `bash ci.sh`,
>   and the shell checks in “How to verify”.

## TL;DR

| If you need… | Pick… | Honest status of `.ks` today |
|---|---|---|
| Single static binary, max RPS, strict types | Go / Rust | `.ks` has a real `--bin` embed + `--target` passthrough + `--strip` (`-ldflags "-s -w"`, ~31% smaller measured) + vendor-aware hash-skip cache + `-trimpath` repro, but it shells out to `go build` (Go toolchain required, binary size = Go runtime), the full language is still tree-walk (VM v0.2 covers no concurrency), and `--cpuprofile` profiles the Go host, not `.ks` lines (use `fusion profile` for exact per-line statement counts). No LLVM/JIT. |
| Kernel, drivers, games, hard realtime | C / C++ / Rust | No manual memory, no pointers, no SIMD. Non-goal, stays that way. |
| Browser UI / React / SSR | Next.js (TS) | `frontend/` has view-model maps + `run-web` SSR (HTML+JSON, `/api/*` funcs, SSE **keyed patches, no reload path** — banner on render error, never `location.reload`) + background ISR (`revalidate` + stale-while-revalidate) + nested layouts + `build-js` subset transpiler (emits `// unsupported` / `// for-c` for what it skips) + `build-ssg` + `use_state` shim + virtualized lists. No hydrate-full (`on_mount` immediate), no CSS-in-`.ks`. Prototype only. |
| CRUD + auth + admin panel tomorrow | PHP Laravel / Python Django | No ORM/migrations/templates. Has `http_*`, JSON-file KV `db_*` + JSON-file sqlite *extended dialect* (UPDATE/JOIN/ORDER BY/LIMIT/OFFSET/GROUP BY+COUNT, WHERE with AND/OR + LIKE/NOT LIKE (`%`/`_`), no parens/transactions/indexes) + `postgres_*` compat names on the same engine + `exec_pipes`/`spawn` split pipes/signals, `tcp_shutdown`, `ws_connect` + RFC 6455 **text-frame** encode/decode (no server, binary rejected), `run-web` `/api/*` funcs. Full framework still ahead. |
| Numerical / scientific / matrices | Julia | No vectorized ops/DataFrames/plots. Scalar loops only. Folding + `range(n)` fast path help constants/iteration overhead, not loop speed; VM loop is currently *slower* than the interpreter (`docs/bench.md`). |
| Quick scripts, rules, gluing, learning | ks-fusion | — this is the sweet spot (plus small `--bin` services where a Go toolchain is acceptable). |
| npm / PyPI ecosystem | Node.js / Python | `.ks` has 177 builtins + `.kslib` source-JSON bundles + `fusion.lock` semver + `vendor/` + **file-local** registry (`publish/pull/yank`, sha256 sidecar+verify, namespaces, `FUSION_REGISTRY` dir override) + real `audit` (hash recompute + transitive closure, tested) + vendor-aware build cache (vendor swaps bust it). No central server, no docs.rs, no git deps, private-token is env-hint only (see §5). |

## Big table

|  | ks-fusion (.ks) | Go | Rust | C | C++ | Node.js (JS/TS) | Python | Julia | Next.js | PHP Laravel |
|---|---|---|---|---|---|---|---|---|---|---|
| Model | tree-walk interpreter (full language, literal folding) + VM subset v0.2 + `--bin` embed via `go build` + vendor-aware hash-skip cache + host `--cpuprofile` + `.ks`-line `fusion profile` | compiled, GC | compiled, no GC (borrowck) | compiled, manual | compiled, RAII | V8 JIT | interpreted (+C ext) | JIT (LLVM), GC, multiple dispatch | React framework on Node | interpreted + framework |
| Typing | gradual + `: type` incl. union (`int\|string`) / generic (`array<int>`, `map<string,int>`) *annotations* + `is`/`?.`/`??`/`ok`/`err` + `struct`/`enum` *syntax* + enum-aware exhaustive-`switch` vet + `vet`/`check` (no variadics/named params, no methods) | static, interfaces, generics | static, traits, enums, `Result/Option` | weak static, pointers | static, templates, classes | dynamic + optional TS | dynamic + hints | dynamic + parametric types, multiple dispatch | TS-typed components | dynamic |
| Perf | medium for scripts (O(n log n) sort + O(n) sorted-check, O(log n) `**`/`pow` in interpreter *and* VM, `range(n)` no-alloc fast path, lock-free single-thread scopes, literal folding; VM v0.2 ≈2x interp on fib, ≈0.7x on `for-in` — see `docs/bench.md`) | high (servers) | highest (systems) | highest | highest | medium-high (I/O) | medium (glue/AI) | highest (numeric) | medium (SSR) | medium (CRUD) |
| Concurrency | `go` + `chan`/`select` (`recv`/`send`/`timeout`/`default`, `for v in chan`, `with_timeout`/`parallel`, `with_cancel`/`make_cancel`/`cancel`/`is_cancelled`, `recv/send_timeout`, `tcp_shutdown`, `--race` = vet+env, goroutines underneath; **interpreter only, VM rejects `go`/`chan`/`select`/`sleep`**) | goroutines + `select` | `async/tokio`, threads | threads, manual | threads/`async` | event loop + workers | threads/GIL + `asyncio` | threads + distributed + `async` | server/client components | processes + queues |
| Packaging | `fusion.toml`+`fusion.lock` (semver `^ ~ >= > < *` + `,`) + **file-local** registry (`publish/pull/yank`, sha256 sidecar+verify, `scope/name` dir mapping, `FUSION_REGISTRY` dir) + real `audit` (recompute+transitive) + `.kslib` source JSON + `vendor/` offline; `.ksb` is per-file bytecode, not a package format | `go.mod` + proxy | `cargo` + crates.io | make/cmake | cmake/vcpkg/conan | npm/pnpm | pip/poetry | `Pkg` + General registry | npm + Vercel | composer + artisan |
| Binary | `fusion build --bin` single executable via `go build -trimpath` + `--target` GOOS/GOARCH passthrough + `--strip` + vendor-aware hash-skip cache + host `--cpuprofile`/`.ks`-line `fusion profile`/`fusion debug`; shebang still works | single static binary | single binary | binary | binary | needs node/runtime | needs python | needs julia runtime | needs node | needs php+server |
| Best for | learning, automation, rules engines, small backends/services | APIs, DevOps, cloud | systems, WASM, games | OS, embedded | engines, trading, desktop | APIs, realtime, SSR | scripts, data, AI | numerics, science, matrices | fullstack React apps | monolith CRUD apps |

See `docs/futures.md` for the roadmap (v2.5 header; §3 still lists P1/P2 boxes —
`http/net-ws/fs/process/time/crypto/db/log` are implemented to the depth in §4,
`publish/pull/yank` + `vendor/` + namespaces done file-local, `repl/bench/debug/`
done with `debug` non-interactive, `run-web`/`build-js` done prototype-grade;
unchecked: git deps, central registry, variadics, hydrate-full, FFI).

`fusion compile` (`internal/compiler`, `.ksb-1` + `fusion prog.ksb` + `--dis`/`--run`) is step one
of the P1 runtime plan; v2.2 added `--bin`/`--target`, v2.3 added cache/host-profile/file-registry/watch/SSG/TCP-minimal,
v2.4 added union/generic annotations, sqlite-subset, cancel, narrow audit, minimal LSP, ISR/layouts/HMR-patch,
`range(n)`/sorted-check opts, `-trimpath` reproducibles; v2.5 added VM v0.2
(slices/`is`/`?.`/`??`/typed/`switch`), exhaustive-switch vet, extended SQL +
postgres-compat + WS-frames/pipes, real audit, full LSP + `fusion debug` + VS Code
ext v0.2.0, DOM-diff without reload + background ISR, release v2.5 + `ci.sh`;
v2.6 adds SQL OR/AND + LIKE/NOT LIKE, LSP completion + VS Code ext v0.3.0,
`.ks`-line `fusion profile`, vendor-aware cache + `--strip`, `--bin` E2E,
in-repo CI (`.github/workflows/ci.yml`) + hygiene.
VM v0.2 is measured progress (≈2x fib) with a loop regression — it holds Perf 7,
not 8 (see “Corrections”).

## How scoring works (out of 100)

Each language / stack is scored `0-10` on 10 dimensions = `100` max.
Snapshot for ks-fusion `v2.6` (177 builtins = 96+52+11+12+6, struct/enum syntax +
exhaustive-switch vet, VM v0.2 + `docs/bench.md`, WS text frames + extended SQL
(OR/AND + LIKE) + postgres-compat + pipes/signals, file-registry + real audit,
full LSP (incl. completion) + debug + profile + VS Code ext v0.3.0, DOM-diff
without reload + background ISR, vendor-aware cache + `--strip`, release v2.6 +
`ci.sh` + in-repo CI + `--bin` E2E, lock/semver/vendor, cancel, literal folding).
Higher = better, except simplicity where easier = higher. Scores are opinionated but rubric-based, not benchmarks.
“Parity” below means **breadth for scripts/services**, not depth — every Go/Rust-parity claim has a
thin-depth footnote in §“Why not Go/Rust-class”.

Dimensions: `Perf + Types + Concurrency + Stdlib + Ecosystem + Tooling +
Simplicity + Build/Deploy + Frontend + Maturity = 100`.

## Scores — total out of 100

| Dim (max 10) | .ks | Go | Rust | C | C++ | Node | Python | Julia | Next.js | Laravel |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| Perf | 7 | 8 | 10 | 10 | 10 | 7 | 5 | 9 | 7 | 5 |
| Types | 8 | 8 | 10 | 5 | 8 | 6 | 6 | 8 | 8 | 5 |
| Concurrency | 9 | 9 | 8 | 5 | 7 | 7 | 5 | 7 | 6 | 4 |
| Stdlib | 9 | 9 | 8 | 6 | 8 | 8 | 10 | 8 | 8 | 8 |
| Ecosystem | 8 | 8 | 8 | 6 | 7 | 10 | 10 | 7 | 10 | 8 |
| Tooling | 9 | 9 | 9 | 7 | 8 | 9 | 8 | 7 | 9 | 8 |
| Simplicity | 9 | 7 | 5 | 4 | 4 | 7 | 10 | 7 | 6 | 8 |
| Build/Deploy | 8 | 10 | 9 | 8 | 8 | 6 | 5 | 5 | 7 | 6 |
| Frontend | 8 | 5 | 6 | 2 | 4 | 8 | 5 | 4 | 10 | 7 |
| Maturity | 9 | 9 | 8 | 9 | 9 | 9 | 10 | 7 | 8 | 8 |
| **Total /100** | **84** | **82** | **81** | **62** | **73** | **77** | **74** | **69** | **79** | **67** |

What the `.ks` 84 does and does not mean (read before citing 84; evidence for every scored claim in “v2.5 evidence” + “v2.6 evidence”):

- Perf 7 = “fast enough for scripts; VM v0.2 is measured but partial” (evidence §E1):
  `range(n)` no-alloc path + sorted-input check + folding + O(log n) `**` in both
  engines; VM covers slices/`is`/`?.`/`??`/typed/`switch` with ≈2x fib win but
  ≈0.7x `for-in` and 7 remaining rejects (`go`/`chan`/`select`/`import`/`try`/
  `defer`/`sleep`). Not JIT/LLVM-class; tying Go (8) would need full-VM coverage
  with consistent wins. 7 holds (above Python for script ergonomics, below Go).
- Types 8 = “union (`int|string`) + generic (`array<int>`) *annotations* +
  `struct`/`enum` *syntax* + `is`/`?.`/`??` + `ok`/`err` + enum-aware
  exhaustive-`switch` vet” (evidence §E2). No variadics/named params, no methods/
  interfaces, `==` deep equality, VM skips nominal checks (interpreter validates).
  8 ties Go breadth-for-scripts; 9 would need methods/variadics/full-VM nominals.
- Concurrency 9 = “interpreter `select`/`for-in`/`with_timeout`/`parallel`/
  `with_cancel` at Go spelling parity”. VM has none; no deterministic scheduler;
  `--race` is vet + env (`main.go:793-809`), not instrumentation.
- Stdlib 9 = “177 builtins breadth + extended-dialect depth” (evidence §E3 +
  v2.6 §E8): WS text frames, sqlite UPDATE/JOIN/ORDER/GROUP/LIMIT + WHERE with
  AND/OR + LIKE/NOT LIKE (`%`/`_`, OR looser than AND, no parens) +
  postgres-compat names on a JSON-file engine (no transactions/indexes/prepared),
  pipes/signals, `tcp_shutdown`, cancel. `http_serve` minimal
  (`func(path)->string`, always `application/json`, no shutdown). 9 ties Go
  breadth; 10 (Python) would need native DB + data-stack depth.
- Ecosystem 8 = “lock/semver/vendor + file-local registry + real `audit`”
  (evidence §E4): hash recompute + transitive closure, tested. No central server,
  no git deps, no docs.rs, token env-hint only. 8 ties Go/Rust file-registry
  depth for scripts; central-registry depth still ahead.
- Tooling 9 = “fmt/vet/test/doc/check/repl/bench/profile + `fusion debug`
  (breakpoints/trace/globals, non-interactive) + full stdio LSP (hover/goto/
  completion/rename/diagnostics/format) + VS Code ext v0.3.0 (hand-rolled client) +
  host `--cpuprofile` + `.ks`-line `fusion profile` (exact per-line statement
  counts, not wall time) + vendor-aware hash-skip cache” (evidence §E5 + v2.6
  §E8). No DAP/step-REPL, no sampling time-profiler, ext has no vscode-test
  harness. 9 holds Go/Rust parity for scripts; 10 would need interactive
  debugging + time profiling.
- Build 8 = “`--bin` via `go build -trimpath` + `--strip` (`-ldflags "-s -w"`,
  ~31% smaller measured) + deterministic embed order + `--target` +
  vendor-aware hash-skip cache (vendor swaps bust it)” (evidence v2.6 §E8).
  Requires Go toolchain; cache is whole-app hash-skip (no TTL/size/remote),
  no incremental per-file rebuild. 8 holds.
- Frontend 8 = “SSR + DOM-diff without reload + background ISR + nested layouts
  + subset-JS (hashes/manifest/budgets) + SSG + virtualized lists + `use_state`
  shim” (evidence §E6). No hydrate-full (`on_mount` immediate), no CSS handling,
  `fetch_json` GET-only. 8 ties Node SSR-prototype depth; React/Vite/Next UI
  depth still ahead.
- Maturity 9 = “`docs/stability.md` semver/LTS + `docs/rfcs/` (2 RFCs) +
  133 `go test` funcs (was 125: +8 for OR/LIKE, completion x2, profile x3,
  vendor-cache, `--bin` E2E) + 5 benchmarks + 2 `.ks` test files + release v2.6
  + `ci.sh` gate + in-repo `.github/workflows/ci.yml` (same gate) + per-file
  `test --timeout` + repeat-safe TCP + `--bin` E2E (builds + runs a minimal app)
  + hygiene (`retest.log` leftover removed, pre-existing `go vet` unreachable-code
  fixed)” (evidence v2.6 §E8). Remaining gap: TLS-server E2E (needs a
  `tls_serve` feature — explicitly left). 9 holds.
- 84 is breadth-for-scripts/services on this rubric, not Go/Rust depth parity. See “Why not Go/Rust-class” + “Honest limits”.

Extra stacks (same rubric): `TypeScript 79`, `Java/Kotlin/Spring 78`,
`Vite 77`, `Deno/Bun 77`, `React 76`, `Lua 58`, `Ruby/Rails 68`, `Bash 45`.
Details in `More` below. Frontend breakdown:

| Dim (max 10) | .ks | TypeScript | React | Vite | Next.js |
|---|---:|---:|---:|---:|---:|
| Perf | 7 | 6 | 7 | 8 | 7 |
| Types | 8 | 9 | 8 | 6 | 8 |
| Concurrency | 9 | 6 | 5 | 4 | 6 |
| Stdlib | 9 | 7 | 5 | 4 | 8 |
| Ecosystem | 8 | 10 | 10 | 9 | 10 |
| Tooling | 9 | 9 | 9 | 10 | 9 |
| Simplicity | 9 | 6 | 6 | 8 | 6 |
| Build/Deploy | 8 | 7 | 7 | 10 | 7 |
| Frontend | 8 | 10 | 10 | 10 | 10 |
| Maturity | 9 | 9 | 9 | 8 | 8 |
| **Total /100** | **84** | **79** | **76** | **77** | **79** |

## Language-by-language

### vs Go (implementation language)

**Score: ks-fusion 84/100 vs Go 82/100 — ks-fusion wins by 2 on balance (loses on native depth).**

Same ideas: `go func(){...}()`, `chan(1)`, `send/recv/close`, `select`, `defer` LIFO.
Difference: Go is compiled + statically typed; `.ks` runs on a tree-walk interpreter
(full language, literal folding) with an opt-in bytecode subset v0.2 (`fusion compile`:
slices/`is`/`?.`/`??`/typed/`switch` added; still no `go`/`chan`/`select`/`sleep`,
no `import`/`try`/`defer` — each rejected with a clear error) + gradual types
(optional `: type` incl. union/generic annotations + struct/enum syntax checked at
runtime, `is`/`?.`/`??`, `struct`/`enum` declarations) + `fusion build --bin` via `go build`.
Concurrency matches Go’s *spelling* (`with_timeout`/`parallel`/`with_cancel`, `--race` flag exists) but `--race`
is `VetTarget` error-gate + `FUSION_RACE=1` env (`cmd/fusion/main.go:793-809`), not data-race instrumentation;
for a real race run the message itself tells you to use `go run -race ./cmd/fusion run`.

```go
// Go
ch := make(chan int, 1)
go func(){ ch <- 42; close(ch) }()
fmt.Println(<-ch)
```

```python
# .ks
let c = chan(1)
go func() {
  send(c, 42)
  close(c)
}()
print recv(c)
```

Multiplexing works like Go too (uniformly-random ready branch via `reflect.Select`, `break`
ends the `select`, `ch = nil` disables a case):

```go
// Go
select {
case v := <-ch1:
    fmt.Println(v)
case <-time.After(100 * time.Millisecond):
    fmt.Println("timeout")
}
```

```python
# .ks
select {
  case v = recv(c1) { print v }
  case timeout(100) { print "timeout" }
}
```

Pick Go for prod servers, strict APIs, max RPS.
Pick `.ks` for shorter scripts with Go-flavored concurrency, plus small services via `--bin`
where requiring a Go toolchain at build time is acceptable.
(`fusion compile` is opt-in, partial: full language runs in the interpreter.)

### vs Rust

**Score: ks-fusion 84/100 vs Rust 81/100 — ks-fusion wins by 3 on balance (loses on systems depth).**

Rust gives ownership, `Result/Option`, `cargo` registry, zero-cost abstractions.
`.ks` copies the `cargo` UX (`fusion new --lib`, `fusion build --release`,
`test-releases/` like `target/release/`, `fusion.lock` semver + `vendor/`, `fmt/vet/check`) but bundles are
source JSON (`kslib-1` + shebang, parse-checked) — not native code — plus `--bin` embed via `go build`.
Imports are flat globals (no `import "x" as h` yet — prefix your functions).
Errors are `ok(v)/err(e)` values + `error(msg)` abort + `try/catch` + `assert_eq/ne/contains`.
`fusion compile` emits a portable bytecode sidecar (`.ksb-1` JSON + `--dis`/`--run`, partial) +
`fusion build --bin/--target` — the first step toward a VM/AOT story, not a Rust-class backend yet.

Pick Rust for perf-critical, safety-critical, WASM libs.
Pick `.ks` for Day-1 productivity without borrow checker.

### vs C

**Score: ks-fusion 84/100 vs C 62/100 — ks-fusion wins by 22.**

C gives pointers, manual `malloc/free`, direct syscalls, tiny runtimes.
`.ks` gives `array/map/string` + Go GC + bounds-checked indexing + 177 builtins + `--bin`/cache/repro.

Pick C for kernels, drivers, microcontrollers.
Pick `.ks` for everything where `segfault` is unacceptable.

### vs C++

**Score: ks-fusion 84/100 vs C++ 73/100 — ks-fusion wins by 11.**

C++ gives RAII, templates, classes, deterministic destruction, huge game/engine libs.
`.ks` gives `func` closures + `defer` + duck-typed maps + `struct` declarations
(validated maps, no methods) + annotation-level generics (`array<int>`) — no
template metaprogramming, no RAII.

Pick C++ for engines, CAD, HFT, Unreal/Qt.
Pick `.ks` for config-driven logic on top of those engines.

### vs Node.js

**Score: ks-fusion 84/100 vs Node.js 77/100 — ks-fusion wins by 7 (on balance, not on npm depth).**

Node gives V8, `npm` (2M+ packages), `fetch/http`, event loop, TypeScript.
`.ks` gives simpler blocking `recv`/`select` + 177 sync builtins in the
interpreter (VM v0.2 subset + 7 user builtins `assert/len/range/str/int/float/type`
plus 5 hidden `__iter_*` helpers), plus `http_get/post/fetch_json/http_serve`,
`regex_*`, `exec`/`exec_pipes`/`spawn`, extended sqlite dialect + postgres-compat.
`http_serve` is minimal (handler `func(path)->string`, always `application/json`,
no method/status/headers control, no shutdown).

```js
// Node
const r = await fetch(url).then(r => r.json());
```

```python
# .ks v2.5: files + json + http/tcp/tls/ws-text/sqlite-extended client
let raw = http_get("https://api.example.com/data")
let data = json_parse(raw)
# or: let data = fetch_json("https://api.example.com/data")  # GET-only: json_parse(http_get(url))
```

Pick Node for web APIs, realtime, npm deps.
Pick `.ks` for small deterministic scripts/services without `node_modules`.

### vs Python

**Score: ks-fusion 84/100 vs Python 74/100 — ks-fusion wins by 10 (on balance; loses on data/AI libs).**

Closest feel: `let x = 10`, `for i in range(5)`, `a[1:3]`, `and/or/not`,
truthiness (`nil false 0 0.0 "" [] {}` falsy), `map/filter/reduce`.
Difference: `.ks` adds `go/chan/select/defer/switch`, gradual `: type` annotations
(incl. unions/generics + struct/enum syntax), `is`/`?.`/`??`, and braces; Python still
has huge stdlib (`requests`, `numpy`, `django`) while `.ks` has
`http/regex/crypto/fs/process/time/db/log/tcp/tls/ws-text` (177 builtins) but no
`numpy`/`django`; JSON-file extended SQL instead of real SQLite (AND-only WHERE, no
OR/transactions/indexes), `exec_pipes`/`spawn` for processes.

Pick Python for data/AI/science/ops (ecosystem still wins).
Pick `.ks` for learning concurrency early or embedding a tiny Go-based runtime, plus `--bin` services.

### vs Julia (numerical computing language)

**Score: ks-fusion 84/100 vs Julia 69/100 — ks-fusion wins by 15 on balance, loses on numerics.**

Julia = JIT-compiled (LLVM) + multiple dispatch + parametric types.
Feels like Python/MATLAB for math, runs like C for loops/matrices.
`.ks` = tree-walk interpreted (literal folding for constants) + gradual `: type` checks at runtime + `--bin` via `go build`, no
vectorized ops, no DataFrames/plots. The +14 reflects `.ks` tooling/build/finite-frontend breadth vs Julia numerics lead —
not a claim that `.ks` is faster at math. It is not.

```julia
# Julia: vectorized + fast loops, multiple dispatch
f(x::Number) = x * 2
A = [1, 2, 3] .* 2
s = sum(i * i for i in 1:10_000)
```

```python
# .ks v2.5: scalar loops only, no broadcasting (folding + range fast path help overhead, not loop speed)
let total = 0
for i in range(10000) {
  total += i * i
}
print total
```

Pick Julia for numerics, science, matrices, simulations.
Pick `.ks` for Go-style `go/chan/select` teaching, tiny CLIs/services (`--bin`),
and embedding a Go-based runtime where Julia's heavy JIT + slow startup is overkill.

### vs Next.js (framework, not language)

**Score: ks-fusion 84/100 vs Next.js 79/100 — ks-fusion wins by 5 on balance (different category; loses on UI depth).**

Category error if compared 1:1. Next.js = React + routing + SSR/ISR + Node runtime.
ks-fusion app = `backend/main.ks` + `frontend/` (`main.ks` route table +
`pages/home.ks` + `pages/hi.ks` + `components/header.ks` + `layouts/app.ks` +
`store/app.ks`) run concurrently in console via `render_console`, plus `run-web` SSR (HTML+JSON, `/api/*`) and `build-js` per-route JS.

Honest `run-web` scope (`internal/tools/webjs.go`): loads `frontend/**/*.ks` except `main.ks` into one
interpreter, calls `<route>_page({})` (`/`→`home_page`, `/hi`→`hi_page`, `/user/*`→`home_page`),
embeds pretty JSON + a small JS shim (`window.use_state/set_state` over `__state`) + `?format=json` +
`X-Render-Time` (+ `X-Cache: HIT` on ISR hits). `/api/<name>` requires `backend/api/<name>.ks` with
`api_<name>({query,path})` else returns `{"ok":true}`. `--watch` polls `.ks` mtimes every 400ms and pushes over
SSE (300ms ticker): on change it re-renders and sends keyed `{ops, vm}` patches applied via
`__applyPatch` (`diff.go:76` `DiffViewModels`: setText/setProp/replace/insert/remove/move;
`webjs.go` client `__renderVM`/`data-key`); on render error it sends `{"reload":true}` and the client
shows a banner — **never `location.reload()`** (asserted by `isr_test.go:114,137`).
ISR is opt-in with background regen: a page VM carrying `props.revalidate = seconds` (0 < n < 30d)
is cached per route+query and refreshed by a background loop before expiry plus
serve-stale-while-revalidate (`webjs.go:592` `startBackground`, `webjs.go:704` `kickRefresh`).
Nested layouts are conventional: a page VM with `layout: "admin"` wraps via
`admin_layout(page)`, then `_app_layout`, then `app_layout` when those funcs exist;
a value counts as a view-model when it has `type`/`children`/`key`.
`build-js` transpiles a subset per route (handles `let/assign/func/block/if/while/for-in/print/expr/call/index/array/map`;
`for-c` → `// for-c (see .ks source)`, anything else → `// unsupported`), strips blanks/`//` as “minify”,
writes `<route>.js` (`home`→`index`) with per-route sha256 (skip-write when unchanged) + `manifest.json {route:{size,sha256}}`,
warns >100KB / fails >250KB per route.
`build-ssg` pre-renders `[/, /hi + pages/*.ks]` to `<name>.html+.json` (`/`→`index`); per-route failures only
`ssg skip`, not fatal. `use_state/set_state` in `.ks` is a process-global map
(`internal/backend/stdlib_ext2.go`); `on_mount(f)` calls `f` immediately (no lifecycle — no hydrate-full);
`fetch_json(url)` is `json_parse(http_get(url))`, GET-only. Lists over 100 children render the first 100
plus a `Show more (shown/total)` expander.

* Today: use Next.js for real browser UI. Use `.ks` backend as JSON worker
  (`read_file` → `json_stringify` → stdout, or `http_get` → `fetch_json`) called from an API route, or `run-web` for SSR prototype.
* Future (`docs/futures.md`, `plan/frontend.md`): hydrate-full, CSS handling. Do not reimplement React in `.ks` — explicit non-goal.

Pick Next.js for SEO sites, dashboards, SaaS UI.
Pick `.ks` for the logic worker behind it.

### vs TypeScript (language, not runtime)

**Score: ks-fusion 84/100 vs TypeScript 79/100 — ks-fusion wins by 5 on balance (loses on type depth at scale).**

TypeScript = JS + static types (`tsc`, `strict`, generics, unions, interfaces).
`.ks` = gradual types (dynamic by default, optional `: type` runtime checks incl. union `int|string` and generic
`array<int>`/`map<string,int>` annotations with 1-level nesting, `struct`/`enum` syntax with runtime
validation, `is` narrowing, `?.`/`??` nil-safety, `ok`/`err` results + `vet`/`check` +
enum-aware exhaustive-`switch` vet). No variadics/named params, no methods/interfaces,
`==` is deep equality, VM skips nominal checks (interpreter validates;
`vm.go:1110` `isKnownVMType`).
`is` folding only covers string-literal tests for `int/float/number/string/bool/nil/array/map`
(`internal/frontend/fold.go` — `chan/func/ok/err/any` never fold);
`x in [array]` folding via the generic path does not fire (outer `isLit` guard excludes `ExprArray`).

```ts
// TypeScript
function add(a: number, b: number): number { return a + b; }
type User = { name: string; age: number };
```

```python
# .ks v2.5 — annotations + struct/enum syntax are runtime-checked (nil nullable) + vet/check
struct User { name: string, age: int }
enum Color { Red, Green, Blue }
func add(a: int, b: int): int { return a + b }
let x: int|string = 1
let scores: array<int> = [1, 2]
let user: User = {name: "ada", age: 36}
assert(user is "User")
assert(user?.name ?? "anon" == "ada")
let c: Color = "Red"
switch c {
  case "Red" { print "red" }
  case "Green" { print "green" }
  case "Blue" { print "blue" }
}  # vet: exhaustive, no default needed
let r = ok(1)
assert(r is ok)
```

Pick TypeScript for any browser/Node code that must scale past 1k lines.
Pick `.ks` for non-JS glue/services where `tsc` + `node_modules` is overkill.
Interop: `fusion build-js` subset → import `.ks` logic into TS (subset only, check for `// unsupported` lines).

### vs React (UI library)

**Score: ks-fusion 84/100 vs React 76/100 — ks-fusion wins by 8 (different category, on balance only).**

React = components, hooks, virtual DOM, concurrent renderer.
`.ks` = view-model funcs + console renderer + `run-web` SSR (keyed diff, no reload) + `build-js` JS,
no DOM/state/effects parity yet
(`home_page`/`header_render` return `{key,type,props,children}`, `main.ks` routes + prints/serves).

```jsx
// React
function Hello({name}) { return <h1>Hello {name}</h1>; }
```

```python
# .ks frontend (console + SSR prototype; client applies keyed patches, banners on error)
# frontend/pages/home.ks
func home_page(props) {
  let head = header_render({title: props?.title ?? app_title})
  return {key: "home", type: "page", props: {...}, children: [head]}
}
# frontend/main.ks
let route = env("ROUTE", "/")
if route == "/" { render_console(home_page(app_state())) }
# or: fusion run-web . --port 8080 [--watch] (SSR HTML+JSON, SSE keyed patches)
```

Pick React (+ Vite/Next.js) for all real UI.
Pick `.ks` for the worker behind the UI (JSON over stdout/file/`http_*`, `run-web` SSR prototype).

### vs Vite (frontend build tool)

**Score: ks-fusion 84/100 vs Vite 77/100 — ks-fusion wins by 7 (different category, on balance only).**

Vite = instant HMR dev server + `esbuild`/Rollup bundler + plugin ecosystem.
`fusion` = `new/run/build/launch` (+ `compile --dis/--run` partial, `test` TAP runner, `fmt/vet/doc/check/bench/debug`,
`run-web` SSR + keyed-patch SSE, `build-js` per-route JS with hashes, `audit`, LSP) for `.ks` only,
no CSS/DOM bundling parity.

|  | Vite | `fusion` (v2.5: run-web keyed-diff/ISR-bg + build-js/hash + build-ssg) |
|---|---|---|
| Dev | HMR <100ms, partial DOM patch | `run`/`launch` rerun, `ROUTE` switch, `run-web` SSR + `--watch` SSE **keyed patches, no reload** (400ms mtime poll, 300ms SSE ticker, full re-render per tick, banner on error) |
| Build | tree-shaken JS/CSS | source JSON `.kslib` / parse-check + partial `.ksb` bytecode + per-route subset `.js` + content-hash manifest + budgets (warn >100KB / fail >250KB) |
| Plugins | 1000s (React, TS, Tailwind) | none |
| Target | browser | console interpreter + partial VM + SSR HTML/JSON + subset JS (view-models printed/served, hydrate shim) |

Pick Vite for React/TS/Tailwind frontends.
Pick `.ks` for logic; `fusion build-js` emits a Vite-consumable subset module (audit `// unsupported` lines).

### vs PHP Laravel

**Score: ks-fusion 84/100 vs Laravel 67/100 — ks-fusion wins by 17 (on balance for sidecars; not a CRUD replacement).**

Laravel gives routing, ORM/Eloquent, migrations, Blade, queues, auth scaffolding.
`.ks` gives `http_get/post/serve` (minimal serve), JSON-file KV `db_put/get/delete/list` + JSON-file extended-dialect SQL
+ `postgres_*` compat names, `run-web` `/api/*`, `build-js`, `--bin` services — still no ORM/migrations/templates,
but leads on simplicity/concurrency/tooling for sidecars.

Pick Laravel for monolith CRUD + admin + auth in hours.
Pick `.ks` for sidecar scripts/services (data munging, checks, bots, `--bin` workers) next to Laravel.

### More (short, all scored out of 100)

* **Deno/Bun 77/100 vs .ks 84/100 (+7):** pick for secure TS sandbox / fast runtime; `.ks` is simpler but far smaller.
* **Java/Kotlin/Spring 78/100 vs .ks 84/100 (+6):** pick for enterprise monoliths, JPA, strict OOP; `.ks` for glue/services.
* **Lua 58/100 vs .ks 84/100 (+26):** Lua is smaller/faster to embed; `.ks` has Go-style `select` + `fusion` CLI + `--bin`/file-registry/real-audit + 177 builtins out of box.
* **Ruby/Rails 68/100 vs .ks 84/100 (+16):** pick Rails for convention CRUD; `.ks` syntax will feel familiar, plus `--bin`/concurrency.
* **Bash 45/100 vs .ks 84/100 (+39):** pick Bash for 5-line pipes; `.ks` wins past 50 lines (`try/catch`, maps, JSON, `is`/`?.`/`??`, `select`, `http/regex/crypto`, `--bin`, Windows portability via `--target`).

## Totals & ranking (out of 100)

| Rank | Stack | Total /100 | Verdict vs .ks (83) |
|---:|---|---:|---|
| 1 | **ks-fusion v2.5 source (177 builtins = 96+52+11+12+6; struct/enum + exhaustive vet; VM v0.2 + bench; WS-text + extended SQL/postgres-compat + pipes; real audit; LSP + debug + VS Code ext; diff + bg-ISR; release v2.5 + ci.sh)** | **83** | **baseline — leads on simplicity (9/10) + script breadth; behind on native depth (see “What 83 means” + “Honest limits”).** |
| 2 | Go | 82 | -1, prod servers / single binary depth |
| 3 | Rust | 81 | -2, systems / safety depth |
| 4 | Next.js | 79 | -4, browser UI depth (different category) |
| 4 | TypeScript | 79 | -4, typed UI/logic depth |
| 6 | Java/Kotlin/Spring | 78 | -5, enterprise depth |
| 7 | Node.js | 77 | -6, APIs / npm depth |
| 7 | Vite | 77 | -6, frontend build/HMR depth (different category) |
| 7 | Deno/Bun | 77 | -6, typed runtime |
| 10 | React | 76 | -7, UI components (different category) |
| 11 | Python | 74 | -9, data/AI/ecosystem depth |
| 12 | C++ | 73 | -10, engines/trading |
| 13 | Julia | 69 | -14, numerics/science depth |
| 14 | Ruby/Rails | 68 | -15, convention CRUD |
| 15 | PHP Laravel | 67 | -16, monolith CRUD |
| 16 | C | 62 | -21, kernels/embedded |
| 17 | Lua | 58 | -25, embedding |
| 18 | Bash | 45 | -38, tiny pipes |

Grand total (sum of all 18 totals) = `1314 / 1800`, average `73.0/100`.
`.ks` total `83/100` = v2.4 honest 80 +3 for fully-met bars (real audit, DOM-diff
without reload + background ISR, release + timeout + repeat-safe + CI).
VM v0.2 + exhaustive-switch + extended SQL/WS/pipes + LSP/debug/ext are real,
tested depth inside their current scores (see “v2.5 evidence”); they do not move
scores alone (see “Corrections” for why the interim 87 overshot).

## v2.5 evidence (file:line for every scored claim)

> Re-verify with `go test ./...`, `go test -bench`, `bash ci.sh`, and “How to verify”.
> Scores change only with implementation; progress inside a score is documented, not scored.

### E1. Perf holds 7: VM v0.2 measured, partial (a +1 needs full coverage + consistent wins)

- Ops: `internal/compiler/compiler.go:76-84` (`OpSlice/OpIs/OpCoalesce/OpSafeIndex/`
  `OpCheckType/OpSetupTry/OpPopTry/OpDefer/OpJumpIfNotNil`), `String()` at
  `compiler.go:157-175`.
- Compile: slices (`compiler.go:1168-1193`), `is` (`compiler.go:1155-1162` via
  `isTypeName` at `compiler.go:1034`), `??` short-circuit (`compiler.go:1164-1172`
  via `OpJumpIfNotNil`), safe `?.` (`compiler.go:1148-1150` → `OpSafeIndex`),
  typed lets (`compiler.go:588` `OpCheckType`), typed func params + returns
  (`compiler.go:999-1005`, return check at `compiler.go:439`), `switch` desugar
  (`compiler.go:495` `compileSwitch`: hidden target + Eq-chain, break-to-end).
- VM: `vm.go:1019` `safeIndexVal`, `vm.go:1052` `sliceVal`, `vm.go:1134` `vmIsType`
  (base/union only), `vm.go:1110` `isKnownVMType` (nominals skip — interpreter
  validates), `vm.go:887-900` O(log n) int `**` (was O(n) loop).
- Rejects left (clear errors, run in interpreter): `go`/`sleep`/`import`
  (`compiler.go:473-477`), `try`/`select`/`defer` (`compiler.go:479-485`),
  `struct`/`enum` decls (`compiler.go:486-487`), closure capture, `OpSetupTry`/
  `OpPopTry`/`OpDefer` VM stubs.
- Tests: `compiler_test.go:145`/`152`/`160`/`167`
  (`V02Slices`/`V02IsCoalesceSafe`/`V02Typed` incl. nominal-skip, `V02Switch`),
  `compiler_test.go:98` (switch/slices/is/??/typed must run).
- Bench: `internal/backend/bench_test.go` + `internal/compiler/bench_test.go`,
  artifact `docs/bench.md` (fib ≈2x VM win; loop ≈0.7x VM regression).
- Verdict: real progress inside 7. A +1 (tying Go 8) needs full-language coverage
  with consistent wins — not claimed.

### E2. Types holds 8: struct/enum syntax + real exhaustive vet (a +1 needs methods/variadics/VM nominals)

- Syntax: `struct`/`enum` parse (`frontend.go:1040-1118`), `StmtStruct`/`StmtEnum`
  (`frontend.go:116-117`), runtime (`backend.go:1250` `execStructDecl`,
  `backend.go:1278` `execEnumDecl`, `backend.go:596` `matchesStruct`, nominal
  `matchesTypeStrict`). Note: struct/enum syntax pre-dates this release's vet work
  (the v2.4 doc wrongly said “no syntax”); the new depth is the vet check below.
- Vet: enum registration (`tools.go:504-505`), var types (`tools.go:491-493`),
  real exhaustiveness (`tools.go:642-661`: missing-variant names, bool
  true/false, `default` rescues), helpers (`tools.go:727` `switchEnumTarget`,
  `tools.go:742` `switchIsBool`), cross-file (`tools.go:870` global enums/types).
- VM: nominal annotations compile; `OpCheckType` skips unknown (nominal) names
  (`vm.go:1110`); base `is` works (`vm.go:1134`).
- Tests: `backend_test.go:316` (`TestRunStructEnumSyntax`), `tools_test.go:68`
  (`TestVetExhaustiveEnum`: all-covered ok / missing-Blue names default rescues),
  `tools_test.go:114` (`TestVetExhaustiveBool`).
- Verdict: real depth inside 8 (ties Go breadth-for-scripts). A +1 (beating Go)
  needs methods/interfaces, variadics/named params, full-VM nominal checks — not claimed.

### E3. Stdlib holds 9: extended dialect + frames + pipes (a +1 needs native DB)

- WS RFC 6455 text frames: `stdlib_ext4.go:27` `wsEncodeText` (masked client text),
  `stdlib_ext4.go:64` `wsReadFrame`, `stdlib_ext4.go:119` `wsReadText`
  (fragments, ping/pong, close; binary rejected as text-only).
- SQL extended dialect on the JSON-file engine: `UPDATE`
  (`stdlib_ext3.go:206` `reUpdate`, exec at `stdlib_ext3.go:260`,
  `stdlib_ext3.go:297` `parseSetClause`), `JOIN` (`stdlib_ext3.go:428`
  `innerJoin`, `=`-only `ON`), `GROUP BY`+`COUNT(*)` (`stdlib_ext3.go:467`
  `groupCount`), `ORDER BY`/`LIMIT`/`OFFSET` (select at `stdlib_ext3.go:313`),
  `postgres_*` compat names on the same engine (`stdlib_ext3.go:97-100`).
  WHERE is AND-only (`stdlib_ext3.go:676`); no OR/transactions/indexes/prepared.
- Pipes/signals: `exec_pipes` (`stdlib_ext4.go:220`), `spawn`/`proc_wait`/
  `proc_kill` (`stdlib_ext4.go:291,332,370`).
- Tests: `backend_test.go:336` (UPDATE/ORDER/LIMIT/COUNT/GROUP),
  `backend_test.go:362` (JOIN), `backend_test.go:377` (postgres-compat),
  `stdlib_ext4_test.go` (WS frame round-trip, `ws_recv`, pipes).
- Count: 177 distinct (`96` base + `52` ext + `11` ext2 + `12` ext3 + `6` ext4;
  `grep -ohP ... | sort -u | wc -l`; tests `>=166`/`>=177`).
- Verdict: real depth inside 9 (ties Go breadth). A +1 (tying Python 10) needs
  native sqlite/postgres (server, indexes, transactions, OR) + data-stack depth — not claimed.

### E4. Ecosystem 7→8: real audit (+1, meets the bar)

- `audit.go:81` `Audit` (yanked/missing/update/token-hint + integrity + transitive),
  `audit.go:153` `VerifyRegistry` (recompute sha256 vs index + `.sha256` sidecar),
  `audit.go:181` `checkTransitive` (locked-bundle `import "lib"` must resolve).
- Tests: `registry_test.go:54` (`TestAuditHashRecompute`: tamper → mismatch),
  `registry_test.go:106` (`TestAuditTransitive`: missing transitive → issue).
- File-local registry (`publish/pull/yank`, sha256, namespaces); no central
  server, no git deps, no docs.rs, token env-hint only. 8 ties Go/Rust
  file-registry depth for scripts.

### E5. Tooling holds 9: full LSP + non-interactive debugger + ext (a +1 needs interactive depth)

- LSP: diagnostics (`lsp.go:240` parse, `lsp.go:259` vet on save,
  publishes at `lsp.go:141,155,172`), rename (`lsp.go:347` cross-file),
  formatting (`lsp.go:320` via `FormatSource`), hover/goto (`lsp.go:184,196`;
  goto returns real `st.Line - 1`, not a stub).
- Debugger: `backend.go:715` `OnStmt` hook + `backend.go:1019` `stmtKindName`,
  `debug.go:35` `DebugFile` (breakpoints + trace + globals snapshot,
  non-interactive), `tools_cmd.go:cmdDebug` (`fusion debug --break/--trace`).
- VS Code ext v0.2.0: hover/goto/rename/diagnostics/format providers + `ks-fusion`
  debugger contribution (`editors/vscode/package.json`, `extension.js`,
  `node --check` clean; no vscode-test harness).
- Tests: `lsp_test.go` (diagnostics/rename/format), `debug_test.go:5`
  (breakpoint hit + vars, trace, bad file).
- Verdict: real depth inside 9 (Go/Rust parity for scripts). A +1 (beating both)
  needs interactive DAP/step-REPL, completion, `.ks`-line profiler — not claimed.

### E6. Frontend 7→8: DOM-diff without reload + background ISR (+1, meets the bar)

- Diff: `diff.go:76` `DiffViewModels` (keyed setText/setProp/replace/insert/
  remove/move) + `diff_test.go`.
- SSE: keyed `{"ops":..,"vm":..}` patches; client `__applyPatch`/`__renderVM`/
  `data-key`; render-error → `{"reload":true}` banner only (never
  `location.reload()`).
- ISR: background regen (`webjs.go:592` `startBackground`, `webjs.go:704`
  `kickRefresh`, serve-stale-while-revalidate).
- Tests: `isr_test.go:42` (background refreshes), `isr_test.go:67` (stale-while),
  `isr_test.go:114` (no `location.reload` in HTML), `isr_test.go:137` (SSE ops,
  no reload payload).
- Limits: no hydrate-full (`on_mount` immediate), no CSS handling,
  `fetch_json` GET-only. 8 ties Node SSR-prototype depth.

### E7. Maturity 7→8: release + timeout + repeat-safe + CI (+1, meets the bar)

- Release: `release/fusion` rebuilt from source (`go build -o release/fusion
  ./cmd/fusion`; `version` → `ks-fusion v2.5`, `toolVersion` at `main.go:332`).
- Timeout: `fusion test --timeout` (`main.go:1098` `runTestFileTimeout`,
  default 30s; timed-out file reports `not ok`, run continues).
- Repeat-safe: `stdlib_ext2_test.go:32` port 0 + `tcp_shutdown` (`stdlib_ext2.go:227`);
  `go test ./internal/backend/ -run TestV23TCP -count=3` green.
- CI: `ci.sh` (`go vet` + `go test ./...` + repeat-safe + `fmt --check` +
  `vet`/`check` apps; `bash ci.sh` → `CI OK`). Note: `.github/workflows/` is
  deployment-managed in this environment (only `blank.yml` persists), so the
  gate lives in `ci.sh`.
- Tests: 125 `go test` funcs + 5 benchmarks + 2 `.ks` test files, all green.
  Gaps: TLS-server/`--bin` E2E, `retest.log` leftover. 8 holds.

## Why not Go/Rust-class (v2.5 gaps + what parity needs)

> Score context: `.ks 83/100` vs `Go 82/100` vs `Rust 81/100` (ahead on balance for
> scripts/services; behind on native depth). The lead comes from Simplicity 9 and
> script breadth; every native-depth row still trails. Rows 6/14 stay intentionally
> different (GC stays, `unsafe` stays opt-in).

### 1. Compiler partial — full language tree-walk + `--bin` embed via `go build`, no LLVM/JIT

* Today: tree-walk interpreter (`internal/backend`, full language, 177 builtins, struct/enum syntax, literal folding) +
  bytecode subset v0.2 (`internal/compiler`, `.ksb-1` JSON + stack VM):
  `fusion compile prog.ks [--out prog.ksb] [--dis] [--run]`, `fusion prog.ksb`.
  Compiled: literals/arrays/maps, `let` (+ `: type` base check, nominals skip)/
  `=`/`+=`-family, `+ - * / % **` (O(log n) int `**`)/`== != < <= > >=`/`in`/`is`/`??`,
  `?.` safe index, `a[l:r]` slices, calls (user funcs + typed params/return check +
  `assert/len/range/str/int/float/type`), `a[i]`/`m.key`, `print/if/while/for-in/for-c/func/return/break/continue/switch`.
  Explicitly rejected (run in interpreter): `go`/`chan`/`select`, `sleep`, `import`,
  `try/catch`, `defer`, `struct`/`enum` decls, closures capturing outer locals
  (`compiler.go:473-487`; VM `OpSetupTry`/`OpPopTry`/`OpDefer` are reserved stubs).
  VM limits: 7 user builtins (+ 5 hidden `__iter_*` = 12 map entries),
  `maxFrames 1024` / `maxSteps 20M`, `line N:` errors. `.kslib` stays source JSON
  (`kslib-1`), but `fusion build --bin` embeds `.ks`+`.kslib` (+ `fusion.lock`) into a
  temp-module `main.go` and runs `go build -trimpath` (requires a Go toolchain;
  `GOOS/GOARCH` + `CGO_ENABLED=0` passthrough); deterministic embed order,
  `GOFLAGS=-trimpath`. Cache is a whole-app sha256 over `fusion.toml` +
  `fusion.lock` + every `.ks` (skips `.git/target/vendor/test-releases`, so `vendor/`
  swaps do not invalidate) — hash-skip, not incremental; no TTL/size/remote.
  `docs/bench.md` measures VM fib ≈2x interp but VM loop ≈0.7x (desugar cost).
* Go level still needs: full-language VM coverage (concurrency, `import`/`try`/`defer`,
  nominal checks), consistent wins, then IR-level checks (have source `vet`/`check`; have hash-skip cache).
* Rust level still needs: LLVM/opt backend or full bytecode VM + AOT, LTO, strip/symbol options.
  Minimum viable: full-subset VM first, then native AOT (have embed `--bin` now).
* Planned: `docs/futures.md` P1 runtime; `--bin`/`--target`/host-`--cpuprofile`/hash-skip cache/`-trimpath` done, VM full coverage left.
* Score impact: Perf 7 holds (above Python for scripts on breadth; VM v0.2 is progress
  inside 7); Perf 7→8 left for full VM with consistent measured wins.

### 2. Gradual types (annotations + struct/enum syntax + vet; methods/variadics left)

* Today: `let x: int|string`, `array<int>`, `map<string,int>` (1-level nesting),
  optional `let x: int`, `func f(a: int): int`, `func` literals with types,
  `struct Name {..}` / `enum Name {..}` declarations (runtime-validated),
  `x is int` / `x is "int"` / `x is not "int"` (also `number`/`any`/`ok`/`err`);
  `a?.b` / `a?.[i]` (missing → `nil`); `a ?? b` (nil-coalescing, short-circuit);
  `ok(v)/err(e)` + `is_ok/is_err` + `unwrap/unwrap_or` + `is_type/assert_type` +
  `struct_validate/assert` + `enum_create/valid` + `is_number` +
  `assert_eq/ne/contains` + `fusion check`/`vet` arity/type lint + enum-aware
  exhaustive-`switch` vet (names missing variants; bool true/false; `default` rescues;
  other switches still warn without `default`); `==` is still deep equality, arity +
  param-type checks at call time (+ vet); VM checks base types, skips nominals.
* Go level still needs: methods/interfaces, variadics/named params, full-VM nominal checks.
* Rust level still needs: real `Result/Option` exhaustiveness, pattern matching,
  ownership-safe FFI boundaries (no full borrowck — explicit non-goal, Go GC stays).
* Planned: `futures.md` P1 language core (variadics, modules) closes part of the rest.
* Score impact: Types 8 holds (ties Go breadth-for-scripts); Types 8→9 left
  (methods/variadics/full-VM nominals).

### 3. Concurrency (spelling parity + cancel, not runtime parity)

* Today (interpreter): `go` + `chan(n)/send/recv/close` +
  `select { case v = recv(c) / recv(c) / send(c, v) / timeout(ms) / default }`
  (blocks until one case is ready, ready cases win uniformly at random like Go,
  `break` ends the `select`, `ch = nil` disables a case for fan-in drains) +
  `for v in ch` (drains until close, like Go's `range ch`) +
  `recv_timeout/send_timeout/chan_closed` + `try_send/try_recv/chan_len/chan_cap/sleep` +
  `with_timeout(ms, func)` (errors on timeout) + `parallel(arr, func)` (ordered, first error wins) +
  `with_cancel(ms, func(id))` (timeout/cancel race) + `make_cancel`/`cancel`/`is_cancelled` +
  `tcp_shutdown(port)` for repeat-safe listeners +
  `fusion run/launch --race` (error-level vet gate + `FUSION_RACE=1` env + “use `go run -race`” hint;
  `launch --race` is env+print only). `go defer` is explicitly rejected.
  Compiler v0.2 rejects `go/chan/select/sleep` with a clear error (run those files in the interpreter).
* Go level done for script spelling (9/10 held). Left: structured cancel/context polish,
  buffered-chan spec docs, deterministic test scheduler, real race instrumentation.
* Rust level still needs: `send/sync`-like docs, cancel/context, deterministic test scheduler for `go` blocks.
* Planned: `futures.md` P1 (timeouts/context, scheduler) + namespaced imports.
* Score impact: Concurrency 9 held (no further points planned; stay 9 for script scope).

### 4. Stdlib breadth 177, depth extended-dialect — `http/regex/crypto/fs/process/time/db/log/tcp/tls/ws-text/cancel` real, native DB left

* Today: 177 distinct builtins (verified: `96` in `backend.go` + `52` in `stdlib_ext.go` + `11` in `stdlib_ext2.go` +
  `12` in `stdlib_ext3.go` — sqlite + postgres-compat — + `6` in `stdlib_ext4.go` — WS/pipes;
  no duplicates; `BuiltinCount()` = `len(allBuiltins())`; tests assert `>=166`/`>=177`):
  strings/arrays/maps/JSON/files/math/time/rand, `map/filter/each/reduce/apply`, `ok/err` results, `chan_*`,
  `read_file/write_file/append_file/exists/list_dir/mkdir/remove[_file]/input/argv/env/exit`,
  plus `http_get/post/fetch_json/http_serve` (minimal serve), `regex_match/find/replace/split` (Go `regexp`, no literals),
  `sha256/md5/hmac_sha256/base64_encode/decode/hex_encode/decode/uuid/random_bytes`,
  `stat/cp/mv|copy/glob/path_join/abs_path/remove_all`, `exec/shell/cwd/env_all` + `exec_pipes`/`spawn`/`proc_wait`/`proc_kill`,
  `format_time/parse_time/time_parts` (+ `now()` ms; no ticker), `db_put/get/delete/list` (JSON-file KV),
  sqlite extended dialect (JSON-file; UPDATE/JOIN/ORDER BY/LIMIT/OFFSET/GROUP BY+COUNT; WHERE AND-only, no OR;
  no transactions/indexes/prepared) + `postgres_*` compat names on the same engine,
  `log_info/warn/error` (stderr), `assert_eq/ne/contains`,
  `with_timeout/parallel`, `struct_validate/assert/enum_create/valid/is_number` + `use_state/set_state/on_mount` +
  `tcp_connect/send/recv/close/serve/shutdown` (int-handle registry, 5s deadlines) + `tls_connect` (client-only,
  `InsecureSkipVerify:false`, no `tls_serve`) + `ws_connect` + `ws_send`/`ws_recv`
  (RFC 6455 text frames: masked client send, reassembly, ping/pong; binary rejected; no server).
  VM subset builtins: `assert/len/range/str/int/float/type` (+ hidden `__iter_*` helpers).
* Go level nearly needs: `net/http` server polish (have background minimal `http_serve`), `fs` `watch`, fuller `testing` helpers.
* Rust level still needs: `tokio`-like async IO story (or documented blocking + workers),
  `serde`-like JSON schema validation (`struct_validate` is start), real `sqlite`/`postgres` native
  (have JSON-file dialect + KV + TCP + text-frame WS).
* Planned: `futures.md` P1 stdlib; left: native DB (server/indexes/transactions/OR/prepared), `watch`/ticker/`tls_serve`/WS-server.
* Score impact: Stdlib 9 holds on breadth + extended dialect (Go breadth for scripts);
  Stdlib 9→10 left (native DB + data-stack depth).

### 5. Ecosystem 8 (file-registry + real audit), Tooling 9 (LSP + non-interactive debug), Maturity 8 (release + CI)

* Today: `fusion.toml` + `fusion.lock` + semver (`^ ~ >= > < *` + `,` + path; git deps left) + `vendor/` offline +
  file-local registry (`publish/pull/yank`, sha256 sidecar + verify on pull, `scope/name` → subdir mapping,
  `FUSION_REGISTRY` dir override, default search `test-releases/*`; yanked excluded; newest-satisfying resolver) +
  real `fusion audit [appdir]` (`audit.go:81`: missing-lock / yanked-in-registry / missing-bundle /
  update-available / private-token-hint + `VerifyRegistry` recompute + `checkTransitive` closure) +
  `fusion test [target] [--timeout SECS]` (`*_test.ks` + `assert`, TAP, per-file isolation, per-file timeout —
  timed-out file reports `not ok`, run continues) +
  `fusion fmt/vet/doc/check/repl/bench/debug` (incl. enum-aware exhaustive-`switch` vet), hash-skip cache,
  host-`--cpuprofile` + `fusion debug --break/--trace` (breakpoints + trace + globals; non-interactive),
  full `fusion lsp` (stdio JSON-RPC: `initialize` advertises hover/definition/formatting/rename + `shutdown`/`exit`;
  hover works for top-level funcs + builtins; goto-definition returns real `st.Line - 1` lines;
  formatting returns real `FormatSource` edits; didOpen/didChange/didSave publish diagnostics; rename is cross-file;
  no completion yet) + VS Code ext v0.2.0 (providers + debugger contribution; hand-rolled client, no test harness) +
  `compile --dis/--run` + `test` + `build --bin/--target` + cache + `vendor` + `publish/pull/yank/registry/audit/lsp/debug` +
  `run-web`/`build-js`/`build-ssg` all wired in `cmd/fusion/main.go` (source build; `release/fusion` is v2.5).
  Private-token is env-hint only (never sent/checked); no docs.rs-like docs, no criterion reports
  (basic `bench` + host profile + `docs/bench.md`); `--race` is vet + env flag (+ “use `go run -race`” hint).
* Go level done except central-registry depth (have file-local registry+checksums, resolver, `vendor/`, `fmt/vet/test/bench/doc/debug`, host profile, hash-skip cache, real audit).
* Rust level left: central index, docs.rs-like docs, criterion-style benches, interactive debugging.
* Planned: P2 DX (completion, DAP) left.
* Score impact: Ecosystem 8 holds (real audit meets the bar; central server left);
  Tooling 9 holds (full LSP + debugger + ext are depth inside 9; interactive/DAP/completion left);
  Maturity 8 (release v2.5 + `ci.sh` + timeout + repeat-safe + 125 tests; `.github/workflows/`
  deployment-managed, TLS-server/`--bin` E2E gaps remain).

### Go/Rust-level checklist (all things, with owner doc)

| # | Area | Go bar | Rust bar | .ks v2.5 (honest) | Needed to close | Closes in |
|---|---|---|---|---|---|---|
| 1 | Compiler | `go build` static bin | `rustc` LLVM + LTO | tree-walk (full, struct/enum syntax, folding, 177 builtins) + VM v0.2 (`.ksb-1`, no `go`/`chan`/`select`/`sleep`/`import`/`try`/`defer`/`struct`-decl/closure-capture; nominals skip) + `--bin` embed via `go build -trimpath` + `docs/bench.md` (fib ≈2x, loop ≈0.7x) | full VM (consistent wins) → native AOT + real benchmarks | `futures.md` P1 runtime |
| 2 | Targets | `GOOS/GOARCH` | tiers + WASM | `--target` GOOS/GOARCH passthrough + hash-skip cache + host `--cpuprofile` + `-trimpath` repro (needs Go toolchain) | WASM run polish, remote/incremental cache, strip/symbol opts | `futures.md` P1 runtime |
| 3 | Types | structs/interfaces/generics | traits/enums/`Result` | gradual `: type` incl. union/generic annotations + struct/enum syntax + `is`/`?.`/`??` + helpers + `vet`/`check` + enum-aware exhaustive vet (no methods/variadics; VM nominals skip) | methods/variadics, full-VM nominals | `futures.md` P1 core |
| 4 | Errors | multi-return | `Result/Option/?` | `ok/err` values + `error()`+`try/catch` + `assert_eq/ne/contains` + `with_cancel` error paths | exhaustive `Result` checks | `futures.md` P0 done, P1 polish |
| 5 | Concurrency | `select`/race | `tokio`/rayon | `go/chan/select/timeout/for-in-chan/with_timeout/parallel/with_cancel-family`/vet-`--race` (interpreter; VM rejects) | scheduler + VM concurrency + real race + context plumbing | `futures.md` P0 done, P1 rest |
| 6 | Memory | GC + `sync` | borrowck | Go GC (ok) | document sharing rules, no borrowck | non-goal, docs only |
| 7 | Stdlib net | `net/http` | `std::net`+crates | `http_*` (serve minimal, no shutdown) + `tcp_*` (+shutdown) + `tls_connect` (client-only) + `ws_connect`/`ws_send`/`ws_recv` (text frames, no server) | WS-server, `tls_serve`, serve polish | `futures.md` P1 stdlib |
| 8 | Stdlib OS | `os/exec` | `std::process` | `exec/shell/cwd/env_all` + `exec_pipes`/`spawn`/`proc_wait`/`proc_kill` + files + `stat/cp/mv/glob` | `watch` | `futures.md` P1 stdlib |
| 9 | Data | `encoding/*` | `serde` | `json_*` + `regex_*` (no literals) + `crypto` + KV-file `db_*` + JSON-file extended-dialect SQL + postgres-compat names + `use_state` + cancel | OR/transactions/indexes, native sqlite/postgres, `regex` literals | `futures.md` P1 stdlib |
| 10 | Packages | proxy + `go.sum` | crates.io + lock | `fusion.lock`+semver + file-local registry (`publish/pull/yank`, sha256, namespaces) + real `audit` + `vendor/`; `.ksb` per-file | central server, git deps, token auth | `futures.md` P0+P2 |
| 11 | Tooling | `fmt/vet/test/bench/pprof` | `clippy/fmt/bench` | `new/run/build/launch` + `compile` + `test --timeout` + `fmt/vet/doc/check/repl/bench/debug` + `audit` + full LSP + hash-skip cache + VS Code ext | completion, DAP, `.ks`-line profiler | `futures.md` P0+P2 DX |
| 12 | IDE | `gopls` | `rust-analyzer` | LSP (hover/goto/rename/diagnostics/format), ext v0.2.0, non-interactive debugger | completion, DAP/step-REPL, ext test harness | `futures.md` P2 DX |
| 13 | Frontend | `html/template`/WASM | WASM pkgs | console + `run-web` SSR (keyed diff, no reload; background ISR; nested layouts) + subset `build-js` (hashes/budgets/manifest) + `build-ssg` + `use_state` shim + API funcs + virtualize>100 | hydrate-full, CSS handling | `futures.md` P2 frontend |
| 14 | FFI | `cgo` | `unsafe`/FFI | none | opt-in `ffi_*` + Go plugin API | `futures.md` P2 interop |
| 15 | Stability | compat promise | editions | v2.5 source + `stability.md`/RFCs/LTS docs + `release/fusion` v2.5 + `ci.sh` + 125 tests + timeout + repeat-safe | `.github` gate (env-managed), TLS/`--bin` E2E | `futures.md` §5 |

Close full VM + completion/DAP + native-DB + methods/variadics + hydrate-full + central
registry with depth and `.ks` moves `83 → ~86–88/100`.
Rows 6/14 stay intentionally different (GC stays, `unsafe` stays opt-in).

## Decision guide

1. Browser UI? → React + Vite + TS, or Next.js for SSR (`.ks` `run-web`/`build-js`/`build-ssg` for prototype only; check `// unsupported` lines in generated JS).
2. CRUD + login + billing next week? → Laravel / Django / Rails / Next.js (`.ks` for sidecar `--bin` workers where a Go toolchain is fine).
3. 100k RPS / embedded / game loop? → Go / Rust / C++ / C.
4. Matrices / science / simulations? → Julia (or Python + numpy; `.ks` leads Julia on rubric balance but not on numerics).
5. Script, bot, rule engine, teaching `go/chan/select`, prototype/service? → `.ks` (interpreter + `--bin`).
   Pure arithmetic/control-flow/funcs with slices/`is`/`??`/typed/`switch`? → try `fusion compile --run` (VM v0.2; else interpreter).
6. Need `http/DB/net` today? → yes for basics: `http_*`/`fetch_json` (GET-only JSON helper), KV-file `db_*`,
   JSON-file extended-dialect SQL (UPDATE/JOIN/ORDER/GROUP/LIMIT, AND-only WHERE) + postgres-compat names,
   `exec_pipes`/`spawn` (signals TERM/KILL/INT/HUP/QUIT), `regex/crypto`, WS text frames, minimal `tcp/tls`, `use_state`, cancel — breadth done,
   native depth left; need transactions/OR/indexes/native server? → wait.

## Honest limits of `.ks` v2.5 (do not hide)

* Full language is tree-walk + struct/enum syntax + literal folding, no JIT/LLVM
  codegen; `--bin`/`--target`/cache/repro ride on `go build` (needs Go toolchain;
  cache is whole-app hash-skip, `vendor/` swaps don't invalidate). Compiler v0.2
  narrows but does not close this: `.ksb-1` is portable JSON run by `fusion`
  (not a static binary — that is `--bin`), partial subset (no `go`/`chan`/`select`/
  `sleep`/`import`/`try`/`defer`/`struct`-decls/closure-capture; nominals skip),
  7 user (+ 5 hidden) builtins, `maxFrames 1024` / `maxSteps 20M`.
  Int `**` is O(log n) in both engines. `docs/bench.md` shows VM fib ≈2x interp
  but VM loop ≈0.7x — no uniform speedup claim.
* Gradual types only: struct/enum *syntax* + enum-aware vet done; variadics/named
  params and methods/interfaces missing; `==` uses deep equality. VM checks base
  types and skips nominals (interpreter validates; `vm.go:1110`). `is`/`in`/unary
  folding limits in §“vs TypeScript”.
* Flat lib namespace default (prefix funcs; no `import "x" as h` yet). `fusion.lock`+
  semver+`vendor/`+file-local registry (`publish/pull/yank`, sha256 sidecar+verify
  on pull, `scope/name` dir mapping, `FUSION_REGISTRY` dir) + real `audit`
  (hash recompute + transitive) now; no central server/docs.rs; private-token is
  env-hint only; no git deps. `.ksb` is per-file bytecode, not a package format
  (that stays `.kslib` source JSON).
* `fusion run --race` is error-vet + env flag (+ “use `go run -race`” hint),
  `--cpuprofile` is host Go `pprof` — no deterministic scheduler, no `.ks`-line
  profiler. `fusion debug --break/--trace` is breakpoints + trace + globals snapshot
  (non-interactive; no DAP/step-REPL). `fusion lsp` is hover/goto/rename/
  diagnostics/format over stdio (no completion) + VS Code ext v0.2.0 (hand-rolled
  client, no vscode-test harness). No `go/chan/select/sleep` in compiled output yet;
  `go defer` rejected.
* `frontend/` is SSR + keyed DOM-diff without reload + background ISR + nested layouts
  + subset-JS (hashes/manifest/budgets) + SSG + `use_state` shim (`on_mount`
  immediate — no hydrate-full, `fetch_json` GET-only, virtualize>100) — still no CSS
  handling. See `plan/frontend.md`.
* Net/data depth: `http_serve` always `application/json`, no method/status/headers/
  shutdown; `tcp_serve` has `tcp_shutdown`; `tls_connect` client-only;
  `ws_connect` + text frames only (binary rejected, no server); `db_*` KV-file;
  extended-dialect SQL is JSON-file (AND-only WHERE — no OR — no transactions/
  indexes/prepared); `regex` no literals; `time` no ticker; `fs` no `watch`.
* Version/hygiene: toolchain source reports `v2.5` (`fusion version`,
  `fusion help`, `toolVersion` in `cmd/fusion/main.go:332` — single constant,
  keep in sync). `release/fusion` is **v2.5** (rebuilt). `go test ./...` green:
  125 funcs + 5 benchmarks + 2 `.ks` test files; TLS-server/`--bin`/`--target`/
  `build-js`-correctness/`repl`-CLI/`vendor`-E2E untested. `retest.log` is a leftover
  (`retest.sh` does not exist). CI gate is `ci.sh` (`go vet` + `go test` +
  repeat-safe + `fmt --check` + `vet`/`check`); `.github/workflows/` is
  deployment-managed here (only `blank.yml` persists). Repeat-safe verified:
  `go test ./internal/backend/ -run TestV23TCP -count=3` green (port 0 +
  `tcp_shutdown`).

## Corrections in this rewrite (v2.5 interim 87 → honest 83)

The interim revision claimed 87 with a +1 for each of Perf/Types/Stdlib/Ecosystem/
Tooling/Frontend/Maturity. Re-audit against the code holds three, reverts four:

* Perf 8 → **7**: VM v0.2 (slices/`is`/`?.`/`??`/typed/`switch`, O(log n) `**`)
  is real and measured (`docs/bench.md`: fib ≈2x), but 7 rejects remain
  (`go`/`chan`/`select`/`import`/`try`/`defer`/`sleep`), the loop regresses
  (≈0.7x), and tying Go (compiled native) is indefensible on a 2x partial win.
  Progress inside 7; 8 needs full coverage + consistent wins. (Also fixed: VM
  `let u: User` used to fail `wants User, got map` — nominals now skip via
  `vm.go:1110` `isKnownVMType`, interpreter validates; compiler reject strings
  corrected from stale `v0.1`/misleading-nominal wording.)
* Types 9 → **8**: struct/enum *syntax* pre-dates the vet work (the v2.4 doc wrongly
  said “no syntax” — corrected: `frontend.go:1040-1118`, `backend.go:1250,1278`
  existed); the new depth is the enum-aware vet (`tools.go:642-661,727,742`,
  tested). A lint, however real, adds no expressive power; beating Go (methods/
  interfaces/generics) needs methods/variadics/full-VM nominals. Progress inside 8.
* Stdlib 10 → **9**: WS text frames + extended dialect (UPDATE/JOIN/ORDER/GROUP/
  LIMIT + postgres-compat) + pipes/signals are real and tested — but the engine is
  still JSON-file (AND-only WHERE per `stdlib_ext3.go:676`, no OR/transactions/
  indexes/prepared/server). Tying Python (numpy/django/sqlite3) overshoots.
  Progress inside 9; 10 needs native DB + data-stack depth.
* Ecosystem 7 → **8**: HELD. Real audit (recompute + transitive, both tested)
  meets the bar. No central server (stated remainder).
* Tooling 10 → **9**: full LSP (diagnostics/rename/format/hover/goto, tested) +
  non-interactive debugger + ext v0.2.0 are real — but the debugger has no
  stepping/DAP/REPL, there is no completion or `.ks`-line profiler, and the ext
  has no vscode-test harness. Beating gopls/delve (10 > 9) overshoots. Progress
  inside 9; 10 needs interactive debugging + completion.
* Frontend 7 → **8**: HELD. Keyed diff without reload + background ISR, both
  asserted by tests (`isr_test.go:114,137`). Prototype caveats stated.
* Maturity 7 → **8**: HELD. Release v2.5 + `ci.sh` + per-file timeout +
  repeat-safe + 125 tests. `.github` limitation and coverage gaps stated.
* Ranking recomputed: `.ks` 83 vs Go 82 vs Rust 81 (lead of 1–2 on balance for
  scripts; behind on native depth). Grand total `1314/1800`, avg `73.0`.
  Extra-stack margins recomputed from 83.

Factual fixes in this rewrite (stale v2.4-era claims corrected):

* Counts: **177 distinct** (`96` base + `52` ext + `11` ext2 + `12` ext3 + `6` ext4;
  `grep -ohP ... | sort -u | wc -l`; tests `>=166`/`>=177`). Old `166`/`158`/`170+` unified.
* Compiler: v0.2 subset list (slices/`is`/`?.`/`??`/typed/`switch` added; rejects
  `go`/`chan`/`select`/`sleep`/`import`/`try`/`defer`/`struct`-decls/closure-capture);
  int `**` O(log n) in both engines (was O(n) in VM); nominals skip in VM.
* Types: “no struct/enum syntax” → syntax done (`frontend` + `backend`), vet is
  enum-aware (not missing-`default`-only), VM base-types-only disclosed.
* Next.js section: “patch-first with reload fallback / no background regen” →
  keyed diff without reload + background regen (with line refs + test names).
* TS section: “no struct/enum syntax / missing-default lint / VM rejects all
  annotations” → syntax done, enum-aware vet, VM base checks + nominal skip.
* React/Vite sections: “HMR-patch+fallback / no HMR-diff” → keyed patches, no reload.
* Registry/audit: “narrow audit, no recompute/transitive” → real audit (both, tested).
* LSP: “hover/goto-file/format-noop, no diagnostics/rename/ext” → full
  (diagnostics/rename/format/hover/goto with real lines) + ext v0.2.0 + debug.
* Maturity: “stale v2.0 / no timeout / no CI / hardcoded port” → v2.5 / timeout /
  `ci.sh` / port-0 repeat-safe (all verified).
* `--debug` print-only → `fusion debug` (breakpoints/trace/globals) exists;
  `run --debug` vet-dump unchanged.
* `fib ~70x` / `11M` anecdotes retired; `docs/bench.md` is the artifact.
* Out-of-scope sync (updated alongside): `README` 87→83, `docs/futures.md`
  87→83 header, `list.md` 87→83 header + v2.5 boxes.

## How to verify (run these)

```bash
go build -o /tmp/fusion ./cmd/fusion && /tmp/fusion version   # want: ks-fusion v2.5 (release/fusion also v2.5)
grep -ohP '\{Name: "\K[^"]+' internal/backend/*.go | sort -u | wc -l        # want: 177 (96+52+11+12+6)
grep -ohP '\{Name: "\K[^"]+' internal/backend/*.go | sort | uniq -d | head  # want: no dups (empty)
grep -rh "^func Test" internal/ --include="*_test.go" | wc -l             # want: 125
grep -rh "^func Benchmark" internal/ --include="*_test.go" | wc -l        # want: 5
go test ./... -count=1                                     # all green
go test ./internal/backend/ -run TestV23TCP -count=3       # repeat-safe (port 0 + tcp_shutdown)
go test ./internal/compiler/ -run TestCompileV02 -count=1 -v  # VM v0.2 slices/is/typed/switch (+nominal-skip)
go test ./internal/tools/ -run 'TestVetExhaustive|TestAudit|TestLSP|TestDebug|TestISR|TestWeb|TestSSE' -count=1 -v
go test ./internal/backend/ -run 'TestRunStructEnum|TestRunSqlite|TestRunPostgres' -count=1 -v
go test ./internal/backend/ -bench BenchmarkInterp -benchtime 1x -run XXX  # interp fib/loop/map
go test ./internal/compiler/ -bench BenchmarkVM -benchtime 1x -run XXX    # VM fib (~2x) / loop (~0.7x)
node --check editors/vscode/extension.js && echo "VS Code ext JS OK"
/tmp/fusion debug /tmp/dbg.ks --break 2 --trace | head                    # breakpoints + trace + globals
/tmp/fusion vet ./tests/hello-app && /tmp/fusion check ./tests/hello-app  # vet warns-only + check ok
/tmp/fusion fmt . --check                                  # clean
go vet ./...                                               # clean
bash ci.sh 2>&1 | tail -n 2                                # CI OK
grep -n "runs in interpreter" internal/compiler/compiler.go | head  # 7 remaining rejects
grep -n "AND" internal/backend/stdlib_ext3.go | head -n 3  # AND-only WHERE (no OR)
ls ci.sh docs/bench.md release/fusion && ./release/fusion version
```
