---
name: ks-fusion
version: v2.7
description: Write, edit, run, test and package KS Fusion (.ks) apps and libs. Use when asked to create or modify .ks files, fusion.toml, frontend/backend code, or run fusion CLI commands.
---

# KS Fusion Skill (v2.7) — for AI models with no prior knowledge

> KS Fusion is a small complete programming language (`.ks` files).
> Motto: **easy like Python, concurrency like Go, packaging like Rust (UX copy, not parity).**
> Toolchain (`fusion` CLI) is written in Go. Go is the implementation language — `.ks` is the real language.
> 177 builtins. Interpreter runs the full language; `fusion compile` covers a subset only.

## 1. Setup — check for a working `fusion` binary

```bash
./fusion version   # -> ks-fusion v2.7
```

If that command does not work, STOP. Do not try to build it, download it, or work around it. Tell the user to install it first, then continue.

All commands below assume `fusion` is on PATH (or use `./fusion`).

## 2. Project shapes — ALWAYS check these first

Before writing any `.ks`, read `fusion.toml` in the target dir. It tells you app vs lib and entry points.

### 2a. App (default: `fusion new myapp`)

```
myapp/
  fusion.toml
  backend/main.ks
  backend/api/<name>.ks      # optional, for run-web /api/<name>
  frontend/main.ks           # route table only, no business logic
  frontend/pages/*.ks        # one func per route: <name>_page(props)
  frontend/pages/user_[id].ks # dynamic route /user/:id -> user_page(props) with props.id
  frontend/components/*.ks  # funcs like header_render(props)
  frontend/layouts/*.ks     # funcs like app_layout(page)
  frontend/store/*.ks       # state + helpers, no view code
  shared/util.ks            # optional, via import
```

`fusion.toml` (app):

```toml
[package]
name = "myapp"
version = "1.0.0"
entry_backend = "backend/main.ks"
entry_frontend = "frontend/main.ks"

[dependencies]          # optional
hello-lib = "0.1.0"     # semver ^ ~ >= > < * and , supported
```

Entry paths resolve **relative to the folder containing fusion.toml**.
A custom config file must also live in the app root:
`fusion launch ./myapp/custom.toml` — OK; `fusion launch ./elsewhere/custom.toml` — fails.

### 2b. Lib (`fusion new --lib mylib`)

```
mylib/
  fusion.toml
  src/lib.ks
```

`fusion.toml` (lib):

```toml
[package]
name = "hello-lib"
version = "0.1.0"
type = "lib"

[lib]
name = "hello-lib"
path = "src/lib.ks"
```

Build libs (like cargo — `test-releases/` = release, `target/` = debug):

```bash
fusion new myapp
fusion new --lib mylib
fusion build ./tests/hello-app                  # app: parse-check + verify deps + write fusion.lock
fusion build ./tests/hello-lib                  # lib debug -> target/<name>-<ver>.kslib
fusion build --release ./tests/hello-lib        # lib release -> test-releases/<name>-<ver>.kslib
fusion build ./tests/hello-app --bin -o myapp   # single static binary (needs Go toolchain)
fusion build --bin --strip ./tests/hello-app -o myapp-small  # -ldflags "-s -w", smaller
fusion build --bin --target linux/amd64 ./tests/hello-app    # GOOS/GOARCH passthrough
```

### 2c. Imports — flat globals, no namespaces

```python
import "shared/util.ks"        # app-root relative path
import "frontend/store/app.ks" # same rule
import "hello-lib"             # lib by name: newest test-releases/hello-lib-*.kslib wins
```

Rules:

- No `export` keyword. Every top-level `func` / `let` becomes global.
- No `import "x" as h`. Alias syntax parses but backend **ignores** it. Still flat.
- Consequence: **prefix all lib functions** (`mylib_greet`, not `greet`) to avoid collisions.
- `fusion build` fails if a declared `[dependencies]` entry has no built `.kslib`.
- A `.kslib` bundle is JSON (`kslib-1`) with parse-checked sources. Bundles and `.ks` scripts start with `#!/usr/bin/env fusion`, so on Linux: `chmod +x prog.ks && ./prog.ks`.

## 3. CLI reference — everything an agent needs

