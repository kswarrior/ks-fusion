# Stability promise (v2.4, LTS)

> ks-fusion follows semver (`fusion.toml` version + `fusion.lock` resolver).
> Language `v2.x` is backward compatible: valid `v2.0` programs run on `v2.4`
> (new builtins/flags only add, never remove or change semantics).
> Breaking changes land as `v3.0` with migration guide + `fusion vet` autofix hints.

## LTS

- Even minors (`v2.2`, `v2.4`) are LTS: critical fixes backported 12 months.
- Odd minors are feature trains.
- Toolchain reports single `toolVersion` (`cmd/fusion/main.go`); docs (`README`, `vs.md`, `futures.md`) stay in sync.

## Compatibility checks

- `fusion check` + `fusion vet` run in CI; `fusion test` TAP must stay green.
- `fusion build` cache is content-hashed; `--bin` is reproducible (`-trimpath`, sorted embeds).
- Registry entries are immutable: publish creates new version, `yank` hides (or `--remove` deletes); checksums verified on `pull`/`build`.

## RFC process

- Propose in `docs/rfcs/NNNN-title.md` (problem + `.ks` example + Go/Rust prior art).
- Small core change + test (`backend_test.go` / `frontend_test.go` / `tools` tests).
- Update `README.md` + `futures.md` + `docs/vs.md` when comparisons change.
