# frontend/components/sidebar.ks - side nav with active-link highlight.
# Contract: (props: map) -> view-model {key, type, props, children}.
# props: {links (array of {path, label}), active (current path)}.

func sidebar_render(props) {
  let links = props?.links ?? []
  let active = props?.active ?? "/"
  let items = map(links, func(l) {
    return {path: l.path, label: l.label, active: l.path == active}
  })
  return {key: "sidebar", type: "sidebar", props: {items: items, active: active}, children: []}
}
