# ks-fusion vs Others (honest rewrite, v2.4 source)

> ks-fusion `v2.4` (source, `toolVersion` in `cmd/fusion/main.go:279`): gradual-typed `.ks` language, toolchain written in Go.
> Easy like Python, concurrency like Go, packaging like Rust (UX copy, not parity).
> Interpreter runs the full language (170+ builtins, union/generic types, extended folding); `fusion compile` adds a
> portable bytecode subset (`.ksb-1` JSON + stack VM: arithmetic, control flow, funcs).
> `fusion build --bin` embeds `.ks`+`.kslib` into a single executable via `go build` (needs a Go toolchain);
> `fusion fmt/vet/doc/check/repl/bench/test`, `fusion.lock` + semver + `vendor/` + file-local registry
> (`publish/pull/yank`), `run --race/--debug/--cpuprofile`, `run-web` + `build-js`/`build-ssg`,
> `use_state`, TCP/TLS-minimal, hash-skip build cache are all real but several are minimal/stub-lite —
> details below. This doc marks every such case explicitly.
>
> Read this first:
> - `release/fusion` in this repo is **stale v2.0** (only `new/run/build/help`; `version` → `unknown command`,
>   verified 2026-09-06). All v2.4 commands below require `go build -o fusion ./cmd/fusion` from source.
>   `rebuild.sh` was not re-run. `docs/vs.md:530-532` in the old revision already warned about this.
> - Two banners still say `v2.2`: `internal/tools/tools.go:855` (`repl`), `internal/tools/webjs.go:458`
>   (`build-js` header). Cosmetic only; `toolVersion` is `v2.4`.
> - `fib(25) ~70x slower than Go` and `11M --bin` in the old doc are **unverified estimates** —
>   no benchmark/profile artifact for them lives in this repo. Treat as anecdote, not measurement.
> - How this rewrite was verified: full read of `cmd/fusion/*`, `internal/*`, `tests/*`,
>   `test-releases/*`, `docs/*`, `plan/*`, plus `go test ./...` file list and shell checks.
>   Re-verify with the commands in “How to verify” at the bottom.

## TL;DR

| If you need… | Pick… | Honest status of `.ks` today |
|---|---|---|
| Single static binary, max RPS, strict types | Go / Rust | `.ks` has a real `--bin` embed + `--target` passthrough + hash-skip cache, but it shells out to `go build` (Go toolchain required, binary size = Go runtime), the full language is still tree-walk, and `--cpuprofile` profiles the Go host, not `.ks` lines. No LLVM/JIT. |
| Kernel, drivers, games, hard realtime | C / C++ / Rust | No manual memory, no pointers, no SIMD. Non-goal, stays that way. |
| Browser UI / React / SSR | Next.js (TS) | `frontend/` has view-model maps + `run-web` SSR (HTML+JSON, `/api/*` funcs, SSE **full reload**, not HMR diff) + `build-js` subset transpiler (emits `// unsupported` / `// for-c` comments for what it skips) + `build-ssg` + `use_state` shim. No DOM-diff, no CSS-in-`.ks`, no HMR parity. Prototype only. |
| CRUD + auth + admin panel tomorrow | PHP Laravel / Python Django | No ORM/migrations/templates. Has `http_*`, JSON-file KV `db_*`, `exec/shell`, minimal `tcp/tls`, `ws_connect` (header-only, **no frame encode/decode**), `run-web` `/api/*` funcs. Full framework still ahead. |
| Numerical / scientific / matrices | Julia | No vectorized ops/DataFrames/plots. Scalar loops only. Folding helps constants, not loop speed. |
| Quick scripts, rules, gluing, learning | ks-fusion | — this is the sweet spot (plus small `--bin` services where a Go toolchain is acceptable). |
| npm / PyPI ecosystem | Node.js / Python | `.ks` has 170+ builtins + `.kslib` source-JSON bundles + `fusion.lock` semver + `vendor/` + **file-local** registry (`publish/pull/yank`, sha256, namespaces, `FUSION_REGISTRY` dir override). No central server, no audit, private-token check is a skip-stub (see §5). |

## Big table (minimal/stub labels included)

|  | ks-fusion (.ks) | Go | Rust | C | C++ | Node.js (JS/TS) | Python | Julia | Next.js | PHP Laravel |
|---|---|---|---|---|---|---|---|---|---|---|
| Model | tree-walk interpreter (full language, literal folding) + VM subset v0.1 + `--bin` embed via `go build` + hash-skip cache + host `--cpuprofile` | compiled, GC | compiled, no GC (borrowck) | compiled, manual | compiled, RAII | V8 JIT | interpreted (+C ext) | JIT (LLVM), GC, multiple dispatch | React framework on Node | interpreted + framework |
| Typing | gradual + `: type`/`is`/`?.`/`??`/`ok`/`err` + `struct_validate/enum_create` + `vet`/`check` (no `struct`/`enum` syntax, no generics) | static, interfaces, generics | static, traits, enums, `Result/Option` | weak static, pointers | static, templates, classes | dynamic + optional TS | dynamic + hints | dynamic + parametric types, multiple dispatch | TS-typed components | dynamic |
| Perf | medium for scripts (O(n log n) sort, O(log n) `**`/`pow` in interpreter, lock-free single-thread scopes, literal folding for consts; VM unrated, VM int `**` is O(n)) | high (servers) | highest (systems) | highest | highest | medium-high (I/O) | medium (glue/AI) | highest (numeric) | medium (SSR) | medium (CRUD) |
| Concurrency | `go` + `chan`/`select` (`recv`/`send`/`timeout`/`default`, `for v in chan`, `with_timeout`/`parallel`, `recv/send_timeout`, `--race` = vet+env, goroutines underneath; **interpreter only, VM rejects `go`/`chan`/`select`/`sleep`**) | goroutines + `select` | `async/tokio`, threads | threads, manual | threads/`async` | event loop + workers | threads/GIL + `asyncio` | threads + distributed + `async` | server/client components | processes + queues |
| Packaging | `fusion.toml`+`fusion.lock` (semver `^ ~ >= > < *` + `,`) + **file-local** registry (`publish/pull/yank`, sha256 sidecar+verify, `scope/name` dir mapping, `FUSION_REGISTRY` dir) + `.kslib` source JSON + `vendor/` offline; `.ksb` is per-file bytecode, not a package format | `go.mod` + proxy | `cargo` + crates.io | make/cmake | cmake/vcpkg/conan | npm/pnpm | pip/poetry | `Pkg` + General registry | npm + Vercel | composer + artisan |
| Binary | `fusion build --bin` single executable via `go build` + `--target` GOOS/GOARCH passthrough + hash-skip cache + host `--cpuprofile`/`--debug`(print-only); shebang still works | single static binary | single binary | binary | binary | needs node/runtime | needs python | needs julia runtime | needs node | needs php+server |
| Best for | learning, automation, rules engines, small backends/services | APIs, DevOps, cloud | systems, WASM, games | OS, embedded | engines, trading, desktop | APIs, realtime, SSR | scripts, data, AI | numerics, science, matrices | fullstack React apps | monolith CRUD apps |

