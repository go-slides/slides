// Command genstems writes emoji/stems.txt, the stems of the Twemoji set
// (go-icons/twemoji's Stems), one a line. Package emoji recognizes emoji from
// that table, so that a Provider holds only the SVG data. A test in
// emoji/embedded fails when the table and the set differ: run
//
//	go run ./internal/genstems emoji/stems.txt
//
// after raising go-icons/twemoji.
package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/go-icons/twemoji"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: genstems emoji/stems.txt")
		os.Exit(2)
	}
	if err := os.WriteFile(os.Args[1], []byte(strings.Join(twemoji.Stems(), "\n")+"\n"), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
