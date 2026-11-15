// Command wasmprobe is the smallest program that renders a deck, the thing the
// size tests build for GOOS=js GOARCH=wasm: it links what a host links to turn
// Markdown into the HTML page (the deck package, its themes, the emoji and math
// data the build tags leave in or out), and nothing of a command line.
package main

import (
	"fmt"
	"os"

	"github.com/go-slides/slides"
)

func main() {
	src, err := os.ReadFile(os.Args[len(os.Args)-1])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	page, _, err := slides.HTML(src, slides.Config{})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Print(page)
}
