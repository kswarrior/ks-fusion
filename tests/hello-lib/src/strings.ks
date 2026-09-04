# hello-lib extra module: string helpers (bundled second, sorted by path).

func shout(s) {
  return s + "!"
}

func repeat(s, n) {
  let out = ""
  for i in range(n) {
    out = out + s
  }
  return out
}
