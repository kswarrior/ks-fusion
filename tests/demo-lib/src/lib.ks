# demo-lib entry: shared helpers for the demo-app full-stack showcase.
# Build me with:  fusion build --release ./tests/demo-lib
# Use me with:    import "demo-lib"  (after adding demo-lib = "0.1.0" to [dependencies])
# NOTE: lib imports share one flat global namespace — every func here is
# prefixed with demo_ to avoid collisions.

func demo_title() {
  return "Demo Full-Stack"
}

func demo_greet(name) {
  return "hello " + name + " from demo-lib"
}

func demo_clamp(x, lo, hi) {
  if x < lo {
    return lo
  }
  if x > hi {
    return hi
  }
  return x
}

func demo_sum(arr) {
  let total = 0
  for v in arr {
    total = total + v
  }
  return total
}

func demo_stats(arr) {
  let n = len(arr)
  if n == 0 {
    return {count: 0, total: 0, avg: 0, min: nil, max: nil}
  }
  let total = demo_sum(arr)
  return {count: n, total: total, avg: total / n, min: min(arr), max: max(arr)}
}

func demo_nav() {
  return [
    {path: "/", label: "Home"},
    {path: "/dashboard", label: "Dashboard"},
    {path: "/about", label: "About"},
    {path: "/docs", label: "Docs"}
  ]
}
