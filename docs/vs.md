# ks-fusion vs Others

> ks-fusion `v2.1` + compiler `v0.1`: gradual-typed `.ks` language, toolchain written in Go.
> Easy like Python, concurrency like Go, packaging like Rust.
> Interpreter runs the full language (97 builtins); `fusion compile` adds a
> portable bytecode subset (`.ksb-1` JSON + stack VM: arithmetic, control flow, funcs).
> This doc is honest about where `.ks` wins and where it loses.

## TL;DR

| If you need… | Pick… | Why not `.ks` yet |
|---|---|---|
| Single static binary, max RPS, strict types | Go / Rust | `.ks` runs on tree-walk interpreter (full language) + VM subset (`fusion compile` v0.1); `fib(25)` ~100x slower than Go (sort/pow optimized, scopes lock-free) |
| Kernel, drivers, games, hard realtime | C / C++ / Rust | No manual memory, no pointers, no SIMD |
| Browser UI / React / SSR | Next.js (TS) | `frontend/` is view-models + console renderer (P0), not DOM yet |
| CRUD + auth + admin panel tomorrow | PHP Laravel / Python Django | No ORM, migrations, HTTP server stdlib yet |
| Numerical / scientific / matrices | Julia | `.ks` has no vectorized ops, no DataFrames/plots, ~100x slower loop math |
| Quick scripts, rules, gluing, learning | ks-fusion | — this is the sweet spot |
| npm / PyPI ecosystem | Node.js / Python | `.ks` has 97 builtins (interpreter; 7 in VM subset) + local `.kslib`/`.ksb` only |

## Big table

|  | ks-fusion (.ks) | Go | Rust | C | C++ | Node.js (JS/TS) | Python | Julia | Next.js | PHP Laravel |
|---|---|---|---|---|---|---|---|---|---|---|
| Model | tree-walk interpreter (full language) + bytecode VM subset v0.1 (`.ksb-1` JSON, `fusion compile`) | compiled, GC | compiled, no GC (borrowck) | compiled, manual | compiled, RAII | V8 JIT | interpreted (+C ext) | JIT (LLVM), GC, multiple dispatch | React framework on Node | interpreted + framework |
| Typing | gradual: dynamic + `: type` annotations, `is`, `?.`/`??`, `ok`/`err` results | static, interfaces, generics | static, traits, enums, `Result/Option` | weak static, pointers | static, templates, classes | dynamic + optional TS | dynamic + hints | dynamic + parametric types, multiple dispatch | TS-typed components | dynamic |
| Perf | medium (scripts, bots, CLIs; interpreter O(n log n) sort, O(log n) pow, lock-free scopes; VM v0.1 subset claims no speedup yet) | high (servers) | highest (systems) | highest | highest | medium-high (I/O) | medium (glue/AI) | highest (numeric; C-speed loops via JIT) | medium (SSR) | medium (CRUD) |
| Concurrency | `go` + `chan`/`select` (`recv`/`send`/`timeout`/`default`, `for v in chan`, goroutines underneath; interpreter only, VM v0.1 has no `go`/`chan`) | goroutines + `select` | `async/tokio`, threads | threads, manual | threads/`async` | event loop + workers | threads/GIL + `asyncio` | threads + distributed + `async` | server/client components | processes + queues |
| Packaging | `fusion.toml` + `.kslib` JSON (`kslib-1`), local `test-releases/`/`target/`; bytecode sidecar `.ksb` JSON (`ksb-1`, subset only) | `go.mod` + proxy | `cargo` + crates.io | make/cmake | cmake/vcpkg/conan | npm/pnpm | pip/poetry | `Pkg` + General registry | npm + Vercel | composer + artisan |
| Binary | needs `fusion` on PATH (shebang), no `--bin` yet (`fusion compile` emits portable `.ksb`, still run by `fusion`) | single static binary | single binary | binary | binary | needs node/runtime | needs python | needs julia runtime (PackageCompiler possible, heavy) | needs node | needs php+server |
| Best for | learning, automation, rules engines, small backends | APIs, DevOps, cloud | systems, WASM, games | OS, embedded | engines, trading, desktop | APIs, realtime, SSR | scripts, data, AI | numerics, science, matrices | fullstack React apps | monolith CRUD apps |

