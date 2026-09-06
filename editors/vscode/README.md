# ks-fusion VS Code extension (minimal)

Language support for `.ks` files backed by `fusion lsp` (this repo:
`internal/tools/lsp.go` — hover, goto-definition, rename, formatting).

## Install (manual)

1. `go build -o fusion ./cmd/fusion` and put `fusion` on `PATH`.
2. Copy this folder to `~/.vscode/extensions/ks-fusion/`.
3. Reload VS Code. Set `ks-fusion.serverPath` if `fusion` is not on `PATH`.

## What works

- Hover over top-level funcs + builtins, goto-definition (file + line),
  rename across the app root, format-on-save via `fusion fmt` rules.
- Diagnostics: parse errors on type, `fusion vet` issues on save.

## Limits (honest)

- No completion, no breakpoints/debugger, no semantic tokens yet.
- Rename is textual (whole-word) within the app root — review diffs.
