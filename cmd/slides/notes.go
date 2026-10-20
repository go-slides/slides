package main

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/go-slides/slides"
	"github.com/go-slides/slides/deck"
)

// notesText is marp-cli's notes output: each slide's notes joined by a blank
// line, the slides by a line of three dashes between blank lines. A deck with
// no notes at all gives nothing.
func notesText(d *deck.Deck) string {
	parts := make([]string, len(d.Slides))
	has := false
	for i, s := range d.Slides {
		parts[i] = strings.Join(s.Notes, "\n\n")
		has = has || len(s.Notes) > 0
	}
	if !has {
		return ""
	}
	return strings.Join(parts, "\n\n---\n\n")
}

// writeNotes is build --notes: the speaker notes as a text file.
func writeNotes(cmd *cobra.Command, o *options, src []byte, path string, cfg slides.Config) error {
	d, err := deck.Parse(src, cfg.Parse)
	if err != nil {
		return err
	}
	stderr := cmd.ErrOrStderr()
	for _, w := range d.Warnings {
		fmt.Fprintln(stderr, "slides: warning:", w)
	}
	text := notesText(d)
	if text == "" {
		fmt.Fprintf(stderr, "slides: warning: %s contains no notes.\n", path)
	}
	name := o.output
	if name == "" {
		name = "-"
		if path != "stdin.md" {
			name = strings.TrimSuffix(path, filepath.Ext(path)) + ".txt"
		}
	}
	return o.emit(cmd, path, name, []byte(text), len(d.Slides))
}
