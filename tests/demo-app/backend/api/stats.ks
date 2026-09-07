# backend/api/stats.ks - "/api/stats" route.
# Contract: func api_stats(req) -> map {count, total, avg, min, max}.
# req = {query: {...}, path: "/api/stats"} (see webjs.go runAPIRouteWithQuery).
# Optional query: ?nums=1,2,3 (defaults to a fixed sample).

func api_stats(req) {
  let q = req?.query ?? {}
  let raw = q?.nums ?? "4,8,15,16,23,42"
  let parts = split(raw, ",")
  let nums = map(parts, func(s) { return int(trim(s)) })
  let total = 0
  for v in nums {
    total = total + v
  }
  let n = len(nums)
  if n == 0 {
    return {count: 0, total: 0, avg: 0, min: nil, max: nil}
  }
  return {count: n, total: total, avg: total / n, min: min(nums), max: max(nums)}
}
