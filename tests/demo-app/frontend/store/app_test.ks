# frontend/store/app_test.ks - store contract tests (run with `fusion test`).
import "frontend/store/app.ks"

assert(app_title == "Demo Full-Stack")
assert(len(app_nav()) == 4)
assert(app_nav()[1].path == "/dashboard")

let s = app_state()
assert(s.user.name == "ada")
assert(len(s.numbers) == 6)

let r = app_fetch_user()
assert(is_ok(r))
assert(unwrap(r).name == "ada")

let st = app_fetch_stats()
assert(is_ok(st))
assert(unwrap(st).total == 108)
assert(unwrap(st).min == 4)
assert(unwrap(st).max == 42)
