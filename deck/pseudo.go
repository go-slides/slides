package deck

import (
	"strconv"
	"strings"
)

// pseudoAfter writes the element that stands for the slide's ::after when
// the layout engine generates no content, with the text the theme's content
// expression gives this slide.
func (r *slideRenderer) pseudoAfter(s *Slide) string {
	attrs := map[string]string{
		"data-marpit-pagination":       strconv.Itoa(s.Page),
		"data-marpit-pagination-total": strconv.Itoa(s.Total),
	}
	for _, k := range s.Dirs.Keys() {
		attrs["data-"+kebab(k)] = s.Dirs.Get(k)
	}
	text := strconv.Itoa(s.Page)
	if r.opts.AfterContent != "" {
		text = evalContent(r.opts.AfterContent, attrs)
	}
	return `<span data-slides-pseudo="after">` + escapeHTML(text) + "</span>"
}

// evalContent evaluates the subset of a CSS content value a slide's ::after
// uses (CSS Generated Content 3 §2, the content property): a sequence of
// strings and attr(name), concatenated. none and normal give nothing; an
// absent attribute gives the empty string; anything else (counters, quotes,
// images) is left out.
func evalContent(v string, attrs map[string]string) string {
	var b strings.Builder
	for i := 0; i < len(v); {
		c := v[i]
		switch {
		case c == '"' || c == '\'':
			s, n := cssString(v[i:])
			b.WriteString(s)
			i += n
		case strings.HasPrefix(v[i:], "attr("):
			end := strings.IndexByte(v[i:], ')')
			if end < 0 {
				return b.String()
			}
			name := strings.TrimSpace(v[i+len("attr(") : i+end])
			// attr(name type, fallback): only the name matters here.
			name, _, _ = strings.Cut(name, ",")
			name, _, _ = strings.Cut(strings.TrimSpace(name), " ")
			b.WriteString(attrs[name])
			i += end + 1
		default:
			i++
		}
	}
	return b.String()
}

// cssString reads a quoted CSS string at the start of v, resolving its
// escapes, and returns it with the number of bytes read.
func cssString(v string) (string, int) {
	q := v[0]
	var b strings.Builder
	i := 1
	for i < len(v) {
		c := v[i]
		switch {
		case c == q:
			return b.String(), i + 1
		case c == '\\' && i+1 < len(v):
			// A hex escape: up to six hex digits and one optional space.
			j := i + 1
			for j < len(v) && j < i+7 && isHex(v[j]) {
				j++
			}
			if j > i+1 {
				n, _ := strconv.ParseUint(v[i+1:j], 16, 32)
				b.WriteRune(rune(n))
				if j < len(v) && v[j] == ' ' {
					j++
				}
				i = j
				continue
			}
			if v[i+1] != '\n' {
				b.WriteByte(v[i+1])
			}
			i += 2
		default:
			b.WriteByte(c)
			i++
		}
	}
	return b.String(), i
}

func isHex(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'
}
