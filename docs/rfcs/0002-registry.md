# RFC 0002: Registry (`publish/pull/yank` + sha256)

- Problem: local newest-wins, no sharing/checksums/yank.
- Proposal: file registry (`FUSION_REGISTRY`, `./registry`, `~/.fusion/registry`) + `index.json` + `.sha256` + `scope/name` + private token.
- Prior art: cargo registry, Go proxy.
- Done v2.4: `tools/registry.go` + `fusion audit`.
