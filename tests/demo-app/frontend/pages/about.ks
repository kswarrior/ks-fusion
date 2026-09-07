# frontend/pages/about.ks - "/about" route.
# Contract: (props: map) -> view-model.
import "frontend/store/app.ks"

func about_page(props) {
  return {
    key: "about",
    type: "page",
    props: {
      title: "About",
      path: "/about",
      rows: [app_title + " v" + app_version, "Easy like Python, concurrency like Go", "Built with ks-fusion"]
    },
    children: []
  }
}
