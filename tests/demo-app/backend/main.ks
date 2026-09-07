# backend/main.ks - logic with Go-like concurrency
let app = "hello-app"
print "backend: starting " + app

func fib(n) {
  if n < 2 {
    return n
  }
  return fib(n - 1) + fib(n - 2)
}
print "fib(10) =", fib(10)

let ch = chan(2)
go func() {
  for i in range(3) {
    send(ch, i * 10)
  }
  close(ch)
}()
for v in range(3) {
  print "backend: job", recv(ch)
}
print "backend: ok"
