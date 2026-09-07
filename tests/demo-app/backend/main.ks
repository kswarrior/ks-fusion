# backend/main.ks - demo-app logic: shared lib + concurrency + JSON API helpers.
import "demo-lib"

let app = "demo-app"
print "backend: starting " + app
print demo_greet(app), "|", demo_title()
print "clamped:", demo_clamp(99, 0, 10), "sum:", demo_sum([1, 2, 3])

let numbers = [4, 8, 15, 16, 23, 42]
let st = demo_stats(numbers)
print "stats:", json_stringify(st)

# fan-out: compute partial sums concurrently, then combine
let parts = [[1, 2, 3], [4, 5], [6, 7, 8, 9]]
let ch = chan(3)
for part in parts {
  go func() {
    send(ch, demo_sum(part))
  }()
}
let grand = 0
for i in range(len(parts)) {
  grand = grand + recv(ch)
}
print "grand total =", grand

# parallel map over the nav labels
let labels = parallel(demo_nav(), func(item) { return item.label })
print "nav:", join(labels, ", ")

print "backend: ok"
