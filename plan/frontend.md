# Frontend Plan (.ks app frontend) — TS / React / Vite / Next.js parity

> Executive summary: upgrade Frontend 3→10, Tooling 4→9, Types 6→8.
> Core idea: `.ks` drives logic + view-models, plain JS handles the DOM.
> Copy React/Vite/Next contracts, don't reimplement them.
> Pipeline: `.ks` page → view-model map → JS runtime diff → DOM.
> Stack: ks-fusion v2.1 (97 builtins, tree-walk) + compiler v0.1 (`.ksb-1` subset).

## 0. System contracts (what we copy)

| System | Contract to copy | .ks equivalent | Priority |
|---|---|---|---|
| React | Functional components + props/state/hooks (`useState`, `useEffect`); keyed reconciliation with `key`; context; error boundaries | Pages/components return `{type, props, children}` maps; `use_state(key, init)`; `on_mount`/`on_props`; runtime diff by `key`; `memo` (props-JSON cache); single state as context; `{type:"error"}` slots | High |
| Vite | Dev server HMR (<50ms), esbuild transpile, per-file modules; Rollup bundle + tree-shake + code-split + minify | `fusion run --web`: watch + incremental re-parse/re-render + WebSocket diff push (aim <100ms); `fusion build --js`: per-route bundle, split, shake, minify analogue; content-hash cache; plugin hooks (e.g. Markdown) | High |
| Next.js | File routing, static/dynamic pages, SSR/SSG/ISR, layouts, `/api` JSON routes | `frontend/pages/` routing (`[param]` dynamic); `layouts/` wrappers; SSR `.ks`→JSON→HTML + hydrate; SSG pre-render; `revalidate` for ISR; `backend/api/*.ks` → `/api/*` | High |
| TypeScript | `tsc --strict`, interfaces/unions, LSP | `fusion check` (parse + arity + `: type` + `is` narrowing) + structs/enums + exhaustive `switch`; LSP hover/goto/rename | High |

## 1. Structure (Next.js-level routing)

```
myapp/
  fusion.toml                  # entry_frontend = "frontend/main.ks" (keep)
  frontend/
    main.ks                    # entry: route table + layout only
    pages/home.ks              # "/"       -> func home_page(props): map
    pages/hi.ks                # "/hi"     -> func hi_page(props): map
    pages/user_[id].ks         # "/user/7" -> dynamic segment (manual if first)
    components/header.ks       # func header_render(props): map
    layouts/app.ks             # func app_layout(page): map
    store/app.ks               # state + fetch/decode, no view code
  backend/
    main.ks
    api/user.ks                # "/api/user" -> print json_stringify(...)
```

- 1 file = 1 page/component/layout/store. `main.ks` routes + composes only.
- Component returns `{key, type, props, children}`. No `print` inside components.
- Flat globals today: prefix (`header_render`, `hi_page`). Migrate to
  `import "x" as h` / `h.render(p)` when namespaced imports land.
- Route table in `main.ks`: `if route == "/" { home_page() }`. File name = route name.

## 2. Minimal viable pipeline (.ks → view-model → DOM)

1. Render: `home_page(props)` returns `{type:"div", key:"...", props:{...}, children:[...]}`.
2. SSR/hydrate: backend renders page → HTML+JSON; browser loads HTML, JS runtime
   hydrates by diffing view-model JSON into DOM + attaching listeners.
3. Updates (CSR): state/props change → re-render new view-model → JS diffs vs old,
   patches only changed nodes. Stable `key`s make list reorder/insert cheap.
4. State/effects: `use_state` instance persists across updates (props/children change,
   instance stays — React rule). `on_mount` + `select/recv_timeout` fetch after render.

## 3. Phased roadmap P0–P10 (exit = concrete check)

- P0 Conventions: layout above + naming rules. Exit: review OK on hello-app.
- P1 Type-check & vet: `fusion check` strict + `as` imports + `vet` bans
  (non-literal `set_html`, index keys) + structs/enums for props. Exit: CI green.
- P2 SSR + HMR dev server: `fusion run --web` SSR on change + WebSocket patch, no full reload.
  Exit: edit `hi.ks` → browser patches in <100ms.
- P3 Hydrate + state: client runtime hydrate + `use_state`/`on_mount` + `fetch_json`.
  Exit: SSR page becomes interactive, fetches data.
- P4 Bundler: `fusion build --js` per-route transpile/split/shake/minify.
  Exit: `/` + `/hi` load as static HTML+JS without `fusion`.
- P5 SSG: `build --ssg` pre-renders JSON+HTML to `target/`. Exit: cached serve works.
- P6 API routes: `backend/api/*.ks` → `/api/*` JSON. Exit: `fetch("/api/user")` returns map.
- P7 Budgets: warn >100KB, fail >250KB per route; log sizes. Exit: warnings in build output.
- P8 Optimizations: content-hash incremental cache, debounce 1 render/tick,
  virtualize lists >100 rows. Exit: `fusion bench` render times OK.
- P9 Testing/CI: `fusion test` TAP unit + `/api` contract tests + `vet` in CI.
  Exit: all green, vet catches regressions.
- P10 Release polish: ISR `revalidate`, extra hooks, LSP, risk fixes. Exit: prod-ready demo.

## 4. Build/tooling (Vite-like)

- Dev skips bundling: serve route modules + HMR over native ESM-style patches.
  `check` runs async (`check --watch` / LSP), never blocks HMR.
- Prod transpiles safe `.ks` subset to JS, splits by route, includes only used
  funcs/builtins (Rollup-style), minifies (esbuild analogue), hashes assets.
- No CSS-in-`.ks`: Tailwind/classes pass through as strings, styles linked externally.

## 5. Types + LSP (TS-like)

- `fusion check` = `tsc --noEmit`: arity + `: type` + `is`/`is not` narrowing +
  exhaustive `switch`. Structs/enums model props; `?.`/`??` on all externals.
- Without generics: maps + `assert_type()` at boundaries.
- LSP: hover/goto/rename/format; diagnostics = `check` rules (undef refs, bad props).

## 6. Safety

- React rule: escape all text by default. Raw HTML only via allowlisted
  `js_call("set_html", ...)` + `vet` + sanitizer for Markdown/CMS HTML. No `eval`, no remote import.
- Next.js rule: secrets server-only. Only `backend/` may call `env()`;
  `env(` in `frontend/` is a `check` error. `fetch_json(path)` validates JSON shape.

## 7. Testing + budgets

- `fusion test` TAP: page unit tests (`assert(... is ok)`), `/api` contract tests
  (`json_parse(fetch(...)) is map`), HMR smoke (edit → partial DOM patch).
- Budgets: TTFR <1s local, HMR <100ms, route JS <100KB warn / <250KB fail.
  Bench render + memory; audit with devtools/Lighthouse.

## 8. Open issues

- `key`: explicit required (no index keys); auto file+func key later?
- `js_call`: sync-only v1; promises later.
- State across navigations/ISR: full reload v1.
- Nested layouts (`_app.ks`): spec TBD. Tests lag features: phase gates enforce.
