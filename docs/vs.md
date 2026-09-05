# ks-fusion vs Others

> ks-fusion `v2.1`: gradual-typed interpreted `.ks` language, toolchain written in Go.
> Easy like Python, concurrency like Go, packaging like Rust.
> This doc is honest about where `.ks` wins and where it loses.

## TL;DR

| If you need… | Pick… | Why not `.ks` yet |
|---|---|---|
| Single static binary, max RPS, strict types | Go / Rust | `.ks` is tree-walk interpreted, gradual-typed, ~5x slower on `fib(25)` |
| Kernel, drivers, games, hard realtime | C / C++ / Rust | No manual memory, no pointers, no SIMD |
| Browser UI / React / SSR | Next.js (TS) | `frontend/main.ks` is console logic today, not DOM |
| CRUD + auth + admin panel tomorrow | PHP Laravel / Python Django | No ORM, migrations, HTTP server stdlib yet |
| Quick scripts, rules, gluing, learning | ks-fusion | — this is the sweet spot |
| npm / PyPI ecosystem | Node.js / Python | `.ks` has ~90 builtins + local `.kslib` only |

## Big table

|  | ks-fusion (.ks) | Go | Rust | C | C++ | Node.js (JS/TS) | Python | Next.js | PHP Laravel |
|---|---|---|---|---|---|---|---|---|---|
| Model | interpreted tree-walk (Go) | compiled, GC | compiled, no GC (borrowck) | compiled, manual | compiled, RAII | V8 JIT | interpreted (+C ext) | React framework on Node | interpreted + framework |
| Typing | gradual: dynamic + `: type` annotations, `is`, `?.`/`??`, `ok`/`err` results | static, interfaces, generics | static, traits, enums, `Result/Option` | weak static, pointers | static, templates, classes | dynamic + optional TS | dynamic + hints | TS-typed components | dynamic |
| Perf | low-medium (scripts, bots, CLIs) | high (servers) | highest (systems) | highest | highest | medium-high (I/O) | medium (glue/AI) | medium (SSR) | medium (CRUD) |
| Concurrency | `go + chan/send/recv/close` (goroutines underneath, no `select` yet) | goroutines + `select` | `async/tokio`, threads | threads, manual | threads/`async` | event loop + workers | threads/GIL + `asyncio` | server/client components | processes + queues |
| Packaging | `fusion.toml` + `.kslib` JSON (`kslib-1`), local `test-releases/`/`target/` | `go.mod` + proxy | `cargo` + crates.io | make/cmake | cmake/vcpkg/conan | npm/pnpm | pip/poetry | npm + Vercel | composer + artisan |
| Binary | needs `fusion` on PATH (shebang), no `--bin` yet | single static binary | single binary | binary | binary | needs node/runtime | needs python | needs node | needs php+server |
| Best for | learning, automation, rules engines, small backends | APIs, DevOps, cloud | systems, WASM, games | OS, embedded | engines, trading, desktop | APIs, realtime, SSR | scripts, data, AI | fullstack React apps | monolith CRUD apps |

See `docs/futures.md` for what closes each gap (`--bin`, `select`, `http_*`, registry, `--js`).

## How scoring works (out of 100)

Each language / stack is scored `0-10` on 10 dimensions = `100` max.
Snapshot for ks-fusion `v2.1`. Higher = better, except simplicity where
easier = higher. Scores are opinionated but rubric-based, not benchmarks.

Dimensions: `Perf + Types + Concurrency + Stdlib + Ecosystem + Tooling +
Simplicity + Build/Deploy + Frontend + Maturity = 100`.

## Scores — total out of 100

