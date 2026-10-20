package interp

import (
	"slices"
	"strconv"
	"strings"
	"testing"
)

// The scanners copy plain runs in one write instead of byte by byte. The
// byte-by-byte versions they replaced are kept here as the oracle.

func markLinesRef(body string, delims [2]string) string {
	left, right := delims[0], delims[1]
	if left == "" {
		left = "{{"
	}
	if right == "" {
		right = "}}"
	}
	var b strings.Builder
	inAction, inMask, trimming := false, false, false
	var quote byte
	line := 0
	atLineStart := true
	for i := 0; i < len(body); {
		if trimming && !strings.ContainsRune(" \t\r\n", rune(body[i])) {
			trimming = false
		}
		if atLineStart && !inAction && !inMask && !trimming {
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

func unmarkLinesRef(s string) (string, []int) {
	var b strings.Builder
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
	return b.String(), append(lines, cur)
}

func maskSpansRef(out *strings.Builder, line, left, right string, mask func(string)) {
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

func checkFastPaths(t *testing.T, src string, delims [2]string) {
	t.Helper()
	for _, d := range [][2]string{delims, {}} {
		if got, want := markLines(src, d), markLinesRef(src, d); got != want {
			t.Fatalf("markLines(%q, %q) = %q, want %q", src, d, got, want)
		}
		left, right := d[0], d[1]
		if left == "" {
			left, right = "{{", "}}"
		}
		var gotOut, wantOut strings.Builder
		var gotMasked, wantMasked []string
		maskSpans(&gotOut, src, left, right, func(s string) { gotMasked = append(gotMasked, s) })
		maskSpansRef(&wantOut, src, left, right, func(s string) { wantMasked = append(wantMasked, s) })
		if gotOut.String() != wantOut.String() || !slices.Equal(gotMasked, wantMasked) {
			t.Fatalf("maskSpans(%q) = %q %q, want %q %q", src, gotOut.String(), gotMasked, wantOut.String(), wantMasked)
		}
	}
	marked := markLinesRef(src, delims) + src
	gs, gl := unmarkLines(marked)
	ws, wl := unmarkLinesRef(marked)
	if gs != ws || !slices.Equal(gl, wl) {
		t.Fatalf("unmarkLines(%q) = %q %v, want %q %v", marked, gs, gl, ws, wl)
	}
}

func TestFastPathsMatchByteByByte(t *testing.T) {
	for _, src := range []string{
		"", "plain\ntext\n", "a {{ .x }} b\n{{- .y -}}\n  c", "`code {{x}}` and ``two ` ticks``\n",
		"{{ \"q}}uote\" }}\n" + maskOpen + "3\n\n" + maskClose + "after\n",
		lineOpen + "7" + lineClose + "x\n" + lineOpen + "bad", "<% .x %>\n<%- .y %>", "é\n{{\n",
	} {
		checkFastPaths(t, src, [2]string{"<%", "%>"})
	}
}

func FuzzFastPathsMatchByteByByte(f *testing.F) {
	f.Add("a {{ .x }} `b` c\n{{- y -}}\n", "{{", "}}")
	f.Add(maskOpen+"1\n"+maskClose+" x "+lineOpen+"2"+lineClose, "<%", "%>")
	f.Fuzz(func(t *testing.T, src, l, r string) {
		if l == "" || r == "" {
			return
		}
		checkFastPaths(t, src, [2]string{l, r})
	})
}
