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

## Language-by-language

### vs Go (implementation language)

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

Rust gives ownership, `Result/Option`, `cargo` registry, zero-cost abstractions.
`.ks` copies the `cargo` UX (`fusion new --lib`, `fusion build --release`,
`test-releases/` like `target/release/`) but bundles are source JSON, imports are
flat globals, errors are `error(msg)` + `try/catch`.

Pick Rust for perf-critical, safety-critical, WASM libs.
Pick `.ks` for Day-1 productivity without borrow checker.

### vs C

C gives pointers, manual `malloc/free`, direct syscalls, tiny runtimes.
`.ks` gives `array/map/string` + GC (from Go) + bounds-checked indexing.

Pick C for kernels, drivers, microcontrollers.
Pick `.ks` for everything where `segfault` is unacceptable.

### vs C++

C++ gives RAII, templates, classes, deterministic destruction, huge game/engine libs.
`.ks` gives `func` closures + `defer` + duck-typed maps instead of classes.

Pick C++ for engines, CAD, HFT, Unreal/Qt.
Pick `.ks` for config-driven logic on top of those engines.

### vs Node.js

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

Closest feel: `let x = 10`, `for i in range(5)`, `a[1:3]`, `and/or/not`,
truthiness (`nil false 0 0.0 "" [] {}` falsy), `map/filter/reduce`.
Difference: `.ks` adds `go/chan/defer/switch` and braces; Python has huge stdlib
(`requests`, `numpy`, `django`) while `.ks` has files/JSON/strings only.

Pick Python for data/AI/science/ops (ecosystem wins).
Pick `.ks` for learning concurrency early or embedding a tiny Go-based runtime.

### vs Next.js (framework, not language)

Category error if compared 1:1. Next.js = React + routing + SSR/ISR + Node runtime.
ks-fusion app = `backend/main.ks + frontend/main.ks` run concurrently in console.

* Today: use Next.js for real browser UI. Use `.ks` backend as JSON worker
  (`read_file` → `json_stringify` → stdout) called from an API route.
* Future (`docs/futures.md`): `fusion build --js` subset + `fusion run --web` hot reload.

Pick Next.js for SEO sites, dashboards, SaaS UI.
Pick `.ks` for the logic worker behind it.

### vs PHP Laravel

Laravel gives routing, ORM/Eloquent, migrations, Blade, queues, auth scaffolding.
`.ks` gives none of that yet — no HTTP server, no DB driver, no templates.

Pick Laravel for monolith CRUD + admin + auth in hours.
Pick `.ks` for sidecar scripts (data munging, checks, bots) next to Laravel.

### More (short)

* **TypeScript/Deno/Bun:** pick for typed Node + secure sandbox; `.ks` is simpler but smaller.
* **Java/Kotlin/Spring:** pick for enterprise monoliths, JPA, strict OOP; `.ks` for glue.
* **Lua:** closest embed rival — Lua is smaller/faster to embed; `.ks` has Go chans + `fusion` CLI out of box.
* **Ruby/Rails:** pick for convention CRUD; `.ks` syntax will feel familiar.
* **Bash:** pick for 5-line pipes; `.ks` wins past 50 lines (`try/catch`, maps, JSON, Windows portability).

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
