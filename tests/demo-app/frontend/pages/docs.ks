# frontend/pages/docs.ks - "/docs" route.
# Contract: (props: map) -> view-model. Sections render as rows here
# and as structured children for SSR clients.
import "frontend/store/app.ks"

func docs_page(props) {
  let sections = [
  {h: "Routing", p: "File pages/<name>.ks serves /<name> via <name>_page(props)."},
  {h: "State", p: "store/app.ks owns state; pages stay pure view-models."},
  {h: "API", p: "backend/api/<name>.ks serves /api/<name> via api_<name>(req)."}
  ]
  let rows = map(sections, func(s) { return s.h + ": " + s.p })
  let kids = map(sections, func(s) {
    return {key: "doc-" + lower(s.h), type: "section", props: {h: s.h, p: s.p}, children: []}
  })
  return {
    key: "docs",
    type: "page",
    props: {title: "Docs", path: "/docs", sections: sections, rows: rows},
    children: kids
  }
}