| Dim (max 10) | .ks | Go | Rust | C | C++ | Node | Python | Next.js | Laravel |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| Perf | 4 | 8 | 10 | 10 | 10 | 7 | 5 | 7 | 5 |
| Types | 6 | 8 | 10 | 5 | 8 | 6 | 6 | 8 | 5 |
| Concurrency | 6 | 9 | 8 | 5 | 7 | 7 | 5 | 6 | 4 |
| Stdlib | 4 | 9 | 8 | 6 | 8 | 8 | 10 | 8 | 8 |
| Ecosystem | 3 | 8 | 8 | 6 | 7 | 10 | 10 | 10 | 8 |
| Tooling | 4 | 9 | 9 | 7 | 8 | 9 | 8 | 9 | 8 |
| Simplicity | 9 | 7 | 5 | 4 | 4 | 7 | 10 | 6 | 8 |
| Build/Deploy | 4 | 10 | 9 | 8 | 8 | 6 | 5 | 7 | 6 |
| Frontend | 3 | 5 | 6 | 2 | 4 | 8 | 5 | 10 | 7 |
| Maturity | 3 | 9 | 8 | 9 | 9 | 9 | 10 | 8 | 8 |
| **Total /100** | **46** | **82** | **81** | **62** | **73** | **77** | **74** | **79** | **67** |

Extra stacks (same rubric): `TypeScript 79`, `Java/Kotlin/Spring 78`,
`Vite 77`, `Deno/Bun 77`, `React 76`, `Lua 58`, `Ruby/Rails 68`, `Bash 45`.
Details in `More` below. Frontend breakdown:

| Dim (max 10) | .ks | TypeScript | React | Vite | Next.js |
|---|---:|---:|---:|---:|---:|
| Perf | 4 | 6 | 7 | 8 | 7 |
| Types | 6 | 9 | 8 | 6 | 8 |
| Concurrency | 6 | 6 | 5 | 4 | 6 |
| Stdlib | 4 | 7 | 5 | 4 | 8 |
| Ecosystem | 3 | 10 | 10 | 9 | 10 |
| Tooling | 4 | 9 | 9 | 10 | 9 |
| Simplicity | 9 | 6 | 6 | 8 | 6 |
| Build/Deploy | 4 | 7 | 7 | 10 | 7 |
| Frontend | 3 | 10 | 10 | 10 | 10 |
| Maturity | 3 | 9 | 9 | 8 | 8 |
| **Total /100** | **46** | **79** | **76** | **77** | **79** |

## Language-by-language

### vs Go (implementation language)

**Score: ks-fusion 46/100 vs Go 82/100 — Go wins by 36.**

Same ideas: `go func(){...}()`, `chan(1)`, `send/recv/close`, `defer` LIFO.
Difference: Go is compiled + statically typed; `.ks` is interpreted + gradual-typed
(optional `: type` annotations checked at runtime, `is`/`?.`/`??`).

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

Pick Go for prod servers, strict APIs, single binary.
Pick `.ks` for shorter scripts with Go-flavored concurrency and no compile step.

### vs Rust

**Score: ks-fusion 46/100 vs Rust 81/100 — Rust wins by 35.**

Rust gives ownership, `Result/Option`, `cargo` registry, zero-cost abstractions.
`.ks` copies the `cargo` UX (`fusion new --lib`, `fusion build --release`,
`test-releases/` like `target/release/`) but bundles are source JSON, imports are
flat globals, errors are `ok(v)/err(e)` values + `error(msg)` + `try/catch`.

Pick Rust for perf-critical, safety-critical, WASM libs.
Pick `.ks` for Day-1 productivity without borrow checker.

### vs C

**Score: ks-fusion 46/100 vs C 62/100 — C wins by 16.**

C gives pointers, manual `malloc/free`, direct syscalls, tiny runtimes.
`.ks` gives `array/map/string` + GC (from Go) + bounds-checked indexing.

Pick C for kernels, drivers, microcontrollers.
Pick `.ks` for everything where `segfault` is unacceptable.

### vs C++

**Score: ks-fusion 46/100 vs C++ 73/100 — C++ wins by 27.**

