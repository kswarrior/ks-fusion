# frontend/pages/home_test.ks - page contract tests (run with `fusion test`).
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

let home = home_page(app_state())
assert(home.key == "home")
assert(home.type == "page")
assert(home.props.title == "Demo Full-Stack")
assert(len(home.props.rows) == 2)

let dash = dashboard_page(app_state())
assert(dash.key == "dashboard")
assert(dash.props.stats.total == 108)
assert(dash.props.stats.count == 6)
assert(len(dash.children) == 5)
assert(dash.children[0].key == "stat-count")

let about = about_page(app_state())
assert(about.key == "about")
assert(about.props.path == "/about")

let docs = docs_page(app_state())
assert(docs.key == "docs")
assert(len(docs.props.sections) == 3)
assert(len(docs.children) == 3)
assert(docs.children[0].type == "section")

let u = user_page({id: "7", path: "/user/7"})
assert(u.key == "user")
assert(u.props.id == "7")
assert(u.props.name == "user-7")

let nf = notfound_page({path: "/nope"})
assert(nf.key == "notfound")

let app = app_layout(home)
assert(app.key == "app")
assert(app.children[0].key == "header")
assert(app.children[1].key == "sidebar")
assert(app.children[2].key == "home")
assert(app.children[1].props.active == "/")