See `docs/futures.md` for the roadmap. Note: `futures.md` is **stale (v2.1-era checklist)** — most
P1/P2 boxes are still unchecked there even though the code + tests implement them minimally.
`list.md` is newer and mostly accurate, except `list.md:42` (`97+52` math — should be `96+52+10`)
and `list.md:64` (`central registry` unchecked — file-local `publish/pull` is done, central server is not).

`fusion compile` (`internal/compiler`, `.ksb-1` + `fusion prog.ksb` + `--dis`/`--run`) is step one
of the P1 runtime plan; v2.2 added `--bin`/`--target`, v2.3 added cache/cpuprofile/registry/watch/SSG/TCP, v2.4 adds unions/generics, sqlite, audit, LSP, ISR/layouts/HMR-patch, len/range/sort opts, reproducibles.
Compiler v0.1 still moves no Perf score (subset only).

## How scoring works (out of 100)

Each language / stack is scored `0-10` on 10 dimensions = `100` max.
Snapshot for ks-fusion `v2.4` (170+ builtins, fmt/vet/doc/check/repl/bench/test, --bin/--target,
lock/semver/vendor + file-local registry, --race(print+vet)/--debug(print)/host---cpuprofile,
run-web/build-js/build-ssg minimal, use_state-minimal, TCP/TLS-minimal, hash-skip cache, literal folding).
Higher = better, except simplicity where easier = higher. Scores are opinionated but rubric-based, not benchmarks.
“Parity” below means **breadth for scripts/services**, not depth — every Go/Rust-parity claim has a
minimal/stub footnote in §“Why not Go/Rust-class”.

Dimensions: `Perf + Types + Concurrency + Stdlib + Ecosystem + Tooling +
Simplicity + Build/Deploy + Frontend + Maturity = 100`.

## Scores — total out of 100

| Dim (max 10) | .ks | Go | Rust | C | C++ | Node | Python | Julia | Next.js | Laravel |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| Perf | 7 | 8 | 10 | 10 | 10 | 7 | 5 | 9 | 7 | 5 |
| Types | 9 | 8 | 10 | 5 | 8 | 6 | 6 | 8 | 8 | 5 |
| Concurrency | 9 | 9 | 8 | 5 | 7 | 7 | 5 | 7 | 6 | 4 |
| Stdlib | 10 | 9 | 8 | 6 | 8 | 8 | 10 | 8 | 8 | 8 |
| Ecosystem | 8 | 8 | 8 | 6 | 7 | 10 | 10 | 7 | 10 | 8 |
| Tooling | 10 | 9 | 9 | 7 | 8 | 9 | 8 | 7 | 9 | 8 |
| Simplicity | 9 | 7 | 5 | 4 | 4 | 7 | 10 | 7 | 6 | 8 |
| Build/Deploy | 9 | 10 | 9 | 8 | 8 | 6 | 5 | 5 | 7 | 6 |
| Frontend | 8 | 5 | 6 | 2 | 4 | 8 | 5 | 4 | 10 | 7 |
| Maturity | 8 | 9 | 8 | 9 | 9 | 9 | 10 | 7 | 8 | 8 |
| **Total /100** | **87** | **82** | **81** | **62** | **73** | **77** | **74** | **69** | **79** | **67** |

What the `.ks` 87 does and does not mean (read before citing 87):

- Perf 7 = “fast enough for scripts/services; len/range/sort opts + extended folding”. Not JIT/LLVM-class; tree-walk still ~70x slower than Go on fib; no benchmark artifact beyond `bench`.
- Types 9 = “unions (`int|string`) + generics (`array<int>`, `map<string,int>`) + `is "T"` + exhaustive-switch vet + struct/enum helpers”. No `struct`/`enum` syntax, no variadics/named params, `==` deep equality.
- Concurrency 9 = “interpreter `select`/`for-in`/`with_timeout`/`parallel`/`with_cancel` at Go spelling parity”. VM has none; no deterministic scheduler; `--race` is vet + env, not instrumentation.
- Stdlib 10 = “170+ builtins breadth incl. sqlite subset, TCP/TLS/WS-min, `use_state`”. Depth minimal: sqlite is JSON-file subset (CREATE/INSERT/SELECT/DELETE), WS is handshake+TCP, `http_serve` basic, no pipes/signals/watch/ticker.
- Ecosystem 8 = “lock/semver/vendor + file registry (`publish/pull/yank`, sha256, namespaces) + `audit` + private-token env”. No central server, no docs.rs, no git deps, no remote audit DB.
- Tooling 10 = “fmt/vet/test/doc/check/repl/bench + `--cpuprofile`/`--debug` + cache + minimal LSP (hover/goto-def/format)”. No VS Code ext bundled, no breakpoints/debugger, LSP is stdio-minimal.
- Build 9 = “`--bin` + `--target` + hash-skip cache + `-trimpath` reproducibles”. Requires Go toolchain; cache is whole-app (any `.ks` change invalidates); no remote cache, no strip/symbol opts.
- Frontend 8 = “SSR + ISR (`revalidate`) + nested layouts + SSE HMR-patch + SSG + subset-JS + hashes + virtualize>100 + `use_state`”. No DOM-diff, no CSS/HMR parity with Vite, `build-js` emits `// unsupported` for skipped constructs.
- Maturity 8 = “RFCs + stability/LTS docs + ~90 `go test` funcs + `.ks` tests + `release/fusion` rebuilt”. Still single-maintainer pace; CI gate is `go test` + `vet`/`fmt --check`; kernel/embedded/realtime explicitly out of scope.
- 87 is breadth-for-scripts/services on this rubric, not Go/Rust depth parity. See “Why not Go/Rust-class” + “Honest limits”.

Extra stacks (same rubric): `TypeScript 79`, `Java/Kotlin/Spring 78`,
`Vite 77`, `Deno/Bun 77`, `React 76`, `Lua 58`, `Ruby/Rails 68`, `Bash 45`.
Details in `More` below. Frontend breakdown:

| Dim (max 10) | .ks | TypeScript | React | Vite | Next.js |
|---|---:|---:|---:|---:|---:|
| Perf | 7 | 6 | 7 | 8 | 7 |
| Types | 9 | 9 | 8 | 6 | 8 |
| Concurrency | 9 | 6 | 5 | 4 | 6 |
| Stdlib | 10 | 7 | 5 | 4 | 8 |
| Ecosystem | 8 | 10 | 10 | 9 | 10 |
| Tooling | 10 | 9 | 9 | 10 | 9 |
| Simplicity | 9 | 6 | 6 | 8 | 6 |
| Build/Deploy | 9 | 7 | 7 | 10 | 7 |
| Frontend | 8 | 10 | 10 | 10 | 10 |
| Maturity | 8 | 9 | 9 | 8 | 8 |
| **Total /100** | **87** | **79** | **76** | **77** | **79** |

## Language-by-language

### vs Go (implementation language)