See `docs/futures.md` for what closes each gap (`--bin`, `http_*`, registry, `--js`).
`fusion compile` (`internal/compiler`, `.ksb-1` + `fusion prog.ksb` + `--dis`/`--run`)
is step one of the `futures.md` P1 runtime plan; scoring below is unchanged until it covers the full language.

## How scoring works (out of 100)

Each language / stack is scored `0-10` on 10 dimensions = `100` max.
Snapshot for ks-fusion `v2.1` + compiler `v0.1`. Higher = better, except simplicity where
easier = higher. Scores are opinionated but rubric-based, not benchmarks.
Compiler v0.1 (subset bytecode/VM) does not move any score yet — see §1.

Dimensions: `Perf + Types + Concurrency + Stdlib + Ecosystem + Tooling +
Simplicity + Build/Deploy + Frontend + Maturity = 100`.

## Scores — total out of 100

| Dim (max 10) | .ks | Go | Rust | C | C++ | Node | Python | Julia | Next.js | Laravel |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| Perf | 5 | 8 | 10 | 10 | 10 | 7 | 5 | 9 | 7 | 5 |
| Types | 6 | 8 | 10 | 5 | 8 | 6 | 6 | 8 | 8 | 5 |
| Concurrency | 8 | 9 | 8 | 5 | 7 | 7 | 5 | 7 | 6 | 4 |
| Stdlib | 4 | 9 | 8 | 6 | 8 | 8 | 10 | 8 | 8 | 8 |
| Ecosystem | 3 | 8 | 8 | 6 | 7 | 10 | 10 | 7 | 10 | 8 |
| Tooling | 5 | 9 | 9 | 7 | 8 | 9 | 8 | 7 | 9 | 8 |
| Simplicity | 9 | 7 | 5 | 4 | 4 | 7 | 10 | 7 | 6 | 8 |
| Build/Deploy | 4 | 10 | 9 | 8 | 8 | 6 | 5 | 5 | 7 | 6 |
| Frontend | 3 | 5 | 6 | 2 | 4 | 8 | 5 | 4 | 10 | 7 |
| Maturity | 3 | 9 | 8 | 9 | 9 | 9 | 10 | 7 | 8 | 8 |
| **Total /100** | **50** | **82** | **81** | **62** | **73** | **77** | **74** | **69** | **79** | **67** |

Extra stacks (same rubric): `TypeScript 79`, `Java/Kotlin/Spring 78`,
`Vite 77`, `Deno/Bun 77`, `React 76`, `Lua 58`, `Ruby/Rails 68`, `Bash 45`.
Details in `More` below. Frontend breakdown:

| Dim (max 10) | .ks | TypeScript | React | Vite | Next.js |
|---|---:|---:|---:|---:|---:|
| Perf | 5 | 6 | 7 | 8 | 7 |
| Types | 6 | 9 | 8 | 6 | 8 |
| Concurrency | 8 | 6 | 5 | 4 | 6 |
| Stdlib | 4 | 7 | 5 | 4 | 8 |
| Ecosystem | 3 | 10 | 10 | 9 | 10 |
| Tooling | 4 | 9 | 9 | 10 | 9 |
| Simplicity | 9 | 6 | 6 | 8 | 6 |
| Build/Deploy | 4 | 7 | 7 | 10 | 7 |
| Frontend | 3 | 10 | 10 | 10 | 10 |
| Maturity | 3 | 9 | 9 | 8 | 8 |
| **Total /100** | **49** | **79** | **76** | **77** | **79** |

## Language-by-language

### vs Go (implementation language)

**Score: ks-fusion 49/100 vs Go 82/100 — Go wins by 33.**

Same ideas: `go func(){...}()`, `chan(1)`, `send/recv/close`, `select`, `defer` LIFO.
Difference: Go is compiled + statically typed; `.ks` runs on a tree-walk interpreter
(full language) with an opt-in bytecode subset (`fusion compile` v0.1) + gradual types
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

