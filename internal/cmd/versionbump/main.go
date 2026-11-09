// Command versionbump moves the described go-slides version in the docs and
// the landing:
//
//	versionbump docs <root> v0.16.0 v0.17.1
//	versionbump landing <root> v0.16.0 v0.17.1
//
// The docs: the "this page describes" line of cli.md (its first match only),
// _index, getting-started, playground, the install line of self-hosting,
// hugo.yaml, the README. The landing: only the CURRENT-version fields of
// hugo.toml and the languages' params files (the lines that start with
// version, label or blurb), the README lines. History is left as written: a
// card's "Since v0.15.0" (a what line) and its when = "v0.15.0".
package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

var langs = []string{"en", "de", "es", "fr", "it", "pl", "pt", "ru", "uk", "ja", "ko", "zh"}

// rewrite replaces each pair's first element by its second in a file: every
// occurrence, or only the first. When only is set, only the lines it accepts
// are changed (it gets the line as written).
func rewrite(out io.Writer, f string, first bool, only func(line string) bool, pairs ...string) error {
	b, err := os.ReadFile(f)
	if err != nil {
		return err
	}
	lines := strings.SplitAfter(string(b), "\n")
	n := 0
	done := map[int]bool{}
	for li, line := range lines {
		if only != nil && !only(line) {
			continue
		}
		for i := 0; i < len(pairs); i += 2 {
			if first && done[i] {
				continue
			}
			c := strings.Count(line, pairs[i])
			if c == 0 {
				continue
			}
			n += c
			if first {
				line = strings.Replace(line, pairs[i], pairs[i+1], 1)
				done[i] = true
			} else {
				line = strings.ReplaceAll(line, pairs[i], pairs[i+1])
			}
		}
		lines[li] = line
	}
	if err := os.WriteFile(f, []byte(strings.Join(lines, "")), 0o644); err != nil {
		return err
	}
	fmt.Fprintln(out, f, n)
	return nil
}

// minor: v0.17.1 -> 0.17
func minor(v string) string {
	p := strings.Split(strings.TrimPrefix(v, "v"), ".")
	if len(p) < 2 {
		return p[0]
	}
	return p[0] + "." + p[1]
}

// current accepts the lines of a landing file that state the current version:
// the version parameter, a link's label, a link's blurb. A card's what and
// when lines are history.
func current(line string) bool {
	t := strings.TrimSpace(line)
	for _, k := range []string{"version", "label", "blurb"} {
		if rest, ok := strings.CutPrefix(t, k); ok && strings.HasPrefix(strings.TrimSpace(rest), "=") {
			return true
		}
	}
	return false
}

func run(out io.Writer, kind, root, from, to string) error {
	var err error
	step := func(e error) {
		if err == nil {
			err = e
		}
	}
	switch kind {
	case "docs":
		for _, l := range langs {
			c := filepath.Join(root, "content", l)
			step(rewrite(out, filepath.Join(c, "_index.md"), false, nil, from, to, "/docs/"+minor(from)+"/", "/docs/"+minor(to)+"/"))
			step(rewrite(out, filepath.Join(c, "cli.md"), true, nil, from, to))
			step(rewrite(out, filepath.Join(c, "getting-started.md"), false, nil, from, to))
			step(rewrite(out, filepath.Join(c, "playground.md"), true, nil, from, to))
			step(rewrite(out, filepath.Join(c, "self-hosting.md"), false, nil, "@"+from, "@"+to))
		}
		step(rewrite(out, filepath.Join(root, "hugo.yaml"), false, nil, `version: "`+from+`"`, `version: "`+to+`"`))
		step(rewrite(out, filepath.Join(root, "README.md"), false, nil, from+" (`params.slides.version`) and are published under `"+minor(from)+"/`", to+" (`params.slides.version`) and are published under `"+minor(to)+"/`"))
	case "landing":
		step(rewrite(out, filepath.Join(root, "hugo.toml"), false, current, from, to, `"v`+minor(from)+`"`, `"v`+minor(to)+`"`))
		for _, l := range langs[1:] {
			step(rewrite(out, filepath.Join(root, "config", "_default", "params."+l+".toml"), false, current, from, to))
		}
		step(rewrite(out, filepath.Join(root, "README.md"), false, nil, "released as "+from+".", "released as "+to+".", "docs/"+minor(from)+"/self-hosting/", "docs/"+minor(to)+"/self-hosting/"))
	default:
		return fmt.Errorf("versionbump: %q is not docs or landing", kind)
	}
	return err
}

func main() {
	if len(os.Args) != 5 {
		fmt.Fprintln(os.Stderr, "usage: versionbump docs|landing <root> <from> <to>")
		os.Exit(2)
	}
	if err := run(os.Stdout, os.Args[1], os.Args[2], os.Args[3], os.Args[4]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
