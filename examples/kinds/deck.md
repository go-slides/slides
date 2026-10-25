---
slides: true
theme: gaia
paginate: true
title: Slide kinds
description: A slide says what it is (title, chapter, quote) and a kinds file says how each looks.
footer: "{{ .title }} · {{ page }}/{{ total }}"
kinds: kinds/brand.yaml
---

<!-- _kind: title -->

# Slide kinds

A slide says **what** it is. A kinds file says how it looks.

---

<!-- _kind: chapter -->

# Part one

The built-in kinds, and one of ours.

---

## Built-in kinds

| Kind      | What it is                                    |
| --------- | --------------------------------------------- |
| `title`   | a cover: no header, footer or page number   |
| `section` | a chapter break                             |
| `content` | the deck's defaults                         |
| `blank`   | nothing around the content                  |

---

<!-- _kind: callout -->

## Our own kinds

The file `kinds/brand.yaml` defines `chapter`, `callout` and `quote`.
This slide is a `callout`: a warm background and a header, from one word.

---

<!-- _kind: quote -->

> Directives written by hand always win over the kind.

---

<!-- _kind: chapter -->

# Part two

Switching kinds clears what the previous kind set, so nothing leaks.
