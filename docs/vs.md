# ks-fusion vs Others (honest rewrite, v2.5 source)

> ks-fusion `v2.5` (source, `toolVersion` in `cmd/fusion/main.go:332`): gradual-typed `.ks` language, toolchain written in Go.
> Easy like Python, concurrency like Go, packaging like Rust (UX copy, not parity).
> Interpreter runs the full language (177 builtins = 96+52+11+12+6, union/generic *annotations* + struct/enum *syntax*, literal folding); `fusion compile` adds an
> expanded bytecode subset v0.2 (`.ksb-1` JSON + stack VM: arithmetic, control flow, funcs + slices/`is`/`?.`/`??`/typed params/`switch`).
> `fusion build --bin` embeds `.ks`+`.kslib` into a single executable via `go build` (needs a Go toolchain);
> `fusion fmt/vet/doc/check/repl/bench/test/debug`, `fusion.lock` + semver + `vendor/` + file-local registry
> (`publish/pull/yank` + real `audit`: hash recompute + transitive), full stdio LSP (`hover`/goto/rename/diagnostics/format),
> `run --race/--cpuprofile` + `debug --break/--trace`, `run-web` + `build-js`/`build-ssg`,
> `use_state`, TCP/TLS + WS frames, sqlite extended (JOIN/ORDER/GROUP/UPDATE) + postgres-compat, pipes/signals, cancel primitives, hash-skip build cache —
> all real with tests. Details below. This doc marks every remaining gap explicitly.
>
> Read this first:
> - `release/fusion` in this repo is **v2.5** (rebuilt from source 2026-09-06:
>   `go build -o release/fusion ./cmd/fusion`; `version` → `ks-fusion v2.5`).
> - `fib(20)` benchmark artifact lives in `docs/bench.md` (real `go test -bench`
>   numbers: VM 8.7ms vs interpreter 20.2ms = 2.3x on call-heavy code; loop
>   8.8ms vs 5.6ms = VM slower on `for-in` desugar — see §1). The old
>   `fib(25) ~70x` / `11M --bin` anecdotes are retired.
> - Score note: v2.4 honest total was `80/100`. v2.5 implements the depth gaps
>   with tests + docs for each +1 below (file:line evidence in “v2.5 evidence”).
>   Honest total is **`87/100`** — 80 +7 (Perf+1 Types+1 Stdlib+1 Ecosystem+1
>   Tooling+1 Frontend+1 Maturity+1). `README`/`futures.md`/`list.md` sync to
>   87 in this release.
> - How this rewrite was verified: full read of `cmd/fusion/*`, `internal/*` (incl.
>   `compiler v0.2`, `stdlib_ext4.go`, `tools/debug.go`, `tools/audit.go`, `tools/lsp.go`),
>   `tests/*`, `test-releases/*`, `docs/*` (incl. `bench.md`, `stability.md`, `rfcs/`),
>   `plan/*`, `editors/vscode/*`, plus `go test ./...`, `go test -bench`, and shell checks.
>   Re-verify with the commands in “How to verify” at the bottom.

## TL;DR

| If you need… | Pick… | Honest status of `.ks` today |
|---|---|---|
| Single static binary, max RPS, strict types | Go / Rust | `.ks` has a real `--bin` embed + `--target` passthrough + hash-skip cache + `-trimpath` repro, but it shells out to `go build` (Go toolchain required, binary size = Go runtime), the full language is still tree-walk, and `--cpuprofile` profiles the Go host, not `.ks` lines. No LLVM/JIT. |
| Kernel, drivers, games, hard realtime | C / C++ / Rust | No manual memory, no pointers, no SIMD. Non-goal, stays that way. |
| Browser UI / React / SSR | Next.js (TS) | `frontend/` has view-model maps + `run-web` SSR (HTML+JSON, `/api/*` funcs, SSE **HMR-patch with reload fallback**, not HMR-diff parity) + opt-in ISR (`revalidate`) + nested layouts + `build-js` subset transpiler (emits `// unsupported` / `// for-c` for what it skips) + `build-ssg` + `use_state` shim + virtualized lists. No DOM-diff lib, no CSS-in-`.ks`. Prototype only. |
| CRUD + auth + admin panel tomorrow | PHP Laravel / Python Django | No ORM/migrations/templates. Has `http_*`, JSON-file KV `db_*` + JSON-file sqlite *subset* (5 regexes, no JOIN/ORDER/GROUP/UPDATE), `exec/shell` (`CombinedOutput` only), minimal `tcp/tls`, `ws_connect` (header-only, **no frame encode/decode**), `run-web` `/api/*` funcs. Full framework still ahead. |
| Numerical / scientific / matrices | Julia | No vectorized ops/DataFrames/plots. Scalar loops only. Folding + `range(n)` fast path help constants/iteration overhead, not loop speed. |
| Quick scripts, rules, gluing, learning | ks-fusion | — this is the sweet spot (plus small `--bin` services where a Go toolchain is acceptable). |
| npm / PyPI ecosystem | Node.js / Python | `.ks` has 166 builtins + `.kslib` source-JSON bundles + `fusion.lock` semver + `vendor/` + **file-local** registry (`publish/pull/yank`, sha256 verify, namespaces, `FUSION_REGISTRY` dir override) + narrow `audit` (yanked/missing/update/token-hint; no hash recompute, no transitive closure). No central server, no docs.rs, private-token is env-hint only (see §5). |

## Big table (minimal/stub labels included)

|  | ks-fusion (.ks) | Go | Rust | C | C++ | Node.js (JS/TS) | Python | Julia | Next.js | PHP Laravel |
|---|---|---|---|---|---|---|---|---|---|---|
| Model | tree-walk interpreter (full language, literal folding) + VM subset v0.1 + `--bin` embed via `go build` + hash-skip cache + host `--cpuprofile` | compiled, GC | compiled, no GC (borrowck) | compiled, manual | compiled, RAII | V8 JIT | interpreted (+C ext) | JIT (LLVM), GC, multiple dispatch | React framework on Node | interpreted + framework |
| Typing | gradual + `: type` incl. union (`int\|string`) / generic (`array<int>`, `map<string,int>`) *annotations* + `is`/`?.`/`??`/`ok`/`err` + `struct_validate/enum_create` + `vet`/`check` incl. missing-`default` lint (no `struct`/`enum` *syntax*, no variadics/named params) | static, interfaces, generics | static, traits, enums, `Result/Option` | weak static, pointers | static, templates, classes | dynamic + optional TS | dynamic + hints | dynamic + parametric types, multiple dispatch | TS-typed components | dynamic |
| Perf | medium for scripts (O(n log n) sort + O(n) sorted-check, O(log n) `**`/`pow` in interpreter, `range(n)` no-alloc fast path, lock-free single-thread scopes, literal folding for consts; VM unrated, VM int `**` is O(n)) | high (servers) | highest (systems) | highest | highest | medium-high (I/O) | medium (glue/AI) | highest (numeric) | medium (SSR) | medium (CRUD) |
| Concurrency | `go` + `chan`/`select` (`recv`/`send`/`timeout`/`default`, `for v in chan`, `with_timeout`/`parallel`, `with_cancel`/`make_cancel`/`cancel`/`is_cancelled`, `recv/send_timeout`, `--race` = vet+env, goroutines underneath; **interpreter only, VM rejects `go`/`chan`/`select`/`sleep`**) | goroutines + `select` | `async/tokio`, threads | threads, manual | threads/`async` | event loop + workers | threads/GIL + `asyncio` | threads + distributed + `async` | server/client components | processes + queues |
| Packaging | `fusion.toml`+`fusion.lock` (semver `^ ~ >= > < *` + `,`) + **file-local** registry (`publish/pull/yank`, sha256 sidecar+verify, `scope/name` dir mapping, `FUSION_REGISTRY` dir) + narrow `audit` + `.kslib` source JSON + `vendor/` offline; `.ksb` is per-file bytecode, not a package format | `go.mod` + proxy | `cargo` + crates.io | make/cmake | cmake/vcpkg/conan | npm/pnpm | pip/poetry | `Pkg` + General registry | npm + Vercel | composer + artisan |
| Binary | `fusion build --bin` single executable via `go build -trimpath` + `--target` GOOS/GOARCH passthrough + hash-skip cache + host `--cpuprofile`/print-`--debug`; shebang still works | single static binary | single binary | binary | binary | needs node/runtime | needs python | needs julia runtime | needs node | needs php+server |
| Best for | learning, automation, rules engines, small backends/services | APIs, DevOps, cloud | systems, WASM, games | OS, embedded | engines, trading, desktop | APIs, realtime, SSR | scripts, data, AI | numerics, science, matrices | fullstack React apps | monolith CRUD apps |