| Task | Command |
|---|---|
| Scaffold | `fusion new <dir>`, `fusion new --lib <dir>` |
| Run app (backend+frontend together) | `fusion run [appdir] [--race] [--debug] [--cpuprofile FILE]` |
| Run one side | `fusion launch . --backend`, `fusion launch . --frontend`, `fusion launch ./app/custom.toml` |
| Run single file | `fusion prog.ks`, `fusion lib.kslib`, `fusion prog.ksb` |
| Build / check deps | `fusion build [dir] [--release] [--out DIR]` |
| Static binary | `fusion build [dir] --bin [-o FILE] [--target OS/ARCH] [--strip]` |
| Bytecode subset | `fusion compile <file.ks> [--out file.ksb] [--dis] [--run]` |
| Tests | `fusion test [target]` — dir (recursive) or single file; `*_test.ks` with `assert`, TAP output |
| Format | `fusion fmt [target]`, `fusion fmt [target] --check` (CI; idempotent) |
| Lint | `fusion vet [target] [--deny-warns]` (unused let, arity, unknown var, env-in-frontend) |
| Strict check | `fusion check [target]` (parse + arity + `: type` + `is` narrowing) |
| Docs | `fusion doc [target] [--out FILE]` (from `#` comments + func sigs) |
| REPL | `fusion repl` (multiline via braces) |
| Bench | `fusion bench [target] [--n N] [--cpuprofile FILE]` |
| Deps | `fusion vendor [appdir]`, `fusion publish [libdir] [--registry DIR]`, `fusion pull <name[@spec]> [--out DIR]`, `fusion yank <name[@ver]> [--remove]`, `fusion registry`, `fusion audit [appdir]` |
| Web | `fusion run-web [appdir] [--port N] [--watch]`, `fusion build-js [appdir] [--out DIR] [--strict\|--no-strict]`, `fusion build-ssg [appdir] [--out DIR]` |
| Debug/profile | `fusion debug <file.ks> [--break LINE] [--trace]`, `fusion profile <file.ks> [--top N]`, `fusion lsp` |
| Misc | `fusion version`, `fusion help` |

Always run after editing: `fusion fmt <target>`, `fusion vet <target>`, `fusion check <target>`, `fusion test <target>`. For apps also `fusion build <target>`.

## 4. Language `.ks` — minimal complete reference

File extension `.ks`. Braces `{}` required on all blocks. Comments: `# ...`, `// ...`, `/* multi */`.

```python
# literals: "double" and 'single' strings; 0xFF 0b101 0o17 1_000 1e3 .5 numbers
let x = 10       # defines in current block/function
x = x + 1        # assigns up the scope chain (never re-let same var in same block)
x += 5           # also -= *= /= %= (desugar)
print "hi " + x          # + on strings concatenates; print takes multiple args:
print "a", "b", 123
sleep 500                # ms; statement AND callable: sleep(500)

# types: nil bool int float string array map func chan (+ number/any/ok/err aliases)
let a = [1, 2.5, "x", true]
let m = {name: "ada", age: 36}
print a[0], m.name, m["age"]
a[0] = 99
m.age = 37
print [1] + [2]          # array concat -> [1, 2]

# gradual types (optional annotations, runtime-checked, nil nullable)
let n: int = 10
let s: string = "hi"
let maybe: int? = nil
func add(a: int, b: int): int { return a + b }
let double = func(x: int): int { return x * 2 }
let u: int|string = 1
let scores: array<int> = [1, 2]
struct User { name: string, age: int }
enum Color { Red, Green, Blue }
let user: User = {name: "ada", age: 36}
assert(n is int)
assert(n is "int")
assert(1 is number and 2.5 is number)
assert(is_type(n, "int"))
assert(assert_type(n, "int") == 10)

# nil-safety: ?. safe access (missing -> nil), ?? default (nil-only, short-circuit)
let user2 = {name: "ada"}
print user2?.name              # ada
print user2?.missing ?? "anon" # anon
print nil?.anything ?? "dflt"  # dflt
print [1, 2]?.[9] ?? "oob"     # oob

# control flow — braces mandatory
if x > 5 { print "big" } else if x == 5 { print "five" } else { print "small" }
while x > 0 {
  x = x - 1
  if x == 2 { continue }
  if x == 0 { break }
}
for i in range(5) { print i }            # 0..4; also range(a,b), range(a,b,step)
for k, v in {a: 1} { print k, v }        # array/map/string; for-c also exists:
for i = 0; i < 3; i = i + 1 { print i }  # AVOID in frontend pages (breaks build-js)
for v in [1, 2] { print v }

func fact(n) { if n <= 1 { return 1 } return n * fact(n - 1) }
print 2 ** 10            # power, right-assoc: -2**2 == -4
print 2 in [1, 2]        # membership: array member / map key / substring
print [1,2,3,4][1:3]     # slicing; also a[:2], a[1:], s[-2:]

try {
  let v = 1 / 0
} catch e {
  print "caught:", e
} finally {
  print "always runs"
}
let r = ok(42)
assert(r is ok and is_ok(r))
assert(unwrap(r) == 42)
assert(unwrap_or(err("boom"), 99) == 99)

switch x {
  case 1 { print "one" }
  case 2, 3 { print "few" }
  default { print "many" }
}

func work() {
  defer print "cleanup"   # LIFO on function return, like Go
}

# concurrency like Go
let c = chan(1)
go func() { send(c, 42) close(c) }()
print recv(c)

select {
  case v = recv(c) { print v }  # receive; send form: case send(out, 1) {...}
  case timeout(100) { print "timed out" }
  # default {...}  # when present: never blocks
}
let jobs = chan(2)
send(jobs, "a")
send(jobs, "b")
close(jobs)
for v in jobs { print v }  # drains until close
```

