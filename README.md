# ks-fusion

Complete programming language (v2.2, 69/100 in docs/vs.md) made in Go.
Easy like Python, concurrency like Go.

> The toolchain is written in Go (like CPython is written in C).
> Go is the implementation language — `.ks` is the real language.

## App structure (app made in ks-fusion)

```
myapp/
  fusion.toml
  backend/main.ks
  frontend/main.ks
  shared/util.ks   # optional, via import
```

`fusion.toml`:
```toml
[package]
name = "myapp"
version = "1.0.0"
entry_backend = "backend/main.ks"
entry_frontend = "frontend/main.ks"
```

## Language v2.2 (.ks, 149 builtins)

```python
# comments: # ...  // ...  /* multi-line */
# strings: "double" and 'single'; numbers: 0xFF 0b101 0o17 1_000 1e3 .5
let x = 10
x = x + 1
x += 5
print "hi " + x          # + with strings concatenates
print "a", "b", 123      # multi-arg print
sleep 500                # ms (also sleep(500))

# types: nil bool int float string array map func chan
#        (+ number/any/ok/err aliases for annotations and `is`)
let a = [1, 2.5, "x", true]
let m = {name: "ada", age: 36}
print a[0], m.name, m["age"]
a[0] = 99
m.age = 37
print [1] + [2]          # array concat

# gradual types (v2.1): optional annotations, runtime-checked, nil nullable
let n: int = 10
let s: string = "hi"
let maybe: int? = nil    # `?` accepted, nullable is the default
func add(a: int, b: int): int { return a + b }
let double = func(x: int): int { return x * 2 }
assert(n is int)
assert(n is "int")
assert(s is not int)
assert(1 is number and 2.5 is number)
assert(is_type(n, "int"))
assert(assert_type(n, "int") == 10)

# nil-safety: `?.` safe access (missing -> nil), `??` default (nil-only, short-circuit)
let user = {name: "ada"}
print user?.name         # ada
print user?.missing      # nil (no error)
print user?.missing ?? "anon"   # anon
print nil?.anything ?? "dflt"   # dflt
print [1, 2]?.[9] ?? "oob"      # oob (out-of-range -> nil with `?.`)

# control flow
if x > 5 {
  print "big"
} else if x == 5 {
  print "five"
} else {
  print "small"
}

while x > 0 {
  x = x - 1
  if x == 2 { continue }
  if x == 0 { break }
}

for i in range(5) { print i }       # array/map/string iteration
for k, v in {a: 1} { print k, v }
for i = 0; i < 3; i = i + 1 { print i }

# functions + closures
func add(a, b) { return a + b }
func fact(n) {
  if n <= 1 { return 1 }
  return n * fact(n - 1)
}
let double = func(x) { return x * 2 }
print 2 ** 10            # power (right-assoc): 1024
print 2 in [1, 2]        # membership: array/map-key/substring
print [1,2,3,4][1:3]     # slicing (also a[:2], a[1:], s[-2:])

# errors: error(msg) aborts, try/catch/finally recovers
# v2.1 also has Result values: ok(v)/err(e) + is_ok/is_err/unwrap/unwrap_or
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

# switch (first match wins, no fallthrough, break ends it)
switch x {
  case 1 { print "one" }
  case 2, 3 { print "few" }
  default { print "many" }
}

# defer runs when the enclosing function returns (LIFO, like Go)
func work() {
  defer print "cleanup"
  print "doing work"
}

# concurrency like Go: go + channels + select
let c = chan(1)
go func() {
  send(c, 42)
  close(c)
}()
print recv(c)  # 42

let out = chan(1)
select {
  case send(out, 1) { print "sent" }  # receive: case v = recv(c) {...}
  case timeout(100) { print "timed out" }
  # default {...}  # uncomment: never blocks when present
}
print recv(out)  # 1

let jobs = chan(2)
send(jobs, "a")
send(jobs, "b")
close(jobs)
for v in jobs { print v }  # a, then b (drains until close)

# imports (app-root relative)
import "shared/util.ks"
```

### Operators (precedence high→low)

```
() [] [:] . ?.         call, index, slice, field, safe access
**                   power (right-assoc, tighter than unary: -2**2 == -4)
- ! not              unary
* / %
+ -
in is                membership (array member, map key, substring); type test
< <= > >=
== !=
and &&, or ||
??                   nil-coalescing (nil-only, short-circuit; looser than or)
```