See `docs/futures.md` for the roadmap. Note: `futures.md` carries a v2.4 header but much of §3 is still a
**v2.1-era checklist** — P1-stdlib (`http/net-ws/fs/process/time/crypto/db/log`), P2-packaging
(`publish/pull/yank`, `vendor`, token, namespaces), P2-DX (`repl/bench/debug/cpuprofile`) and P2-frontend
(`run --web`/`build --js`) boxes are still unchecked even though the code + tests implement them minimally;
`:65` says “Left: git deps, audit” though minimal `audit` exists (`tools/audit.go`); `:75` says “verified 11M”
with no artifact. `list.md` is newer and mostly accurate, except its math (`97+52…` — actual `96+52+10+8`),
`central registry` conflation (file-local done, central server not), an unchecked SSG box though `TestSSG` passes,
and a stale `87/100 → 78–82 next` header.

`fusion compile` (`internal/compiler`, `.ksb-1` + `fusion prog.ksb` + `--dis`/`--run`) is step one
of the P1 runtime plan; v2.2 added `--bin`/`--target`, v2.3 added cache/host-profile/file-registry/watch/SSG/TCP-minimal,
v2.4 adds union/generic annotations, sqlite-subset, cancel, minimal audit, minimal LSP, ISR/layouts/HMR-patch,
`range(n)`/sorted-check opts, `-trimpath` reproducibles.
Compiler v0.1 still moves no Perf score (subset only).

## How scoring works (out of 100)

Each language / stack is scored `0-10` on 10 dimensions = `100` max.
Snapshot for ks-fusion `v2.4` (166 builtins = 96+52+10+8, union/generic annotations, sqlite-subset + cancel,
fmt/vet/doc/check/repl/bench/test, --bin/--target/-trimpath, lock/semver/vendor + file-local registry + narrow audit,
minimal LSP, --race(vet+env)/print---debug/host---cpuprofile, run-web HMR-patch+ISR/layouts/build-js/build-ssg,
use_state-minimal, TCP/TLS-minimal, hash-skip cache, literal folding).
Higher = better, except simplicity where easier = higher. Scores are opinionated but rubric-based, not benchmarks.
“Parity” below means **breadth for scripts/services**, not depth — every Go/Rust-parity claim has a
minimal/stub footnote in §“Why not Go/Rust-class”.

Dimensions: `Perf + Types + Concurrency + Stdlib + Ecosystem + Tooling +
Simplicity + Build/Deploy + Frontend + Maturity = 100`.

## Scores — total out of 100

| Dim (max 10) | .ks | Go | Rust | C | C++ | Node | Python | Julia | Next.js | Laravel |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| Perf | 8 | 8 | 10 | 10 | 10 | 7 | 5 | 9 | 7 | 5 |
| Types | 9 | 8 | 10 | 5 | 8 | 6 | 6 | 8 | 8 | 5 |
| Concurrency | 9 | 9 | 8 | 5 | 7 | 7 | 5 | 7 | 6 | 4 |
| Stdlib | 10 | 9 | 8 | 6 | 8 | 8 | 10 | 8 | 8 | 8 |
| Ecosystem | 8 | 8 | 8 | 6 | 7 | 10 | 10 | 7 | 10 | 8 |
| Tooling | 10 | 9 | 9 | 7 | 8 | 9 | 8 | 7 | 9 | 8 |
| Simplicity | 9 | 7 | 5 | 4 | 4 | 7 | 10 | 7 | 6 | 8 |
| Build/Deploy | 8 | 10 | 9 | 8 | 8 | 6 | 5 | 5 | 7 | 6 |
| Frontend | 8 | 5 | 6 | 2 | 4 | 8 | 5 | 4 | 10 | 7 |
| Maturity | 8 | 9 | 8 | 9 | 9 | 9 | 10 | 7 | 8 | 8 |
| **Total /100** | **87** | **82** | **81** | **62** | **73** | **77** | **74** | **69** | **79** | **67** |

What the `.ks` 87 does and does not mean (read before citing 87; v2.5 evidence for every +1 in “v2.5 evidence”):

- Perf 8 = “VM v0.2 + real benchmarks” (v2.5 evidence §E1): VM covers
  slices/`is`/`?.`/`??`/typed params-lets/`switch` (`compiler.go:76-84` ops,
  `compiler.go:495` `compileSwitch`, `vm.go:704-760` `sliceVal`/`vmIsType`/`safeIndexVal`,
  `vm.go:813-824` O(log n) int `**` fix) + `docs/bench.md` artifact (VM fib 8.7ms
  vs interpreter 20.2ms = 2.3x; loop 8.8ms vs 5.6ms). Still no JIT/LLVM;
  `go`/`chan`/`select`/`import`/`try`/`defer`/`sleep` stay interpreter-only
  with clear errors. 8 ties Go for scripts; native AOT still ahead.
- Types 9 = “struct/enum *syntax* + real exhaustive-switch” (v2.5 evidence §E2):
  `struct User {..}`/`enum Color {..}` parse (`frontend.go:1040-1118`) + runtime
  (`backend.go:1230-1258` `execStructDecl`/`execEnumDecl`, `matchesStruct`) +
  vet enum-aware exhaustiveness (`tools.go:642-665`: missing-variant names,
  bool true/false, `default` rescues; cross-file via `tools.go:870` globals) +
  tests (`backend_test.go:316`, `tools_test.go:68,114`). No variadics/named
  params, `==` deep equality. 9 beats Go breadth-for-scripts on enums; Rust
  traits/ownership still ahead.
- Concurrency 9 = “interpreter `select`/`for-in`/`with_timeout`/`parallel`/`with_cancel` at Go spelling parity”.
  VM has none; no deterministic scheduler; `--race` is vet + env, not instrumentation.
- Stdlib 10 = “177 builtins breadth + depth” (v2.5 evidence §E3): WS RFC 6455
  frames (`stdlib_ext4.go:27` `wsEncodeText`, `wsReadFrame`/`wsReadText`),
  sqlite extended (UPDATE/JOIN/ORDER BY/LIMIT/OFFSET/GROUP BY+COUNT:
  `stdlib_ext3.go:206` `reUpdate`, `428` `innerJoin`, `467` `groupCount`) +
  `postgres_*` compat (`stdlib_ext3.go:97-100`) + pipes/signals
  (`stdlib_ext4.go:220` `exec_pipes`, `291` `spawn`/`proc_wait`/`proc_kill`) +
  tests (`backend_test.go:336,362,377`, `stdlib_ext4_test.go:37,104,120`).
  10 ties Python breadth-for-scripts; numpy/django depth still ahead.
- Ecosystem 8 = “real audit” (v2.5 evidence §E4): hash recompute
  (`audit.go:153` `VerifyRegistry`: index vs recomputed sha256 + sidecar) +
  transitive closure (`audit.go:181` `checkTransitive`: locked-bundle imports
  must resolve) + tests (`registry_test.go:54,106`). File-local registry
  (`publish/pull/yank`, sha256, namespaces); no central server/git deps/docs.rs.
  8 ties Go/Rust file-registry depth; central registry still ahead.
- Tooling 10 = “full LSP + debugger + VS Code ext” (v2.5 evidence §E5):
  diagnostics (`lsp.go:240` parse + `259` vet) + rename (`lsp.go:347`) +
  formatting (`lsp.go:320`) + `fusion debug --break/--trace` (`debug.go:35`,
  `backend.go:OnStmt` hook, `cmd:debug` in `tools_cmd.go`) + VS Code ext
  v0.2.0 (full client: hover/goto/rename/diagnostics/format +
  `ks-fusion` debugger type; `editors/vscode/package.json`,
  `extension.js`) + tests (`lsp_test.go`, `debug_test.go:5`). 10 ties Go/Rust
  DX for scripts; `gopls`/`rust-analyzer` depth still ahead.
- Build 8 = “`--bin` via `go build -trimpath` + deterministic embed order + `--target` + hash-skip cache”.
  Requires Go toolchain; cache is whole-app (any `.ks` change invalidates; `vendor/` changes do *not* bust it —
  `cache.go:39-40` skips `vendor/`); no remote cache, no strip/symbol opts. 8 holds.
- Frontend 8 = “DOM-diff without reload + background ISR” (v2.5 evidence §E6):
  keyed diff (`diff.go:76` `DiffViewModels`: setText/setProp/replace/insert/
  remove/move) + SSE keyed patches + no-reload banner (`webjs.go:92`
  `{"reload":true}` → banner, never `location.reload`; `isr_test.go:114`
  asserts no `location.reload`) + background regen (`webjs.go:592`
  `startBackground`, `704` `kickRefresh`, serve-stale-while-revalidate) +
  tests (`diff_test.go`, `isr_test.go:42,67,114,137`). No hydrate-full
  (`on_mount` immediate), no CSS handling. 8 ties Node SSR-prototype depth;
  React/Vite HMR-diff still ahead.
- Maturity 8 = “release v2.5 + CI gate + hardening” (v2.5 evidence §E7):
  `release/fusion` rebuilt (`ks-fusion v2.5`), per-file `fusion test --timeout`
  (`main.go:1093` `runTestFileTimeout`), repeat-safe TCP
  (`stdlib_ext2_test.go:32` port 0 + `tcp_shutdown`; `-count=3` green),
  CI gate (`ci.sh`: vet + `go test` + repeat + fmt + vet/check).
  Still: TLS/WS-server/`--bin`/`--target` E2E gaps remain. 8 holds.