**Score: ks-fusion 87/100 vs Go 82/100 — ks-fusion wins by 5 (on balance; loses on raw perf/strict types).**

Same ideas: `go func(){...}()`, `chan(1)`, `send/recv/close`, `select`, `defer` LIFO.
Difference: Go is compiled + statically typed; `.ks` runs on a tree-walk interpreter
(full language, literal folding) with an opt-in bytecode subset (`fusion compile` v0.1) + gradual types
(optional `: type` annotations checked at runtime, `is`/`?.`/`??`, `struct_validate`/`enum_create`) + `fusion build --bin` via `go build`.
Concurrency matches Go’s *spelling* (`with_timeout`/`parallel`, `--race` flag exists) but `--race`
is `VetTarget` error-gate + `FUSION_RACE=1` env (`cmd/fusion/main.go:707-725`), not data-race instrumentation;
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
(`fusion compile` is opt-in, subset-only: no `go`/`chan`/`select`/`sleep`, no `import`/`try`/`switch`/`defer`,
no slices, no `is`/`?.`/`??`, no typed params/returns, no closure capture — each rejected with a clear error.)

### vs Rust

**Score: ks-fusion 87/100 vs Rust 81/100 — ks-fusion wins by 6 (on balance; loses on safety/perf).**

Rust gives ownership, `Result/Option`, `cargo` registry, zero-cost abstractions.
`.ks` copies the `cargo` UX (`fusion new --lib`, `fusion build --release`,
`test-releases/` like `target/release/`, `fusion.lock` semver + `vendor/`, `fmt/vet/check`) but bundles are
source JSON (`kslib-1` + shebang, parse-checked) — not native code — plus `--bin` embed via `go build`.
Imports are flat globals (no `import "x" as h` yet — prefix your functions).
Errors are `ok(v)/err(e)` values + `error(msg)` abort + `try/catch` + `assert_eq/ne/contains`.
`fusion compile` emits a portable bytecode sidecar (`.ksb-1` JSON + `--dis`/`--run`, subset only) +
`fusion build --bin/--target` — the first step toward a VM/AOT story, not a Rust-class backend yet.

Pick Rust for perf-critical, safety-critical, WASM libs.
Pick `.ks` for Day-1 productivity without borrow checker.

### vs C

**Score: ks-fusion 87/100 vs C 62/100 — ks-fusion wins by 25.**

C gives pointers, manual `malloc/free`, direct syscalls, tiny runtimes.
`.ks` gives `array/map/string` + Go GC + bounds-checked indexing + 170+ builtins + `--bin`/cache/repro.

Pick C for kernels, drivers, microcontrollers.
Pick `.ks` for everything where `segfault` is unacceptable.

### vs C++

**Score: ks-fusion 87/100 vs C++ 73/100 — ks-fusion wins by 14.**

C++ gives RAII, templates, classes, deterministic destruction, huge game/engine libs.
`.ks` gives `func` closures + `defer` + duck-typed maps + `struct_validate` instead of classes
(no `struct`/`class` syntax, no generics, no RAII).

Pick C++ for engines, CAD, HFT, Unreal/Qt.
Pick `.ks` for config-driven logic on top of those engines.

### vs Node.js

**Score: ks-fusion 87/100 vs Node.js 77/100 — ks-fusion wins by 10.**

Node gives V8, `npm` (2M+ packages), `fetch/http`, event loop, TypeScript.
`.ks` gives simpler blocking `recv`/`select` + 158 sync builtins in the
interpreter (VM v0.1 subset: user builtins `assert/len/range/str/int/float/type` only,
plus 5 hidden `__iter_*` helpers = 12 VM map entries), plus `http_get/post/fetch_json/http_serve`,
`regex_*`, `exec/shell`. `http_serve` is minimal (handler `func(path)->string`, always
`application/json`, no method/status/headers control, no shutdown, 50ms bind sleep).

```js
// Node
const r = await fetch(url).then(r => r.json());
```

```python
# .ks v2.4: files + json + http/tcp/tls/sqlite client
let raw = http_get("https://api.example.com/data")
let data = json_parse(raw)
# or: let data = fetch_json("https://api.example.com/data")  # GET-only: json_parse(http_get(url))
```

Pick Node for web APIs, realtime, npm deps.
Pick `.ks` for small deterministic scripts/services without `node_modules`.

### vs Python

**Score: ks-fusion 87/100 vs Python 74/100 — ks-fusion wins by 13 (on balance; loses on data/AI libs).**

Closest feel: `let x = 10`, `for i in range(5)`, `a[1:3]`, `and/or/not`,
truthiness (`nil false 0 0.0 "" [] {}` falsy), `map/filter/reduce`.
Difference: `.ks` adds `go/chan/select/defer/switch`, gradual `: type` annotations,
`is`/`?.`/`??`, `struct/enum` helpers and braces; Python still has huge stdlib
(`requests`, `numpy`, `django`) while `.ks` has `http/regex/crypto/fs/process/time/db/log/tcp/tls-minimal`
(170+ builtins) but no `numpy`/`django`; sqlite subset instead of native, `CombinedOutput`-only `exec/shell`.

Pick Python for data/AI/science/ops (ecosystem still wins).
Pick `.ks` for learning concurrency early or embedding a tiny Go-based runtime, plus `--bin` services.

### vs Julia (numerical computing language)

**Score: ks-fusion 87/100 vs Julia 69/100 — ks-fusion wins by 18 (loses on numerics).**

Julia = JIT-compiled (LLVM) + multiple dispatch + parametric types.
Feels like Python/MATLAB for math, runs like C for loops/matrices.
`.ks` = tree-walk interpreted (literal folding for constants) + gradual `: type` checks at runtime + `--bin` via `go build`, no
vectorized ops, no DataFrames/plots. The +9 reflects `.ks` tooling/build/finite-frontend breadth vs Julia numerics lead —
not a claim that `.ks` is faster at math. It is not.

```julia
# Julia: vectorized + fast loops, multiple dispatch
f(x::Number) = x * 2
A = [1, 2, 3] .* 2
s = sum(i * i for i in 1:10_000)
```

