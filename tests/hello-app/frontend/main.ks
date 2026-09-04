# frontend/main.ks - UI part (v1.0)
let title = "Hello from ks-fusion"
print title
let count = 1 + 2
print "count = " + count

let user = {name: "ada", tags: ["ks", "fusion"]}
print "user:", user.name
for i, t in user.tags {
  print "tag", i, "=", t
}
print "frontend: ok"