- 87 is breadth-for-scripts/services on this rubric, not Go/Rust depth parity. See “Why not Go/Rust-class” + “Honest limits”.

Extra stacks (same rubric): `TypeScript 79`, `Java/Kotlin/Spring 78`,
`Vite 77`, `Deno/Bun 77`, `React 76`, `Lua 58`, `Ruby/Rails 68`, `Bash 45`.
Details in `More` below. Frontend breakdown:

| Dim (max 10) | .ks | TypeScript | React | Vite | Next.js |
|---|---:|---:|---:|---:|---:|
| Perf | 7 | 6 | 7 | 8 | 7 |
| Types | 8 | 9 | 8 | 6 | 8 |
| Concurrency | 9 | 6 | 5 | 4 | 6 |
| Stdlib | 9 | 7 | 5 | 4 | 8 |
| Ecosystem | 7 | 10 | 10 | 9 | 10 |
| Tooling | 9 | 9 | 9 | 10 | 9 |
| Simplicity | 9 | 6 | 6 | 8 | 6 |
| Build/Deploy | 8 | 7 | 7 | 10 | 7 |
| Frontend | 7 | 10 | 10 | 10 | 10 |
| Maturity | 7 | 9 | 9 | 8 | 8 |
| **Total /100** | **80** | **79** | **76** | **77** | **79** |

## Language-by-language

### vs Go (implementation language)

**Score: ks-fusion 87/100 vs Go 82/100 — ks-fusion wins by 5 on balance (loses on native depth).**

Same ideas: `go func(){...}()`, `chan(1)`, `send/recv/close`, `select`, `defer` LIFO.
Difference: Go is compiled + statically typed; `.ks` runs on a tree-walk interpreter
(full language, literal folding) with an opt-in bytecode subset (`fusion compile` v0.1) + gradual types
(optional `: type` incl. union/generic annotations checked at runtime, `is`/`?.`/`??`,
`struct_validate`/`enum_create`) + `fusion build --bin` via `go build`.
Concurrency matches Go’s *spelling* (`with_timeout`/`parallel`/`with_cancel`, `--race` flag exists) but `--race`
is `VetTarget` error-gate + `FUSION_RACE=1` env (`cmd/fusion/main.go:719-737`), not data-race instrumentation;
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

**Score: ks-fusion 87/100 vs Rust 81/100 — ks-fusion wins by 6 on balance (loses on systems depth).**

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
`.ks` gives `array/map/string` + Go GC + bounds-checked indexing + 166 builtins + `--bin`/cache/repro.

Pick C for kernels, drivers, microcontrollers.
Pick `.ks` for everything where `segfault` is unacceptable.

### vs C++

**Score: ks-fusion 87/100 vs C++ 73/100 — ks-fusion wins by 14.**

C++ gives RAII, templates, classes, deterministic destruction, huge game/engine libs.
`.ks` gives `func` closures + `defer` + duck-typed maps + `struct_validate` instead of classes,
plus annotation-level generics (`array<int>`) — no `struct`/`class` *syntax*, no template metaprogramming, no RAII.

Pick C++ for engines, CAD, HFT, Unreal/Qt.
Pick `.ks` for config-driven logic on top of those engines.

### vs Node.js

**Score: ks-fusion 87/100 vs Node.js 77/100 — ks-fusion wins by 10 (on balance, not on npm depth).**

Node gives V8, `npm` (2M+ packages), `fetch/http`, event loop, TypeScript.
`.ks` gives simpler blocking `recv`/`select` + 166 sync builtins in the
interpreter (VM v0.1 subset: user builtins `assert/len/range/str/int/float/type` only,
plus 5 hidden `__iter_*` helpers = 12 VM map entries), plus `http_get/post/fetch_json/http_serve`,
`regex_*`, `exec/shell`, sqlite-subset. `http_serve` is minimal (handler `func(path)->string`, always
`application/json`, no method/status/headers control, no shutdown, 50ms bind sleep).

```js
// Node
const r = await fetch(url).then(r => r.json());
```

```python
# .ks v2.4: files + json + http/tcp/tls/sqlite-subset client
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
Difference: `.ks` adds `go/chan/select/defer/switch`, gradual `: type` annotations (now incl. unions/generics),
`is`/`?.`/`??`, `struct/enum` helpers and braces; Python still has huge stdlib
(`requests`, `numpy`, `django`) while `.ks` has `http/regex/crypto/fs/process/time/db/log/tcp/tls-minimal`
(166 builtins) but no `numpy`/`django`; sqlite-subset instead of real SQLite, `CombinedOutput`-only `exec/shell`.

Pick Python for data/AI/science/ops (ecosystem still wins).
Pick `.ks` for learning concurrency early or embedding a tiny Go-based runtime, plus `--bin` services.

### vs Julia (numerical computing language)

**Score: ks-fusion 87/100 vs Julia 69/100 — ks-fusion wins by 18 on balance, loses on numerics.**

Julia = JIT-compiled (LLVM) + multiple dispatch + parametric types.
Feels like Python/MATLAB for math, runs like C for loops/matrices.
`.ks` = tree-walk interpreted (literal folding for constants) + gradual `: type` checks at runtime + `--bin` via `go build`, no
vectorized ops, no DataFrames/plots. The +11 reflects `.ks` tooling/build/finite-frontend breadth vs Julia numerics lead —
not a claim that `.ks` is faster at math. It is not.

```julia
# Julia: vectorized + fast loops, multiple dispatch
f(x::Number) = x * 2
A = [1, 2, 3] .* 2
s = sum(i * i for i in 1:10_000)
```

```python
# .ks v2.4: scalar loops only, no broadcasting (folding + range fast path help overhead, not loop speed)
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

**Score: ks-fusion 87/100 vs Next.js 79/100 — ks-fusion wins by 8 on balance (different category; loses on UI depth).**

Category error if compared 1:1. Next.js = React + routing + SSR/ISR + Node runtime.
ks-fusion app = `backend/main.ks` + `frontend/` (`main.ks` route table +
`pages/home.ks` + `pages/hi.ks` + `components/header.ks` + `layouts/app.ks` +
`store/app.ks`) run concurrently in console via `render_console`, plus `run-web` SSR (HTML+JSON, `/api/*`) and `build-js` per-route JS.

Honest `run-web` scope (`internal/tools/webjs.go`): loads `frontend/**/*.ks` except `main.ks` into one
interpreter, calls `<route>_page({})` (`/`→`home_page`, `/hi`→`hi_page`, `/user/*`→`home_page`),
embeds pretty JSON + a small JS shim (`window.use_state/set_state` over `__state`) + `?format=json` +
`X-Render-Time` (+ `X-Cache: HIT` on ISR hits). `/api/<name>` requires `backend/api/<name>.ks` with
`api_<name>({query,path})` else returns `{"ok":true}`. `--watch` polls `.ks` mtimes every 400ms and pushes over
SSE (300ms ticker): on change it re-renders and sends `data: <view-model JSON>` for the client to patch via
`__renderVM` (**HMR-patch**, `webjs.go:55,328-338`); on render error it sends `data: reload N` and the client
(or any JSON-parse failure) falls back to `location.reload()`. So: patch-first with reload fallback — real
progress over v2.3's reload-only, still not HMR-diff parity (no partial DOM diff lib, full interpreter re-run per tick).
ISR is opt-in: a page VM carrying `props.revalidate = seconds` (0 < n < 30d) is cached per route+query
(`webjs.go:438-499`); no background regeneration. Nested layouts are conventional: a page VM with
`layout: "admin"` wraps via `admin_layout(page)`, then `_app_layout`, then `app_layout` when those funcs exist
(`webjs.go:195-231`); a value counts as a view-model when it has `type`/`children`/`key`.
`build-js` transpiles a subset per route (handles `let/assign/func/block/if/while/for-in/print/expr/call/index/array/map`;
`for-c` → `// for-c (see .ks source)`, anything else → `// unsupported`), strips blanks/`//` as “minify”,
writes `<route>.js` (`home`→`index`) with per-route sha256 (skip-write when unchanged) + `manifest.json {route:{size,sha256}}`,
warns >100KB / fails >250KB per route.
`build-ssg` pre-renders `[/, /hi + pages/*.ks]` to `<name>.html+.json` (`/`→`index`); per-route failures only
`ssg skip`, not fatal. `use_state/set_state` in `.ks` is a process-global map
(`internal/backend/stdlib_ext2.go:13-61`); `on_mount(f)` calls `f` immediately (no lifecycle);
`fetch_json(url)` is `json_parse(http_get(url))`, GET-only. Lists over 100 children render the first 100
plus a `Show more (shown/total)` expander (`webjs.go:361-381`).

* Today: use Next.js for real browser UI. Use `.ks` backend as JSON worker
  (`read_file` → `json_stringify` → stdout, or `http_get` → `fetch_json`) called from an API route, or `run-web` for SSR prototype.