C++ gives RAII, templates, classes, deterministic destruction, huge game/engine libs.
`.ks` gives `func` closures + `defer` + duck-typed maps instead of classes.

Pick C++ for engines, CAD, HFT, Unreal/Qt.
Pick `.ks` for config-driven logic on top of those engines.

### vs Node.js

**Score: ks-fusion 46/100 vs Node.js 77/100 — Node wins by 31.**

Node gives V8, `npm` (2M+ packages), `fetch/http`, event loop, TypeScript.
`.ks` gives simpler blocking `recv/sleep` + ~90 sync builtins, no `http_*` yet.

```js
// Node
const r = await fetch(url).then(r => r.json());
```

```python
# .ks today: files + json only, no http yet (see futures.md)
let raw = read_file("data.json")
let data = json_parse(raw)
```

Pick Node for web APIs, realtime, npm deps.
Pick `.ks` for small deterministic scripts without `node_modules`.

### vs Python

**Score: ks-fusion 46/100 vs Python 74/100 — Python wins by 28.**

Closest feel: `let x = 10`, `for i in range(5)`, `a[1:3]`, `and/or/not`,
truthiness (`nil false 0 0.0 "" [] {}` falsy), `map/filter/reduce`.
Difference: `.ks` adds `go/chan/defer/switch`, gradual `: type` annotations,
`is`/`?.`/`??` and braces; Python has huge stdlib
(`requests`, `numpy`, `django`) while `.ks` has files/JSON/strings only.

Pick Python for data/AI/science/ops (ecosystem wins).
Pick `.ks` for learning concurrency early or embedding a tiny Go-based runtime.

### vs Next.js (framework, not language)

**Score: ks-fusion 46/100 vs Next.js 79/100 — Next.js wins by 33 (different category).**

Category error if compared 1:1. Next.js = React + routing + SSR/ISR + Node runtime.
ks-fusion app = `backend/main.ks + frontend/main.ks` run concurrently in console.

* Today: use Next.js for real browser UI. Use `.ks` backend as JSON worker
  (`read_file` → `json_stringify` → stdout) called from an API route.
* Future (`docs/futures.md`): `fusion build --js` subset + `fusion run --web` hot reload.

Pick Next.js for SEO sites, dashboards, SaaS UI.
Pick `.ks` for the logic worker behind it.

### vs TypeScript (language, not runtime)

**Score: ks-fusion 46/100 vs TypeScript 79/100 — TS wins by 33.**

TypeScript = JS + static types (`tsc`, `strict`, generics, unions).
`.ks` = gradual types (dynamic by default, optional `: type` runtime checks,
`is` narrowing, `?.`/`??` nil-safety, `ok`/`err` results).

```ts
// TypeScript
function add(a: number, b: number): number { return a + b; }
type User = { name: string; age: number };
```

```python
# .ks v2.1 — annotations are runtime-checked (nil passes as nullable)
func add(a: int, b: int): int { return a + b }
let user = {name: "ada", age: 36}
assert(user is map)
assert(user?.name ?? "anon" == "ada")
let r = ok(1)
assert(r is ok)
```

Pick TypeScript for any browser/Node code that must scale past 1k lines.
Pick `.ks` for non-JS glue where `tsc` + `node_modules` is overkill.
Interop future: `fusion build --js` subset → import `.ks` logic into TS.

### vs React (UI library)

**Score: ks-fusion 44/100 vs React 76/100 — React wins by 32 (different category).**

React = components, hooks, virtual DOM, concurrent renderer.
`.ks` `frontend/main.ks` = console `print`, no DOM/state/effects.

```jsx
// React
function Hello({name}) { return <h1>Hello {name}</h1>; }
```

```python
# .ks frontend today
let title = "Hello from ks-fusion"
print title
```

Pick React (+ Vite/Next.js) for all real UI.
Pick `.ks` for the worker behind the UI (JSON over stdout/file, later `http_*`).
Do not reimplement React in `.ks` — explicit non-goal in `futures.md`.

