# frontend/pages/home_test.ks - page contract tests (run with `fusion test`).
import "frontend/store/app.ks"
import "frontend/components/header.ks"
import "frontend/layouts/app.ks"
import "frontend/pages/home.ks"
import "frontend/pages/hi.ks"

let vm = home_page(app_state())
assert(vm.key == "home")
assert(vm.type == "page")
assert(vm.props.title == "Hello from ks-fusion")
assert(vm.children[0].key == "header")

let app = app_layout(vm)
assert(app.key == "app")
assert(app.children[0].key == "home")

let hi = hi_page({})
assert(hi.key == "hi")
assert(hi.props.title == "hi")
