---
slides: true
theme: default
paginate: true
title: Math
description: Inline and display formulas, typeset to SVG at build time and identical in HTML, images and PDF.
---

# Math

`$…$` inline, `$$…$$` display. No JavaScript, no network.

---

## Inline

Einstein wrote $E = mc^2$, Euler $e^{i\pi} + 1 = 0$, and the roots of
$ax^2 + bx + c = 0$ are what the next slide is about.

A formula follows the text's size and sits on its baseline: $\alpha + \beta$,
$\sqrt{2}$, $x_{n+1}$, $\sum_{k=1}^{n} k$.

---

## The quadratic formula

$$
x = \frac{-b \pm \sqrt{b^2 - 4ac}}{2a}
$$

Display formulas are centred and sized for the slide.

---

## Calculus and sums

$$
\int_{-\infty}^{\infty} e^{-x^2}\,dx = \sqrt{\pi}
\qquad
\sum_{n=1}^{\infty} \frac{1}{n^2} = \frac{\pi^2}{6}
$$

---

## Matrices

$$
A = \begin{pmatrix} 1 & 2 \\ 3 & 4 \end{pmatrix},
\qquad
\det A = 1 \cdot 4 - 2 \cdot 3 = -2
$$

---

## Prices are not math

Following Marp Core's rules, a `$` followed by a space does not open a formula
and one followed by a digit does not close it: this costs $5 and that costs
$10, and both stay text.
