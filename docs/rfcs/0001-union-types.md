# RFC 0001: Union + generic types (`int|string`, `array<int>`)

- Problem: gradual types too coarse (`any` everywhere, no `nil`-safe unions).
- Proposal: `|` unions + `array<T>`/`map<K,V>` in annotations + `is "T"` strings, nullable by default.
- Prior art: TypeScript unions/generics, Go generics, Python `X | Y`.
- Done v2.4: `parseTypeName` + `validType`/`matchesTypeStrict` + `vet exhaustive-switch`.
