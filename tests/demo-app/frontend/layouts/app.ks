# frontend/layouts/app.ks - shared wrapper: header + sidebar + page.
# Contract: (page: map) -> view-model.
import "frontend/store/app.ks"
import "frontend/components/header.ks"
import "frontend/components/sidebar.ks"

func app_layout(page) {
  let active = page?.props?.path ?? "/"
  let head = header_render({title: app_title, nav: app_nav(), active: active})
  let side = sidebar_render({links: app_nav(), active: active})
  return {key: "app", type: "layout", props: {}, children: [head, side, page]}
}
