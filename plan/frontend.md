# Frontend Plan (.ks app frontend) — TS / React / Vite / Next.js parity

> Status: planning. `frontend/main.ks` is console logic today (Frontend 3/10), not browser UI.
> Target: Frontend 3→10, Tooling 4→9, Types 6→8 for frontend code. `.ks` owns logic +
> view-model, JS owns DOM. Non-goal: reimplementing React/Vite/Next.js in `.ks` —
> copy their contracts, reuse their runtimes where possible.
> Stack: ks-fusion v2.1 (97 builtins, tree-walk) + compiler v0.1 (`.ksb-1` subset).

## 0. Parity bar (what "level" means)

| System | What we copy | .ks equivalent | Score move |
|---|---|---|---|
| TypeScript | `tsc --strict`, generics/unions, LSP hover/goto/rename | `fusion check` (parse + arity + `: type` + `is` narrowing), structs/enums, LSP | Types 6→8, Tooling 4→9 |
| React | components/props/state/hooks, keyed reconciliation, memo/context/error boundaries | view-model protocol + `key` diff runtime, `use_state`/`on_mount` store helpers, `memo`, error slots | Frontend 3→10 (logic side) |
| Vite | HMR <100ms, esbuild transpile cache, code-split, tree-shake, plugins | `fusion run --web` watch + incremental `--js` cache, per-route bundles, Go plugin builtins | Build 4→9 |
| Next.js | file routing, layouts, SSR/SSG/ISR, `/api` routes | `pages/` routing, `layout.ks`, SSR JSON → hydrate, `build --js --ssg`, `backend/api/*.ks` → JSON | Frontend 3→10 |

## 1. Structure (Next.js-level routing)

```
myapp/
  fusion.toml                  # entry_frontend = "frontend/main.ks" (keep)
  frontend/
    main.ks                    # entry: route table + layout only
    pages/home.ks              # "/"       -> func home_page(props): map
    pages/hi.ks                # "/hi"     -> func hi_page(props): map
    pages/user_[id].ks         # "/user/7" -> dynamic segment (plan; manual if first)
    components/header.ks       # func header_render(props): map
    layouts/app.ks             # func app_layout(page): map (header/footer once)
    store/app.ks               # state + fetch/decode, no view code
  backend/
    main.ks
    api/user.ks                # "/api/user" -> print json_stringify(...) (plan)
```

Conventions (review-enforced until `fusion vet` lands):
- 1 file = 1 page/component/layout/store. `main.ks` routes + composes only.
- Route table lives in `main.ks`: `if route == "/" { home_page() }`. File name = route name.
- Component returns `{key, type, props, children}`. No `print` inside components/pages.
- Flat globals today: prefix (`header_render`, `hi_page`). Migrate to
  `import "x" as h` / `h.render(p)` when namespaced imports land.

## 2. TypeScript level (`fusion check` + structs)

- `fusion check` = `tsc --noEmit` for `.ks`: parse + unused-var + arity +
  `: type` param/return checks + `is`/`is not` narrowing warnings. Runs in CI.
- Types needed for frontend props (P1 core, in order): `type Props = {title: string}`,
  enums for route/variant, exhaustive `switch` on variant (error when a case missing).
- Until structs land: `func f(p: map): map` + `assert_type(p.title, "string")` on every
  page/component boundary + `?.`/`??` on all optional props.
- LSP (P2 DX): hover/goto-def/rename/format-on-save for `frontend/`; `check` powers diagnostics.

## 3. React level (component runtime contract)

- Props/state: `props: map` in, view-model map out. Local state via
  `store/app.ks` `use_state(key, init)` helper (map + `merge()` + one `render(state)` per tick).
- Effects: `on_mount(fn)` / `on_props(fn)` store helpers backed by
  `go` + `select { case v = recv(ch) / case timeout(ms) }`. Never bare `recv` on render path.
- Reconciliation: runtime diffs by stable `key`, patches only changed `props`/text.
  Keys explicit and stable (`key: "user-" + str(id)`); index-keys banned by `vet`.