`/ `always yields float (`7/2 == 3.5`), `%` needs ints.
`and`/`or` return operand values (Python-like); `!`/`not` return bool.
`??` returns left when non-nil else right (short-circuit, nil-only unlike `or`).
`is` tests runtime types: `x is int`, `x is "int"`, `x is not "int"`
(names: `nil bool int float number string array map func chan any ok err`).
Truthiness: `nil false 0 0.0 "" [] {}` are falsy, everything else truthy.

### Builtins

```
# core
print(...)             statement (also print("a", "b"))
len(x)                 string/array/map/chan length
str(x) int(x) float(x) bool(x)   conversions (bool = truthiness)
chr(n) ord(s) hex(n)   char codes, 255 -> "0xff"
type(x)                 "nil|bool|int|float|string|array|map|func|chan"
is_type(v, t)          bool: v is T (T in nil|bool|int|float|number|string|array|map|func|chan|any|ok|err)
assert_type(v, t)      v when v is T else error
range(n) range(a,b) range(a,b,step)
assert(cond, msg?) error(msg) panic(msg)
ok(v) err(e)           Result values {ok: v} / {err: "msg"}
is_ok(v) is_err(v)     Result tests (also `v is ok` / `v is err`)
unwrap(v)              ok value or error(err); unwrap_or(v, dflt) for default

# arrays
push(arr, v) pop(arr)  mutate array (push returns new len)
insert(arr, i, v) remove(arr, i)   index ops (remove returns value)
clear(arr/map)         empty in place
reverse(arr) sort(arr) in place, return arr
slice(x, i, j?)        array/string slice (negatives ok); or x[i:j]

# maps
keys(m) values(m) has(m, k)
delete(m, k)           returns true if the key existed
merge(m1, m2, ...)     new map combining all (later wins)
get(m, k, default?)    value or default/nil

# strings (all return new strings)
split(s, sep) join(arr, sep)
upper(s) lower(s) trim(s, cutset?)
contains(h, n)         substring / array member / map key
index_of(h, n)         rune index (string) or position (array), -1 if absent
starts_with(s, p) ends_with(s, sfx)
replace(s, old, new) substr(s, start, len?) repeat(s, n)

# math + time + random
abs(x) min(...) max(...)      (also min/max of one array)
floor(x) ceil(x) round(x) sqrt(x) pow(a, b) pi()
now()                  ms since epoch
rand()                 float in [0, 1); randint(lo, hi); seed(n)

# bitwise (ints)
bit_and(a,b) bit_or(a,b) bit_xor(a,b) bit_shl(a,n) bit_shr(a,n) bit_not(a)

# functional
map(arr, fn) filter(arr, fn) each(arr, fn)
reduce(arr, fn, init?) apply(fn, argsArray)

# json
json_stringify(v) json_parse(s)

# files + OS
read_file(p) write_file(p, s) append_file(p, s)
exists(p) list_dir(dir?) mkdir(p) remove(p) remove_file(p)
input(prompt?)         read a line from stdin
argv()                 process args; env(name, default?)
exit(code?)

# concurrency (goroutines underneath)
chan(n?) send(ch, v) recv(ch) close(ch)
select { case v = recv(c) {...} case send(c, v) {...} case timeout(ms) {...} default {...} }
for v in ch { ... }    drain until close (ch = nil disables a select case)
try_send(ch, v)        non-blocking send, returns bool
try_recv(ch)           non-blocking recv, nil when empty
recv_timeout(ch, ms)   value, or nil on timeout (also nil on drained close)
send_timeout(ch, v, ms)  true if sent, false on timeout
chan_len(ch) chan_cap(ch) chan_closed(ch)
with_timeout(ms, fn)   run fn with timeout; parallel(arr, fn) map in parallel
sleep(ms)              also a statement

# http + fetch (v2.2)
http_get(url, headers?) http_post(url, body, ctype?) fetch_json(url) http_serve(port, handler)

# regex (v2.2)
regex_match(s, pat) regex_find(s, pat) regex_replace(s, pat, repl) regex_split(s, pat)

# crypto/encoding (v2.2)
sha256(s) md5(s) hmac_sha256(msg, key) base64_encode(s) base64_decode(s)
hex_encode(s) hex_decode(s) uuid() random_bytes(n)

# fs full (v2.2)
stat(p) cp(src,dst) mv(src,dst) copy(src,dst) glob(pat) path_join(...) abs_path(p) remove_all(p)

# process/time (v2.2)
exec(cmd, args?) shell(cmd) cwd() env_all() format_time(ms, layout?) parse_time(s, layout?) time_parts(ms)

# kv db / log / asserts (v2.2)
db_put(db,k,v) db_get(db,k,dflt?) db_delete(db,k) db_list(db)
log_info(m) log_warn(m) log_error(m) assert_eq(a,b) assert_ne(a,b) assert_contains(h,n)

# types (v2.2)
struct_validate(m, schema) struct_assert(m, schema) enum_create(arr) enum_valid(e, v) is_number(x)
trim_prefix(s,p) trim_suffix(s,sfx) repeat_str(s,n)
```