### vs Vite (frontend build tool)

**Score: ks-fusion 44/100 vs Vite 77/100 — Vite wins by 33 (different category).**

Vite = instant HMR dev server + `esbuild`/Rollup bundler + plugin ecosystem.
`fusion` = `new/run/build` for `.ks` only, no HMR, no bundling, no CSS/DOM.

|  | Vite | `fusion` (v2.0) |
|---|---|---|
| Dev | HMR <100ms | `run` rerun, no watch |
| Build | tree-shaken JS/CSS | source JSON `.kslib` / parse-check |
| Plugins | 1000s (React, TS, Tailwind) | none yet |
| Target | browser | console interpreter |

Pick Vite for React/TS/Tailwind frontends.
Pick `.ks` for logic; future `fusion run --web` will copy the HMR idea, and
`fusion build --js` is meant to emit a Vite-consumable module.

### vs PHP Laravel

**Score: ks-fusion 44/100 vs Laravel 67/100 — Laravel wins by 23.**

Laravel gives routing, ORM/Eloquent, migrations, Blade, queues, auth scaffolding.
`.ks` gives none of that yet — no HTTP server, no DB driver, no templates.

Pick Laravel for monolith CRUD + admin + auth in hours.
Pick `.ks` for sidecar scripts (data munging, checks, bots) next to Laravel.

### More (short, all scored out of 100)

* **Deno/Bun 77/100 vs .ks 44/100 (-33):** see TypeScript above; pick for secure TS sandbox / fast runtime; `.ks` is simpler but smaller.
* **Java/Kotlin/Spring 78/100 vs .ks 44/100 (-34):** pick for enterprise monoliths, JPA, strict OOP; `.ks` for glue.
* **Lua 58/100 vs .ks 44/100 (-14):** closest embed rival — Lua is smaller/faster to embed; `.ks` has Go chans + `fusion` CLI out of box.
* **Ruby/Rails 68/100 vs .ks 44/100 (-24):** pick for convention CRUD; `.ks` syntax will feel familiar.
* **Bash 45/100 vs .ks 44/100 (-1):** almost tied — pick Bash for 5-line pipes; `.ks` wins past 50 lines (`try/catch`, maps, JSON, Windows portability).

## Totals & ranking (out of 100)

| Rank | Stack | Total /100 | Verdict vs .ks (44) |
|---:|---|---:|---|
| 1 | Go | 82 | +38, prod servers / single binary |
| 2 | Rust | 81 | +37, systems / safety |
| 3 | Next.js | 79 | +35, browser UI (different category) |
| 3 | TypeScript | 79 | +35, typed UI/logic |
| 5 | Java/Kotlin/Spring | 78 | +34, enterprise |
| 6 | Node.js | 77 | +33, APIs / npm |
| 6 | Vite | 77 | +33, frontend build/HMR (different category) |
| 6 | Deno/Bun | 77 | +33, typed runtime |
| 9 | React | 76 | +32, UI components (different category) |
| 10 | Python | 74 | +30, data/AI/ecosystem |
| 11 | C++ | 73 | +29, engines/trading |
| 12 | Ruby/Rails | 68 | +24, convention CRUD |
| 13 | PHP Laravel | 67 | +23, monolith CRUD |
| 14 | C | 62 | +18, kernels/embedded |
| 15 | Lua | 58 | +14, embedding |
| 16 | Bash | 45 | +1, tiny pipes |
| 17 | **ks-fusion v2.0** | **44** | **baseline — wins on simplicity (9/10)** |

Grand total (sum of all 17 totals) = `1197 / 1700`, average `70.4/100`.
`.ks` total `44/100` reflects v2.0 reality: best at learning/scripts,
behind everywhere else until `futures.md` P0/P1 land.

## Why not Go/Rust-class (v2.0 gaps + what parity needs)

