# hello-lib entry: shared helpers for ks-fusion apps.
# Build me with:  fusion build --release ./tests/hello-lib
# Use me with:    import "hello-lib"

func greet(name) {
  return "hello " + name + " from hello-lib"
}

func clamp(x, lo, hi) {
  if x < lo {
    return lo
  }
  if x > hi {
    return hi
  }
  return x
}

func sum(arr) {
  let total = 0
  for v in arr {
    total = total + v
  }
  return total
}
