# ks-fusion vs Others

> ks-fusion `v2.3` + compiler `v0.1`: gradual-typed `.ks` language, toolchain written in Go.
> Easy like Python, concurrency like Go, packaging like Rust.
> Interpreter runs the full language (158 builtins, extended folding); `fusion compile` adds a
> portable bytecode subset (`.ksb-1` JSON + stack VM: arithmetic, control flow, funcs).
> `fusion build --bin` emits a single static executable; `fusion fmt/vet/doc/check/repl/bench`,
> `fusion.lock` + semver + `vendor/` + registry (`publish/pull/yank`), `run --race/--debug/--cpuprofile`, `run-web --watch` + `build-js`/`build-ssg`, `use_state`, TCP/TLS, build cache are all real.
> This doc is honest about where `.ks` wins and where it loses.

## TL;DR

| If you need… | Pick… | Why not `.ks` yet |
|---|---|---|
| Single static binary, max RPS, strict types | Go / Rust | `.ks` now has `fusion build --bin` (single static 11M binary, embeds `.ks`+`.kslib`, `--target` matrix) + constant folding; full language still tree-walk, `fib(25)` ~80x slower than Go (was ~100x) |
| Kernel, drivers, games, hard realtime | C / C++ / Rust | No manual memory, no pointers, no SIMD |
| Browser UI / React / SSR | Next.js (TS) | `frontend/` has view-models + `run-web` SSR (HTML+JSON, `/api/*`) + `build-js` per-route JS, hydrate stub — still no DOM/HMR/bundler parity |
| CRUD + auth + admin panel tomorrow | PHP Laravel / Python Django | No ORM, migrations; has `http_get/post`, `db_put/get/list` JSON-file KV, `exec/shell`, but no full HTTP server framework yet |
| Numerical / scientific / matrices | Julia | `.ks` has no vectorized ops, no DataFrames/plots, loop math still slower (folding helps scalar consts only) |
| Quick scripts, rules, gluing, learning | ks-fusion | — this is the sweet spot (now also small services via `--bin`) |
| npm / PyPI ecosystem | Node.js / Python | `.ks` has 149 builtins (interpreter; 7 in VM subset) + local `.kslib`/`.ksb` + `fusion.lock` semver (`^ ~ >=`) + `vendor/` (no central registry yet) |

## Big table

|  | ks-fusion (.ks) | Go | Rust | C | C++ | Node.js (JS/TS) | Python | Julia | Next.js | PHP Laravel |
|---|---|---|---|---|---|---|---|---|---|---|
| Model | tree-walk interpreter (full language, constant folding) + bytecode VM subset v0.1 (`.ksb-1` JSON, `fusion compile`) + `--bin` AOT-ish embed | compiled, GC | compiled, no GC (borrowck) | compiled, manual | compiled, RAII | V8 JIT | interpreted (+C ext) | JIT (LLVM), GC, multiple dispatch | React framework on Node | interpreted + framework |
| Typing | gradual: dynamic + `: type` annotations, `is`, `?.`/`??`, `ok`/`err` results + `struct_validate/assert`, `enum_create/valid`, `is_number`, `vet`/`check` | static, interfaces, generics | static, traits, enums, `Result/Option` | weak static, pointers | static, templates, classes | dynamic + optional TS | dynamic + hints | dynamic + parametric types, multiple dispatch | TS-typed components | dynamic |
| Perf | medium+ (scripts, bots, CLIs, small services; O(n log n) sort, O(log n) pow, lock-free scopes, constant folding; VM subset unrated) | high (servers) | highest (systems) | highest | highest | medium-high (I/O) | medium (glue/AI) | highest (numeric; C-speed loops via JIT) | medium (SSR) | medium (CRUD) |
| Concurrency | `go` + `chan`/`select` (`recv`/`send`/`timeout`/`default`, `for v in chan`, `with_timeout`/`parallel`, `recv/send_timeout`, `--race`, goroutines underneath; interpreter only, VM v0.1 has no `go`/`chan`) | goroutines + `select` | `async/tokio`, threads | threads, manual | threads/`async` | event loop + workers | threads/GIL + `asyncio` | threads + distributed + `async` | server/client components | processes + queues |
| Packaging | `fusion.toml` + `fusion.lock` (semver `^ ~ >=`, `*`) + `.kslib` JSON (`kslib-1`), local `test-releases/`/`target/` + `vendor/` offline; bytecode sidecar `.ksb` JSON (`ksb-1`, subset only) | `go.mod` + proxy | `cargo` + crates.io | make/cmake | cmake/vcpkg/conan | npm/pnpm | pip/poetry | `Pkg` + General registry | npm + Vercel | composer + artisan |
| Binary | `fusion build --bin` single static executable (embeds `.ks`+`.kslib`, runs without `fusion` on PATH) + `--target linux/amd64,arm64,darwin,windows,wasm`; shebang still works | single static binary | single binary | binary | binary | needs node/runtime | needs python | needs julia runtime (PackageCompiler possible, heavy) | needs node | needs php+server |
| Best for | learning, automation, rules engines, small backends/services | APIs, DevOps, cloud | systems, WASM, games | OS, embedded | engines, trading, desktop | APIs, realtime, SSR | scripts, data, AI | numerics, science, matrices | fullstack React apps | monolith CRUD apps |

