# Frontend Plan (.ks app frontend)

> Status: planning. `frontend/main.ks` is console logic today, not browser UI.
> Non-goal: reimplementing React in `.ks`. `.ks` owns logic + view-model, JS owns DOM.
> Stack: ks-fusion v2.1 (97 builtins, tree-walk) + compiler v0.1 (`.ksb-1` subset).

## 1. Structure

```
myapp/
  fusion.toml                  # entry_frontend = "frontend/main.ks" (keep)
  frontend/
    main.ks                    # entry: routing + layout only, no business logic
    pages/home.ks              # one func per page: func page_home(props)
    components/header.ks       # one func per component: func header_render(props)
    store/app.ks               # shared state + fetch/decode helpers
  backend/main.ks              # JSON API worker (read_file -> json_stringify -> stdout, later http_*)
```

Conventions (enforce by review until `fusion vet` lands):
- 1 file = 1 component/page/store. `main.ks` composes, never fetches directly.
- Component signature: `func <prefix>_<name>(props): map` returning
  `{key, type, props, children}` view-model. No `print` inside components.
- Flat globals today: prefix everything (`header_render`, `home_page`).
  Switch to `import "x" as h` / `h.render()` when namespaced imports land (P0).

## 2. Imports

- Today: `import "frontend/components/header.ks"` (app-root relative, flat globals).
- P0 fix: `import "frontend/components/header.ks" as header`, call `header.render(p)`.
- Until then: lint rule — every exported func starts with `<file>_`.

## 3. React-like updates (data-diff, no vDOM in .ks)

- Components return data, runtime patches DOM:
  `{key: "header", type: "header", props: {title: "hi"}, children: [...]}`
- Runtime (`fusion run --web` Go server, later `fusion build --js` output) diffs
  by `key`, updates only changed `props`/text nodes.
- State rule: single `let state = {...}` in `store/app.ks`, mutate via
  `merge()` + one `render(state)` call per event. No DOM mutation from `.ks`
  except via `js_call(name, args)` in JS output (sync-only at first).
- `?.` / `??` required on all optional props: `props?.title ?? "untitled"`.
- `ok()/err()` for every fetch: `is_ok(r)` renders data, `is_err(r)` renders error slot.

## 4. Speed (load)

- `--js` code-split by page: only `pages/<route>.ks` + its components ship per route.
- Lazy import: `import` inside `if route == "/admin"` branch, not top-level.
- Memo pure components: `memo(fn)` helper caches by `json_stringify(props)`.
- Keep per-page bundle small: no full-stdlib dump; tree-shake unused builtins
  in transpiler. Reuse compiler v0.1 subset discipline (arithmetic/control/funcs first).
- Measure: time-to-first-render + bundle bytes per page; add `fusion bench` case later.

## 5. Response time

- Backend contract: JSON over stdout/file today
  (`read_file` -> `json_parse` -> `json_stringify`), `http_*` when P1 stdlib lands.
- Frontend never blocks: `recv_timeout(ch, ms)` / `send_timeout`, never bare `recv`
  on the render path. Cache last-good `state`, render optimistic then reconcile.
- Live data: `select { case v = recv(ch) / case timeout(ms) / default }` in store,
  debounced re-render (one render per tick, not per message).

## 6. Safety

- Types: `func f(p: map): map`, `let title: string`, `assert_type` on API boundary.
- Nil-safety: `?.` / `??` on all external data; `assert(r is ok)` in tests.
- XSS: runtime escapes all text by default; raw HTML only via explicit
  `js_call("set_html", ...)` with allowlisted caller — never with user input.
- No `eval`, no dynamic `import` of remote URLs. `fusion vet` (unused/bad-arity)
  + `fusion test` (`*_test.ks`, assert + TAP) gate every component.
- Secrets: `env(name)` read in backend only, never shipped to `--js` bundle.

## 7. Phases

1. Conventions only (this doc + `tests/hello-app/frontend/` example). No toolchain change.
2. Namespaced imports (`as` alias) + `fusion vet` prefix/arity checks.
3. `fusion run --web`: Go serves `frontend/`, watches files, diff-renders by `key`.
4. `fusion build --js`: subset transpiler (maps/arrays/json/ok-err/is/?./?? first),
   per-page bundles + `js_call` bridge.
5. Perf/safety gates: `fusion test`, `fusion bench`, bundle-size budget.

## 8. Open decisions

- `key` stability: file+func auto-key vs explicit `key` prop (lean explicit first).
- Async bridge: sync `js_call` only, or promise-backed (lean sync-only v1).
- Styling ownership: Tailwind/classes pass-through strings, no CSS-in-`.ks`.
