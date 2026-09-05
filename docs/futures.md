# ks-fusion Futures / Roadmap

> Current: `v2.0` — tree-walk interpreter in Go, dynamic `.ks` language.
> This doc lists what exists, what is planned, and what is explicitly non-goals.

## 1. Where we are (v2.0, real today)

* Frontend: lexer + parser (`internal/frontend`) — `# // /* */` comments,
  `"double"`/`'single'` strings, `0xFF 0b101 0o17 1_000 1e3 .5` numbers.
* Backend: tree-walk interpreter (`internal/backend`) with closures,
  `if/while/for-in/for-c/switch/try-catch-finally/defer/break/continue`.
* Types: `nil bool int float string array map func chan` — dynamic only.
* Concurrency: `go` + `chan(n) send/recv/close/try_send/try_recv/chan_len/chan_cap/sleep`
  backed by Go goroutines.
* Stdlib (~80 builtins): `len str int float bool type range`,
  array/map/string helpers, `math/time/rand/bit_*`,
  `map/filter/each/reduce/apply`, `json_stringify/json_parse`,
  `read_file/write_file/append_file/exists/list_dir/mkdir/remove/input/argv/env/exit`.
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

* [ ] `select` for channels (with `timeout` / `default`), currently only
  blocking `recv` + `try_recv`.
* [ ] `fusion fmt` (canonical formatter) + `fusion vet` (unused var, bad arity lint).
* [ ] `fusion test` runner: `*_test.ks` with `assert`, exit-code + TAP output.
* [ ] `fusion doc` from `#` comments.
* [ ] Namespaced imports: `import "hello-lib" as hl` / `hl.greet()`.
  Today: flat globals, prefix your functions.
* [ ] `fusion.lock` + real semver resolver (`^0.1.0`, `>=`, path + git deps).
  Today: newest `test-releases/<name>-<ver>.kslib` wins.
* [ ] Error values (`Result`-style) as alternative to `error(msg)` abort +
  `try/catch`. Keep both.

### P1 — runtime + performance

* [ ] Bytecode compiler + VM (drop tree-walk hot loop, 5–20x speedup target).
  Keep `RunFile` / shebang behavior identical.
* [ ] `fusion build --bin` → single static executable (embed VM + bytecode,
  like `go build`). Today output is source JSON or parse-check only.
* [ ] Cross-compile: `--target linux/amd64,arm64,darwin,windows,wasm`.
* [ ] Profiler hooks: `fusion run --cpuprofile`, opcode counts.
* [ ] Optimizations: constant folding, local-slot lookup (replace scope-chain
  scan on hot paths), string-builder concat fast path.
* [ ] WASM target: run `.ks` in browser / edge workers.

### P1 — language core

* [ ] `select/sleep` timeouts, `chan` iteration `for v in ch`, buffered-chan semantics docs.
* [ ] Structs / typed maps: `type User = {name: string, age: int}` (optional static check).
* [ ] Enums + exhaustive `switch` check.
* [ ] `?.` nil-safe access + `??` default operator (today: `get(m,k,default)`).
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
