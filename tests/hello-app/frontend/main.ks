# frontend/main.ks - entry: route table + layout only (P0).
# File name = route name. No business logic here; pages/components own it.
import "frontend/store/app.ks"
import "frontend/components/header.ks"
import "frontend/layouts/app.ks"
import "frontend/pages/home.ks"
import "frontend/pages/hi.ks"

# Console renderer (P0 stand-in for the future --web diff runtime):
# components return view-models, main prints them. Output stays stable.
func render_console(vm) {
  let t = vm?.type ?? "unknown"
  if t == "page" {
    let p = vm.props
    print p.title
    print "count = " + p.count
    print "user:", p.user.name
    for i, tag in p.user.tags {
      print "tag", i, "=", tag
    }
    return nil
  }
  if t == "text" {
    print vm.props?.title ?? "hi"
    return nil
  }
  print json_stringify(vm)
  return nil
}

let route = env("ROUTE", "/")
let r = app_fetch_user()
assert(is_ok(r))

if route == "/" {
  let vm = home_page(app_state())
  let app = app_layout(vm)
  assert(app.key == "app")
  render_console(vm)
} else if route == "/hi" {
  render_console(hi_page({}))
} else {
  error("unknown route: " + route)
}
print "frontend: ok"
