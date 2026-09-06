# Tooling Plan P1 — fmt / vet / test (like Go + cargo)

> Goal: Tooling 4→7, Maturity 3→5. Small Go code, every dev feels it.
> Non-goal: full LSP/debugger/profiler (P2 DX). This is gates only.
> Stack: `internal/frontend` (lexer+parser) + `cmd/fusion` + `internal/backend`.

## 0. Contracts (what we copy)

| System | Contract | .ks equivalent | Priority |
|---|---|---|---|
| `go fmt` / `cargo fmt` | One canonical format, idempotent, CI-checkable | `fusion fmt [path]` rewrites `.ks`; `fmt --check` fails CI on diff | High |
| `go vet` / `clippy` | Catch likely bugs, never false-positive on valid code | `fusion vet [path]`: unused `let`, arity, unknown var, index-keys, `set_html` literal, `env(` in `frontend/` | High |
| `go test` / `cargo test` | `*_test` files, assert, TAP/exit-code, fast | `fusion test [dir]`: runs `*_test.ks`, `assert` failures = FAIL, TAP output, non-zero exit | High |

## 1. `fusion fmt` spec

- Formats all `.ks` under target (default `.`): normalize indent (2 spaces),
  braces (`} else {` same line), spaces around `=`/`==`/`,`/`:` in maps,
  single trailing newline, no trailing whitespace. Never changes strings/comments/AST.
- Idempotent: `fmt(fmt(x)) == fmt(x)`. `--check` prints diff list, exits 1 when dirty.
- Exit: `fmt` on `tests/hello-app` is no-op second run; `--check` green in CI.

## 2. `fusion vet` spec (AST-only, no execution)

- unused-let: `let x` never read in scope → warn (allow `_` prefix to silence).
- arity: call arg count vs `func` params (user funcs only, builtins skipped v1).
- unknown-var: `x = ...` / bare `x` with no `let`/`func`/param/builtin in chain → error.
- frontend rules (warn): index-key `key: i` in `frontend/`; `set_html` with non-literal;
  `env(` inside `frontend/` (must be backend-only).
- Output: `file:line: rule: message`, exit 1 on error, 0 with warns only (`--deny-warns` flips).
- Exit: `vet ./tests/hello-app` clean; catches planted bad file in test.

## 3. `fusion test` spec

- Discovers `*_test.ks` under target (default `.`), runs each in fresh interpreter
  with app dir as baseDir (imports work). `assert(cond, msg?)` failure = test FAIL.
- Output TAP: `TAP version 13`, `ok 1 - <file>`, `not ok N - <file> (line L: msg)`,
  `1..N`, summary `PASS/FAIL`. Exit 0 all-pass, 1 otherwise.
- Isolation: one file = one process-state (no shared globals between files).
  Timeout per file (e.g. 5s) kills hung `while`/`recv`.
- Exit: `test ./tests/hello-app` runs example `frontend/*_test.ks` + `backend/*_test.ks` green.

## 4. Phases + checklist

- [x] P1.0 Example tests: add `tests/hello-app/frontend/pages/home_test.ks`
  (`assert(home_page({}).key == "home")`) + `store/app_test.ks` (`assert(is_ok(app_fetch_user()))`).
- [ ] P1.1 `fmt`: implement printer from `frontend` AST + `--check`; run on repo until clean.
- [ ] P1.2 `vet`: AST walkers for 6 rules above; `cmd/fusion` wiring + `--deny-warns`.
- [x] P1.3 `test`: discovery + runner + TAP + timeout; wire `launch` env (`ROUTE`) passthrough.
  Done except per-file timeout (documented limitation: a hung file blocks the run).
- [ ] P1.4 CI gate: README + help text; `vet` + `test` run in CI script; `vs.md` Tooling 4→7 note.
- [ ] P1.5 Docs: `fusion help` + README `Use` block updated; `futures.md` P0 boxes checked.

## 5. Open decisions

- `fmt` config file? No — single style v1 (lean gofmt).
- `vet` auto-fix? No — report only v1.
- `test` parallel? Sequential v1 (deterministic TAP numbers); parallel later.
