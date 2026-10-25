---
slides: true
theme: default
paginate: true
title: Images and backgrounds
description: Inline images with sizes and filters, full backgrounds, and split backgrounds.
footer: Images are original SVGs by the go-slides authors, released under CC0 1.0.
---

# Images and backgrounds

Everything on these slides is an SVG in `media/`.

![bg](media/hills.svg)

---

## An inline image, sized

![w:260](media/mark.svg)

`![w:260](media/mark.svg)` sets the width. `h:` sets the height.

---

## Filters

![w:220 grayscale](media/mark.svg) ![w:220 sepia](media/mark.svg) ![w:220 blur:4px](media/mark.svg)

`grayscale`, `sepia` and `blur:4px`, as in Marp.

---

![bg right:40%](media/grid.svg)

## A split background

```markdown
![bg right:40%](media/grid.svg)
```

The image takes the right 40% (`left` takes the left) and the text flows into the rest.

---

<!-- _class: invert -->
<!-- _footer: "" -->
<!-- _paginate: false -->

![bg](media/grid.svg)
![bg](media/hills.svg)

## Several backgrounds

Two `![bg]` images share the slide side by side.

---

![bg fit](media/mark.svg)

---

<!-- _footer: "" -->

## Attribution

`mark.svg`, `hills.svg` and `grid.svg` are original work of the go-slides
authors, released under [CC0 1.0](https://creativecommons.org/publicdomain/zero/1.0/).
You can reuse them without asking.
