# frontend/components/header.ks - one func per component.
# Contract: (props: map) -> view-model {key, type, props, children}.

func header_render(props) {
  let title = props?.title ?? "untitled"
  return {key: "header", type: "header", props: {title: title}, children: []}
}
