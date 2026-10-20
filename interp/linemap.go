package interp

import (
	"bytes"
	"strconv"
	"strings"
	"text/template"
)

// Line marks tag the template's own lines so that, after execution, every
// output line can be traced back to the source line that produced it: a
// line copied as it is, a line a {{range}} repeated, or the first line of
// what an {{include}} brought in. Like the sentinels they live in the
// private-use area, and they are removed before anyone sees the output.
const (
	lineOpen  = ""
	lineClose = ""
)

// ExpandLines is Expand, also returning the source line (0-based, in src)
// of each output line. Lines a template generates without copying a source
// line (an included fragment, the inside of a code block) take the line of
// the last source line before them.
//
// A line whose first action trims the whitespace before it ({{- ...) is
// not marked: a mark there would stop the trim and change the output.
// Errors are those of Expand, with Expand's columns.
func ExpandLines(src string, opts Options) (string, []int, error) {
	var masked []string
	body := src
	if !opts.Code {
		body, masked = maskCode(src, opts.Delims)
	}
	marked := markLines(body, opts.Delims)
	out, err := execute(marked, opts)
	if err != nil {
		// The marks shift the columns errors report: fail the way Expand
		// does. (Should the marked template alone fail, the expansion
		// comes back unmapped rather than not at all.)
		s, err := Expand(src, opts)
		return s, nil, err
	}
	text, lines := unmarkLines(out)
	return unmaskCode(text, masked), lines, nil
}

// markLines puts a mark at the start of every line of a (masked) template
// that is outside an action and outside a code placeholder.
func markLines(body string, delims [2]string) string {
	left, right := delims[0], delims[1]
	if left == "" {
		left = "{{"
	}
	if right == "" {
		right = "}}"
	}
	var b strings.Builder
	b.Grow(len(body) + len(body)/8)
	inAction, inMask := false, false
	// trimming is set after an action that trims the white space after it
	// (-}}): a mark there would stop the trim, so none is written before
	// the next character that is not white space.
	trimming := false
	var quote byte // the quote of a string inside an action, 0 if none
	line := 0
	atLineStart := true
	for i := 0; i < len(body); {
		if trimming && !strings.ContainsRune(" \t\r\n", rune(body[i])) {
			trimming = false
		}
		if atLineStart && !inAction && !inMask && !trimming {
			// {{- trims all the white space before it, blank lines
			// included: no mark on a line it would reach.
			rest := strings.TrimLeft(body[i:], " \t\r\n")
			if !strings.HasPrefix(rest, left+"-") {
				b.WriteString(lineOpen + strconv.Itoa(line) + lineClose)
			}
		}
		atLineStart = false
		switch {
		case inAction && quote != 0:
			c := body[i]
			if c == '\\' && quote != '`' && i+1 < len(body) {
				b.WriteString(body[i : i+2])
				i += 2
				continue
			}
			if c == quote {
				quote = 0
			}
		case inAction && (body[i] == '"' || body[i] == '`' || body[i] == '\''):
			quote = body[i]
		case inAction && strings.HasPrefix(body[i:], right):
			inAction = false
			trimming = i > 0 && body[i-1] == '-'
			b.WriteString(right)
			i += len(right)
			continue
		case !inAction && !inMask && strings.HasPrefix(body[i:], left):
			inAction = true
			b.WriteString(left)
			i += len(left)
			continue
		case !inAction && strings.HasPrefix(body[i:], maskOpen):
			inMask = true
		case inMask && strings.HasPrefix(body[i:], maskClose):
			inMask = false
		}
		if body[i] == '\n' {
			line++
			atLineStart = true
		}
		b.WriteByte(body[i])
		i++
	}
	return b.String()
}

// unmarkLines removes the marks and returns, for each line of the result,
// the source line its first mark names, or the last mark before it.
func unmarkLines(s string) (string, []int) {
	var b strings.Builder
	b.Grow(len(s))
	lines := []int{}
	last, cur := 0, -1
	for i := 0; i < len(s); {
		if strings.HasPrefix(s[i:], lineOpen) {
			end := strings.Index(s[i+len(lineOpen):], lineClose)
			if end >= 0 {
				if n, err := strconv.Atoi(s[i+len(lineOpen) : i+len(lineOpen)+end]); err == nil {
					last = n
					if cur < 0 {
						cur = n
					}
				}
				i += len(lineOpen) + end + len(lineClose)
				continue
			}
		}
		if s[i] == '\n' {
			if cur < 0 {
				cur = last
			}
			lines = append(lines, cur)
			cur = -1
		}
		b.WriteByte(s[i])
		i++
	}
	if cur < 0 {
		cur = last
	}
	lines = append(lines, cur)
	return b.String(), lines
}

// execute parses and runs a template body; Expand's core.
func execute(body string, opts Options) (string, error) {
	name := opts.Name
	if name == "" {
		name = "deck"
	}
	t := template.New(name).Option("missingkey=error").Funcs(Funcs(opts))
	if opts.Delims[0] != "" || opts.Delims[1] != "" {
		t = t.Delims(opts.Delims[0], opts.Delims[1])
	}
	t, err := t.Parse(body)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, opts.Data); err != nil {
		return "", err
	}
	return buf.String(), nil
}