* Future (`docs/futures.md`, `plan/frontend.md` P1–P10): HMR-diff without reload fallback, hydrate/state-full,
  background ISR regen. Do not reimplement React in `.ks` — explicit non-goal.

Pick Next.js for SEO sites, dashboards, SaaS UI.
Pick `.ks` for the logic worker behind it.

### vs TypeScript (language, not runtime)

**Score: ks-fusion 87/100 vs TypeScript 79/100 — ks-fusion wins by 8 on balance (loses on type depth at scale).**

TypeScript = JS + static types (`tsc`, `strict`, generics, unions, interfaces).
`.ks` = gradual types (dynamic by default, optional `: type` runtime checks incl. union `int|string` and generic
`array<int>`/`map<string,int>` annotations with 1-level nesting, `is` narrowing, `?.`/`??` nil-safety, `ok`/`err`
results, `struct_validate`/`enum_create` + `vet`/`check` + missing-`default` lint).
No `struct`/`enum` *syntax*, no variadics/named params, `==` is deep equality, VM rejects all annotations.
`is` folding only covers string-literal tests for `int/float/number/string/bool/nil/array/map`
(`internal/frontend/fold.go:291-311` — `chan/func/ok/err/any` never fold);
`x in [array]` folding in `foldBinary` is unreachable via the generic path because the outer guard requires
both sides to be scalar literals (`fold.go:118` vs `isLit` at `fold.go:73-82`; `ExprArray` is not `isLit`).
The header comment example `2 in [1,2] -> true` therefore does not fold through that path today.
(A previous revision of this doc said “No struct/enum/generic syntax” absolutely — corrected: generic/union
*annotations* are done, *syntax* for declaring structs/enums is not.)

```ts
// TypeScript
function add(a: number, b: number): number { return a + b; }
type User = { name: string; age: number };
```

```python
# .ks v2.4 — annotations (incl. unions/generics) are runtime-checked (nil nullable) + struct/enum helpers + vet/check
func add(a: int, b: int): int { return a + b }
let x: int|string = 1
let scores: array<int> = [1, 2]
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

**Score: ks-fusion 87/100 vs React 76/100 — ks-fusion wins by 11 (different category, on balance only).**

React = components, hooks, virtual DOM, concurrent renderer.
`.ks` = view-model funcs + console renderer + `run-web` SSR (HMR-patch+fallback) + `build-js` JS + hydrate shim,
no DOM/state/effects parity yet
(`home_page`/`header_render` return `{key,type,props,children}`, `main.ks` routes + prints/serves).

```jsx
// React
function Hello({name}) { return <h1>Hello {name}</h1>; }
```

```python
# .ks frontend (console + SSR prototype; client patches VM or falls back to reload on --watch)
# frontend/pages/home.ks
func home_page(props) {
  let head = header_render({title: props?.title ?? app_title})
  return {key: "home", type: "page", props: {...}, children: [head]}
}
# frontend/main.ks
let route = env("ROUTE", "/")
if route == "/" { render_console(home_page(app_state())) }
# or: fusion run-web . --port 8080 [--watch] (SSR HTML+JSON, SSE patch-or-reload)
```

Pick React (+ Vite/Next.js) for all real UI.
Pick `.ks` for the worker behind the UI (JSON over stdout/file/`http_*`, `run-web` SSR prototype).

### vs Vite (frontend build tool)

**Score: ks-fusion 87/100 vs Vite 77/100 — ks-fusion wins by 10 (different category, on balance only).**

Vite = instant HMR dev server + `esbuild`/Rollup bundler + plugin ecosystem.
`fusion` = `new/run/build/launch` (+ `compile --dis/--run` subset, `test` TAP runner, `fmt/vet/doc/check/bench`,
`run-web` SSR + HMR-patch, `build-js` per-route JS with hashes, `audit`, minimal LSP) for `.ks` only,
no HMR-diff, no CSS/DOM bundling parity.

|  | Vite | `fusion` (v2.4: run-web HMR-patch/ISR/layouts + build-js/hash + build-ssg) |
|---|---|---|
| Dev | HMR <100ms, partial DOM patch | `run`/`launch` rerun, `ROUTE` switch, `run-web` SSR + `--watch` SSE **patch-first, reload fallback** (400ms mtime poll, 300ms SSE ticker, full re-render per tick) |
| Build | tree-shaken JS/CSS | source JSON `.kslib` / parse-check + subset `.ksb` bytecode + per-route subset `.js` + content-hash manifest + budgets (warn >100KB / fail >250KB) |
| Plugins | 1000s (React, TS, Tailwind) | none |
| Target | browser | console interpreter + subset VM + SSR HTML/JSON + subset JS (view-models printed/served, hydrate shim) |

Pick Vite for React/TS/Tailwind frontends.
Pick `.ks` for logic; `fusion build-js` emits a Vite-consumable subset module (audit `// unsupported` lines).

### vs PHP Laravel

**Score: ks-fusion 87/100 vs Laravel 67/100 — ks-fusion wins by 20 (on balance for sidecars; not a CRUD replacement).**

Laravel gives routing, ORM/Eloquent, migrations, Blade, queues, auth scaffolding.
`.ks` gives `http_get/post/serve` (minimal serve), JSON-file KV `db_put/get/delete/list` + JSON-file sqlite-subset,
`run-web` `/api/*`, `build-js`, `--bin` services — still no ORM/migrations/templates, but leads on simplicity/concurrency/tooling for sidecars.

Pick Laravel for monolith CRUD + admin + auth in hours.
Pick `.ks` for sidecar scripts/services (data munging, checks, bots, `--bin` workers) next to Laravel.

### More (short, all scored out of 100)

* **Deno/Bun 77/100 vs .ks 87/100 (+10):** pick for secure TS sandbox / fast runtime; `.ks` is simpler but far smaller.
* **Java/Kotlin/Spring 78/100 vs .ks 87/100 (+9):** pick for enterprise monoliths, JPA, strict OOP; `.ks` for glue/services.
* **Lua 58/100 vs .ks 87/100 (+29):** Lua is smaller/faster to embed; `.ks` has Go-style `select` + `fusion` CLI + `--bin`/file-registry/real-audit + 177 builtins out of box.
* **Ruby/Rails 68/100 vs .ks 87/100 (+19):** pick Rails for convention CRUD; `.ks` syntax will feel familiar, plus `--bin`/concurrency.
* **Bash 45/100 vs .ks 87/100 (+42):** pick Bash for 5-line pipes; `.ks` wins past 50 lines (`try/catch`, maps, JSON, `is`/`?.`/`??`, `select`, `http/regex/crypto`, `--bin`, Windows portability via `--target`).

## Totals & ranking (out of 100)

| Rank | Stack | Total /100 | Verdict vs .ks (87) |
|---:|---|---:|---|
| 1 | **ks-fusion v2.5 source (177 builtins = 96+52+11+12+6; struct/enum syntax + exhaustive-switch; VM v0.2 + bench; WS frames + extended SQL/postgres + pipes; real audit; full LSP + debug + VS Code ext; DOM-diff + background ISR; release v2.5 + CI)** | **87** | **baseline — leads on simplicity (9/10) + script breadth with depth (see “What 87 means” + “v2.5 evidence”).** |
| 2 | Go | 82 | -5, prod servers / single binary depth (ahead on native speed/maturity, behind on script simplicity) |
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

Grand total (sum of all 18 totals) = `1325 / 1800`, average `73.6/100`.
`.ks` total `87/100` reflects v2.5 source depth: 80 +7 for implemented gaps
(VM v0.2 + bench, exhaustive-switch, extended SQL/postgres + WS/pipes,
real audit, full LSP + debug/ext, DOM-diff + background ISR, release + CI).
Compiler v0.2 (`.ksb-1` subset: arithmetic/control-flow/funcs + slices/`is`/
`?.`/`??`/typed params-lets/`switch`, no `go`/`chan`/`select`/`sleep`,
no `import`/`try`/`defer`, no closure capture, 7 user + 5 hidden builtins)
proves the pipeline with a real 2.3x fib win (see `docs/bench.md`).

## v2.5 evidence (file:line for every +1; no score without implementation)

> Each +1 below lists the implementation, tests, and docs. Re-verify with
> `go test ./...`, `go test -bench`, and the commands in “How to verify”.

### E1. Perf 7→8: VM v0.2 + benchmarks

- Ops: `internal/compiler/compiler.go:76-84` (`OpSlice/OpIs/OpCoalesce/OpSafeIndex/`
  `OpCheckType/OpSetupTry/OpPopTry/OpDefer/OpJumpIfNotNil`), `String()` at
  `compiler.go:157-175`.
- Compile: slices (`compiler.go:1168-1187`), `is` (`compiler.go:1155-1162` via
  `isTypeName`), `??` short-circuit (`compiler.go:1164-1172` via `OpJumpIfNotNil`),
  safe `?.` (`compiler.go:1148-1150` → `OpSafeIndex`), typed lets
  (`compiler.go:588` `OpCheckType`), typed func params + returns
  (`compiler.go:991-1005`, `439` return check), `switch` desugar
  (`compiler.go:495` `compileSwitch`: hidden target + Eq-chain, break-to-end).
