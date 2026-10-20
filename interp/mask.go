package interp

import (
	"strconv"
	"strings"
)

// Placeholders stand for code set aside during execution. Like the
// sentinels they live in the private-use area, so neither the template
// parser nor the template's own output can produce one by accident.
const (
	maskOpen  = ""
	maskClose = ""
)

// maskCode replaces the contents of fenced code blocks and of inline code
// spans with placeholders, returning the masked text and the originals.
//
// Fences follow CommonMark: a run of at least three backticks or tildes
// indented by at most three spaces (block quote markers allowed) opens a
// block that a run of the same character, at least as long, closes; an
// unclosed fence runs to the end of the document. Fence lines themselves stay
// visible so a template can still wrap a whole block. Inline spans are matched
// within one line, and never inside a template action, where backticks are
// Go raw strings.
func maskCode(src string, delims [2]string) (string, []string) {
	left, right := delims[0], delims[1]
	if left == "" {
		left = "{{"
	}
	if right == "" {
		right = "}}"
	}
	var (
		out    strings.Builder
		masked []string
		fence  string // the open fence's marker run, "" outside a block
		block  strings.Builder
	)
	mask := func(s string) {
		// The placeholder keeps the code's line breaks, so that template
		// errors after a code block report the deck's own line numbers.
		out.WriteString(maskOpen + strconv.Itoa(len(masked)) + strings.Repeat("\n", strings.Count(s, "\n")) + maskClose)
		masked = append(masked, s)
	}
	lines := strings.SplitAfter(src, "\n")
	for _, line := range lines {
		if fence != "" {
			if closesFence(line, fence) {
				if block.Len() > 0 {
					mask(block.String())
					block.Reset()
				}
				out.WriteString(line)
				fence = ""
				continue
			}
			block.WriteString(line)
			continue
		}
		if f := opensFence(line); f != "" {
			fence = f
			out.WriteString(line)
			continue
		}
		maskSpans(&out, line, left, right, mask)
	}
	if block.Len() > 0 {
		mask(block.String())
	}
	return out.String(), masked
}

// fenceBody strips block quote markers and up to three spaces of indent.
func fenceBody(line string) string {
	s := line
	for {
		t := strings.TrimLeft(s, " ")
		if len(s)-len(t) > 3 {
			return ""
		}
		if strings.HasPrefix(t, ">") {
			s = t[1:]
			continue
		}
		return t
	}
}

func opensFence(line string) string {
	s := fenceBody(line)
	if s == "" {
		return ""
	}
	c := s[0]
	if c != '`' && c != '~' {
		return ""
	}
	n := 0
	for n < len(s) && s[n] == c {
		n++
	}
	if n < 3 {
		return ""
	}
	// A backtick fence's info string cannot contain a backtick.
	if c == '`' && strings.Contains(s[n:], "`") {
		return ""
	}
	return s[:n]
}

func closesFence(line, fence string) bool {
	s := fenceBody(line)
	n := 0
	for n < len(s) && s[n] == fence[0] {
		n++
	}
	return n >= len(fence) && strings.TrimSpace(s[n:]) == ""
}

// maskSpans copies line to out, masking the inside of inline code spans.
func maskSpans(out *strings.Builder, line, left, right string, mask func(string)) {
	i := 0
	for i < len(line) {
		if strings.HasPrefix(line[i:], left) {
			end := strings.Index(line[i+len(left):], right)
			if end < 0 {
				out.WriteString(line[i:])
				return
			}
			end += i + len(left) + len(right)
			out.WriteString(line[i:end])
			i = end
			continue
		}
		if line[i] != '`' {
			out.WriteByte(line[i])
			i++
			continue
		}
		n := 0
		for i+n < len(line) && line[i+n] == '`' {
			n++
		}
		run := line[i : i+n]
		closeAt := findRun(line, i+n, n)
		if closeAt < 0 {
			out.WriteString(run)
			i += n
			continue
		}
		out.WriteString(run)
		mask(line[i+n : closeAt])
		out.WriteString(run)
		i = closeAt + n
	}
}

// findRun finds the next run of exactly n backticks at or after from.
func findRun(s string, from, n int) int {
	for j := from; j < len(s); {
		if s[j] != '`' {
			j++
			continue
		}
		k := j
		for k < len(s) && s[k] == '`' {
			k++
		}
		if k-j == n {
			return j
		}
		j = k
	}
	return -1
}

func unmaskCode(s string, masked []string) string {
	if len(masked) == 0 {
		return s
	}
	var out strings.Builder
	for {
		i := strings.Index(s, maskOpen)
		if i < 0 {
			out.WriteString(s)
			return out.String()
		}
		j := strings.Index(s[i:], maskClose)
		if j < 0 {
			out.WriteString(s)
			return out.String()
		}
		n, err := strconv.Atoi(strings.TrimRight(s[i+len(maskOpen):i+j], "\n"))
		out.WriteString(s[:i])
		if err == nil && n >= 0 && n < len(masked) {
			out.WriteString(masked[n])
		} else {
			out.WriteString(s[i : i+j+len(maskClose)])
		}
		s = s[i+j+len(maskClose):]
	}
}
