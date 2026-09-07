# frontend/main.ks - entry: route table + layout only.
# File name = route name. No business logic here; pages/components own it.
import "frontend/store/app.ks"
import "frontend/components/header.ks"
import "frontend/components/sidebar.ks"
import "frontend/layouts/app.ks"
import "frontend/pages/home.ks"
import "frontend/pages/dashboard.ks"
import "frontend/pages/about.ks"
import "frontend/pages/docs.ks"
import "frontend/pages/user_[id].ks"
import "frontend/pages/404.ks"

# Console renderer (stand-in for the run-web diff runtime):
# pages return view-models, main prints them. Output stays stable.
func render_console(vm) {
  let t = vm?.type ?? "unknown"
  if t == "page" {
    let p = vm.props
    print p?.title ?? "untitled"
    for r in (p?.rows ?? []) {
      print r
    }
    for c in (vm?.children ?? []) {
      print "child:", c?.key ?? "?"
    }
    return nil
  }
  print json_stringify(vm)
  return nil
}

let route = env("ROUTE", "/")
let r = app_fetch_user()
assert(is_ok(r))

let vm = notfound_page({path: route})
if route == "/" {
  vm = home_page(app_state())
} else if route == "/dashboard" {
  vm = dashboard_page(app_state())
} else if route == "/about" {
  vm = about_page(app_state())
} else if route == "/docs" {
  vm = docs_page(app_state())
} else if starts_with(route, "/user/") {
  let id = replace(route, "/user/", "")
  vm = user_page({id: id, path: route})
}
let app = app_layout(vm)
assert(app.key == "app")
assert(app.children[2].key == vm.key)
render_console(vm)
print "frontend: ok"