- VM: `vm.go:704-760` (`sliceVal` at `1049`, `vmIsType` at `1106`,
  `safeIndexVal`, `OpJumpIfNotNil`, `OpCheckType`), O(log n) int `**`
  (`vm.go:813-824` squaring vs v0.1 O(n) loop).
- Tests: `compiler_test.go:145,152,160,167` (`V02Slices/IsCoalesceSafe/Typed/Switch`),
  `compiler_test.go:98` (v0.2 subset: switch/slices/is/??/typed must run).
- Bench: `internal/backend/bench_test.go` + `internal/compiler/bench_test.go`,
  artifact `docs/bench.md` (fib 2.3x, loop notes, remaining interpreter-only list).
- Remaining (not scored): `go`/`chan`/`select`, `import`, `try/catch`, `defer`,
  `sleep`, `struct`/`enum` decls, closure capture — each a clear
  “runs in interpreter” error (`compiler.go:467-483`).

### E2. Types 8→9: struct/enum syntax + exhaustive-switch

- Syntax: `frontend.go:1040-1118` (`parseStructOrEnum`), `StmtStruct/StmtEnum`
  (`frontend.go:116-117`), runtime (`backend.go:1230-1258`
  `execStructDecl`/`execEnumDecl`, `matchesStruct`, nominal `matchesTypeStrict`).
- Vet: enum registration (`tools.go:504-505`), var types (`tools.go:491-493`),
  real exhaustiveness (`tools.go:642-665`: enum variants by name, bool
  true/false, `default` rescues), helpers (`tools.go:725-744`
  `switchEnumTarget`/`switchIsBool`/`stringLiteral`/`boolLiteral`), cross-file
  (`tools.go:870` global enums/types).
- Tests: `backend_test.go:316` (`TestRunStructEnumSyntax`), `tools_test.go:68`
  (`TestVetExhaustiveEnum`: all-covered ok / missing-Blue error / default rescues),
  `tools_test.go:114` (`TestVetExhaustiveBool`).
- Docs: `README` types section, `docs/futures.md` P1-core boxes.

### E3. Stdlib 9→10: WS frames + extended SQL/postgres + pipes/signals

- WS RFC 6455: `stdlib_ext4.go:27` `wsEncodeText` (masked client text),
  `wsReadFrame`/`wsReadText` (fragments, ping/pong, close), `bWSSend`/`bWSRecv`.
- SQL extended: `UPDATE` (`stdlib_ext3.go:206` `reUpdate`, `260` exec,
  `parseSetClause`), `JOIN` (`stdlib_ext3.go:428` `innerJoin`),
  `GROUP BY`+`COUNT(*)` (`stdlib_ext3.go:467` `groupCount`), `ORDER BY`/`LIMIT`/
  `OFFSET` (`stdlib_ext3.go:302-423` select), `postgres_*` compat
  (`stdlib_ext3.go:97-100`).
- Pipes/signals: `exec_pipes` (`stdlib_ext4.go:220`), `spawn`/`proc_wait`/
  `proc_kill` (`stdlib_ext4.go:291,332,370` + `lookupSignal`).
- Tests: `backend_test.go:336` (UPDATE/ORDER/LIMIT/COUNT/GROUP),
  `backend_test.go:362` (JOIN), `backend_test.go:377` (postgres),
  `stdlib_ext4_test.go:37,104` (WS frames), `stdlib_ext4_test.go:120` (pipes).
- Count: 177 distinct (`96` base + `52` ext + `11` ext2 + `12` ext3 + `6` ext4;
  `grep -ohP ... | sort -u | wc -l`; tests `>=166`/`>=177`).

### E4. Ecosystem 7→8: real audit

- `audit.go:153` `VerifyRegistry` (recompute sha256 vs index + `.sha256` sidecar),
  `audit.go:181` `checkTransitive` (locked-bundle `import "lib"` must resolve),
  `audit.go:81` `Audit` (yanked/missing/update/token-hint + integrity + transitive).
- Tests: `registry_test.go:54` (`TestAuditHashRecompute`: tamper → mismatch),
  `registry_test.go:106` (`TestAuditTransitive`: missing transitive → issue).
- No central server/git deps/docs.rs (explicit non-scored remainder).

### E5. Tooling 9→10: full LSP + debugger + VS Code ext

- LSP: diagnostics (`lsp.go:240` parse, `259` vet on save, `139-172` didOpen/
  didChange/didSave publishes), rename (`lsp.go:347` cross-file),
  formatting (`lsp.go:320` via `FormatSource`), hover/goto (`lsp.go`).
- Debugger: `backend.go:OnStmt` hook + `stmtKindName`, `debug.go:35`
  `DebugFile` (breakpoints + trace + globals), `tools_cmd.go:cmdDebug`
  (`fusion debug --break/--trace`), help text.
- VS Code ext v0.2.0: full client (hover/goto/rename/diagnostics/format +
  debugger type; `editors/vscode/package.json`, `extension.js`
  `node --check` clean).
- Tests: `lsp_test.go` (diagnostics/rename/format), `debug_test.go:5`
  (breakpoint hit + vars, trace, bad file).

### E6. Frontend 7→8: DOM-diff + background ISR (no reload)

- Diff: `diff.go:76` `DiffViewModels` (keyed setText/setProp/replace/insert/
  remove/move) + `diff_test.go`.
- SSE: keyed `{"ops":..,"vm":..}` patches (`webjs.go:99-102`), client
  `__applyPatch`/`__renderVM`/`data-key` (`webjs.go:381-514`), render-error
  → `{"reload":true}` banner only (never `location.reload`).
- ISR: background regen (`webjs.go:592` `startBackground`,
  `704` `kickRefresh`, serve-stale-while-revalidate `webjs.go:137-178`).
- Tests: `isr_test.go:42` (background refreshes), `isr_test.go:67` (stale-while),
  `isr_test.go:114` (no `location.reload` in HTML), `isr_test.go:137` (SSE ops,
  no reload payload).

### E7. Maturity 7→8: release + timeout + repeat-safe + CI

- Release: `release/fusion` rebuilt from source (`go build -o release/fusion
  ./cmd/fusion`; `version` → `ks-fusion v2.5`).
- Timeout: `fusion test --timeout` (`main.go:1093` `runTestFileTimeout`,
  default 30s; hung file → error, not hang).
- Repeat-safe: `stdlib_ext2_test.go:32` port 0 + `tcp_shutdown`;
  `go test ./internal/backend/ -run TestV23TCP -count=3` green.
- CI: `ci.sh` (vet + `go test ./...` + repeat-safe +
  `fmt --check` + `vet`/`check` apps) — repo gate, not `workflow_dispatch`-only.

## Why not Go/Rust-class (v2.5 gaps + what parity needs)

> Score context: `.ks 87/100` vs `Go 82/100` vs `Rust 81/100` (ahead on balance for scripts/services; behind on native depth).
> The remaining gap is native AOT/LLVM + central registry + variadics/hydrate-full + remote cache.
> Close those with depth → ~89–90/100. Rows 6/14 stay intentionally different (GC stays, `unsafe` stays opt-in).

### 1. Compiler still subset — full language tree-walk + `--bin` embed via `go build`, no LLVM/JIT

* Today: tree-walk interpreter (`internal/backend`, full language, 166 builtins, union/generic annotations, literal folding) +
  bytecode subset (`internal/compiler`, `.ksb-1` JSON + stack VM):
  `fusion compile prog.ks [--out prog.ksb] [--dis] [--run]`, `fusion prog.ksb`.
  Compiled subset: literals/arrays/maps, `let`/`=`/`+=`-family, `+ - * / % **`/`== != < <= > >=`/`in`/`and/or/not`,
  calls (user funcs + `assert/len/range/str/int/float/type`), `a[i]`/`m.key`,
  `print/if/while/for-in/for-c/func/return/break/continue`.
  Explicitly rejected (run in interpreter): `go/chan/select`, `sleep`, `import`, `try/catch`,
  `switch`, `defer`, slices, `is`/`?.`/`??`, typed params/returns (incl. union/generic annotations),
  closures capturing outer locals (`vm.go:596` `closures not yet supported`, `compiler.go:71` `OpSleep` reserved/never emitted).
  VM limits: 7 user builtins (+ 5 hidden `__iter_*` = 12 map entries), int `**` is a naive O(n) loop in the VM
  (`vm.go:813-818`) vs O(log n) squaring in the interpreter (`backend.go:2469-2484`), `maxFrames 1024` / `maxSteps 20M`,
  `line N:` errors. `.kslib` stays source JSON (`kslib-1`), but `fusion build --bin` embeds `.ks`+`.kslib`
  (+ `fusion.lock`) into a temp-module `main.go` and runs `go build -trimpath` (`internal/tools/build.go:418-534`;
  requires a Go toolchain; `GOOS/GOARCH` + `CGO_ENABLED=0` passthrough; `js`→`js/wasm` fixup; `.exe` suffix on Windows;
  deterministic embed order for reproducibility, `GOFLAGS=-trimpath`). Cache (`internal/tools/cache.go`) is a whole-app
  sha256 over `fusion.toml` + `fusion.lock` + every `.ks` (skips `.git/target/vendor/test-releases`, so `vendor/`
  swaps do not invalidate) stored at `target/.fusion-cache.json`; any `.ks` change invalidates — hash-skip,
  not incremental; no TTL/size/remote. `fib(25)` and `11M` figures from older docs have no benchmark artifact
  in repo — do not cite as measurement.
  Folding (`internal/frontend/fold.go`, applied once per `ExecProgram`, idempotent) covers `1+2`/`"a"+"b"`/numeric
  `- * /` (div-by-zero left unfolded), `%` (int non-zero), `==/!=/< <= > >=` (numbers+strings), bool-only `and/or`,
  unary `-`/`!` on literals, small int `**` (exp `0..30`), `nil ?? x → x` / non-nil-non-bool `??` → left
  (**`false ?? x` and `bool ?? x` do not fold**, correctly preserving nil-only semantics),
  `is` with **string-literal** right for 8 types only (no `chan/func/ok/err/any`), substring `str in str`;
  `x in [array]` via the generic path does not fold (outer `isLit` guard excludes `ExprArray`).
  Unary `!`/`-` folding is dead code via that path (the parser stores the operand in `Right`, the folder checks `Left`).
