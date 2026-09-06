# frontend/layouts/app.ks - shared wrapper (header/footer once).
# Contract: (page: map) -> view-model.

func app_layout(page) {
  return {key: "app", type: "layout", props: {}, children: [page]}
}
