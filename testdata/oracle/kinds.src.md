---
title: Quarterly review
paginate: true
header: "**ACME**"
footer: "{{ .title }} — {{ page }}/{{ total }}"
kind: content
data:
  topics: [Revenue, Hiring]
kinds:
  title:
    class: lead
    paginate: false
    header: ""
    footer: ""
  quote:
    extends: blank
    class: invert
---

<!-- _kind: title -->

# {{ .title }}

{{ range .topics }}
---

## {{ . }}

* point `{{ .notInterpolated }}`
{{ end }}

---

<!-- kind: quote -->

> A quote

---

Still a quote slide, sticky kind.

---

<!-- kind: section -->

# A section divider

---

<!-- class: override -->

# Section with an explicit class

---

<!-- kind: content -->

Back to content: the section's class is gone.
