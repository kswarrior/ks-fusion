# frontend/components/footer.ks - bottom bar.
# Contract: (props: map) -> view-model {key, type, props, children}.

func footer_render(props) {
  let text = props?.text ?? "Demo Full-Stack — built with ks-fusion"
  return {key: "footer", type: "footer", props: {text: text}, children: []}
}
