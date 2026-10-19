---hcl
# An HCL front matter: Marp takes the block as front matter (its rule opens
# on any first line starting with three dashes) and, the text not being a
# YAML mapping, applies none of it. None of this may show below.
theme    = "gaia"
paginate = true
class    = "invert"
header   = "HCL header: ${upper("x")}"
footer   = "${page()}/${total()}"
data = {
  company = "Acme"
}
kind "hero" {
  class = "hero"
}
---

# First slide

The front matter above is HCL.

<!-- a note -->

---

## Second slide

- one
- two