See `docs/futures.md` for what closes each remaining gap (registry, full VM, LSP).
`fusion compile` (`internal/compiler`, `.ksb-1` + `fusion prog.ksb` + `--dis`/`--run`)
is step one of the `futures.md` P1 runtime plan; v2.2 adds `--bin`/`--target` on top.

## How scoring works (out of 100)

Each language / stack is scored `0-10` on 10 dimensions = `100` max.
Snapshot for ks-fusion `v2.3` (158 builtins, fmt/vet/doc/check/repl/bench, --bin/--target, lock/semver/vendor+registry, --race/--debug/--cpuprofile, run-web --watch/build-js/build-ssg, use_state, TCP/TLS, cache, extended folding). Higher = better, except simplicity where
easier = higher. Scores are opinionated but rubric-based, not benchmarks.
Compiler v0.1 (subset bytecode/VM) still moves no score — see §1; v2.3 gains add registry, watch/SSG/use_state/API, TCP/TLS, extended folding, cache, cpuprofile/debug.

Dimensions: `Perf + Types + Concurrency + Stdlib + Ecosystem + Tooling +
Simplicity + Build/Deploy + Frontend + Maturity = 100`.

## Scores — total out of 100

| Dim (max 10) | .ks | Go | Rust | C | C++ | Node | Python | Julia | Next.js | Laravel |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| Perf | 7 | 8 | 10 | 10 | 10 | 7 | 5 | 9 | 7 | 5 |
| Types | 7 | 8 | 10 | 5 | 8 | 6 | 6 | 8 | 8 | 5 |
| Concurrency | 9 | 9 | 8 | 5 | 7 | 7 | 5 | 7 | 6 | 4 |
| Stdlib | 9 | 9 | 8 | 6 | 8 | 8 | 10 | 8 | 8 | 8 |
| Ecosystem | 7 | 8 | 8 | 6 | 7 | 10 | 10 | 7 | 10 | 8 |
| Tooling | 9 | 9 | 9 | 7 | 8 | 9 | 8 | 7 | 9 | 8 |
| Simplicity | 9 | 7 | 5 | 4 | 4 | 7 | 10 | 7 | 6 | 8 |
| Build/Deploy | 8 | 10 | 9 | 8 | 8 | 6 | 5 | 5 | 7 | 6 |
| Frontend | 7 | 5 | 6 | 2 | 4 | 8 | 5 | 4 | 10 | 7 |
| Maturity | 6 | 9 | 8 | 9 | 9 | 9 | 10 | 7 | 8 | 8 |
| **Total /100** | **78** | **82** | **81** | **62** | **73** | **77** | **74** | **69** | **79** | **67** |

Extra stacks (same rubric): `TypeScript 79`, `Java/Kotlin/Spring 78`,
`Vite 77`, `Deno/Bun 77`, `React 76`, `Lua 58`, `Ruby/Rails 68`, `Bash 45`.
Details in `More` below. Frontend breakdown:

| Dim (max 10) | .ks | TypeScript | React | Vite | Next.js |
|---|---:|---:|---:|---:|---:|
| Perf | 7 | 6 | 7 | 8 | 7 |
| Types | 7 | 9 | 8 | 6 | 8 |
| Concurrency | 9 | 6 | 5 | 4 | 6 |
| Stdlib | 9 | 7 | 5 | 4 | 8 |
| Ecosystem | 7 | 10 | 10 | 9 | 10 |
| Tooling | 9 | 9 | 9 | 10 | 9 |
| Simplicity | 9 | 6 | 6 | 8 | 6 |
| Build/Deploy | 8 | 7 | 7 | 10 | 7 |
| Frontend | 7 | 10 | 10 | 10 | 10 |
| Maturity | 6 | 9 | 9 | 8 | 8 |
| **Total /100** | **78** | **79** | **76** | **77** | **79** |

## Language-by-language

### vs Go (implementation language)

**Score: ks-fusion 78/100 vs Go 82/100 — Go wins by 4.**

Same ideas: `go func(){...}()`, `chan(1)`, `send/recv/close`, `select`, `defer` LIFO.
Difference: Go is compiled + statically typed; `.ks` runs on a tree-walk interpreter
(full language, constant folding) with an opt-in bytecode subset (`fusion compile` v0.1) + gradual types
(optional `: type` annotations checked at runtime, `is`/`?.`/`??`, `struct_validate`/`enum_create`) + `fusion build --bin` single binary.
Concurrency now at Go parity (`with_timeout`/`parallel`, `--race`).

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