### 4a. Operator precedence (high → low)

```
() [] [:] . ?.            call, index, slice, field, safe access
**                        power (right-assoc, tighter than unary)
- ! not                   unary
* / %
+ -
in is                     membership; type test
< <= > >=
== !=                     == is deep equality
and &&, or ||
??                        nil-coalescing (nil-only, short-circuit; looser than or)
```

`/ ` always yields float (`7/2 == 3.5`); `%` needs ints.
`and`/`or` return operand values (Python-like); `!`/`not` return bool.
`??` returns left when non-nil else right.
`is` names: `nil bool int float number string array map func chan any ok err`. Also `x is not int`.
Truthiness: `nil false 0 0.0 "" [] {}` are falsy, everything else truthy. **This differs from JS/Python — check explicitly with `== nil` when it matters.**

### 4b. Scoping

`let` defines in current block/function; plain `=` walks up the scope chain. Functions capture defining scope (closures). `for-in` loop vars are per-iteration (Go 1.22 semantics).

## 5. Builtins (177) — grouped cheat sheet

```python
# core
print(...)             # statement, multi-arg ok
len(x)                 # string/array/map/chan
str(x) int(x) float(x) bool(x)
chr(n) ord(s) hex(n)
type(x)                # "nil|bool|int|float|string|array|map|func|chan"
is_type(v, t) assert_type(v, t)
range(n) range(a, b) range(a, b, step)
assert(cond, msg?) error(msg) panic(msg)
ok(v) err(e) is_ok(v) is_err(v) unwrap(v) unwrap_or(v, dflt)

# arrays
push(arr, v) pop(arr) insert(arr, i, v) remove(arr, i) clear(arr) reverse(arr) sort(arr)
slice(x, i, j?)        # or x[i:j], negatives ok

# maps
keys(m) values(m) has(m, k) delete(m, k) merge(m1, m2, ...) get(m, k, default?)

# strings (all return new strings)
split(s, sep) join(arr, sep) upper(s) lower(s) trim(s, cutset?)
contains(h, n) index_of(h, n) starts_with(s, p) ends_with(s, sfx)
replace(s, old, new) substr(s, start, len?) repeat(s, n) trim_prefix(s, p) trim_suffix(s, sfx)

# math/time/random
abs(x) min(...) max(...) floor(x) ceil(x) round(x) sqrt(x) pow(a, b) pi()
now() rand() randint(lo, hi) seed(n)
bit_and(a,b) bit_or(a,b) bit_xor(a,b) bit_shl(a,n) bit_shr(a,n) bit_not(a)

# functional
map(arr, fn) filter(arr, fn) each(arr, fn) reduce(arr, fn, init?) apply(fn, argsArray)

# json / files / os
json_stringify(v) json_parse(s)
read_file(p) write_file(p, s) append_file(p, s) exists(p) list_dir(d?)
mkdir(p) remove(p) remove_file(p) remove_all(p) stat(p) cp(s,d) mv(s,d) copy(s,d)
glob(pat) path_join(...) abs_path(p) cwd()
input(prompt?) argv() env(name, default?) env_all() exit(code?) exec(cmd, args?) shell(cmd)

# concurrency
chan(n?) send(ch, v) recv(ch) close(ch)
try_send(ch, v) try_recv(ch) recv_timeout(ch, ms) send_timeout(ch, v, ms)
chan_len(ch) chan_cap(ch) chan_closed(ch) with_timeout(ms, fn) parallel(arr, fn) sleep(ms)
# select { case v = recv(c) {...} case send(c, v) {...} case timeout(ms) {...} default {...} }

# http/fetch/regex/crypto/encoding
http_get(url, headers?) http_post(url, body, ctype?) fetch_json(url) http_serve(port, handler)
regex_match(s, pat) regex_find(s, pat) regex_replace(s, pat, repl) regex_split(s, pat)
sha256(s) md5(s) hmac_sha256(msg, key) base64_encode(s) base64_decode(s)
hex_encode(s) hex_decode(s) uuid() random_bytes(n)

# process/time/db/log/asserts/types/state/net
format_time(ms, layout?) parse_time(s, layout?) time_parts(ms)
db_put(db,k,v) db_get(db,k,dflt?) db_delete(db,k) db_list(db)
log_info(m) log_warn(m) log_error(m) assert_eq(a,b) assert_ne(a,b) assert_contains(h,n)
struct_validate(m, schema) struct_assert(m, schema) enum_create(arr) enum_valid(e, v) is_number(x)
use_state(k, init) set_state(k, v) on_mount(fn)
tcp_connect(host,port) tcp_send(id,s) tcp_recv(id,n?) tcp_close(id) tcp_serve(port,fn)
tls_connect(host,port) ws_connect(host,port)
```

