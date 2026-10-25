---
slides: true
theme: uncover
paginate: true
title: Presenting
description: Transitions between slides, speaker notes, and the presenter and overview views of the HTML page.
transition: fade 0.5s
---

# Presenting

Open this deck in a browser and press **p**.

<!--
Welcome. This note is only for you: it shows in the presenter view, never on
the slide. Press `p` for that view, `o` or Escape for the overview.
-->

---

## Speaker notes

Any HTML comment that is not a directive is a speaker note.

<!--
Notes can run several lines and use plain text.

Keep each one to what you would say, not what is on the slide.
-->

---

<!-- _transition: slide 0.6s -->

## Transitions

```markdown
<!-- transition: fade 0.5s -->
<!-- _transition: slide 0.6s -->
```

The first sets one for every slide after it, the second for this slide alone.
This one slides.

<!-- This slide has its own transition, `slide`. The others fade. -->

---

<!-- _transition: cover 0.8s -->

## 33 built in

fade, slide, cover, zoom, cube, swipe… the names are marp-cli's, played with
the browser's View Transition API.

<!-- Browsers without the API get a plain fade, and so does anyone who asked for reduced motion. -->

---

<!-- _transition: zoom 0.8s -->

## The overview

Press **o** or **Escape**: every slide in a grid. Arrow keys move, Enter goes.

<!-- The overview syncs with the presenter view. -->

---

# Thank you

<!-- Take questions here. The timer in the presenter view tells you how long you took. -->