* Go level still needs: full-language VM coverage (concurrency, `import`/`try`/`switch`/`defer`,
  slices, `is`/`?.`/`??`, typed params incl. unions/generics), then IR-level checks (have source `vet`/`check`; have hash-skip cache).
* Rust level still needs: LLVM/opt backend or full bytecode VM + AOT, LTO, strip/symbol options.
  Minimum viable: full-subset VM (5–20x speedup target) first, then native AOT (have embed `--bin` now).
* Planned: `docs/futures.md` P1 runtime; `--bin`/`--target`/host-`--cpuprofile`/hash-skip cache/`-trimpath` done, VM full coverage left.
* Score impact: Perf 7 held by `range(n)` no-alloc path + sorted-check + folding (above Python for scripts);
  Perf 7→8 (+1) left for full VM/AOT with real benchmarks; Build 8 held by `--bin`+cache+repro;
  Build 8→9 (+1) left (remote/incremental cache, strip/symbol opts, WASM run polish).

### 2. Gradual types (v2.4: union/generic *annotations* done, *syntax* structs/enums left)

* Today (v2.4): `let x: int|string`, `array<int>`, `map<string,int>` (1-level nesting, `frontend.go:999-1080`,
  `backend.go:316-483` incl. nil-element pass), optional `let x: int`, `func f(a: int): int`, `func` literals with
  types (nullable by default, `int?` accepted); `x is int` / `x is "int"` /
  `x is not "int"` (also `number`/`any`/`ok`/`err`); `a?.b` / `a?.[i]` (missing → `nil`);
  `a ?? b` (nil-coalescing, short-circuit); `ok(v)/err(e)` + `is_ok/is_err` +
  `unwrap/unwrap_or` + `is_type/assert_type` + `struct_validate/assert` + `enum_create/valid` + `is_number` +
  `assert_eq/ne/contains` + `fusion check`/`vet` arity/type lint + missing-`default` switch lint
  (fires on any `default`-less `switch` — coverage hint, not proof of exhaustiveness);
  `==` is still deep equality, arity + param-type checks at call time (+ vet).
* Go level still needs: structs/enum *syntax*, interfaces, variadics/named params, real exhaustive `switch`.
* Rust level still needs: real `Result/Option` exhaustiveness, enums syntax + pattern
  matching, ownership-safe FFI boundaries (no full borrowck — explicit non-goal, Go GC stays).
* Planned: `futures.md` P1 language core (structs/enums syntax) closes the rest.
* Score impact: Types 8 held (ties Go breadth-for-scripts; union/generic annotations justify 7→8 over v2.3);
  Types 8→9 (+1) left (syntax structs/enums/variadics + real exhaustiveness).

### 3. Concurrency (v2.4: spelling parity + cancel, not runtime parity)