Pick Go for prod servers, strict APIs, single binary.
Pick `.ks` for shorter scripts with Go-flavored concurrency and no mandatory compile step
(`fusion compile` is opt-in, subset-only: no `go`/`chan`/`select`).

### vs Rust

**Score: ks-fusion 49/100 vs Rust 81/100 — Rust wins by 32.**

Rust gives ownership, `Result/Option`, `cargo` registry, zero-cost abstractions.
`.ks` copies the `cargo` UX (`fusion new --lib`, `fusion build --release`,
`test-releases/` like `target/release/`) but bundles are source JSON, imports are
flat globals, errors are `ok(v)/err(e)` values + `error(msg)` + `try/catch`.
New: `fusion compile` emits a portable bytecode sidecar (`.ksb-1` JSON + `--dis`/`--run`,
subset only) — the first step toward a VM/AOT story, not a Rust-class backend yet.

Pick Rust for perf-critical, safety-critical, WASM libs.
Pick `.ks` for Day-1 productivity without borrow checker.

### vs C

**Score: ks-fusion 49/100 vs C 62/100 — C wins by 13.**

C gives pointers, manual `malloc/free`, direct syscalls, tiny runtimes.
`.ks` gives `array/map/string` + GC (from Go) + bounds-checked indexing.

Pick C for kernels, drivers, microcontrollers.
Pick `.ks` for everything where `segfault` is unacceptable.

### vs C++

**Score: ks-fusion 49/100 vs C++ 73/100 — C++ wins by 24.**

C++ gives RAII, templates, classes, deterministic destruction, huge game/engine libs.
`.ks` gives `func` closures + `defer` + duck-typed maps instead of classes.

Pick C++ for engines, CAD, HFT, Unreal/Qt.
Pick `.ks` for config-driven logic on top of those engines.

### vs Node.js

**Score: ks-fusion 49/100 vs Node.js 77/100 — Node wins by 28.**

Node gives V8, `npm` (2M+ packages), `fetch/http`, event loop, TypeScript.
`.ks` gives simpler blocking `recv`/`select` + 97 sync builtins in the
interpreter (VM v0.1 subset: `assert/len/range/str/int/float/type` only),
no `http_*` yet.

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

**Score: ks-fusion 49/100 vs Python 74/100 — Python wins by 25.**

Closest feel: `let x = 10`, `for i in range(5)`, `a[1:3]`, `and/or/not`,
truthiness (`nil false 0 0.0 "" [] {}` falsy), `map/filter/reduce`.
Difference: `.ks` adds `go/chan/select/defer/switch`, gradual `: type` annotations,
`is`/`?.`/`??` and braces; Python has huge stdlib
(`requests`, `numpy`, `django`) while `.ks` has files/JSON/strings only.

Pick Python for data/AI/science/ops (ecosystem wins).
Pick `.ks` for learning concurrency early or embedding a tiny Go-based runtime.

### vs Julia (numerical computing language)

**Score: ks-fusion 49/100 vs Julia 69/100 — Julia wins by 20.**

Julia = JIT-compiled (LLVM) + multiple dispatch + parametric types.
Feels like Python/MATLAB for math, runs like C for loops/matrices.
`.ks` = tree-walk interpreted + gradual `: type` checks at runtime, no
vectorized ops, no DataFrames/plots.

```julia
# Julia: vectorized + fast loops, multiple dispatch
f(x::Number) = x * 2
A = [1, 2, 3] .* 2
s = sum(i * i for i in 1:10_000)
```

```python
# .ks v2.1: scalar loops only, no broadcasting
let total = 0
for i in range(10000) {
  total += i * i
}
print total
```

