---
slides: true
theme: default
paginate: true
title: Go templates
description: Data, range loops that generate slides, page numbers and include, all before the Markdown is parsed.
footer: "{{ .team }} · {{ page }}/{{ total }}"
data:
  team: Platform team
  quarter: Q3
  releases:
    - {name: Aurora, date: "2026-07-14", highlights: [Faster builds, New CLI]}
    - {name: Borealis, date: "2026-08-25", highlights: [Plugin API, Better errors, Docs search]}
    - {name: Cirrus, date: "2026-09-29", highlights: [Windows support]}
---

# {{ .team }}: {{ .quarter }} releases

{{ len .releases }} releases this quarter, one slide each.

---

## Agenda

{{ range $i, $r := .releases }}{{ add $i 1 }}. {{ $r.name }} — {{ $r.date }}
{{ end }}
{{ range .releases }}
---

## {{ .name }}

Released {{ .date }}.

{{ range .highlights }}- {{ . }}
{{ end }}
{{ end }}
---

## What happened here

The `range` above wrote **{{ len .releases }}** slides. This one is number
**{{ page }}** of **{{ total }}**, and the deck never says so by hand.

Code stays untouched: `{{ .name }}` is shown as text, not run.

{{ include "parts/thanks.md" }}