> Score context: `.ks 44/100` vs `Go 82/100` vs `Rust 81/100`.
> The 37–38 pt gap is exactly the 5 blocks below. Fix them → ~75–80/100.

### 1. No compiler — interpreted AST, no native/static binary, no LLVM, no JIT

* Today: tree-walk interpreter (`internal/backend`), `.kslib` = source JSON (`kslib-1`),
  needs `fusion` on PATH. `fib(25)` ~5x slower than Go.
* Go level needs: `fusion build --bin` single static executable, cross-compile
  `--target linux/amd64,arm64,darwin,windows,wasm`, build cache, `go vet`-style IR check.
* Rust level needs: LLVM/opt backend or bytecode VM + AOT, LTO, strip/symbol options,
  reproducible builds. Minimum viable: bytecode VM (5–20x speedup) first, then AOT.
* Planned: `docs/futures.md` P1 runtime (`VM → --bin → --target → --cpuprofile`).
* Score impact: `Perf 4→8 (+4)`, `Build 4→9 (+5)`.

### 2. No static types — only dynamic `nil/bool/int/float/string/array/map/func/chan`

* Today: dynamic only, no structs/enums/generics/traits/borrow-checker,
  `==` is deep equality, arity checked only at call time.
* Go level needs: optional static check — structs, interfaces, generics,
  `nil`-safety (`?.`/`??`), exhaustive `switch`, `vet` for unused/arity.
* Rust level needs: `Result/Option` instead of abort-only `error(msg)`,
  enums + pattern matching, ownership-safe FFI boundaries (no full borrowck —
  explicit non-goal, Go GC stays).
* Planned: `futures.md` P0 error-values + P1 language core.
* Score impact: `Types 4→8 (+4)`.

### 3. Concurrency subset — goroutines underneath, but no `select`, no race detector

* Today: `go + chan(n)/send/recv/close/try_send/try_recv/chan_len/chan_cap/sleep`,
  flat global lib namespace (name collisions across `go` closures).
* Go level needs: `select` + `timeout/default`, `for v in chan`, buffered-chan spec,
  `fusion run --race` (reuse Go race detector), structured workers/timeouts.
* Rust level needs: `send/sync`-like docs, cancel/context (`with_timeout`),
  deterministic test scheduler for `go` blocks.
* Planned: `futures.md` P0 `select`, P1 `chan` iteration + namespaced imports.
* Score impact: `Concurrency 6→9 (+3)`.

### 4. Small stdlib — no `http/net/socket`, no threads/process control

* Today: ~80 builtins (strings/arrays/maps/JSON/files/math/time/rand) — no
  `http_serve/get/post`, TCP/WS, `exec/pipes/signals`, `regex/crypto/db`.
* Go level needs: `net/http` (server + client), `fs` full (`stat/cp/mv/glob/watch`),
  `process.exec`, `time` formatting, `log/flags`, `testing` helpers.
* Rust level needs: `tokio`-like async IO story (or documented blocking + workers),
  `serde`-like JSON schema validation, `sqlite_*` → `postgres_*`, TLS.
* Planned: `futures.md` P1 stdlib list in that exact order.
* Score impact: `Stdlib 4→9 (+5)`.

### 5. No ecosystem — local file search only, no registry/resolver/LSP

* Today: `fusion.toml` + newest local `test-releases/<name>-<ver>.kslib` wins,
  no lockfile, no semver range, no `fmt/vet/test/bench/doc/repl/LSP/debugger/profiler`.
* Go level needs: proxy-style registry + checksums, `fusion.lock`, `^/~ />=` resolver,
  `vendor/`, `fusion fmt/vet/test/bench/doc`, `cpuprofile`, VS Code ext.
* Rust level needs: `cargo publish/yank`, namespaces (`scope/name`), yank + audit,
  docs.rs-like docs, criterion-style benches.
