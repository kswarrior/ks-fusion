# frontend/pages/about.ks - "/about" route.
# Contract: (props: map) -> view-model.
import "frontend/store/app.ks"

func about_page(props) {
  let feats = [
  {key: "feat-conc", type: "section", props: {h: "Concurrency", p: "Backend workers use go/chan/select with Go-style spelling."}, children: []},
  {key: "feat-ssr", type: "section", props: {h: "Server rendering", p: "Pages are view-models rendered to HTML with keyed live patches."}, children: []},
  {key: "feat-pkg", type: "section", props: {h: "Packaging", p: "Shared code ships as versioned .kslib bundles with semver lockfiles."}, children: []}
  ]
  return {
    key: "about",
    type: "page",
    props: {
      title: "About",
      path: "/about",
      text: app_title + " v" + app_version + " — easy like Python, concurrency like Go.",
      rows: ["Backend, frontend and shared lib in one repo"]
    },
    children: feats
  }
}
