# frontend/store/app.ks - shared state + helpers (no view code).
# Single state map threaded as context; fetches return ok()/err().

let app_title = "Demo Full-Stack"
let app_version = "1.0.0"

func app_nav() {
  return [
  {path: "/", label: "Home"},
  {path: "/dashboard", label: "Dashboard"},
  {path: "/about", label: "About"},
  {path: "/docs", label: "Docs"}
  ]
}

func app_state() {
  return {
    title: app_title,
    version: app_version,
    user: {name: "ada", tags: ["ks", "fusion"]},
    numbers: [4, 8, 15, 16, 23, 42]
  }
}

func app_fetch_user() {
  let s = app_state()
  return ok(s.user)
}

func app_fetch_stats() {
  let nums = app_state().numbers
  let total = 0
  for v in nums {
    total = total + v
  }
  return ok({count: len(nums), total: total, avg: total / len(nums), min: min(nums), max: max(nums)})
}