* Planned: `futures.md` P0 tooling + P2 registry/DX.
* Score impact: `Ecosystem 3→8 (+5)`, `Tooling 4→9 (+5)`, `Maturity 3→8 (+5)`.

### Go/Rust-level checklist (all things, with owner doc)

| # | Area | Go bar | Rust bar | .ks v2.0 | Needed to close | Closes in |
|---|---|---|---|---|---|---|
| 1 | Compiler | `go build` static bin | `rustc` LLVM + LTO | tree-walk only | VM → AOT `--bin` | `futures.md` P1 runtime |
| 2 | Targets | `GOOS/GOARCH` | tiers + WASM | none | `--target` matrix + WASM | `futures.md` P1 runtime |
| 3 | Types | structs/interfaces/generics | traits/enums/`Result` | 8 dynamic types | opt-in structs/enums/`Result` | `futures.md` P0+P1 core |
| 4 | Errors | multi-return | `Result/Option/?` | `error()`+`try/catch` | error values, keep `try/catch` | `futures.md` P0 |
| 5 | Concurrency | `select`/race | `tokio`/rayon | `go/chan` only | `select`, `--race`, timeouts | `futures.md` P0+P1 |
| 6 | Memory | GC + `sync` | borrowck | Go GC (ok) | document sharing rules, no borrowck | non-goal, docs only |
| 7 | Stdlib net | `net/http` | `std::net`+crates | none | `http_*`, `net/ws`+TLS | `futures.md` P1 stdlib |
| 8 | Stdlib OS | `os/exec` | `std::process` | `sleep/exit` only | `exec`/pipes/signals, full `fs` | `futures.md` P1 stdlib |
| 9 | Data | `encoding/*` | `serde` | `json_*` only | schema validation, `regex`, `crypto`, `sqlite/postgres` | `futures.md` P1 stdlib |
| 10 | Packages | proxy + `go.sum` | crates.io + lock | local newest-wins | registry + `fusion.lock` + semver + vendor | `futures.md` P0+P2 |
| 11 | Tooling | `fmt/vet/test/bench/pprof` | `clippy/fmt/bench` | `new/run/build` only | `fmt/vet/test/bench/doc/repl` | `futures.md` P0+P2 DX |
| 12 | IDE | `gopls` | `rust-analyzer` | none | LSP + VS Code ext + debugger | `futures.md` P2 DX |
| 13 | Frontend | `html/template`/WASM | WASM pkgs | console `frontend/` | `--web` reload + `--js` subset + React/Vite/Next.js pattern | `futures.md` P2 frontend |
| 14 | FFI | `cgo` | `unsafe`/FFI | none | opt-in `ffi_*` + Go plugin API | `futures.md` P2 interop |
| 15 | Stability | compat promise | editions | v2.0 | RFC process + semver + LTS | `futures.md` §5 |

Close rows 1–5 + 10–11 and `.ks` moves `44 → ~75/100` (Go/Rust-class for scripts/services).
Rows 6/14 stay intentionally different (GC stays, `unsafe` stays opt-in).

## Decision guide

1. Browser UI? → React + Vite + TS, or Next.js for SSR.
2. CRUD + login + billing next week? → Laravel / Django / Rails / Next.js.
3. 100k RPS / embedded / game loop? → Go / Rust / C++ / C.
4. Script, bot, rule engine, teaching `go/chan`, prototype? → `.ks`.
5. Need `http/DB` in `.ks` today? → shell out or wait — see `docs/futures.md` P1 stdlib.

## Honest limits of `.ks` v2.0 (do not hide)

* Interpreted, no JIT/native binary, no cross-compile matrix.
* Dynamic only, no structs/enums/generics, `==` uses deep equality.
* Flat lib namespace (prefix functions), newest local bundle wins, no lockfile.
* No `select`, no `for v in chan`, no HTTP/WS/DB/regex/crypto stdlib.
* `frontend/` is not web — no DOM, no CSS, no SSR.
