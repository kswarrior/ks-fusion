# frontend/pages/404.ks - fallback for unknown routes (P1).
# Spec tries `404_page`; `func 404_page` is a .ks parse error today
# (identifiers may not start with a digit), so this file defines the
# working alias `notfound_page`. Go tries 404_page then notfound_page
# (see lookup404Func in internal/tools/webjs.go). Props: {path, query, params}.

func notfound_page(props) {
  let path = props?.path ?? "unknown"
  return {key: "notfound", type: "page", props: {path: path}, children: []}
}
