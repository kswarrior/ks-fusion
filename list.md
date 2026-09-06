# Missing List — v2.2 done (score 69/100 → 78–82 next)

> Source: `docs/vs.md` gaps + `docs/futures.md` roadmap + `plan/frontend.md` + `plan/tooling.md`.
> Checked = exists today. Unchecked = missing. Score impact in brackets.
> v2.2: 50 → 69 (+19: Perf+1 Types+1 Concurrency+1 Stdlib+4 Ecosystem+2 Tooling+3 Build+3 Frontend+2 Maturity+2).

## Tooling P0 (Tooling 5→8 done, Maturity 3→5 done)

- [x] `fusion new/run/build/launch/compile` (launch has --backend/--frontend/--config/--race)
- [x] `fusion version`, `run/build/launch --help`, v2.2 single constant
- [x] `fusion fmt` + `--check` (canonical format, idempotent) [+1 Tooling done]
- [x] `fusion vet` (unused let, arity, unknown var, env-in-frontend) [+1 Tooling done]
- [x] `fusion test` (`*_test.ks`, assert, TAP, isolation; per-file timeout still missing)
- [x] `fusion doc` from `#` comments (done v2.2)
- [x] `fusion check` strict (parse + arity + `: type` + `is` narrowing) (done v2.2)
- [ ] Namespaced imports (`import "x" as h`) — today flat globals
- [x] `fusion.lock` + semver resolver (`^`, `~`, `>=`, path) — done v2.2 (git deps left)

## Runtime P1 (Perf 5→6 done, Build 4→7 done)

- [x] Tree-walk opts (lock-free scopes, O(n log n) sort, O(log n) pow)
- [x] Compiler v0.1 subset (`.ksb-1`, 7 builtins, no go/chan/try/switch/slices/is/?./??)
- [ ] Full-language VM (go/chan/select, import, try, switch, defer, slices, is/?./??, typed params) [+2 Perf left]
- [x] `fusion build --bin` single static executable [+3 Build done]
- [x] `--target` matrix (linux/amd64,arm64,darwin,windows,wasm via GOOS/GOARCH) (done v2.2)
- [ ] `--cpuprofile`, opcode counts
- [x] Constant folding (done v2.2); [ ] local-slot lookup, string-builder concat
- [x] `fusion run --race`, `with_timeout`/`parallel` (done v2.2); [ ] deterministic scheduler

## Language core P1 (Types 6→7 done)

- [x] Gradual `: type`, `is`/`is not`, `?.`/`??`, `ok`/`err` + helpers + `struct_validate/enum_create` (done v2.2)
- [ ] Structs syntax / typed maps (`type User = {name: string}`) [+1 Types left]
- [ ] Enums syntax + exhaustive `switch` check (helpers done, syntax left)
- [ ] Default args / named params / variadics (`...rest`)
- [ ] Modules with `export` (compat shim over flat globals)
- [x] Regex `regex_*` builtins (done v2.2); [ ] Regex literal
- [ ] `defer` inside `go` routines

## Stdlib P1 (Stdlib 4→8 done)

- [x] 149 builtins (v2.1 97 + v2.2 52: http/regex/crypto/fs/process/time/db/log/concurrency/types)
- [x] `http` (`http_get/post/serve`, headers, JSON codec) [+2 Stdlib done]
- [ ] `net/ws` TCP + WebSocket [+1 Stdlib left]
- [x] `fs` full (`stat/cp/mv/glob`, path joins) (done; `watch` left)
- [x] `process` (`exec/shell/cwd/env_all`) (pipes/signals left)
- [x] `time` (`format/parse/parts`) (ticker left)
- [x] `crypto/hash` (`sha256/md5/hmac/base64/hex/uuid/random_bytes`) (done)
- [x] `db` KV (`db_put/get/delete/list` JSON-file) (sqlite/postgres native left)
- [x] `log/flags/testing` CLI helpers (`log_*`, `assert_eq/ne/contains`) (flags left)

## Frontend P2 (Frontend 3→5 done)

- [x] P0: `pages/components/layouts/store`, route table, view-model `{key,type,props,children}`, `ROUTE` switch, `fusion new` scaffolds it
- [x] P1: `fusion check` + `vet` frontend rules (done v2.2)
- [x] P2: `fusion run-web` SSR (HTML+JSON, `/api/*`) (done v2.2, HMR left)
- [ ] P3: client hydrate + `use_state`/`on_mount` (stub done, full left) + `fetch_json` (done)
- [x] P4: `fusion build-js` per-route + manifest + budgets (done v2.2)
- [ ] P5: SSG pre-render; P6: `backend/api/*.ks` full routes (stub done); P7: bundle budgets (done); P8: cache/virtualize/bench (bench done)
- [ ] Non-goal reminder: no React/Vite/Next reimplementation, no CSS-in-`.ks`

## Packaging + registry P2 (Ecosystem 3→5 done)

- [ ] Central registry (`publish/pull`, checksums, yank)
- [x] `fusion vendor` offline builds (done v2.2); [ ] private registries + token auth; `scope/name` namespaces

## DX P2 (Tooling/Maturity)

- [x] REPL (`fusion repl` history + multiline) (done v2.2)
- [ ] LSP + VS Code extension (hover, goto-def, rename, format-on-save)
- [ ] Debugger (`--debug`, breakpoints, stack)
- [x] Benchmarks (`fusion bench`) (done v2.2, criterion-style reports left)

## Interop P2

- [ ] C FFI (`ffi_open/load/call`, opt-in unsafe)
- [ ] Go plugin API (`backend.Value` custom builtins — `Lookup/Call/ValueToJSONable` exported as start)
- [ ] Node/Python bridges (have `exec`/`shell` + JSON stdio helpers)

## Explicit non-goals (never build)

- Manual memory / borrow checker; static-only typing; kernel/embedded/realtime; npm/PyPI clone
