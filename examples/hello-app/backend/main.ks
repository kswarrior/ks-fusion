# backend/main.ks - logic part, Go-like concurrency
let app = "hello-app"
print "backend: starting " + app
go print "backend: job 1 done"
go print "backend: job 2 done"
sleep 100
print "backend: ok"