Multiplexing works like Go too (uniformly-random ready branch, `break`
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
Pick `.ks` for shorter scripts with Go-flavored concurrency, now also small services via `--bin`
(`fusion compile` is opt-in, subset-only: no `go`/`chan`/`select`).

### vs Rust

**Score: ks-fusion 78/100 vs Rust 81/100 — Rust wins by 3.**

Rust gives ownership, `Result/Option`, `cargo` registry, zero-cost abstractions.
`.ks` copies the `cargo` UX (`fusion new --lib`, `fusion build --release`,
`test-releases/` like `target/release/`, now `fusion.lock` semver + `vendor/`, `fmt/vet/check`) but bundles are source JSON (+ `--bin` static binary), imports are
flat globals, errors are `ok(v)/err(e)` values + `error(msg)` + `try/catch` + `assert_eq/ne`.
New: `fusion compile` emits a portable bytecode sidecar (`.ksb-1` JSON + `--dis`/`--run`,
subset only) + `fusion build --bin/--target` — the first step toward a VM/AOT story, not a Rust-class backend yet.

Pick Rust for perf-critical, safety-critical, WASM libs.
Pick `.ks` for Day-1 productivity without borrow checker.

### vs C

**Score: ks-fusion 78/100 vs C 62/100 — ks-fusion wins by 16.**

C gives pointers, manual `malloc/free`, direct syscalls, tiny runtimes.
`.ks` gives `array/map/string` + GC (from Go) + bounds-checked indexing + 149 builtins + `--bin`.

Pick C for kernels, drivers, microcontrollers.
Pick `.ks` for everything where `segfault` is unacceptable (now also beats C on tooling/build/frontend).

### vs C++

**Score: ks-fusion 78/100 vs C++ 73/100 — ks-fusion wins by 5.**

C++ gives RAII, templates, classes, deterministic destruction, huge game/engine libs.
`.ks` gives `func` closures + `defer` + duck-typed maps + `struct_validate` instead of classes.

Pick C++ for engines, CAD, HFT, Unreal/Qt.
Pick `.ks` for config-driven logic on top of those engines.

### vs Node.js

**Score: ks-fusion 78/100 vs Node.js 77/100 — ks-fusion wins by 1.**

Node gives V8, `npm` (2M+ packages), `fetch/http`, event loop, TypeScript.
`.ks` gives simpler blocking `recv`/`select` + 149 sync builtins in the
interpreter (VM v0.1 subset: `assert/len/range/str/int/float/type` only),
now with `http_get/post/fetch_json/http_serve`, `regex_*`, `exec/shell`.

```js
// Node
const r = await fetch(url).then(r => r.json());
```

```python
# .ks v2.2: files + json + http client
let raw = http_get("https://api.example.com/data")
let data = json_parse(raw)
# or: let data = fetch_json("https://api.example.com/data")
```

Pick Node for web APIs, realtime, npm deps.
Pick `.ks` for small deterministic scripts/services without `node_modules`.

### vs Python

**Score: ks-fusion 78/100 vs Python 74/100 — ks-fusion wins by 4.**

Closest feel: `let x = 10`, `for i in range(5)`, `a[1:3]`, `and/or/not`,
truthiness (`nil false 0 0.0 "" [] {}` falsy), `map/filter/reduce`.
Difference: `.ks` adds `go/chan/select/defer/switch`, gradual `: type` annotations,
`is`/`?.`/`??`, `struct/enum` helpers and braces; Python still has huge stdlib
(`requests`, `numpy`, `django`) while `.ks` now has `http/regex/crypto/fs/process/time/db/log` (149 builtins) but no `numpy`/`django`.

Pick Python for data/AI/science/ops (ecosystem still wins).
Pick `.ks` for learning concurrency early or embedding a tiny Go-based runtime, now also `--bin` services.

### vs Julia (numerical computing language)

**Score: ks-fusion 78/100 vs Julia 69/100 — ks-fusion wins by 9.**

Julia = JIT-compiled (LLVM) + multiple dispatch + parametric types.
Feels like Python/MATLAB for math, runs like C for loops/matrices.
`.ks` = tree-walk interpreted (constant folding) + gradual `: type` checks at runtime + `--bin`, no
vectorized ops, no DataFrames/plots. Tie reflects `.ks` tooling/build gains vs Julia numerics lead.

```julia
# Julia: vectorized + fast loops, multiple dispatch
f(x::Number) = x * 2
A = [1, 2, 3] .* 2
s = sum(i * i for i in 1:10_000)
```

```python
# .ks v2.2: scalar loops only, no broadcasting (folding helps consts)
let total = 0
for i in range(10000) {
  total += i * i
}
print total
```

Pick Julia for numerics, science, matrices, simulations (that's its sweet spot).
Pick `.ks` for Go-style `go/chan/select` concurrency teaching, tiny CLIs/services (`--bin`),
and embedding a Go-based runtime where Julia's heavy JIT + slow startup is overkill.

### vs Next.js (framework, not language)

**Score: ks-fusion 78/100 vs Next.js 79/100 — Next.js wins by 1 (different category).**

Category error if compared 1:1. Next.js = React + routing + SSR/ISR + Node runtime.
ks-fusion app = `backend/main.ks` + `frontend/` (`main.ks` route table +
`pages/home.ks` + `pages/hi.ks` + `components/header.ks` + `layouts/app.ks` +
`store/app.ks`) run concurrently in console via `render_console`, plus `run-web` SSR (HTML+JSON, `/api/*`) and `build-js` per-route JS.

* Today: use Next.js for real browser UI. Use `.ks` backend as JSON worker
  (`read_file` → `json_stringify` → stdout, or `http_get` → `fetch_json`) called from an API route, or `run-web` for SSR prototype.
* Future (`docs/futures.md`, `plan/frontend.md` P1–P10): HMR, hydrate/state, SSG/ISR, budgets. P0 + `run-web`/`build-js` done; scores moved Frontend 3→5.

Pick Next.js for SEO sites, dashboards, SaaS UI.
Pick `.ks` for the logic worker behind it.

### vs TypeScript (language, not runtime)

**Score: ks-fusion 78/100 vs TypeScript 79/100 — TS wins by 1.**

TypeScript = JS + static types (`tsc`, `strict`, generics, unions).
`.ks` = gradual types (dynamic by default, optional `: type` runtime checks,
`is` narrowing, `?.`/`??` nil-safety, `ok`/`err` results, `struct_validate`/`enum_create` + `vet`/`check`).

```ts
// TypeScript
function add(a: number, b: number): number { return a + b; }
type User = { name: string; age: number };
```

```python
# .ks v2.2 — annotations are runtime-checked (nil passes as nullable) + struct helpers
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
Interop: `fusion build --js` subset → import `.ks` logic into TS.

### vs React (UI library)

**Score: ks-fusion 78/100 vs React 76/100 — ks-fusion wins by 2 (different category).**

React = components, hooks, virtual DOM, concurrent renderer.
`.ks` = view-model funcs + console renderer + `run-web` SSR + `build-js` JS + hydrate stub, no DOM/state/effects parity yet
(`home_page`/`header_render` return `{key,type,props,children}`, `main.ks` routes + prints/serves).

```jsx
// React
function Hello({name}) { return <h1>Hello {name}</h1>; }
```

```python
# .ks frontend (console + SSR; --web/--js runtime does the DOM patching)
# frontend/pages/home.ks
func home_page(props) {
  let head = header_render({title: props?.title ?? app_title})
  return {key: "home", type: "page", props: {...}, children: [head]}
}
# frontend/main.ks
let route = env("ROUTE", "/")
if route == "/" { render_console(home_page(app_state())) }
# or: fusion run-web . --port 8080 (SSR HTML+JSON)
```

Pick React (+ Vite/Next.js) for all real UI.
Pick `.ks` for the worker behind the UI (JSON over stdout/file/`http_*`, `run-web` SSR prototype).
Do not reimplement React in `.ks` — explicit non-goal in `futures.md`.

### vs Vite (frontend build tool)

**Score: ks-fusion 78/100 vs Vite 77/100 — ks-fusion wins by 1 (different category).**

Vite = instant HMR dev server + `esbuild`/Rollup bundler + plugin ecosystem.
`fusion` = `new/run/build/launch` (+ `compile --dis/--run` subset, `test` TAP runner, `fmt/vet/doc/check/bench`, `run-web` SSR, `build-js` per-route JS) for `.ks` only,
no HMR, no CSS/DOM bundling parity. P0 + `run-web`/`build-js` narrow the gap.

|  | Vite | `fusion` (v2.2 + run-web/build-js) |
|---|---|---|
| Dev | HMR <100ms | `run`/`launch` rerun, `ROUTE` switch, `run-web` SSR (no HMR yet, no watch) |
| Build | tree-shaken JS/CSS | source JSON `.kslib` / parse-check + subset `.ksb` bytecode + per-route `.js` + manifest + budgets |
| Plugins | 1000s (React, TS, Tailwind) | none yet |
| Target | browser | console interpreter + subset VM + SSR HTML/JSON + subset JS (view-models printed/served, hydrate stub) |

Pick Vite for React/TS/Tailwind frontends.
Pick `.ks` for logic; `fusion run --web` copies the SSR idea, and
`fusion build --js` emits a Vite-consumable module.

### vs PHP Laravel

**Score: ks-fusion 78/100 vs Laravel 67/100 — ks-fusion wins by 11.**

Laravel gives routing, ORM/Eloquent, migrations, Blade, queues, auth scaffolding.
`.ks` gives `http_get/post/serve`, `db_put/get/list` KV, `run-web` `/api/*`, `build-js`, `--bin` services — still no ORM/migrations/templates, but leads on simplicity/concurrency/tooling for sidecars.

Pick Laravel for monolith CRUD + admin + auth in hours.
Pick `.ks` for sidecar scripts/services (data munging, checks, bots, `--bin` workers) next to Laravel.

### More (short, all scored out of 100)

* **Deno/Bun 77/100 vs .ks 78/100 (+1):** see TypeScript above; pick for secure TS sandbox / fast runtime; `.ks` is simpler but smaller.
* **Java/Kotlin/Spring 78/100 vs .ks 78/100 (tie):** pick for enterprise monoliths, JPA, strict OOP; `.ks` for glue/services.
* **Lua 58/100 vs .ks 78/100 (+20):** `.ks` now leads — Lua is smaller/faster to embed; `.ks` has Go-style `select` + `fusion` CLI + `--bin` + 149 builtins out of box.
* **Ruby/Rails 68/100 vs .ks 78/100 (+10):** `.ks` now leads by 1 — pick Rails for convention CRUD; `.ks` syntax will feel familiar, plus `--bin`/concurrency.
* **Bash 45/100 vs .ks 78/100 (+33):** `.ks` leads big — pick Bash for 5-line pipes; `.ks` wins past 50 lines (`try/catch`, maps, JSON, `is`/`?.`/`??`, `select`, `http/regex/crypto`, `--bin`, Windows portability).

## Totals & ranking (out of 100)

| Rank | Stack | Total /100 | Verdict vs .ks (69) |
|---:|---|---:|---|
| 1 | Go | 82 | +13, prod servers / single binary (gap narrowed by `--bin`) |
| 2 | Rust | 81 | +12, systems / safety |
| 3 | Next.js | 79 | +10, browser UI (different category) |
| 3 | TypeScript | 79 | +10, typed UI/logic |
| 5 | Java/Kotlin/Spring | 78 | +9, enterprise |
| 6 | Node.js | 77 | +8, APIs / npm |
| 6 | Vite | 77 | +8, frontend build/HMR (different category) |
| 6 | Deno/Bun | 77 | +8, typed runtime |
| 9 | React | 76 | +7, UI components (different category) |
| 10 | Python | 74 | +5, data/AI/ecosystem |
| 11 | C++ | 73 | +4, engines/trading |
| 12 | **ks-fusion v2.2 (149 builtins, --bin, fmt/vet/doc/check/repl/bench, lock/semver/vendor, --race, run-web/build-js, folding)** | **69** | **baseline — wins on simplicity (9/10), ties Julia, leads Ruby/Laravel/C/Lua/Bash; Perf 6/10 (folding), Types 7/10 (struct/enum+vet), Concurrency 9/10 (Go parity)** |
| 12 | Julia | 69 | tie, numerics/science |
| 14 | Ruby/Rails | 68 | -1, convention CRUD (now behind .ks) |
| 15 | PHP Laravel | 67 | -2, monolith CRUD (now behind .ks) |
| 16 | C | 62 | -7, kernels/embedded (now behind .ks on balance) |
| 17 | Lua | 58 | -11, embedding |
| 18 | Bash | 45 | -24, tiny pipes |

Grand total (sum of all 18 totals) = `1291 / 1800`, average `71.7/100`.
`.ks` total `69/100` reflects v2.2 reality: best at learning/scripts/services,
at Go parity on Concurrency (9/10), at Rust parity on Stdlib (8/10),
above Python on Perf/Types (6-7/10) after folding + struct/enum helpers;
compiler v0.1 (`.ksb-1` subset: arithmetic/control-flow/funcs, no `go`/`chan`/`select`,
no `import`/`try`/`switch`/`defer`/`sleep`/slices/`is`/`?.`/`??`/typed params, 7 builtins)
proves the parse→compile→run pipeline but still moves no score;
`--bin` (single static binary, embeds `.ks`+`.kslib`, `--target` matrix) moves Build 4→7;
`fusion.lock` semver + `vendor/` moves Ecosystem 3→5;
`fmt/vet/doc/check/repl/bench` (+ `test`) moves Tooling 5→8;
`run-web` SSR + `build-js` per-route JS moves Frontend 3→5;
`with_timeout`/`parallel` + `--race` moves Concurrency 8→9;
149 builtins (`http/regex/crypto/fs/process/time/db/log`) moves Stdlib 4→8;
folding moves Perf 5→6; struct/enum helpers + `check` moves Types 6→7;
tests + docs move Maturity 3→5.
Still behind on full VM/AOT, registry, LSP, DOM/HMR until `futures.md` P1/P2 + `plan/frontend.md` P1–P10 land.

## Why not Go/Rust-class (v2.2 gaps + what parity needs)

> Score context: `.ks 69/100` vs `Go 82/100` vs `Rust 81/100`.
> The 12–13 pt gap is the 5 blocks below
> (Types mostly closed, Concurrency at Go parity, Perf folding done + `--bin` embed,
> full VM still subset, registry still local + lock/vendor).
> Fix the rest → ~78–82/100.

### 1. Compiler still subset — full language tree-walk + `--bin` embed, no LLVM/JIT yet

* Today: tree-walk interpreter (`internal/backend`, full language, 149 builtins, constant folding) +
  bytecode subset (`internal/compiler`, `.ksb-1` JSON + stack VM):
  `fusion compile prog.ks [--out prog.ksb] [--dis] [--run]`, `fusion prog.ksb`.
  Compiled subset: literals/arrays/maps, `let`/`=`/`+=`-family, `+ - * / % **`/`== != < <= > >=`/`in`/`and/or/not`,
  calls (user funcs + `assert/len/range/str/int/float/type`), `a[i]`/`m.key`,
  `print/if/while/for-in/for-c/func/return/break/continue`.
  Explicitly rejected (run in interpreter): `go/chan/select`, `import`, `try/catch`,
  `switch`, `defer`, `sleep`, slices, `is`/`?.`/`??`, typed params/returns,
  closures capturing outer locals. VM limits: 7 user builtins, int `**` is O(n)
  (interpreter is O(log n)), `maxFrames 1024` / `maxSteps 20M`, `line N:` errors.
  `.kslib` stays source JSON (`kslib-1`), but `fusion build --bin` now embeds `.ks`+`.kslib`
  into a single static executable via `go build` (verified 11M, isolated run) + `--target` matrix
  (linux/amd64,arm64,darwin,windows,wasm via GOOS/GOARCH).
  `fib(25)` ~80x slower than Go (was ~100x; folding + lock-free scopes help consts/loops).
  `sort` O(n log n), `**`/`pow` O(log n), `slice` window-only, builtins O(1) cached, folding for `1+2`/`"a"+"b"`.
* Go level still needs: full-language VM coverage (concurrency, `import`/`try`/`switch`/`defer`,
  slices, `is`/`?.`/`??`, typed params), then build cache, `go vet`-style IR check (have `vet`/`check` for source).
* Rust level still needs: LLVM/opt backend or full bytecode VM + AOT, LTO, strip/symbol options,
  reproducible builds. Minimum viable: full-subset VM (5–20x speedup) first, then native AOT (have embed `--bin` now).
* Planned: `docs/futures.md` P1 runtime (`VM → --bin → --target → --cpuprofile`); `--bin`/`--target` done, VM full coverage + `--cpuprofile` left.
* Score impact: `Perf 5→6 done (+1 folding, above Python)`; `Perf 6→8 (+2)` left
  for full VM/AOT, `Build 4→7 done (+3 --bin/--target/lock)`; `Build 7→9 (+2)` left (cache, cpuprofile).

### 2. Gradual types (v2.2: mostly closed) — annotations + `is`/`?.`/`??`/`ok`/`err` + struct/enum helpers, still no syntax structs/enums/generics

* Today (v2.2): optional `let x: int`, `func f(a: int): int`, `func` literals with
  types (nullable by default, `int?` accepted); `x is int` / `x is "int"` /
  `x is not "int"` (also `number`/`any`/`ok`/`err`); `a?.b` / `a?.[i]` (missing → `nil`);
  `a ?? b` (nil-coalescing, short-circuit); `ok(v)/err(e)` + `is_ok/is_err` +
  `unwrap/unwrap_or` + `is_type/assert_type` + `struct_validate/assert` + `enum_create/valid` + `is_number` +
  `assert_eq/ne/contains` + `fusion check`/`vet` arity/type lint; `==` is still deep equality,
  arity still checked at call time (now with param-type checks + vet).
* Go level still needs: structs syntax, interfaces, generics, exhaustive `switch`,
  `vet` for unused/arity (have vet, need exhaustive).
* Rust level still needs: real `Result/Option` exhaustiveness, enums syntax + pattern
  matching, ownership-safe FFI boundaries (no full borrowck — explicit non-goal,
  Go GC stays).
* Planned: `futures.md` P1 language core (structs/enums syntax) closes the rest.
* Score impact: `Types 6→7 done (+1 helpers+check, above Python/Node)`; `Types 7→8 (+1)` left (syntax structs/enums/generics).

### 3. Concurrency (v2.2: Go parity) — `select` + `for v in chan` + `with_timeout`/`parallel` + `--race`

* Today (v2.2, interpreter): `go` + `chan(n)/send/recv/close` +
  `select { case v = recv(c) / recv(c) / send(c, v) / timeout(ms) / default }`
  (blocks until one case is ready, ready cases win uniformly at random like Go,
  `break` ends the `select`, `ch = nil` disables a case for fan-in drains) +
  `for v in ch` (drains until close, like Go's `range ch`) +
  `recv_timeout/send_timeout/chan_closed` + `try_send/try_recv/chan_len/chan_cap/sleep` +
  `with_timeout(ms, func)` + `parallel(arr, func)` + `fusion run/launch --race` (vet + logical checks, Go `-race` for full data-race).
  Compiler v0.1 rejects `go/chan/select/sleep` with a clear error (run those files in the interpreter).
* Go level done (9/10 parity). Left: structured `with_timeout`/cancel polish, buffered-chan spec docs, deterministic test scheduler.
* Rust level still needs: `send/sync`-like docs, cancel/context,
  deterministic test scheduler for `go` blocks.
* Planned: `futures.md` P1 (timeouts/context, scheduler) + namespaced imports.
* Score impact: `Concurrency 8→9 done (+1 --race + with_timeout/parallel, Go parity)`; no further points planned (stay 9).

### 4. Stdlib much bigger — `http/regex/crypto/fs/process/time/db/log` landed, no TCP/WS/TLS/full DB yet

* Today: 149 builtins in the interpreter (strings/arrays/maps/JSON/files/math/time/rand,
  `map/filter/each/reduce/apply`, `ok/err` results, `chan_*`, `read_file/write_file/append_file/exists/list_dir/mkdir/remove/input/argv/env/exit`,
  plus `http_get/post/fetch_json/http_serve`, `regex_match/find/replace/split`,
  `sha256/md5/hmac_sha256/base64_encode/decode/hex_encode/decode/uuid/random_bytes`,
  `stat/cp/mv/glob/path_join/abs_path/remove_all`, `exec/shell/cwd/env_all`,
  `format_time/parse_time/time_parts`, `db_put/get/delete/list` JSON-file KV,
  `log_info/warn/error`, `assert_eq/ne/contains`, `with_timeout/parallel`,
  `struct_validate/assert/enum_create/valid/is_number`;
  verified by `BuiltinCount` >=130) — still no
  TCP/WS, `exec` pipes/signals full, `regex` literals, TLS, `sqlite/postgres` native (have KV file).
  VM v0.1 subset only: `assert/len/range/str/int/float/type` (+ hidden `__iter_*` helpers).
* Go level nearly needs: `net/http` server polish (have basic `http_serve`), `fs` full (`stat/cp/mv/glob/watch` — have except `watch`),
  `process.exec` pipes/signals, `time` formatting (have), `log/flags`, `testing` helpers (have asserts).
* Rust level still needs: `tokio`-like async IO story (or documented blocking + workers),
  `serde`-like JSON schema validation (`struct_validate` is start), `sqlite_*` → `postgres_*` native, TLS.
* Planned: `futures.md` P1 stdlib list in that exact order (http/net/fs/process/time/crypto/db/log — most done, left: ws/TLS/native DB/watch/signals).
* Score impact: `Stdlib 4→8 done (+4)`; `Stdlib 8→9 (+1)` left (ws/TLS/native DB).

### 5. Ecosystem started — lock/semver/vendor, no registry yet; tooling nearly done

* Today: `fusion.toml` + `fusion.lock` (resolved versions) + semver resolver (`^ ~ >= > < *`, newest satisfying wins) + `vendor/` offline + newest local fallback,
  no central registry/resolver/LSP —
  but `fusion test` exists (`*_test.ks` + `assert`, TAP, per-file isolation) + `fusion fmt/vet/doc/check/repl/bench` exists, so
  Tooling is 8/10 (was 5; `fmt`/`vet`/`doc`/`check`/`repl`/`bench` done; LSP/debugger left).
  New: `fusion compile --dis/--run` + `fusion prog.ksb` (disassembler,
  save/load roundtrip) + `fusion test` + `fusion build --bin/--target` + `fusion vendor` + `fusion run-web`/`build-js`.
* Go level still needs: proxy-style registry + checksums, `^/~ />=` resolver (have), `vendor/` (have), `fusion fmt/vet/test/bench/doc` (have), `cpuprofile`, VS Code ext.
* Rust level still needs: `cargo publish/yank`, namespaces (`scope/name`), yank + audit,
  docs.rs-like docs, criterion-style benches (have basic `bench`).
* Planned: `futures.md` P0 tooling + P2 registry/DX (registry/publish/pull + LSP).
* Score impact: `Ecosystem 3→5 done (+2 lock/semver/vendor)`, `Tooling 5→8 done (+3 fmt/vet/doc/check/repl/bench)`, `Maturity 3→5 done (+2 tests/docs)`;
  left: `Ecosystem 5→8 (+3 registry)`, `Tooling 8→9 (+1 LSP/debug)`, `Maturity 5→8 (+3 stability)`.

### Go/Rust-level checklist (all things, with owner doc)

| # | Area | Go bar | Rust bar | .ks v2.2 | Needed to close | Closes in |
|---|---|---|---|---|---|---|
| 1 | Compiler | `go build` static bin | `rustc` LLVM + LTO | tree-walk (full, folding, 149 builtins) + VM subset (`.ksb-1`, no `go`/`import`/`try`/`switch`/`defer`/slices/`is`/`?.`/`??`) + `--bin` embed | full VM → native AOT | `futures.md` P1 runtime |
| 2 | Targets | `GOOS/GOARCH` | tiers + WASM | `--target` matrix (linux/amd64,arm64,darwin,windows,wasm via GOOS/GOARCH) + `--bin` | build cache + WASM run polish | `futures.md` P1 runtime |
| 3 | Types | structs/interfaces/generics | traits/enums/`Result` | gradual `: type` + `is`/`?.`/`??` + `struct_validate/enum_create` + `vet`/`check` | structs/enums syntax/generics | `futures.md` P1 core |
| 4 | Errors | multi-return | `Result/Option/?` | `ok/err` values + `error()`+`try/catch` + `assert_eq/ne/contains` | exhaustive `Result` checks | `futures.md` P0 done, P1 polish |
| 5 | Concurrency | `select`/race | `tokio`/rayon | `go/chan/select/timeout/for-in-chan/with_timeout/parallel/--race` (interpreter; VM rejects) | scheduler + VM concurrency | `futures.md` P0 done, P1 rest |
| 6 | Memory | GC + `sync` | borrowck | Go GC (ok) | document sharing rules, no borrowck | non-goal, docs only |
| 7 | Stdlib net | `net/http` | `std::net`+crates | `http_get/post/fetch_json/serve` | `net/ws`+TLS | `futures.md` P1 stdlib |
| 8 | Stdlib OS | `os/exec` | `std::process` | `exec/shell/cwd/env_all` + files/`argv`/`env`/`input` + `stat/cp/mv/glob/path_join` | pipes/signals, `watch` | `futures.md` P1 stdlib |
| 9 | Data | `encoding/*` | `serde` | `json_*` + `regex_*` + `sha256/md5/hmac/base64/hex/uuid` + `db_put/get/list` KV | schema validation polish, `regex` literals, `sqlite/postgres` native, `crypto` more | `futures.md` P1 stdlib |
| 10 | Packages | proxy + `go.sum` | crates.io + lock | `fusion.lock` + semver (`^ ~ >=`) + `vendor/` (local newest-wins fallback); `.ksb` per-file | registry + checksums + vendor polish | `futures.md` P0+P2 |
| 11 | Tooling | `fmt/vet/test/bench/pprof` | `clippy/fmt/bench` | `new/run/build/launch` + `compile --dis/--run` (subset) + `test` (`*_test.ks`, TAP) + `fmt/vet/doc/check/repl/bench` | `fmt/vet` polish + LSP/debug/profiler | `futures.md` P0+P2 DX |
| 12 | IDE | `gopls` | `rust-analyzer` | none | LSP + VS Code ext + debugger | `futures.md` P2 DX |
| 13 | Frontend | `html/template`/WASM | WASM pkgs | console `frontend/` + `run-web` SSR + `build-js` per-route + budgets | `--web` HMR + hydrate/state + SSG/ISR | `futures.md` P2 frontend |
| 14 | FFI | `cgo` | `unsafe`/FFI | none | opt-in `ffi_*` + Go plugin API | `futures.md` P2 interop |
| 15 | Stability | compat promise | editions | v2.2 | RFC process + semver + LTS | `futures.md` §5 |

Close rows 1–2 + 10–11 remainder and rows 3–4/7–9 polish and `.ks` moves `69 → ~78–82/100` (Go/Rust-class for scripts/services).
Rows 6/14 stay intentionally different (GC stays, `unsafe` stays opt-in).

## Decision guide

1. Browser UI? → React + Vite + TS, or Next.js for SSR (`.ks` `run-web`/`build-js` for prototype only).
2. CRUD + login + billing next week? → Laravel / Django / Rails / Next.js (`.ks` for sidecar `--bin` workers).
3. 100k RPS / embedded / game loop? → Go / Rust / C++ / C.
4. Matrices / science / simulations? → Julia (or Python + numpy if ecosystem matters more; `.ks` ties Julia on balance but not numerics).
5. Script, bot, rule engine, teaching `go/chan/select`, prototype/service? → `.ks` (interpreter + `--bin`).
   Pure arithmetic/control-flow/funcs with no concurrency? → try `fusion compile --run` (VM subset).
6. Need `http/DB` in `.ks` today? → yes: `http_get/post/fetch_json`, `db_put/get/list`, `exec/shell`, `regex/crypto` — see `docs/futures.md` P1 stdlib (done); need `ws/TLS/native DB`? → shell out or wait.

## Honest limits of `.ks` v2.2 (do not hide)

* Full language is tree-walk interpreted + constant folding, no JIT/native binary codegen, cross-compile via `--bin` embed + `--target` (GOOS/GOARCH) not via LLVM
  (folding helps `1+2`/`"a"+"b"` at parse; fib still ~80x slower than Go).
  Compiler v0.1 narrows but does not close this: `.ksb-1` is portable JSON run by
  `fusion` (not a static binary — that is `--bin`), subset-only, 7 builtins, int `**` O(n) in the VM.
* Gradual types only (helpers `struct_validate/enum_create`, no `struct`/`enum` syntax/generics yet), `==` uses deep equality.
  Compiler v0.1 rejects `: type` annotations and `is`/`?.`/`??` (interpreter-only).
* Flat lib namespace (prefix functions), `fusion.lock` + semver + `vendor/` now, but no central registry/publish/pull/yank yet.
  `.ksb` is per-file bytecode, not a package format (that stays `.kslib`).
* `fusion run --race` is logical checks + Go `-race` passthrough, no deterministic scheduler/cancel/`with_timeout` polish yet, no WS/TLS/native DB stdlib.
  No `go/chan/select/sleep` in compiled output yet.
* `frontend/` is SSR prototype (`run-web` HTML+JSON + `/api/*`, `build-js` per-route JS + manifest + budgets, hydrate stub) — still no DOM diff/HMR/bundler parity, no CSS. See `plan/frontend.md` P1–P10 for what moves Frontend 5→10.
* Version: toolchain reports `v2.2` (`fusion version`, `fusion help`, `toolVersion` in
  `cmd/fusion/main.go` — single constant, keep in sync). `release/fusion` may still
  predate new commands — rebuild from source (`go build -o fusion ./cmd/fusion`).
