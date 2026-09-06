# frontend/pages/home.ks - "/" route.
# Contract: (props: map) -> view-model. Uses store + header component.

func home_page(props) {
  let title = props?.title ?? app_title
  let user = props?.user ?? {name: "ada", tags: ["ks", "fusion"]}
  let head = header_render({title: title})
  return {
    key: "home",
    type: "page",
    props: {title: title, count: 1 + 2, user: user},
    children: [head],
    head: {title: title}
  }
}