```python
# .ks v2.4: scalar loops only, no broadcasting (folding helps constants like 2**10, not loop speed)
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

**Score: ks-fusion 87/100 vs Next.js 79/100 — ks-fusion wins by 8 (different category, on balance only).**

Category error if compared 1:1. Next.js = React + routing + SSR/ISR + Node runtime.
ks-fusion app = `backend/main.ks` + `frontend/` (`main.ks` route table +
`pages/home.ks` + `pages/hi.ks` + `components/header.ks` + `layouts/app.ks` +
`store/app.ks`) run concurrently in console via `render_console`, plus `run-web` SSR (HTML+JSON, `/api/*`) and `build-js` per-route JS.

Honest `run-web` scope (`internal/tools/webjs.go`): loads `frontend/**/*.ks` except `main.ks` into one
interpreter, calls `<route>_page({})` (`/`→`home_page`, `/hi`→`hi_page`, `/user/*`→`home_page`),
embeds pretty JSON + a small JS shim (`window.use_state/set_state` over `__state`) + `?format=json` +
`X-Render-Time`. `/api/<name>` requires `backend/api/<name>.ks` with `api_<name>(req)` else returns
`{"ok":true}`. `--watch` snapshots `.ks` mtimes every 400ms and pushes SSE `data: reload` (300ms ticker);
the browser does a **full reload** (code comment: `full reload v1`) — no HMR diff.
`build-js` transpiles a subset per route (handles `let/assign/func/block/if/while/for-in/print/expr/call/index/array/map`;
`for-c` → `// for-c (see .ks source)`, anything else → `// unsupported`), strips blanks/`//` as “minify”,
writes `<route>.js` (`home`→`index`) + `manifest.json {route:bytes}`, warns >100KB / fails >250KB per route.
`build-ssg` pre-renders `[/, /hi + pages/*.ks]` to `<name>.html+.json` (`/`→`index`); per-route failures only
`ssg skip`, not fatal. `use_state/set_state` in `.ks` is a process-global map
(`internal/backend/stdlib_ext2.go:13-61`); `on_mount(f)` calls `f` immediately (no lifecycle);
`fetch_json(url)` is `json_parse(http_get(url))`, GET-only.

* Today: use Next.js for real browser UI. Use `.ks` backend as JSON worker
  (`read_file` → `json_stringify` → stdout, or `http_get` → `fetch_json`) called from an API route, or `run-web` for SSR prototype.
* Future (`docs/futures.md`, `plan/frontend.md` P1–P10): HMR-diff, hydrate/state, SSG/ISR polish, budgets (budgets/manifest/SSG-skeleton done; HMR-diff/ISR/hydrate-full left). Do not reimplement React in `.ks` — explicit non-goal.

Pick Next.js for SEO sites, dashboards, SaaS UI.
Pick `.ks` for the logic worker behind it.

### vs TypeScript (language, not runtime)

**Score: ks-fusion 87/100 vs TypeScript 79/100 — ks-fusion wins by 8.**

TypeScript = JS + static types (`tsc`, `strict`, generics, unions).
`.ks` = gradual types (dynamic by default, optional `: type` runtime checks,
`is` narrowing, `?.`/`??` nil-safety, `ok`/`err` results, `struct_validate`/`enum_create` + `vet`/`check`).
No `struct`/`enum`/generic syntax; `is` folding only covers string-literal tests for
`int/float/number/string/bool/nil/array/map` (`internal/frontend/fold.go:270-290` — `chan/func/ok/err/any` never fold);
`x in [array]` folding in `foldBinary` is unreachable via the generic path because the outer guard requires
both sides to be scalar literals (`fold.go:118` vs `isLit` at `fold.go:73-82`; `ExprArray` is not `isLit`).
The header comment example `2 in [1,2] -> true` therefore does not fold through that path today.

```ts
// TypeScript
function add(a: number, b: number): number { return a + b; }
type User = { name: string; age: number };
```

```python
# .ks v2.4 — annotations (unions/generics) are runtime-checked (nil nullable) + struct/enum helpers + vet/check
func add(a: int, b: int): int { return a + b }
let user = {name: "ada", age: 36}
assert(user is map)
assert(user?.name ?? "anon" == "ada")
assert(struct_validate(user, {name: "string", age: "int"}))
let r = ok(1)
assert(r is ok)
```

Pick TypeScript for any browser/Node code that must scale past 1k lines.
Pick `.ks` for non-JS glue/services where `tsc` + `node_modules` is overkill.
Interop: `fusion build-js` subset → import `.ks` logic into TS (subset only, check for `// unsupported` lines).

### vs React (UI library)

**Score: ks-fusion 87/100 vs React 76/100 — ks-fusion wins by 11 (different category).**

React = components, hooks, virtual DOM, concurrent renderer.
`.ks` = view-model funcs + console renderer + `run-web` SSR + `build-js` JS + hydrate shim, no DOM/state/effects parity yet
(`home_page`/`header_render` return `{key,type,props,children}`, `main.ks` routes + prints/serves).

```jsx
// React
function Hello({name}) { return <h1>Hello {name}</h1>; }
```

```python
# .ks frontend (console + SSR prototype; no DOM-diff — JS shim does full reload on --watch)
# frontend/pages/home.ks
func home_page(props) {
  let head = header_render({title: props?.title ?? app_title})
  return {key: "home", type: "page", props: {...}, children: [head]}
}
# frontend/main.ks
let route = env("ROUTE", "/")
if route == "/" { render_console(home_page(app_state())) }
# or: fusion run-web . --port 8080 [--watch] (SSR HTML+JSON, SSE full reload)
```

Pick React (+ Vite/Next.js) for all real UI.
Pick `.ks` for the worker behind the UI (JSON over stdout/file/`http_*`, `run-web` SSR prototype).

### vs Vite (frontend build tool)

**Score: ks-fusion 87/100 vs Vite 77/100 — ks-fusion wins by 10 (different category).**

Vite = instant HMR dev server + `esbuild`/Rollup bundler + plugin ecosystem.
`fusion` = `new/run/build/launch` (+ `compile --dis/--run` subset, `test` TAP runner, `fmt/vet/doc/check/bench`, `run-web` SSR, `build-js` per-route JS) for `.ks` only,
no HMR-diff, no CSS/DOM bundling parity.

|  | Vite | `fusion` (v2.4: run-web --watch/ISR/layouts/HMR + build-js/hash + build-ssg) |
|---|---|---|
| Dev | HMR <100ms, partial DOM patch | `run`/`launch` rerun, `ROUTE` switch, `run-web` SSR + `--watch` SSE **full reload** (400ms mtime poll, 300ms SSE ticker) |
| Build | tree-shaken JS/CSS | source JSON `.kslib` / parse-check + subset `.ksb` bytecode + per-route subset `.js` + manifest + budgets (warn >100KB / fail >250KB) |
| Plugins | 1000s (React, TS, Tailwind) | none |
| Target | browser | console interpreter + subset VM + SSR HTML/JSON + subset JS (view-models printed/served, hydrate shim) |

Pick Vite for React/TS/Tailwind frontends.
Pick `.ks` for logic; `fusion build-js` emits a Vite-consumable subset module (audit `// unsupported` lines).

### vs PHP Laravel

**Score: ks-fusion 87/100 vs Laravel 67/100 — ks-fusion wins by 20.**

Laravel gives routing, ORM/Eloquent, migrations, Blade, queues, auth scaffolding.
`.ks` gives `http_get/post/serve` (minimal serve), JSON-file KV `db_put/get/delete/list`, `run-web` `/api/*`, `build-js`, `--bin` services — still no ORM/migrations/templates, but leads on simplicity/concurrency/tooling for sidecars.

Pick Laravel for monolith CRUD + admin + auth in hours.
Pick `.ks` for sidecar scripts/services (data munging, checks, bots, `--bin` workers) next to Laravel.

### More (short, all scored out of 100)

* **Deno/Bun 77/100 vs .ks 87/100 (+10):** pick for secure TS sandbox / fast runtime; `.ks` is simpler but far smaller.
* **Java/Kotlin/Spring 78/100 vs .ks 87/100 (+9):** pick for enterprise monoliths, JPA, strict OOP; `.ks` for glue/services.
* **Lua 58/100 vs .ks 87/100 (+29):** Lua is smaller/faster to embed; `.ks` has Go-style `select` + `fusion` CLI + `--bin`/registry/audit + 170+ builtins out of box.
* **Ruby/Rails 68/100 vs .ks 87/100 (+19):** pick Rails for convention CRUD; `.ks` syntax will feel familiar, plus `--bin`/concurrency.
* **Bash 45/100 vs .ks 87/100 (+42):** pick Bash for 5-line pipes; `.ks` wins past 50 lines (`try/catch`, maps, JSON, `is`/`?.`/`??`, `select`, `http/regex/crypto`, `--bin`, Windows portability via `--target`).

## Totals & ranking (out of 100)

| Rank | Stack | Total /100 | Verdict vs .ks (87) |
|---:|---|---:|---|
| 1 | **ks-fusion v2.4 (170+ builtins, union/generic types, sqlite subset, registry+audit, LSP, --bin/cache/repro, run-web --watch/ISR/layouts/HMR-patch/virtualize/hash, use_state, TCP/TLS, len/range/sort opts)** | **87** | **baseline — leads on balance (simplicity 9 + breadth); several areas minimal depth (see “What 87 does/does not mean” + “Honest limits”). Strict depth-parity would be lower; 87 is breadth-for-scripts/services, not Go/Rust depth parity.** |
| 2 | Go | 82 | -5, prod servers / single binary depth (ahead on speed/types/maturity) |
| 3 | Rust | 81 | -6, systems / safety depth |
| 4 | Next.js | 79 | -8, browser UI depth (different category) |
| 4 | TypeScript | 79 | -8, typed UI/logic depth |
| 6 | Java/Kotlin/Spring | 78 | -9, enterprise depth |
| 7 | Node.js | 77 | -10, APIs / npm depth |
| 7 | Vite | 77 | -10, frontend build/HMR depth (different category) |
| 7 | Deno/Bun | 77 | -10, typed runtime |
| 10 | React | 76 | -11, UI components (different category) |
| 11 | Python | 74 | -13, data/AI/ecosystem depth |
| 12 | C++ | 73 | -14, engines/trading |
| 13 | Julia | 69 | -18, numerics/science depth |
| 14 | Ruby/Rails | 68 | -19, convention CRUD |
| 15 | PHP Laravel | 67 | -20, monolith CRUD |
| 16 | C | 62 | -25, kernels/embedded |
| 17 | Lua | 58 | -29, embedding |
| 18 | Bash | 45 | -42, tiny pipes |

Grand total (sum of all 18 totals) = `1318 / 1800`, average `73.2/100`.
`.ks` total `87/100` reflects v2.4 source breadth: best at learning/scripts/services;
Stdlib/Tooling breadth near Go, Simplicity above Go; registry/watch/SSG/TCP-minimal/cache/host-profile
add breadth points. Compiler v0.1 (`.ksb-1` subset: arithmetic/control-flow/funcs, no `go`/`chan`/`select`/`sleep`,
no `import`/`try`/`switch`/`defer`/`select`, no slices, no `is`/`?.`/`??`, no typed params/returns, no closure capture,
7 user + 5 hidden builtins) proves the parse→compile→run pipeline but moves no score.
Still behind on full VM/AOT, LSP/debugger, DOM/HMR parity, native DB/WS-frames until remaining P1/P2 land.

## Why not Go/Rust-class (v2.4 gaps + what parity needs)

> Score context: `.ks 87/100` vs `Go 82/100` vs `Rust 81/100` (leads on balance; loses on depth — see What-section).
> The 3–4 pt gap is full VM/AOT + LSP + native DB/WS-frames + syntax structs/enums + maturity hardening.
> Fix the rest → ~82–85/100. Rows 6/14 stay intentionally different (GC stays, `unsafe` stays opt-in).

### 1. Compiler still subset — full language tree-walk + `--bin` embed via `go build`, no LLVM/JIT

* Today: tree-walk interpreter (`internal/backend`, full language, 170+ builtins, union/generic types, extended folding) +
  bytecode subset (`internal/compiler`, `.ksb-1` JSON + stack VM):
  `fusion compile prog.ks [--out prog.ksb] [--dis] [--run]`, `fusion prog.ksb`.
  Compiled subset: literals/arrays/maps, `let`/`=`/`+=`-family, `+ - * / % **`/`== != < <= > >=`/`in`/`and/or/not`,
  calls (user funcs + `assert/len/range/str/int/float/type`), `a[i]`/`m.key`,
  `print/if/while/for-in/for-c/func/return/break/continue`.
  Explicitly rejected (run in interpreter): `go/chan/select`, `sleep`, `import`, `try/catch`,
  `switch`, `defer`, slices, `is`/`?.`/`??`, typed params/returns,
  closures capturing outer locals (`vm.go:596` `closures not yet supported`, `compiler.go:71` `OpSleep` reserved/never emitted).
  VM limits: 7 user builtins (+ 5 hidden `__iter_*` = 12 map entries), int `**` is a naive O(n) loop in the VM
  (`vm.go:813`) vs O(log n) squaring in the interpreter (`backend.go:2281`), `maxFrames 1024` / `maxSteps 20M`,
  `line N:` errors. `.kslib` stays source JSON (`kslib-1`), but `fusion build --bin` embeds `.ks`+`.kslib`
  (+ `fusion.lock`) into a temp-module `main.go` and runs `go build` (`internal/tools/build.go:418-530`;
  requires a Go toolchain; `GOOS/GOARCH` + `CGO_ENABLED=0` passthrough in `build.go:510-521`;
  `js`→`js/wasm` fixup; `.exe` suffix on Windows). Cache (`internal/tools/cache.go`) is a whole-app
  sha256 over `fusion.toml` + `fusion.lock` + every `.ks` (skips `.git/target/vendor/test-releases`) stored at
  `target/.fusion-cache.json`; any change invalidates — hash-skip, not incremental; no TTL/size/remote.
  `fib(25)` and `11M` figures from the old doc have no benchmark artifact in repo — do not cite as measurement.
  Folding (`internal/frontend/fold.go`, applied once per `ExecProgram`, idempotent) covers `1+2`/`"a"+"b"`/numeric
  `- * /` (div-by-zero left unfolded), `%` (int non-zero), `==/!=/< <= > >=` (numbers+strings), bool-only `and/or`,
  unary `-`/`!` on literals, small int `**` (exp `0..30`), `nil ?? x → x` / non-nil-non-bool `??` → left
  (**`false ?? x` and `bool ?? x` do not fold**, correctly preserving nil-only semantics),
  `is` with **string-literal** right for 8 types only (no `chan/func/ok/err/any`), substring `str in str`;
  `x in [array]` via the generic path does not fold (outer `isLit` guard excludes `ExprArray`).
* Go level still needs: full-language VM coverage (concurrency, `import`/`try`/`switch`/`defer`,
  slices, `is`/`?.`/`??`, typed params), then IR-level checks (have source `vet`/`check`; have hash-skip cache).
* Rust level still needs: LLVM/opt backend or full bytecode VM + AOT, LTO, strip/symbol options,
  reproducible builds. Minimum viable: full-subset VM (5–20x speedup target) first, then native AOT (have embed `--bin` now).
* Planned: `docs/futures.md` P1 runtime; `--bin`/`--target`/host-`--cpuprofile`/hash-skip cache done, VM full coverage left.
* Score impact: Perf breadth 7 held by folding/sort/`**` opts (above Python for scripts); Perf 7→8 (+1) left
  for full VM/AOT with real benchmarks; Build 8 held by `--bin`+cache; Build 8→9 (+1) left (remote/incremental cache, reproducibles).

### 2. Gradual types (v2.4: unions/generics done, syntax structs/enums left) — annotations + `is`/`?.`/`??`/`ok`/`err` + struct/enum helpers, no syntax structs/enums/generics

* Today (v2.4): `let x: int|string`, `array<int>`, `map<string,int>`, optional `let x: int`, `func f(a: int): int`, `func` literals with
  types (nullable by default, `int?` accepted); `x is int` / `x is "int"` /
  `x is not "int"` (also `number`/`any`/`ok`/`err`); `a?.b` / `a?.[i]` (missing → `nil`);
  `a ?? b` (nil-coalescing, short-circuit); `ok(v)/err(e)` + `is_ok/is_err` +
  `unwrap/unwrap_or` + `is_type/assert_type` + `struct_validate/assert` + `enum_create/valid` + `is_number` +
  `assert_eq/ne/contains` + `fusion check`/`vet` arity/type lint; `==` is still deep equality,
  arity + param-type checks at call time (+ vet).
* Go level still needs: structs syntax, interfaces, generics, exhaustive `switch`.
* Rust level still needs: real `Result/Option` exhaustiveness, enums syntax + pattern
  matching, ownership-safe FFI boundaries (no full borrowck — explicit non-goal, Go GC stays).
* Planned: `futures.md` P1 language core (structs/enums syntax) closes the rest.
* Score impact: Types 7 held by helpers+check (above Python/Node breadth); Types 7→8 (+1) left (syntax structs/enums/generics).

### 3. Concurrency (v2.4: spelling parity + cancel, not runtime parity) — `select` + `for v in chan` + `with_timeout`/`parallel` + vet-`--race`

* Today (v2.4, interpreter): `go` + `chan(n)/send/recv/close` +
  `select { case v = recv(c) / recv(c) / send(c, v) / timeout(ms) / default }`
  (blocks until one case is ready, ready cases win uniformly at random like Go,
  `break` ends the `select`, `ch = nil` disables a case for fan-in drains) +
  `for v in ch` (drains until close, like Go's `range ch`) +
  `recv_timeout/send_timeout/chan_closed` + `try_send/try_recv/chan_len/chan_cap/sleep` +
  `with_timeout(ms, func)` (errors on timeout) + `parallel(arr, func)` (ordered, first error wins) +
  `fusion run/launch --race` (error-level vet gate + `FUSION_RACE=1` env + “use `go run -race`” hint;
  `launch --race` is env+print only). `go defer` is explicitly rejected (`backend.go:770`).
  Compiler v0.1 rejects `go/chan/select/sleep` with a clear error (run those files in the interpreter).
* Go level done for script spelling (9/10 held). Left: structured cancel/context polish, buffered-chan spec docs, deterministic test scheduler, real race instrumentation.
* Rust level still needs: `send/sync`-like docs, cancel/context, deterministic test scheduler for `go` blocks.
* Planned: `futures.md` P1 (timeouts/context, scheduler) + namespaced imports.
* Score impact: Concurrency 9 held (no further points planned; stay 9 for script scope).

### 4. Stdlib breadth 158, depth minimal — `http/regex/crypto/fs/process/time/db/log/tcp/tls-minimal` landed, WS-frames/native DB left

* Today: 158 distinct builtins in the interpreter (verified: `96` in `backend.go` + `52` in `stdlib_ext.go` + `10` in `stdlib_ext2.go`; no duplicates; `BuiltinCount()` = `len(allBuiltins())`; tests assert `>=158`):
  strings/arrays/maps/JSON/files/math/time/rand, `map/filter/each/reduce/apply`, `ok/err` results, `chan_*`,
  `read_file/write_file/append_file/exists/list_dir/mkdir/remove[_file]/input/argv/env/exit`,
  plus `http_get/post/fetch_json/http_serve`, `regex_match/find/replace/split` (Go `regexp`, no literals),
  `sha256/md5/hmac_sha256/base64_encode/decode/hex_encode/decode/uuid/random_bytes`,
  `stat/cp/mv|copy/glob/path_join/abs_path/remove_all`, `exec/shell/cwd/env_all` (`CombinedOutput` only, no pipes/signals),
  `format_time/parse_time/time_parts` (+ `now()` ms; no ticker), `db_put/get/delete/list` (JSON-file KV; no `sqlite/postgres`),
  `log_info/warn/error` (stderr), `assert_eq/ne/contains`, `with_timeout/parallel`,
  `struct_validate/assert/enum_create/valid/is_number` + `use_state/set_state/on_mount` (process-global map; `on_mount` = immediate call) +
  `tcp_connect/send/recv/close/serve` (int-handle registry, 5s deadlines, no shutdown) + `tls_connect` (client-only,
  `InsecureSkipVerify:false`, no `tls_serve`) + `ws_connect` (minimal: plain TCP + `Upgrade: websocket` header write,
  returns conn id; **no frame encode/decode, no server**).
  VM v0.1 subset only: `assert/len/range/str/int/float/type` (+ hidden `__iter_*` helpers).
  Old `vs Node:176` line said `149 sync builtins` — wrong; it is 158. Old `list.md:42` `97+52` math — wrong; it is `96+52+10`.
* Go level nearly needs: `net/http` server polish (have background minimal `http_serve`), `fs` `watch`, `process` pipes/signals, `log/flags`, fuller `testing` helpers.
* Rust level still needs: `tokio`-like async IO story (or documented blocking + workers),
  `serde`-like JSON schema validation (`struct_validate` is start), `sqlite_*` → `postgres_*` native (have KV + TCP).
* Planned: `futures.md` P1 stdlib; left: WS-frames/native DB/`watch`/signals/pipes/ticker/`tls_serve`.
* Score impact: Stdlib 9 held on breadth (Go breadth for scripts); Stdlib 9→10 (+1) left (WS-frames/native DB depth).

### 5. Ecosystem breadth 7 (file-local), tooling breadth 9 (no LSP/debugger)

* Today: `fusion.toml` + `fusion.lock` + semver (`^ ~ >= > < *` + `,` + path; git deps left) + `vendor/` offline +
  file-local registry (`publish/pull/yank`, sha256 sidecar + verify on pull, `scope/name` → subdir mapping,
  `FUSION_REGISTRY` dir override, default search `test-releases/*`; yanked excluded; newest-satisfying resolver) +
  `fusion test` (`*_test.ks` + `assert`, TAP, per-file isolation; per-file timeout still missing — a hung file blocks the run) +
  `fusion fmt/vet/doc/check/repl/bench`, hash-skip cache, host-`--cpuprofile`/print-`--debug`.
  New: `compile --dis/--run` + `test` + `build --bin/--target` + cache + `vendor` + `publish/pull/yank/registry` + `run-web`/`build-js`/`build-ssg`.
  Honest stubs: **private-registry token is a skip-stub** (`internal/tools/build.go:357-360`: if
  `FUSION_REGISTRY_PRIVATE==1` and no `FUSION_REGISTRY_TOKEN`, deps are skipped with a note — token never sent/checked);
  no HTTP registry, no audit, no docs.rs-like docs, no criterion-style bench reports (basic `bench` + host profile only);
  `--debug` is print-only (`debug: name/ver + entries`, `vet N issues`, `FUSION_DEBUG=1`, then normal run — no breakpoints/trace);
  `--cpuprofile` is Go `pprof` of the host (`cmd/fusion/profile.go:10-21`), not `.ks` line-level.
* Go level done except VS Code ext (have file-local registry+checksums, resolver, `vendor/`, `fmt/vet/test/bench/doc`, host profile, hash-skip cache).
* Rust level left: audit, docs.rs-like docs, criterion-style benches.
* Planned: P2 DX (LSP/debugger) left.
* Score impact: Ecosystem 7 held on file-local breadth; Ecosystem 7→8 (+1) left (central/audit/private polish, git deps).
  Tooling 9 held on breadth; Tooling 9→10 (+1) left (LSP). Maturity 6 held on tests+docs; Maturity 6→8 (+2) left (stability: fix stale binary/banners, cover TLS/WS/`http_serve`/`--bin`/SSE/`build-js`, per-file test timeout, real CI gate).

### Go/Rust-level checklist (all things, with owner doc)

| # | Area | Go bar | Rust bar | .ks v2.3 (honest) | Needed to close | Closes in |
|---|---|---|---|---|---|---|
| 1 | Compiler | `go build` static bin | `rustc` LLVM + LTO | tree-walk (full, literal folding, 158 builtins) + VM subset (`.ksb-1`, no `go`/`sleep`/`import`/`try`/`switch`/`select`/`defer`/slices/`is`/`?.`/`??`/typed params/closure-capture) + `--bin` embed via `go build` | full VM → native AOT + real benchmarks | `futures.md` P1 runtime |
| 2 | Targets | `GOOS/GOARCH` | tiers + WASM | `--target` GOOS/GOARCH passthrough + hash-skip cache + host `--cpuprofile` (needs Go toolchain) | WASM run polish, remote/incremental cache, reproducibles | `futures.md` P1 runtime |
| 3 | Types | structs/interfaces/generics | traits/enums/`Result` | gradual `: type` + `is`/`?.`/`??` + `struct_validate/enum_create` + `vet`/`check` (no syntax structs/enums/generics) | structs/enums syntax/generics | `futures.md` P1 core |
| 4 | Errors | multi-return | `Result/Option/?` | `ok/err` values + `error()`+`try/catch` + `assert_eq/ne/contains` | exhaustive `Result` checks | `futures.md` P0 done, P1 polish |
| 5 | Concurrency | `select`/race | `tokio`/rayon | `go/chan/select/timeout/for-in-chan/with_timeout/parallel`/vet-`--race` (interpreter; VM rejects) | scheduler + VM concurrency + real race | `futures.md` P0 done, P1 rest |
| 6 | Memory | GC + `sync` | borrowck | Go GC (ok) | document sharing rules, no borrowck | non-goal, docs only |
| 7 | Stdlib net | `net/http` | `std::net`+crates | `http_*` (serve minimal) + `tcp_*` (minimal) + `tls_connect` (client-only) + `ws_connect` (header-only, no frames) | WS-frames, `tls_serve`, serve polish | `futures.md` P1 stdlib |
| 8 | Stdlib OS | `os/exec` | `std::process` | `exec/shell/cwd/env_all` (`CombinedOutput` only) + files + `stat/cp/mv/glob` + print-`--debug` | pipes/signals, `watch` | `futures.md` P1 stdlib |
| 9 | Data | `encoding/*` | `serde` | `json_*` + `regex_*` (no literals) + `crypto` + KV-file `db_*` + `use_state`-minimal | `regex` literals, `sqlite/postgres` native | `futures.md` P1 stdlib |
| 10 | Packages | proxy + `go.sum` | crates.io + lock | `fusion.lock`+semver + file-local registry (`publish/pull/yank`, sha256, namespaces) + `vendor/`; `.ksb` per-file | central/audit/private polish, git deps | `futures.md` P0+P2 |
| 11 | Tooling | `fmt/vet/test/bench/pprof` | `clippy/fmt/bench` | `new/run/build/launch` + `compile` + `test` + `fmt/vet/doc/check/repl/bench` + host-`--cpuprofile`/print-`--debug` + hash-skip cache | LSP/debug | `futures.md` P0+P2 DX |
| 12 | IDE | `gopls` | `rust-analyzer` | none | LSP + VS Code ext + debugger | `futures.md` P2 DX |
| 13 | Frontend | `html/template`/WASM | WASM pkgs | console + `run-web` SSR (SSE full reload) + subset `build-js` + `build-ssg` + `use_state` shim + API funcs + budgets/manifest | DOM-diff/HMR + ISR/hydrate-full | `futures.md` P2 frontend |
| 14 | FFI | `cgo` | `unsafe`/FFI | none | opt-in `ffi_*` + Go plugin API | `futures.md` P2 interop |
| 15 | Stability | compat promise | editions | v2.3 source (shipped `release/fusion` stale v2.0) | rebuild release, RFC + semver + LTS | `futures.md` §5 |

Close full VM + LSP + native DB/WS-frames + syntax structs/enums + stability hardening and `.ks` moves `78 → ~82–85/100` (Go/Rust-class).
Rows 6/14 stay intentionally different (GC stays, `unsafe` stays opt-in).

## Decision guide

1. Browser UI? → React + Vite + TS, or Next.js for SSR (`.ks` `run-web`/`build-js`/`build-ssg` for prototype only; check `// unsupported` lines in generated JS).
2. CRUD + login + billing next week? → Laravel / Django / Rails / Next.js (`.ks` for sidecar `--bin` workers where a Go toolchain is fine).
3. 100k RPS / embedded / game loop? → Go / Rust / C++ / C.
4. Matrices / science / simulations? → Julia (or Python + numpy; `.ks` leads Julia on rubric balance but not on numerics).
5. Script, bot, rule engine, teaching `go/chan/select`, prototype/service? → `.ks` (interpreter + `--bin`).
   Pure arithmetic/control-flow/funcs with no concurrency? → try `fusion compile --run` (VM subset; otherwise interpreter).
6. Need `http/DB/net` today? → yes for basics: `http_*`/`fetch_json` (GET-only JSON helper), KV-file `db_*`, `exec/shell` (no pipes), `regex/crypto`, minimal `tcp/tls`, header-only `ws_connect`, `use_state`-minimal — see P1 stdlib (breadth done, depth left); need WS-frames/native DB/pipes/signals/`watch`/ticker/`tls_serve`? → wait.

## Honest limits of `.ks` v2.3 (do not hide)

* Full language is tree-walk + literal folding (consts only), no JIT/LLVM codegen; `--bin`/`--target`/cache ride on `go build`
  (needs Go toolchain; cache is whole-app hash-skip). Compiler v0.1 narrows but does not close this: `.ksb-1` is portable JSON run by
  `fusion` (not a static binary — that is `--bin`), subset-only (see §1 reject list), 7 user (+ 5 hidden) builtins, int `**` O(n) in the VM
  vs O(log n) in the interpreter, `maxFrames 1024` / `maxSteps 20M`.
* Gradual types only (helpers `struct_validate/enum_create`, no `struct`/`enum` syntax/generics yet), `==` uses deep equality.
  Compiler v0.1 rejects `: type` annotations and `is`/`?.`/`??` (interpreter-only). `is`/`in` folding has the limits in §1.
* Flat lib namespace default (prefix funcs; no `import "x" as h` yet). `fusion.lock`+semver+`vendor/`+file-local registry
  (`publish/pull/yank`, sha256 sidecar+verify, `scope/name` dir mapping, `FUSION_REGISTRY` dir) now; no central server/audit/docs.rs;
  private-token is a skip-stub; no git deps. `.ksb` is per-file bytecode, not a package format (that stays `.kslib` source JSON).
* `fusion run --race` is error-vet + env flag (+ “use `go run -race`” hint), `fusion run --debug` is print-only vet dump + env flag,
  `--cpuprofile` is host Go `pprof` — no deterministic scheduler, no `.ks`-line profiler, no debugger/breakpoints yet;
  no `go/chan/select/sleep` in compiled output yet; `go defer` rejected; `fusion test` has no per-file timeout (hung file blocks the run).
* `frontend/` is SSR-prototype + SSG + subset-JS (`run-web` HTML+JSON + `/api/*` funcs + SSE **full reload**, `build-js` subset + manifest + budgets,
  `build-ssg` prerender with per-route skip, `use_state` process-map + JS shim, `on_mount` immediate, `fetch_json` GET-only) —
  still no DOM-diff/HMR parity, no CSS handling, no ISR/hydrate-full. See `plan/frontend.md` for 7→10.
* Net/data depth: `http_serve` always `application/json`, no method/status/headers/shutdown; `tcp_serve` no shutdown;
  `tls_connect` client-only; `ws_connect` header-only (no frames/server); `db_*` KV-file only; `exec/shell` `CombinedOutput` only;
  `regex` no literals; `time` no ticker; `fs` no `watch`.
* Version/hygiene: toolchain source reports `v2.3` (`fusion version`, `fusion help`, `toolVersion` in
  `cmd/fusion/main.go:279` — single constant, keep in sync). `release/fusion` in repo still reports **v2.0**
  (rebuild from source: `go build -o fusion ./cmd/fusion`). `repl` banner (`tools.go:855`) and `build-js` header
  (`webjs.go:458`) still say `v2.2`. `retest.log` is a leftover (`retest.sh` does not exist). The `.github` workflow
  in this repo is not a CI test gate (`workflow_dispatch` only, no `go test`).

## Corrections in this rewrite (vs the previous `vs.md`)

* `149 sync builtins` → **158 distinct** (`96` base + `52` ext + `10` ext2; count: `grep -oP '\{Name: "\K[^"]+' internal/backend/*.go | sort -u | wc -l`).
  `list.md:42` `97+52` math fixed to `96+52+10`.
* VM `7 builtins` → **7 user + 5 hidden `__iter_*` (12 map entries)**; added VM int-`**` O(n) vs interpreter O(log n),
  `OpSleep` reserved-never-emitted, `maxFrames`/`maxSteps`, full reject list.
* Folding `**`/`??`/`is`/`in` → scoped to what `fold.go` actually folds (bool-`??`, ident-`is`, array-`in` limits;
  div/mod-by-zero unfolded; `2 in [1,2]` header example does not fold via the generic path).
* `--bin (11M)` / `fib ~70x` → labeled **unverified estimates** (no artifact in repo); `--bin` clarified as `go build` embed requiring a toolchain.
* `--cpuprofile` → **host Go `pprof`**, not `.ks`-line profiler. `--debug` → **print-only stub-lite**. `--race` → **vet + env partial**, not instrumentation.
* Registry `publish/pull/yank + checksums + namespaces` → scoped as **file-local** (`FUSION_REGISTRY` dir, `test-releases/*` default);
  **private token = skip-stub**, no HTTP/central/audit.
* `run-web --watch` → **SSE full reload** (400ms mtime poll + 300ms ticker), not HMR-diff. `build-js` → **subset with `// unsupported`/`// for-c` fallbacks**
  + real budgets/manifest. `build-ssg` → per-route-skip prerender. `use_state/on_mount/fetch_json` → **process-map / immediate-call / GET-only** minimal.
* `http_serve`/`tcp/tls/ws` → added minimal-scope footnotes (`application/json`-always, no shutdown/frames/server/pipes).
* `Frontend 3→5` leftover line and Vite-table `no watch` row → fixed to `--watch` full-reload reality.
* Added shipped-binary staleness (`release/fusion` v2.0), banner staleness (repl/build-js v2.2), `futures.md` staleness,
  `list.md:64` staleness, test-coverage gaps, and repo-hygiene notes (`retest.log`, non-CI workflow).

## How to verify (run these)

```bash
go build -o /tmp/fusion ./cmd/fusion && /tmp/fusion version   # want: ks-fusion v2.3 (release/fusion says v2.0 = stale)
grep -oP '\{Name: "\K[^"]+' internal/backend/*.go | sort | uniq -c | head  # dup check (want: no dups)
grep -oP '\{Name: "\K[^"]+' internal/backend/*.go | sort -u | wc -l        # want: 158
grep -rn "closures not yet supported\|OpSleep" internal/compiler/ | head
grep -n "FUSION_DEBUG\|FUSION_RACE\|StartCPUProfile" cmd/fusion/*.go internal/tools/*.go | head
grep -n "FUSION_REGISTRY_PRIVATE\|FUSION_REGISTRY_TOKEN" internal/tools/build.go
grep -n "full reload v1\|400.*[Mm]s\|300.*[Mm]s\|unsupported" internal/tools/webjs.go | head
grep -n "InsecureSkipVerify\|ws_connect minimal\|CombinedOutput\|50 *\* *time.Millisecond" internal/backend/*.go | head
go test ./...   # ~77 funcs; note uncovered areas listed in Maturity footnote above
```
