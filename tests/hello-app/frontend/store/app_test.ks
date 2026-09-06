# frontend/store/app_test.ks - store contract tests (run with `fusion test`).
import "frontend/store/app.ks"

let s = app_state()
assert(s.title == "Hello from ks-fusion")
assert(s.count == 3)
assert(s.user.name == "ada")

let r = app_fetch_user()
assert(is_ok(r))
assert(unwrap(r).name == "ada")
