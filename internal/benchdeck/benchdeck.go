// Package benchdeck generates the decks go-slides is benchmarked on, so that
// the Go benchmarks and the slidesbench command (go-slides against marp-cli)
// measure the same input. The decks are deterministic: the same call gives
// the same bytes.
package benchdeck

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"strings"
)

// Large is a deck of n slides in the default theme that exercises what a
// real talk has: headings, paragraphs with inline markup, nested lists,
// fenced code, tables, and a background image given as a data: URI.
func Large(n int) []byte {
	var b strings.Builder
	b.WriteString("---\nmarp: true\npaginate: true\ntitle: Benchmark deck\n---\n\n")
	bg := Background()
	for i := 1; i <= n; i++ {
		if i > 1 {
			b.WriteString("\n---\n\n")
		}
		switch i % 5 {
		case 0:
			fmt.Fprintf(&b, "![bg right:40%%](%s)\n\n# Slide %d: an image\n\nThe background is a PNG in a `data:` URI.\n", bg, i)
		case 1:
			fmt.Fprintf(&b, "# Slide %d: text\n\nA paragraph with **strong**, *emphasis*, `code` and a [link](https://example.com/%d).\n"+
				"It wraps over several lines so that the layout engine has a real paragraph\n"+
				"to break, with words of different lengths, punctuation; and numbers 1234.\n", i, i)
		case 2:
			fmt.Fprintf(&b, "## Slide %d: lists\n\n- first item\n- second item\n  - nested *one*\n  - nested **two**\n- third item\n\n1. ordered\n2. list\n3. here\n", i)
		case 3:
			fmt.Fprintf(&b, "## Slide %d: code\n\n```go\nfunc fib(n int) int {\n\tif n < 2 {\n\t\treturn n\n\t}\n\treturn fib(n-1) + fib(n-2) // %d\n}\n```\n", i, i)
		case 4:
			fmt.Fprintf(&b, "## Slide %d: table\n\n| Tool | Mode | Time |\n|------|:----:|-----:|\n| a | html | %d ms |\n| b | pdf | %d ms |\n| c | png | %d ms |\n", i, i, 2*i, 3*i)
		}
	}
	return []byte(b.String())
}

// Gaia is a deck of n slides in the gaia theme, which asks for web fonts
// (Lato, Roboto Mono) and uses the lead and invert classes.
func Gaia(n int) []byte {
	var b strings.Builder
	b.WriteString("---\nmarp: true\ntheme: gaia\npaginate: true\n---\n\n<!-- _class: lead -->\n\n# Gaia deck\n\nA title slide\n")
	for i := 2; i <= n; i++ {
		b.WriteString("\n---\n\n")
		if i%4 == 0 {
			b.WriteString("<!-- _class: invert -->\n\n")
		}
		fmt.Fprintf(&b, "## Gaia slide %d\n\n- Point one with `mono`\n- Point **two**\n\n```js\nconsole.log(%d)\n```\n", i, i)
	}
	return []byte(b.String())
}

// MathCode is a deck of n slides in the default theme with inline and
// display math, left to each tool's default typesetter (marp-cli's is
// MathJax, go-slides' go-tex/math), and highlighted code in several
// languages.
func MathCode(n int) []byte {
	var b strings.Builder
	b.WriteString("---\nmarp: true\npaginate: true\n---\n\n")
	codes := []string{
		"```go\nfunc sum(xs []int) (s int) {\n\tfor _, x := range xs {\n\t\ts += x\n\t}\n\treturn s\n}\n```\n",
		"```python\ndef mean(xs):\n    \"\"\"The arithmetic mean.\"\"\"\n    return sum(xs) / len(xs)\n```\n",
		"```js\nconst sq = (x) => x * x;\nconsole.log([1, 2, 3].map(sq));\n```\n",
		"```rust\nfn main() {\n    let v: Vec<u32> = (1..=10).collect();\n    println!(\"{}\", v.iter().sum::<u32>());\n}\n```\n",
	}
	for i := 1; i <= n; i++ {
		if i > 1 {
			b.WriteString("\n---\n\n")
		}
		fmt.Fprintf(&b, "## Slide %d: $e^{i\\pi} + 1 = 0$\n\nInline $\\sum_{k=1}^{%d} k = \\frac{%d(%d+1)}{2}$ and $\\sqrt{x^2+y^2}$.\n\n", i, i, i, i)
		if i%2 == 0 {
			b.WriteString("$$\n\\int_0^\\infty e^{-x^2}\\,dx = \\frac{\\sqrt{\\pi}}{2}\n$$\n\n")
		} else {
			b.WriteString("$$\nA = \\begin{pmatrix} a & b \\\\ c & d \\end{pmatrix},\\quad \\det A = ad - bc\n$$\n\n")
		}
		b.WriteString(codes[i%len(codes)])
	}
	return []byte(b.String())
}

// Background is a 640×360 gradient PNG as a data: URI.
func Background() string {
	const w, h = 640, 360
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetNRGBA(x, y, color.NRGBA{R: uint8(x * 255 / w), G: uint8(y * 255 / h), B: uint8((x + y) % 256), A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		panic(err) // encoding an in-memory image cannot fail
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())
}
