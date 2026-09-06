# frontend/pages/hi.ks - "/hi" route.
# Contract: (props: map) -> view-model.

func hi_page(props) {
  return {key: "hi", type: "text", props: {title: "hi"}, children: []}
}