- `memo(fn)`: cache by `json_stringify(props)` for pure components (lists, headers).
- Context: single `state` map threaded as first arg; no globals except component funcs.
- Error boundaries: every fetch returns `ok()/err()`; `is_err(r)` renders `{type: "error"}`
  slot. `try/catch` at page root maps to error slot, never blank screen.

## 4. Vite level (`run --web` DX + `build --js` bundles)

- `fusion run --web`: Go `net/http` serves `frontend/`, watches files, re-parses only
  changed file + dependents, re-renders, pushes patch over WebSocket. Budget: <100ms reload.
- `fusion build --js`: transpile safe subset per route (maps/arrays/strings/json,
  `ok/err`, `is`/`?.`/`??`, control flow, funcs). Reject `go/chan/select/sleep/files`
  with file:line + interpreter fallback note (same UX as compiler v0.1 subset errors).
- Per-route code-split + tree-shake: only imported funcs + used builtins per bundle.
  Shared `store/` chunked once. Bundle budget per page (e.g. warn over 100KB, fail over 250KB).
- Plugins: Go `backend.Value` API for custom builtins (charts, markdown) without forking;
  CSS/Tailwind classes pass through as strings — no CSS-in-`.ks`.
- Cache: content-hash incremental builds (`target/web/`), `--dis`-style bundle inspect.

## 5. Next.js level (rendering modes + API)

- Routing: static (`pages/hi.ks` → `/hi`) first, dynamic (`user_[id].ks`) second.
  `layouts/app.ks` wraps every page (header/footer fetched once).
- SSR: backend renders view-model JSON (`json_stringify(page(props))`), server HTML-shells it,
  browser hydrates + takes over diffing. SSG: `build --js --ssg` pre-renders JSON at build time.
  ISR: `revalidate: N` field in page map re-renders on interval (later).
- API routes: `backend/api/*.ks` → `/api/*` JSON. Frontend `store/` fetches via thin
  `fetch_json(path)` helper (today: file/stdout stub, after P1 `http_*`: real fetch).
  Contract test: `*_test.ks` asserts `json_parse(fetch("/api/user")) is map`.
- SEO/meta: `head: {title, desc}` map in page model rendered to `<head>` by runtime.

## 6. Budgets (speed / response)

- Load: TTFR <1s local, per-route JS <100KB warn / <250KB fail, shared chunk cached.
- Response: optimistic render from cached `state`, reconcile on `recv_timeout` data;
  debounce store events to 1 render/tick; lists virtualized past ~100 rows (runtime-owned).
- Measure: `fusion bench` page-render case + bundle-bytes report in `build --js` output.

## 7. Safety

- Boundary checks: `assert_type` + `is` on all API/store data; `?.`/`??` on externals.
- XSS: text escaped by default; raw HTML only via `js_call("set_html", ...)` allowlist,
  never with user input. `vet` bans `set_html` with non-literal unless sanitized helper used.
- No `eval`, no remote dynamic `import`. Secrets via backend `env()` only — `build --js`
  fails if `env(` appears in `frontend/`.
- Gates: `fusion fmt` + `fusion vet` + `fusion test` (`*_test.ks`, assert + TAP) required per page.

## 8. Phases (exit criteria)

1. Conventions (no toolchain): `tests/hello-app/frontend/` matches §1 + route table. Exit: review passes.
2. `fusion check` + namespaced `import as` + `vet` (prefix/arity/`set_html`/index-key rules). Exit: CI runs `check`.
3. `run --web` SSR-hydrate + watch <100ms on hello-app. Exit: edit `hi.ks` → browser patches, no full reload.
4. `build --js` per-route bundles + `js_call` sync bridge + SSG JSON. Exit: `/` + `/hi` load without `fusion` running.
5. Budgets + API routes + dynamic segments + ISR field. Exit: bench + bundle report in build output.

## 9. Non-goals / open decisions

- Non-goals: vDOM in `.ks`, CSS-in-`.ks`, reimplementing React/Vite/Next.js, remote imports.
- Open: explicit `key` vs auto file+func key (lean explicit); sync-only `js_call` v1;
  Tailwind pass-through vs runtime class map; `[id]` syntax (`user_[id].ks` vs `user/:id`).
