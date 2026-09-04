# backend/main.ks - logic part, Go-like concurrency (v1.0)
import "hello-lib"

let app = "hello-app"
print "backend: starting " + app
print greet(app), "|", shout("lib works")
print "clamped:", clamp(99, 0, 10), "sum:", sum([1, 2, 3])
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
