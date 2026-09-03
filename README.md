# ks-fusion

Simple programming language (v0.1) made in Go.
Easy like Python, concurrency like Go.

## App structure (app made in ks-fusion)

```
myapp/
  fusion.toml
  backend/main.ks
  frontend/main.ks
```

`fusion.toml`:
```toml
[package]
name = "myapp"
version = "0.1.0"
entry_backend = "backend/main.ks"
entry_frontend = "frontend/main.ks"
```

## Language v0.1 (.ks)

```python
# comment
let x = 10
x = x + 1
print "hi " + x
sleep 500     # ms
go print "runs together"
```

## Toolchain (this repo, in Go)

```
cmd/fusion/          fusion CLI
internal/frontend/   lexer + parser
internal/backend/    interpreter (goroutines for `go`)
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
