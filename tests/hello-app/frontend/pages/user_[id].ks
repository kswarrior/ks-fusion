# frontend/pages/user_[id].ks - "/user/:id" dynamic route (P1 STRICT).
# Convention (P1 approved): file foo_[bar].ks -> func foo_page with param bar.
# File user_[id].ks -> func user_page(props) where props.id = path param.
# Props: {id, path, query, params}. Uses props.id (nil-safe via ?.).

func user_page(props) {
  let id = props?.id ?? props?.params?.id ?? "unknown"
  let path = props?.path ?? "/user/" + id
  return {key: "user", type: "page", props: {id: id, path: path}, children: []}
}
