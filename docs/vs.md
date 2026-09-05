# ks-fusion vs Others

> ks-fusion `v2.0`: dynamic interpreted `.ks` language, toolchain written in Go.
> Easy like Python, concurrency like Go, packaging like Rust.
> This doc is honest about where `.ks` wins and where it loses.

## TL;DR

| If you need… | Pick… | Why not `.ks` yet |
|---|---|---|
| Single static binary, max RPS, strict types | Go / Rust | `.ks` is tree-walk interpreted, dynamic, ~5x slower on `fib(25)` |
| Kernel, drivers, games, hard realtime | C / C++ / Rust | No manual memory, no pointers, no SIMD |
| Browser UI / React / SSR | Next.js (TS) | `frontend/main.ks` is console logic today, not DOM |
| CRUD + auth + admin panel tomorrow | PHP Laravel / Python Django | No ORM, migrations, HTTP server stdlib yet |
| Quick scripts, rules, gluing, learning | ks-fusion | — this is the sweet spot |
| npm / PyPI ecosystem | Node.js / Python | `.ks` has ~80 builtins + local `.kslib` only |

## Big table

|  | ks-fusion (.ks) | Go | Rust | C | C++ | Node.js (JS/TS) | Python | Next.js | PHP Laravel |
|---|---|---|---|---|---|---|---|---|---|
| Model | interpreted tree-walk (Go) | compiled, GC | compiled, no GC (borrowck) | compiled, manual | compiled, RAII | V8 JIT | interpreted (+C ext) | React framework on Node | interpreted + framework |
| Typing | dynamic: `nil bool int float string array map func chan` | static, interfaces, generics | static, traits, enums, `Result/Option` | weak static, pointers | static, templates, classes | dynamic + optional TS | dynamic + hints | TS-typed components | dynamic |
| Perf | low-medium (scripts, bots, CLIs) | high (servers) | highest (systems) | highest | highest | medium-high (I/O) | medium (glue/AI) | medium (SSR) | medium (CRUD) |
| Concurrency | `go + chan/send/recv/close` (goroutines underneath, no `select` yet) | goroutines + `select` | `async/tokio`, threads | threads, manual | threads/`async` | event loop + workers | threads/GIL + `asyncio` | server/client components | processes + queues |
| Packaging | `fusion.toml` + `.kslib` JSON (`kslib-1`), local `test-releases/`/`target/` | `go.mod` + proxy | `cargo` + crates.io | make/cmake | cmake/vcpkg/conan | npm/pnpm | pip/poetry | npm + Vercel | composer + artisan |
| Binary | needs `fusion` on PATH (shebang), no `--bin` yet | single static binary | single binary | binary | binary | needs node/runtime | needs python | needs node | needs php+server |
| Best for | learning, automation, rules engines, small backends | APIs, DevOps, cloud | systems, WASM, games | OS, embedded | engines, trading, desktop | APIs, realtime, SSR | scripts, data, AI | fullstack React apps | monolith CRUD apps |

See `docs/futures.md` for what closes each gap (`--bin`, `select`, `http_*`, registry, `--js`).

## How scoring works (out of 100)

Each language / stack is scored `0-10` on 10 dimensions = `100` max.
Snapshot for ks-fusion `v2.0`. Higher = better, except simplicity where
easier = higher. Scores are opinionated but rubric-based, not benchmarks.

Dimensions: `Perf + Types + Concurrency + Stdlib + Ecosystem + Tooling +
Simplicity + Build/Deploy + Frontend + Maturity = 100`.

## Scores — total out of 100

| Dim (max 10) | .ks | Go | Rust | C | C++ | Node | Python | Next.js | Laravel |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| Perf | 4 | 8 | 10 | 10 | 10 | 7 | 5 | 7 | 5 |
| Types | 4 | 8 | 10 | 5 | 8 | 6 | 6 | 8 | 5 |
| Concurrency | 6 | 9 | 8 | 5 | 7 | 7 | 5 | 6 | 4 |
| Stdlib | 4 | 9 | 8 | 6 | 8 | 8 | 10 | 8 | 8 |
| Ecosystem | 3 | 8 | 8 | 6 | 7 | 10 | 10 | 10 | 8 |
| Tooling | 4 | 9 | 9 | 7 | 8 | 9 | 8 | 9 | 8 |
| Simplicity | 9 | 7 | 5 | 4 | 4 | 7 | 10 | 6 | 8 |
| Build/Deploy | 4 | 10 | 9 | 8 | 8 | 6 | 5 | 7 | 6 |
| Frontend | 3 | 5 | 6 | 2 | 4 | 8 | 5 | 10 | 7 |
| Maturity | 3 | 9 | 8 | 9 | 9 | 9 | 10 | 8 | 8 |
| **Total /100** | **44** | **82** | **81** | **62** | **73** | **77** | **74** | **79** | **67** |

Extra stacks (same rubric): `TypeScript/Deno/Bun 77`, `Java/Kotlin/Spring 78`,
`Lua 58`, `Ruby/Rails 68`, `Bash 45`. Details in `More` below.

