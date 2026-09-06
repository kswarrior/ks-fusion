# ks-fusion Futures / Roadmap

> Current: `v2.5` (83/100 in docs/vs.md) — tree-walk interpreter in Go, gradual-typed `.ks` language
> (`: type` annotations, `is`, `?.`/`??`, `ok`/`err` results, struct/enum syntax, 177 builtins, --bin, fmt/vet/doc/check/repl/bench/debug, VM v0.2, real audit, full LSP).
> This doc lists what exists, what is planned, and what is explicitly non-goals.

## 1. Where we are (v2.4, real today)

* Frontend: lexer + parser (`internal/frontend`) — `# // /* */` comments,
  `"double"`/`'single'` strings, `0xFF 0b101 0o17 1_000 1e3 .5` numbers,
  `?.`/`??`/`is`, `: type` annotations on `let`/`func`.
* Backend: tree-walk interpreter (`internal/backend`) with closures,
  `if/while/for-in/for-c/switch/try-catch-finally/defer/break/continue`,
  runtime type checks for annotations, `is`/`?.`/`??`.
* Types (v2.4, gradual): `nil bool int float number string array map func chan any ok err` —
  dynamic by default, optional `: type` (nullable), `is`/`is not` tests,
  `is_type`/`assert_type`, `ok(v)/err(e)` + `is_ok/is_err/unwrap/unwrap_or`,
  plus `struct_validate/assert`, `enum_create/valid`, `is_number`, `assert_eq/ne/contains`, `fusion check`/`vet`.
* Concurrency: `go` + `chan(n) send/recv/close/try_send/try_recv/chan_len/chan_cap/sleep`
  + `select { case v = recv(c) / recv(c) / send(c, v) / timeout(ms) / default }`
  + `for v in ch` drain-until-close + `recv_timeout/send_timeout/chan_closed`,
  + `with_timeout/parallel` + `fusion run --race`,
  backed by Go goroutines.
* Stdlib (166 builtins): all v2.1 plus `http_get/post/fetch_json/http_serve`,
  `regex_match/find/replace/split`, `sha256/md5/hmac_sha256/base64_encode/decode/hex_encode/decode/uuid/random_bytes`,
  `stat/cp/mv/glob/path_join/abs_path/remove_all`, `exec/shell/cwd/env_all`,
  `format_time/parse_time/time_parts`, `db_put/get/delete/list`, `log_info/warn/error`,
  `assert_eq/ne/contains`, `with_timeout/parallel`, `struct_validate/enum_create`.
* Tooling (v2.4): `fusion fmt/vet/doc/check/repl/bench` + `test`, constant folding.
* Build (v2.4): `fusion build --bin` single static binary + `--target` matrix + `fusion.lock` semver + `vendor/`.
* Frontend (v2.4): `fusion run-web` SSR + `fusion build-js` per-route JS + budgets.
* Packaging (`fusion.toml` + `.kslib`):
  `fusion new [--lib]`, `fusion build [--release] [--out DIR]`, `fusion run`,
  direct `fusion prog.ks` / `fusion lib.kslib` + `#!/usr/bin/env fusion` shebang.
  `.kslib` is JSON (`kslib-1`) with parse-checked sources.
  Lib imports share one flat global namespace.
* App shape: `backend/main.ks + frontend/main.ks` run concurrently.
  Today `frontend/main.ks` is console logic, not browser UI.

## 2. Design goals (unchanged)

1. Easy like Python, concurrency like Go, packaging like Rust (`cargo`).
2. Single `fusion` binary, zero dependencies, `go build -o fusion ./cmd/fusion`.
3. Batteries included, predictable errors with `line N:`.
4. Source-portable bundles before native binaries.

## 3. Planned futures

### P0 — correctness + tooling gaps

* [x] `select` for channels (with `timeout` / `default`). Done v2.1:
  `select { case v = recv(c) / recv(c) / send(c, v) / timeout(ms) / default }`
  (blocks until one case is ready, ready cases win uniformly at random,
  `break` ends the `select`, `ch = nil` disables a case).
  Done v2.4: `fusion run --race` + `with_timeout`/`parallel`.
* [x] `fusion fmt` (canonical formatter) + `fusion vet` (unused var, bad arity lint). Done v2.4:
  `fusion fmt [target] [--check]`, `fusion vet [target] [--deny-warns]`, `fusion check`, `fusion doc`, `fusion repl`, `fusion bench`.
* [x] `fusion test` runner: `*_test.ks` with `assert`, exit-code + TAP output. Done:
  `fusion test [target]` (dir recursive or single file), per-file isolation, app-root
  imports via nearest `fusion.toml`. Left: per-file timeout.
* [x] `fusion doc` from `#` comments. Done v2.4: `fusion doc [target] [--out FILE]`.
* [ ] Namespaced imports: `import "hello-lib" as hl` / `hl.greet()`.
  Today: flat globals, prefix your functions.
* [x] `fusion.lock` + real semver resolver (`^0.1.0`, `>=`, path + git deps). Done v2.4:
  `^ ~ >= > < *` + `fusion.lock` + `fusion vendor`. Done v2.4: registry `publish/pull/yank` + sha256 + namespaces + private token. Left: git deps, audit.
* [x] Error values (`Result`-style) as alternative to `error(msg)` abort +
  `try/catch`. Keep both. Done v2.1: `ok(v)/err(e)`, `is_ok/is_err`,
  `unwrap/unwrap_or`, `v is ok/err`.

### P1 — runtime + performance

