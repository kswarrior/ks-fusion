# frontend/pages/home.ks - "/" route.
# Contract: (props: map) -> view-model. Every page fills props.rows
# (array of printable lines) so render_console stays generic.
import "frontend/store/app.ks"

func home_page(props) {
  let title = props?.title ?? app_title
  let ctas = [
  {key: "cta-dash", type: "a", props: {href: "/dashboard", class: "btn btn-primary", text: "Open dashboard"}, children: []},
  {key: "cta-docs", type: "a", props: {href: "/docs", class: "btn", text: "Read docs"}, children: []}
  ]
  return {
    key: "home",
    type: "page",
    props: {
      title: title,
      path: "/",
      text: "A full-stack ks-fusion showcase: typed backend workers, live API routes, and server-rendered pages.",
      rows: ["Stats, tables and docs below — every link navigates for real"]
    },
    children: ctas
  }
}
