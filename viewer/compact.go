package viewer

import "strings"

// compact makes the page's own scripts and styles smaller without changing
// them: it drops the lines that are only a comment, the indentation and the
// blank lines. Line ends stay, so that automatic semicolon insertion works as
// written. It is not a minifier and does not read the language: the scripts
// here keep no multi-line strings and no comment marker inside a string at the
// start of a line, which the tests check by running the result.
func compact(s string) string {
	var b strings.Builder
	for _, l := range strings.Split(s, "\n") {
		l = strings.TrimSpace(l)
		if l == "" || strings.HasPrefix(l, "//") {
			continue
		}
		b.WriteString(l)
		b.WriteByte('\n')
	}
	return b.String()
}
