# frontend/pages/dashboard.ks - "/dashboard" route.
# Contract: (props: map) -> view-model. Stats come from the store via
# ok()/unwrap_or; one mini card per metric plus a real data table.
import "frontend/store/app.ks"

func stat_card(key, label, value) {
  return {key: key, type: "stat", props: {label: label, value: value}, children: []}
}

func num_row(n) {
  return {
    key: "num-" + n,
    type: "tr",
    props: {},
    children: [
    {key: "num-" + n + "-v", type: "td", props: {text: str(n)}, children: []},
    {key: "num-" + n + "-sq", type: "td", props: {text: str(n * n)}, children: []}
    ]
  }
}

func dashboard_page(props) {
  let st = unwrap_or(app_fetch_stats(), {count: 0, total: 0, avg: 0, min: nil, max: nil})
  let nums = app_state().numbers
  let head = {
    key: "nums-head",
    type: "tr",
    props: {},
    children: [
    {key: "nums-head-a", type: "td", props: {text: "Number", class: "th"}, children: []},
    {key: "nums-head-b", type: "td", props: {text: "Square", class: "th"}, children: []}
    ]
  }
  let rows = map(nums, num_row)
  let table = {key: "nums-table", type: "table", props: {}, children: [head] + rows}
  let cards = [
  stat_card("stat-count", "count", st.count),
  stat_card("stat-total", "total", st.total),
  stat_card("stat-avg", "avg", st.avg),
  stat_card("stat-min", "min", st.min),
  stat_card("stat-max", "max", st.max)
  ]
  return {
    key: "dashboard",
    type: "page",
    props: {
      title: "Dashboard",
      path: "/dashboard",
      text: "Live numbers computed in the store on every render.",
      stats: st,
      rows: ["count = " + st.count, "total = " + st.total, "avg = " + st.avg, "min = " + st.min, "max = " + st.max]
    },
    children: cards + [table]
  }
}
