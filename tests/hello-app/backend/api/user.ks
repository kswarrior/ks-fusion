# backend/api/user.ks - "/api/user" route (P2 hydrate-full fixture).
# Contract: func api_user(req) -> map {id, name from req.query}.
# req = {query: {id: ...}, path: "/api/user"} (see webjs.go runAPIRouteWithQuery).

func api_user(req) {
  let q = req?.query ?? {}
  let id = q?.id ?? "unknown"
  return {id: id, name: "user-" + id}
}
