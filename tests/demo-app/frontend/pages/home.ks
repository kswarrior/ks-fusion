# frontend/pages/home.ks - "/" route.
# Contract: (props: map) -> view-model. Every page fills props.rows
# (array of printable lines) so render_console stays generic.
import "frontend/store/app.ks"

func home_page(props) {
  let title = props?.title ?? app_title
  return {
    key: "home",
    type: "page",
    props: {
      title: title,
      path: "/",
      rows: ["Welcome to " + title, "Try /dashboard, /about, /docs, /user/7"]
    },
    children: []
  }
}
