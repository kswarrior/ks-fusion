# Missing List — everything not yet done (score 50/100 → 75–80)

> Source: `docs/vs.md` gaps + `docs/futures.md` roadmap + `plan/frontend.md` + `plan/tooling.md`.
> Checked = exists today. Unchecked = missing. Score impact in brackets.

## Tooling P0 (Tooling 5→9, Maturity 3→8)

- [x] `fusion new/run/build/launch/compile` (launch has --backend/--frontend/--config)
- [x] `fusion version`, `run/build/launch --help`, v2.1 single constant
- [ ] `fusion fmt` + `--check` (canonical format, idempotent) [+1 Tooling]
- [ ] `fusion vet` (unused let, arity, unknown var, index-keys, set_html, env-in-frontend) [+1 Tooling]
- [x] `fusion test` (`*_test.ks`, assert, TAP, isolation; per-file timeout still missing)
- [ ] `fusion doc` from `#` comments
- [ ] `fusion check` strict (parse + arity + `: type` + `is` narrowing, structs/enums later)
- [ ] Namespaced imports (`import "x" as h`) — today flat globals
- [ ] `fusion.lock` + semver resolver (`^`, `>=`, path + git deps) — today newest-wins

## Runtime P1 (Perf 5→8, Build 4→9)

- [x] Tree-walk opts (lock-free scopes, O(n log n) sort, O(log n) pow)
- [x] Compiler v0.1 subset (`.ksb-1`, 7 builtins, no go/chan/try/switch/slices/is/?./??)
- [ ] Full-language VM (go/chan/select, import, try, switch, defer, slices, is/?./??, typed params) [+3 Perf]
- [ ] `fusion build --bin` single static executable [+3 Build]
- [ ] `--target` matrix (linux/amd64,arm64,darwin,windows,wasm) + WASM run
- [ ] `--cpuprofile`, opcode counts
- [ ] Constant folding, local-slot lookup, string-builder concat
- [ ] `fusion run --race`, `with_timeout`/cancel, deterministic scheduler

## Language core P1 (Types 6→8)

- [x] Gradual `: type`, `is`/`is not`, `?.`/`??`, `ok`/`err` + helpers
- [ ] Structs / typed maps (`type User = {name: string}`) [+1 Types]
- [ ] Enums + exhaustive `switch` check [+1 Types]
- [ ] Default args / named params / variadics (`...rest`)
- [ ] Modules with `export` (compat shim over flat globals)
- [ ] Regex literal / `regex_*` builtins
- [ ] `defer` inside `go` routines

## Stdlib P1 (Stdlib 4→9)

- [x] 97 builtins (strings/arrays/maps/JSON/files/math/time/rand/bit/functional/chan)
- [ ] `http` (`http_get/post/serve`, headers, JSON codec) [+2 Stdlib]
- [ ] `net/ws` TCP + WebSocket [+1 Stdlib]
- [ ] `fs` full (`stat/cp/mv/glob/watch`, path joins)
- [ ] `process` (`exec`, pipes, signals)
- [ ] `time` (`date/format/parse/ticker`)
- [ ] `crypto/hash` (`sha256/hmac/random_bytes/uuid`)
- [ ] `db` (`sqlite_*`, then `postgres_*`)
- [ ] `log/flags/testing` CLI helpers

## Frontend P2 (Frontend 3→10)

- [x] P0: `pages/components/layouts/store`, route table, view-model `{key,type,props,children}`, `ROUTE` switch, `fusion new` scaffolds it
- [ ] P1: `fusion check` + `vet` frontend rules (done when Tooling P1 lands)
- [ ] P2: `fusion run --web` SSR + HMR <100ms [+3 Frontend]
- [ ] P3: client hydrate + `use_state`/`on_mount` + `fetch_json`
- [ ] P4: `fusion build --js` per-route split/shake/minify [+2 Frontend]
- [ ] P5: SSG pre-render; P6: `backend/api/*.ks` routes; P7: bundle budgets; P8: cache/virtualize/bench
- [ ] Non-goal reminder: no React/Vite/Next reimplementation, no CSS-in-`.ks`

## Packaging + registry P2 (Ecosystem 3→8)

- [ ] Central registry (`publish/pull`, checksums, yank)
- [ ] `fusion vendor` offline builds; private registries + token auth; `scope/name` namespaces

## DX P2 (Tooling/Maturity)

- [ ] REPL (`fusion repl` history + multiline)
- [ ] LSP + VS Code extension (hover, goto-def, rename, format-on-save)
- [ ] Debugger (`--debug`, breakpoints, stack)
- [ ] Benchmarks (`fusion bench` criterion-style)

## Interop P2

- [ ] C FFI (`ffi_open/load/call`, opt-in unsafe)
- [ ] Go plugin API (`backend.Value` custom builtins)
- [ ] Node/Python bridges (subprocess + JSON stdio)

## Explicit non-goals (never build)

- Manual memory / borrow checker; static-only typing; kernel/embedded/realtime; npm/PyPI clone
