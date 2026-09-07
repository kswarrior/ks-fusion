# frontend/pages/home.ks - "/" route.
# Contract: (props: map) -> view-model. Every page fills props.rows
# (array of printable lines) so render_console stays generic.
import "frontend/store/app.ks"

func home_page(props) {
  let ctas = [
  {key: "cta-dash", type: "a", props: {href: "/dashboard", class: "btn btn-primary", text: "Open dashboard"}, children: []},
  {key: "cta-docs", type: "a", props: {href: "/docs", class: "btn", text: "Read docs"}, children: []}
  ]
  return {
    key: "home",
    type: "page",
    props: {
      title: "One language, full stack",
      path: "/",
      text: "Demo Full-Stack v1.0.0 — typed backend workers, live API routes, and server-rendered pages in one repo.",
      rows: []
    },
    children: ctas
  }
}
