---
slides: true
theme: default
paginate: true
title: Code with line numbers and callouts
description: Highlighted code, optional line numbers, and numbered callouts that point at notes below the block.
---

# Code

Highlighting, line numbers and callouts.

---

## Highlighting

```go
package main

import "fmt"

func main() {
    for i := 1; i <= 3; i++ {
        fmt.Println("slide", i)
    }
}
```

Colours come from the theme, through highlight.js class names.

---

## Line numbers

```python {lineNumbers}
def fizzbuzz(n):
    for i in range(1, n + 1):
        if i % 15 == 0:
            print("FizzBuzz")
        elif i % 3 == 0:
            print("Fizz")
        elif i % 5 == 0:
            print("Buzz")
        else:
            print(i)
```

`{lineNumbers}` after the language. They are not copied with the code.

---

## Starting somewhere else

```js {lineNumbers=42}
export function clamp(x, lo, hi) {
  return Math.min(Math.max(x, lo), hi);
}
```

`{lineNumbers=42}` starts at 42, for a snippet that lives in the middle of a
file.

---

## Callouts

```go {lineNumbers}
func serve(addr string) error {
    mux := http.NewServeMux() // <1>
    mux.HandleFunc("/", home) // <2>
    return http.ListenAndServe(addr, mux) // <3>
}
```

1. A router of our own, not the global one.
2. Every request not matched elsewhere lands here.
3. Blocks until the server fails.

---

## Hover to connect

Point at a disc, or tab to it: its note lights up, and the other way round.

```sh
slides build deck.md # <1>
slides deck.md --pdf # <2>
```

1. Writes `deck.html`.
2. Writes `deck.pdf`, with vectors and selectable text.