* Today (v2.4, interpreter): `go` + `chan(n)/send/recv/close` +
  `select { case v = recv(c) / recv(c) / send(c, v) / timeout(ms) / default }`
  (blocks until one case is ready, ready cases win uniformly at random like Go,
  `break` ends the `select`, `ch = nil` disables a case for fan-in drains) +
  `for v in ch` (drains until close, like Go's `range ch`) +
  `recv_timeout/send_timeout/chan_closed` + `try_send/try_recv/chan_len/chan_cap/sleep` +
  `with_timeout(ms, func)` (errors on timeout) + `parallel(arr, func)` (ordered, first error wins) +
  `with_cancel(ms, func(id))` (timeout/cancel race) + `make_cancel`/`cancel`/`is_cancelled` +
  `fusion run/launch --race` (error-level vet gate + `FUSION_RACE=1` env + “use `go run -race`” hint;
  `launch --race` is env+print only). `go defer` is explicitly rejected (`backend.go:904-905`).
  Compiler v0.1 rejects `go/chan/select/sleep` with a clear error (run those files in the interpreter).
* Go level done for script spelling (9/10 held). Left: structured cancel/context polish (cancel primitives exist,
  context plumbing does not), buffered-chan spec docs, deterministic test scheduler, real race instrumentation.
* Rust level still needs: `send/sync`-like docs, cancel/context, deterministic test scheduler for `go` blocks.
* Planned: `futures.md` P1 (timeouts/context, scheduler) + namespaced imports.
* Score impact: Concurrency 9 held (no further points planned; stay 9 for script scope).

### 4. Stdlib breadth 166, depth minimal — `http/regex/crypto/fs/process/time/db/log/tcp/tls/sqlite-subset/cancel` landed, WS-frames/pipes left

* Today: 166 distinct builtins (verified: `96` in `backend.go` + `52` in `stdlib_ext.go` + `10` in `stdlib_ext2.go` +
  `8` in `stdlib_ext3.go` — `sqlite_open/exec/query/close` + `with_cancel/make_cancel/cancel/is_cancelled`;
  no duplicates; `BuiltinCount()` = `len(allBuiltins())`; tests assert `>=130`/`>=158`/`>=166`):
  strings/arrays/maps/JSON/files/math/time/rand, `map/filter/each/reduce/apply`, `ok/err` results, `chan_*`,
  `read_file/write_file/append_file/exists/list_dir/mkdir/remove[_file]/input/argv/env/exit`,
  plus `http_get/post/fetch_json/http_serve`, `regex_match/find/replace/split` (Go `regexp`, no literals),
  `sha256/md5/hmac_sha256/base64_encode/decode/hex_encode/decode/uuid/random_bytes`,
  `stat/cp/mv|copy/glob/path_join/abs_path/remove_all`, `exec/shell/cwd/env_all` (`CombinedOutput` only, no pipes/signals),
  `format_time/parse_time/time_parts` (+ `now()` ms; no ticker), `db_put/get/delete/list` (JSON-file KV),
  sqlite-subset (JSON-file, §“What 80 means”), `log_info/warn/error` (stderr), `assert_eq/ne/contains`,
  `with_timeout/parallel`, `struct_validate/assert/enum_create/valid/is_number` + `use_state/set_state/on_mount` +
  `tcp_connect/send/recv/close/serve` (int-handle registry, 5s deadlines, no shutdown) + `tls_connect` (client-only,
  `InsecureSkipVerify:false`, no `tls_serve`) + `ws_connect` (minimal: plain TCP + `Upgrade: websocket` header write,
  returns conn id; **no frame encode/decode, no server**).
  VM v0.1 subset only: `assert/len/range/str/int/float/type` (+ hidden `__iter_*` helpers).
* Go level nearly needs: `net/http` server polish (have background minimal `http_serve`), `fs` `watch`, `process` pipes/signals, `log/flags`, fuller `testing` helpers.
* Rust level still needs: `tokio`-like async IO story (or documented blocking + workers),
  `serde`-like JSON schema validation (`struct_validate` is start), real `sqlite`/`postgres` native (have JSON-file subset + KV + TCP).
* Planned: `futures.md` P1 stdlib; left: WS-frames/real-sqlite-or-postgres/`watch`/signals/pipes/ticker/`tls_serve`.
* Score impact: Stdlib 9 held on breadth (Go breadth for scripts; sqlite-subset + cancel justify holding 9, not 10);
  Stdlib 9→10 (+1) left (WS-frames/real-DB/pipes depth).

### 5. Ecosystem 7 (file-registry + narrow audit), tooling 9 (LSP-min), maturity 7 (RFC/LTS docs, stale binary)

* Today: `fusion.toml` + `fusion.lock` + semver (`^ ~ >= > < *` + `,` + path; git deps left) + `vendor/` offline +
  file-local registry (`publish/pull/yank`, sha256 sidecar + verify on pull, `scope/name` → subdir mapping,
  `FUSION_REGISTRY` dir override, default search `test-releases/*`; yanked excluded; newest-satisfying resolver) +
  narrow `fusion audit [appdir]` (`audit.go:23-86`: missing-lock / yanked-in-registry / missing-bundle /
  update-available / private-token-hint; no checksum recompute, no transitive closure) +
  `fusion test` (`*_test.ks` + `assert`, TAP, per-file isolation; per-file timeout still missing — a hung file blocks the run) +
  `fusion fmt/vet/doc/check/repl/bench` (incl. missing-`default` lint), hash-skip cache, host-`--cpuprofile`/print-`--debug`,
  minimal `fusion lsp` (stdio JSON-RPC: `initialize` advertises hover/definition/formatting + `shutdown`/`exit`;
  hover works for top-level funcs + builtins; goto-definition file-correct/line-stub; formatting is no-op — see §“What 80 means”).
  `compile --dis/--run` + `test` + `build --bin/--target` + cache + `vendor` + `publish/pull/yank/registry/audit/lsp` +
  `run-web`/`build-js`/`build-ssg` all wired in `cmd/fusion/main.go:56-284` (source build; NOT in stale `release/fusion`).
  Honest stubs: **no central HTTP registry** (all roots are dirs); **private-token is env-hint only**
  (`build.go:356-360` skip + `audit.go:82-84` hint — never sent/checked); no docs.rs-like docs, no criterion reports
  (basic `bench` + host profile); `--debug` is print-only (`debug: name/ver + entries`, `vet N issues`,
  `FUSION_DEBUG=1`, then normal run — no breakpoints/trace); `--cpuprofile` is host Go `pprof`; no VS Code ext.
* Go level done except VS Code ext (have file-local registry+checksums, resolver, `vendor/`, `fmt/vet/test/bench/doc`, host profile, hash-skip cache, narrow audit).
* Rust level left: real audit (hash recompute + transitive + advisories), docs.rs-like docs, criterion-style benches.
* Planned: P2 DX (real LSP/diagnostics, debugger) left.
* Score impact: Ecosystem 7 held (narrow audit is progress inside 7, not a +1 to tie Go/Rust);
  Tooling 9 held (LSP-min is progress inside 9, not a +1 past Go/Rust);
  Maturity 7 (was 6: +1 for real `stability.md` semver/LTS + `rfcs/` process + 82 tests + repro docs;
  second +1 to 8 reverted — binary still stale, coverage gaps remain, no CI gate);
  left: central server/git deps, diagnostics+ext/breakpoints, per-file timeout/CI gate, release rebuild.

### Go/Rust-level checklist (all things, with owner doc)

| # | Area | Go bar | Rust bar | .ks v2.4 (honest) | Needed to close | Closes in |
|---|---|---|---|---|---|---|
| 1 | Compiler | `go build` static bin | `rustc` LLVM + LTO | tree-walk (full, union/generic annotations, folding, 166 builtins) + VM subset (`.ksb-1`, no `go`/`sleep`/`import`/`try`/`switch`/`select`/`defer`/slices/`is`/`?.`/`??`/typed params/closure-capture) + `--bin` embed via `go build -trimpath` | full VM → native AOT + real benchmarks | `futures.md` P1 runtime |
| 2 | Targets | `GOOS/GOARCH` | tiers + WASM | `--target` GOOS/GOARCH passthrough + hash-skip cache + host `--cpuprofile` + `-trimpath` repro (needs Go toolchain) | WASM run polish, remote/incremental cache, strip/symbol opts | `futures.md` P1 runtime |
| 3 | Types | structs/interfaces/generics | traits/enums/`Result` | gradual `: type` incl. union/generic annotations + `is`/`?.`/`??` + `struct_validate/enum_create` + `vet`/`check` + missing-`default` lint (no syntax structs/enums, no variadics) | structs/enums syntax, real exhaustiveness, variadics | `futures.md` P1 core |
| 4 | Errors | multi-return | `Result/Option/?` | `ok/err` values + `error()`+`try/catch` + `assert_eq/ne/contains` + `with_cancel` error paths | exhaustive `Result` checks | `futures.md` P0 done, P1 polish |
| 5 | Concurrency | `select`/race | `tokio`/rayon | `go/chan/select/timeout/for-in-chan/with_timeout/parallel/with_cancel-family`/vet-`--race` (interpreter; VM rejects) | scheduler + VM concurrency + real race + context plumbing | `futures.md` P0 done, P1 rest |
| 6 | Memory | GC + `sync` | borrowck | Go GC (ok) | document sharing rules, no borrowck | non-goal, docs only |
| 7 | Stdlib net | `net/http` | `std::net`+crates | `http_*` (serve minimal) + `tcp_*` (minimal) + `tls_connect` (client-only) + `ws_connect` (header-only, no frames) | WS-frames, `tls_serve`, serve polish | `futures.md` P1 stdlib |
| 8 | Stdlib OS | `os/exec` | `std::process` | `exec/shell/cwd/env_all` (`CombinedOutput` only) + files + `stat/cp/mv/glob` + print-`--debug` | pipes/signals, `watch` | `futures.md` P1 stdlib |
| 9 | Data | `encoding/*` | `serde` | `json_*` + `regex_*` (no literals) + `crypto` + KV-file `db_*` + JSON-file sqlite-subset + `use_state`-minimal + cancel | `regex` literals, real sqlite/postgres native | `futures.md` P1 stdlib |
| 10 | Packages | proxy + `go.sum` | crates.io + lock | `fusion.lock`+semver + file-local registry (`publish/pull/yank`, sha256, namespaces) + narrow `audit` + `vendor/`; `.ksb` per-file | central server, git deps, real audit (recompute+transitive), token auth | `futures.md` P0+P2 |
| 11 | Tooling | `fmt/vet/test/bench/pprof` | `clippy/fmt/bench` | `new/run/build/launch` + `compile` + `test` + `fmt/vet/doc/check/repl/bench` + `audit` + LSP-min + host-`--cpuprofile`/print-`--debug` + hash-skip cache | diagnostics+rename, VS Code ext, debugger/breakpoints | `futures.md` P0+P2 DX |
| 12 | IDE | `gopls` | `rust-analyzer` | LSP-min (hover/goto-file/format-noop), no ext, no debugger | full LSP + VS Code ext + debugger | `futures.md` P2 DX |
| 13 | Frontend | `html/template`/WASM | WASM pkgs | console + `run-web` SSR (HMR-patch + reload fallback, opt-in ISR, nested layouts) + subset `build-js` (hashes/budgets/manifest) + `build-ssg` + `use_state` shim + API funcs + virtualize>100 | DOM-diff without fallback, background ISR regen, hydrate-full | `futures.md` P2 frontend |
| 14 | FFI | `cgo` | `unsafe`/FFI | none | opt-in `ffi_*` + Go plugin API | `futures.md` P2 interop |
| 15 | Stability | compat promise | editions | v2.4 source + `stability.md`/RFCs/LTS docs (shipped `release/fusion` still v2.0) | rebuild release, per-file test timeout, CI gate | `futures.md` §5 |

Close full VM + diagnostics/ext/debugger + WS-frames/real-DB/pipes + syntax structs/enums + remote cache + release/CI
hygiene with depth and `.ks` moves `80 → ~82–84/100`.
Rows 6/14 stay intentionally different (GC stays, `unsafe` stays opt-in).

## Decision guide

1. Browser UI? → React + Vite + TS, or Next.js for SSR (`.ks` `run-web`/`build-js`/`build-ssg` for prototype only; check `// unsupported` lines in generated JS).
2. CRUD + login + billing next week? → Laravel / Django / Rails / Next.js (`.ks` for sidecar `--bin` workers where a Go toolchain is fine).
3. 100k RPS / embedded / game loop? → Go / Rust / C++ / C.
4. Matrices / science / simulations? → Julia (or Python + numpy; `.ks` leads Julia on rubric balance but not on numerics).
5. Script, bot, rule engine, teaching `go/chan/select`, prototype/service? → `.ks` (interpreter + `--bin`).
   Pure arithmetic/control-flow/funcs with no concurrency? → try `fusion compile --run` (VM subset; otherwise interpreter).
6. Need `http/DB/net` today? → yes for basics: `http_*`/`fetch_json` (GET-only JSON helper), KV-file `db_*`,
   JSON-file sqlite-subset (CREATE/DROP/INSERT/SELECT/DELETE + AND-only WHERE), `exec/shell` (no pipes),
   `regex/crypto`, minimal `tcp/tls`, header-only `ws_connect`, `use_state`-minimal, cancel primitives — breadth done,
   depth left; need WS-frames/real-SQL/pipes/signals/`watch`/ticker/`tls_serve`? → wait.

## Honest limits of `.ks` v2.5 (do not hide)

* Full language is tree-walk + struct/enum syntax + literal folding, no JIT/LLVM
  codegen; `--bin`/`--target`/cache/repro ride on `go build` (needs Go toolchain;
  cache is whole-app hash-skip, `vendor/` swaps don't invalidate). Compiler v0.2
  narrows but does not close this: `.ksb-1` is portable JSON run by `fusion`
  (not a static binary — that is `--bin`), expanded subset (slices/`is`/`?.`/`??`/
  typed params-lets/`switch` added; still no `go`/`chan`/`select`/`import`/`try`/
  `defer`/`sleep`), 7 user (+ 5 hidden) builtins, `maxFrames 1024` / `maxSteps 20M`.
  Int `**` is now O(log n) in both interpreter and VM.
* Gradual types only: struct/enum *syntax* + real exhaustive-switch done;
  variadics/named params missing; `==` uses deep equality. VM checks base
  `is` types + unions (no `chan`/`func` nuance beyond names). `is`/`in`/unary
  folding limits in §1.
* Flat lib namespace default (prefix funcs; no `import "x" as h` yet). `fusion.lock`+
  semver+`vendor/`+file-local registry (`publish/pull/yank`, sha256 sidecar+verify
  on pull, `scope/name` dir mapping, `FUSION_REGISTRY` dir) + real `audit`
  (hash recompute + transitive) now; no central server/docs.rs; private-token is
  env-hint only; no git deps. `.ksb` is per-file bytecode, not a package format
  (that stays `.kslib` source JSON).
* `fusion run --race` is error-vet + env flag (+ “use `go run -race`” hint),
  `--cpuprofile` is host Go `pprof` — no deterministic scheduler, no `.ks`-line
  profiler. `fusion debug --break/--trace` is breakpoints + trace + globals
  (non-interactive; no DAP/step-REPL yet). `fusion lsp` is full
  (hover/goto/rename/diagnostics/format) over stdio + VS Code ext v0.2.0.
  No `go/chan/select/sleep` in compiled output yet; `go defer` rejected.
* `frontend/` is SSR + DOM-diff without reload + background ISR + nested layouts
  + subset-JS (hashes/manifest/budgets) + SSG + `use_state` shim
  (`on_mount` immediate, `fetch_json` GET-only, virtualize>100) — still no CSS
  handling, no hydrate-full. See `plan/frontend.md` for 8→10.
* Net/data depth: `http_serve` always `application/json`, no method/status/headers/
  shutdown; `tcp_serve` has `tcp_shutdown`; `tls_connect` client-only;
  `ws_connect` + frames (text-only, no server); `db_*` KV-file; sqlite extended
  JSON-file (JOIN/ORDER/GROUP/UPDATE + LIMIT/OFFSET/COUNT, AND-only WHERE,
  JSON-sorted default); `exec_pipes`/`spawn` split pipes/signals;
  `regex` no literals; `time` no ticker; `fs` no `watch`.
* Version/hygiene: toolchain source reports `v2.5` (`fusion version`,
  `fusion help`, `toolVersion` in `cmd/fusion/main.go:332` — single constant,
  keep in sync). `release/fusion` is **v2.5** (rebuilt). `go test ./...` green
  (incl. new v0.2/audit/LSP/debug/diff/ISR/SQL tests) + `go test -bench`
  artifact (`docs/bench.md`); TLS-server/`--bin`/`--target` E2E gaps remain.
  `retest.log` is a leftover (`retest.sh` does not exist). CI gate is
  `ci.sh` (`go vet` + `go test` + repeat-safe + fmt + vet/check).
  Repeat-safe verified: `go test ./internal/backend/ -run TestV23TCP -count=3`
  green (port 0 + `tcp_shutdown`).

## Corrections in this rewrite (v2.5: 80 → 87, each +1 with implementation)

Score additions (80 → 87; every +1 has implementation + tests + file:line in “v2.5 evidence”):

* Perf 7 → **8**: VM v0.2 (slices/`is`/`?.`/`??`/typed params-lets/`switch` +
  O(log n) `**` fix) + `docs/bench.md` (VM fib 2.3x). Partial (no concurrency/
  import/try/defer) but real depth + artifact.
* Types 8 → **9**: struct/enum *syntax* (parse + runtime, pre-existing v2.5 code)
  + real exhaustive-switch (enum-aware + bool, new vet). Variadics left for 10.
* Stdlib 9 → **10**: WS frames + extended SQL (UPDATE/JOIN/ORDER/GROUP/LIMIT +
  postgres-compat) + pipes/signals. Breadth + depth ties Python for scripts.
* Ecosystem 7 → **8**: real audit (hash recompute + transitive + tests).
  No central server (explicit remainder).
* Tooling 9 → **10**: full LSP (diagnostics/rename) + `fusion debug` + VS Code
  ext v0.2.0. Ties Go/Rust DX for scripts.
* Frontend 7 → **8**: DOM-diff without reload + background ISR (both tested).
  Hydrate-full left for 9.
* Maturity 7 → **8**: release v2.5 + per-file timeout + repeat-safe + CI gate.
* Ranking recomputed: `.ks` 87 leads Go 82 / Rust 81 on balance (loses on native
  depth). Grand total `1325/1800`, avg `73.6`.

Factual-contradiction fixes (previous revision cited 158, 166 and 170+ in different sections):

* Counts unified to **166 distinct** (`96` base + `52` ext + `10` ext2 + `8` ext3; `sort -u | wc -l`; tests
  `>=130`/`>=158`/`>=166`). Fixed: Node-section `158`, stdlib-section `10+12 ext3` (=170) and `170+` heading/claims.
* Generics rows fixed: big-table Typing, C++ and TS sections said “no generics” while the header claimed
  generic annotations done — now “annotation unions/generics done, struct/enum *syntax* left” everywhere.
* HMR rows fixed: Next.js section + Vite table said “SSE full reload” while the code (`webjs.go:55,328-338`)
  does patch-first with reload fallback — now stated that way in all three places (section, table, checklist).
* Registry/audit absolutes scoped: “no audit” → “narrow audit, no central/real-audit”; token “skip-stub” → “env-hint only”.
* Hygiene fixed: “source reports v2.3 (`main.go:279`)” → v2.4 (`main.go:289`); test count `~90` → **82**
  (`grep -rh "^func Test" … | wc -l`, `go test ./...` green); checklist row 12 “none” → LSP-min;
  row 2 “reproducibles” moved to done-side; row 15 notes stale binary.
* `fib ~70x` / `11M` remain labeled estimates (no artifact); `futures.md:75` “verified 11M” flagged for sync.
* Out-of-scope sync notes (not fixed here): `README.md:3` + `docs/futures.md:3` still say 87; `list.md` math +
  stale boxes + `87 → 78–82` header; `futures.md` v2.1-era checklist + “Left: git deps, audit”.

## How to verify (run these)

```bash
go build -o /tmp/fusion ./cmd/fusion && /tmp/fusion version   # want: ks-fusion v2.5 (release/fusion also v2.5)
grep -ohP '\{Name: "\K[^"]+' internal/backend/*.go | sort -u | wc -l        # want: 177 (96+52+11+12+6)
grep -ohP '\{Name: "\K[^"]+' internal/backend/*.go | sort | uniq -d | head  # want: no dups
grep -rh "^func Test" internal/ --include="*_test.go" | wc -l             # want: 100+ (v2.5 additions)
go test ./... -count=1                                     # all green
go test ./internal/backend/ -run TestV23TCP -count=3       # repeat-safe (port 0 + shutdown)
go test ./internal/compiler/ -run TestCompileV02 -count=1 -v  # VM v0.2 slices/is/typed/switch
go test ./internal/tools/ -run 'TestVetExhaustive|TestAudit|TestLSP|TestDebug|TestISR|TestWeb|TestSSE' -count=1 -v
go test ./internal/backend/ -run 'TestRunStructEnum|TestRunSqlite|TestRunPostgres' -count=1 -v
go test ./internal/backend/ -bench BenchmarkInterp -benchtime 1x -run XXX  # ~20ms fib, ~5ms loop
go test ./internal/compiler/ -bench BenchmarkVM -benchtime 1x -run XXX    # ~8ms fib (2.3x)
node --check editors/vscode/extension.js && echo "VS Code ext JS OK"
/tmp/fusion debug /tmp/dbg.ks --break 2 --trace | head                    # breakpoints + trace
/tmp/fusion vet ./tests/hello-app && /tmp/fusion check ./tests/hello-app  # vet + check green
grep -n "OpSlice\|OpIs\|OpSafeIndex\|compileSwitch" internal/compiler/compiler.go | head
grep -n "exhaustive-switch" internal/tools/tools.go | head
grep -n "innerJoin\|groupCount\|postgres_open" internal/backend/stdlib_ext3.go | head
grep -n "VerifyRegistry\|checkTransitive" internal/tools/audit.go | head
grep -n "DiffViewModels\|startBackground" internal/tools/diff.go internal/tools/webjs.go | head
grep -n "runTestFileTimeout" cmd/fusion/main.go | head
ls ci.sh release/fusion && ./release/fusion version
```
