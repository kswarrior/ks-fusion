# backend/main.ks - logic part, Go-like concurrency (v1.0)
let app = "hello-app"
print "backend: starting " + app
go print "backend: job 1 done"
go print "backend: job 2 done"
sleep 100

func fib(n) {
  if n < 2 {
    return n
  }
  return fib(n - 1) + fib(n - 2)
}
print "fib(10) =", fib(10)

let total = 0
for i in range(5) {
  total += i
}
print "sum =", total
print "backend: ok"
