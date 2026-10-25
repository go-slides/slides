---
slides: true
theme: default
paginate: true
title: The default theme
description: Marp's default theme, as a clean light deck for documentation and reports.
footer: go-slides · default theme
---

# The default theme

A clean, light deck: black text on white, a blue accent, room to breathe.

---

## Headings, lists, quotes

- Level-one and level-two headings get a rule under them
- **Bold**, *italic*, `inline code` and [links](https://github.com/go-slides/slides)
- Lists nest:
  - like this
  - and this

> A quotation is set apart by a bar on its left.

---

## Tables and code

| Theme   | Feels like     | Best for           |
| ------- | -------------- | ------------------ |
| default | a clean page   | documentation      |
| gaia    | a warm poster  | talks              |
| uncover | a calm stage   | keynotes           |

```go
func main() {
	fmt.Println("hello, slides")
}
```

---

<!-- _class: invert -->

## Invert

`<!-- _class: invert -->` flips one slide to light-on-dark, whatever the
theme. Nothing else changes.
