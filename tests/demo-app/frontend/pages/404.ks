# frontend/pages/404.ks - fallback for unknown routes.
# Spec tries `404_page`; `func 404_page` is a .ks parse error today
# (identifiers may not start with a digit), so this file defines the
# working alias `notfound_page`. Props: {path, query, params}.

func notfound_page(props) {
  let path = props?.path ?? "unknown"
  return {
    key: "notfound",
    type: "page",
    props: {title: "Not found", path: path, rows: ["no page for " + path]},
    children: []
  }
}
