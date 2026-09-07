# frontend/layouts/app.ks - shared wrapper: header + sidebar + page + footer.
# Contract: (page: map) -> view-model.
import "frontend/store/app.ks"
import "frontend/components/header.ks"
import "frontend/components/sidebar.ks"
import "frontend/components/footer.ks"

func app_layout(page) {
  let active = page?.props?.path ?? "/"
  let head = header_render({title: app_title, nav: app_nav(), active: active})
  let side = sidebar_render({links: app_nav(), active: active})
  let foot = footer_render({})
  return {key: "app", type: "layout", props: {}, children: [head, side, page, foot]}
}