## Language-by-language

### vs Go (implementation language)

**Score: ks-fusion 44/100 vs Go 82/100 — Go wins by 38.**

Same ideas: `go func(){...}()`, `chan(1)`, `send/recv/close`, `defer` LIFO.
Difference: Go is compiled + statically typed; `.ks` is interpreted + dynamic.

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

**Score: ks-fusion 44/100 vs Rust 81/100 — Rust wins by 37.**

Rust gives ownership, `Result/Option`, `cargo` registry, zero-cost abstractions.
`.ks` copies the `cargo` UX (`fusion new --lib`, `fusion build --release`,
`test-releases/` like `target/release/`) but bundles are source JSON, imports are
flat globals, errors are `error(msg)` + `try/catch`.

Pick Rust for perf-critical, safety-critical, WASM libs.
Pick `.ks` for Day-1 productivity without borrow checker.

### vs C

**Score: ks-fusion 44/100 vs C 62/100 — C wins by 18.**

C gives pointers, manual `malloc/free`, direct syscalls, tiny runtimes.
`.ks` gives `array/map/string` + GC (from Go) + bounds-checked indexing.

Pick C for kernels, drivers, microcontrollers.
Pick `.ks` for everything where `segfault` is unacceptable.

### vs C++

**Score: ks-fusion 44/100 vs C++ 73/100 — C++ wins by 29.**

C++ gives RAII, templates, classes, deterministic destruction, huge game/engine libs.
`.ks` gives `func` closures + `defer` + duck-typed maps instead of classes.

Pick C++ for engines, CAD, HFT, Unreal/Qt.
Pick `.ks` for config-driven logic on top of those engines.

### vs Node.js

**Score: ks-fusion 44/100 vs Node.js 77/100 — Node wins by 33.**

Node gives V8, `npm` (2M+ packages), `fetch/http`, event loop, TypeScript.
`.ks` gives simpler blocking `recv/sleep` + ~80 sync builtins, no `http_*` yet.

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

**Score: ks-fusion 44/100 vs Python 74/100 — Python wins by 30.**

Closest feel: `let x = 10`, `for i in range(5)`, `a[1:3]`, `and/or/not`,
truthiness (`nil false 0 0.0 "" [] {}` falsy), `map/filter/reduce`.
Difference: `.ks` adds `go/chan/defer/switch` and braces; Python has huge stdlib
(`requests`, `numpy`, `django`) while `.ks` has files/JSON/strings only.

Pick Python for data/AI/science/ops (ecosystem wins).
Pick `.ks` for learning concurrency early or embedding a tiny Go-based runtime.

### vs Next.js (framework, not language)

**Score: ks-fusion 44/100 vs Next.js 79/100 — Next.js wins by 35 (different category).**

Category error if compared 1:1. Next.js = React + routing + SSR/ISR + Node runtime.
ks-fusion app = `backend/main.ks + frontend/main.ks` run concurrently in console.

* Today: use Next.js for real browser UI. Use `.ks` backend as JSON worker
  (`read_file` → `json_stringify` → stdout) called from an API route.
* Future (`docs/futures.md`): `fusion build --js` subset + `fusion run --web` hot reload.

Pick Next.js for SEO sites, dashboards, SaaS UI.
Pick `.ks` for the logic worker behind it.

### vs PHP Laravel

**Score: ks-fusion 44/100 vs Laravel 67/100 — Laravel wins by 23.**

Laravel gives routing, ORM/Eloquent, migrations, Blade, queues, auth scaffolding.
`.ks` gives none of that yet — no HTTP server, no DB driver, no templates.

Pick Laravel for monolith CRUD + admin + auth in hours.
Pick `.ks` for sidecar scripts (data munging, checks, bots) next to Laravel.

### More (short, all scored out of 100)

* **TypeScript/Deno/Bun 77/100 vs .ks 44/100 (-33):** pick for typed Node + secure sandbox; `.ks` is simpler but smaller.
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
| 4 | Java/Kotlin/Spring | 78 | +34, enterprise |
| 5 | Node.js | 77 | +33, APIs / npm |
| 5 | TypeScript/Deno/Bun | 77 | +33, typed runtime |
| 7 | Python | 74 | +30, data/AI/ecosystem |
| 8 | C++ | 73 | +29, engines/trading |
| 9 | Ruby/Rails | 68 | +24, convention CRUD |
| 10 | PHP Laravel | 67 | +23, monolith CRUD |
| 11 | C | 62 | +18, kernels/embedded |
| 12 | Lua | 58 | +14, embedding |
| 13 | Bash | 45 | +1, tiny pipes |
| 14 | **ks-fusion v2.0** | **44** | **baseline — wins on simplicity (9/10)** |

Grand total (sum of all 14 totals) = `965 / 1400`, average `68.9/100`.
`.ks` total `44/100` reflects v2.0 reality: best at learning/scripts,
behind everywhere else until `futures.md` P0/P1 land.

## Decision guide

1. Browser UI? → Next.js.
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