* [ ] Bytecode compiler + VM (drop tree-walk hot loop, 5–20x speedup target).
  Keep `RunFile` / shebang behavior identical.
* [x] `fusion build --bin` → single static executable (embed `.ks`+`.kslib`,
  like `go build`). Done v2.4: `fusion build --bin [-o FILE] [--target OS/ARCH]`, verified 11M isolated run.
* [x] Cross-compile: `--target linux/amd64,arm64,darwin,windows,wasm`. Done v2.4 (GOOS/GOARCH passthrough).
* [ ] Profiler hooks: `fusion run --cpuprofile`, opcode counts.
* [x] Tree-walk opts done v2.1 (Perf 4→5): lock-free single-threaded scopes
  (`conc` flag, no RWMutex/mutex per lookup/`fail` until first `go`), halved
  env allocs (func body runs in callEnv; if/while/for/switch/try reuse Block
  scope), O(1) builtin cache, string+string fast path, O(n log n) `sort`
  (was insertion O(n²)), O(log n) int `**`/`pow`, window-only `slice`/`bSlice`.
  Done v2.4 (Perf 5→6): constant folding (`1+2`→`3`, `"a"+"b"`→`"ab"`, `frontend/fold.go`, idempotent, tested).
  Left: full local-slot lookup, string-builder concat.
* [ ] Optimizations (remaining): local-slot lookup (replace scope-chain
  scan on hot paths), string-builder concat fast path.
* [ ] WASM run polish: run `.ks` in browser / edge workers (`--target wasm` builds, runtime polish left).

### P1 — language core

* [x] `chan` iteration `for v in ch` (drains until close, Go `range` semantics)
  + `recv_timeout/send_timeout/chan_closed` helpers. Done v2.1.
  Left: `select/sleep` deadline contexts, buffered-chan semantics docs.
* [ ] Structs / typed maps: `type User = {name: string, age: int}` (optional static check).
  Today v2.1 has primitive `: type` annotations only.
* [ ] Enums + exhaustive `switch` check.
* [x] `?.` nil-safe access + `??` default operator. Done v2.1
  (`a?.b`/`a?.[i]` missing → `nil`, `a ?? b` nil-only short-circuit;
  replaces `get(m,k,default)` for nil-safety).
* [x] Gradual `: type` annotations + `is` tests. Done v2.1
  (`let x: int`, `func f(a: int): int`, `x is int`, `is_type/assert_type`;
  nullable by default, `number/any/ok/err` aliases).
* [ ] Named params / default args, variadics `func f(a, ...rest)`.
* [ ] Modules with `export` instead of flat globals (backward-compat shim).
* [ ] Regex literal / `regex_*` builtins.
* [ ] `defer` in `go` routines (today: rejected, must scope explicitly).

### P1 — stdlib (most requested)

* [ ] `http`: `http_get/post/serve/listen`, headers, JSON auto-codec.
* [ ] `net/ws`: TCP + WebSocket client/server.
* [ ] `fs` full: `stat/cp/mv/glob/watch`, path joins.
* [ ] `process`: `exec(cmd, args)`, pipes, signals.
* [ ] `time`: `sleep` already exists; add `date/format/parse/ticker`.
* [ ] `crypto/hash`: `sha256/md5/hmac/random_bytes/uuid`.
* [ ] `db`: `sqlite_*` first, then `postgres_*`.
* [ ] `log/flags/testing` helpers for CLIs.

### P2 — frontend story (biggest gap vs Next.js)

Today `frontend/main.ks` prints to console. Futures:

* [ ] `fusion run --web`: serve `frontend/` with hot reload (file watch).
* [ ] `fusion build --js`: transpile a safe subset of `.ks` to JS for browser use.
* [ ] JS bridge: `js_call(name, args)` when running under Node/browser.
* [ ] Next.js interop: `.ks` backend as API routes (`/api/*.ks` → JSON),
  documented pattern in `docs/vs.md`.
* [ ] Non-goal: reimplementing React in `.ks`. Use `.ks` for logic, JS for DOM.

### P2 — interop

* [ ] C FFI: `ffi_open/load/call` for `.so`/`.dylib` (explicitly unsafe, opt-in).
* [ ] Go plugins: expose `backend.Value` API for custom builtins without forking.
* [ ] Node/Python bridges via subprocess + JSON stdio helpers.

### P2 — packaging + registry

* [ ] Central registry (`fusion publish/pull`) + checksums + yank.
* [ ] Vendoring: `fusion vendor` → `vendor/` offline builds.
* [ ] Private registries + token auth.
* [ ] Namespaces: `scope/name` to avoid flat-name collisions.

### P2 — DX

* [ ] REPL: `fusion repl` with history + multi-line.
* [ ] LSP + VS Code extension (hover, goto-def, rename, format-on-save).
* [ ] Debugger: `fusion run --debug`, breakpoints, `print` stack.
* [ ] Benchmarks: `fusion bench` + criterion-style reports.

## 4. Explicit non-goals

* Manual memory management / borrow checker — Go GC stays.
* Static-only typing — dynamic stays default; types are opt-in lints.
* Kernel / embedded / hard-realtime — use C/Rust there.
* Full npm/PyPI compatibility — bridges, not clones.

## 5. How to propose a future

1. Open an issue with problem + `.ks` example + Go/Rust/Node prior art.
2. Small core change + test in `internal/backend/backend_test.go` or
   `internal/frontend/frontend_test.go`.
3. Update `README.md` + this file + `docs/vs.md` if comparison changes.
