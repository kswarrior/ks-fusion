# frontend/components/header.ks - top bar with title + nav links.
# Contract: (props: map) -> view-model {key, type, props, children}.
# props: {title, nav (array of {path, label}), active (current path)}.

func header_render(props) {
  let title = props?.title ?? "untitled"
  let nav = props?.nav ?? []
  let active = props?.active ?? "/"
  return {key: "header", type: "header", props: {title: title, links: nav, active: active}, children: []}
}
