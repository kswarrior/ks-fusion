# frontend/pages/dashboard.ks - "/dashboard" route.
# Contract: (props: map) -> view-model. Stats come from the store via
# ok()/unwrap_or; one mini card per metric in children.
import "frontend/store/app.ks"

func dashboard_page(props) {
  let st = unwrap_or(app_fetch_stats(), {count: 0, total: 0, avg: 0, min: nil, max: nil})
  let cards = [
    {key: "stat-count", type: "stat", props: {label: "count", value: st.count}, children: []},
    {key: "stat-total", type: "stat", props: {label: "total", value: st.total}, children: []},
    {key: "stat-avg", type: "stat", props: {label: "avg", value: st.avg}, children: []},
    {key: "stat-min", type: "stat", props: {label: "min", value: st.min}, children: []},
    {key: "stat-max", type: "stat", props: {label: "max", value: st.max}, children: []}
  ]
  return {
    key: "dashboard",
    type: "page",
    props: {
      title: "Dashboard",
      path: "/dashboard",
      stats: st,
      rows: ["count = " + st.count, "total = " + st.total, "avg = " + st.avg, "min = " + st.min, "max = " + st.max]
    },
    children: cards
  }
}
