# frontend/store/app.ks - shared state + helpers (no view code).
# P0: single state map threaded as context; fetches return ok()/err().

let app_title = "Hello from ks-fusion"

func app_state() {
  return {
    title: app_title,
    count: 1 + 2,
    user: {name: "ada", tags: ["ks", "fusion"]}
  }
}

func app_fetch_user() {
  let s = app_state()
  return ok(s.user)
}
