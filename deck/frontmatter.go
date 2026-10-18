package deck

import "strings"

// splitFrontMatter separates a leading YAML front matter the way
// markdown-it-front-matter does for Marpit. The document must start with a
// run of at least three dashes (whatever follows on that line is ignored).
// The block ends at a line indented by at most three spaces that holds a run
// of at least as many dashes and nothing but spaces after it. A front matter
// that is never closed swallows the rest of the document: Marpit then renders
// one empty slide, and so do we.
//
// src must already have its line endings normalized to "\n". lines is how
// many source lines the front matter took, so positions in body can be
// reported against the original file.
func splitFrontMatter(src string) (fm string, body string, lines int, ok bool) {
	n := 0
	for n < len(src) && src[n] == '-' {
		n++
	}
	if n < 3 {
		return "", src, 0, false
	}
	_, rest, found := strings.Cut(src, "\n")
	if !found {
		return "", "", 1, true
	}
	var b strings.Builder
	lines = 1
	for rest != "" {
		line, after, _ := strings.Cut(rest, "\n")
		lines++
		if closesFrontMatter(line, n) {
			return b.String(), after, lines, true
		}
		b.WriteString(line + "\n")
		rest = after
	}
	return b.String(), "", lines, true
}

func closesFrontMatter(line string, n int) bool {
	t := strings.TrimLeft(line, " ")
	if len(line)-len(t) > 3 {
		return false
	}
	d := 0
	for d < len(t) && t[d] == '-' {
		d++
	}
	return d >= n && strings.Trim(t[d:], " ") == ""
}
