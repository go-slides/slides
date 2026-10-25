---
slides: true
theme: gaia
paginate: true
title: Decks are code
description: A complete short talk that combines kinds, templates, math, code with callouts, images, emoji and speaker notes.
footer: "{{ .speaker }} · {{ .event }} · {{ page }}/{{ total }}"
transition: fade 0.4s
lineNumbers: true
data:
  speaker: Ada Example
  event: Slides Meetup
  principles:
    - {title: Plain text, why: "A deck is Markdown: it diffs, merges and reviews like code."}
    - {title: "One source, many outputs", why: "HTML, PDF, PPTX, ODP and images come from the same file."}
    - {title: Reproducible, why: "The same input gives the same bytes, on your laptop and in CI."}
kinds:
  chapter:
    extends: section
    class: invert
    footer: ""
  quote:
    extends: blank
    class: lead
---

<!-- _kind: title -->

![bg right:38%](media/hills.svg)

# Decks are code

{{ .speaker }}
{{ .event }}

<!--
Introduce yourself in one sentence. The point of the talk: the way we treat
code is a good way to treat slides. :tada:
-->

---

<!-- _kind: chapter -->

# Three principles

---

{{ range .principles }}
## {{ .title }}

{{ .why }}

---
{{ end }}

<!-- _kind: chapter -->

# Show, don't tell

---

## A tiny build

```sh
slides build talk.md   # <1>
slides talk.md --pdf   # <2>
slides talk.md --pptx  # <3>
```

1. The HTML page, with a presenter view on **p**.
2. A PDF with vectors and selectable text.
3. A PowerPoint file you can still edit.

<!-- Run these live if the network allows; the output files are tiny. -->

---

## Measure it

The speed-up of a parallel build on $n$ cores, with a serial fraction $s$ of
the work, is bounded by

$$
S(n) = \frac{1}{s + \frac{1 - s}{n}}
$$

so even $s = 5\%$ caps it at $20\times$.

<!-- Amdahl's law. Do not derive it; point at the cap. -->

---

<!-- _kind: quote -->

> If you can build it from a clean checkout, you can ship it.

---

<!-- _kind: title -->

# Thank you :rocket:

github.com/go-slides/slides

<!--
Pictures on these slides are original SVGs by the go-slides authors, CC0 1.0.
-->