Notes: `http_serve` handler is `func(path)->string`, always `application/json`, no shutdown. `fetch_json(url)` is GET-only `json_parse(http_get(url))`. `http_serve`, DB (`db_*` is JSON-file KV), SQL helpers are thin — fine for scripts/sidecars, not a Laravel/Django replacement.

## 6. Frontend contract (must follow exactly)

- `frontend/main.ks`: route table + layout only. Reads `env("ROUTE", "/")`, calls `<route>_page(props)`, wraps with `app_layout`, prints via `render_console`.
- Route mapping: `/` → `home_page`, `/hi` → `hi_page`, `/user/7` → `user_page` with `props.id="7"` when `frontend/pages/user_[id].ks` exists; unknown → `404_page`/`notfound_page` else 404 JSON.
- Pages/components/layouts/store contract: `(props: map) -> view-model {key, type, props, children}`.
- View-model example:

```python
# frontend/pages/home.ks
func home_page(props) {
  let title = props?.title ?? app_title
  let user = props?.user ?? {name: "ada", tags: ["ks", "fusion"]}
  let head = header_render({title: title})
  return {key: "home", type: "page", props: {title: title, count: 1 + 2, user: user}, children: [head]}
}
```

- `fusion run-web` serves SSR HTML+JSON plus `/api/*` (each `backend/api/<name>.ks` must define `api_<name>(req)` returning a map; `req = {query, path}`).
- `fusion build-js` transpiles a **subset only** (STRICT default): `let/assign/func/block/if/while/for-in/break/continue/return/print/expr/call/index/slice/array/map/func-lit`. It **fails** on `for-c go sleep try switch select defer struct enum` in frontend files. Keep frontend pages to the subset if `build-js` matters. CSS: no CSS-in-`.ks`; use `props.class` strings, put files in `frontend/styles/*.css`.

## 7. Backend patterns

```python
# http + json worker (typical Next.js/Laravel sidecar use)
let raw = http_get("https://api.example.com/data")
let data = json_parse(raw)
# or: let data = fetch_json("https://api.example.com/data")

# files + json CLI
let cfg = json_parse(read_file("config.json"))
write_file("out.json", json_stringify(cfg))

# fan-out
let out = parallel([1, 2, 3], func(x) { return x * 2 })
print out  # [2, 4, 6]

# timeout
let v = with_timeout(200, func() { sleep 500 return "slow" })
print v ?? "timed out"

# result values instead of exceptions
func load(p) {
  if !exists(p) { return err("missing: " + p) }
  return ok(read_file(p))
}
let r2 = load("a.txt")
if is_err(r2) { print unwrap_or(r2, "default") }
```

## 8. Tests

`*_test.ks` anywhere under target dir, top-level `assert`s, imports at top:

```python
# frontend/pages/home_test.ks
import "frontend/store/app.ks"
import "frontend/components/header.ks"
import "frontend/layouts/app.ks"
import "frontend/pages/home.ks"

let vm = home_page(app_state())
assert(vm.key == "home")
assert(vm.props.title == "Hello from ks-fusion")
```

```bash
fusion test ./myapp
fusion test ./myapp --timeout 30   # per-file timeout variant
```

## 9. Rules for AI agents — do NOT violate

1. **Read `fusion.toml` + entry files first.** Never invent paths; use the entries in the config.
2. **`let` once, `=` after.** `let x = 1` then `x = 2`. Re-`let` in same block is wrong.
3. **Braces always.** `if x { }`, `for ... { }`, `func f() { }` — no bare `:` / indentation blocks.
4. **No `export`, no `as` alias, no modules.** Flat globals; prefix lib funcs (`libname_verb`).
5. **`print` and `sleep` are statements** (`print x`, `sleep 100` both work with and without parens).
6. **Strings: only `" "` and `' '`.** No backticks, no `${}` interpolation — use `"a " + x`.
7. **`/` is float division; `%` ints only.** `7/2 == 3.5`.
8. **Falsy set is wide:** `nil false 0 0.0 "" [] {}`. Use `== nil` / `is_ok` / `len()` checks, not bare `if x` for emptiness unless intended.
9. **Frontend pages: subset only** if `build-js`/`run-web` is used. No `go sleep try switch select defer struct enum for-c exec shell env` (except `env("ROUTE",..)` in `main.ks`). Backend may use everything.
10. **Error messages carry `line N:`.** When a command fails, read the line it names.
11. **Always format+lint+check+test:** `fusion fmt`, `fusion vet`, `fusion check`, `fusion test`, then `fusion build` for apps.
12. **Never hand-edit `fusion.lock` or `vendor/` or `*.kslib`.** Regenerate via `fusion build / vendor / pull`.
13. **Registry is file-local** (`publish/pull/yank` + sha256 sidecar, `FUSION_REGISTRY` dir override). No npm-style central install.
14. **`fusion compile` is partial** (no `go/chan/select/import/defer/finally/struct`). If it rejects a file, that is expected — run with interpreter (`fusion run` / `fusion prog.ks`).

## 10. Recipes

```bash
# new app, run, verify
fusion new myapp
fusion run ./myapp
fusion fmt ./myapp && fusion vet ./myapp && fusion check ./myapp && fusion test ./myapp && fusion build ./myapp

# add a page /hi (3 files touch: page + main route + optional test)
# 1. create frontend/pages/hi.ks with func hi_page(props)
# 2. import it in frontend/main.ks, add: else if route == "/hi" { render_console(hi_page({})) }
# 3. fusion run-web . --port 8080 --watch

# add an API route /api/user
# 1. create backend/api/user.ks with func api_user(req) { ... return {id: ..., name: ...} }
# 2. fusion run-web . --port 8080   # GET /api/user?id=7

# use a lib
fusion new --lib mylib
# ... edit src/lib.ks with prefixed funcs mylib_* ...
fusion build --release ./mylib
# in app fusion.toml: [dependencies] mylib = "0.1.0"
# in .ks: import "mylib"
fusion vendor ./myapp   # offline copy into vendor/
fusion audit ./myapp
```

## 11. Verify checklist (definition of done)

- [ ] `fusion fmt <target> --check` clean
- [ ] `fusion vet <target>` has 0 errors (warnings triaged; lib calls show as `unknown-var` warns until `.kslib` is built — build the lib first, then re-vet)
- [ ] `fusion check <target>` passes
- [ ] `fusion test <target>` all `ok`
- [ ] `fusion build <target>` prints `build ok` (app) or `built ...kslib` (lib)
- [ ] `fusion run <target>` output matches expectation

## 12. Further reading (in this repo)

- `README.md` — full language + builtins + toolchain (source of truth for syntax)
- `docs/vs.md` — honest comparison + file:line evidence for every claim
- `docs/futures.md` — roadmap, non-goals (do not reimplement React/npm; no FFI/kernel work)
- `docs/bench.md`, `docs/stability.md` — perf artifacts, semver policy
- `tests/hello-app/` — canonical app example; `tests/hello-lib/` — canonical lib example
- `editors/vscode/` — LSP + VS Code extension (hover/goto/completion/rename/format)
