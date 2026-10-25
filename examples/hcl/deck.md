---hcl
theme    = "uncover"
paginate = true
title    = "HCL front matter"
description = "The front matter in HCL2: expressions, functions and typed data, with the same directives as YAML."

data = {
  company = "ACME"
  quarter = 3
  growth  = [4.2, 5.1, 6.8]
}

header = format("%s — Q%d review", data.company, data.quarter)
footer = "${upper(data.company)} · ${page()}/${total()}"

kind "chapter" {
  extends = "section"
  class   = ["invert"]
}
---

# {{ .company }} Q{{ .quarter }}

Front matter written in HCL, not YAML.

---

## Expressions and functions

The header and footer above are not text, they are **expressions**:

```hcl
header = format("%s — Q%d review", data.company, data.quarter)
footer = "${upper(data.company)} · ${page()}/${total()}"
```

`page()` and `total()` are go-slides' own; the rest is HCL's standard library.

---

<!-- _kind: chapter -->

# Typed data

Numbers stay numbers, lists stay lists.

---

## Quarterly growth

{{ range $i, $g := .growth }}- Month {{ add $i 1 }}: **{{ $g }} %**
{{ end }}

`slides expand` writes this front matter back as YAML, so Marp can read the
deck too.