Pick Julia for numerics, science, matrices, simulations (that's its sweet spot).
Pick `.ks` for Go-style `go/chan/select` concurrency teaching, tiny CLIs,
and embedding a Go-based runtime where Julia's heavy JIT + slow startup is overkill.

### vs Next.js (framework, not language)

**Score: ks-fusion 49/100 vs Next.js 79/100 — Next.js wins by 30 (different category).**

Category error if compared 1:1. Next.js = React + routing + SSR/ISR + Node runtime.
ks-fusion app = `backend/main.ks` + `frontend/` (P0: `main.ks` route table +
`pages/home.ks` + `pages/hi.ks` + `components/header.ks` + `layouts/app.ks` +
`store/app.ks`) run concurrently in console via `render_console` (no DOM yet).

* Today: use Next.js for real browser UI. Use `.ks` backend as JSON worker
  (`read_file` → `json_stringify` → stdout) called from an API route.
* Future (`docs/futures.md`, `plan/frontend.md` P1–P10): `fusion build --js` subset +
  `fusion run --web` hot reload + SSR/hydrate. P0 (conventions + view-model
  `{key,type,props,children}` + `ROUTE` switch) is done; scores unchanged.

Pick Next.js for SEO sites, dashboards, SaaS UI.
Pick `.ks` for the logic worker behind it.

### vs TypeScript (language, not runtime)

**Score: ks-fusion 49/100 vs TypeScript 79/100 — TS wins by 30.**

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

**Score: ks-fusion 49/100 vs React 76/100 — React wins by 27 (different category).**

React = components, hooks, virtual DOM, concurrent renderer.
`.ks` P0 = view-model funcs + console renderer, no DOM/state/effects yet
(`home_page`/`header_render` return `{key,type,props,children}`, `main.ks` routes + prints).

```jsx
// React
function Hello({name}) { return <h1>Hello {name}</h1>; }
```

```python
# .ks frontend P0 (console; --web/--js runtime does the DOM patching later)
# frontend/pages/home.ks
func home_page(props) {
  let head = header_render({title: props?.title ?? app_title})
  return {key: "home", type: "page", props: {...}, children: [head]}
}
# frontend/main.ks
let route = env("ROUTE", "/")
if route == "/" { render_console(home_page(app_state())) }
```

Pick React (+ Vite/Next.js) for all real UI.
Pick `.ks` for the worker behind the UI (JSON over stdout/file, later `http_*`).
Do not reimplement React in `.ks` — explicit non-goal in `futures.md`.

### vs Vite (frontend build tool)

**Score: ks-fusion 49/100 vs Vite 77/100 — Vite wins by 28 (different category).**

Vite = instant HMR dev server + `esbuild`/Rollup bundler + plugin ecosystem.
`fusion` = `new/run/build/launch` (+ `compile --dis/--run` subset, `test` TAP runner) for `.ks` only,
no HMR, no bundling, no CSS/DOM. P0 adds the file layout (`pages/components/layouts/store`)
and `fusion new` scaffolds it, but no watcher/bundler yet.

|  | Vite | `fusion` (v2.1 + compiler v0.1 + frontend P0) |
|---|---|---|
| Dev | HMR <100ms | `run`/`launch` rerun, `ROUTE` switch, no watch |
| Build | tree-shaken JS/CSS | source JSON `.kslib` / parse-check + subset `.ksb` bytecode |
| Plugins | 1000s (React, TS, Tailwind) | none yet |
| Target | browser | console interpreter + subset VM (view-models printed, not patched) |

Pick Vite for React/TS/Tailwind frontends.
Pick `.ks` for logic; future `fusion run --web` will copy the HMR idea, and
`fusion build --js` is meant to emit a Vite-consumable module.

### vs PHP Laravel

**Score: ks-fusion 49/100 vs Laravel 67/100 — Laravel wins by 18.**

Laravel gives routing, ORM/Eloquent, migrations, Blade, queues, auth scaffolding.
`.ks` gives none of that yet — no HTTP server, no DB driver, no templates.

Pick Laravel for monolith CRUD + admin + auth in hours.
Pick `.ks` for sidecar scripts (data munging, checks, bots) next to Laravel.

### More (short, all scored out of 100)

* **Deno/Bun 77/100 vs .ks 49/100 (-28):** see TypeScript above; pick for secure TS sandbox / fast runtime; `.ks` is simpler but smaller.
* **Java/Kotlin/Spring 78/100 vs .ks 49/100 (-29):** pick for enterprise monoliths, JPA, strict OOP; `.ks` for glue.
* **Lua 58/100 vs .ks 49/100 (-9):** closest embed rival — Lua is smaller/faster to embed; `.ks` has Go-style `select` + `fusion` CLI out of box.
* **Ruby/Rails 68/100 vs .ks 49/100 (-19):** pick for convention CRUD; `.ks` syntax will feel familiar.
* **Bash 45/100 vs .ks 49/100 (+4):** `.ks` leads — pick Bash for 5-line pipes; `.ks` wins past 50 lines (`try/catch`, maps, JSON, `is`/`?.`/`??`, `select`, Windows portability).

## Totals & ranking (out of 100)

| Rank | Stack | Total /100 | Verdict vs .ks (49) |
|---:|---|---:|---|
| 1 | Go | 82 | +33, prod servers / single binary |
| 2 | Rust | 81 | +32, systems / safety |
| 3 | Next.js | 79 | +30, browser UI (different category) |
| 3 | TypeScript | 79 | +30, typed UI/logic |
| 5 | Java/Kotlin/Spring | 78 | +29, enterprise |
| 6 | Node.js | 77 | +28, APIs / npm |
| 6 | Vite | 77 | +28, frontend build/HMR (different category) |
| 6 | Deno/Bun | 77 | +28, typed runtime |
| 9 | React | 76 | +27, UI components (different category) |
| 10 | Python | 74 | +25, data/AI/ecosystem |
| 11 | C++ | 73 | +24, engines/trading |
| 12 | Julia | 69 | +20, numerics/science |
| 13 | Ruby/Rails | 68 | +19, convention CRUD |
| 14 | PHP Laravel | 67 | +18, monolith CRUD |
| 15 | C | 62 | +13, kernels/embedded |
| 16 | Lua | 58 | +9, embedding |
| 17 | **ks-fusion v2.1 + compiler v0.1 + frontend P0** | **49** | **baseline — wins on simplicity (9/10), leads Bash by 4; Perf 5/10 after tree-walk opts (VM subset unrated, P0 moves no score)** |
| 18 | Bash | 45 | -4, tiny pipes |

Grand total (sum of all 18 totals) = `1271 / 1800`, average `70.6/100`.
`.ks` total `49/100` reflects v2.1 + compiler v0.1 + frontend P0 reality: best at learning/scripts,
at Python/Node parity on Types (6/10) and Rust parity on Concurrency (8/10),
at Python parity on Perf (5/10) after tree-walk opts;
compiler v0.1 (`.ksb-1` subset: arithmetic/control-flow/funcs, no `go`/`chan`/`select`,
no `import`/`try`/`switch`/`defer`/`sleep`/slices/`is`/`?.`/`??`/typed params, 7 builtins)
proves the parse→compile→run pipeline but moves no score yet;
frontend P0 (route table + `pages/components/layouts/store` + view-model
`{key,type,props,children}` + `render_console`, `fusion new` scaffolds it) is
conventions only — still console, no DOM/HMR/bundler, so Frontend stays 3/10;
still behind everywhere else until `futures.md` P0/P1 + `plan/frontend.md` P1–P10 land.

## Why not Go/Rust-class (v2.1 + compiler v0.1 gaps + what parity needs)

> Score context: `.ks 49/100` vs `Go 82/100` vs `Rust 81/100`.
> The 32–33 pt gap is the 5 blocks below
> (Types half-closed, Concurrency at Rust parity, Perf tree-walk opts done in v2.1,
> compiler v0.1 pipeline proven but subset-only).
> Fix the rest → ~75–80/100.

### 1. Compiler v0.1 exists (subset) — full language still tree-walk, no native/static binary, no LLVM, no JIT

* Today: tree-walk interpreter (`internal/backend`, full language, 97 builtins) +
  bytecode subset (`internal/compiler`, `.ksb-1` JSON + stack VM):
  `fusion compile prog.ks [--out prog.ksb] [--dis] [--run]`, `fusion prog.ksb`.
  Compiled subset: literals/arrays/maps, `let`/`=`/`+=`-family, `+ - * / % **`/`== != < <= > >=`/`in`/`and/or/not`,
  calls (user funcs + `assert/len/range/str/int/float/type`), `a[i]`/`m.key`,
  `print/if/while/for-in/for-c/func/return/break/continue`.
  Explicitly rejected (run in interpreter): `go/chan/select`, `import`, `try/catch`,
  `switch`, `defer`, `sleep`, slices, `is`/`?.`/`??`, typed params/returns,
  closures capturing outer locals. VM limits: 7 user builtins, int `**` is O(n)
  (interpreter is O(log n)), `maxFrames 1024` / `maxSteps 20M`, `line N:` errors.
  `.kslib` stays source JSON (`kslib-1`), needs `fusion` on PATH.
  `fib(25)` ~100x slower than Go (was ~130x; lock-free
  single-threaded scopes + halved env allocs give ~10-15% on fib/loops).
  `sort` is now O(n log n) (was insertion O(n²): 5k reversed 0.45s→0.004s),
  interpreter `**`/`pow` O(log n) (was O(n)), `slice` copies only the window, builtins O(1) cached.
* Go level needs: full-language VM coverage first (concurrency, `import`/`try`/`switch`/`defer`,
  slices, `is`/`?.`/`??`, typed params), then `fusion build --bin` single static executable,
  cross-compile `--target linux/amd64,arm64,darwin,windows,wasm`, build cache, `go vet`-style IR check.
* Rust level needs: LLVM/opt backend or full bytecode VM + AOT, LTO, strip/symbol options,
  reproducible builds. Minimum viable: full-subset VM (5–20x speedup) first, then AOT.
* Planned: `docs/futures.md` P1 runtime (`VM → --bin → --target → --cpuprofile`); compiler v0.1
  (`compile --dis/--run`, `RunFile`, save/load roundtrip + disassembler tests) is step one.
* Score impact: `Perf 4→5 done (+1 tree-walk opts, Python parity)`; `Perf 5→8 (+3)` left
  for full VM/AOT, `Build 4→9 (+5)` (still `4` today: `.ksb` is portable bytecode, not a binary).

### 2. Gradual types (v2.1: half-closed) — annotations + `is`/`?.`/`??`/`ok`/`err`, still no structs/enums/generics

* Today (v2.1): optional `let x: int`, `func f(a: int): int`, `func` literals with
  types (nullable by default, `int?` accepted); `x is int` / `x is "int"` /
  `x is not "int"` (also `number`/`any`/`ok`/`err`); `a?.b` / `a?.[i]` (missing → `nil`);
  `a ?? b` (nil-coalescing, short-circuit); `ok(v)/err(e)` + `is_ok/is_err` +
  `unwrap/unwrap_or` + `is_type/assert_type`; `==` is still deep equality,
  arity still checked at call time (now with param-type checks).
* Go level still needs: structs, interfaces, generics, exhaustive `switch`,
  `vet` for unused/arity.
* Rust level still needs: real `Result/Option` exhaustiveness, enums + pattern
  matching, ownership-safe FFI boundaries (no full borrowck — explicit non-goal,
  Go GC stays).
* Planned: `futures.md` P1 language core (structs/enums) closes the rest.
* Score impact: `Types 4→6 done (+2, Python/Node parity)`; `Types 6→8 (+2)` left.

### 3. Concurrency (v2.1: Rust parity in interpreter) — `select` + `for v in chan` landed; VM has none yet, no `--race`/cancel yet

* Today (v2.1, interpreter): `go` + `chan(n)/send/recv/close` +
  `select { case v = recv(c) / recv(c) / send(c, v) / timeout(ms) / default }`
  (blocks until one case is ready, ready cases win uniformly at random like Go,
  `break` ends the `select`, `ch = nil` disables a case for fan-in drains) +
  `for v in ch` (drains until close, like Go's `range ch`) +
  `recv_timeout/send_timeout/chan_closed` + `try_send/try_recv/chan_len/chan_cap/sleep`.
  Compiler v0.1 rejects `go/chan/select/sleep` with a clear error (run those files in the interpreter).
* Go level still needs: `fusion run --race` (reuse Go race detector),
  structured workers/timeouts (`with_timeout`), buffered-chan spec docs.
* Rust level still needs: `send/sync`-like docs, cancel/context,
  deterministic test scheduler for `go` blocks.
* Planned: `futures.md` P1 (timeouts/context, scheduler) + namespaced imports.
* Score impact: `Concurrency 6→8 done (+2, Rust parity)`; `Concurrency 8→9 (+1)` left
  for `--race` + structured cancellation.

### 4. Small stdlib — no `http/net/socket`, no threads/process control

* Today: 97 builtins in the interpreter (strings/arrays/maps/JSON/files/math/time/rand,
  `map/filter/each/reduce/apply`, `ok/err` results, `chan_*`, `read_file/write_file/append_file/exists/list_dir/mkdir/remove_file/input/argv/env/exit`;
  verified by `grep -c '{Name:' internal/backend/backend.go`) — no
  `http_serve/get/post`, TCP/WS, `exec/pipes/signals`, `regex/crypto/db`.
  VM v0.1 subset only: `assert/len/range/str/int/float/type` (+ hidden `__iter_*` helpers).
* Go level needs: `net/http` (server + client), `fs` full (`stat/cp/mv/glob/watch`),
  `process.exec`, `time` formatting, `log/flags`, `testing` helpers.
* Rust level needs: `tokio`-like async IO story (or documented blocking + workers),
  `serde`-like JSON schema validation, `sqlite_*` → `postgres_*`, TLS.
* Planned: `futures.md` P1 stdlib list in that exact order.
* Score impact: `Stdlib 4→9 (+5)`.

### 5. No ecosystem — local file search only, no registry/resolver/LSP

* Today: `fusion.toml` + newest local `test-releases/<name>-<ver>.kslib` wins,
  no lockfile, no semver range, no `fmt/vet/bench/doc/repl/LSP/debugger/profiler` —
  but `fusion test` exists (`*_test.ks` + `assert`, TAP, per-file isolation), so
  Tooling is 5/10 (was 4 before the runner; `fmt`/`vet` still missing).
  New since v2.1 text: `fusion compile --dis/--run` + `fusion prog.ksb` (disassembler,
  save/load roundtrip) + `fusion test`.
* Go level needs: proxy-style registry + checksums, `fusion.lock`, `^/~ />=` resolver,
  `vendor/`, `fusion fmt/vet/test/bench/doc`, `cpuprofile`, VS Code ext.
* Rust level needs: `cargo publish/yank`, namespaces (`scope/name`), yank + audit,
  docs.rs-like docs, criterion-style benches.
* Planned: `futures.md` P0 tooling + P2 registry/DX.
* Score impact: `Ecosystem 3→8 (+5)`, `Tooling 5→9 (+4 left; test done)`, `Maturity 3→8 (+5)`.

### Go/Rust-level checklist (all things, with owner doc)

| # | Area | Go bar | Rust bar | .ks v2.1 + compiler v0.1 | Needed to close | Closes in |
|---|---|---|---|---|---|---|
| 1 | Compiler | `go build` static bin | `rustc` LLVM + LTO | tree-walk (full) + VM subset (`.ksb-1`, no `go`/`import`/`try`/`switch`/`defer`/slices/`is`/`?.`/`??`) | full VM → AOT `--bin` | `futures.md` P1 runtime |
| 2 | Targets | `GOOS/GOARCH` | tiers + WASM | none (`.ksb` is portable JSON, still needs `fusion`) | `--target` matrix + WASM | `futures.md` P1 runtime |
| 3 | Types | structs/interfaces/generics | traits/enums/`Result` | gradual `: type` + `is`/`?.`/`??` | structs/enums/generics | `futures.md` P1 core |
| 4 | Errors | multi-return | `Result/Option/?` | `ok/err` values + `error()`+`try/catch` | exhaustive `Result` checks | `futures.md` P0 done, P1 polish |
| 5 | Concurrency | `select`/race | `tokio`/rayon | `go/chan/select/timeout/for-in-chan` (interpreter; VM rejects) | `--race`, `with_timeout`, scheduler + VM concurrency | `futures.md` P0 done, P1 rest |
| 6 | Memory | GC + `sync` | borrowck | Go GC (ok) | document sharing rules, no borrowck | non-goal, docs only |
| 7 | Stdlib net | `net/http` | `std::net`+crates | none | `http_*`, `net/ws`+TLS | `futures.md` P1 stdlib |
| 8 | Stdlib OS | `os/exec` | `std::process` | `sleep/exit` only (+ files/`argv`/`env`/`input`) | `exec`/pipes/signals, full `fs` | `futures.md` P1 stdlib |
| 9 | Data | `encoding/*` | `serde` | `json_*` only | schema validation, `regex`, `crypto`, `sqlite/postgres` | `futures.md` P1 stdlib |
| 10 | Packages | proxy + `go.sum` | crates.io + lock | local newest-wins (`.kslib`); `.ksb` is per-file, not a package | registry + `fusion.lock` + semver + vendor | `futures.md` P0+P2 |
| 11 | Tooling | `fmt/vet/test/bench/pprof` | `clippy/fmt/bench` | `new/run/build/launch` + `compile --dis/--run` (subset) + `test` (`*_test.ks`, TAP) | `fmt/vet/test/bench/doc/repl` | `futures.md` P0+P2 DX |
| 12 | IDE | `gopls` | `rust-analyzer` | none | LSP + VS Code ext + debugger | `futures.md` P2 DX |
| 13 | Frontend | `html/template`/WASM | WASM pkgs | console `frontend/` | `--web` reload + `--js` subset + React/Vite/Next.js pattern | `futures.md` P2 frontend |
| 14 | FFI | `cgo` | `unsafe`/FFI | none | opt-in `ffi_*` + Go plugin API | `futures.md` P2 interop |
| 15 | Stability | compat promise | editions | v2.0 | RFC process + semver + LTS | `futures.md` §5 |

Close rows 1–2 + 5 + 10–11 and the rest of rows 3–4 and `.ks` moves `49 → ~75–80/100` (Go/Rust-class for scripts/services).
Rows 6/14 stay intentionally different (GC stays, `unsafe` stays opt-in).

## Decision guide

1. Browser UI? → React + Vite + TS, or Next.js for SSR.
2. CRUD + login + billing next week? → Laravel / Django / Rails / Next.js.
3. 100k RPS / embedded / game loop? → Go / Rust / C++ / C.
4. Matrices / science / simulations? → Julia (or Python + numpy if ecosystem matters more).
5. Script, bot, rule engine, teaching `go/chan/select`, prototype? → `.ks` (interpreter).
   Pure arithmetic/control-flow/funcs with no concurrency? → try `fusion compile --run` (VM subset).
6. Need `http/DB` in `.ks` today? → shell out or wait — see `docs/futures.md` P1 stdlib.

## Honest limits of `.ks` v2.1 + compiler v0.1 + frontend P0 (do not hide)

* Full language is tree-walk interpreted, no JIT/native binary, no cross-compile matrix
  (v2.1 tree-walk opts: lock-free scopes, halved env allocs, O(n log n) sort,
  O(log n) pow, string+string fast path — fib still ~100x slower than Go).
  Compiler v0.1 narrows but does not close this: `.ksb-1` is portable JSON run by
  `fusion` (not a static binary), subset-only, 7 builtins, int `**` O(n) in the VM.
* Gradual types only (no structs/enums/generics yet), `==` uses deep equality.
  Compiler v0.1 rejects `: type` annotations and `is`/`?.`/`??` (interpreter-only).
* Flat lib namespace (prefix functions), newest local bundle wins, no lockfile.
  `.ksb` is per-file bytecode, not a package format (that stays `.kslib`).
* No `fusion run --race`, no cancel/`with_timeout` yet, no HTTP/WS/DB/regex/crypto stdlib.
  No `go/chan/select/sleep` in compiled output yet.
* `frontend/` is not web yet — P0 gives route table + view-models + console renderer,
  no DOM, no CSS, no SSR/HMR. See `plan/frontend.md` P1–P10 for what moves Frontend 3→10.
* Version: toolchain reports `v2.1` (`fusion version`, `fusion help`, `toolVersion` in
  `cmd/fusion/main.go` — single constant, keep in sync). `release/fusion` may still
  predate `compile` — rebuild from source (`go build -o fusion ./cmd/fusion`).