### Scoping

`let` defines in the current block/function; assignment finds the
variable up the scope chain. Functions capture their defining scope
(closures). `for-in` loop variables are per-iteration (Go 1.22 semantics).

## Libraries (like Rust)

```bash
fusion new --lib mylib              # scaffold: fusion.toml (type="lib") + src/lib.ks
fusion build --release ./tests/hello-lib   # -> test-releases/hello-lib-0.1.0.kslib
fusion build ./tests/hello-lib      # debug instead -> target/hello-lib-0.1.0.kslib
```

Output dirs are relative to where you run `fusion` (`--out DIR`
overrides). `test-releases/` here holds this repo's built libs,
like Rust's `target/release/`.

`fusion.toml` for a lib:

```toml
[package]
name = "hello-lib"
version = "0.1.0"
type = "lib"

[lib]
name = "hello-lib"
path = "src/lib.ks"
```

Apps declare and use libs:

```toml
[dependencies]
hello-lib = "0.1.0"
```

```python
import "hello-lib"          # newest test-releases/hello-lib-*.kslib wins
print greet("world")
```

`fusion build` fails if a declared dependency has no built bundle.
Notes: lib imports share one flat global namespace (prefix your
functions), and a `.kslib` bundle is JSON (`kslib-1`) with the lib's
parse-checked sources — see `test-releases/` here for a real one.

Bundles (and `.ks` scripts, where `#` is already a comment) start with
`#!/usr/bin/env fusion`, so on Linux they run directly:

```bash
export PATH="$PWD/release:$PATH"   # fusion must be on PATH
chmod +x test-releases/hello-lib-0.1.0.kslib
./test-releases/hello-lib-0.1.0.kslib   # loads the lib, exits 0
chmod +x prog.ks && ./prog.ks           # same for scripts
```

## Toolchain (this repo, in Go)

```
cmd/fusion/          fusion CLI
internal/frontend/   lexer + parser
internal/backend/    tree-walk interpreter (goroutines for `go`)
internal/config/     fusion.toml (apps, libs, dependencies)
internal/lib/        .kslib bundles: build/load/find
tests/hello-app/     test app (backend/ frontend/ fusion.toml)
tests/hello-lib/     test library (src/*.ks)
test-releases/       built lib bundles (like Rust's target/release)
```

## Use

```bash
go build -o fusion ./cmd/fusion
./fusion new myapp
./fusion new --lib mylib
./fusion build ./tests/hello-app            # + fusion.lock (semver) 
./fusion build ./tests/hello-app --bin -o myapp  # single static binary
./fusion build --release ./tests/hello-lib
./fusion run ./tests/hello-app --race
./fusion test ./tests/hello-app   # *_test.ks with assert, TAP output
./fusion fmt ./tests/hello-app --check
./fusion vet ./tests/hello-app
./fusion doc ./tests/hello-app
./fusion check ./tests/hello-app
./fusion bench ./tests/hello-app --n 20
./fusion repl
./fusion vendor ./tests/hello-app
./fusion run-web ./tests/hello-app --port 8080  # SSR HTML+JSON
./fusion build-js ./tests/hello-app --out /tmp/js
./fusion launch . --backend   # only backend; --frontend for frontend only
./fusion launch ./tests/hello-app/custom.toml  # custom config must live in app root
go test ./...
```
