# test-releases/

Built ks-fusion library bundles (`.kslib`), like Rust's `target/release/*.rlib`.

Do not hand-edit: these are build outputs. Rebuild with:

```bash
fusion build --release ./tests/hello-lib
```

Debug builds go to `target/` instead (`fusion build ./tests/hello-lib`).
Apps use a bundle with `import "<name>"` after declaring it under
`[dependencies]` in `fusion.toml`.
