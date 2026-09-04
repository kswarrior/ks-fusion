# ks-fusion

Complete programming language (v1.0) made in Go.
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

## Language v1.0 (.ks)

```python
# comment (# and //)
let x = 10
x = x + 1
x += 5
print "hi " + x          # + with strings concatenates
print "a", "b", 123      # multi-arg print
sleep 500                # ms (also sleep(500))

# types: nil bool int float string array map func chan
let a = [1, 2.5, "x", true]
let m = {name: "ada", age: 36}
print a[0], m.name, m["age"]
a[0] = 99
m.age = 37
print [1] + [2]          # array concat

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

# concurrency like Go: go + channels
let c = chan(1)
go func() {
  send(c, 42)
  close(c)
}()
print recv(c)

# imports (app-root relative)
import "shared/util.ks"
```

### Operators (precedence high→low)

```
() [] .            call, index, field
- ! not            unary
* / %
+ -
< <= > >=
== !=
and &&, or ||
```

`/ `always yields float (`7/2 == 3.5`), `%` needs ints.
`and`/`or` return operand values (Python-like); `!`/`not` return bool.
Truthiness: `nil false 0 0.0 "" [] {}` are falsy, everything else truthy.

### Builtins

```
print(...)             statement (also print("a", "b"))
len(x)                 string/array/map length
str(x) int(x) float(x) conversions
type(x)                 "nil|bool|int|float|string|array|map|func|chan"
range(n) range(a,b) range(a,b,step)
push(arr, v) pop(arr)  mutate array
keys(m) values(m) has(m, k)
chan(n?) send(ch, v) recv(ch) close(ch)
sleep(ms)              also a statement
assert(cond, msg?) error(msg)
```

### Scoping

`let` defines in the current block/function; assignment finds the
variable up the scope chain. Functions capture their defining scope
(closures). `for-in` loop variables are per-iteration (Go 1.22 semantics).

## Toolchain (this repo, in Go)

```
cmd/fusion/          fusion CLI
internal/frontend/   lexer + parser
internal/backend/    tree-walk interpreter (goroutines for `go`)
tests/hello-app/     test app (backend/ frontend/ fusion.toml)
```

## Use

```bash
go build -o fusion ./cmd/fusion
./fusion new myapp
./fusion build ./tests/hello-app
./fusion run ./tests/hello-app
go test ./...
```
